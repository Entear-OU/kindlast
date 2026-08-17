// Package corpus holds the domain rules for the regulatory corpus: what a pack
// has to look like before any of it reaches the database (ENT-207).
//
// Pure functions over already-loaded data, no database and no proto, for the
// same reason `domain/records` is written that way. Everything here is a rule
// about whether a citation can be trusted, and a rule that can only be
// exercised through a live ingest is a rule nobody tests exhaustively.
//
// # THE PACK IS VALIDATED WHOLE, BEFORE ANYTHING IS WRITTEN
//
// A pack that fails halfway would leave a corpus that is partly the new
// snapshot and partly the old, which is the one state nobody can reason about.
// So the checks that can be made without the database are made here first, and
// the ones that need it (does this article exist) are made inside the ingest
// transaction before it commits.
package corpus

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Bounds the database also enforces, restated here so a curator gets a message
// naming the offending row rather than a constraint name.
const (
	SummaryMax = 2000
	// Articles, recitals and paragraphs may be terse: some articles genuinely
	// are one sentence.
	ArticleSummaryMin = 1
	// Obligations, annexes and enforcement decisions may not. This is the text a
	// person reads to decide whether a finding applies to them, and a one-liner
	// pushes them to the regulation for every finding.
	LongSummaryMin = 100
)

// Citation kinds.
const (
	KindArticle = "article"
	KindRecital = "recital"
	KindAnnex   = "annex"
)

// Severities the database constrains.
var severities = map[string]bool{"low": true, "medium": true, "high": true}

// Citation is what an obligation points at.
type Citation struct {
	Kind           string
	Celex          string
	ArticleNumber  int
	RecitalNumber  int
	AnnexLabel     string
	ParagraphLabel string
}

// Target renders a citation as the thing a human would look up, for error
// messages and for logs. Not stored: the database keeps the parts.
func (c Citation) Target() string {
	switch c.Kind {
	case KindArticle:
		if c.ParagraphLabel != "" {
			return fmt.Sprintf("%s Article %d(%s)", c.Celex, c.ArticleNumber, c.ParagraphLabel)
		}
		return fmt.Sprintf("%s Article %d", c.Celex, c.ArticleNumber)
	case KindRecital:
		return fmt.Sprintf("%s Recital %d", c.Celex, c.RecitalNumber)
	case KindAnnex:
		return fmt.Sprintf("%s Annex %s", c.Celex, c.AnnexLabel)
	default:
		return fmt.Sprintf("%s (unknown citation kind %q)", c.Celex, c.Kind)
	}
}

type Paragraph struct {
	Label    string
	Summary  string
	Ordering int
}

type Article struct {
	Number        int
	Heading       string
	Summary       string
	EffectiveDate string
	Paragraphs    []Paragraph
}

type Recital struct {
	Number  int
	Summary string
}

type AnnexItem struct {
	Label         string
	Heading       string
	Summary       string
	EffectiveDate string
	Ordering      int
}

type Annex struct {
	Label         string
	Heading       string
	Summary       string
	EffectiveDate string
	Items         []AnnexItem
}

type ArticleRecitalLink struct {
	ArticleNumber int
	RecitalNumber int
}

type Document struct {
	Celex           string
	Title           string
	ShortTitle      string
	VersionDate     string
	OfficialURL     string
	Articles        []Article
	Recitals        []Recital
	Annexes         []Annex
	ArticleRecitals []ArticleRecitalLink
}

type Obligation struct {
	Slug            string
	Title           string
	Summary         string
	Citation        Citation
	AppliesWhenJSON string
	Severity        string
	DueWithinDays   int
	Recurrence      string
	EffectiveDate   string
	TopicTags       []string
	ActionType      string
}

type Guideline struct {
	Slug        string
	Publisher   string
	Title       string
	AdoptedDate string
	Version     string
	SourceURL   string
	TopicTags   []string
}

type EnforcementDecision struct {
	Slug         string
	DPA          string
	Title        string
	DecisionDate string
	FineEUR      int64
	HasFine      bool
	Summary      string
	SourceURL    string
	GDPRArticles []int
	TopicTags    []string
}

// Pack is one coherent unit of regulatory reference data.
type Pack struct {
	ID                   string
	Document             *Document
	Obligations          []Obligation
	Guidelines           []Guideline
	EnforcementDecisions []EnforcementDecision
}

