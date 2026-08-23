package interceptor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	sweepservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/sweep"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// The internal surface's gate (ENT-203).
//
// What is under test here is the chain, not the sweep: that `internal:ingest`
// is required, that a human console token cannot reach it, and that a request
// naming no organisation is refused rather than quietly sweeping nothing.
//
// The producer is a recorder rather than a real pool, and that is a deliberate
// exception to §13.2's rule against mocking. That rule protects the verifier,
// because stubbing it would mean every scope test was really testing the stub.
// The verifier here is real and mints real tokens against a real JWKS. The
// producer is not a security boundary; it is the thing being protected, and
// what these tests need to know is whether it was reached.
type recordingProducer struct {
	calls      int
	lastOrg    string
	detectOnly bool
	expiries   int

	analyses     []string
	settled      []string
	settleErrors []string
}

func (r *recordingProducer) ExpireSnoozes(context.Context) (postgres.Expiry, error) {
	r.expiries++
	return postgres.Expiry{Reemerged: 4, RanAt: time.Unix(0, 0).UTC()}, nil
}

// The part-four surface. The recorder answers one pending trigger for
// `alphaOrg`, analyses whatever organisation it is given, and remembers what
// it was told to settle.
func (r *recordingProducer) RunAnalyst(_ context.Context, orgID string) (postgres.Analysis, error) {
	r.analyses = append(r.analyses, orgID)
	return postgres.Analysis{Findings: 2, RanAt: time.Unix(0, 0).UTC()}, nil
}

func (r *recordingProducer) PendingSweepTriggers(context.Context, int) ([]postgres.SweepTrigger, error) {
	return []postgres.SweepTrigger{{ID: "55555555-5555-5555-5555-555555555555", OrgID: alphaOrg, Reason: "onboarding_confirmed"}}, nil
}

func (r *recordingProducer) SettleSweepTrigger(_ context.Context, id string, cause error) (bool, error) {
	r.settled = append(r.settled, id)
	if cause != nil {
		r.settleErrors = append(r.settleErrors, cause.Error())
	}
	return true, nil
}

func (r *recordingProducer) SweepTargets(context.Context) ([]string, error) {
	return []string{alphaOrg, betaOrg}, nil
}

func (r *recordingProducer) RunSweep(_ context.Context, orgID string, detectOnly bool) (postgres.Sweep, error) {
	r.calls++
	r.lastOrg = orgID
	r.detectOnly = detectOnly
	return postgres.Sweep{Signals: 3, Findings: 2, RanAt: time.Unix(0, 0).UTC()}, nil
}

func buildSweepChain(t *testing.T, a *authServer) (
	platformv1connect.SweepServiceClient, *recordingProducer,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)

	// The production chain for this surface: no tenancy interceptor. A service
	// client has no membership, so tenancy would resolve it to "no
	// organisation" and the sweep would run against the nil uuid, touch
	// nothing, and report success.
	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)

	producer := &recordingProducer{}
	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewSweepServiceHandler(
		sweepservice.New(producer), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return platformv1connect.NewSweepServiceClient(server.Client(), server.URL), producer
}

func sweepHeaders(t *testing.T, a *authServer, scope, orgID string) map[string]string {
	t.Helper()

	claim := "sweep-client"
	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t,
			mapClaims(a, claim, map[string]any{"scope": scope})),
	}
	if orgID != "" {
		headers[interceptor.OrgHeader] = orgID
	}
	return headers
}

func TestASweepNeedsTheInternalScope(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)

	// A fully-scoped console token. Everything a human could ever hold, and
	// still refused: internal:* is issued to service clients through client
	// credentials and never to the browser client.
	human := sweepHeaders(t, a,
		"openid profile email findings:read findings:act dashboard:read org:read org:manage",
		alphaOrg)

	_, err := client.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{}), human))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
	if producer.calls != 0 {
		t.Fatalf("the producer ran %d times for a refused request, want 0", producer.calls)
	}
}

