package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// The corpus write path, against the real schema (ENT-207).
//
// # WHY THIS RUNS AGAINST A DATABASE RATHER THAN A MOCK
//
// Because the properties under test ARE database properties. Idempotence is an
// `on conflict` clause behaving; citation resolution is a join finding
// something; "nothing is deleted" is a missing grant. A mock would assert that
// this file's Go is self-consistent, which is not the question.
//
// It also catches the class of bug that has already cost this stack three
// incidents: SQL that compiles as a Go string and is wrong. The first draft of
// the document upsert was missing a parenthesis in a row-comparison and would
// have shipped a syntax error that no amount of `go build` would have found.
//
// # CLEANUP IS BY CELEX AND SLUG, NOT BY TRUNCATION
//
// The corpus is shared reference data with no `org_id`, so there is no tenant
// transaction to roll back and no organisation to scope a delete to. Every
// fixture uses a per-run prefix and is removed by natural key afterwards, which
// is the only way to run this beside a populated corpus without eating it.

func ingestStore(t *testing.T) *CorpusStore {
	t.Helper()

	dsn := os.Getenv("PG_INGEST_URL")
	if dsn == "" {
		dsn = "postgres://kindlast_ingest:ingest-dev-password@127.0.0.1:5433/kindlast"
	}

	store, err := NewCorpus(t.Context(), dsn)
	if err != nil {
		// Skips on a laptop, fails in CI, exactly as `testStore` does.
		//
		// The first version was a bare Fatalf, copied from entitlement_test.go's
		// `migratorConn` without noticing why that one can afford to be fatal:
		// it is only ever reached AFTER `testStore` has already skipped. This
		// helper is the first call in every corpus test, so it had nothing in
		// front of it, and it turned the no-stack CI job red.
		//
		// The skip is still not a way to lose coverage. The compose job sets
		// KINDLAST_REQUIRE_STACK, which turns this same line into a failure, so
		// a green CI run cannot mean the ingest was never exercised.
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", dsn, err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", dsn, err)
	}
	t.Cleanup(store.Close)
	return store
}

