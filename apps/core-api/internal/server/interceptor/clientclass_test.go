package interceptor_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	dashboardservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/dashboard"
	sweepservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/sweep"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// Client-class scope resolution (ENT-221, approved 2026-08-16).
//
// A token from the configured human client holds the human scope set by
// construction, with no grant anywhere. Every other token holds exactly what it
// carries.
//
// The tests worth reading are the negative ones. A mechanism that hands out a
// scope set is only safe if it hands out exactly that set, to exactly that
// client, so the ones that matter are: a human client cannot reach `internal:*`,
// a machine client is unaffected, and an unknown client gets nothing.

const webClientID = "386092738858254342"

// buildClassChain serves a scoped read endpoint and the internal surface behind
// one chain, with client-class resolution enabled.
func buildClassChain(t *testing.T, a *authServer) (
	corev1connect.DashboardServiceClient, platformv1connect.SweepServiceClient,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	scopes, err := interceptor.NewScope(server.Services(),
		interceptor.WithHumanClient(webClientID))
	if err != nil {
		t.Fatalf("building the scope table: %v", err)
	}

	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	)
	internal := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboardservice.New(), chain))
	mux.Handle(platformv1connect.NewSweepServiceHandler(
		sweepservice.New(&recordingProducer{}), internal))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return corev1connect.NewDashboardServiceClient(srv.Client(), srv.URL),
		platformv1connect.NewSweepServiceClient(srv.Client(), srv.URL)
}

// tokenFrom mints a token for a client, carrying whatever scopes are given.
func tokenFrom(t *testing.T, a *authServer, clientID, scopes string) map[string]string {
	t.Helper()

	claim := "class-subject"
	extra := map[string]any{"scope": scopes}
	if clientID != "" {
		extra["client_id"] = clientID
	}
	return map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, extra)),
	}
}

// The mechanism itself: no grant, no roles claim, and the scope still holds.
func TestTheHumanClientHoldsTheHumanSetWithoutAnyGrant(t *testing.T) {
	a := newAuthServer(t)
	board, _ := buildClassChain(t, a)

	// `openid` only. This is precisely the token ENT-221 measured on a real
	// session: no roles claim at all.
	headers := tokenFrom(t, a, webClientID, "openid")

	_, err := board.GetDashboard(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetDashboardRequest{}), headers))

	// Reaching the handler is the assertion. It may still fail on tenancy,
	// because this subject belongs to no organisation, but it must not fail on
	// scope: dashboard:read is what it would have lacked.
	if err != nil && connect.CodeOf(err) == connect.CodePermissionDenied {
		t.Fatalf("the human client was refused a human scope: %v", err)
	}
}

// The negative that makes the mechanism safe. A human client must never reach
// the platform surface, and it is the contents of HumanScopes that guarantee
// it rather than a check somewhere that could be forgotten.
func TestTheHumanClientCannotReachTheInternalSurface(t *testing.T) {
	a := newAuthServer(t)
	_, sweeps := buildClassChain(t, a)

	headers := tokenFrom(t, a, webClientID, "openid")
	headers[interceptor.OrgHeader] = alphaOrg

	_, err := sweeps.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{}), headers))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
}

// And the human set is not additive: a scope outside it is still refused even
// when the token literally carries it. This is what keeps the set a constant
// rather than a floor, so one person cannot be widened out of band.
func TestTheHumanSetReplacesRatherThanExtendsTheTokensScopes(t *testing.T) {
	a := newAuthServer(t)
	_, sweeps := buildClassChain(t, a)

	// A token from the human client that carries internal:ingest outright.
	headers := tokenFrom(t, a, webClientID, "openid internal:ingest")
	headers[interceptor.OrgHeader] = alphaOrg

	_, err := sweeps.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{}), headers))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("a human-client token carrying internal:ingest got %v, want permission_denied", got)
	}
}

// Machine clients are untouched: the scope layer stays real for them, which is
// the half of ENT-221's settled rule that does carry information.
func TestAnotherClientStillReadsItsGrantedScopes(t *testing.T) {
	a := newAuthServer(t)
	_, sweeps := buildClassChain(t, a)

	machine := tokenFrom(t, a, "core-api-client", "openid internal:ingest")
	machine[interceptor.OrgHeader] = alphaOrg

	if _, err := sweeps.RunSweep(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RunSweepRequest{}), machine)); err != nil {
		t.Fatalf("a machine token carrying internal:ingest was refused: %v", err)
	}

	// And the same client without it is refused, so the previous assertion is
	// about the scope rather than about the client being special.
	bare := tokenFrom(t, a, "core-api-client", "openid")
	bare[interceptor.OrgHeader] = alphaOrg

	if got := codeOf(t, func() error {
		_, err := sweeps.RunSweep(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.RunSweepRequest{}), bare))
		return err
	}()); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
}

// No default. An unknown client gets nothing, rather than falling through to
// the human set because a comparison happened to be loose.
func TestAnUnknownClientGetsNothing(t *testing.T) {
	a := newAuthServer(t)
	board, _ := buildClassChain(t, a)

	headers := tokenFrom(t, a, "some-other-client", "openid")

	_, err := board.GetDashboard(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetDashboardRequest{}), headers))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
}

// A token with no client_id at all must not match an unset or empty
// configuration. The guard is on humanClient being set, and this is the test
// that keeps it there.
func TestATokenWithNoClientIDGetsNothing(t *testing.T) {
	a := newAuthServer(t)
	board, _ := buildClassChain(t, a)

	headers := tokenFrom(t, a, "", "openid")

	_, err := board.GetDashboard(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetDashboardRequest{}), headers))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
}

// With resolution disabled, nothing changes for anyone. This is the
// pre-ENT-221 behaviour and the supported configuration a deployment gets by
// leaving KINDLAST_HUMAN_CLIENT_ID unset.
func TestWithNoHumanClientConfiguredTheHumanSetIsNotHandedOut(t *testing.T) {
	a := newAuthServer(t)
	live := requireStack(t, a.server.URL)

	scopes, err := interceptor.NewScope(server.Services())
	if err != nil {
		t.Fatalf("building the scope table: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboardservice.New(),
		connect.WithInterceptors(
			interceptor.Auth(a.verifier(t)),
			interceptor.JTI(live.revocations),
			scopes.Interceptor(),
			interceptor.Tenancy(tenantOpener{live.store}),
		)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	board := corev1connect.NewDashboardServiceClient(srv.Client(), srv.URL)
	headers := tokenFrom(t, a, webClientID, "openid")

	_, err = board.GetDashboard(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetDashboardRequest{}), headers))

	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied with resolution off", got)
	}
}

// Guards the set's contents rather than its behaviour, because the contents are
// the security property: `internal:*` in HumanScopes would hand the platform
// surface to every browser session, and no other test here would catch it.
func TestTheHumanSetContainsNoInternalScope(t *testing.T) {
	for _, held := range interceptor.HumanScopes {
		if len(held) >= 9 && held[:9] == "internal:" {
			t.Fatalf("HumanScopes contains %q", held)
		}
	}
	if len(interceptor.HumanScopes) == 0 {
		t.Fatal("HumanScopes is empty, so no human can do anything")
	}
}
