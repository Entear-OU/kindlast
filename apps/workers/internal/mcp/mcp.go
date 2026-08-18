// Package mcp speaks the three Model Context Protocol methods this gateway
// needs, over HTTP, and nothing else (ENT-231).
//
// # WHY THIS IS HAND-WRITTEN AND NOT AN SDK
//
// There is an official Go SDK and using it would normally be the right call:
// AGENTS.md says to add dependencies with the tool rather than to reimplement
// them, and that rule is a good one. This is the exception, and the reason is
// specific rather than a preference for writing code.
//
// The control this package exists to preserve is that the gateway decides
// where a packet may go. That control lives in an `http.Client` built by the
// egress package, with a dialer that inspects the resolved address and a
// redirect policy that refuses. An SDK owns its own transport by design, and
// handing it ours means depending on it never bypassing what it was given,
// across every future version, for a property whose failure is silent.
//
// The surface actually needed is `initialize`, `tools/list` and `tools/call`:
// three JSON-RPC calls with no session resumption, no server-sent events, no
// sampling and no notifications, because the gateway makes one request and
// stores what came back. That is a small enough thing to own outright, and
// owning it means the dialer cannot be routed around.
//
// # WHAT IS DELIBERATELY NOT IMPLEMENTED
//
// The streaming half of the Streamable HTTP transport. A server may answer
// with `text/event-stream`, and this client asks for JSON and treats a stream
// as an error it can explain. The gateway wants one answer to one question;
// a long-lived event stream from a customer's server into this process is a
// resource somebody has to bound, and there is no use for it here yet.
//
// Everything the protocol offers a server for driving the client: sampling,
// roots, elicitation. Those exist so a server can ask the model to do
// something, which is precisely the direction this design refuses to open.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ProtocolVersion is what this client declares at `initialize`.
//
// Pinned, per AGENTS.md's rule that nothing is fetched at runtime and every
// version is a decision somebody made. A server that requires a newer version
// says so in its response and the error names both, which is a better failure
// than negotiating silently into a shape this client does not implement.
const ProtocolVersion = "2025-06-18"

// Tool is one tool as a server described it.
type Tool struct {
	Name        string
	Description string
	// Annotations as the server sent them, unread by this package. The
	// toolpolicy package decides what they mean, so that the reading of
	// "does this write" lives in one place with the refusal it feeds.
	Annotations map[string]any
}

// Client talks to one endpoint.
//
// The http.Client is supplied rather than built, and that is the whole point:
// it comes from the egress package with its dialer and redirect policy
// already in place. A Client constructed with http.DefaultClient would work
// perfectly and enforce nothing, which is why nothing in this repository
// constructs one that way.
type Client struct {
	http     *http.Client
	endpoint string
	// credential is a bearer token, or empty for an endpoint that wants none.
	credential string
}

func New(httpClient *http.Client, endpoint, credential string) *Client {
	return &Client{http: httpClient, endpoint: endpoint, credential: credential}
}

