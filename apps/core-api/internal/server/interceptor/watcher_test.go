package interceptor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	watcherservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/watcher"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// The Watcher's surface (ENT-258) behind the real chain.
//
// What is asserted: no human token reaches either RPC, a service token reaches
// both, and the validation a model's output has to pass is done here rather
// than at the database, so a wrong `kind` is a message naming the four rather
// than a constraint name out of a failed transaction.
//
// The producer is a recorder, which is the same deliberate exception the sweep
// tests make: the verifier is real and the producer is the thing being
// protected, so what these need to know is whether it was reached.

type recordingWatcher struct {
	contexts int
	signals  []postgres.Signal
	orgs     []string
	err      error
}

func (r *recordingWatcher) WatcherContextFor(_ context.Context, orgID string) (postgres.WatcherContext, error) {
	r.contexts++
	r.orgs = append(r.orgs, orgID)
	if r.err != nil {
		return postgres.WatcherContext{}, r.err
	}
	swept := time.Unix(0, 0).UTC()
	return postgres.WatcherContext{
		HasProfile:  true,
		ProfileID:   "p1",
		LastSweptAt: &swept,
		Facts: []postgres.WatchedFact{{
			Key: "staff_count", ValueJSON: `{"value":4}`, Source: "onboarding", ValidFrom: swept,
		}},
		Connections: []postgres.WatchedConnection{{
			ID: "c1", Kind: "mcp", DisplayName: "The helpdesk", Status: "active",
			Tools: []postgres.WatchedTool{{Name: "search", Granted: true}},
		}},
		OpenSignals: []postgres.OpenSignal{{
			ID: "s1", Kind: "profile_gap", DedupKey: "gap:obligation:gdpr-art-30-ropa",
			Title: "No record of processing", Severity: "critical", UpdatedAt: swept,
		}},
	}, nil
}

func (r *recordingWatcher) RaiseSignal(_ context.Context, orgID string, signal postgres.Signal) (string, bool, error) {
	r.orgs = append(r.orgs, orgID)
	if r.err != nil {
		return "", false, r.err
	}
	r.signals = append(r.signals, signal)
	return "11111111-1111-4111-8111-111111111111", true, nil
}

func buildWatcherChain(t *testing.T, a *authServer) (platformv1connect.WatcherServiceClient, *recordingWatcher) {
	t.Helper()
	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)
	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)
	producer := &recordingWatcher{}
	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewWatcherServiceHandler(watcherservice.New(producer, nil), chain))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return platformv1connect.NewWatcherServiceClient(server.Client(), server.URL), producer
}

func aSignal() *platformv1.RaiseSignalRequest {
	return &platformv1.RaiseSignalRequest{
		OrgId: alphaOrg, Kind: "profile_gap", DedupKey: "gap:obligation:gdpr-art-30-ropa",
		Title: "No record of processing", Detail: "Nothing in the profile shows one",
		Severity: "critical", ObligationSlug: "gdpr-art-30-ropa",
		MetadataJson: `{"missing":["ropa"]}`,
	}
}

func TestNoWatcherRPCIsReachableWithAHumanToken(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildWatcherChain(t, a)
	human := sweepHeaders(t, a, humanScopes, alphaOrg)

	_, err := client.WatcherContext(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.WatcherContextRequest{OrgId: alphaOrg}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("context: got %v, want permission_denied", got)
	}
	_, err = client.RaiseSignal(t.Context(), withHeaders(connect.NewRequest(aSignal()), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("signal: got %v, want permission_denied", got)
	}
	if producer.contexts != 0 || len(producer.signals) != 0 {
		t.Fatalf("a human token reached the producer: %+v", producer)
	}
}

func TestAServiceTokenReadsTheContextAndRaisesASignal(t *testing.T) {
	a := newAuthServer(t)
	client, producer := buildWatcherChain(t, a)
	service := sweepHeaders(t, a, "internal:ingest", "")

	context, err := client.WatcherContext(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.WatcherContextRequest{OrgId: alphaOrg}), service))
	if err != nil {
		t.Fatalf("reading the context: %v", err)
	}
	if !context.Msg.GetHasProfile() || len(context.Msg.GetFacts()) != 1 {
		t.Fatalf("context = %+v", context.Msg)
	}
	// The connection is named and its tools listed, and no endpoint crosses:
	// the agent decides what to look at, not where to dial.
	connections := context.Msg.GetConnections()
	if len(connections) != 1 || connections[0].GetDisplayName() != "The helpdesk" ||
		len(connections[0].GetTools()) != 1 {
		t.Fatalf("connections = %+v", connections)
	}
	// And it is told what it has already said, so a run can decide something
	// has changed rather than repeating itself.
	if len(context.Msg.GetOpenSignals()) != 1 ||
		context.Msg.GetOpenSignals()[0].GetDedupKey() != "gap:obligation:gdpr-art-30-ropa" {
		t.Fatalf("open signals = %+v", context.Msg.GetOpenSignals())
	}

	raised, err := client.RaiseSignal(t.Context(), withHeaders(connect.NewRequest(aSignal()), service))
	if err != nil {
		t.Fatalf("raising: %v", err)
	}
	if !raised.Msg.GetRaised() || raised.Msg.GetSignalId() == "" {
		t.Fatalf("raise = %+v", raised.Msg)
	}
	if len(producer.signals) != 1 || producer.signals[0].DedupKey != "gap:obligation:gdpr-art-30-ropa" {
		t.Fatalf("the producer was sent %+v", producer.signals)
	}
}

// What a model's output has to pass. Every one of these is a thing a model
// does: an invented severity, a plausible-looking kind, a missing dedup key, a
// title that is really a paragraph, metadata that is not JSON.
func TestASignalIsValidatedBeforeItReachesTheDatabase(t *testing.T) {
	a := newAuthServer(t)
	service := sweepHeaders(t, a, "internal:ingest", "")

	for name, mutate := range map[string]func(*platformv1.RaiseSignalRequest){
		"a kind outside the vocabulary": func(s *platformv1.RaiseSignalRequest) { s.Kind = "policy_gap" },
		"an invented severity":          func(s *platformv1.RaiseSignalRequest) { s.Severity = "urgent" },
		"no dedup key":                  func(s *platformv1.RaiseSignalRequest) { s.DedupKey = "" },
		"no title":                      func(s *platformv1.RaiseSignalRequest) { s.Title = "" },
		"no organisation":               func(s *platformv1.RaiseSignalRequest) { s.OrgId = "" },
		"metadata that is not JSON":     func(s *platformv1.RaiseSignalRequest) { s.MetadataJson = "the ropa is missing" },
		"a title that is a paragraph": func(s *platformv1.RaiseSignalRequest) {
			s.Title = string(make([]byte, 300))
		},
	} {
		client, producer := buildWatcherChain(t, a)
		signal := aSignal()
		mutate(signal)

		_, err := client.RaiseSignal(t.Context(), withHeaders(connect.NewRequest(signal), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid_argument", name, got)
		}
		if len(producer.signals) != 0 {
			t.Errorf("%s: reached the database", name)
		}
	}
}