// cleanup removes a run's fixtures by natural key, as the migrator, because the
// ingest role deliberately holds no delete grant.
func cleanupCorpus(t *testing.T, celex string, slugs ...string) {
	t.Helper()

	t.Cleanup(func() {
		ctx := context.Background()

		dsn := os.Getenv("PG_MIGRATOR_URL")
		if dsn == "" {
			dsn = "postgres://kindlast_migrator:migrator-dev-password@127.0.0.1:5433/kindlast"
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Errorf("cleaning up: %v", err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()

		for _, slug := range slugs {
			if _, err := conn.Exec(ctx, "delete from obligations where slug = $1", slug); err != nil {
				t.Errorf("cleaning up obligation %s: %v", slug, err)
			}
		}
		// The document cascades its articles, recitals, annexes and links.
		if _, err := conn.Exec(ctx,
			"delete from regulatory_documents where celex_number = $1", celex); err != nil {
			t.Errorf("cleaning up document %s: %v", celex, err)
		}
	})
}

func testCelex(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("TEST-%s-%d", strings.ToUpper(t.Name()[:4]), os.Getpid())
}

func testPack(celex string) corpus.Pack {
	long := strings.Repeat("Regulatory summary text. ", 8)

	return corpus.Pack{
		ID: "test-pack",
		Document: &corpus.Document{
			Celex:       celex,
			Title:       "A test regulation",
			ShortTitle:  "Test Reg",
			VersionDate: "2016-05-04",
			OfficialURL: "https://example.test/reg",
			Articles: []corpus.Article{
				{
					Number:  30,
					Heading: "Records of processing activities",
					Summary: "Controllers must maintain a record.",
					Paragraphs: []corpus.Paragraph{
						{Label: "5", Summary: "The exemption.", Ordering: 0},
					},
				},
				{
					Number:        4,
					Heading:       "AI literacy",
					Summary:       "Staff must be trained.",
					EffectiveDate: "2025-02-02",
				},
			},
			Recitals: []corpus.Recital{{Number: 82, Summary: "On records."}},
			Annexes: []corpus.Annex{{
				Label:   "III",
				Heading: "High-risk AI systems",
				Summary: long,
				Items: []corpus.AnnexItem{
					{Label: "1", Heading: "Biometrics", Summary: long, Ordering: 0},
				},
			}},
			ArticleRecitals: []corpus.ArticleRecitalLink{
				{ArticleNumber: 30, RecitalNumber: 82},
			},
		},
	}
}

func TestAPackLandsWholeAndTheSqlIsActuallyValid(t *testing.T) {
	store := ingestStore(t)
	celex := testCelex(t)
	cleanupCorpus(t, celex)

	counts, unresolved, err := store.Ingest(t.Context(), testPack(celex), false)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(unresolved) > 0 {
		t.Fatalf("unresolved citations in a pack with no obligations: %v", unresolved)
	}

	// Every table the pack touched, so a query that silently wrote nothing
	// fails here rather than at the point somebody looks for a citation.
	if counts.Documents != 1 || counts.Articles != 2 || counts.Paragraphs != 1 ||
		counts.Recitals != 1 || counts.Annexes != 1 || counts.AnnexItems != 1 ||
		counts.ArticleRecitalLinks != 1 {
		t.Fatalf("counts: %+v", counts)
	}
}

func TestReIngestingTheSamePackDuplicatesNothingAndDriftsNothing(t *testing.T) {
	// The hard requirement. §20.3 makes this a singleton on a schedule once
	// Temporal lands, and a schedule that double-wrote on retry would compound
	// silently.
	store := ingestStore(t)
	celex := testCelex(t)
	cleanupCorpus(t, celex)

	pack := testPack(celex)
	for i := 0; i < 3; i++ {
		if _, _, err := store.Ingest(t.Context(), pack, false); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}

	conn := migratorConn(t)
	var articles, paragraphs, recitals, annexes, items, links int
	err := conn.QueryRow(t.Context(), `
		select
			(select count(*) from regulatory_articles a
			   join regulatory_documents d on d.id = a.document_id
			  where d.celex_number = $1),
			(select count(*) from regulatory_article_paragraphs p
			   join regulatory_articles a on a.id = p.article_id
			   join regulatory_documents d on d.id = a.document_id
			  where d.celex_number = $1),
			(select count(*) from regulatory_recitals r
			   join regulatory_documents d on d.id = r.document_id
			  where d.celex_number = $1),
			(select count(*) from regulatory_annexes x
			   join regulatory_documents d on d.id = x.document_id
			  where d.celex_number = $1),
			(select count(*) from regulatory_annex_items i
			   join regulatory_annexes x on x.id = i.annex_id
			   join regulatory_documents d on d.id = x.document_id
			  where d.celex_number = $1),
			(select count(*) from regulatory_article_recitals ar
			   join regulatory_articles a on a.id = ar.article_id
			   join regulatory_documents d on d.id = a.document_id
			  where d.celex_number = $1)
	`, celex).Scan(&articles, &paragraphs, &recitals, &annexes, &items, &links)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}

	if articles != 2 || paragraphs != 1 || recitals != 1 ||
		annexes != 1 || items != 1 || links != 1 {
		t.Fatalf("three ingests produced %d articles, %d paragraphs, %d recitals, "+
			"%d annexes, %d items, %d links", articles, paragraphs, recitals, annexes, items, links)
	}
}

func TestAnUnchangedReIngestLeavesTimestampsAlone(t *testing.T) {
	// Otherwise every scheduled run rewrites the modification time of the whole
	// corpus, and "what changed last Tuesday" stops being answerable.
	store := ingestStore(t)
	celex := testCelex(t)
	cleanupCorpus(t, celex)

	pack := testPack(celex)
	if _, _, err := store.Ingest(t.Context(), pack, false); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	conn := migratorConn(t)
	var before string
	if err := conn.QueryRow(t.Context(),
		"select updated_at::text from regulatory_documents where celex_number = $1",
		celex).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if _, _, err := store.Ingest(t.Context(), pack, false); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	var after string
	if err := conn.QueryRow(t.Context(),
		"select updated_at::text from regulatory_documents where celex_number = $1",
		celex).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if before != after {
		t.Fatalf("an unchanged re-ingest bumped updated_at: %s then %s", before, after)
	}
}

func TestAChangedSummaryOverwritesAndBumpsTheTimestamp(t *testing.T) {
	store := ingestStore(t)
	celex := testCelex(t)
	cleanupCorpus(t, celex)

	pack := testPack(celex)
	if _, _, err := store.Ingest(t.Context(), pack, false); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	pack.Document.Articles[0].Summary = "A revised summary of Article 30."
	if _, _, err := store.Ingest(t.Context(), pack, false); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	conn := migratorConn(t)
	var summary string
	err := conn.QueryRow(t.Context(), `
		select a.summary from regulatory_articles a
		  join regulatory_documents d on d.id = a.document_id
		 where d.celex_number = $1 and a.article_number = 30
	`, celex).Scan(&summary)
	if err != nil {
		t.Fatalf("reading the summary: %v", err)
	}

	// Last write wins. A corpus that refused to correct itself would make every
	// mistake permanent.
	if summary != "A revised summary of Article 30." {
		t.Fatalf("the revision did not land: %q", summary)
	}
}

func TestAnObligationCitingNothingIsRefusedAndWritesNothing(t *testing.T) {
	// AGENTS.md opens by calling a fabricated citation worse than nothing. An
	// obligation pointing at an article that is not in the corpus surfaces to a
	// customer as a finding whose "check this against the law" goes nowhere.
	store := ingestStore(t)
	celex := testCelex(t)
	slug := fmt.Sprintf("test-dangling-%d", os.Getpid())
	cleanupCorpus(t, celex, slug)

	pack := testPack(celex)
	pack.Obligations = []corpus.Obligation{{
		Slug:     slug,
		Title:    "An obligation citing thin air",
		Summary:  strings.Repeat("Summary text. ", 10),
		Citation: corpus.Citation{Kind: corpus.KindArticle, Celex: celex, ArticleNumber: 999},
		Severity: "high",
	}}

	counts, unresolved, err := store.Ingest(t.Context(), pack, false)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(unresolved) != 1 {
		t.Fatalf("want one unresolved citation, got %v", unresolved)
	}
	if !strings.Contains(unresolved[0], "Article 999") {
		t.Fatalf("the message does not name the citation: %q", unresolved[0])
	}
	if counts.Obligations != 0 {
		t.Fatalf("an obligation was counted despite the refusal: %+v", counts)
	}

	// And the whole pack rolled back, including the document that was fine. A
	// pack that half-landed is a corpus somebody has to reconcile by hand.
	conn := migratorConn(t)
	var documents int
	if err := conn.QueryRow(t.Context(),
		"select count(*) from regulatory_documents where celex_number = $1",
		celex).Scan(&documents); err != nil {
		t.Fatalf("counting documents: %v", err)
	}
	if documents != 0 {
		t.Fatal("the document survived a refused pack")
	}
}

func TestAnObligationCitingItsOwnPackResolves(t *testing.T) {
	// The ordering that makes this work: the citation check runs after the
	// document is written and before the commit. Checked on another connection
	// it would refuse every self-contained pack, because those rows are not
	// committed yet.
	store := ingestStore(t)
	celex := testCelex(t)
	slug := fmt.Sprintf("test-selfcontained-%d", os.Getpid())
	cleanupCorpus(t, celex, slug)

	pack := testPack(celex)
	pack.Obligations = []corpus.Obligation{{
		Slug:     slug,
		Title:    "An obligation citing its own pack",
		Summary:  strings.Repeat("Summary text. ", 10),
		Citation: corpus.Citation{Kind: corpus.KindArticle, Celex: celex, ArticleNumber: 30},
		Severity: "high",
	}}

	counts, unresolved, err := store.Ingest(t.Context(), pack, false)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(unresolved) > 0 {
		t.Fatalf("a self-contained pack refused itself: %v", unresolved)
	}
	if counts.Obligations != 1 {
		t.Fatalf("counts: %+v", counts)
	}
}

func TestAParagraphCitationIsCheckedToo(t *testing.T) {
	// Paragraph labels are hand-written, so a wrong one is the likelier mistake.
	store := ingestStore(t)
	celex := testCelex(t)
	slug := fmt.Sprintf("test-para-%d", os.Getpid())
	cleanupCorpus(t, celex, slug)

	pack := testPack(celex)
	pack.Obligations = []corpus.Obligation{{
		Slug:    slug,
		Title:   "An obligation citing a paragraph",
		Summary: strings.Repeat("Summary text. ", 10),
		Citation: corpus.Citation{
			Kind: corpus.KindArticle, Celex: celex,
			ArticleNumber: 30, ParagraphLabel: "99",
		},
		Severity: "medium",
	}}

	_, unresolved, err := store.Ingest(t.Context(), pack, false)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(unresolved) != 1 {
		t.Fatalf("a nonexistent paragraph resolved: %v", unresolved)
	}

	// The one that does exist must pass, or the check is just refusing
	// everything.
	pack.Obligations[0].Citation.ParagraphLabel = "5"
	_, unresolved, err = store.Ingest(t.Context(), pack, false)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(unresolved) > 0 {
		t.Fatalf("a real paragraph was refused: %v", unresolved)
	}
}

func TestARecitalAndAnAnnexCitationResolveToo(t *testing.T) {
	store := ingestStore(t)
	celex := testCelex(t)
	recitalSlug := fmt.Sprintf("test-recital-%d", os.Getpid())
	annexSlug := fmt.Sprintf("test-annex-%d", os.Getpid())
	cleanupCorpus(t, celex, recitalSlug, annexSlug)

	pack := testPack(celex)
	summary := strings.Repeat("Summary text. ", 10)
	pack.Obligations = []corpus.Obligation{
		{
			Slug: recitalSlug, Title: "Cites a recital", Summary: summary,
			Citation: corpus.Citation{
				Kind: corpus.KindRecital, Celex: celex, RecitalNumber: 82,
			},
			Severity: "low",
		},
		{
			Slug: annexSlug, Title: "Cites an annex", Summary: summary,
			Citation: corpus.Citation{
				Kind: corpus.KindAnnex, Celex: celex, AnnexLabel: "III",
			},
			Severity: "high",
		},
	}

	_, unresolved, err := store.Ingest(t.Context(), pack, false)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(unresolved) > 0 {
		t.Fatalf("real citations were refused: %v", unresolved)
	}
}

func TestADryRunValidatesAndWritesNothing(t *testing.T) {
	store := ingestStore(t)
	celex := testCelex(t)
	cleanupCorpus(t, celex)

	counts, unresolved, err := store.Ingest(t.Context(), testPack(celex), true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(unresolved) > 0 {
		t.Fatalf("unresolved: %v", unresolved)
	}
	// The counts are what WOULD be written, which is the point of the dry run.
	if counts.Articles != 2 {
		t.Fatalf("counts: %+v", counts)
	}

	conn := migratorConn(t)
	var documents int
	if err := conn.QueryRow(t.Context(),
		"select count(*) from regulatory_documents where celex_number = $1",
		celex).Scan(&documents); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if documents != 0 {
		t.Fatal("a dry run wrote to the corpus")
	}
}

func TestADecisionWithNoFineStoresNullRatherThanZero(t *testing.T) {
	// A reprimand or a processing ban is an outcome too. Reading "no fine" as
	// zero would make an enforcement register read as a list of free passes.
	store := ingestStore(t)
	slug := fmt.Sprintf("test-nofine-%d", os.Getpid())

	conn := migratorConn(t)
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(),
			"delete from regulatory_enforcement_decisions where slug = $1", slug); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	})

	pack := corpus.Pack{
		ID: "test-pack",
		EnforcementDecisions: []corpus.EnforcementDecision{{
			Slug: slug, DPA: "Test DPA", Title: "A reprimand",
			DecisionDate: "2024-01-01",
			HasFine:      false,
			Summary:      strings.Repeat("Summary text. ", 10),
			SourceURL:    "https://example.test/decision",
		}},
	}

	if _, _, err := store.Ingest(t.Context(), pack, false); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	var fine *int64
	if err := conn.QueryRow(t.Context(),
		"select fine_eur from regulatory_enforcement_decisions where slug = $1",
		slug).Scan(&fine); err != nil {
		t.Fatalf("reading the fine: %v", err)
	}
	if fine != nil {
		t.Fatalf("a decision with no fine stored %d", *fine)
	}
}
