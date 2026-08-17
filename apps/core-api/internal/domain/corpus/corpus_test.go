package corpus

import (
	"strings"
	"testing"
)

// A valid pack, which each test then breaks in exactly one way.
//
// Built by a function rather than shared as a variable because several tests
// mutate nested slices, and a shared fixture would let one test's edit change
// another's meaning.
func validPack() Pack {
	long := strings.Repeat("Regulatory summary text. ", 8) // comfortably over 100

	return Pack{
		ID: "test-pack",
		Document: &Document{
			Celex:       "32016R0679",
			Title:       "A regulation",
			ShortTitle:  "Reg",
			VersionDate: "2016-05-04",
			OfficialURL: "https://eur-lex.europa.eu/eli/reg/2016/679/oj",
			Articles: []Article{
				{
					Number:  30,
					Heading: "Records of processing activities",
					Summary: "Controllers must maintain a record.",
					Paragraphs: []Paragraph{
						{Label: "5", Summary: "The 250-employee exemption.", Ordering: 0},
					},
				},
				{Number: 5, Heading: "Principles", Summary: "The principles."},
			},
			Recitals: []Recital{{Number: 82, Summary: "On records."}},
			Annexes: []Annex{
				{
					Label:   "III",
					Heading: "High-risk AI systems",
					Summary: long,
					Items: []AnnexItem{
						{Label: "1", Heading: "Biometrics", Summary: long, Ordering: 0},
					},
				},
			},
			ArticleRecitals: []ArticleRecitalLink{{ArticleNumber: 30, RecitalNumber: 82}},
		},
		Obligations: []Obligation{{
			Slug:            "gdpr-art-30-ropa",
			Title:           "Records of Processing Activities",
			Summary:         long,
			Citation:        Citation{Kind: KindArticle, Celex: "32016R0679", ArticleNumber: 30},
			AppliesWhenJSON: `{"role":"controller"}`,
			Severity:        "high",
			Recurrence:      "continuous",
			EffectiveDate:   "2018-05-25",
			TopicTags:       []string{"ropa"},
			ActionType:      "create_ropa",
		}},
		Guidelines: []Guideline{{
			Slug:        "edpb-01-2022",
			Publisher:   "EDPB",
			Title:       "Guidelines on data subject rights",
			AdoptedDate: "2022-01-18",
			SourceURL:   "https://edpb.europa.eu/guidelines",
		}},
		EnforcementDecisions: []EnforcementDecision{{
			Slug:         "cnil-2022-google",
			DPA:          "CNIL",
			Title:        "A decision",
			DecisionDate: "2022-12-31",
			FineEUR:      60_000_000,
			HasFine:      true,
			Summary:      long,
			SourceURL:    "https://www.cnil.fr/decision",
			GDPRArticles: []int{5, 6},
		}},
	}
}

func TestAValidPackHasNoProblems(t *testing.T) {
	// The fixture has to be genuinely clean, or every test below is asserting
	// against noise it did not introduce.
	if problems := validPack().Validate(); len(problems) > 0 {
		t.Fatalf("the fixture is not valid: %s", Problems(problems))
	}
}

func TestAnEmptyPackIsRefused(t *testing.T) {
	// Almost certainly a caller that failed to load its file. On a schedule, an
	// ingest of nothing reported as a success looks exactly like everything
	// working.
	problems := Pack{ID: "test-pack"}.Validate()
	if !containing(problems, "empty") {
		t.Fatalf("an empty pack was accepted: %s", Problems(problems))
	}
}

func TestACitationMustNameExactlyOneThing(t *testing.T) {
	for name, citation := range map[string]Citation{
		"article and recital": {
			Kind: KindArticle, Celex: "X", ArticleNumber: 30, RecitalNumber: 82,
		},
		"article and annex": {
			Kind: KindArticle, Celex: "X", ArticleNumber: 30, AnnexLabel: "III",
		},
		"recital and article": {
			Kind: KindRecital, Celex: "X", RecitalNumber: 82, ArticleNumber: 30,
		},
		"annex and article": {
			Kind: KindAnnex, Celex: "X", AnnexLabel: "III", ArticleNumber: 30,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// The database constraint catches this too, but by then the ingest
			// transaction is dead and the message names a constraint. A citation
			// that says two things is one somebody has to interpret, and this
			// product's whole claim is that they should not have to.
			problems := citation.validate("obligation \"x\"")
			if !containing(problems, "names one thing") {
				t.Fatalf("accepted: %s", Problems(problems))
			}
		})
	}
}

