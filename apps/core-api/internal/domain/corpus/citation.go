package corpus

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Rendering a citation for a reader (ENT-259, inside ENT-225).
//
// # WHY THIS MOVED OUT OF SQL, AND WHY IT HAD TO MOVE ALL AT ONCE
//
// `analyst_citation_label` and `analyst_citation_url` were plpgsql, and
// `corpus_read.go` used to say at length why it called them rather than
// rendering in Go: a finding saying `GDPR Art. 30` while the obligation page
// says `32016R0679 Art. 30` is the inconsistency that makes a customer stop
// trusting both. That reasoning was right and it is the reason this file
// exists rather than an argument against it. There is still exactly one
// renderer; it is now in Go, and both callers use it.
//
// Moving only the Analyst's half would have created precisely the divergence
// that comment warned about, which is why the corpus read moves in the same
// commit.
//
// # WHAT IS PRESERVED, INCLUDING THE PART THAT LOOKS WRONG
//
// This is a move, not a change. Every output below is what the plpgsql
// produced for the same input, including the paragraph suffix on an article,
// which produces an unbalanced parenthesis and is pinned by a test that says
// so. Correcting it would change a label a customer has already read, which is
// a product decision and belongs in its own change with its own reasoning.

// regulationAbbrev is the short title a reader knows the regulation by.
//
// A CELEX number is what the corpus stores and what resolves at EUR-Lex, and
// it is not what anybody calls the law. An unrecognised one falls through to
// itself rather than to a guess: a wrong short title is worse than a number,
// because a number is visibly a number.
func regulationAbbrev(celex string) string {
	switch celex {
	case "32016R0679":
		return "GDPR"
	case "32024R1689":
		return "EU AI Act"
	default:
		return celex
	}
}

// RegulationAbbrev exposes the short title for callers outside this package.
func RegulationAbbrev(celex string) string { return regulationAbbrev(celex) }

// Label renders the citation as a reader would cite it, for example
// `GDPR Art. 30`.
//
// The empty string stands for the SQL NULL the plpgsql returned, which happened
// when a citation named a kind whose number was absent: Postgres concatenation
// with NULL is NULL, so `article` with no article number rendered as nothing at
// all. Reproduced rather than repaired, for the reason the header gives.
func (c Citation) Label() string {
	abbrev := regulationAbbrev(c.Celex)

	switch c.Kind {
	case KindArticle:
		if c.ArticleNumber == 0 {
			// `'GDPR Art. ' || null` was NULL. See the doc comment.
			return ""
		}
		label := abbrev + " Art. " + strconv.Itoa(c.ArticleNumber)
		if c.ParagraphLabel != "" {
			// Faithful to `'(' || replace(p_paragraph, '(', ')(')`, which
			// opens a parenthesis it never closes. Pinned by a test.
			label += "(" + strings.ReplaceAll(c.ParagraphLabel, "(", ")(")
		}
		return label

	case KindRecital:
		if c.RecitalNumber == 0 {
			return ""
		}
		return abbrev + " Recital " + strconv.Itoa(c.RecitalNumber)

	case KindAnnex:
		if c.AnnexLabel == "" {
			return ""
		}
		label := abbrev + " Annex " + c.AnnexLabel
		if c.ParagraphLabel != "" {
			label += " (" + c.ParagraphLabel + ")"
		}
		return label

	default:
		// Anything else renders as the regulation alone, which is the honest
		// answer: we know which law and not which part of it.
		return abbrev
	}
}

// celexRegulation matches a CELEX number for a regulation and pulls out the
// year and the sequence number, which is everything an ELI URL needs.
var celexRegulation = regexp.MustCompile(`^3(\d{4})R(\d{4})$`)

// URL renders the citation as a link into EUR-Lex's ELI copy.
//
// The empty string stands for the SQL NULL: a CELEX number that is not a
// regulation has no ELI URL of this shape, and inventing one would produce a
// link that 404s under a claim about a customer's legal exposure. Better to
// carry no link than a broken one.
func (c Citation) URL() string {
	m := celexRegulation.FindStringSubmatch(c.Celex)
	if m == nil {
		return ""
	}

	// `(v_m[2])::int` in the plpgsql, so `0679` becomes `679`. The leading
	// zeros are part of the CELEX number and not part of the ELI path.
	sequence, err := strconv.Atoi(m[2])
	if err != nil {
		return ""
	}
	base := fmt.Sprintf("https://eur-lex.europa.eu/eli/reg/%s/%d/oj", m[1], sequence)

	switch c.Kind {
	case KindArticle:
		if c.ArticleNumber == 0 {
			return ""
		}
		return base + "#art_" + strconv.Itoa(c.ArticleNumber)
	case KindRecital:
		if c.RecitalNumber == 0 {
			return ""
		}
		return base + "#rct_" + strconv.Itoa(c.RecitalNumber)
	case KindAnnex:
		if c.AnnexLabel == "" {
			return ""
		}
		return base + "#anx_" + c.AnnexLabel
	default:
		// The plpgsql's `else ''` branch: the regulation, with no fragment.
		return base
	}
}
