package postgres

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

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

	// Counted against the manifest rather than a literal 5. The literal was
	// itself a copy of the assumption ENT-233 removed: adding a regulation is a
	// line in `packs.json`, and a test that had to be edited alongside it would
	// be one more place the pack boundary leaked into code.
	manifest, err := corpuspack.LoadManifest(corpusDir(t))
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	if len(packs) != len(manifest.Packs) {
		t.Fatalf("loaded %d packs, but the manifest lists %d", len(packs), len(manifest.Packs))
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
		// Every regulation the manifest lists, rather than the two this test
		// was written against. A third act is then covered here the moment it
		// is added to `packs.json`, which is the point of the manifest.
		manifest, err := corpuspack.LoadManifest(dir)
		if err != nil {
			t.Fatalf("loading the manifest: %v", err)
		}

		documents := 0
		for _, entry := range manifest.Packs {
			if entry.Kind != corpuspack.KindDocument {
				continue
			}
			documents++

			pack, err := corpuspack.Load(dir, entry)
			if err != nil {
				t.Fatalf("loading %s: %v", entry.File, err)
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
			for _, article := range rawArticles(t, dir, entry.File) {
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

		// A manifest that listed no regulations would make every assertion above
		// vacuous while the subtest still reported green, which is the failure
		// mode this whole file exists to prevent.
		if documents == 0 {
			t.Error("the manifest lists no document packs, so nothing here was checked")
		}
	})
}

func TestReIngestingTheWholeCorpusChangesNothing(t *testing.T) {
	// Idempotence over the real thing rather than a fixture. §20.3 makes this a
	// scheduled singleton once Temporal lands, so the case that matters is the
	// hundredth run rather than the first.
	//
	// # THE MEASUREMENT IS THE IDENTITY SET, NOT THE TABLE (ENT-252)
	//
	// This took `count(*)` over each corpus table until ENT-252, and that made
	// it intermittently red for a reason with nothing to do with the corpus.
	// `go test ./...` runs different packages in parallel, every package in
	// apps/core-api connects to the same database, and
	// internal/server/interceptor seeds an obligation citing the real GDPR
	// CELEX and deletes it again. A whole-table count taken either side of this
	// ingest attributes that sibling's row to this ingest. The tell was that the
	// obligation count moved in both directions across runs, sixteen to
	// seventeen and seventeen to sixteen, and an ingest cannot decrease a count.
	//
	// So the fingerprint below covers exactly the rows the repository names: the
	// CELEX numbers in the document packs, and the slugs in the obligation,
	// guideline and enforcement files, all read from the files. A sibling's
	// fixture has a slug the repository does not contain, so it falls outside
	// the measurement rather than being tolerated inside it.
	//
	// # THIS TIGHTENS THE COMPARISON RATHER THAN LOOSENING IT
	//
	// Worth being explicit about, because ENT-244 warned that making the flake
	// go away by comparing less would restore the property ENT-207 removed: a
	// test that passes without checking. The old snapshot was nine counts and
	// one `max(updated_at)` over articles. The new one is a content digest of
	// every column of every shipped row in all ten tables, `updated_at`
	// included, so a re-ingest that rewrote an obligation summary in place is
	// now caught where before a count of sixteen matching a count of sixteen
	// said nothing about it.
	//
	// One thing is given up, deliberately, and it is named here so nobody
	// mistakes it for an oversight: a second ingest inventing a row under a slug
	// the repository does not contain would fall outside the identity set. That
	// is a loader inventing rows rather than an ingest failing to be idempotent,
	// and TestTheDatabaseHoldsWhatTheRepositorySays is where it belongs.
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
	shipped := shippedIdentity(t, dir)

	before := corpusFingerprint(t, conn, shipped)

	// A digest over an empty selection is a green run that checked nothing, so
	// what was selected is asserted before it is compared. This is the same
	// guard rawObligations carries against an obligations file that parsed to
	// no obligations, applied to the database side.
	assertShippedRowsPresent(t, shipped, before)

	ingestAll()
	after := corpusFingerprint(t, conn, shipped)

	for _, scope := range corpusScopes {
		if before[scope.name] != after[scope.name] {
			t.Errorf("a second ingest of the same corpus changed %s:\n  before %s\n  after  %s",
				scope.name, before[scope.name], after[scope.name])
		}
	}
}

// scopeState is what one table looked like at one instant: how many of the
// shipped rows are there, and what they contain.
type scopeState struct {
	count  int
	digest string
}

func (s scopeState) String() string {
	return fmt.Sprintf("%d rows, digest %s", s.count, s.digest)
}

// shippedCorpus is the identity of every row the repository claims: what to
// measure, named by natural key rather than by table.
//
// Read from the files rather than from corpuspack, for the reason rawObligation
// spells out. The loader deciding which rows the drift guard looks at would let
// a loader that silently dropped an obligation shrink both sides of the
// comparison together.
type shippedCorpus struct {
	celexes     []string // regulations, and everything hanging off them
	obligations []string // slugs
	guidelines  []string // slugs
	decisions   []string // slugs
}

// corpusScope is one table, restricted to the rows the shipped corpus names.
//
// `rows` selects them and takes exactly one `text[]` parameter, which `key`
// supplies. `exact` says the row count must equal the size of that identity
// set, which holds for the tables whose natural key IS the identity (a document
// per CELEX, an obligation per slug) and not for the ones hanging off a
// document, where the repository names the regulation and not the article.
type corpusScope struct {
	name  string
	rows  string
	key   func(shippedCorpus) []string
	exact bool
}

// The ten tables the ingest writes. `regulatory_article_recitals` is here and
// was not in the count snapshot this replaces: the link rows are written by the
// same transaction and are as capable of churning as anything else.
var corpusScopes = []corpusScope{
	{
		name:  "documents",
		rows:  `select * from regulatory_documents where celex_number = any($1::text[])`,
		key:   func(s shippedCorpus) []string { return s.celexes },
		exact: true,
	},
	{
		name: "articles",
		rows: `select a.* from regulatory_articles a
		         join regulatory_documents d on d.id = a.document_id
		        where d.celex_number = any($1::text[])`,
		key: func(s shippedCorpus) []string { return s.celexes },
	},
	{
		name: "article paragraphs",
		rows: `select p.* from regulatory_article_paragraphs p
		         join regulatory_articles a on a.id = p.article_id
		         join regulatory_documents d on d.id = a.document_id
		        where d.celex_number = any($1::text[])`,
		key: func(s shippedCorpus) []string { return s.celexes },
	},
	{
		name: "recitals",
		rows: `select r.* from regulatory_recitals r
		         join regulatory_documents d on d.id = r.document_id
		        where d.celex_number = any($1::text[])`,
		key: func(s shippedCorpus) []string { return s.celexes },
	},
	{
		name: "article to recital links",
		rows: `select l.* from regulatory_article_recitals l
		         join regulatory_articles a on a.id = l.article_id
		         join regulatory_documents d on d.id = a.document_id
		        where d.celex_number = any($1::text[])`,
		key: func(s shippedCorpus) []string { return s.celexes },
	},
	{
		name: "annexes",
		rows: `select x.* from regulatory_annexes x
		         join regulatory_documents d on d.id = x.document_id
		        where d.celex_number = any($1::text[])`,
		key: func(s shippedCorpus) []string { return s.celexes },
	},
	{
		name: "annex items",
		rows: `select i.* from regulatory_annex_items i
		         join regulatory_annexes x on x.id = i.annex_id
		         join regulatory_documents d on d.id = x.document_id
		        where d.celex_number = any($1::text[])`,
		key: func(s shippedCorpus) []string { return s.celexes },
	},
	{
		name:  "obligations",
		rows:  `select * from obligations where slug = any($1::text[])`,
		key:   func(s shippedCorpus) []string { return s.obligations },
		exact: true,
	},
	{
		name:  "guidelines",
		rows:  `select * from regulatory_guidelines where slug = any($1::text[])`,
		key:   func(s shippedCorpus) []string { return s.guidelines },
		exact: true,
	},
	{
		name:  "enforcement decisions",
		rows:  `select * from regulatory_enforcement_decisions where slug = any($1::text[])`,
		key:   func(s shippedCorpus) []string { return s.decisions },
		exact: true,
	},
}

// corpusFingerprint reads a count and a content digest per scope.
//
// The digest is over the whole row rather than a chosen column list, so a
// column added by a later migration is covered without anyone remembering to
// come back here. It includes `updated_at`, which is the property the `where`
// on every `do update` in corpus_write.go exists to hold: an unchanged row is
// not touched, so its timestamp does not move.
func corpusFingerprint(t *testing.T, conn *pgx.Conn, shipped shippedCorpus) map[string]string {
	t.Helper()

	out := make(map[string]string, len(corpusScopes))
	for _, scope := range corpusScopes {
		var count int
		var digest string
		// The interpolated half is a constant in this file and the identity set
		// is a bound parameter, which is the split that matters.
		query := fmt.Sprintf(`
			select count(*),
			       coalesce(md5(string_agg(r::text, E'\n' order by r::text)), 'no rows')
			  from (%s) as r`, scope.rows)
		if err := conn.QueryRow(t.Context(), query, scope.key(shipped)).Scan(&count, &digest); err != nil {
			t.Fatalf("fingerprinting %s: %v", scope.name, err)
		}
		out[scope.name] = fmt.Sprintf("%d rows, digest %s", count, digest)
	}
	return out
}

// assertShippedRowsPresent stops the comparison from being vacuous.
func assertShippedRowsPresent(t *testing.T, shipped shippedCorpus, fingerprint map[string]string) {
	t.Helper()

	for _, scope := range corpusScopes {
		want := len(scope.key(shipped))
		if want == 0 {
			t.Fatalf("the repository names no identities for %s, so nothing would be measured", scope.name)
		}

		got := fingerprint[scope.name]
		switch {
		case scope.exact && !strings.HasPrefix(got, fmt.Sprintf("%d rows,", want)):
			t.Fatalf("the repository names %d %s and the database holds %s",
				want, scope.name, got)
		case !scope.exact && strings.HasPrefix(got, "0 rows,"):
			t.Fatalf("the database holds no %s for the shipped regulations, so the digest checks nothing",
				scope.name)
		}
	}
}

// shippedIdentity reads the identity set out of the corpus files.
//
// The manifest supplies which files exist, as the articles subtest already does
// for the same reason ENT-233 gives: adding a regulation is an edit to
// packs.json rather than to Go. What each file CONTAINS is parsed here rather
// than through corpuspack, because that is the half a broken loader could
// otherwise agree with itself about.
func shippedIdentity(t *testing.T, dir string) shippedCorpus {
	t.Helper()

	manifest, err := corpuspack.LoadManifest(dir)
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}

	var shipped shippedCorpus
	for _, entry := range manifest.Packs {
		switch entry.Kind {
		case corpuspack.KindDocument:
			shipped.celexes = append(shipped.celexes, rawCelex(t, dir, entry.File))
		case corpuspack.KindObligations:
			for _, obligation := range rawObligations(t, dir) {
				shipped.obligations = append(shipped.obligations, obligation.Slug)
			}
		case corpuspack.KindGuidelines:
			shipped.guidelines = append(shipped.guidelines, rawSlugs(t, dir, entry.File, "guidelines")...)
		case corpuspack.KindEnforcement:
			shipped.decisions = append(shipped.decisions, rawSlugs(t, dir, entry.File, "decisions")...)
		default:
			t.Fatalf("pack %q has kind %q, which this test does not measure", entry.ID, entry.Kind)
		}
	}
	return shipped
}

// rawCelex reads a document pack's CELEX number, independently of the loader.
func rawCelex(t *testing.T, dir, name string) string {
	t.Helper()

	var parsed struct {
		Document struct {
			CelexNumber string `json:"celexNumber"`
		} `json:"document"`
	}
	rawJSON(t, dir, name, &parsed)

	if parsed.Document.CelexNumber == "" {
		t.Fatalf("%s names no CELEX number", name)
	}
	return parsed.Document.CelexNumber
}

// rawSlugs reads the slugs out of one array in a flat pack, independently of
// the loader.
func rawSlugs(t *testing.T, dir, name, field string) []string {
	t.Helper()

	var parsed map[string]json.RawMessage
	rawJSON(t, dir, name, &parsed)

	body, ok := parsed[field]
	if !ok {
		t.Fatalf("%s has no %q array", name, field)
	}

	var entries []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("parsing %s of %s: %v", field, name, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s parsed to no %s", name, field)
	}

	slugs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Slug == "" {
			t.Fatalf("%s carries an entry with no slug", name)
		}
		slugs = append(slugs, entry.Slug)
	}
	return slugs
}

func rawJSON(t *testing.T, dir, name string, into any) {
	t.Helper()

	blob, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if err := json.Unmarshal(blob, into); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
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