func TestACitationKindMustBeOneOfThree(t *testing.T) {
	problems := Citation{Kind: "paragraph", Celex: "X"}.validate("obligation \"x\"")
	if !containing(problems, "not one of article, recital, annex") {
		t.Fatalf("accepted an invented citation kind: %s", Problems(problems))
	}
}

func TestARecitalCitationCannotCarryAParagraph(t *testing.T) {
	// Recitals have no paragraphs. Dropping the label silently would store a
	// citation saying less than the pack claimed, and nobody would find out.
	problems := Citation{
		Kind: KindRecital, Celex: "X", RecitalNumber: 82, ParagraphLabel: "3",
	}.validate("obligation \"x\"")

	if !containing(problems, "paragraph label") {
		t.Fatalf("accepted: %s", Problems(problems))
	}
}

func TestACitationWithNoDocumentIsRefused(t *testing.T) {
	problems := Citation{Kind: KindArticle, ArticleNumber: 30}.validate("obligation \"x\"")
	if !containing(problems, "cites no document") {
		t.Fatalf("accepted: %s", Problems(problems))
	}
}

func TestADuplicateNaturalKeyInsideOnePackIsRefused(t *testing.T) {
	// The database would refuse the second write of a pair, but the message
	// would name a constraint rather than the article, and the transaction
	// would already be dead.
	t.Run("article", func(t *testing.T) {
		pack := validPack()
		pack.Document.Articles = append(pack.Document.Articles, Article{
			Number: 30, Heading: "Again", Summary: "A second article 30.",
		})
		if !containing(pack.Validate(), "article 30 appears twice") {
			t.Fatalf("accepted: %s", Problems(pack.Validate()))
		}
	})

	t.Run("obligation", func(t *testing.T) {
		pack := validPack()
		pack.Obligations = append(pack.Obligations, pack.Obligations[0])
		if !containing(pack.Validate(), "appears twice") {
			t.Fatalf("accepted: %s", Problems(pack.Validate()))
		}
	})

	t.Run("paragraph", func(t *testing.T) {
		pack := validPack()
		pack.Document.Articles[0].Paragraphs = append(
			pack.Document.Articles[0].Paragraphs,
			Paragraph{Label: "5", Summary: "Again.", Ordering: 1},
		)
		if !containing(pack.Validate(), "appears twice") {
			t.Fatalf("accepted: %s", Problems(pack.Validate()))
		}
	})
}

func TestARecitalLinkMustNameThingsInTheSamePack(t *testing.T) {
	pack := validPack()
	pack.Document.ArticleRecitals = []ArticleRecitalLink{
		{ArticleNumber: 99, RecitalNumber: 82},
		{ArticleNumber: 30, RecitalNumber: 999},
	}

	problems := pack.Validate()
	// Caught here rather than left to the foreign key, because "article 99 is
	// not in this pack" is actionable and `23503` is not.
	if !containing(problems, "article 99, which is not in this pack") {
		t.Fatalf("dangling article link accepted: %s", Problems(problems))
	}
	if !containing(problems, "recital 999, which is not in this pack") {
		t.Fatalf("dangling recital link accepted: %s", Problems(problems))
	}
}

func TestSummaryBoundsMatchTheDatabaseConstraints(t *testing.T) {
	t.Run("an obligation summary may not be a one-liner", func(t *testing.T) {
		pack := validPack()
		pack.Obligations[0].Summary = "Too short."
		if !containing(pack.Validate(), "the minimum is 100") {
			t.Fatalf("accepted: %s", Problems(pack.Validate()))
		}
	})

	t.Run("an article summary may be terse", func(t *testing.T) {
		// Some articles genuinely are one sentence, and the schema's article
		// bound is 1 rather than 100 for exactly that reason.
		pack := validPack()
		pack.Document.Articles[0].Summary = "Short."
		if problems := pack.Validate(); len(problems) > 0 {
			t.Fatalf("a terse article summary was refused: %s", Problems(problems))
		}
	})

	t.Run("nothing may exceed the maximum", func(t *testing.T) {
		pack := validPack()
		pack.Document.Articles[0].Summary = strings.Repeat("x", SummaryMax+1)
		if !containing(pack.Validate(), "the maximum is 2000") {
			t.Fatalf("accepted: %s", Problems(pack.Validate()))
		}
	})

	t.Run("length is counted in characters, not bytes", func(t *testing.T) {
		// Postgres counts `char_length` in characters. A summary of accented
		// text measured in bytes would pass here and be refused by the
		// constraint, which is the worst place to find out.
		pack := validPack()
		pack.Obligations[0].Summary = strings.Repeat("é", SummaryMax)
		if problems := pack.Validate(); len(problems) > 0 {
			t.Fatalf("a 2000-character accented summary was refused: %s", Problems(problems))
		}

		pack.Obligations[0].Summary = strings.Repeat("é", SummaryMax+1)
		if !containing(pack.Validate(), "the maximum is 2000") {
			t.Fatalf("a 2001-character summary was accepted")
		}
	})
}

