package onboarding

import (
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// Session is one organisation's interview.
type Session struct {
	ID          string
	Status      string
	StartedAt   time.Time
	CompletedAt *time.Time
}

// Statuses `onboarding_sessions.status` accepts. The database's check
// constraint is the authority; these exist so a bad value is refused with a
// sentence rather than with a constraint name.
const (
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusAbandoned  = "abandoned"
)

// Roles `onboarding_messages.role` accepts.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// SkippedContent is what a declined question leaves in the transcript.
//
// Neutral on purpose. The transcript is read back to the person who wrote it,
// and putting a sentence like "I would rather not say" in their mouth would be
// this product inventing a quote. "Skipped." is visibly a marker rather than
// something anybody said.
const SkippedContent = "Skipped."

// Turn is one line of the interview, as stored.
type Turn struct {
	ID      string
	Role    string
	Content string

	// Which fact this turn is about. Empty for the greeting and for anything
	// outside the question sequence.
	Key string

	// What the answer was taken to mean. Empty on a question and on a skip.
	ValueJSON string

	Ordering  int32
	CreatedAt time.Time

	// Which human said it. Onboarding is a person talking, and where two
	// members of one organisation both take part, each turn records who.
	CreatedBy string
}

// Skipped reports whether this turn is a declined question.
//
// Derived rather than stored, because the database already distinguishes the
// three states structurally: a question has a key and no value, an answer has
// both, and a skip is a person's turn with a key and no value. A fourth column
// would be a second way to say the same thing and a chance for the two to
// disagree.
func (t Turn) Skipped() bool {
	return t.Role == RoleUser && t.Key != "" && t.ValueJSON == ""
}

// AnswersFrom reduces a transcript to the latest answer for each question.
//
// LATEST WINS, AND THAT IS WHAT LETS SOMEBODY CHANGE THEIR MIND. A person who
// realises at question nine that they misread question three answers it again,
// and the interview has to take the second answer without losing the first from
// the transcript. Reducing on read rather than editing a row is what keeps both
// true at once.
//
// The transcript must be in `ordering` order, which is how the store reads it.
func AnswersFrom(transcript []Turn) Answers {
	answers := make(Answers, len(Script()))
	for _, turn := range transcript {
		if turn.Role != RoleUser || turn.Key == "" {
			continue
		}
		answers[turn.Key] = Answer{
			Text:      turn.Content,
			ValueJSON: turn.ValueJSON,
			Skipped:   turn.Skipped(),
		}
	}
	return answers
}

// FactsFrom is what would be recorded: every answer that is not a skip.
//
// A skipped question produces no fact at all, rather than a fact holding a
// placeholder. That is ENT-212's "an unanswerable question leaves a field empty
// rather than guessed", and it is why this returns a map with the key missing
// instead of one with an empty value in it.
func FactsFrom(answers Answers) map[string]string {
	facts := make(map[string]string, len(answers))
	for key, answer := range answers {
		if answer.Skipped || answer.ValueJSON == "" {
			continue
		}
		if _, known := memory.Kinds[key]; !known {
			continue
		}
		facts[key] = answer.ValueJSON
	}
	return facts
}

// OrderedFacts returns the facts to write, in script order.
//
// Order matters for one narrow reason: each fact opens its own interval at its
// own `clock_timestamp()`, so writing them in a map's iteration order would
// give a customer a history whose entries are ordered differently on every
// confirmation. Script order makes the history read the way the conversation
// went.
func OrderedFacts(facts map[string]string) []struct {
	Key       string
	ValueJSON string
} {
	out := make([]struct {
		Key       string
		ValueJSON string
	}, 0, len(facts))
	for _, question := range Script() {
		if value, ok := facts[question.Key]; ok {
			out = append(out, struct {
				Key       string
				ValueJSON string
			}{question.Key, value})
		}
	}
	return out
}
