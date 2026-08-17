// Command corpus-load ingests `data/corpus/` through core-api (ENT-207).
//
// # IT CALLS THE RPC, IT DOES NOT TOUCH THE DATABASE
//
// That is the whole point of it existing rather than being five scripts with a
// connection string, which is what it replaces. §16.5's single-writer rule says
// nothing outside core-api writes the schema, and the five TypeScript scripts
// that died with the console each held a service-role Supabase client. A loader
// with a database handle is a second writer no matter how careful it is, and it
// is the second writer nobody remembers to update when a constraint changes.
//
// So this speaks HTTP to `/internal/v1/corpus:ingest` with a bearer token, and
// the only thing it knows about Postgres is that core-api has one.
//
// # AND IT IS THE INTERIM, NOT THE DESTINATION
//
// §20.3 makes ingestion a singleton with a fixed workflow id once Temporal
// arrives at build-order step 8. Until then a manually-invoked run is
// acceptable, and this is it. When Temporal lands it becomes the thing that
// calls the same RPC, so the schedule arrives as a caller rather than as a
// rewrite of this file.
//
// # USAGE
//
//	corpus-load -api http://localhost:8080 -token "$TOKEN"
//	corpus-load -api http://localhost:8080 -token "$TOKEN" -dry-run
//
// The token needs the `internal:ingest` scope, which the seed issues to service
// clients through client credentials and never to the browser client. The
// Postman collection's "Token, client credentials" request records how to get
// one, including the two facts that cost an afternoon each: Zitadel's client_id
// for a service user is its username rather than its id, and the audience is
// the project id.
//
// A dry run validates everything and writes nothing, and it is the one to reach
// for first: it reports every dangling citation at once rather than failing on
// the first, so a curator can see whether to fix the pack or ingest the
// regulation it depends on.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/corpuspack"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "corpus-load: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	api := flag.String("api", envOr("KINDLAST_CORE_API_URL", "http://localhost:8080"),
		"core-api base URL")
	token := flag.String("token", os.Getenv("KINDLAST_INGEST_TOKEN"),
		"a bearer token carrying the internal:ingest scope")
	dir := flag.String("dir", "data/corpus", "the corpus directory")
	dryRun := flag.Bool("dry-run", false, "validate and report, write nothing")
	timeout := flag.Duration("timeout", 2*time.Minute, "per-request timeout")
	flag.Parse()

	if strings.TrimSpace(*token) == "" {
		return errors.New("no token: pass -token or set KINDLAST_INGEST_TOKEN. " +
			"It must carry internal:ingest, which is issued to service clients " +
			"through client credentials and never to the browser client")
	}

	packs, err := corpuspack.All(*dir)
	if err != nil {
		return err
	}

	// Validated here as well as in the handler, so a curator with a malformed
	// file learns it without a round trip and without a token.
	for _, pack := range packs {
		if problems := pack.Validate(); len(problems) > 0 {
			return fmt.Errorf("%s does not validate:\n  %s",
				pack.ID, strings.ReplaceAll(corpus.Problems(problems), "; ", "\n  "))
		}
	}

	base := strings.TrimRight(*api, "/")
	client := &http.Client{Timeout: *timeout}

	// In order, and the order is not cosmetic: obligations cite articles, so
	// the regulations go first. A run that sent obligations first would have
	// every one of them refused against an empty corpus, entirely correctly.
	for _, pack := range packs {
		if err := ingest(context.Background(), client, base, *token, pack, *dryRun); err != nil {
			return err
		}
	}

	if *dryRun {
		fmt.Println("dry run: nothing was written")
	}
	return nil
}

type ingestResponse struct {
	Applied bool `json:"applied"`
	Counts  struct {
		Documents            int32 `json:"documents"`
		Articles             int32 `json:"articles"`
		Paragraphs           int32 `json:"paragraphs"`
		Recitals             int32 `json:"recitals"`
		Annexes              int32 `json:"annexes"`
		AnnexItems           int32 `json:"annexItems"`
		ArticleRecitalLinks  int32 `json:"articleRecitalLinks"`
		Obligations          int32 `json:"obligations"`
		Guidelines           int32 `json:"guidelines"`
		EnforcementDecisions int32 `json:"enforcementDecisions"`
	} `json:"counts"`
	UnresolvedCitations []string `json:"unresolvedCitations"`
}

