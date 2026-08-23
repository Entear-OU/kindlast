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
//
// # OR IT MINTS ITS OWN TOKEN, WHICH IS WHAT LETS A FRESH STACK LOAD (ENT-266)
//
// A person at a terminal can paste a token. A job container cannot: a token
// lives ten minutes, a cold `docker compose up` takes longer than that, and
// there is nobody there to paste anything. So this also accepts a client id
// and a secret and mints, through the same client-credentials grant every
// other machine principal in this system uses, against the same credential
// file core-api, Intelligence and the Temporal worker read.
//
//	corpus-load -client-file /machinekey/core-api-client.json \
//	  -audience-file /machinekey/core-api-audience.txt \
//	  -oidc-discovery-url http://auth:8080/.well-known/openid-configuration \
//	  -oidc-issuer http://localhost:8300 -oidc-host-header localhost:8300
//
// Every flag also reads the environment variable core-api reads for the same
// setting, so the compose job's environment block is a copy of core-api's
// rather than a second vocabulary to keep in step.
//
// The wait is not politeness. `auth` and this job start together, so the first
// discovery routinely lands before Zitadel answers, and a loader that gave up
// there would leave a stack whose Regulation page says no regulation has been
// loaded, which is the state ENT-266 exists to remove.
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
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "corpus-load: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	// Its own flag set rather than the package-level one, so a test can drive
	// this the way an operator does: with arguments, from the top, more than
	// once in a process.
	fs := flag.NewFlagSet("corpus-load", flag.ContinueOnError)
	fs.SetOutput(out)

	api := fs.String("api", envOr("KINDLAST_CORE_API_URL", "http://localhost:8080"),
		"core-api base URL")
	token := fs.String("token", os.Getenv("KINDLAST_INGEST_TOKEN"),
		"a bearer token carrying the internal:ingest scope")
	dir := fs.String("dir", "data/corpus", "the corpus directory")
	dryRun := fs.Bool("dry-run", false, "validate and report, write nothing")
	timeout := fs.Duration("timeout", 2*time.Minute, "per-request timeout")

	// The client-credentials half. Named for the environment variables
	// core-api already reads, so a deployment configures one vocabulary.
	issuer := fs.String("oidc-issuer", os.Getenv("KINDLAST_OIDC_ISSUER"),
		"the issuer the discovery document must declare")
	discovery := fs.String("oidc-discovery-url", os.Getenv("KINDLAST_OIDC_DISCOVERY_URL"),
		"where to fetch the discovery document, when that is not the issuer's own address")
	hostHeader := fs.String("oidc-host-header", os.Getenv("KINDLAST_OIDC_HOST_HEADER"),
		"the Host header to send to the authorization server, for a split-horizon deployment")
	audience := fs.String("audience", os.Getenv("KINDLAST_OIDC_AUDIENCE"),
		"the audience to request, which on Zitadel is the project id")
	audienceFile := fs.String("audience-file", os.Getenv("KINDLAST_OIDC_AUDIENCE_FILE"),
		"a file holding the audience, written by the seed because the project id is generated")
	clientID := fs.String("client-id", os.Getenv("KINDLAST_INTERNAL_CLIENT_ID"),
		"the client id, which on Zitadel is a service user's username rather than its id")
	clientSecret := fs.String("client-secret", os.Getenv("KINDLAST_INTERNAL_CLIENT_SECRET"),
		"the client secret")
	clientFile := fs.String("client-file", os.Getenv("KINDLAST_INTERNAL_CLIENT_FILE"),
		"a file holding {clientId, clientSecret}, as the authorization server wrote it")
	wait := fs.Duration("wait", 2*time.Minute,
		"how long to wait for the authorization server to start answering")
	retryInterval := fs.Duration("retry-interval", 2*time.Second,
		"how long to wait between attempts at the authorization server")

	if err := fs.Parse(args); err != nil {
		return err
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

	ctx := context.Background()

	bearer, err := resolveToken(ctx, out, credentials{
		token:         *token,
		issuer:        *issuer,
		discoveryURL:  *discovery,
		hostHeader:    *hostHeader,
		audience:      *audience,
		audienceFile:  *audienceFile,
		clientID:      *clientID,
		clientSecret:  *clientSecret,
		clientFile:    *clientFile,
		wait:          *wait,
		retryInterval: *retryInterval,
	})
	if err != nil {
		return err
	}

	base := strings.TrimRight(*api, "/")
	client := &http.Client{Timeout: *timeout}

	// In order, and the order is not cosmetic: obligations cite articles, so
	// the regulations go first. A run that sent obligations first would have
	// every one of them refused against an empty corpus, entirely correctly.
	for _, pack := range packs {
		if err := ingest(ctx, client, out, base, bearer, pack, *dryRun); err != nil {
			return err
		}
	}

	if *dryRun {
		_, _ = fmt.Fprintln(out, "dry run: nothing was written")
	}
	return nil
}

// credentials is every way this command can come by a bearer token.
type credentials struct {
	token        string
	issuer       string
	discoveryURL string
	hostHeader   string
	audience     string
	audienceFile string
	clientID     string
	clientSecret string
	clientFile   string

	wait          time.Duration
	retryInterval time.Duration
}