func TestAServiceTokenCanRunASweep(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)

	res, err := client.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{}),
		sweepHeaders(t, a, "internal:ingest", alphaOrg)))
	if err != nil {
		t.Fatalf("running a sweep: %v", err)
	}

	if producer.calls != 1 {
		t.Fatalf("the producer ran %d times, want 1", producer.calls)
	}
	if producer.lastOrg != alphaOrg {
		t.Errorf("swept %q, want the organisation the header named", producer.lastOrg)
	}
	if res.Msg.GetSignals() != 3 || res.Msg.GetFindings() != 2 {
		t.Errorf("counts did not survive the round trip: %+v", res.Msg)
	}
}

// The refusal that stops a silent no-op. Without a header there is no sensible
// default: "do nothing" looks like success, and "sweep everyone" has every
// customer in its blast radius.
func TestASweepWithNoOrganisationIsRefused(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)

	_, err := client.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{}),
		sweepHeaders(t, a, "internal:ingest", "")))

	if got := codeOf(t, err); got != connect.CodeInvalidArgument {
		t.Fatalf("got %v, want invalid_argument", got)
	}
	if producer.calls != 0 {
		t.Fatalf("the producer ran %d times without an organisation, want 0", producer.calls)
	}
}

func TestDetectOnlyReachesTheProducer(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)

	if _, err := client.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{DetectOnly: true}),
		sweepHeaders(t, a, "internal:ingest", alphaOrg),
	)); err != nil {
		t.Fatalf("running a detect-only sweep: %v", err)
	}

	if !producer.detectOnly {
		t.Fatal("detect_only did not reach the producer")
	}
}

// Expiring snoozes needs the same machine scope as a sweep, and nothing a
// person holds reaches it (ENT-256, part two).
func TestExpiringSnoozesNeedsTheInternalScope(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)

	human := sweepHeaders(t, a,
		"openid profile email findings:read findings:act dashboard:read org:read org:manage",
		"")

	_, err := client.ExpireSnoozes(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ExpireSnoozesRequest{}), human))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
	if producer.expiries != 0 {
		t.Fatalf("the producer expired %d times for a refused request, want 0", producer.expiries)
	}
}

// And it takes no organisation header, which is the one way it differs from
// a sweep: a maintenance pass over every organisation has no tenant to name,
// and requiring one would force the caller to invent it.
func TestAServiceTokenCanExpireSnoozesWithNoOrganisation(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)

	res, err := client.ExpireSnoozes(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ExpireSnoozesRequest{}),
		sweepHeaders(t, a, "internal:ingest", "")))
	if err != nil {
		t.Fatalf("expiring snoozes: %v", err)
	}

	if producer.expiries != 1 {
		t.Fatalf("the producer expired %d times, want 1", producer.expiries)
	}
	if res.Msg.GetReemerged() != 4 {
		t.Errorf("count did not survive the round trip: %+v", res.Msg)
	}
}

// The part-four surface (ENT-256): the Analyst alone, the two lists the
// schedules ask for, and the settle. Same gate as the sweep, and the
// organisation-header rule applies to the one that names an organisation.
func TestNoPartFourSweepRPCIsReachableWithAHumanToken(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)
	human := sweepHeaders(t, a,
		"openid profile email findings:read findings:act dashboard:read org:read org:manage",
		alphaOrg)

	if _, err := client.RunAnalyst(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunAnalystRequest{}), human)); codeOf(t, err) != connect.CodePermissionDenied {
		t.Fatalf("analyst: got %v, want permission_denied", codeOf(t, err))
	}
	if _, err := client.ListSweepTriggers(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListSweepTriggersRequest{}), human)); codeOf(t, err) != connect.CodePermissionDenied {
		t.Fatalf("list triggers: got %v, want permission_denied", codeOf(t, err))
	}
	if _, err := client.SettleSweepTrigger(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleSweepTriggerRequest{
			TriggerId: "55555555-5555-5555-5555-555555555555",
			Outcome:   platformv1.SettleSweepTriggerRequest_OUTCOME_DONE}), human)); codeOf(t, err) != connect.CodePermissionDenied {
		t.Fatalf("settle: got %v, want permission_denied", codeOf(t, err))
	}
	if _, err := client.ListSweepTargets(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListSweepTargetsRequest{}), human)); codeOf(t, err) != connect.CodePermissionDenied {
		t.Fatalf("list targets: got %v, want permission_denied", codeOf(t, err))
	}
	if len(producer.analyses)+len(producer.settled) != 0 {
		t.Fatalf("a human token reached the producer: %+v", producer)
	}
}

