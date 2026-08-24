package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// derivedUserID is what a subject claim becomes in `memberships`, which is not
// the claim itself: the derivation is one-way on purpose, so a fixture that
// seeded the claim as a user id would seed a membership nothing resolves to.
func derivedUserID(t *testing.T, subjectClaim string) uuid.UUID {
	t.Helper()
	userID, err := subject.UUID(testIssuer, subjectClaim)
	if err != nil {
		t.Fatalf("deriving the user id: %v", err)
	}
	return userID
}

// seedChannelOrg makes an organisation with one owner and returns the id and
// the owner's subject claim.
//
// Its own fixture rather than the seeded Alpha, because these tests write to a
// table keyed `(org_id, user_id)` and one of them has to commit to prove the
// unique index. Sharing a fixture organisation with the rest of the package
// would make that commit visible to a neighbouring test, which is the shape of
// failure `-p 1` exists to prevent and which no amount of `-p 1` would fix
// inside one package.
func seedChannelOrg(t *testing.T) (string, string) {
	t.Helper()

	conn := migratorConn(t)
	org := uuid.NewString()
	owner := "channels-owner-" + org[:8]

	if _, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "channels-"+org[:8], "Channel linking test"); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	if _, err := conn.Exec(t.Context(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org, derivedUserID(t, owner)); err != nil {
		t.Fatalf("seeding an owner: %v", err)
	}

	t.Cleanup(func() {
		// The cascade takes the channel rows with it, which is the erasure
		// path exercised in passing: nothing else in the deployment removes a
		// row from `notification_channels` except the person who owns it.
		//nolint:errcheck // best effort cleanup on a test fixture
		conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, org)
	})
	return org, owner
}

