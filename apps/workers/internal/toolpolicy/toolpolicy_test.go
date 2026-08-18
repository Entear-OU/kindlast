package toolpolicy_test

import (
	"errors"
	"testing"

	"github.com/Entear-OU/kindlast/apps/workers/internal/toolpolicy"
)

// A connection's write-capable tools are unreachable unless explicitly granted
// per connection (ENT-231 acceptance criterion).
//
// The criterion has three failure modes and each gets its own case below,
// because a single "the happy path works" test would pass with any two of the
// three checks deleted.

func TestAWriteCapableToolIsRefusedWithoutAnExplicitWriteGrant(t *testing.T) {
	// The tool is granted. It is not write-granted. This is the case that
	// matters: everything about the connection says "yes, use this tool", and
	// the only thing missing is the tick that says a human knew it writes.
	policy := toolpolicy.Policy{
		Granted:      []string{"search_tickets", "close_ticket"},
		WriteGranted: []string{},
	}

	err := toolpolicy.Decide(policy, "close_ticket", true, false)
	if !errors.Is(err, toolpolicy.ErrRefused) {
		t.Fatalf("got %v, want a write-capable tool with no write grant refused", err)
	}
}

// The guard is only worth having if it can fail. Adding the tool to the
// write-granted list must make the same call permitted.
//
// Without this, a Decide that refused everything would pass the test above
// while making the whole gateway useless and proving nothing about grants.
func TestTheWriteRefusalCanActuallyFail(t *testing.T) {
	policy := toolpolicy.Policy{
		Granted:      []string{"search_tickets", "close_ticket"},
		WriteGranted: []string{"close_ticket"},
	}

	if err := toolpolicy.Decide(policy, "close_ticket", true, false); err != nil {
		t.Fatalf("got %v, want an explicitly write-granted tool permitted", err)
	}
}

// A tool that was never granted is refused whatever else is true of it,
// including being in the write-granted list, which is what a caller sending a
// half-built policy would look like.
func TestAnUngrantedToolIsRefused(t *testing.T) {
	policy := toolpolicy.Policy{
		Granted:      []string{"search_tickets"},
		WriteGranted: []string{"delete_everything"},
	}

	for _, tool := range []string{"delete_everything", "list_users", ""} {
		if err := toolpolicy.Decide(policy, tool, false, false); !errors.Is(err, toolpolicy.ErrRefused) {
			t.Errorf("%q: got %v, want a refusal", tool, err)
		}
	}
}

// A read-only tool that both parties agree is read-only goes through, which is
// the ordinary case and the one everything else is measured against.
func TestAGrantedReadOnlyToolIsPermitted(t *testing.T) {
	policy := toolpolicy.Policy{Granted: []string{"search_tickets"}}

	if err := toolpolicy.Decide(policy, "search_tickets", false, false); err != nil {
		t.Fatalf("got %v, want a granted read-only tool permitted", err)
	}
}

// EITHER side saying the tool writes makes it a write.
//
// The case worth naming is the second one: the caller's stored flag says
// read-only, because that is what discovery recorded months ago, and the
// endpoint now annotates it as writing. Taking the union means a tool that has
// started writing is refused until a human looks at it again, rather than
// sailing through on a stale record.
func TestEitherPartyCallingItAWriteMakesItAWrite(t *testing.T) {
	policy := toolpolicy.Policy{
		Granted:      []string{"sync_contacts"},
		WriteGranted: []string{},
	}

	if err := toolpolicy.Decide(policy, "sync_contacts", true, false); !errors.Is(err, toolpolicy.ErrRefused) {
		t.Errorf("the caller alone calling it a write: got %v, want a refusal", err)
	}
	if err := toolpolicy.Decide(policy, "sync_contacts", false, true); !errors.Is(err, toolpolicy.ErrRefused) {
		t.Errorf("the endpoint alone calling it a write: got %v, want a refusal", err)
	}
}

// An unannotated tool reads as writing.
//
// The tempting default is read-only, because most tools are reads and it makes
// a friendlier consent screen. It is also the default that lets a
// `delete_everything` tool from a server that annotates nothing through the
// gate marked safe.
func TestAToolWithNoAnnotationsReadsAsWriting(t *testing.T) {
	if toolpolicy.ReadsOnly(nil) {
		t.Error("a tool with no annotations at all read as read-only")
	}
	if toolpolicy.ReadsOnly(map[string]any{"title": "Search tickets"}) {
		t.Error("a tool annotated with something else read as read-only")
	}
	if toolpolicy.ReadsOnly(map[string]any{"readOnlyHint": "true"}) {
		t.Error("a readOnlyHint that is a string rather than a boolean read as read-only")
	}
	if toolpolicy.ReadsOnly(map[string]any{"readOnlyHint": false}) {
		t.Error("readOnlyHint: false read as read-only")
	}
	if !toolpolicy.ReadsOnly(map[string]any{"readOnlyHint": true}) {
		t.Error("readOnlyHint: true did not read as read-only")
	}
}
