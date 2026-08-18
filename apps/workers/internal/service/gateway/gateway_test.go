package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/workers/internal/egress"
	"github.com/Entear-OU/kindlast/apps/workers/internal/mcp"
	"github.com/Entear-OU/kindlast/apps/workers/internal/ratelimit"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The gateway's two headline properties, asserted end to end through the
// handler (ENT-231 acceptance criteria).
//
// # WHY THESE TESTS COUNT DIALS RATHER THAN READING ERROR MESSAGES
//
// Both criteria are claims about ORDERING: "unreachable unless granted" and
// "refused before any request leaves". A test asserting only that an error
// came back would pass just as happily if the gateway dialled the customer's
// server, got an answer, and then decided it should not have. So the fake
// caller below counts what it was asked to do, and the assertions are about a
// counter that must still read zero.
//
// The tests are inside the package rather than beside it, because the seam
// they substitute is unexported on purpose: a public way to replace the dialer
// would be a public way to reach an endpoint without the checks above it.

// countingCaller records what it was asked to do and answers successfully.
//
// Answering successfully matters: a fake that failed would make every
// assertion below pass for the wrong reason.
type countingCaller struct {
	lists atomic.Int64
	calls atomic.Int64
	tools []mcp.Tool
}

func (c *countingCaller) ListTools(context.Context) ([]mcp.Tool, error) {
	c.lists.Add(1)
	return c.tools, nil
}

func (c *countingCaller) CallTool(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	c.calls.Add(1)
	return json.RawMessage(`{"content":[{"type":"text","text":"ada@example.com opened 3 tickets"}]}`), nil
}

func (c *countingCaller) total() int64 { return c.lists.Load() + c.calls.Load() }

func serviceWith(t *testing.T, caller *countingCaller, hosts string) *Service {
	t.Helper()
	allow := egress.Parse(hosts, false)
	service := New(allow, allow.Client(time.Second), ratelimit.New(100, time.Minute), nil)
	service.dial = func(string, string) toolCaller { return caller }
	return service
}

func helpdeskTools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "search_tickets", Description: "Search", Annotations: map[string]any{"readOnlyHint": true}},
		{Name: "close_ticket", Description: "Close a ticket"},
	}
}

func callRequest(endpoint, tool string, granted, writeGranted []string, writeCapable bool) *connect.Request[platformv1.CallToolRequest] {
	return connect.NewRequest(&platformv1.CallToolRequest{
		Endpoint:      &platformv1.Endpoint{Url: endpoint},
		OrgId:         "org-1",
		ConnectionId:  "conn-1",
		Tool:          tool,
		ArgumentsJson: `{"status":"open"}`,
		WriteCapable:  writeCapable,
		Policy: &platformv1.ToolPolicy{
			GrantedTools:      granted,
			WriteGrantedTools: writeGranted,
		},
	})
}

// ---------------------------------------------------------------------------
// A customer-supplied endpoint outside the egress allow-list is refused before
// any request leaves.
// ---------------------------------------------------------------------------

func TestAnEndpointOutsideTheAllowListIsRefusedBeforeAnythingLeaves(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	_, err := service.CallTool(t.Context(),
		callRequest("https://evil.example.net/mcp", "search_tickets",
			[]string{"search_tickets"}, nil, false))

	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("got %v (%v), want PermissionDenied", err, connect.CodeOf(err))
	}
	if got := caller.total(); got != 0 {
		t.Fatalf("%d outbound operations happened before the refusal; want 0", got)
	}

	// And on the discovery path, which is where a customer first types a URL
	// and therefore where this refusal is most often seen.
	_, err = service.ListTools(t.Context(), connect.NewRequest(&platformv1.ListToolsRequest{
		Endpoint: &platformv1.Endpoint{Url: "https://evil.example.net/mcp"},
		OrgId:    "org-1",
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("ListTools: got %v (%v), want PermissionDenied", err, connect.CodeOf(err))
	}
	if got := caller.total(); got != 0 {
		t.Fatalf("%d outbound operations happened before the discovery refusal; want 0", got)
	}
}

// The guard is only worth having if it can fail.
//
// The same call with the host on the allow-list must go through, and the
// counter must move. Without this, an egress check that refused everything
// would keep the test above green while making the gateway useless.
func TestTheEgressRefusalCanActuallyFail(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	response, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "search_tickets",
			[]string{"search_tickets"}, nil, false))
	if err != nil {
		t.Fatalf("a permitted host was refused: %v", err)
	}
	if caller.calls.Load() == 0 {
		t.Fatal("the permitted call never reached the endpoint; the counter proves nothing")
	}
	if response.Msg.GetContentJson() == "" {
		t.Fatal("nothing came back from a permitted call")
	}
}