func ingest(
	ctx context.Context, client *http.Client,
	base, token string, pack corpus.Pack, dryRun bool,
) error {
	body, err := json.Marshal(map[string]any{
		"pack":   toWire(pack),
		"dryRun": dryRun,
	})
	if err != nil {
		return fmt.Errorf("encoding %s: %w", pack.ID, err)
	}

	url := base + "/kindlast.platform.v1.IngestService/IngestCorpus"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building the request for %s: %w", pack.ID, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling core-api for %s: %w", pack.ID, err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading the response for %s: %w", pack.ID, err)
	}

	if res.StatusCode != http.StatusOK {
		// Connect puts the reason in a JSON body. Printed whole rather than
		// summarised, because the useful case is a list of dangling citations
		// and truncating it would send somebody back for a dry run.
		return fmt.Errorf("%s: core-api answered %d: %s", pack.ID, res.StatusCode, raw)
	}

	var parsed ingestResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("parsing the response for %s: %w", pack.ID, err)
	}

	if len(parsed.UnresolvedCitations) > 0 {
		fmt.Printf("%s: %d citation(s) do not resolve:\n", pack.ID, len(parsed.UnresolvedCitations))
		for _, citation := range parsed.UnresolvedCitations {
			fmt.Printf("  %s\n", citation)
		}
		// Not an error on a dry run, which is what a dry run is for. A real run
		// would have failed above with a non-200.
		return nil
	}

	fmt.Printf("%s: %s\n", pack.ID, summarise(parsed))
	return nil
}

