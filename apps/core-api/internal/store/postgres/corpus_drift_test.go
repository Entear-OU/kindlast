package postgres

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/corpuspack"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// The corpus in the database is the corpus in the repository (ENT-207).
//
// # WHAT THIS REPLACES, AND WHY IT IS A TEST RATHER THAN A SCRIPT
//
// 00001 carries a seed of `obligations` generated from
// `data/corpus/obligations.json` by a TypeScript script, guarded by a drift
// test in the console. Both died with the console (2a5c454). Since then
// nothing has checked that the seed and the file agree, which is why 00009's
// classification migration asserts its own three rows in plpgsql: it had no
// other way to know the corpus it was reasoning about was the corpus that
// shipped.
//
// A regenerate script would put that check back as something somebody has to
// remember to run. This is the version that cannot be forgotten: it loads the
// files, ingests them through the real path, and reads the rows back.
//
// # IT ALSO POPULATES THE CORPUS, WHICH WAS EMPTY
//
// Worth stating plainly because it was the surprise of this issue. Before
// ENT-207 every regulatory table except `obligations` held zero rows on a fresh
// stack: `regulatory_documents`, `regulatory_articles`, `regulatory_recitals`,
// all of them. So all sixteen seeded obligations cited articles that did not
// exist, `analyst_citation_label` fell through to its fallback for every one of
// them, and a finding's "check this against the law" had nothing behind it.
// The ingest scripts that would have filled them went with the console and
// nothing replaced them until now.

// corpusDir resolves `data/corpus/` from this package.
func corpusDir(t *testing.T) string {
	t.Helper()
	// internal/store/postgres -> apps/core-api -> repository root.
	return filepath.Join("..", "..", "..", "..", "..", "data", "corpus")
}

func TestTheRepositoryCorpusParsesAndValidates(t *testing.T) {
	// No database needed. This is the check that the curated files are
	// internally consistent, and it is the one a curator wants to run first.
	packs, err := corpuspack.All(corpusDir(t))
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	if len(packs) != 5 {
		t.Fatalf("loaded %d packs, want 5", len(packs))
	}

	for _, pack := range packs {
		if problems := pack.Validate(); len(problems) > 0 {
			t.Errorf("%s does not validate: %s", pack.ID, corpus.Problems(problems))
		}
	}
}

func TestEveryObligationCitesSomethingThatExists(t *testing.T) {
	// The acceptance criterion, and the thing AGENTS.md opens by calling worse
	// than nothing when it is false. Ingesting the whole corpus and finding no
	// unresolved citation is the assertion.
	store := ingestStore(t)
	dir := corpusDir(t)

	packs, err := corpuspack.All(dir)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	for _, pack := range packs {
		counts, unresolved, err := store.Ingest(t.Context(), pack, false)
		if err != nil {
			t.Fatalf("ingesting %s: %v", pack.ID, err)
		}
		if len(unresolved) > 0 {
			t.Fatalf("%s carries %d citation(s) that resolve to nothing:\n  %s",
				pack.ID, len(unresolved), strings.Join(unresolved, "\n  "))
		}
		t.Logf("%s: %+v", pack.ID, counts)
	}
}

