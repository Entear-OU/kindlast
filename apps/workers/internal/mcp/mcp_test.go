package mcp_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/workers/internal/mcp"
)

// The protocol client, driven against a real HTTP server rather than a mock
// transport, because what is being tested is what goes on the wire.

// server answers JSON-RPC, recording what it was asked.
type server struct {
	*httptest.Server
	methods []string
	headers []http.Header
}

func newServer(t *testing.T, answer func(method string) (any, *rpcError)) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.methods = append(s.methods, request.Method)
		s.headers = append(s.headers, r.Header.Clone())

		result, rpcErr := answer(request.Method)
		w.Header().Set("Content-Type", "application/json")
		if rpcErr != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": 1,
				"error": map[string]any{"code": rpcErr.code, "message": rpcErr.message},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": result,
		})
	}))
	t.Cleanup(s.Close)
	return s
}

type rpcError struct {
	code    int
	message string
}

func toolsResult() any {
	return map[string]any{
		"tools": []any{
			map[string]any{
				"name":        "search_tickets",
				"description": "Search the helpdesk",
				"annotations": map[string]any{"readOnlyHint": true},
			},
			map[string]any{
				"name":        "close_ticket",
				"description": "Close a ticket",
			},
		},
	}
}

func TestListToolsInitialisesFirstAndReturnsWhatTheServerOffers(t *testing.T) {
	endpoint := newServer(t, func(string) (any, *rpcError) { return toolsResult(), nil })

	tools, err := mcp.New(endpoint.Client(), endpoint.URL, "").ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// `initialize` before `tools/list`, in that order. A server is within its
	// rights to refuse a cold `tools/list`, so the order is part of the
	// contract rather than an implementation detail.
	if want := []string{"initialize", "tools/list"}; !equal(endpoint.methods, want) {
		t.Errorf("methods called: %v, want %v", endpoint.methods, want)
	}

	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	if tools[0].Name != "search_tickets" {
		t.Errorf("first tool is %q", tools[0].Name)
	}
	// The annotations are carried through unread. The decision about what they
	// mean belongs to toolpolicy, beside the refusal it feeds.
	if hint, _ := tools[0].Annotations["readOnlyHint"].(bool); !hint {
		t.Error("the readOnlyHint annotation did not survive the round trip")
	}
	if tools[1].Annotations != nil {
		t.Errorf("an unannotated tool arrived with annotations: %v", tools[1].Annotations)
	}
}

// A credential is sent as a bearer token, and its absence sends no header at
// all rather than an empty one.
func TestTheCredentialTravelsAsABearerToken(t *testing.T) {
	endpoint := newServer(t, func(string) (any, *rpcError) { return toolsResult(), nil })

	if _, err := mcp.New(endpoint.Client(), endpoint.URL, "s3cret").ListTools(t.Context()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if got := endpoint.headers[0].Get("Authorization"); got != "Bearer s3cret" {
		t.Errorf("Authorization was %q", got)
	}

	bare := newServer(t, func(string) (any, *rpcError) { return toolsResult(), nil })
	if _, err := mcp.New(bare.Client(), bare.URL, "").ListTools(t.Context()); err != nil {
		t.Fatalf("ListTools without a credential: %v", err)
	}
	if got := bare.headers[0].Get("Authorization"); got != "" {
		t.Errorf("an empty credential still sent Authorization: %q", got)
	}
}

// The server's own error message is passed through, because it ends up in a
// fetch record the customer reads and their server's wording is more use to
// them than ours.
func TestAServerErrorIsReportedWithItsOwnMessage(t *testing.T) {
	endpoint := newServer(t, func(method string) (any, *rpcError) {
		if method == "initialize" {
			return map[string]any{}, nil
		}
		return nil, &rpcError{code: -32601, message: "tools are disabled on this instance"}
	})

	_, err := mcp.New(endpoint.Client(), endpoint.URL, "").ListTools(t.Context())
	if !errors.Is(err, mcp.ErrEndpoint) {
		t.Fatalf("got %v, want an endpoint error", err)
	}
	if !strings.Contains(err.Error(), "tools are disabled on this instance") {
		t.Errorf("the server's message was lost: %v", err)
	}
}

// An event stream is reported rather than half-read.
//
// The gateway makes one request and stores one answer. A long-lived stream
// from a customer's server into this process is a resource somebody has to
// bound, and there is no use for one here.
func TestAnEventStreamIsRefusedWithAnExplanation(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {}\n\n")
	}))
	defer endpoint.Close()

	_, err := mcp.New(endpoint.Client(), endpoint.URL, "").ListTools(t.Context())
	if !errors.Is(err, mcp.ErrEndpoint) {
		t.Fatalf("got %v, want an endpoint error", err)
	}
	if !strings.Contains(err.Error(), "event stream") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// A response larger than the bound is refused rather than read into memory.
//
// Without this, any customer's endpoint can exhaust this process by answering
// endlessly, which is a denial of service requiring no cleverness at all.
func TestAnEnormousAnswerIsRefused(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 5 MiB of padding inside a valid envelope, over the 4 MiB bound.
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"padding":%q}}`,
			strings.Repeat("x", 5<<20))
	}))
	defer endpoint.Close()

	_, err := mcp.New(endpoint.Client(), endpoint.URL, "").ListTools(t.Context())
	if !errors.Is(err, mcp.ErrEndpoint) {
		t.Fatalf("got %v, want an endpoint error", err)
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("the error does not name the bound: %v", err)
	}
}

func TestCallToolReturnsTheResultAsRawJSON(t *testing.T) {
	endpoint := newServer(t, func(method string) (any, *rpcError) {
		if method == "initialize" {
			return map[string]any{}, nil
		}
		return map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "42 open tickets"}},
		}, nil
	})

	raw, err := mcp.New(endpoint.Client(), endpoint.URL, "").
		CallTool(t.Context(), "search_tickets", map[string]any{"status": "open"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(string(raw), "42 open tickets") {
		t.Errorf("the result did not come back: %s", raw)
	}
	if want := []string{"initialize", "tools/call"}; !equal(endpoint.methods, want) {
		t.Errorf("methods called: %v, want %v", endpoint.methods, want)
	}
}

// A non-2xx status is an error, named by its status so an operator can tell a
// 401 from a 503 without turning on request logging.
func TestANonSuccessStatusIsAnError(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer endpoint.Close()

	_, err := mcp.New(endpoint.Client(), endpoint.URL, "").ListTools(t.Context())
	if !errors.Is(err, mcp.ErrEndpoint) {
		t.Fatalf("got %v, want an endpoint error", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("the status is not in the message: %v", err)
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
