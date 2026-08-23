package watcher

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/stackenv"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// A signal is the one thing an agent writes, so validation here is the whole
// of what stands between a model's output and a row a customer will read.
// Every case is something a model has a reason to produce: a plausible token
// that is not in the vocabulary, a title it decided to write an essay into, a
// missing deduplication key, metadata that is prose rather than JSON.
func TestWhatAModelMightProduceAndTheHandlerRefuses(t *testing.T) {
	t.Parallel()

	// The request that passes, which every case below alters one field of, so
	// each failure names exactly the field it is about.
	good := func() *platformv1.RaiseSignalRequest {
		return &platformv1.RaiseSignalRequest{
			OrgId:          "6d1cfa32-1c3d-4dd8-9c1e-6bb3a5c3f0f1",
			Kind:           "profile_gap",
			DedupKey:       "profile_gap:dpo_appointed",
			Title:          "No data protection officer recorded",
			Detail:         "Article 37 obliges some controllers to appoint one.",
			Severity:       "medium",
			ObligationSlug: "gdpr-art-37",
			MetadataJson:   `{"fact":"dpo_appointed"}`,
		}
	}

	if err := validate(good()); err != nil {
		t.Fatalf("the baseline request must be valid, and is not: %v", err)
	}

	for _, c := range []struct {
		name  string
		alter func(*platformv1.RaiseSignalRequest)
		says  string
	}{
		{"no organisation", func(r *platformv1.RaiseSignalRequest) { r.OrgId = "" }, "org_id"},
		{"a kind that reads plausibly and is not one of the four",
			func(r *platformv1.RaiseSignalRequest) { r.Kind = "policy_gap" }, "kind"},
		{"a severity from some other product's vocabulary",
			func(r *platformv1.RaiseSignalRequest) { r.Severity = "urgent" }, "severity"},
		{"no deduplication key, which is a row a day",
			func(r *platformv1.RaiseSignalRequest) { r.DedupKey = "" }, "dedup_key"},
		{"a deduplication key that is really the detail",
			func(r *platformv1.RaiseSignalRequest) { r.DedupKey = long(maxDedupKey + 1) }, "dedup_key"},
		{"no title", func(r *platformv1.RaiseSignalRequest) { r.Title = "" }, "title"},
		{"an essay in the title",
			func(r *platformv1.RaiseSignalRequest) { r.Title = long(maxTitle + 1) }, "title"},
		{"an essay in the detail",
			func(r *platformv1.RaiseSignalRequest) { r.Detail = long(maxDetail + 1) }, "detail"},
		{"metadata past the bound",
			func(r *platformv1.RaiseSignalRequest) { r.MetadataJson = `"` + long(maxMetadata) + `"` }, "metadata_json"},
		{"metadata that is prose rather than JSON",
			func(r *platformv1.RaiseSignalRequest) { r.MetadataJson = "the DPO field is empty" }, "metadata_json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			req := good()
			c.alter(req)
			err := validate(req)
			if err == nil {
				t.Fatalf("accepted %s", c.name)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the message must name %q so the caller can fix it, and says: %v", c.says, err)
			}
		})
	}
}

// Empty metadata is not absent metadata being rejected: a signal that carries
// no structured detail is ordinary, and the JSON check must not turn that into
// a refusal.
func TestNoMetadataIsNotBrokenMetadata(t *testing.T) {
	t.Parallel()

	if err := validate(&platformv1.RaiseSignalRequest{
		OrgId: "6d1cfa32-1c3d-4dd8-9c1e-6bb3a5c3f0f1", Kind: "deadline",
		DedupKey: "deadline:dsar:1", Title: "A response is due", Severity: "high",
	}); err != nil {
		t.Fatalf("a signal with no metadata must be valid, and is not: %v", err)
	}
}

// THE VOCABULARY OFFERED IS THE VOCABULARY THE SCHEMA ACCEPTS.
//
// The lists above exist so a model gets `invalid_argument` and the four
// permitted tokens rather than a constraint name out of a failed transaction.
// That is only worth having while the two agree: a token this package accepts
// and the schema rejects turns a helpful message into a lie and an internal
// error, and a token the schema accepts and this package rejects makes a
// capability unreachable with no error anywhere to find it by.
//
// So the claim is checked against the constraint rather than against a second
// copy of the same list, following `corpus_vocabulary_test.go`, and skipping on
// a laptop while failing in CI for the reason that file gives at length.
func TestTheVocabularyOfferedIsTheVocabularyTheSchemaAccepts(t *testing.T) {
	t.Parallel()

	dsn := stackenv.DSN("migrator")
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", dsn, err)
		}
		t.Skipf("no stack at %s: %v", dsn, err)
	}
	t.Cleanup(func() { _ = conn.Close(t.Context()) })

	for _, c := range []struct {
		constraint string
		offered    []string
	}{
		{"watcher_findings_kind_check", kinds},
		{"watcher_findings_severity_check", severities},
	} {
		var def string
		if err := conn.QueryRow(t.Context(), `
			select pg_get_constraintdef(oid)
			  from pg_constraint
			 where conrelid = 'public.watcher_findings'::regclass and conname = $1
		`, c.constraint).Scan(&def); err != nil {
			t.Fatalf("reading %s: %v", c.constraint, err)
		}

		accepted := tokensIn(def)
		slices.Sort(accepted)
		offered := slices.Clone(c.offered)
		slices.Sort(offered)
		if !slices.Equal(accepted, offered) {
			t.Errorf("%s accepts %v and this package offers %v", c.constraint, accepted, offered)
		}
	}
}

// tokensIn pulls the literals out of a `x = ANY (ARRAY['a'::text, ...])` body.
var tokenLiteral = regexp.MustCompile(`'([a-z_]+)'::text`)

func tokensIn(constraintDef string) []string {
	var found []string
	for _, m := range tokenLiteral.FindAllStringSubmatch(constraintDef, -1) {
		found = append(found, m[1])
	}
	return found
}

func long(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