func TestTheDatabaseHoldsWhatTheRepositorySays(t *testing.T) {
	// The drift guard. Runs after the ingest above has written, and compares
	// row for row rather than counting: a count matching while a summary has
	// silently changed is exactly the failure this exists to catch.
	store := ingestStore(t)
	dir := corpusDir(t)

	packs, err := corpuspack.All(dir)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	for _, pack := range packs {
		if _, unresolved, err := store.Ingest(t.Context(), pack, false); err != nil {
			t.Fatalf("ingesting %s: %v", pack.ID, err)
		} else if len(unresolved) > 0 {
			t.Fatalf("%s: %v", pack.ID, unresolved)
		}
	}

	conn := migratorConn(t)

	t.Run("obligations", func(t *testing.T) {
		// READ THE JSON HERE RATHER THAN THROUGH corpuspack, DELIBERATELY.
		//
		// The first version of this compared the database against the loader's
		// own output, and it could not fail: breaking the loader broke both
		// sides of the comparison identically, so a field it dropped or
		// mangled was invisible. Proved by adding a suffix to every obligation
		// summary in the loader and watching this stay green.
		//
		// So the comparison runs against the file, parsed independently. This
		// duplicates a little struct definition and that is the price of the
		// two sides being genuinely two sides.
		for _, want := range rawObligations(t, dir) {
			var title, summary, severity, celex, kind, actionType string
			err := conn.QueryRow(t.Context(), `
				select title, summary, severity, citation_celex, citation_kind, action_type
				  from obligations where slug = $1
			`, want.Slug).Scan(&title, &summary, &severity, &celex, &kind, &actionType)
			if err != nil {
				t.Errorf("%s is in the repository and not in the database: %v", want.Slug, err)
				continue
			}

			wantAction := want.ActionType
			if wantAction == "" {
				// The column is NOT NULL and defaults to `review` (00007), and
				// an obligation with no register behind it genuinely is a
				// review: approving records the decision and creates no row.
				wantAction = "review"
			}

			for _, field := range []struct{ name, got, want string }{
				{"title", title, want.Title},
				{"summary", summary, want.Summary},
				{"severity", severity, want.Severity},
				{"citation celex", celex, want.Citation.Celex},
				{"citation kind", kind, want.Citation.Kind},
				{"action type", actionType, wantAction},
			} {
				if field.got != field.want {
					t.Errorf("%s: %s drifted\n  database:   %s\n  repository: %s",
						want.Slug, field.name, truncate(field.got), truncate(field.want))
				}
			}
		}
	})

	t.Run("articles and recitals", func(t *testing.T) {
		for _, name := range []string{corpuspack.GDPRFile, corpuspack.AIActFile} {
			pack, err := corpuspack.LoadDocument(dir, name)
			if err != nil {
				t.Fatalf("loading %s: %v", name, err)
			}
			celex := pack.Document.Celex

			var articles, recitals, annexes int
			err = conn.QueryRow(t.Context(), `
				select
					(select count(*) from regulatory_articles a
					   join regulatory_documents d on d.id = a.document_id
					  where d.celex_number = $1),
					(select count(*) from regulatory_recitals r
					   join regulatory_documents d on d.id = r.document_id
					  where d.celex_number = $1),
					(select count(*) from regulatory_annexes x
					   join regulatory_documents d on d.id = x.document_id
					  where d.celex_number = $1)
			`, celex).Scan(&articles, &recitals, &annexes)
			if err != nil {
				t.Fatalf("counting %s: %v", celex, err)
			}

			// Greater-or-equal rather than exact, because the corpus is shared
			// reference data: another pack may legitimately have added an
			// article to the same regulation. What must never happen is a row
			// in the repository that is missing from the database.
			if articles < len(pack.Document.Articles) {
				t.Errorf("%s: %d articles in the database, %d in the repository",
					celex, articles, len(pack.Document.Articles))
			}
			if recitals < len(pack.Document.Recitals) {
				t.Errorf("%s: %d recitals in the database, %d in the repository",
					celex, recitals, len(pack.Document.Recitals))
			}
			if annexes < len(pack.Document.Annexes) {
				t.Errorf("%s: %d annexes in the database, %d in the repository",
					celex, annexes, len(pack.Document.Annexes))
			}

			// And a spot check on content, against the file rather than the
			// loader, for the reason the obligations subtest spells out.
			for _, article := range rawArticles(t, dir, name) {
				var summary string
				err := conn.QueryRow(t.Context(), `
					select a.summary from regulatory_articles a
					  join regulatory_documents d on d.id = a.document_id
					 where d.celex_number = $1 and a.article_number = $2
				`, celex, article.ArticleNumber).Scan(&summary)
				if err != nil {
					t.Errorf("%s article %d missing: %v", celex, article.ArticleNumber, err)
					continue
				}
				if summary != article.Summary {
					t.Errorf("%s article %d: summary drifted\n  database:   %s\n  repository: %s",
						celex, article.ArticleNumber, truncate(summary), truncate(article.Summary))
				}
			}
		}
	})
}