// Validate checks everything that can be checked without the database.
//
// Returns every problem rather than the first, because a curator fixing a pack
// wants the list. An ingest that reported one dangling citation per run would
// turn a ten-error pack into ten round trips through a schedule.
func (p Pack) Validate() []error {
	var problems []error

	if strings.TrimSpace(p.ID) == "" {
		problems = append(problems, fmt.Errorf("the pack has no id"))
	}

	if p.Document == nil &&
		len(p.Obligations) == 0 &&
		len(p.Guidelines) == 0 &&
		len(p.EnforcementDecisions) == 0 {
		// An empty pack is almost certainly a caller that failed to load its
		// file. Refusing rather than reporting a successful ingest of nothing,
		// which on a schedule would look like everything working.
		problems = append(problems, fmt.Errorf("the pack is empty"))
	}

	if p.Document != nil {
		problems = append(problems, p.Document.validate()...)
	}
	problems = append(problems, validateObligations(p.Obligations)...)
	problems = append(problems, validateGuidelines(p.Guidelines)...)
	problems = append(problems, validateDecisions(p.EnforcementDecisions)...)

	return problems
}

func (d *Document) validate() []error {
	var problems []error

	if strings.TrimSpace(d.Celex) == "" {
		problems = append(problems, fmt.Errorf("the document has no CELEX number"))
	}
	if strings.TrimSpace(d.Title) == "" {
		problems = append(problems, fmt.Errorf("document %s has no title", d.Celex))
	}
	if strings.TrimSpace(d.ShortTitle) == "" {
		problems = append(problems, fmt.Errorf("document %s has no short title", d.Celex))
	}
	problems = append(problems, checkDate("document "+d.Celex, "version date", d.VersionDate, true)...)
	problems = append(problems, checkURL("document "+d.Celex, "official URL", d.OfficialURL)...)

	// Duplicate natural keys WITHIN the pack. The database would refuse the
	// second write of a pair, but the message would name a constraint rather
	// than the article, and by then the transaction is dead.
	articles := map[int]bool{}
	for _, a := range d.Articles {
		where := fmt.Sprintf("article %d", a.Number)
		if a.Number <= 0 {
			problems = append(problems, fmt.Errorf("%s: article numbers start at 1", where))
		}
		if articles[a.Number] {
			problems = append(problems, fmt.Errorf("%s appears twice in the pack", where))
		}
		articles[a.Number] = true

		if strings.TrimSpace(a.Heading) == "" {
			problems = append(problems, fmt.Errorf("%s has no heading", where))
		}
		problems = append(problems, checkSummary(where, a.Summary, ArticleSummaryMin)...)
		problems = append(problems, checkDate(where, "effective date", a.EffectiveDate, false)...)

		labels := map[string]bool{}
		for _, para := range a.Paragraphs {
			at := fmt.Sprintf("%s paragraph %q", where, para.Label)
			if strings.TrimSpace(para.Label) == "" {
				problems = append(problems, fmt.Errorf("%s: a paragraph has no label", where))
			}
			if labels[para.Label] {
				problems = append(problems, fmt.Errorf("%s appears twice in the pack", at))
			}
			labels[para.Label] = true
			problems = append(problems, checkSummary(at, para.Summary, ArticleSummaryMin)...)
		}
	}

	recitals := map[int]bool{}
	for _, r := range d.Recitals {
		where := fmt.Sprintf("recital %d", r.Number)
		if r.Number <= 0 {
			problems = append(problems, fmt.Errorf("%s: recital numbers start at 1", where))
		}
		if recitals[r.Number] {
			problems = append(problems, fmt.Errorf("%s appears twice in the pack", where))
		}
		recitals[r.Number] = true
		problems = append(problems, checkSummary(where, r.Summary, ArticleSummaryMin)...)
	}

	annexes := map[string]bool{}
	for _, x := range d.Annexes {
		where := fmt.Sprintf("annex %s", x.Label)
		if strings.TrimSpace(x.Label) == "" {
			problems = append(problems, fmt.Errorf("an annex has no label"))
		}
		if annexes[x.Label] {
			problems = append(problems, fmt.Errorf("%s appears twice in the pack", where))
		}
		annexes[x.Label] = true

		if strings.TrimSpace(x.Heading) == "" {
			problems = append(problems, fmt.Errorf("%s has no heading", where))
		}
		problems = append(problems, checkSummary(where, x.Summary, LongSummaryMin)...)
		problems = append(problems, checkDate(where, "effective date", x.EffectiveDate, false)...)

		items := map[string]bool{}
		for _, item := range x.Items {
			at := fmt.Sprintf("%s item %q", where, item.Label)
			if strings.TrimSpace(item.Label) == "" {
				problems = append(problems, fmt.Errorf("%s: an item has no label", where))
			}
			if items[item.Label] {
				problems = append(problems, fmt.Errorf("%s appears twice in the pack", at))
			}
			items[item.Label] = true
			problems = append(problems, checkSummary(at, item.Summary, LongSummaryMin)...)
			problems = append(problems, checkDate(at, "effective date", item.EffectiveDate, false)...)
		}
	}

	// A link naming an article or recital the pack does not carry. Checked here
	// rather than left to the foreign key, because the message matters: "article
	// 99 is not in this pack" is actionable and `23503` is not.
	for _, link := range d.ArticleRecitals {
		if !articles[link.ArticleNumber] {
			problems = append(problems, fmt.Errorf(
				"a recital link names article %d, which is not in this pack", link.ArticleNumber))
		}
		if !recitals[link.RecitalNumber] {
			problems = append(problems, fmt.Errorf(
				"a recital link names recital %d, which is not in this pack", link.RecitalNumber))
		}
	}

	return problems
}

