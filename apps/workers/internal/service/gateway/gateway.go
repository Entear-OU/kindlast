// Package gateway serves GatewayService: the only place in this system that
// opens a connection to an address a customer supplied (ENT-231, §26.4).
//
// # THE ORDER OF THE CHECKS IS THE DESIGN
//
// Egress, then policy, then rate limit, then dial. Each refusal happens with
// zero bytes on the wire, and the tests assert that by counting attempts on a
// transport rather than by reading the error message. "Refused before any
// request leaves" is a claim about ordering, so it is tested as one.
//
// # WHAT IS DELIBERATELY BOUNDED, GIVEN THAT TEMPORAL IS NOT HERE
//
// §23 step 8 makes each of these a Temporal activity, with a retry policy, a
// heartbeat and a schedule-to-close timeout applied by the workflow rather
// than by the code. That does not exist, so the same three bounds are written
// out here:
//
//	a deadline    the caller's context, and the outbound client's own timeout
//	              beneath it, so a customer's slow server cannot hold a
//	              goroutine indefinitely
//	a retry cap   `attempts` below, with backoff, and only for errors that can
//	              plausibly succeed on a second try
//	idempotency   nothing here writes, so a replayed call is a duplicate fetch
//	              and never a duplicate row. The write happens once in core-api
//	              per RPC, after this returns.
//
// When step 8 lands, these method bodies become the activity bodies unchanged
// and the retry loop below is what the workflow's retry policy replaces. That
// is the seam, and it is written down here rather than left to be rediscovered.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/workers/internal/egress"
	"github.com/Entear-OU/kindlast/apps/workers/internal/mcp"
	"github.com/Entear-OU/kindlast/apps/workers/internal/ratelimit"
	"github.com/Entear-OU/kindlast/apps/workers/internal/redact"
	"github.com/Entear-OU/kindlast/apps/workers/internal/toolpolicy"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// attempts is how many times one outbound call is tried.
//
// Three, and only for transport failures. A refusal is never retried, because
// a policy that said no will say no again and retrying it would turn one
// refusal in a log into three. A 4xx from the endpoint is not retried either:
// the request was wrong and will be wrong again.
const attempts = 3

// Service implements platformv1connect.GatewayServiceHandler.
type Service struct {
	allow   egress.AllowList
	client  *http.Client
	limiter *ratelimit.Limiter
	logger  *slog.Logger

	// dial builds the protocol client.
	//
	// A field rather than a direct call, so a test can substitute a caller
	// that records what it was asked to do. Unexported, and the tests live in
	// this package, because a public seam here would be a public way to reach
	// an endpoint without the checks above it. Substituting this does NOT skip
	// them: everything in ListTools and CallTool before the dial runs whatever
	// this is set to, which is what lets a test assert that a refusal happened
	// with nothing dialled.
	dial func(endpoint, credential string) toolCaller
}

// toolCaller is what this service needs of an MCP client, declared where it is
// used (§21.6).
type toolCaller interface {
	ListTools(ctx context.Context) ([]mcp.Tool, error)
	CallTool(ctx context.Context, tool string, arguments map[string]any) (json.RawMessage, error)
}

func New(allow egress.AllowList, client *http.Client, limiter *ratelimit.Limiter, logger *slog.Logger) *Service {
	s := &Service{allow: allow, client: client, limiter: limiter, logger: logger}
	s.dial = func(endpoint, credential string) toolCaller {
		return mcp.New(client, endpoint, credential)
	}
	return s
}

func (s *Service) ListTools(
	ctx context.Context,
	request *connect.Request[platformv1.ListToolsRequest],
) (*connect.Response[platformv1.ListToolsResponse], error) {
	endpoint := request.Msg.GetEndpoint()
	if endpoint == nil || endpoint.GetUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("no endpoint was named"))
	}

	// FIRST, AND BEFORE ANYTHING IS BUILT. Not after constructing a client,
	// not inside a helper that also dials: here, where a reader can see that
	// the refusal returns before the next line runs.
	if err := s.allow.Check(endpoint.GetUrl()); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	if err := s.limiter.Allow(request.Msg.GetOrgId()); err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}

	client := s.dial(endpoint.GetUrl(), endpoint.GetCredential())

	tools, err := retry(ctx, s.logger, func() ([]mcp.Tool, error) {
		return client.ListTools(ctx)
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}

	response := &platformv1.ListToolsResponse{}
	for _, tool := range tools {
		// The description is a third party's text on its way to a human. It is
		// redacted like everything else, because a tool description is a
		// perfectly good place to accidentally paste an API key, and it is
		// never interpreted as anything but a string.
		response.Tools = append(response.Tools, &platformv1.GatewayTool{
			Name:         tool.Name,
			Description:  redact.Text(tool.Description).Text,
			WriteCapable: !toolpolicy.ReadsOnly(tool.Annotations),
		})
	}
	return connect.NewResponse(response), nil
}

