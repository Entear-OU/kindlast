package oidc

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ErrKeyNotFound is returned when a token names a key id the authorization
// server does not serve, after a refetch has been given its chance.
var ErrKeyNotFound = errors.New("oidc: no signing key for kid")

// DefaultRefetchCooldown bounds how often an unknown key id may reach the
// network.
//
// One minute is chosen against the two failure modes either side of it. Too
// short and an unknown kid becomes a request amplifier: every caller holding a
// stale or forged token drives a fetch, and the authorization server absorbs
// our traffic. Too long and a genuine key rotation is a multi-minute outage,
// because tokens signed with the new key cannot verify until the cooldown
// lapses. Access tokens live ten minutes (§1.2), so a minute is well inside
// the window a rotation has to complete in.
const DefaultRefetchCooldown = time.Minute

// KeySet is an in-process cache of an authorization server's public signing
// keys.
//
// In-process rather than in Redis, deliberately: it is a few kilobytes that
// can always be rebuilt from the network, and putting it in Redis would add a
// hop and a failure mode to something that has neither (§15.2).
//
// The whole subtlety of this type is when it goes back to the network, and it
// is worth stating plainly because getting it wrong produces an outage that
// looks like a signature bug.
//
// A freshly seeded Zitadel serves `{"keys": []}`. It generates its signing key
// lazily, on the first token it issues, so an empty set at boot is correct
// rather than broken. A cache populated once at boot would therefore hold
// nothing for the entire life of the process and reject every token that
// followed, reporting each one as an unknown key rather than as an empty
// cache. Hence the rule this type exists to enforce: **the boot fetch must
// never be the last fetch.** Warm is explicitly not counted as a refetch, so
// the first token to arrive still gets one.
//
// Found on the real stack while building the Postman collection against a
// clean checkout, and recorded at §1.4.
type KeySet struct {
	uri      string
	client   *http.Client
	cooldown time.Duration

	// now is injectable so the refetch policy can be tested without sleeping.
	now func() time.Time

	// mu guards everything below it, and is deliberately held across the HTTP
	// fetch. That serialises concurrent misses into a single request, which is
	// the property the "exactly one refetch, not one per request" criterion
	// asks for; a lock released before the fetch would let a thundering herd
	// through. It is safe to hold because the client carries a timeout, and
	// cheap because a fetch happens at most once per cooldown.
	mu          sync.Mutex
	keys        map[string]crypto.PublicKey
	refetched   bool
	lastRefetch time.Time
}

// NewKeySet returns a cache for the JWKS served at uri. It performs no I/O;
// call Warm to populate it at boot, or let the first token drive the fetch.
func NewKeySet(uri string, client *http.Client) *KeySet {
	if client == nil {
		client = defaultClient()
	}
	return &KeySet{
		uri:      uri,
		client:   client,
		cooldown: DefaultRefetchCooldown,
		now:      time.Now,
		keys:     map[string]crypto.PublicKey{},
	}
}

// SetRefetchCooldown overrides DefaultRefetchCooldown.
func (k *KeySet) SetRefetchCooldown(d time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cooldown = d
}

// Warm fetches the key set once at boot.
//
// A failure here is worth logging and not worth crashing on. `auth` and
// `core-api` start together, so losing the race is ordinary, and the first
// token to arrive will fetch anyway. Crashing on it would turn a startup
// ordering detail into a restart loop.
//
// Warm does not count as a refetch. See the type comment for why that single
// line is the difference between a working stack and one that rejects every
// token it is ever shown.
func (k *KeySet) Warm(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.fetchLocked(ctx)
}

// KeyFor returns the public key for a key id, refetching once if the id is
// unknown.
//
// An empty kid is accepted only when the server serves exactly one key, which
// keeps this usable against an IdP that omits the header on a single-key set
// without ever weakening the check to "trust whichever key happens to work"
// (§18.2 asks for portability, not for leniency about signatures).
func (k *KeySet) KeyFor(ctx context.Context, kid string) (crypto.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if key, ok := k.lookupLocked(kid); ok {
		return key, nil
	}

	if !k.mayRefetchLocked() {
		return nil, fmt.Errorf("%w: %q (refetched within the last %s)", ErrKeyNotFound, kid, k.cooldown)
	}

	// The outcome of the fetch does not change the bookkeeping: a failed or
	// fruitless refetch still starts the cooldown, or a down authorization
	// server would mean one outbound request per inbound request, which is the
	// amplification this cache exists to prevent.
	k.refetched = true
	k.lastRefetch = k.now()
	if err := k.fetchLocked(ctx); err != nil {
		return nil, fmt.Errorf("%w: %q, and the refetch failed: %v", ErrKeyNotFound, kid, err)
	}

	if key, ok := k.lookupLocked(kid); ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrKeyNotFound, kid)
}

func (k *KeySet) lookupLocked(kid string) (crypto.PublicKey, bool) {
	if kid != "" {
		key, ok := k.keys[kid]
		return key, ok
	}
	if len(k.keys) != 1 {
		return nil, false
	}
	for _, key := range k.keys {
		return key, true
	}
	return nil, false
}

// mayRefetchLocked answers the one question this cache exists to get right.
//
// The first miss always goes to the network, whatever Warm did, because Warm
// may have cached an empty set from an authorization server that had not
// generated its key yet. After that the cooldown applies.
func (k *KeySet) mayRefetchLocked() bool {
	if !k.refetched {
		return true
	}
	return k.now().Sub(k.lastRefetch) >= k.cooldown
}

func (k *KeySet) fetchLocked(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, k.uri, nil)
	if err != nil {
		return fmt.Errorf("oidc: building jwks request: %w", err)
	}

	response, err := k.client.Do(request)
	if err != nil {
		return fmt.Errorf("oidc: fetching jwks from %s: %w", k.uri, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks at %s returned %s", k.uri, response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("oidc: reading jwks: %w", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		return err
	}
	k.keys = keys
	return nil
}