func TestReIngestingTheWholeCorpusChangesNothing(t *testing.T) {
	// Idempotence over the real thing rather than a fixture. §20.3 makes this a
	// scheduled singleton once Temporal lands, so the case that matters is the
	// hundredth run rather than the first.
	store := ingestStore(t)
	dir := corpusDir(t)

	packs, err := corpuspack.All(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	ingestAll := func() {
		for _, pack := range packs {
			if _, unresolved, err := store.Ingest(t.Context(), pack, false); err != nil {
				t.Fatalf("ingesting %s: %v", pack.ID, err)
			} else if len(unresolved) > 0 {
				t.Fatalf("%s: %v", pack.ID, unresolved)
			}
		}
	}

	ingestAll()

	conn := migratorConn(t)
	snapshot := func() string {
		var out string
		err := conn.QueryRow(t.Context(), `
			select
				(select count(*) from regulatory_documents)::text || '/' ||
				(select count(*) from regulatory_articles)::text || '/' ||
				(select count(*) from regulatory_article_paragraphs)::text || '/' ||
				(select count(*) from regulatory_recitals)::text || '/' ||
				(select count(*) from regulatory_annexes)::text || '/' ||
				(select count(*) from regulatory_annex_items)::text || '/' ||
				(select count(*) from obligations)::text || '/' ||
				(select count(*) from regulatory_guidelines)::text || '/' ||
				(select count(*) from regulatory_enforcement_decisions)::text || '/' ||
				(select coalesce(max(updated_at)::text, 'none') from regulatory_articles)
		`).Scan(&out)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		return out
	}

	before := snapshot()
	ingestAll()
	after := snapshot()

	// The last component is the newest `updated_at` across every article, so
	// this asserts both halves at once: no new rows, and no timestamp churn on
	// a run that changed nothing.
	if before != after {
		t.Fatalf("a second ingest of the same corpus changed the database:\n  before %s\n  after  %s",
			before, after)
	}
}

func truncate(s string) string {
	const limit = 80
	if len([]rune(s)) <= limit {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%q...", string([]rune(s)[:limit]))
}

// rawObligation is a second, independent reading of obligations.json.
//
// Deliberately not corpuspack's type. A drift guard that parses the file with
// the same code the loader uses is a guard that agrees with the loader by
// construction, including when the loader is wrong. The first version of this
// test did exactly that, and adding a suffix to every summary inside the loader
// left it green.
type rawObligation struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"`
	Citation struct {
		Kind  string `json:"kind"`
		Celex string `json:"celex"`
	} `json:"citation"`
	ActionType string `json:"actionType"`
}

func rawObligations(t *testing.T, dir string) []rawObligation {
	t.Helper()

	blob, err := os.ReadFile(filepath.Join(dir, corpuspack.ObligationsFile))
	if err != nil {
		t.Fatalf("reading obligations.json: %v", err)
	}

	var parsed struct {
		Obligations []rawObligation `json:"obligations"`
	}
	if err := json.Unmarshal(blob, &parsed); err != nil {
		t.Fatalf("parsing obligations.json: %v", err)
	}
	if len(parsed.Obligations) == 0 {
		// A guard that read an empty list would pass over every row it never
		// checked, which is the failure mode of every compare-against-a-file
		// test ever written.
		t.Fatal("obligations.json parsed to no obligations")
	}
	return parsed.Obligations
}

type rawArticle struct {
	ArticleNumber int    `json:"articleNumber"`
	Summary       string `json:"summary"`
}

func rawArticles(t *testing.T, dir, name string) []rawArticle {
	t.Helper()

	blob, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	var parsed struct {
		Articles []rawArticle `json:"articles"`
	}
	if err := json.Unmarshal(blob, &parsed); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	if len(parsed.Articles) == 0 {
		t.Fatalf("%s parsed to no articles", name)
	}
	return parsed.Articles
}