// ---------------------------------------------------------------------------
// A connection's write-capable tools are unreachable unless explicitly granted.
// ---------------------------------------------------------------------------

func TestAWriteCapableToolIsUnreachableWithoutAnExplicitGrant(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	// `close_ticket` is granted. It is not WRITE-granted. Everything else
	// about this request is correct, which is what makes it the right test:
	// the only thing standing between the caller and a write in somebody's
	// helpdesk is the second list.
	_, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "close_ticket",
			[]string{"search_tickets", "close_ticket"}, nil, true))

	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("got %v (%v), want PermissionDenied", err, connect.CodeOf(err))
	}
	if got := caller.total(); got != 0 {
		t.Fatalf("%d outbound operations happened before the refusal; want 0", got)
	}
	if !strings.Contains(err.Error(), "write access") {
		t.Errorf("the refusal does not say why, and a customer reads this: %v", err)
	}
}

// The guard is only worth having if it can fail. Naming the tool in the
// write-granted list must make the same call go through.
func TestTheWriteRefusalCanActuallyFail(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	_, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "close_ticket",
			[]string{"search_tickets", "close_ticket"}, []string{"close_ticket"}, true))
	if err != nil {
		t.Fatalf("an explicitly write-granted tool was refused: %v", err)
	}
	if caller.calls.Load() != 1 {
		t.Fatalf("the granted write did not reach the endpoint (%d calls)", caller.calls.Load())
	}
}

// A tool the connection never granted is refused, and refused before the
// endpoint is asked whether it exists.
func TestAnUngrantedToolIsRefusedBeforeAnythingLeaves(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	_, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "close_ticket",
			[]string{"search_tickets"}, nil, false))

	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("got %v (%v), want PermissionDenied", err, connect.CodeOf(err))
	}
	if got := caller.total(); got != 0 {
		t.Fatalf("%d outbound operations happened before the refusal; want 0", got)
	}
}

// A tool that has STARTED writing since discovery is caught by the endpoint's
// own annotation, even though the caller's stored flag says read-only.
//
// This is the case a stale database record produces, and it is the reason the
// gateway takes the union of the two readings rather than trusting the caller.
func TestAToolThatHasStartedWritingSinceDiscoveryIsRefused(t *testing.T) {
	caller := &countingCaller{tools: []mcp.Tool{
		// No readOnlyHint any more, so the gateway reads it as writing.
		{Name: "search_tickets", Description: "Search, and quietly log the query"},
	}}
	service := serviceWith(t, caller, "tools.example.com")

	// The caller still believes it is read-only, which is what its months-old
	// discovery recorded, so it sends no write grant.
	_, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "search_tickets",
			[]string{"search_tickets"}, nil, false))

	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("got %v (%v), want PermissionDenied", err, connect.CodeOf(err))
	}
	// The list happened, because that is where the fresh reading comes from.
	// The CALL did not, which is the thing that matters.
	if caller.calls.Load() != 0 {
		t.Fatalf("the tool was called anyway (%d times)", caller.calls.Load())
	}
}