func summarise(res ingestResponse) string {
	parts := []string{}
	for _, item := range []struct {
		label string
		count int32
	}{
		{"documents", res.Counts.Documents},
		{"articles", res.Counts.Articles},
		{"paragraphs", res.Counts.Paragraphs},
		{"recitals", res.Counts.Recitals},
		{"annexes", res.Counts.Annexes},
		{"annex items", res.Counts.AnnexItems},
		{"recital links", res.Counts.ArticleRecitalLinks},
		{"obligations", res.Counts.Obligations},
		{"guidelines", res.Counts.Guidelines},
		{"decisions", res.Counts.EnforcementDecisions},
	} {
		if item.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", item.count, item.label))
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// toWire renders a pack as the JSON the Connect endpoint expects.
//
// Hand-built rather than reusing the generated types, so this command does not
// carry a protobuf runtime for one request. The field names are proto3 JSON's
// lowerCamelCase, which is what Connect's JSON codec reads.
func toWire(pack corpus.Pack) map[string]any {
	out := map[string]any{"packId": pack.ID}

	if pack.Document != nil {
		document := map[string]any{
			"celexNumber": pack.Document.Celex,
			"title":       pack.Document.Title,
			"shortTitle":  pack.Document.ShortTitle,
			"versionDate": pack.Document.VersionDate,
			"officialUrl": pack.Document.OfficialURL,
		}

		articles := make([]map[string]any, 0, len(pack.Document.Articles))
		for _, a := range pack.Document.Articles {
			article := map[string]any{
				"articleNumber": a.Number,
				"heading":       a.Heading,
				"summary":       a.Summary,
			}
			if a.EffectiveDate != "" {
				article["effectiveDate"] = a.EffectiveDate
			}
			if len(a.Paragraphs) > 0 {
				paragraphs := make([]map[string]any, 0, len(a.Paragraphs))
				for _, p := range a.Paragraphs {
					paragraphs = append(paragraphs, map[string]any{
						"label": p.Label, "summary": p.Summary, "ordering": p.Ordering,
					})
				}
				article["paragraphs"] = paragraphs
			}
			articles = append(articles, article)
		}
		if len(articles) > 0 {
			document["articles"] = articles
		}

		recitals := make([]map[string]any, 0, len(pack.Document.Recitals))
		for _, r := range pack.Document.Recitals {
			recitals = append(recitals, map[string]any{
				"recitalNumber": r.Number, "summary": r.Summary,
			})
		}
		if len(recitals) > 0 {
			document["recitals"] = recitals
		}

		annexes := make([]map[string]any, 0, len(pack.Document.Annexes))
		for _, x := range pack.Document.Annexes {
			annex := map[string]any{
				"label": x.Label, "heading": x.Heading, "summary": x.Summary,
			}
			if x.EffectiveDate != "" {
				annex["effectiveDate"] = x.EffectiveDate
			}
			items := make([]map[string]any, 0, len(x.Items))
			for _, item := range x.Items {
				wire := map[string]any{
					"label": item.Label, "summary": item.Summary, "ordering": item.Ordering,
				}
				if item.Heading != "" {
					wire["heading"] = item.Heading
				}
				if item.EffectiveDate != "" {
					wire["effectiveDate"] = item.EffectiveDate
				}
				items = append(items, wire)
			}
			if len(items) > 0 {
				annex["items"] = items
			}
			annexes = append(annexes, annex)
		}
		if len(annexes) > 0 {
			document["annexes"] = annexes
		}

		links := make([]map[string]any, 0, len(pack.Document.ArticleRecitals))
		for _, link := range pack.Document.ArticleRecitals {
			links = append(links, map[string]any{
				"articleNumber": link.ArticleNumber, "recitalNumber": link.RecitalNumber,
			})
		}
		if len(links) > 0 {
			document["articleRecitals"] = links
		}

		out["document"] = document
	}

	obligations := make([]map[string]any, 0, len(pack.Obligations))
	for _, o := range pack.Obligations {
		citation := map[string]any{"kind": o.Citation.Kind, "celex": o.Citation.Celex}
		if o.Citation.ArticleNumber != 0 {
			citation["articleNumber"] = o.Citation.ArticleNumber
		}
		if o.Citation.RecitalNumber != 0 {
			citation["recitalNumber"] = o.Citation.RecitalNumber
		}
		if o.Citation.AnnexLabel != "" {
			citation["annexLabel"] = o.Citation.AnnexLabel
		}
		if o.Citation.ParagraphLabel != "" {
			citation["paragraphLabel"] = o.Citation.ParagraphLabel
		}

		obligation := map[string]any{
			"slug":     o.Slug,
			"title":    o.Title,
			"summary":  o.Summary,
			"citation": citation,
			"severity": o.Severity,
		}
		if o.AppliesWhenJSON != "" {
			obligation["appliesWhenJson"] = o.AppliesWhenJSON
		}
		if o.DueWithinDays != 0 {
			obligation["dueWithinDays"] = o.DueWithinDays
		}
		if o.Recurrence != "" {
			obligation["recurrence"] = o.Recurrence
		}
		if o.EffectiveDate != "" {
			obligation["effectiveDate"] = o.EffectiveDate
		}
		if len(o.TopicTags) > 0 {
			obligation["topicTags"] = o.TopicTags
		}
		if o.ActionType != "" {
			obligation["actionType"] = o.ActionType
		}
		obligations = append(obligations, obligation)
	}
	if len(obligations) > 0 {
		out["obligations"] = obligations
	}

	guidelines := make([]map[string]any, 0, len(pack.Guidelines))
	for _, g := range pack.Guidelines {
		wire := map[string]any{
			"slug": g.Slug, "publisher": g.Publisher, "title": g.Title,
			"adoptedDate": g.AdoptedDate, "sourceUrl": g.SourceURL,
		}
		if g.Version != "" {
			wire["version"] = g.Version
		}
		if len(g.TopicTags) > 0 {
			wire["topicTags"] = g.TopicTags
		}
		guidelines = append(guidelines, wire)
	}
	if len(guidelines) > 0 {
		out["guidelines"] = guidelines
	}

	decisions := make([]map[string]any, 0, len(pack.EnforcementDecisions))
	for _, d := range pack.EnforcementDecisions {
		wire := map[string]any{
			"slug": d.Slug, "dpa": d.DPA, "title": d.Title,
			"decisionDate": d.DecisionDate, "summary": d.Summary,
			"sourceUrl": d.SourceURL,
		}
		if d.HasFine {
			// Both, because proto3 JSON omits a zero and a fine that is genuinely
			// zero must not read as absent. `hasFine` is what carries the
			// difference between "no penalty" and "a penalty of nothing".
			//
			// int64 travels as a STRING in proto3 JSON, which is the rule that
			// catches everybody: sending it as a number is rejected by some
			// codecs and silently truncated by others.
			wire["fineEur"] = fmt.Sprintf("%d", d.FineEUR)
			wire["hasFine"] = true
		}
		if len(d.GDPRArticles) > 0 {
			wire["gdprArticles"] = d.GDPRArticles
		}
		if len(d.TopicTags) > 0 {
			wire["topicTags"] = d.TopicTags
		}
		decisions = append(decisions, wire)
	}
	if len(decisions) > 0 {
		out["enforcementDecisions"] = decisions
	}

	return out
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
