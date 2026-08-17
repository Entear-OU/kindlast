package corpus

// The corpus as it is READ, rather than as a pack presents it for ingest
// (ENT-207).
//
// Separate types from the ones above `Pack`, and the difference is in the
// direction that matters: these carry `Label` and `URL`, which the database
// computes and a pack never supplies. One shared type would invite a caller to
// set a label on the way in, and a label is derived rather than given.

// StoredObligation is an obligation as the corpus holds it, with its citation
// already rendered for a reader.
type StoredObligation struct {
	Slug          string
	Title         string
	Summary       string
	Severity      string
	Recurrence    string
	DueWithinDays int
	EffectiveDate string
	TopicTags     []string
	ActionType    string

	Citation Citation

	// Rendered by `analyst_citation_label` and `analyst_citation_url`, the same
	// functions the Analyst uses when it writes a finding.
	//
	// Deliberately not reimplemented in Go. Two implementations of "what is
	// this citation called" diverge the first time a regulation needs a special
	// case, and a finding saying `GDPR Art. 30` while the obligation page says
	// `32016R0679 Art. 30` is the inconsistency that makes a customer stop
	// trusting both.
	Label string
	URL   string
}

// CitedText is the regulatory text an obligation's citation points at.
//
// A summary rather than the Official Journal wording, because the corpus stores
// no verbatim text: the citation's URL resolves to the publisher's copy, which
// is the one that stays canonical.
type CitedText struct {
	// The article, recital or annex summary. Empty when the citation resolves
	// to nothing, which ingest refuses to create.
	Summary string
	// Articles and annexes have one; recitals do not.
	Heading string
	// Set when the citation names a paragraph.
	ParagraphSummary string
}

// StoredDocument is a regulation in the corpus, with its size.
//
// The counts are the point rather than decoration. A compliance product that
// cannot say which version of the law it is working from, and how much of it it
// holds, is asking for trust it has not earned.
type StoredDocument struct {
	Celex        string
	Title        string
	ShortTitle   string
	VersionDate  string
	OfficialURL  string
	ArticleCount int
	RecitalCount int
	AnnexCount   int
}