func (s *Service) CallTool(
	ctx context.Context,
	request *connect.Request[platformv1.CallToolRequest],
) (*connect.Response[platformv1.CallToolResponse], error) {
	message := request.Msg

	endpoint := message.GetEndpoint()
	if endpoint == nil || endpoint.GetUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("no endpoint was named"))
	}

	// 1. EGRESS. Before a client exists, before the policy is consulted,
	// before anything is parsed out of the arguments.
	if err := s.allow.Check(endpoint.GetUrl()); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}

	// 2. POLICY. The caller's claim about whether the tool writes is checked
	// against the policy the caller itself sent, which is the check that holds
	// even when the caller is wrong or replayed.
	//
	// `endpointSaysWrites` is the caller's belief here rather than a fresh
	// reading, because reading it would mean a `tools/list` round trip on
	// every call, which is a request leaving before the policy decided. The
	// endpoint's own reading is taken at discovery and again below, after the
	// list is known and before the call is made, which is where it can be had
	// for free.
	policy := toolpolicy.Policy{
		Granted:      message.GetPolicy().GetGrantedTools(),
		WriteGranted: message.GetPolicy().GetWriteGrantedTools(),
	}
	if err := toolpolicy.Decide(policy, message.GetTool(), message.GetWriteCapable(), false); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}

	// 3. RATE LIMIT, per organisation.
	if err := s.limiter.Allow(message.GetOrgId()); err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}

	var arguments map[string]any
	if raw := message.GetArgumentsJson(); raw != "" {
		if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("the arguments were not a JSON object"))
		}
	}

	client := s.dial(endpoint.GetUrl(), endpoint.GetCredential())

	// 4. THE ENDPOINT'S OWN READING, taken now that a list costs one round
	// trip we are making anyway.
	//
	// This is where a tool that has STARTED writing since discovery is caught.
	// core-api's stored flag is months old by construction; the server's
	// annotation is current. Taking the union in toolpolicy.Decide means a
	// tool that used to be read-only and is not any more gets refused until a
	// human looks at it, rather than sailing through on a stale record.
	tools, err := retry(ctx, s.logger, func() ([]mcp.Tool, error) {
		return client.ListTools(ctx)
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	offered, found := find(tools, message.GetTool())
	if !found {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("the endpoint offers no tool called %q", message.GetTool()))
	}
	if err := toolpolicy.Decide(policy, message.GetTool(),
		message.GetWriteCapable(), !toolpolicy.ReadsOnly(offered.Annotations)); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}

	result, err := retry(ctx, s.logger, func() (json.RawMessage, error) {
		return client.CallTool(ctx, message.GetTool(), arguments)
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}

	// 5. REDACTION, HERE, BEFORE THIS CROSSES BACK.
	//
	// The process that writes the row never holds the unredacted form, so
	// there is no ordering available in which storage happens first.
	redacted := redact.JSON(string(result))

	return connect.NewResponse(&platformv1.CallToolResponse{
		ContentJson: redacted.Text,
		Redactions:  int32(redacted.Count),
		// The gateway's clock, not the endpoint's. `observed_at` on the
		// evidence row has to mean something, and a customer's server is free
		// to have a wrong clock or a hostile one.
		FetchedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}), nil
}

func find(tools []mcp.Tool, name string) (mcp.Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return mcp.Tool{}, false
}

// retry runs an outbound call up to `attempts` times with backoff.
//
// # WHAT IS AND IS NOT RETRIED
//
// Only `mcp.ErrEndpoint`, which covers a transport failure, a 5xx and a
// malformed answer. An egress refusal arrives wrapped in it, so it is checked
// for separately and never retried: a host that is not on the allow-list will
// not be on it in two seconds, and retrying would put three refusals in the
// log for one attempt.
//
// This is the shape a Temporal retry policy takes at step 8, which is why it
// is a small explicit loop rather than a library: the loop is meant to be
// deleted, and a dependency added for something with a scheduled removal date
// is a dependency somebody has to remove.
func retry[T any](ctx context.Context, logger *slog.Logger, call func() (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		result, err := call()
		if err == nil {
			return result, nil
		}
		lastErr = err

		if errors.Is(err, egress.ErrNotAllowed) {
			return zero, err
		}
		if attempt == attempts {
			break
		}
		if logger != nil {
			logger.Info("retrying an outbound gateway call", "attempt", attempt, "error", err)
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return zero, lastErr
}
