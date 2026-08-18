// Package corpuspack reads the curated JSON in `data/corpus/` into packs
// (ENT-207).
//
// # WHY THIS IS A PACKAGE AND NOT A SCRIPT
//
// The loader that died with the console was five TypeScript scripts, each
// talking to the database directly, plus a sixth that regenerated a SQL seed
// from the same JSON. Nothing checked that the seed and the JSON still agreed,
// and after the console was removed the check went with it: today `obligations`
// is populated by a seed baked into 00001, and the file it was generated from
// could say anything.
//
// So the parser lives here, in Go, next to the ingest that uses it, and it is
// used by exactly two callers: the `corpus-load` command, and the test that
// asserts the corpus in the database matches the corpus in the repository.
// Those being the same code is the point. A drift guard that parses the files
// differently from the loader is a drift guard that can pass while the loader
// is broken.
//
// # THE JSON SHAPE IS THE CURATOR'S, NOT THE SCHEMA'S
//
// camelCase, `fineEur` rather than `has_fine` plus a number, articles carrying
// optional `paragraphs`. It is hand-edited by people writing summaries of
// regulation, so it stays readable in a text editor and this file absorbs the
// difference.
package corpuspack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// ManifestFile lists the packs and the order they load in. See `packs.json`
// itself for why the order matters.
const ManifestFile = "packs.json"

// Pack kinds, which select a parser. Four rather than five because the curator
// writes four shapes: a regulation, the obligations derived from regulations,
// a bibliography of guidelines, and a register of enforcement decisions.
const (
	KindDocument    = "document"
	KindObligations = "obligations"
	KindGuidelines  = "guidelines"
	KindEnforcement = "enforcement"
)

// ObligationsFile is still named here for one caller: the drift test reads the
// raw obligations JSON to compare it field by field with the stored rows. It is
// the only place that needs a path rather than a pack, and it is a deliberate
// exception rather than the last survivor of the old const block.
const ObligationsFile = "obligations.json"

// Manifest is `packs.json` parsed.
type Manifest struct {
	// `$comment` is where the reasoning lives, and `DisallowUnknownFields`
	// would refuse the file without somewhere to put it. Kept as raw JSON
	// because nothing reads it: it is written for a person opening the file.
	Comment json.RawMessage `json:"$comment"`
	Packs   []ManifestEntry `json:"packs"`
}

// ManifestEntry is one pack: what it is called, how to parse it, where it is.
type ManifestEntry struct {
	Comment json.RawMessage `json:"$comment"`
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	File    string          `json:"file"`
	Title   string          `json:"title"`
}

// LoadManifest reads `packs.json`.
//
// Validated here rather than at the first parse failure, because the mistakes
// this catches are made by somebody adding a regulation, and "kind is not one
// of document, obligations, guidelines, enforcement" is a better afternoon than
// a JSON decode error against a file they did not know was the wrong one.
func LoadManifest(dir string) (Manifest, error) {
	var manifest Manifest
	if err := readJSON(dir, ManifestFile, &manifest); err != nil {
		return Manifest{}, err
	}

	if len(manifest.Packs) == 0 {
		return Manifest{}, fmt.Errorf(
			"corpuspack: %s lists no packs, so an ingest would report a clean run of nothing",
			ManifestFile)
	}

	seenID := map[string]bool{}
	seenFile := map[string]bool{}
	for _, entry := range manifest.Packs {
		switch {
		case strings.TrimSpace(entry.ID) == "":
			return Manifest{}, fmt.Errorf("corpuspack: %s has a pack with no id", ManifestFile)
		case strings.TrimSpace(entry.File) == "":
			return Manifest{}, fmt.Errorf("corpuspack: pack %q names no file", entry.ID)
		case seenID[entry.ID]:
			return Manifest{}, fmt.Errorf("corpuspack: pack id %q appears twice", entry.ID)
		case seenFile[entry.File]:
			// Two entries over one file would ingest it twice. Harmless, since
			// every write is an upsert on a natural key, but it means the run
			// reports counts nobody can reconcile against the files.
			return Manifest{}, fmt.Errorf("corpuspack: file %q is listed twice", entry.File)
		}
		seenID[entry.ID] = true
		seenFile[entry.File] = true

		switch entry.Kind {
		case KindDocument, KindObligations, KindGuidelines, KindEnforcement:
		default:
			return Manifest{}, fmt.Errorf(
				"corpuspack: pack %q has kind %q, which is not one of %s, %s, %s, %s",
				entry.ID, entry.Kind,
				KindDocument, KindObligations, KindGuidelines, KindEnforcement)
		}
	}

	return manifest, nil
}

