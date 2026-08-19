package memory_test

import (
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	memoryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/memory"
)

// The note survives the trip to the wire.
//
// This is a small test for a small field, and it is here because the field was
// missing for a reason worth guarding against rather than by an oversight in
// one line. `CorrectFactRequest.note` accepted the sentence, the column stored
// it, and `profileFactColumns` even selected it into `domain.Fact.Note`: every
// layer carried it except the last one, so the product asked a person why they
// were changing a compliance fact and then put the answer somewhere only a
// database client could reach.
//
// A write path with no read path fails silently by construction. Nothing errors
// and no test that exercises the write can notice, which is why the assertion
// that matters is this one, on the boundary the console actually reads.
func TestTheNoteOnAFactReachesTheWire(t *testing.T) {
	t.Parallel()

	fact := memory.Fact{
		Key:        "staff_count",
		ValueJSON:  `20`,
		Source:     "human",
		ValidFrom:  time.Now(),
		RecordedBy: "11111111-1111-4111-8111-111111111111",
		Note:       "We hired eight people since the interview",
	}

	out, err := memoryservice.ToProto(fact)
	if err != nil {
		t.Fatalf("rendering the fact: %v", err)
	}

	if got := out.GetNote(); got != fact.Note {
		t.Fatalf("note = %q, want %q", got, fact.Note)
	}
}

// Most facts carry none, and an absent note must stay absent rather than
// becoming a quoted empty string on the page that renders it.
func TestAFactWithoutANoteCarriesNone(t *testing.T) {
	t.Parallel()

	out, err := memoryservice.ToProto(memory.Fact{
		Key:       "staff_count",
		ValueJSON: `12`,
		Source:    "onboarding",
		ValidFrom: time.Now(),
	})
	if err != nil {
		t.Fatalf("rendering the fact: %v", err)
	}

	if got := out.GetNote(); got != "" {
		t.Fatalf("note = %q, want empty", got)
	}
}