func TestSeverityIsConstrainedToTheThreeTheSchemaStores(t *testing.T) {
	pack := validPack()
	pack.Obligations[0].Severity = "critical"

	if !containing(pack.Validate(), "not one of low, medium, high") {
		t.Fatalf("accepted: %s", Problems(pack.Validate()))
	}
}

func TestADateMustBeAnIsoDate(t *testing.T) {
	pack := validPack()
	pack.Document.VersionDate = "4 May 2016"

	if !containing(pack.Validate(), "is not an ISO date") {
		t.Fatalf("accepted: %s", Problems(pack.Validate()))
	}
}

func TestAnOptionalDateMayBeAbsentButNotMalformed(t *testing.T) {
	pack := validPack()
	pack.Document.Articles[0].EffectiveDate = ""
	if problems := pack.Validate(); len(problems) > 0 {
		t.Fatalf("an absent effective date was refused: %s", Problems(problems))
	}

	pack.Document.Articles[0].EffectiveDate = "2026-13-45"
	if !containing(pack.Validate(), "is not an ISO date") {
		t.Fatalf("a malformed effective date was accepted")
	}
}

func TestACitationPastedWhereAUrlBelongsIsRefused(t *testing.T) {
	// The mistake that actually happens. Not a fetch and not a full RFC parse:
	// a link that resolves today and 404s next year is not something a validator
	// can catch, which is part of why the corpus stores no verbatim text.
	pack := validPack()
	pack.Document.OfficialURL = "Regulation (EU) 2016/679, Article 30"

	if !containing(pack.Validate(), "is not a URL") {
		t.Fatalf("accepted: %s", Problems(pack.Validate()))
	}
}

func TestEveryProblemIsReportedNotJustTheFirst(t *testing.T) {
	// A curator fixing a pack wants the list. An ingest reporting one problem
	// per run would turn a ten-error pack into ten round trips through a
	// schedule.
	pack := validPack()
	pack.Obligations[0].Severity = "critical"
	pack.Obligations[0].Summary = "Short."
	pack.Document.VersionDate = "not a date"

	if got := len(pack.Validate()); got < 3 {
		t.Fatalf("reported %d problems, want at least 3: %s", got, Problems(pack.Validate()))
	}
}

func TestProblemsReadsTheSameOnEveryRun(t *testing.T) {
	// Sorted, so a curator diffing two runs sees what changed rather than what
	// a map iteration reordered.
	pack := validPack()
	pack.Obligations[0].Severity = "critical"
	pack.Document.VersionDate = "not a date"

	first := Problems(pack.Validate())
	for i := 0; i < 5; i++ {
		if got := Problems(pack.Validate()); got != first {
			t.Fatalf("run %d differed:\n%s\n%s", i, first, got)
		}
	}
}

func TestCitationTargetNamesWhatAHumanWouldLookUp(t *testing.T) {
	for want, citation := range map[string]Citation{
		"32016R0679 Article 30": {
			Kind: KindArticle, Celex: "32016R0679", ArticleNumber: 30,
		},
		"32016R0679 Article 30(5)": {
			Kind: KindArticle, Celex: "32016R0679", ArticleNumber: 30, ParagraphLabel: "5",
		},
		"32016R0679 Recital 82": {
			Kind: KindRecital, Celex: "32016R0679", RecitalNumber: 82,
		},
		"32024R1689 Annex III": {
			Kind: KindAnnex, Celex: "32024R1689", AnnexLabel: "III",
		},
	} {
		if got := citation.Target(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func containing(problems []error, substring string) bool {
	for _, problem := range problems {
		if strings.Contains(problem.Error(), substring) {
			return true
		}
	}
	return false
}