// Load reads one manifest entry into a pack.
func Load(dir string, entry ManifestEntry) (corpus.Pack, error) {
	var (
		pack corpus.Pack
		err  error
	)

	switch entry.Kind {
	case KindDocument:
		pack, err = LoadDocument(dir, entry.File)
	case KindObligations:
		pack, err = LoadObligations(dir, entry.File)
	case KindGuidelines:
		pack, err = LoadGuidelines(dir, entry.File)
	case KindEnforcement:
		pack, err = LoadEnforcement(dir, entry.File)
	default:
		return corpus.Pack{}, fmt.Errorf(
			"corpuspack: pack %q has unknown kind %q", entry.ID, entry.Kind)
	}
	if err != nil {
		return corpus.Pack{}, err
	}

	// The manifest names the pack, so the id is the curator's rather than a
	// file name. A pack is not a file: `obligations.json` spans two
	// regulations today, and a regulation could arrive as several files.
	pack.ID = entry.ID
	return pack, nil
}

type jsonDocument struct {
	Document struct {
		Title       string `json:"title"`
		ShortTitle  string `json:"shortTitle"`
		CelexNumber string `json:"celexNumber"`
		VersionDate string `json:"versionDate"`
		OfficialURL string `json:"officialUrl"`
	} `json:"document"`
	Articles []struct {
		ArticleNumber int    `json:"articleNumber"`
		Heading       string `json:"heading"`
		Summary       string `json:"summary"`
		EffectiveDate string `json:"effectiveDate"`
		Paragraphs    []struct {
			Label    string `json:"label"`
			Summary  string `json:"summary"`
			Ordering int    `json:"ordering"`
		} `json:"paragraphs"`
	} `json:"articles"`
	Recitals []struct {
		RecitalNumber int    `json:"recitalNumber"`
		Summary       string `json:"summary"`
	} `json:"recitals"`
	Annexes []struct {
		Label         string `json:"label"`
		Heading       string `json:"heading"`
		Summary       string `json:"summary"`
		EffectiveDate string `json:"effectiveDate"`
		Items         []struct {
			Label         string `json:"label"`
			Heading       string `json:"heading"`
			Summary       string `json:"summary"`
			EffectiveDate string `json:"effectiveDate"`
			Ordering      int    `json:"ordering"`
		} `json:"items"`
	} `json:"annexes"`
	ArticleRecitals []struct {
		ArticleNumber int `json:"articleNumber"`
		RecitalNumber int `json:"recitalNumber"`
	} `json:"articleRecitals"`
}

type jsonObligations struct {
	Obligations []struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Citation struct {
			Kind          string `json:"kind"`
			Celex         string `json:"celex"`
			ArticleNumber int    `json:"articleNumber"`
			RecitalNumber int    `json:"recitalNumber"`
			AnnexLabel    string `json:"annexLabel"`
			Paragraph     string `json:"paragraph"`
		} `json:"citation"`
		// Kept as raw JSON rather than parsed. It is matching input for the
		// Watcher, which is the thing that has an opinion about its shape, and
		// re-encoding it here would let this file quietly change what the
		// curator wrote.
		AppliesWhen   json.RawMessage `json:"appliesWhen"`
		Severity      string          `json:"severity"`
		DueWithinDays *int            `json:"dueWithinDays"`
		Recurrence    string          `json:"recurrence"`
		EffectiveDate string          `json:"effectiveDate"`
		TopicTags     []string        `json:"topicTags"`
		ActionType    string          `json:"actionType"`
	} `json:"obligations"`
}

