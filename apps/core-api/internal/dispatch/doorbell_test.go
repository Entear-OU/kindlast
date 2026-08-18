package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
)

// The decision half of §8's approve link (ENT-249).
//
// The boundary is in the schema: 00027 refuses a finding-bound delegation for
// an address nobody proved they control, for every writer including the schema
// owner that the mint runs as. What is tested here is the decision that keeps
// the dispatcher from meeting that boundary as an exception, because an
// exception inside the delivery transaction would abort the notification and
// retry it forever, and the visible symptom would be one person silently
// receiving no compliance mail at all.
//
// PROVEN ABLE TO FAIL. Removing the `if !r.EmailVerified` guard from
// approveLink turns "mints nothing for an unverified address" red on its own,
// with the rest green.

// mintRecorder is the Doorbells surface, with only the two calls these tests
// reach implemented. Everything else panics rather than returning a zero
// value, so a test that starts exercising more of the dispatcher fails loudly
// instead of asserting against a fake that quietly agreed with it.
type mintRecorder struct {
	minted  []string
	decline bool
	fail    error
}

func (m *mintRecorder) MintApprovalDelegation(
	_ context.Context, _ pgx.Tx, _, userID, _ string, lifetime time.Duration,
) (bool, error) {
	if m.fail != nil {
		return false, m.fail
	}
	if m.decline {
		return false, nil
	}
	if lifetime > time.Hour {
		// The database would refuse it, and a test that let it through would be
		// asserting about a delegation nobody can mint.
		return false, errors.New("dispatch: asked for a delegation longer than the ceiling")
	}
	m.minted = append(m.minted, userID)
	return true, nil
}

func (m *mintRecorder) Begin(context.Context) (pgx.Tx, error) { panic("not used") }
func (m *mintRecorder) ClaimDoorbell(context.Context, pgx.Tx) (postgres.Doorbell, error) {
	panic("not used")
}
func (m *mintRecorder) Recipients(context.Context, pgx.Tx, string) ([]postgres.Recipient, error) {
	panic("not used")
}
func (m *mintRecorder) MintCapabilityToken(
	context.Context, pgx.Tx, string, string, string, string, string,
) error {
	panic("not used")
}
func (m *mintRecorder) MarkDoorbellSent(context.Context, pgx.Tx, string) error { panic("not used") }
func (m *mintRecorder) MarkDoorbellSkipped(context.Context, pgx.Tx, string, string) error {
	panic("not used")
}
func (m *mintRecorder) MarkDoorbellFailed(context.Context, pgx.Tx, string, error) error {
	panic("not used")
}

func dispatcherFor(store Doorbells) *DoorbellDispatcher {
	return &DoorbellDispatcher{store: store, baseURL: "http://localhost:3000"}
}

var doorbell = postgres.Doorbell{
	ID:        "b0a1c2d3-0000-0000-0000-000000000001",
	OrgID:     "b0a1c2d3-0000-0000-0000-000000000002",
	FindingID: "b0a1c2d3-0000-0000-0000-000000000003",
}

func TestAVerifiedAddressGetsALinkNamingTheFinding(t *testing.T) {
	t.Parallel()

	store := &mintRecorder{}
	link, err := dispatcherFor(store).approveLink(t.Context(), nil, doorbell,
		postgres.Recipient{UserID: "someone", Email: "a@example.invalid", EmailVerified: true})
	if err != nil {
		t.Fatalf("minting an approve link failed: %v", err)
	}

	// The finding is in the URL as well as in the credential, because both are
	// needed to redeem it. A token recovered on its own, from a mail relay's
	// logs or a truncated URL, must approve nothing.
	if !strings.HasPrefix(link, "http://localhost:3000/approve/"+doorbell.FindingID+"/") {
		t.Fatalf("the link does not name the finding: %q", link)
	}
	if strings.HasSuffix(link, "/") {
		t.Fatalf("the link carries no credential: %q", link)
	}
	if len(store.minted) != 1 {
		t.Fatalf("minted %d delegations, want exactly one for this recipient", len(store.minted))
	}
}

func TestAnUnverifiedAddressGetsTheDoorbellAndNoAuthority(t *testing.T) {
	t.Parallel()

	// §1.8's gate. Miko is a genuine member who is genuinely reachable; what is
	// missing is anybody having proved the address is theirs, and an approve
	// link is authority to make a regulatory decision.
	store := &mintRecorder{}
	link, err := dispatcherFor(store).approveLink(t.Context(), nil, doorbell,
		postgres.Recipient{UserID: "miko", Email: "miko@example.invalid", EmailVerified: false})
	if err != nil {
		t.Fatalf("an unverified recipient failed the delivery: %v", err)
	}
	if link != "" {
		t.Fatalf("an unverified address was sent an approve link: %q", link)
	}
	if len(store.minted) != 0 {
		t.Fatal("a delegation was minted for an address nobody proved they control")
	}
}

func TestADeclinedMintStillDeliversTheNotification(t *testing.T) {
	t.Parallel()

	// The outbox row went away, or the person is no longer a member: ordinary
	// races against a row claimed moments ago. The doorbell still has to ring,
	// because a compliance finding nobody hears about is the failure this
	// product exists to prevent.
	store := &mintRecorder{decline: true}
	link, err := dispatcherFor(store).approveLink(t.Context(), nil, doorbell,
		postgres.Recipient{UserID: "ada", Email: "ada@example.invalid", EmailVerified: true})
	if err != nil {
		t.Fatalf("a declined mint failed the delivery: %v", err)
	}
	if link != "" {
		t.Fatalf("a declined mint produced a link anyway: %q", link)
	}
}

func TestAFailedMintFailsTheDeliveryRatherThanSendingALinkThatCannotWork(t *testing.T) {
	t.Parallel()

	// A database error is not a decline. Sending the message anyway would put a
	// link in somebody's mailbox with no row behind it, and they would find out
	// by clicking it and being told it has expired.
	store := &mintRecorder{fail: errors.New("connection reset")}
	if _, err := dispatcherFor(store).approveLink(t.Context(), nil, doorbell,
		postgres.Recipient{UserID: "ada", Email: "ada@example.invalid", EmailVerified: true},
	); err == nil {
		t.Fatal("a failed mint was treated as no link rather than as a failure")
	}
}
