package corpuspack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The manifest is the pack boundary in data (ENT-233), so its failure modes are
// worth more than its happy path: everything here is a mistake somebody makes
// while adding a regulation, and the useful outcome is a message that says
// which line of `packs.json` is wrong.

// writeManifest builds a corpus directory holding just a manifest.
func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return dir
}

func manifestError(t *testing.T, body string) string {
	t.Helper()
	_, err := LoadManifest(writeManifest(t, body))
	if err == nil {
		t.Fatal("the manifest was accepted, but it should not have been")
	}
	return err.Error()
}

func TestAManifestListingNoPacksIsRefused(t *testing.T) {
	// An empty manifest would ingest nothing and report a clean run, which on a
	// schedule looks exactly like everything working.
	if got := manifestError(t, `{"packs":[]}`); !strings.Contains(got, "no packs") {
		t.Errorf("the message does not explain the problem: %s", got)
	}
}

func TestAPackWithAnUnknownKindIsRefused(t *testing.T) {
	got := manifestError(t, `{"packs":[{"id":"soc2","kind":"controls","file":"soc2.json"}]}`)

	for _, want := range []string{"soc2", "controls"} {
		if !strings.Contains(got, want) {
			t.Errorf("the message does not name %q: %s", want, got)
		}
	}
}

func TestAPackWithNoIdOrNoFileIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"no id", `{"packs":[{"kind":"document","file":"x.json"}]}`, "no id"},
		{"no file", `{"packs":[{"id":"x","kind":"document"}]}`, "names no file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := manifestError(t, tc.body); !strings.Contains(got, tc.want) {
				t.Errorf("want a message mentioning %q, got: %s", tc.want, got)
			}
		})
	}
}

// Duplicates, both kinds. A repeated file ingests twice and reports counts
// nobody can reconcile; a repeated id makes the log ambiguous about which pack
// a line refers to.
func TestDuplicateIdsAndFilesAreRefused(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{
			"id twice",
			`{"packs":[{"id":"a","kind":"document","file":"one.json"},
			           {"id":"a","kind":"document","file":"two.json"}]}`,
			"appears twice",
		},
		{
			"file twice",
			`{"packs":[{"id":"a","kind":"document","file":"one.json"},
			           {"id":"b","kind":"document","file":"one.json"}]}`,
			"listed twice",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := manifestError(t, tc.body); !strings.Contains(got, tc.want) {
				t.Errorf("want a message mentioning %q, got: %s", tc.want, got)
			}
		})
	}
}

// A field the loader does not know about is refused, inherited from
// `readJSON`'s DisallowUnknownFields. Asserted here because the manifest is the
// file a newcomer edits, and a silently dropped key is how they would learn
// that their pack was never loaded.
func TestAnUnknownManifestFieldIsRefused(t *testing.T) {
	got := manifestError(t,
		`{"packs":[{"id":"a","kind":"document","file":"one.json","enabled":false}]}`)
	if !strings.Contains(got, "enabled") {
		t.Errorf("an unknown field was accepted or not named: %s", got)
	}
}

// The repository's own manifest loads, and its order puts the regulations
// before the obligations that cite them.
//
// The ordering assertion is the one worth having: it is the property that would
// otherwise be discovered by an ingest run refusing every obligation, and it is
// the easiest thing to get wrong when adding a pack, because a new regulation
// reads naturally as an append.
func TestTheRepositoryManifestLoadsAndOrdersRegulationsFirst(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "..", "..", "..", "data", "corpus"))
	if err != nil {
		t.Fatalf("loading the repository manifest: %v", err)
	}

	seenObligations := false
	for _, entry := range manifest.Packs {
		switch entry.Kind {
		case KindObligations:
			seenObligations = true
		case KindDocument:
			if seenObligations {
				t.Errorf("regulation %q is listed after a pack of obligations. Obligations cite "+
					"articles and the citation check reads the database, so every one of them "+
					"would be refused against a corpus this regulation is not in yet", entry.ID)
			}
		}
	}

	if !seenObligations {
		t.Error("the manifest lists no obligations, so the ordering rule was not exercised")
	}
}
