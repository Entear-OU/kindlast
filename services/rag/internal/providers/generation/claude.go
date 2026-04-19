package generation

import (
	"context"
	"errors"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/entear/kindlast/services/rag/internal/providers"
)

// ClaudeProvider implements the GenerationProvider interface for Anthropic Claude
type ClaudeProvider struct {
	client *anthropic.Client
	model  anthropic.Model
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(apiKey string, model string) (*ClaudeProvider, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic API key is required")
	}

	// Use latest Claude Sonnet 4.6 (released Feb 17, 2026)
	// Note: Always use versioned model strings in production
	modelEnum := anthropic.Model("claude-sonnet-4-6")
	if model != "" {
		modelEnum = anthropic.Model(model)
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &ClaudeProvider{
		client: &client,
		model:  modelEnum,
	}, nil
}

// Name returns the provider name
func (p *ClaudeProvider) Name() string {
	return "claude"
}

// Generate generates a non-streaming response
func (p *ClaudeProvider) Generate(ctx context.Context, req providers.GenerationRequest) (*providers.GenerationResponse, error) {
	if len(req.Messages) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "messages are required",
		}
	}

	// Convert messages to Claude format
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue // System messages are handled separately
		}
		if msg.Role == "user" {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		} else if msg.Role == "assistant" {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}

	// Set default values
	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	temperature := req.Temperature
	if temperature == 0 {
		temperature = 1.0
	}

	// Build request parameters
	params := anthropic.MessageNewParams{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: anthropic.Float(temperature),
	}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{
				Type: "text",
				Text: req.SystemPrompt,
			},
		}
	}

	// Make API call
	message, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to generate response",
			Err:      err,
		}
	}

	// Extract content
	var content string
	if len(message.Content) > 0 {
		contentBlock := message.Content[0]
		if contentBlock.Type == "text" {
			content = contentBlock.Text
		}
	}

	return &providers.GenerationResponse{
		Content:      content,
		FinishReason: string(message.StopReason),
		Usage: providers.TokenUsage{
			InputTokens:  int(message.Usage.InputTokens),
			OutputTokens: int(message.Usage.OutputTokens),
		},
	}, nil
}

// Stream generates a streaming response
func (p *ClaudeProvider) Stream(ctx context.Context, req providers.GenerationRequest) (<-chan providers.StreamChunk, error) {
	if len(req.Messages) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "messages are required",
		}
	}

	// Convert messages to Claude format
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue // System messages are handled separately
		}
		if msg.Role == "user" {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		} else if msg.Role == "assistant" {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}

	// Set default values
	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	temperature := req.Temperature
	if temperature == 0 {
		temperature = 1.0
	}

	// Build request parameters
	params := anthropic.MessageNewParams{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: anthropic.Float(temperature),
	}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{
				Type: "text",
				Text: req.SystemPrompt,
			},
		}
	}

	// Create streaming response
	stream := p.client.Messages.NewStreaming(ctx, params)

	// Create output channel
	chunks := make(chan providers.StreamChunk)

	// Process stream in goroutine
	go func() {
		defer close(chunks)

		for stream.Next() {
			event := stream.Current()

			// Handle content block delta events
			if event.Type == "content_block_delta" {
				if event.Delta.Type == "text_delta" {
					chunks <- providers.StreamChunk{
						Content: event.Delta.Text,
					}
				}
			}

			// Handle message delta events
			if event.Type == "message_delta" {
				if event.Delta.StopReason != "" {
					chunks <- providers.StreamChunk{
						FinishReason: string(event.Delta.StopReason),
					}
				}
			}
		}

		// Check for errors
		if err := stream.Err(); err != nil {
			chunks <- providers.StreamChunk{
				Error: &providers.ProviderError{
					Provider: p.Name(),
					Message:  "streaming error",
					Err:      err,
				},
			}
		}
	}()

	return chunks, nil
}