func validateObligations(obligations []Obligation) []error {
	var problems []error
	slugs := map[string]bool{}

	for _, o := range obligations {
		where := fmt.Sprintf("obligation %q", o.Slug)
		if strings.TrimSpace(o.Slug) == "" {
			problems = append(problems, fmt.Errorf("an obligation has no slug"))
			continue
		}
		if slugs[o.Slug] {
			problems = append(problems, fmt.Errorf("%s appears twice in the pack", where))
		}
		slugs[o.Slug] = true

		if strings.TrimSpace(o.Title) == "" {
			problems = append(problems, fmt.Errorf("%s has no title", where))
		}
		problems = append(problems, checkSummary(where, o.Summary, LongSummaryMin)...)

		if !severities[o.Severity] {
			problems = append(problems, fmt.Errorf(
				"%s has severity %q, which is not one of low, medium, high", where, o.Severity))
		}
		if o.DueWithinDays < 0 {
			problems = append(problems, fmt.Errorf("%s has a negative due window", where))
		}
		problems = append(problems, checkDate(where, "effective date", o.EffectiveDate, false)...)
		problems = append(problems, o.Citation.validate(where)...)
	}

	return problems
}

// validate checks a citation's shape. Whether it RESOLVES is a database
// question and is answered inside the ingest transaction.
func (c Citation) validate(where string) []error {
	var problems []error

	if strings.TrimSpace(c.Celex) == "" {
		problems = append(problems, fmt.Errorf("%s cites no document", where))
	}

	switch c.Kind {
	case KindArticle:
		if c.ArticleNumber <= 0 {
			problems = append(problems, fmt.Errorf("%s cites an article with no number", where))
		}
		if c.RecitalNumber != 0 || c.AnnexLabel != "" {
			problems = append(problems, fmt.Errorf(
				"%s cites an article and also a recital or annex; a citation names one thing", where))
		}
	case KindRecital:
		if c.RecitalNumber <= 0 {
			problems = append(problems, fmt.Errorf("%s cites a recital with no number", where))
		}
		if c.ArticleNumber != 0 || c.AnnexLabel != "" {
			problems = append(problems, fmt.Errorf(
				"%s cites a recital and also an article or annex; a citation names one thing", where))
		}
		if c.ParagraphLabel != "" {
			// Recitals have no paragraphs. Silently dropping it would store a
			// citation that says less than the pack claimed.
			problems = append(problems, fmt.Errorf("%s cites a recital with a paragraph label", where))
		}
	case KindAnnex:
		if strings.TrimSpace(c.AnnexLabel) == "" {
			problems = append(problems, fmt.Errorf("%s cites an annex with no label", where))
		}
		if c.ArticleNumber != 0 || c.RecitalNumber != 0 {
			problems = append(problems, fmt.Errorf(
				"%s cites an annex and also an article or recital; a citation names one thing", where))
		}
	default:
		problems = append(problems, fmt.Errorf(
			"%s has citation kind %q, which is not one of article, recital, annex", where, c.Kind))
	}

	return problems
}

