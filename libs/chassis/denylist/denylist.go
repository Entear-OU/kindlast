// Package denylist holds token ids that must stop working before they expire.
//
// This is the other half of the trade §1.4 makes. Verifying tokens locally
// rather than introspecting per request means a revoked token stays valid
// until it expires; short access tokens bound that window, and this closes it
// where the bound is still too long. Account deletion and an owner
// force-logging out a departing member are both cases where "up to ten more
// minutes" is the wrong answer.
//
// Infrastructure only: a set of opaque ids with expiries. It knows nothing
// about who revoked what or why (§21.5).
package denylist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// DefaultPrefix namespaces these keys away from sessions and rate limits,
// which share the instance.
const DefaultPrefix = "denylist:jti:"

// Redis is a deny-list backed by the shared Redis instance.
//
// Shared rather than in-process, and that is load-bearing: a per-replica set
// would mean a newly started replica begins with no history and happily
// accepts every token the others have revoked.
type Redis struct {
	client redis.UniversalClient
	prefix string
}

// NewRedis returns a deny-list over an existing client.
func NewRedis(client redis.UniversalClient, prefix string) *Redis {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	return &Redis{client: client, prefix: prefix}
}

// IsDenied reports whether a token has been revoked.
//
// The error is never swallowed into a false here. The caller is required to
// fail closed on it, because an unreachable deny-list that reads as "not
// revoked" silently un-revokes every token in it.
func (r *Redis) IsDenied(ctx context.Context, tokenID string) (bool, error) {
	if tokenID == "" {
		return false, errors.New("denylist: empty token id")
	}

	count, err := r.client.Exists(ctx, r.prefix+tokenID).Result()
	if err != nil {
		return false, fmt.Errorf("denylist: checking %q: %w", tokenID, err)
	}
	return count > 0, nil
}

// Deny revokes a token until the moment it would have expired anyway.
//
// The TTL is the token's own expiry, so the entry self-cleans and the
// deny-list stays the size of the currently outstanding revocations rather
// than growing forever (§15.1). A token already past its expiry is a no-op:
// adding it would be storage bought for no security.
func (r *Redis) Deny(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if tokenID == "" {
		return errors.New("denylist: empty token id")
	}

	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}

	if err := r.client.Set(ctx, r.prefix+tokenID, "revoked", ttl).Err(); err != nil {
		return fmt.Errorf("denylist: revoking %q: %w", tokenID, err)
	}
	return nil
}

// TTL reports how long a revocation has left, for the test that asserts these
// entries expire rather than accumulate.
func (r *Redis) TTL(ctx context.Context, tokenID string) (time.Duration, error) {
	return r.client.TTL(ctx, r.prefix+tokenID).Result()
}

// MaxMemoryPolicy reports the instance's eviction policy.
//
// Exposed because it is a security control rather than a tuning knob, and it
// deserves an assertion rather than a comment in a compose file. `maxmemory-policy`
// is per instance, not per key: configure this Redis as an LRU cache and the
// deny-list becomes evictable, which means that under memory pressure Redis
// silently un-revokes a token, with no error and no log line (§15.3). The
// correct value is noeviction.
func (r *Redis) MaxMemoryPolicy(ctx context.Context) (string, error) {
	values, err := r.client.ConfigGet(ctx, "maxmemory-policy").Result()
	if err != nil {
		return "", fmt.Errorf("denylist: reading maxmemory-policy: %w", err)
	}
	policy, ok := values["maxmemory-policy"]
	if !ok {
		return "", errors.New("denylist: redis reported no maxmemory-policy")
	}
	return strings.TrimSpace(policy), nil
}