// ListTools initialises the session and returns what the server offers.
//
// Two round trips, because MCP requires `initialize` before anything else and
// a server is within its rights to refuse a `tools/list` that arrives cold.
// Both are inside the caller's context, so the caller's deadline bounds the
// pair rather than each half.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}

	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Annotations map[string]any `json:"annotations"`
		} `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, t := range result.Tools {
		tools = append(tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			Annotations: t.Annotations,
		})
	}
	return tools, nil
}

// CallTool invokes one tool and returns its result as JSON.
//
// The result is returned as raw JSON rather than a typed structure, because it
// is on its way to becoming a stored observation. Typing it here would invite
// code that branches on a third party's content, which is the thing the whole
// design is arranged to prevent: this is data, and the only thing that happens
// to it is redaction and storage.
func (c *Client) CallTool(ctx context.Context, tool string, arguments map[string]any) (json.RawMessage, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	if arguments == nil {
		arguments = map[string]any{}
	}

	var raw json.RawMessage
	err := c.call(ctx, "tools/call", map[string]any{
		"name":      tool,
		"arguments": arguments,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// initialize performs the handshake.
//
// The response is not kept. A fuller client would remember the server's
// capabilities and skip calls the server said it does not support; this one
// makes exactly one kind of call and would rather hear the server's own
// refusal than act on a capability list it cached.
func (c *Client) initialize(ctx context.Context) error {
	var discarded json.RawMessage
	return c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		// No capabilities. This client offers the server nothing: no sampling,
		// no roots, no elicitation. A server that wanted to drive the model
		// through us finds nothing to drive.
		"capabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    "kindlast-gateway",
			"version": "1",
		},
	}, &discarded)
}

// jsonrpcError is the error object a server returns inside a 200.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ErrEndpoint is what every failure to get a usable answer wraps.
//
// A single sentinel because the caller's next move is the same for all of
// them: record the fetch as failed with this message. The distinction the
// caller does care about, refused by policy versus failed at the endpoint,
// is a different error from a different package.
var ErrEndpoint = errors.New("the endpoint did not answer usefully")

// call performs one JSON-RPC request.
//
// # THE RESPONSE SIZE IS BOUNDED, AND IT HAS TO BE
//
// The body comes from a server this deployment does not control. Reading it
// with io.ReadAll and no limit means any customer's endpoint can exhaust this
// process's memory by answering slowly and endlessly, which is a denial of
// service that requires no cleverness at all.
func (c *Client) call(ctx context.Context, method string, params any, into any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		// A constant id rather than a counter. Each call here is a fresh
		// request-response pair with nothing in flight beside it, so an id
		// exists to satisfy the protocol rather than to correlate anything.
		"id":     1,
		"method": method,
		"params": params,
	})
	if err != nil {
		return fmt.Errorf("%w: could not encode the request: %w", ErrEndpoint, err)
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrEndpoint, err)
	}
	request.Header.Set("Content-Type", "application/json")
	// JSON only. A server that can only answer with an event stream is one
	// this client reports rather than one it silently half-reads.
	request.Header.Set("Accept", "application/json")
	request.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if c.credential != "" {
		request.Header.Set("Authorization", "Bearer "+c.credential)
	}

	response, err := c.http.Do(request)
	if err != nil {
		// Includes every egress refusal, because those surface as transport
		// errors from the dialer. Wrapped rather than replaced, so a caller
		// checking errors.Is for the egress sentinel still finds it.
		return fmt.Errorf("%w: %w", ErrEndpoint, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, response.Body); _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: it answered %s", ErrEndpoint, response.Status)
	}
	if contentType := response.Header.Get("Content-Type"); strings.Contains(contentType, "text/event-stream") {
		return fmt.Errorf(
			"%w: it answered with an event stream, and this gateway makes single request-response calls",
			ErrEndpoint)
	}

	const maxResponse = 4 << 20 // 4 MiB
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrEndpoint, err)
	}
	if len(body) > maxResponse {
		return fmt.Errorf("%w: its answer was larger than %d bytes", ErrEndpoint, maxResponse)
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *jsonrpcError   `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%w: its answer was not JSON-RPC", ErrEndpoint)
	}
	if envelope.Error != nil {
		// The server's own message, passed through. It ends up in a fetch
		// record the customer reads, and their server's wording is more use to
		// them than ours would be.
		return fmt.Errorf("%w: %s (code %d)", ErrEndpoint, envelope.Error.Message, envelope.Error.Code)
	}
	if len(envelope.Result) == 0 {
		return fmt.Errorf("%w: it answered with neither a result nor an error", ErrEndpoint)
	}
	if err := json.Unmarshal(envelope.Result, into); err != nil {
		return fmt.Errorf("%w: its result was not the shape %s returns", ErrEndpoint, method)
	}
	return nil
}