func validateGuidelines(guidelines []Guideline) []error {
	var problems []error
	slugs := map[string]bool{}

	for _, g := range guidelines {
		where := fmt.Sprintf("guideline %q", g.Slug)
		if strings.TrimSpace(g.Slug) == "" {
			problems = append(problems, fmt.Errorf("a guideline has no slug"))
			continue
		}
		if slugs[g.Slug] {
			problems = append(problems, fmt.Errorf("%s appears twice in the pack", where))
		}
		slugs[g.Slug] = true

		if strings.TrimSpace(g.Title) == "" {
			problems = append(problems, fmt.Errorf("%s has no title", where))
		}
		if strings.TrimSpace(g.Publisher) == "" {
			problems = append(problems, fmt.Errorf("%s has no publisher", where))
		}
		problems = append(problems, checkDate(where, "adopted date", g.AdoptedDate, true)...)
		problems = append(problems, checkURL(where, "source URL", g.SourceURL)...)
	}

	return problems
}

func validateDecisions(decisions []EnforcementDecision) []error {
	var problems []error
	slugs := map[string]bool{}

	for _, d := range decisions {
		where := fmt.Sprintf("enforcement decision %q", d.Slug)
		if strings.TrimSpace(d.Slug) == "" {
			problems = append(problems, fmt.Errorf("an enforcement decision has no slug"))
			continue
		}
		if slugs[d.Slug] {
			problems = append(problems, fmt.Errorf("%s appears twice in the pack", where))
		}
		slugs[d.Slug] = true

		if strings.TrimSpace(d.Title) == "" {
			problems = append(problems, fmt.Errorf("%s has no title", where))
		}
		if strings.TrimSpace(d.DPA) == "" {
			problems = append(problems, fmt.Errorf("%s names no supervisory authority", where))
		}
		problems = append(problems, checkSummary(where, d.Summary, LongSummaryMin)...)
		problems = append(problems, checkDate(where, "decision date", d.DecisionDate, true)...)
		problems = append(problems, checkURL(where, "source URL", d.SourceURL)...)

		if d.HasFine && d.FineEUR < 0 {
			problems = append(problems, fmt.Errorf("%s has a negative fine", where))
		}
	}

	return problems
}

func checkSummary(where, summary string, min int) []error {
	// Counted in runes rather than bytes, matching Postgres's `char_length`. A
	// summary of accented text would otherwise pass here and be refused by the
	// constraint, which is the worst place to find out.
	length := len([]rune(summary))
	switch {
	case length < min:
		return []error{fmt.Errorf(
			"%s: summary is %d characters, the minimum is %d", where, length, min)}
	case length > SummaryMax:
		return []error{fmt.Errorf(
			"%s: summary is %d characters, the maximum is %d", where, length, SummaryMax)}
	}
	return nil
}

func checkDate(where, field, value string, required bool) []error {
	if value == "" {
		if required {
			return []error{fmt.Errorf("%s has no %s", where, field)}
		}
		return nil
	}
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return []error{fmt.Errorf(
			"%s: %s %q is not an ISO date (YYYY-MM-DD)", where, field, value)}
	}
	return nil
}

// checkURL is deliberately shallow: a scheme and a host.
//
// Not a fetch, and not a full parse against every RFC. What this is protecting
// against is a curator pasting a citation instead of a link, which is the
// mistake that actually happens; a URL that resolves today and 404s next year
// is not something a validator can catch, and the corpus stores no verbatim
// text precisely so that the publisher's copy stays the canonical one.
func checkURL(where, field, value string) []error {
	if strings.TrimSpace(value) == "" {
		return []error{fmt.Errorf("%s has no %s", where, field)}
	}
	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
		return []error{fmt.Errorf("%s: %s %q is not a URL", where, field, value)}
	}
	if rest := strings.SplitN(value, "://", 2); len(rest) != 2 || rest[1] == "" {
		return []error{fmt.Errorf("%s: %s %q has no host", where, field, value)}
	}
	return nil
}

// Problems renders a list of errors for a caller that has to put them in one
// message, sorted so two runs over the same pack read identically.
func Problems(problems []error) string {
	messages := make([]string, 0, len(problems))
	for _, problem := range problems {
		messages = append(messages, problem.Error())
	}
	sort.Strings(messages)
	return strings.Join(messages, "; ")
}