// addMember seeds a second person into an organisation, as the migrator,
// because a membership is not something a tenant transaction may invent.
func addMember(t *testing.T, org string) string {
	t.Helper()

	claim := "channels-member-" + uuid.NewString()[:8]
	if _, err := migratorConn(t).Exec(t.Context(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'member')`,
		org, derivedUserID(t, claim)); err != nil {
		t.Fatalf("seeding a colleague: %v", err)
	}
	return claim
}

// Linking a Telegram chat, against the real database (ENT-263, migration
// 00044), because every property here is the row's: that the pending code and
// the verified state are the two halves of one state machine the check
// constraint will not let disagree, that the attempt budget survives the
// request that spent it, and that relinking replaces rather than accumulating.
//
// A fake store would be testing the fake. The constraint is the thing doing the
// work in three of these, and it is not in Go.
//
// Every test runs inside a tenant transaction that is rolled back, so nothing
// here needs cleaning up and nothing it writes is visible to a sibling package.

// linkedFor is the caller's telegram row, read back through the store.
func linkedFor(t *testing.T, tenant *Tenant) (LinkedChannel, bool) {
	t.Helper()
	linked, err := tenant.LinkedChannels(t.Context())
	if err != nil {
		t.Fatalf("listing linked channels: %v", err)
	}
	for _, c := range linked {
		if c.Kind == notify.ChannelTelegram {
			return c, true
		}
	}
	return LinkedChannel{}, false
}

// link claims a chat with a known code and returns the code.
func link(t *testing.T, tenant *Tenant, chatID string) string {
	t.Helper()
	const code = "424242"
	if err := tenant.LinkTelegramChat(t.Context(), chatID,
		notify.HashVerificationCode(code), time.Now().Add(notify.VerificationCodeLifetime)); err != nil {
		t.Fatalf("linking %s: %v", chatID, err)
	}
	return code
}

func TestAClaimedChatIsNotVerifiedUntilTheCodeComesBack(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)
	tenant, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	code := link(t, tenant, "987654321")

	claimed, ok := linkedFor(t, tenant)
	if !ok {
		t.Fatal("no channel was recorded")
	}
	if claimed.Verified {
		t.Fatal("a chat was verified by the act of claiming it. The code is the whole " +
			"of the proof: without it the chat id is a string one person asserted " +
			"about somebody else's messenger account.")
	}
	if claimed.PendingUntil.IsZero() {
		t.Error("no expiry was recorded, so the code never expires")
	}

	if err := tenant.VerifyTelegramChat(t.Context(), code); err != nil {
		t.Fatalf("verifying with the right code: %v", err)
	}

	verified, _ := linkedFor(t, tenant)
	if !verified.Verified {
		t.Fatal("the right code did not verify the chat")
	}
	// A verified channel holds no code, which the check constraint enforces
	// and which is what stops a kept code re-verifying a chat later.
	if !verified.PendingUntil.IsZero() {
		t.Error("a verified channel still has a pending code outstanding")
	}
}

func TestASpentCodeCannotBeUsedTwice(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)
	tenant, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	code := link(t, tenant, "987654321")
	if err := tenant.VerifyTelegramChat(t.Context(), code); err != nil {
		t.Fatalf("verifying: %v", err)
	}

	if err := tenant.VerifyTelegramChat(t.Context(), code); !errors.Is(err, ErrNoPendingChannel) {
		t.Fatalf("re-verifying with the same code = %v, want ErrNoPendingChannel", err)
	}
}

// The attempt budget is what stops six digits being guessable, and it has to
// survive the request that incremented it, which means the column and not a
// counter in memory.
func TestTheAttemptBudgetIsSpentAndThenTheCodeIsDestroyed(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)
	tenant, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	code := link(t, tenant, "987654321")

	for i := range notify.MaxVerificationAttempts {
		if err := tenant.VerifyTelegramChat(t.Context(), "000000"); !errors.Is(err, ErrNoPendingChannel) {
			t.Fatalf("wrong guess %d = %v, want ErrNoPendingChannel", i+1, err)
		}
	}

	// The budget is gone, and the right code no longer works: the guessing is
	// over for this code rather than for this attempt.
	if err := tenant.VerifyTelegramChat(t.Context(), code); !errors.Is(err, ErrTooManyVerificationAttempts) {
		t.Fatalf("after the budget = %v, want ErrTooManyVerificationAttempts", err)
	}
	if err := tenant.VerifyTelegramChat(t.Context(), code); !errors.Is(err, ErrNoPendingChannel) {
		t.Fatalf("the right code still works after the budget was spent: %v", err)
	}

	// The claim survives so the person can ask for a new code without
	// retyping the chat id, and it is emphatically not verified.
	claimed, ok := linkedFor(t, tenant)
	if !ok {
		t.Fatal("the claim was removed along with the code")
	}
	if claimed.Verified {
		t.Fatal("guessing verified the chat")
	}
}

func TestAnExpiredCodeIsRefusedAndDestroyed(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)
	tenant, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	const code = "424242"
	if err := tenant.LinkTelegramChat(t.Context(), "987654321",
		notify.HashVerificationCode(code), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("linking with an already expired code: %v", err)
	}

	if err := tenant.VerifyTelegramChat(t.Context(), code); !errors.Is(err, ErrNoPendingChannel) {
		t.Fatalf("an expired code = %v, want ErrNoPendingChannel", err)
	}
	claimed, _ := linkedFor(t, tenant)
	if !claimed.PendingUntil.IsZero() {
		t.Error("the expired code was left on the row rather than destroyed")
	}
}

// Relinking replaces, and un-verifies. Somebody relinking because they lost
// access to the old chat must stop receiving compliance notifications there at
// the moment they say so, not when they finish proving the new one.
func TestRelinkingReplacesTheChatAndUnverifiesIt(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)
	tenant, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	code := link(t, tenant, "111111111")
	if err := tenant.VerifyTelegramChat(t.Context(), code); err != nil {
		t.Fatalf("verifying the first chat: %v", err)
	}

	link(t, tenant, "222222222")

	linked, err := tenant.LinkedChannels(t.Context())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("%d channels, want one: relinking replaces rather than accumulating", len(linked))
	}
	if linked[0].ChatID != "222222222" {
		t.Errorf("ChatID = %q, want the new chat", linked[0].ChatID)
	}
	if linked[0].Verified {
		t.Fatal("the new chat inherited the old one's verification, so a chat nobody " +
			"proved they hold would receive compliance notifications")
	}
}

// Unlinking is a delete, which is the acceptance criterion: future messages go
// to the remaining channel or nowhere and never to the unlinked chat. A soft
// delete would leave a row every future query had to remember to filter.
func TestUnlinkingRemovesTheRow(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)
	tenant, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	code := link(t, tenant, "987654321")
	if err := tenant.VerifyTelegramChat(t.Context(), code); err != nil {
		t.Fatalf("verifying: %v", err)
	}

	removed, err := tenant.UnlinkTelegramChat(t.Context())
	if err != nil {
		t.Fatalf("unlinking: %v", err)
	}
	if !removed {
		t.Error("unlinking reported nothing to remove")
	}
	if _, ok := linkedFor(t, tenant); ok {
		t.Fatal("the row survived the unlink")
	}

	// Twice is the same outcome as once. A console refreshed in another tab
	// should not be told it did something wrong.
	again, err := tenant.UnlinkTelegramChat(t.Context())
	if err != nil {
		t.Fatalf("unlinking twice: %v", err)
	}
	if again {
		t.Error("unlinking twice reported a second removal")
	}
}

// One person per chat within an organisation, enforced by the unique index
// rather than by a pre-read, because a pre-read is a race: two people
// submitting the same id at the same moment both find it free.
func TestTwoMembersCannotBothClaimOneChat(t *testing.T) {
	store := testStore(t)
	org, owner := seedChannelOrg(t)

	// The first claim is committed rather than rolled back, because a unique
	// index is only proved against a row that is actually there. The fixture's
	// cleanup drops the organisation, and the row cascades with it.
	first, err := store.BeginTenant(t.Context(), owner, org)
	if err != nil {
		t.Fatalf("the owner's transaction: %v", err)
	}
	defer first.Rollback(t.Context())
	link(t, first, "987654321")
	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing the first claim: %v", err)
	}

	colleague := addMember(t, org)
	other, err := store.BeginTenant(t.Context(), colleague, org)
	if err != nil {
		t.Fatalf("the colleague's transaction: %v", err)
	}
	defer other.Rollback(t.Context())

	err = other.LinkTelegramChat(t.Context(), "987654321",
		notify.HashVerificationCode("999999"), time.Now().Add(notify.VerificationCodeLifetime))
	if !errors.Is(err, ErrChatAlreadyLinked) {
		t.Fatalf("claiming a colleague's chat = %v, want ErrChatAlreadyLinked. Without "+
			"this the second to verify would start receiving findings addressed "+
			"to the first.", err)
	}
}