type jsonGuidelines struct {
	Guidelines []struct {
		Slug        string   `json:"slug"`
		Publisher   string   `json:"publisher"`
		Title       string   `json:"title"`
		AdoptedDate string   `json:"adoptedDate"`
		Version     string   `json:"version"`
		SourceURL   string   `json:"sourceUrl"`
		TopicTags   []string `json:"topicTags"`
	} `json:"guidelines"`
}

type jsonDecisions struct {
	Decisions []struct {
		Slug         string   `json:"slug"`
		DPA          string   `json:"dpa"`
		Title        string   `json:"title"`
		DecisionDate string   `json:"decisionDate"`
		FineEUR      *int64   `json:"fineEur"`
		Summary      string   `json:"summary"`
		SourceURL    string   `json:"sourceUrl"`
		GDPRArticles []int    `json:"gdprArticles"`
		TopicTags    []string `json:"topicTags"`
	} `json:"decisions"`
}

// LoadDocument reads one regulation file into a pack carrying only its
// document.
func LoadDocument(dir, name string) (corpus.Pack, error) {
	var raw jsonDocument
	if err := readJSON(dir, name, &raw); err != nil {
		return corpus.Pack{}, err
	}

	document := &corpus.Document{
		Celex:       raw.Document.CelexNumber,
		Title:       raw.Document.Title,
		ShortTitle:  raw.Document.ShortTitle,
		VersionDate: raw.Document.VersionDate,
		OfficialURL: raw.Document.OfficialURL,
	}

	for _, a := range raw.Articles {
		article := corpus.Article{
			Number:        a.ArticleNumber,
			Heading:       a.Heading,
			Summary:       a.Summary,
			EffectiveDate: a.EffectiveDate,
		}
		for _, p := range a.Paragraphs {
			article.Paragraphs = append(article.Paragraphs, corpus.Paragraph{
				Label:    p.Label,
				Summary:  p.Summary,
				Ordering: p.Ordering,
			})
		}
		document.Articles = append(document.Articles, article)
	}

	for _, r := range raw.Recitals {
		document.Recitals = append(document.Recitals, corpus.Recital{
			Number:  r.RecitalNumber,
			Summary: r.Summary,
		})
	}

	for _, x := range raw.Annexes {
		annex := corpus.Annex{
			Label:         x.Label,
			Heading:       x.Heading,
			Summary:       x.Summary,
			EffectiveDate: x.EffectiveDate,
		}
		for _, item := range x.Items {
			annex.Items = append(annex.Items, corpus.AnnexItem{
				Label:         item.Label,
				Heading:       item.Heading,
				Summary:       item.Summary,
				EffectiveDate: item.EffectiveDate,
				Ordering:      item.Ordering,
			})
		}
		document.Annexes = append(document.Annexes, annex)
	}

	for _, link := range raw.ArticleRecitals {
		document.ArticleRecitals = append(document.ArticleRecitals,
			corpus.ArticleRecitalLink{
				ArticleNumber: link.ArticleNumber,
				RecitalNumber: link.RecitalNumber,
			})
	}

	return corpus.Pack{ID: name, Document: document}, nil
}

// LoadObligations reads the obligations file.
//
// Its citations point across regulations: an obligation in this one file cites
// both the GDPR and the AI Act. So it is its own pack, and the documents it
// cites must already be in the corpus or arrive in an earlier call. That is the
// ordering `corpus-load` uses and the reason the citation check reads the
// database rather than the pack.
func LoadObligations(dir, name string) (corpus.Pack, error) {
	var raw jsonObligations
	if err := readJSON(dir, name, &raw); err != nil {
		return corpus.Pack{}, err
	}

	pack := corpus.Pack{ID: name}
	for _, o := range raw.Obligations {
		obligation := corpus.Obligation{
			Slug:    o.Slug,
			Title:   o.Title,
			Summary: o.Summary,
			Citation: corpus.Citation{
				Kind:           o.Citation.Kind,
				Celex:          o.Citation.Celex,
				ArticleNumber:  o.Citation.ArticleNumber,
				RecitalNumber:  o.Citation.RecitalNumber,
				AnnexLabel:     o.Citation.AnnexLabel,
				ParagraphLabel: o.Citation.Paragraph,
			},
			Severity:      o.Severity,
			Recurrence:    o.Recurrence,
			EffectiveDate: o.EffectiveDate,
			TopicTags:     o.TopicTags,
			ActionType:    o.ActionType,
		}
		if o.DueWithinDays != nil {
			obligation.DueWithinDays = *o.DueWithinDays
		}
		if len(o.AppliesWhen) > 0 {
			obligation.AppliesWhenJSON = string(o.AppliesWhen)
		}
		pack.Obligations = append(pack.Obligations, obligation)
	}

	return pack, nil
}

