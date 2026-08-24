package interceptor

import (
	"context"
	"errors"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
)

// APIKeyScheme is the Authorization scheme a partner's key arrives under
// (ENT-262, §23).
//
// # WHY A SECOND SCHEME AND NOT A SECOND KIND OF BEARER
//
// §1.7 lists the credential schemes that must never share a verification path,
// and the reason is that a shared path has to GUESS which kind of credential it
// was handed. A guess is a place where a value that fails one verifier gets a
// second chance at another, and that is how a credential ends up validated by
// the wrong code.
//
// Putting the key under `Bearer` would force exactly that guess: is this string
// a JWT or a key? Answering it by trying the JWT verifier and falling back would
// mean every malformed token became a key lookup, and every unknown key became a
// signature check. Naming the scheme makes the dispatch a fact the caller stated
// rather than a shape this code inferred. RFC 7235 defines the scheme token for
// precisely this, and a non-standard scheme is well within what it allows.
//
// The consequence to keep: `bearerToken` and `apiKeyCredential` never fall back
// to one another. A request whose scheme is `ApiKey` is refused if the key is
// not good, and is never retried as a token.
const APIKeyScheme = "ApiKey"

// Authenticator turns a presented credential into the key it names.
//
// Declared here because this is where it is used (§21.6), and satisfied by the
// Postgres store without that package importing this one. Like TenantOpener and
// DelegationResolver, it must not be mocked in this package's tests: a stubbed
// authenticator would assert that the interceptor calls something, which is not
// a property worth having. The tests here mint real keys into a real table.
type Authenticator interface {
	// AuthenticateAPIKey resolves a credential, or returns apikey.ErrMalformed
	// for everything that will not resolve.
	//
	// It does NOT check membership. That is the tenancy stage's job one call
	// later, run against the same `memberships` policy every human request uses,
	// so a person removed from the organisation is refused on their key's next
	// request by the check the rest of the system already depends on.
	AuthenticateAPIKey(ctx context.Context, credential string) (apikey.Principal, error)
}

// ErrNoUsableAPIKey is the single answer for every key that will not resolve.
//
// Malformed, unknown, revoked and belonging-to-a-deleted-organisation are one
// error, for the same reason delegation.ErrUnusable is one: a caller presenting
// a credential has proved nothing that entitles them to the difference, and four
// distinguishable answers make this an oracle for which keys are real.
var ErrNoUsableAPIKey = errors.New("interceptor: no usable API key")

// WithAPIKey attaches an authenticated key to the request.
//
// Exported for tests that exercise a later stage in isolation, the same reason
// WithClaims and WithGrant are. Nothing in production calls it except Auth.
func WithAPIKey(ctx context.Context, principal apikey.Principal) context.Context {
	return context.WithValue(ctx, apiKeyKey, principal)
}

// APIKeyFrom returns the key this request authenticated with, if any.
//
// The boolean means "this request arrived on a partner's key rather than a
// person's session". Every stage after Auth asks it, because the answer changes
// what that stage should do: see the branch at the top of JTI, ActOnBehalf,
// Scope and Tenancy, each of which says why.
func APIKeyFrom(ctx context.Context) (apikey.Principal, bool) {
	principal, ok := ctx.Value(apiKeyKey).(apikey.Principal)
	return principal, ok && principal.ID != ""
}

// apiKeyCredential pulls the key out of the Authorization header.
//
// Deliberately a near copy of bearerToken rather than a shared helper with a
// scheme parameter. The duplication is four lines and it buys the property §1.7
// asks for: there is no single function that could be called with the wrong
// scheme, and no place where a change made for one credential quietly applies to
// the other.
func apiKeyCredential(header string) (string, bool) {
	scheme, credential, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, APIKeyScheme) {
		return "", false
	}
	credential = strings.TrimSpace(credential)
	return credential, credential != ""
}
