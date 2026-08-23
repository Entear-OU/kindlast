// Package completion serves CompletionService: one chat completion, on the
// model an organisation's choice resolves to, with the key that never leaves
// this process (ENT-256, part five; the §25 hardening).
//
// On the internal chain, on `internal:intelligence`: the caller is the Python
// service, naming an organisation and sending the messages it built. This
// handler resolves the route, opens the key, makes the call on the OpenAI
// chat-completions wire format (which is what llama-server, vLLM and every
// hosted provider the allow-list names all speak), and returns the content
// and what it cost. The prompt is forwarded as data and never read.
package completion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Router is the one question this handler asks: where does this
// organisation's completion go.
type Router interface {
	Resolve(ctx context.Context, orgID string) (modelroute.Route, error)
}

// Service implements platformv1connect.CompletionServiceHandler.
type Service struct {
	router Router
	client *http.Client
}

// DefaultTimeout bounds one completion. A local model on a small machine
// takes minutes for a long answer; the Python harness's own budget is the
// tighter bound and this one only has to not fire first.
const DefaultTimeout = 5 * time.Minute

// New builds the handler. A nil client gets one with DefaultTimeout.
func New(router Router, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Service{router: router, client: client}
}

// Bounds on a request, so a caller bug cannot send a megabyte of prompt or
// ask for a novel.
const (
	maxMessages       = 64
	maxMessageBytes   = 64 << 10
	maxTokensCeiling  = 8192
	maxSchemaBytes    = 32 << 10
	responseBodyLimit = 4 << 20
)

// Complete makes one chat completion.
func (s *Service) Complete(
	ctx context.Context,
	req *connect.Request[platformv1.CompleteRequest],
) (*connect.Response[platformv1.CompleteResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	if err := validate(req.Msg); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	route, err := s.router.Resolve(ctx, req.Msg.GetOrgId())
	if err != nil {
		// FailedPrecondition rather than Internal: nothing broke. This
		// organisation asked for a provider that cannot be honoured, or this
		// deployment runs no model, and the call refusing is the guardrail
		// working. The run fails and says why; nothing falls back.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	body := map[string]any{"messages": messagesOf(req.Msg)}
	if route.Model != "" {
		body["model"] = route.Model
	}
	if req.Msg.GetMaxTokens() > 0 {
		body["max_tokens"] = req.Msg.GetMaxTokens()
	}
	if req.Msg.GetTemperatureSet() {
		body["temperature"] = req.Msg.GetTemperature()
	}
	if schema := req.Msg.GetResponseSchemaJson(); schema != "" {
		var parsed json.RawMessage
		if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("response_schema_json is not JSON: %w", err))
		}
		body["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "output", "schema": parsed},
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(route.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if route.APIKey != "" {
		// THE ONLY PLACE THE KEY IS USED. Set on the outbound request and on
		// nothing else: not on the error returned below, not on a log line,
		// not on the response.
		httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	}

	res, err := s.client.Do(httpReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("the model endpoint did not answer: %w", err))
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, responseBodyLimit))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("reading the model's answer: %w", err))
	}

	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		// The provider refused this organisation's key. Not retried by
		// anybody: the run fails and says so, and the answer is a person
		// fixing the key, not a retry.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("the %s endpoint refused this organisation credential (HTTP %d)", route.Provider, res.StatusCode))
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("the %s endpoint answered HTTP %d", route.Provider, res.StatusCode))
	case res.StatusCode >= 400:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("the %s endpoint rejected the request (HTTP %d): %s", route.Provider, res.StatusCode, excerpt(raw)))
	}

	var payload completionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("the model returned no JSON envelope: %w", err))
	}
	if len(payload.Choices) == 0 {
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("the model returned no choices"))
	}
	choice := payload.Choices[0]
	if choice.Message.ReasoningContent != "" {
		// The Python harness made this check when it dialled the model, for
		// a reason that still holds: the per-run token budget assumes
		// reasoning is off, and a trace that comes back means it is not.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the model returned a reasoning trace, so --reasoning off is not in effect; "+
				"the per-run token budget assumes it is"))
	}

	return connect.NewResponse(&platformv1.CompleteResponse{
		Content:           choice.Message.Content,
		InputTokens:       payload.Usage.PromptTokens,
		CachedInputTokens: payload.Usage.PromptTokensDetails.CachedTokens,
		OutputTokens:      payload.Usage.CompletionTokens,
		FinishReason:      choice.FinishReason,
		Provider:          route.Provider,
		Model:             route.Model,
	}), nil
}

func validate(msg *platformv1.CompleteRequest) error {
	if msg.GetOrgId() == "" {
		return errors.New("org_id is required")
	}
	if len(msg.GetMessages()) == 0 {
		return errors.New("at least one message is required")
	}
	if len(msg.GetMessages()) > maxMessages {
		return fmt.Errorf("at most %d messages", maxMessages)
	}
	for i, m := range msg.GetMessages() {
		switch m.GetRole() {
		case "system", "user", "assistant":
		default:
			return fmt.Errorf("messages[%d].role must be system, user or assistant", i)
		}
		if len(m.GetContent()) > maxMessageBytes {
			return fmt.Errorf("messages[%d] exceeds %d bytes", i, maxMessageBytes)
		}
	}
	if msg.GetMaxTokens() < 0 || msg.GetMaxTokens() > maxTokensCeiling {
		return fmt.Errorf("max_tokens must be between 0 and %d", maxTokensCeiling)
	}
	if len(msg.GetResponseSchemaJson()) > maxSchemaBytes {
		return fmt.Errorf("response_schema_json exceeds %d bytes", maxSchemaBytes)
	}
	return nil
}

func messagesOf(msg *platformv1.CompleteRequest) []map[string]string {
	out := make([]map[string]string, 0, len(msg.GetMessages()))
	for _, m := range msg.GetMessages() {
		out = append(out, map[string]string{"role": m.GetRole(), "content": m.GetContent()})
	}
	return out
}

// completionPayload is the part of the OpenAI chat-completions answer this
// handler reads.
type completionPayload struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int32 `json:"prompt_tokens"`
		CompletionTokens    int32 `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int32 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// excerpt is the first line of a provider's error body, for a message that
// names the problem without pasting a page of HTML into a log.
func excerpt(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
