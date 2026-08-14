// Package interceptor is the middleware chain every RPC passes through.
//
// The order is not a matter of taste, and it is the first thing to check if
// something here looks wrong (core-api-surface §0.2):
//
//	Auth  -> JTI -> Scope -> Tenancy -> handler
//	 who    still   may the   which rows
//	          ?     client
//	                touch
//	              this kind
//	              of thing
//
// Each stage may only assume the stages before it have run. Auth is first
// because nothing downstream means anything without a verified subject. JTI
// follows immediately, so a revoked token is refused before it can cost a
// database round trip. Scope precedes Tenancy for the same reason: comparing a
// declared scope against a claim is free, and opening a transaction is not.
//
// The three layers are also not the same question, and conflating them
// produces a system that looks secure and is not (§0.5). Scope asks whether
// this client may touch this kind of resource at all, and answers 403.
// Tenancy asks which rows exist, and answers with an empty result rather than
// an error, because it is enforced by Postgres policies rather than by this
// code. A token carrying `findings:act` does not mean the holder may act on
// any finding.
package interceptor

import (
	"context"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// contextKey keeps the values this package attaches to a request context
// unforgeable from outside it. An exported string key would let any package
// write a set of claims into a context and have a handler downstream trust
// them.
type contextKey int

const (
	claimsKey contextKey = iota
	tenantKey
	tokenKey
)

// WithClaims attaches a verified identity to the context.
//
// Exported for tests that need to exercise a later stage of the chain in
// isolation. Nothing in production calls it except the Auth interceptor.
func WithClaims(ctx context.Context, claims *oidc.Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFrom returns the verified identity for a request.
//
// The boolean is not decoration. A handler that ignores it and dereferences a
// nil pointer is a panic; a handler that treats "absent" as "anonymous but
// fine" is an authentication bypass. Absent means the Auth interceptor did not
// run, which is a wiring bug, so callers should treat it as internal failure
// rather than as a valid state.
func ClaimsFrom(ctx context.Context) (*oidc.Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*oidc.Claims)
	return claims, ok && claims != nil
}

// WithToken attaches the raw bearer credential the request arrived with.
//
// Carrying a live credential further into the process than it strictly has to
// go is a cost, so it is worth saying what buys it. Some things can only be
// asked of the authorization server *as the caller*: OIDC userinfo is the
// standard example, and it is the only conformant way to learn a display name
// when the access token carries none. Passing the caller's own token is what
// makes that request authorised by the person it concerns, rather than this
// service using a privileged credential to read about whoever it likes.
//
// The bounds that keep the cost small: the key is unexported, so no package
// outside this one can retrieve the value without going through TokenFrom; the
// value lives only as long as the request; and it is never logged. Treat it as
// a credential everywhere it is read.
func WithToken(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, tokenKey, raw)
}

// TokenFrom returns the raw bearer credential, for the narrow set of callers
// that must present it upstream on the caller's behalf.
//
// Never use this to make an authorization decision. The verified Claims are
// the only thing that has been checked; this is the uninterpreted string the
// client sent.
func TokenFrom(ctx context.Context) (string, bool) {
	raw, ok := ctx.Value(tokenKey).(string)
	return raw, ok && raw != ""
}

// WithTenant attaches the resolved organisation and its open transaction.
func WithTenant(ctx context.Context, tenant Tenant) context.Context {
	return context.WithValue(ctx, tenantKey, tenant)
}

// TenantFrom returns the transaction the Tenancy interceptor opened, with both
// tenancy GUCs already set on it.
//
// A handler that reaches for its own connection instead of this one is outside
// the RLS session settings and will read nothing, or worse, read across
// tenants if it ever runs as a role that bypasses policies. There is one
// correct connection per request and this is how to get it.
func TenantFrom(ctx context.Context) (Tenant, bool) {
	tenant, ok := ctx.Value(tenantKey).(Tenant)
	return tenant, ok && tenant != nil
}