// resolveToken returns the token to present, minting one if it was given
// credentials instead.
//
// A token that was handed over is used as it is and nothing is contacted. That
// is the curator's path, and it stays the shortest one: a person debugging a
// refused ingest should not also be debugging a token exchange.
func resolveToken(ctx context.Context, out io.Writer, c credentials) (string, error) {
	if given := strings.TrimSpace(c.token); given != "" {
		return given, nil
	}

	id, secret, err := clientCredentials(c)
	if err != nil {
		return "", err
	}
	if id == "" || secret == "" {
		return "", errors.New(
			"no way in: pass -token (or KINDLAST_INGEST_TOKEN) with a token carrying " +
				"internal:ingest, or pass -client-id and -client-secret (or -client-file, " +
				"or KINDLAST_INTERNAL_CLIENT_FILE) so this can mint one. The scope is " +
				"issued to service clients through client credentials and never to the " +
				"browser client")
	}

	audience, err := valueOrFile(c.audience, c.audienceFile)
	if err != nil {
		return "", fmt.Errorf("reading the audience: %w", err)
	}
	if audience == "" {
		return "", errors.New(
			"no audience: pass -audience or -audience-file. On Zitadel it is the " +
				"project id, which the seed writes to the shared volume because it is " +
				"generated rather than configured")
	}

	discoveryURL := strings.TrimSpace(c.discoveryURL)
	issuer := strings.TrimSpace(c.issuer)
	if discoveryURL == "" {
		if issuer == "" {
			return "", errors.New(
				"no authorization server: pass -oidc-issuer, or -oidc-discovery-url " +
					"when the address this process can reach differs from the issuer " +
					"the document declares")
		}
		discoveryURL = strings.TrimSuffix(issuer, "/") + oidc.DiscoveryPath
	}

	transport := &oidc.Transport{Host: strings.TrimSpace(c.hostHeader)}
	provider, err := discoverWithRetry(ctx, out, transport, discoveryURL, issuer,
		c.wait, c.retryInterval)
	if err != nil {
		return "", err
	}
	if provider.TokenEndpoint == "" {
		return "", errors.New("the authorization server advertises no token_endpoint, " +
			"so the corpus loader cannot mint a token to call core-api")
	}

	source, err := oidc.NewClientCredentials(oidc.ClientCredentialsConfig{
		Endpoint: provider.TokenEndpoint,
		ClientID: id,
		Secret:   secret,
		Audience: audience,
		// Both, and the second is the one people leave out. With the audience
		// scope alone the token authenticates perfectly and carries no roles,
		// so core-api answers permission_denied and sends somebody reading
		// grants that are already correct. The plural is not a typo.
		Scopes: []string{
			"openid",
			"urn:zitadel:iam:org:projects:roles",
			fmt.Sprintf("urn:zitadel:iam:org:project:id:%s:aud", audience),
		},
		Transport: transport,
	})
	if err != nil {
		return "", fmt.Errorf("building the corpus loader's token source: %w", err)
	}

	minted, err := source.Token(ctx)
	if err != nil {
		return "", err
	}
	return minted, nil
}

// clientCredentials resolves the client id and secret, from flags or from the
// file the authorization server wrote.
//
// The field names are Zitadel's, read as it writes them rather than
// transformed by the seed into a shape of our own, which is the same decision
// core-api's config made and for the same reason: one fewer step that can
// silently stop matching.
func clientCredentials(c credentials) (id, secret string, err error) {
	id = strings.TrimSpace(c.clientID)
	secret = strings.TrimSpace(c.clientSecret)
	if id != "" && secret != "" {
		return id, secret, nil
	}

	path := strings.TrimSpace(c.clientFile)
	if path == "" {
		return id, secret, nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		// Loud, unlike core-api's, which treats an unreadable file as "this
		// deployment does not narrate". Here there is nothing else to fall
		// back to and the run is about to fail anyway, so it fails saying
		// which file it could not read.
		return "", "", fmt.Errorf("reading the client credentials from %s: %w", path, err)
	}

	var credential struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(contents, &credential); err != nil {
		return "", "", fmt.Errorf("parsing the client credentials in %s: %w", path, err)
	}

	if id == "" {
		id = strings.TrimSpace(credential.ClientID)
	}
	if secret == "" {
		secret = strings.TrimSpace(credential.ClientSecret)
	}
	return id, secret, nil
}

// valueOrFile prefers a value and falls back to the contents of a file.
//
// Trimmed, and the trim is load-bearing: the audience file is written by a
// shell `printf` onto a shared volume, and an audience carrying a newline
// produces a scope Zitadel does not recognise and therefore a token with no
// roles at all.
func valueOrFile(value, path string) (string, error) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed, nil
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(contents)), nil
}

// discoverWithRetry is core-api's and the worker's, for the same race: `auth`
// and this job start together and the first discovery lands before Zitadel
// answers more often than not.
func discoverWithRetry(
	ctx context.Context, out io.Writer, transport *oidc.Transport,
	discoveryURL, expectedIssuer string, wait, interval time.Duration,
) (*oidc.Provider, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(wait)

	var lastErr error
	for attempt := 1; ; attempt++ {
		provider, err := oidc.DiscoverAt(ctx, transport, discoveryURL, expectedIssuer)
		if err == nil {
			return provider, nil
		}
		lastErr = err

		if !time.Now().Add(interval).Before(deadline) {
			return nil, fmt.Errorf(
				"the authorization server at %s never answered within %s: %w",
				discoveryURL, wait, lastErr)
		}
		_, _ = fmt.Fprintf(out, "waiting for the authorization server at %s (attempt %d): %v\n",
			discoveryURL, attempt, err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
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
	ctx context.Context, client *http.Client, out io.Writer,
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
		_, _ = fmt.Fprintf(out, "%s: %d citation(s) do not resolve:\n",
			pack.ID, len(parsed.UnresolvedCitations))
		for _, citation := range parsed.UnresolvedCitations {
			_, _ = fmt.Fprintf(out, "  %s\n", citation)
		}
		// Not an error on a dry run, which is what a dry run is for. A real run
		// would have failed above with a non-200.
		return nil
	}

	_, _ = fmt.Fprintf(out, "%s: %s\n", pack.ID, summarise(parsed))
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
