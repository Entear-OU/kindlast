package corpus

import "testing"

// Citation rendering, pinned output by output against the plpgsql it replaces
// (ENT-259).
//
// The expectations here were read off the running database rather than derived
// from the function body, which is the difference between a test that says
// "this is what the code does" and one that says "this is what the product has
// been showing customers".
//
// Proven able to fail: dropping the `Art. ` prefix, or rendering the sequence
// number with its leading zero, turns rows here red on their own.

func TestCitationLabel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		citation Citation
		want     string
	}{
		{
			name:     "a GDPR article",
			citation: Citation{Kind: KindArticle, Celex: "32016R0679", ArticleNumber: 30},
			want:     "GDPR Art. 30",
		},
		{
			name:     "an AI Act article",
			citation: Citation{Kind: KindArticle, Celex: "32024R1689", ArticleNumber: 26},
			want:     "EU AI Act Art. 26",
		},
		{
			name:     "an AI Act annex",
			citation: Citation{Kind: KindAnnex, Celex: "32024R1689", AnnexLabel: "III"},
			want:     "EU AI Act Annex III",
		},
		{
			name:     "a recital",
			citation: Citation{Kind: KindRecital, Celex: "32016R0679", RecitalNumber: 26},
			want:     "GDPR Recital 26",
		},
		{
			// An unrecognised CELEX falls through to itself. A number is
			// visibly a number; a wrong short title is not visibly wrong.
			name:     "an unrecognised regulation keeps its CELEX number",
			citation: Citation{Kind: KindArticle, Celex: "32022R2065", ArticleNumber: 9},
			want:     "32022R2065 Art. 9",
		},
		{
			name:     "an annex with a paragraph",
			citation: Citation{Kind: KindAnnex, Celex: "32024R1689", AnnexLabel: "IV", ParagraphLabel: "2"},
			want:     "EU AI Act Annex IV (2)",
		},
		{
			// PRESERVED, NOT REPAIRED. The plpgsql wrote
			// `'(' || replace(p_paragraph, '(', ')(')`, which opens a
			// parenthesis it never closes. No obligation in the corpus carries
			// a paragraph label today, so nothing has ever rendered this, but
			// the move must not change it: correcting a label a customer may
			// have read is a product decision with its own reasoning, not
			// something to slip into a refactor.
			name:     "an article paragraph renders exactly as it always has, unbalanced",
			citation: Citation{Kind: KindArticle, Celex: "32016R0679", ArticleNumber: 30, ParagraphLabel: "5"},
			want:     "GDPR Art. 30(5",
		},
		{
			// The kind with no number: `'GDPR Art. ' || null` was NULL, and
			// the empty string stands for it.
			name:     "an article with no number renders as nothing",
			citation: Citation{Kind: KindArticle, Celex: "32016R0679"},
			want:     "",
		},
		{
			name:     "an unknown kind falls back to the regulation alone",
			citation: Citation{Kind: "chapter", Celex: "32016R0679"},
			want:     "GDPR",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.citation.Label(); got != tc.want {
				t.Errorf("Label: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCitationURL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		citation Citation
		want     string
	}{
		{
			// The leading zero in `0679` is part of the CELEX number and not
			// part of the ELI path, which is why the plpgsql cast it to int.
			name:     "a GDPR article drops the sequence's leading zero",
			citation: Citation{Kind: KindArticle, Celex: "32016R0679", ArticleNumber: 30},
			want:     "https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_30",
		},
		{
			name:     "an AI Act article",
			citation: Citation{Kind: KindArticle, Celex: "32024R1689", ArticleNumber: 4},
			want:     "https://eur-lex.europa.eu/eli/reg/2024/1689/oj#art_4",
		},
		{
			name:     "an annex",
			citation: Citation{Kind: KindAnnex, Celex: "32024R1689", AnnexLabel: "III"},
			want:     "https://eur-lex.europa.eu/eli/reg/2024/1689/oj#anx_III",
		},
		{
			name:     "a recital",
			citation: Citation{Kind: KindRecital, Celex: "32016R0679", RecitalNumber: 26},
			want:     "https://eur-lex.europa.eu/eli/reg/2016/679/oj#rct_26",
		},
		{
			name:     "an unknown kind links to the regulation with no fragment",
			citation: Citation{Kind: "chapter", Celex: "32016R0679"},
			want:     "https://eur-lex.europa.eu/eli/reg/2016/679/oj",
		},
		{
			// A directive or a decision has no ELI URL of this shape.
			// Inventing one would put a link that 404s under a claim about a
			// customer's legal exposure.
			name:     "a CELEX number that is not a regulation has no URL",
			citation: Citation{Kind: KindArticle, Celex: "32002L0058", ArticleNumber: 5},
			want:     "",
		},
		{
			name:     "an empty CELEX number has no URL",
			citation: Citation{Kind: KindArticle, Celex: "", ArticleNumber: 5},
			want:     "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.citation.URL(); got != tc.want {
				t.Errorf("URL: got %q, want %q", got, tc.want)
			}
		})
	}
}
