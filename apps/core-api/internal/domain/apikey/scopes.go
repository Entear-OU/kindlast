package apikey

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// GrantableScopes is what a partner's key may be minted with.
//
// # THIS IS A DECISION, WHICH IS WHY IT IS HERE AND NOT A CONSTRAINT
//
// The invariant lives in 00043: no key, ever, carries an `internal:*` scope,
// enforced by a CHECK that binds the migrator and a psql prompt as well as this
// process. This list is the narrower thing on top of it, the set a partner
// integration is expected to need, and it will move as the product grows. A
// list that will change next quarter is Go's (db/README.md).
//
// # WHY IT IS A LIST AND NOT "WHATEVER THE MINTER HOLDS"
//
// The minter is a person and holds HumanScopes, which is everything. Handing a
// key the whole human surface because a human minted it would make every key a
// session that never expires, and would mean the narrowest key anybody could
// create was as wide as the console. Starting from a list and letting the
// caller subset it is the other way round, and it is the one that has a floor.
//
// # THE THREE DELIBERATE ABSENCES
//
// `org:manage` is the important one. It covers minting API keys, so a key
// holding it could mint another key, and a credential that can extend its own
// lifetime and multiply itself with no human in the loop is not a credential a
// customer can reason about. Every other exclusion is a nice-to-have; this one
// is the reason the list exists.
//
// `billing:manage` is out because changing what a customer pays is not
// something a partner integration should be able to do quietly, and there is no
// integration story that needs it.
//
// `model:write` is out because choosing a sub-processor is the customer
// exercising control over where their data is processed (ENT-236). That is a
// decision a named person takes and acknowledges the consequence of, which is
// precisely what a long-lived machine credential cannot do.
//
// Every entry below is also in interceptor.HumanScopes, and
// TestEveryGrantableScopeIsAHumanScope asserts it: a key must never be able to
// reach a surface a signed-in person could not, because it acts under a
// person's membership and would be borrowing authority its lender does not
// have.
var GrantableScopes = []string{
	"audit:read",
	"billing:read",
	"corpus:read",
	"dashboard:read",
	"findings:act",
	"findings:read",
	"integrations:read",
	"integrations:write",
	"memory:read",
	"memory:write",
	"model:read",
	"notifications:read",
	"notifications:write",
	"onboarding:read",
	"org:read",
	"records:ai-systems:write",
	"records:dsar:write",
	"records:read",
	"records:ropa:write",
}

// MaxScopes bounds how many a single key may carry.
//
// Not a security property, since every entry is already from a fixed list. It
// stops a request asking for the same scope ten thousand times and turning a
// mint into a large array write, and it makes the error a caller gets a
// sentence rather than a constraint violation.
const MaxScopes = 32

// MaxNameLength matches 00043's CHECK rather than being independently chosen.
//
// Two different ceilings would mean the application refusing at ninety nine
// characters and the database refusing at a hundred and one, with nothing to
// say which was the rule. The database's is the one that binds; this exists so
// a person gets a readable message instead of a 23514.
const MaxNameLength = 100

// ErrNoScopes, ErrUnknownScope and ErrBadName are the three ways a mint is
// refused.
//
// Distinguishable, unlike ErrMalformed, and the difference is who is asking. A
// caller minting a key has already authenticated, already proved membership and
// already passed the scope interceptor, so telling them exactly which scope they
// asked for does not exist is help rather than an oracle.
var (
	ErrNoScopes = errors.New("apikey: a key with no scopes could do nothing")
	ErrBadName  = errors.New("apikey: a key needs a name of 1 to 100 characters")
)

// Mint is what a caller asks for when it wants a key.
type Mint struct {
	Name   string
	Scopes []string
}

// Validate refuses a request rather than trimming it down to what is allowed.
//
// Silently dropping a scope the caller asked for would hand back a key that is
// not the one they requested, and they would find out when an integration
// started returning 403 at three in the morning. Refusing names the problem at
// the moment somebody made it. Same reasoning as delegation.Mint.Validate.
//
// It returns a NORMALISED copy: trimmed name, and scopes deduplicated and
// sorted. Sorting is so two keys minted with the same set compare equal in a
// console and in an audit row; deduplication is so `["records:read",
// "records:read"]` does not become a row that reads oddly forever.
func (m Mint) Validate() (Mint, error) {
	name := strings.TrimSpace(m.Name)
	if name == "" || len(name) > MaxNameLength {
		return Mint{}, ErrBadName
	}

	if len(m.Scopes) == 0 {
		return Mint{}, ErrNoScopes
	}
	if len(m.Scopes) > MaxScopes {
		return Mint{}, fmt.Errorf("apikey: %d scopes is more than the %d a key may carry",
			len(m.Scopes), MaxScopes)
	}

	seen := make(map[string]struct{}, len(m.Scopes))
	scopes := make([]string, 0, len(m.Scopes))
	for _, scope := range m.Scopes {
		scope = strings.TrimSpace(scope)
		if !Grantable(scope) {
			// Named, because the caller is authenticated and this is a mistake
			// rather than a probe. The message says what the value was so it can
			// be found in a client's configuration.
			return Mint{}, fmt.Errorf("apikey: %q is not a scope an API key may carry", scope)
		}
		if _, duplicate := seen[scope]; duplicate {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	return Mint{Name: name, Scopes: scopes}, nil
}

// Grantable reports whether a scope may be minted onto a key.
//
// Exact match, never a prefix. `records:read` must not satisfy a request for
// `records:ropa:write`, for the same reason oidc.Claims.HasScope compares
// exactly.
func Grantable(scope string) bool {
	for _, allowed := range GrantableScopes {
		if allowed == scope {
			return true
		}
	}
	return false
}

// Principal is a key that authenticated: whose authority it borrows, where, and
// what it may do.
//
// The shape mirrors delegation.Grant deliberately, because the two answer the
// same question by different routes. Note what it does NOT carry: the
// credential. A Principal is the answer to presenting one, and carrying the
// credential further would mean a live secret travelling through a request
// context for no purpose.
type Principal struct {
	// ID names the row, and it is what lands on every audit entry this request
	// writes. The public handle would do as a label, but the id is what a
	// revocation targets, so a reader who finds a key in the audit log can act
	// on what they found.
	ID string
	// UserID is the person who minted the key. The request runs as them, and
	// their membership is re-checked on every call.
	UserID string
	// OrgID is the one organisation the key is good for. Single-org for the same
	// reason a delegation is: a consultant belongs to several, and a credential
	// that followed an active-organisation header would let whoever set the
	// header move a partner's key between a customer's tenants.
	OrgID string
	// Scopes is what this key may exercise, already a subset of GrantableScopes
	// because that was checked at mint.
	Scopes []string
}

// Holds reports whether the key carries a scope.
func (p Principal) Holds(scope string) bool {
	for _, held := range p.Scopes {
		if held == scope {
			return true
		}
	}
	return false
}
