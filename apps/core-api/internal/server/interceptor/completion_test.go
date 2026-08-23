package interceptor_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	completionservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/completion"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// Completions through core-api (ENT-256, part five), on the real chain with a
// real verifier, against a fake model endpoint that speaks the OpenAI
// chat-completions wire format and records what it was sent.
//
// What is asserted: a human token reaches nothing; a service token's prompt
// arrives at the endpoint as data with the organisation's key on that request
// and nowhere else; the endpoint's answer comes back with its usage; and the
// three error codes the Python harness and the retry policies key on are the
// ones the handler actually returns.

// fakeModel is llama-server, or a hosted provider, minus the model.
type fakeModel struct {
	mu       sync.Mutex
	status   int
	answer   string
	requests []fakeModelRequest
	server   *httptest.Server
}

type fakeModelRequest struct {
	Authorization string
	Body          map[string]any
}

func newFakeModel(t *testing.T, status int, answer string) *fakeModel {
	t.Helper()
	f := &fakeModel{status: status, answer: answer}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.requests = append(f.requests, fakeModelRequest{Authorization: r.Header.Get("Authorization"), Body: body})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		if f.status != http.StatusOK {
			_, _ = w.Write([]byte(`{"error":{"message":"no"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"content": f.answer},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens": 42, "completion_tokens": 7,
				"prompt_tokens_details": map[string]any{"cached_tokens": 10},
			},
		})
	}))
	t.Cleanup(f.server.Close)
	return f
}

// keyedRouter routes every organisation to the fake model, with a key, as a
// chosen provider would.
type keyedRouter struct {
	url string
	key string
}

func (k keyedRouter) Resolve(_ context.Context, _ string) (modelroute.Route, error) {
	return modelroute.Route{Provider: "openai", BaseURL: k.url, Model: "gpt-oss-120b", APIKey: k.key}, nil
}

func buildCompletionChain(t *testing.T, a *authServer, router completionservice.Router) platformv1connect.CompletionServiceClient {
	t.Helper()
	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)
	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)
	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewCompletionServiceHandler(completionservice.New(router, nil), chain))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return platformv1connect.NewCompletionServiceClient(server.Client(), server.URL)
}

func completeRequest() *connect.Request[platformv1.CompleteRequest] {
	return connect.NewRequest(&platformv1.CompleteRequest{
		OrgId: alphaOrg,
		Messages: []*platformv1.ChatMessage{
			{Role: "system", Content: "You explain findings."},
			{Role: "user", Content: "Why does Article 30 apply to a bakery?"},
		},
		MaxTokens: 200,
	})
}

func TestACompletionNeedsTheIntelligenceScope(t *testing.T) {
	a := newAuthServer(t)
	model := newFakeModel(t, http.StatusOK, "Because it processes personal data.")
	client := buildCompletionChain(t, a, keyedRouter{url: model.server.URL, key: "sk-proj-secret"})

	human := sweepHeaders(t, a, humanScopes, "")
	_, err := client.Complete(t.Context(), withHeaders(completeRequest(), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("got %v, want permission_denied", got)
	}
	// And `internal:ingest`, the worker's scope, is not enough either: the
	// prompt is a model call on a customer's data, and only the service that
	// drafts holds the scope for it.
	ingest := sweepHeaders(t, a, "internal:ingest", "")
	_, err = client.Complete(t.Context(), withHeaders(completeRequest(), ingest))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("ingest scope: got %v, want permission_denied", got)
	}
	if len(model.requests) != 0 {
		t.Fatal("the model endpoint was reached by a token without the scope")
	}
}

func TestAServiceTokenCompletesAndTheKeyGoesToTheEndpointOnly(t *testing.T) {
	a := newAuthServer(t)
	model := newFakeModel(t, http.StatusOK, "Because it processes personal data.")
	client := buildCompletionChain(t, a, keyedRouter{url: model.server.URL, key: "sk-proj-secret"})

	res, err := client.Complete(t.Context(), withHeaders(completeRequest(),
		sweepHeaders(t, a, "internal:intelligence", "")))
	if err != nil {
		t.Fatalf("completing: %v", err)
	}
	if res.Msg.GetContent() != "Because it processes personal data." {
		t.Fatalf("content = %q", res.Msg.GetContent())
	}
	if res.Msg.GetInputTokens() != 42 || res.Msg.GetOutputTokens() != 7 || res.Msg.GetCachedInputTokens() != 10 {
		t.Fatalf("usage = %+v, want 42/7/10", res.Msg)
	}
	if res.Msg.GetProvider() != "openai" || res.Msg.GetModel() != "gpt-oss-120b" || res.Msg.GetFinishReason() != "stop" {
		t.Fatalf("provenance = %+v", res.Msg)
	}

	if len(model.requests) != 1 {
		t.Fatalf("the endpoint was called %d times, want 1", len(model.requests))
	}
	sent := model.requests[0]
	// THE KEY IS ON THE REQUEST TO THE ENDPOINT, AND NOWHERE THE CALLER CAN
	// SEE. The response carries content and usage; a caller that could read
	// the key back would be Intelligence obtaining a credential again.
	if sent.Authorization != "Bearer sk-proj-secret" {
		t.Fatalf("the endpoint was called with %q, want the organisation's key", sent.Authorization)
	}
	serialised, _ := json.Marshal(res.Msg)
	if strings.Contains(string(serialised), "sk-proj") {
		t.Fatal("the key reached the response")
	}
	// The prompt arrived as data, in order, with the model name the route
	// names and the caller's bound.
	msgs, _ := sent.Body["messages"].([]any)
	if len(msgs) != 2 || sent.Body["model"] != "gpt-oss-120b" || sent.Body["max_tokens"].(float64) != 200 {
		t.Fatalf("the endpoint was sent %+v", sent.Body)
	}
}

// The codes the Python harness and the retry policies key on.
func TestCompletionErrorCodesAreTheContract(t *testing.T) {
	a := newAuthServer(t)
	service := sweepHeaders(t, a, "internal:intelligence", "")

	t.Run("the provider refusing the key is failed_precondition", func(t *testing.T) {
		model := newFakeModel(t, http.StatusUnauthorized, "")
		client := buildCompletionChain(t, a, keyedRouter{url: model.server.URL, key: "sk-proj-wrong"})
		_, err := client.Complete(t.Context(), withHeaders(completeRequest(), service))
		if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
			t.Fatalf("got %v, want failed_precondition", got)
		}
		if strings.Contains(err.Error(), "sk-proj") {
			t.Fatal("the key reached the error")
		}
	})

	t.Run("the endpoint answering 503 is unavailable", func(t *testing.T) {
		model := newFakeModel(t, http.StatusServiceUnavailable, "")
		client := buildCompletionChain(t, a, keyedRouter{url: model.server.URL})
		_, err := client.Complete(t.Context(), withHeaders(completeRequest(), service))
		if got := codeOf(t, err); got != connect.CodeUnavailable {
			t.Fatalf("got %v, want unavailable", got)
		}
	})

	t.Run("no model anywhere is failed_precondition with a reason", func(t *testing.T) {
		client := buildCompletionChain(t, a, modelroute.New("", ""))
		_, err := client.Complete(t.Context(), withHeaders(completeRequest(), service))
		if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
			t.Fatalf("got %v, want failed_precondition", got)
		}
	})

	t.Run("no messages is invalid_argument and the endpoint is not called", func(t *testing.T) {
		model := newFakeModel(t, http.StatusOK, "x")
		client := buildCompletionChain(t, a, keyedRouter{url: model.server.URL})
		_, err := client.Complete(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.CompleteRequest{OrgId: alphaOrg}), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Fatalf("got %v, want invalid_argument", got)
		}
		if len(model.requests) != 0 {
			t.Fatal("an empty prompt reached the endpoint")
		}
	})
}
