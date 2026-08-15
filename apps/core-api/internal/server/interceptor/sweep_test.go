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
