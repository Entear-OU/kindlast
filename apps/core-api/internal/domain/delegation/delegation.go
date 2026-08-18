// Package delegation holds what it means for an agent to act for a person
// (ENT-230, §26.3).
//
// A tiny package, and it exists for a boring reason: the type that says "this
// request is really Ada, with the Analyst holding the pen" has to be nameable
// by the interceptor chain, by the Postgres store and by the handler that
// records an agent run. Putting it in any of those three would make the other
// two depend on something they have no business importing (§21.6).
//
// The rules here are decisions rather than invariants, which is why they are in
// Go: how long a delegation may live, what an agent may call itself, and what a
// caller is told when a delegation will not resolve. The invariants that must
// hold no matter who writes are in 00021, and they are not duplicates of these:
// the database's TTL ceiling binds the migrator, and the bound below binds the
// application, which is the only caller that should ever be minting.
package delegation

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Grant is a delegation that resolved: who it names, where, and what is acting.
//
// Note what it does NOT carry: the credential. A Grant is the answer to
// presenting one, and passing the token itself any further than the store would
// mean a live credential travelling through a request context for no purpose.
type Grant struct {
	// UserID is this system's own identifier for the person, the same value
	// `memberships`, `created_by` and `approved_by` store.
	UserID string
	// OrgID is the one organisation this delegation is good for. Single-org is
	// not a convenience: a consultant belongs to several, and a delegation that
	// followed an active-organisation header would let an agent be moved
	// between a customer's tenants by whoever set the header.
	OrgID string
	// ActingAgent names what is holding the pen, for the audit row. A slug, and
	// see the pattern below for why it is not free text.
	ActingAgent string
}

// ErrUnusable is the single answer for every delegation that will not resolve.
//
// Expired, revoked, already redeemed, malformed and never existed are one error
// on purpose. A caller presenting a delegation has proved nothing that entitles
// them to the difference, and four distinguishable answers make this an oracle
// for which credentials are real. The same decision `accept_invitation` and
// `redeem_capability_token` made in the schema.
var ErrUnusable = errors.New("delegation: no usable delegation")

// MaxTTL is the longest a delegation may be asked to live.
//
// It matches 00021's check constraint rather than being independently chosen,
// because two different ceilings would mean the application refusing at fifty
// nine minutes and the database refusing at sixty one, with nothing to say
// which was the rule. The database's is the one that binds; this one exists so
// a caller gets a readable error instead of a constraint violation.
const MaxTTL = time.Hour

// DefaultTTL is what a run gets when it does not say.
//
// Minutes rather than the ceiling, because §26.3 asks for a delegation that
// "expires with the run" and a run is minutes. The ceiling is the outer bound
// beyond which short-lived stops being true; this is the ordinary case.
const DefaultTTL = 15 * time.Minute

// agentSlug is what an acting agent may be called.
//
// Constrained because the value lands in an audit row a customer reads. Free
// text there would put whatever a caller sent in front of a person as though
// this system vouched for it, and the audit surface is precisely where that
// must not happen.
var agentSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// Mint is what a caller asks for when it wants a delegation.
type Mint struct {
	// ActingAgent is the skill or channel that will hold it: `analyst`, `rail`,
	// `email`.
	ActingAgent string
	// TTL is how long it should live. Zero means DefaultTTL.
	TTL time.Duration
	// SingleUse spends the delegation on its first redemption.
	//
	// False for a run, whose tools are many calls under one delegation. True
	// for the approve link, where redeeming twice is the thing to prevent.
	SingleUse bool
	// FindingID binds the delegation to one finding (ENT-249, §8).
	//
	// Empty for a run delegation, which is not about any one thing. Set for the
	// approve link, and then the credential answers only to a caller that names
	// the same finding, so a token recovered from a mail relay's logs without
	// the message body approves nothing.
	FindingID string
}

// Validate refuses a request rather than clamping it.
//
// Clamping a TTL of two hours down to one would hand the caller a delegation
// that is not the one they asked for, with no way to notice. The caller here is
// code we ship, so a wrong number is a bug to fix rather than a value to
// tolerate.
func (m Mint) Validate() error {
	if !agentSlug.MatchString(m.ActingAgent) {
		return fmt.Errorf(
			"delegation: %q is not an agent name: lower case, digits and hyphens, 2 to 64 characters",
			m.ActingAgent)
	}
	if m.TTL < 0 {
		return errors.New("delegation: a negative lifetime is not a lifetime")
	}
	if m.TTL > MaxTTL {
		return fmt.Errorf("delegation: %s is longer than the %s ceiling", m.TTL, MaxTTL)
	}
	// Refused rather than corrected, for the same reason the TTL is. A caller
	// asking for a reusable approve link has misunderstood what it is, and
	// quietly setting the flag would ship that misunderstanding into a mailbox.
	// 00027 refuses the row as well, and that is the boundary; this is the
	// readable message at the point somebody made the mistake.
	if m.FindingID != "" && !m.SingleUse {
		return errors.New(
			"delegation: a delegation bound to a finding must be single use")
	}
	return nil
}

// Approval is the mint for §8's one-tap approve link.
//
// A constructor rather than three fields a caller sets by hand, because the
// three have to agree: the channel is what the audit row will name, the binding
// is what makes the credential worth less than a session, and single use is
// what stops the same message approving twice. A caller that assembled them
// itself could get two right.
//
// The lifetime is the ceiling on purpose, and it is the one place this design
// is uncomfortable. §8 wants a link somebody can act on when they read their
// mail, and people read compliance mail late; 00021's ceiling says no
// delegation is ever long-lived, and that is a claim this schema makes to a
// customer rather than to its own application. The ceiling wins, and the cost
// is paid by the interstitial: a link that has expired says so and points at
// the finding in the console, which is one more click rather than a dead end.
func Approval(findingID string) Mint {
	return Mint{
		ActingAgent: EmailChannel,
		TTL:         MaxTTL,
		SingleUse:   true,
		FindingID:   findingID,
	}
}

// EmailChannel is what an approval from a link calls itself in the audit row.
//
// §26.3 asks the trail to say what was holding the pen. For an agent that is a
// skill; for this path it is the medium the decision arrived through, which is
// the fact a person reading the trail actually needs in order to ask whether a
// link in a mailbox should have been able to do this.
const EmailChannel = "email"

// Lifetime is the TTL to apply, with the default filled in.
func (m Mint) Lifetime() time.Duration {
	if m.TTL == 0 {
		return DefaultTTL
	}
	return m.TTL
}