// A tool the endpoint does not offer at all is not called, whatever the policy
// says about it.
func TestAToolTheEndpointDoesNotOfferIsNotCalled(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	_, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "delete_everything",
			[]string{"delete_everything"}, []string{"delete_everything"}, true))

	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("got %v (%v), want NotFound", err, connect.CodeOf(err))
	}
	if caller.calls.Load() != 0 {
		t.Fatalf("it was called anyway (%d times)", caller.calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Redaction happens here, before anything crosses back to the process that
// stores it.
// ---------------------------------------------------------------------------

func TestWhatComesBackIsAlreadyRedacted(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	response, err := service.CallTool(t.Context(),
		callRequest("https://tools.example.com/mcp", "search_tickets",
			[]string{"search_tickets"}, nil, false))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if strings.Contains(response.Msg.GetContentJson(), "ada@example.com") {
		t.Errorf("an address left the gateway unredacted: %s", response.Msg.GetContentJson())
	}
	if response.Msg.GetRedactions() != 1 {
		t.Errorf("Redactions is %d, want 1", response.Msg.GetRedactions())
	}
	if response.Msg.GetFetchedAt() == "" {
		t.Error("no fetched_at; the evidence row would have nothing to stamp itself with")
	}
	if _, err := time.Parse(time.RFC3339Nano, response.Msg.GetFetchedAt()); err != nil {
		t.Errorf("fetched_at is not RFC 3339: %v", err)
	}
}

// Tool descriptions are redacted too, because a description is a perfectly
// good place to paste an API key by accident and it is on its way to a screen.
func TestToolDescriptionsAreRedactedOnTheWayToTheConsentScreen(t *testing.T) {
	caller := &countingCaller{tools: []mcp.Tool{
		{Name: "sync", Description: "Sync. Ask ops@example.com for access."},
	}}
	service := serviceWith(t, caller, "tools.example.com")

	response, err := service.ListTools(t.Context(), connect.NewRequest(&platformv1.ListToolsRequest{
		Endpoint: &platformv1.Endpoint{Url: "https://tools.example.com/mcp"},
		OrgId:    "org-1",
	}))
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(response.Msg.GetTools()) != 1 {
		t.Fatalf("got %d tools", len(response.Msg.GetTools()))
	}
	if strings.Contains(response.Msg.GetTools()[0].GetDescription(), "ops@example.com") {
		t.Errorf("an address survived in a description: %q", response.Msg.GetTools()[0].GetDescription())
	}
	// And an unannotated tool comes back marked as writing, which is what puts
	// the extra tick in front of the customer.
	if !response.Msg.GetTools()[0].GetWriteCapable() {
		t.Error("an unannotated tool was reported as read-only")
	}
}

// ---------------------------------------------------------------------------
// Bounds, standing in for what Temporal will provide at step 8.
// ---------------------------------------------------------------------------

// The per-organisation rate limit refuses rather than queueing, and the
// refusal costs nothing outbound.
func TestTheRateLimitRefusesWithoutDialling(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	allow := egress.Parse("tools.example.com", false)
	service := New(allow, allow.Client(time.Second), ratelimit.New(1, time.Minute), nil)
	service.dial = func(string, string) toolCaller { return caller }

	request := callRequest("https://tools.example.com/mcp", "search_tickets",
		[]string{"search_tickets"}, nil, false)

	if _, err := service.CallTool(t.Context(), request); err != nil {
		t.Fatalf("the first call: %v", err)
	}
	before := caller.total()

	_, err := service.CallTool(t.Context(), request)
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("got %v (%v), want ResourceExhausted", err, connect.CodeOf(err))
	}
	if caller.total() != before {
		t.Fatalf("the limited call still reached the endpoint (%d then %d)", before, caller.total())
	}
}

// A retry is capped, and an egress refusal is never retried.
//
// Retrying a refusal would put three identical entries in a log for one
// attempt, and would make a customer's fetch record read as though the gateway
// kept trying to do something it had already decided not to do.
func TestAnEgressRefusalIsNotRetried(t *testing.T) {
	var tries atomic.Int64

	// Wrapped, because that is how it arrives in practice: the dialer's
	// refusal comes back through the HTTP client and out of the protocol
	// client inside an mcp.ErrEndpoint.
	_, err := retry(t.Context(), nil, func() (int, error) {
		tries.Add(1)
		return 0, fmt.Errorf("%w: %w", mcp.ErrEndpoint, egress.ErrNotAllowed)
	})
	if !errors.Is(err, egress.ErrNotAllowed) {
		t.Fatalf("got %v, want the egress sentinel to survive wrapping", err)
	}
	if got := tries.Load(); got != 1 {
		t.Fatalf("an egress refusal was tried %d times, want 1", got)
	}
}

// A transport failure is retried up to the cap and then reported, which is the
// bound standing in for a Temporal retry policy.
func TestATransportFailureIsRetriedUpToTheCap(t *testing.T) {
	var tries atomic.Int64

	_, err := retry(t.Context(), nil, func() (int, error) {
		tries.Add(1)
		return 0, mcp.ErrEndpoint
	})
	if !errors.Is(err, mcp.ErrEndpoint) {
		t.Fatalf("got %v, want the endpoint error", err)
	}
	if got := tries.Load(); got != attempts {
		t.Fatalf("tried %d times, want %d", got, attempts)
	}
}

// A request naming no endpoint is refused before anything else is decided,
// because "where" is the first question and a nil endpoint has no answer.
func TestARequestWithNoEndpointIsRefused(t *testing.T) {
	caller := &countingCaller{tools: helpdeskTools()}
	service := serviceWith(t, caller, "tools.example.com")

	_, err := service.CallTool(t.Context(), connect.NewRequest(&platformv1.CallToolRequest{
		OrgId: "org-1", Tool: "search_tickets",
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v (%v), want InvalidArgument", err, connect.CodeOf(err))
	}
	if caller.total() != 0 {
		t.Fatal("something was dialled for a request that named nowhere")
	}
}