func TestAServiceTokenDrivesTheSweepWorkflowsVerbs(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildSweepChain(t, a)
	service := sweepHeaders(t, a, "internal:ingest", "")

	targets, err := client.ListSweepTargets(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListSweepTargetsRequest{}), service))
	if err != nil {
		t.Fatalf("listing targets: %v", err)
	}
	if got := targets.Msg.GetOrgIds(); len(got) != 2 {
		t.Fatalf("targets = %v, want the recorder's two", got)
	}

	listed, err := client.ListSweepTriggers(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListSweepTriggersRequest{}), service))
	if err != nil {
		t.Fatalf("listing triggers: %v", err)
	}
	if got := listed.Msg.GetTriggers(); len(got) != 1 || got[0].GetOrgId() != alphaOrg || got[0].GetReason() != "onboarding_confirmed" {
		t.Fatalf("triggers = %v, want the one pending trigger for alpha", got)
	}
	trigger := listed.Msg.GetTriggers()[0]

	// The Analyst names its organisation in the header like the Watcher.
	_, err = client.RunAnalyst(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunAnalystRequest{}), service))
	if got := codeOf(t, err); got != connect.CodeInvalidArgument {
		t.Fatalf("analyst with no organisation: got %v, want invalid_argument", got)
	}
	analysed, err := client.RunAnalyst(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunAnalystRequest{}),
		sweepHeaders(t, a, "internal:ingest", trigger.GetOrgId())))
	if err != nil {
		t.Fatalf("analysing: %v", err)
	}
	if analysed.Msg.GetFindings() != 2 || len(producer.analyses) != 1 || producer.analyses[0] != alphaOrg {
		t.Fatalf("analysis = %+v for %v, want 2 findings for alpha", analysed.Msg, producer.analyses)
	}

	// A failed attempt needs its reason; done does not.
	_, err = client.SettleSweepTrigger(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleSweepTriggerRequest{
			TriggerId: trigger.GetTriggerId(), Outcome: platformv1.SettleSweepTriggerRequest_OUTCOME_FAILED}), service))
	if got := codeOf(t, err); got != connect.CodeInvalidArgument {
		t.Fatalf("failed without a reason: got %v, want invalid_argument", got)
	}
	if _, err := client.SettleSweepTrigger(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleSweepTriggerRequest{
			TriggerId: trigger.GetTriggerId(), Outcome: platformv1.SettleSweepTriggerRequest_OUTCOME_FAILED,
			Error: "the watcher fell over"}), service)); err != nil {
		t.Fatalf("recording a failed attempt: %v", err)
	}
	settled, err := client.SettleSweepTrigger(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleSweepTriggerRequest{
			TriggerId: trigger.GetTriggerId(), Outcome: platformv1.SettleSweepTriggerRequest_OUTCOME_DONE}), service))
	if err != nil || !settled.Msg.GetSettled() {
		t.Fatalf("settling done: err=%v settled=%v", err, settled.Msg.GetSettled())
	}
	if len(producer.settled) != 2 || len(producer.settleErrors) != 1 || producer.settleErrors[0] != "the watcher fell over" {
		t.Fatalf("settled %v with errors %v; want two settles, one with the cause", producer.settled, producer.settleErrors)
	}
}
