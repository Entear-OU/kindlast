// Package gateway is core-api's client for the workers policy gateway
// (ENT-231, §26.4).
//
// # THE SEAM, AND WHAT CHANGES WHEN TEMPORAL LANDS
//
// This is where a fetch leaves core-api, and at build-order step 8 it is where
// a Temporal activity is started instead. Written down here rather than left
// to be rediscovered, because the shape is deliberately the shape an activity
// takes:
//
//	the deadline    a context timeout set per call, which becomes the
//	                activity's schedule-to-close timeout
//	the retry       none here. The gateway retries its own outbound call with a
//	                cap, and retrying at this layer as well would multiply the
//	                two. The workflow's retry policy replaces the gateway's.
//	idempotency     the caller writes exactly one fetch row per call, after
//	                this returns, whatever the outcome. So a replayed activity
//	                writes a second fetch row and never a second connection or
//	                a second consent, which is the property that makes a replay
//	                merely noisy rather than wrong.
//
// # WHY THE CREDENTIAL TRAVELS PER CALL
//
// The gateway holds nothing at rest, so a fetch carries the endpoint and the
// credential it needs and the gateway forgets both. That is what makes a
// revocation take effect immediately: there is no cached session to
// invalidate, because the next call simply does not arrive.
package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// Tool is what discovery found.
type Tool struct {
	Name         string
	Description  string
	WriteCapable bool
}

// Result is what a call returned.
type Result struct {
	ContentJSON string
	Redactions  int32
	FetchedAt   time.Time
}

// Policy is what a connection permits, sent per call.
type Policy struct {
	Granted      []string
	WriteGranted []string
}

// ErrRefused is what a policy or egress refusal comes back as.
//
// Separated from a transport failure because the two mean different things on
// a fetch record and to a person reading it: `refused` says a control worked,
// `failed` says something broke, and collapsing them would make the log unable
// to answer the question it exists for.
var ErrRefused = errors.New("the gateway refused that call")

// Client talks to the gateway.
type Client struct {
	rpc    platformv1connect.GatewayServiceClient
	secret string
	// timeout bounds one call. Explicit rather than inherited from the
	// request's context alone, because a browser that hangs on to a request
	// forever would otherwise hold a database transaction open for as long as
	// a customer's slow server takes.
	timeout time.Duration
}

// New builds a client, or returns nil when this deployment has no gateway.
//
// NIL IS A SUPPORTED DEPLOYMENT. Without a gateway URL there is no
// IntegrationsService at all, which is better than serving one whose every
// call fails: "this deployment connects nothing" and "the gateway is
// misconfigured" want different reactions from an operator.
func New(baseURL, secret string, timeout time.Duration) *Client {
	if baseURL == "" || secret == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		// A plain client with a timeout above whatever the gateway's own
		// outbound bound is, so the inner deadline is the one that fires and
		// the error a customer sees names the endpoint rather than us.
		rpc:     platformv1connect.NewGatewayServiceClient(&http.Client{Timeout: timeout + 5*time.Second}, baseURL),
		secret:  secret,
		timeout: timeout,
	}
}

// ListTools asks an endpoint what it offers.
//
// The egress allow-list is checked inside the gateway, so an endpoint outside
// it is refused here with nothing having been sent to it. That is why
// discovery goes through the gateway rather than being a check core-api could
// make itself: the allow-list has one home, and the consent screen's data
// comes from the same door every later fetch goes through.
func (c *Client) ListTools(ctx context.Context, orgID, endpoint, credential string) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request := connect.NewRequest(&platformv1.ListToolsRequest{
		Endpoint: &platformv1.Endpoint{Url: endpoint, Credential: credential},
		OrgId:    orgID,
	})
	c.authorise(request.Header())

	response, err := c.rpc.ListTools(ctx, request)
	if err != nil {
		return nil, classify(err)
	}

	tools := make([]Tool, 0, len(response.Msg.GetTools()))
	for _, tool := range response.Msg.GetTools() {
		tools = append(tools, Tool{
			Name:         tool.GetName(),
			Description:  tool.GetDescription(),
			WriteCapable: tool.GetWriteCapable(),
		})
	}
	return tools, nil
}

// CallTool performs one fetch.
func (c *Client) CallTool(
	ctx context.Context,
	orgID, connectionID, endpoint, credential, tool, argumentsJSON string,
	writeCapable bool,
	policy Policy,
) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request := connect.NewRequest(&platformv1.CallToolRequest{
		Endpoint:      &platformv1.Endpoint{Url: endpoint, Credential: credential},
		OrgId:         orgID,
		ConnectionId:  connectionID,
		Tool:          tool,
		ArgumentsJson: argumentsJSON,
		WriteCapable:  writeCapable,
		Policy: &platformv1.ToolPolicy{
			GrantedTools:      policy.Granted,
			WriteGrantedTools: policy.WriteGranted,
		},
	})
	c.authorise(request.Header())

	response, err := c.rpc.CallTool(ctx, request)
	if err != nil {
		return Result{}, classify(err)
	}

	result := Result{
		ContentJSON: response.Msg.GetContentJson(),
		Redactions:  response.Msg.GetRedactions(),
	}
	if stamp := response.Msg.GetFetchedAt(); stamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
			result.FetchedAt = parsed
		}
	}
	if result.FetchedAt.IsZero() {
		// The gateway sends one and this is unreachable in practice. Falling
		// back to now rather than leaving it zero, because `observed_at` is
		// NOT NULL and a zero time would be stored as year one, which reads as
		// data corruption rather than as a missing stamp.
		result.FetchedAt = time.Now().UTC()
	}
	return result, nil
}

func (c *Client) authorise(header http.Header) {
	header.Set("Authorization", "Bearer "+c.secret)
}

// classify turns a Connect error into either a refusal or a failure.
//
// The code rather than the message, because the message is a sentence written
// for a person and matching on one would break the day somebody improves the
// wording. PermissionDenied and ResourceExhausted are both controls working:
// the first is policy or egress, the second is the rate limit, and neither is
// something breaking.
func classify(err error) error {
	switch connect.CodeOf(err) {
	case connect.CodePermissionDenied, connect.CodeResourceExhausted, connect.CodeNotFound:
		return errors.Join(ErrRefused, err)
	default:
		return err
	}
}
