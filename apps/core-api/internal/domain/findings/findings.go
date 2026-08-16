package findings

import "time"

// Finding is one row of the feed, as the store reads it.
//
// Every field is stored. Nothing here is computed at read time, which is the
// point: the severity is the one the Analyst assessed and the citation is the
// one it recorded, not values re-derived against today's rules.
type Finding struct {
	ID              string
	Status          string
	Severity        string
	Detected        string
	ProposedAction  string
	EffortEstimate  string
	ActionType      string
	Citation        Citation
	CreatedAt       time.Time
	SnoozedUntil    *time.Time
	ApprovedBy      string
	RejectionReason string
}

// Citation is the regulatory basis for a finding, exactly as stored.
//
// Label and URL were assembled by analyst_citation_label and
// analyst_citation_url when the finding was created. They are carried, never
// rebuilt: a second assembler is a second thing that can disagree with the
// record, and the product's whole claim is that a human can check the citation
// against the law.
type Citation struct {
	ObligationSlug string
	Title          string
	CELEX          string
	Kind           string
	Article        int32
	Recital        int32
	Annex          string
	Paragraph      string
	Label          string
	URL            string
}

// SupportingChunk is one quoted passage behind a finding, from
// finding_supporting_chunks.
type SupportingChunk struct {
	Ordinal    int32
	Label      string
	QuotedText string
	SourceURL  string
}

// Page is one page of the feed plus the cursor for the next.
//
// NextCursor is empty when this is the last page. A caller must key "there is
// more" off that rather than off a full page, because a last page that happens
// to be full is ordinary.
type Page struct {
	Findings   []Finding
	NextCursor string
}

// Acted is the outcome of an act-path call.
//
// Applied is false when the finding was already in the target state, is
// unknown, or belongs to another organisation. Those are one answer on purpose:
// distinguishing them is how a caller probes for a tenancy leak.
type Acted struct {
	Applied bool
	// The record the Executor created, when it created one. Empty when the
	// finding's action_type is `review`, which is every finding until the
	// corpus is classified (ENT-165).
	CreatedRecordID    string
	CreatedRecordTable string
	// Set by a snooze.
	SnoozedUntil *time.Time
}

// Pipeline is whether the agents have actually run.
//
// The honest answer to "is this thing working", which the console has never
// been able to give. ProfileExists separates "the agents have not run" from
// "there is nothing for them to run against": one is waiting for a schedule,
// the other is waiting for onboarding, and they need different words.
type Pipeline struct {
	WatcherLastRunAt *time.Time
	ProfileExists    bool
}

// Dashboard is everything the stat row and the agent rail render.
type Dashboard struct {
	Posture  Posture
	Headline string
	Counts   SeverityCounts
	Pipeline Pipeline
}