func LoadGuidelines(dir, name string) (corpus.Pack, error) {
	var raw jsonGuidelines
	if err := readJSON(dir, name, &raw); err != nil {
		return corpus.Pack{}, err
	}

	pack := corpus.Pack{ID: name}
	for _, g := range raw.Guidelines {
		pack.Guidelines = append(pack.Guidelines, corpus.Guideline{
			Slug:        g.Slug,
			Publisher:   g.Publisher,
			Title:       g.Title,
			AdoptedDate: g.AdoptedDate,
			Version:     g.Version,
			SourceURL:   g.SourceURL,
			TopicTags:   g.TopicTags,
		})
	}
	return pack, nil
}

func LoadEnforcement(dir, name string) (corpus.Pack, error) {
	var raw jsonDecisions
	if err := readJSON(dir, name, &raw); err != nil {
		return corpus.Pack{}, err
	}

	pack := corpus.Pack{ID: name}
	for _, d := range raw.Decisions {
		decision := corpus.EnforcementDecision{
			Slug:         d.Slug,
			DPA:          d.DPA,
			Title:        d.Title,
			DecisionDate: d.DecisionDate,
			Summary:      d.Summary,
			SourceURL:    d.SourceURL,
			GDPRArticles: d.GDPRArticles,
			TopicTags:    d.TopicTags,
		}
		// A pointer in the JSON, so an absent fine and a fine of zero are
		// different facts. A reprimand or a processing ban is an outcome too,
		// and reading "no fine" as zero would make this register look like a
		// list of free passes.
		if d.FineEUR != nil {
			decision.FineEUR = *d.FineEUR
			decision.HasFine = true
		}
		pack.EnforcementDecisions = append(pack.EnforcementDecisions, decision)
	}
	return pack, nil
}

// All reads every pack the manifest lists, in the order it lists them.
//
// The order is not cosmetic and the manifest is where it is now decided.
// Obligations cite articles and the citation check reads the database, so the
// regulations go first; a run that ingested obligations first would refuse
// every one of them on an empty corpus and be entirely correct to.
//
// This used to be a hardcoded list of two regulation files. Adding a third act
// was a Go change, which is the thing ENT-233 set out to stop: a pack is data
// the harness loads, not a shape it is built around. It is now a line in
// `packs.json`.
func All(dir string) ([]corpus.Pack, error) {
	manifest, err := LoadManifest(dir)
	if err != nil {
		return nil, err
	}

	packs := make([]corpus.Pack, 0, len(manifest.Packs))
	for _, entry := range manifest.Packs {
		pack, err := Load(dir, entry)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}

	return packs, nil
}

func readJSON(dir, name string, into any) error {
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path) //nolint:gosec // a repo-relative corpus path
	if err != nil {
		return fmt.Errorf("corpuspack: reading %s: %w", path, err)
	}

	// DisallowUnknownFields is deliberate. A curator renaming a field, or
	// adding one this loader does not know about, would otherwise have their
	// edit silently dropped: the ingest would report a clean run and the corpus
	// would be missing whatever they meant to add. Failing loudly here is the
	// only way that mistake surfaces at all, because there is no second system
	// comparing the file to the table.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("corpuspack: parsing %s: %w", path, err)
	}
	return nil
}
