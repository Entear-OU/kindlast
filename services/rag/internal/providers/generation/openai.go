package generation

import (
	"context"
	"io"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/entear/kindlast/services/rag/internal/providers"
)

// OpenAIProvider implements the GenerationProvider interface for OpenAI
type OpenAIProvider struct {
	client *openai.Client
	model  openai.ChatModel
}

// NewOpenAIProvider creates a new OpenAI provider
// For local models (LMstudio), pass baseURL like "http://localhost:1234/v1"
// apiKey can be empty for local models
func NewOpenAIProvider(apiKey string, model string, baseURL string) (*OpenAIProvider, error) {
	modelEnum := openai.ChatModelGPT4o
	if model != "" {
		modelEnum = openai.ChatModel(model)
	}

	// Build client options
	opts := []option.RequestOption{}

	// API key is optional for local models (LMstudio)
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	} else {
		// LMstudio doesn't need auth, but the SDK requires *something*
		opts = append(opts, option.WithAPIKey("not-needed"))
	}

	// Override base URL for local models
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAIProvider{
		client: &client,
		model:  modelEnum,
	}, nil
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Generate generates a non-streaming response
func (p *OpenAIProvider) Generate(ctx context.Context, req providers.GenerationRequest) (*providers.GenerationResponse, error) {
	if len(req.Messages) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "messages are required",
		}
	}

	// Convert messages to OpenAI format
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append(messages, openai.SystemMessage(req.SystemPrompt))
	}

	// Add conversation messages
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, openai.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(msg.Content))
		case "system":
			messages = append(messages, openai.SystemMessage(msg.Content))
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
	params := openai.ChatCompletionNewParams{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   openai.Int(maxTokens),
		Temperature: openai.Float(temperature),
	}

	// Make API call
	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to generate response",
			Err:      err,
		}
	}

	// Extract content
	var content string
	var finishReason string
	if len(completion.Choices) > 0 {
		content = completion.Choices[0].Message.Content
		finishReason = string(completion.Choices[0].FinishReason)
	}

	return &providers.GenerationResponse{
		Content:      content,
		FinishReason: finishReason,
		Usage: providers.TokenUsage{
			InputTokens:  int(completion.Usage.PromptTokens),
			OutputTokens: int(completion.Usage.CompletionTokens),
		},
	}, nil
}

// Stream generates a streaming response
func (p *OpenAIProvider) Stream(ctx context.Context, req providers.GenerationRequest) (<-chan providers.StreamChunk, error) {
	if len(req.Messages) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "messages are required",
		}
	}

	// Convert messages to OpenAI format
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append(messages, openai.SystemMessage(req.SystemPrompt))
	}

	// Add conversation messages
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, openai.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(msg.Content))
		case "system":
			messages = append(messages, openai.SystemMessage(msg.Content))
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
	params := openai.ChatCompletionNewParams{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   openai.Int(maxTokens),
		Temperature: openai.Float(temperature),
	}

	// Create streaming response
	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	// Create output channel
	chunks := make(chan providers.StreamChunk)

	// Process stream in goroutine
	go func() {
		defer close(chunks)

		for stream.Next() {
			chunk := stream.Current()

			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]

				// Send content delta
				if choice.Delta.Content != "" {
					chunks <- providers.StreamChunk{
						Content: choice.Delta.Content,
					}
				}

				// Send finish reason if present
				if choice.FinishReason != "" {
					chunks <- providers.StreamChunk{
						FinishReason: string(choice.FinishReason),
					}
				}
			}
		}

		// Check for errors
		if err := stream.Err(); err != nil {
			// Don't send EOF errors as they're expected
			if err != io.EOF {
				chunks <- providers.StreamChunk{
					Error: &providers.ProviderError{
						Provider: p.Name(),
						Message:  "streaming error",
						Err:      err,
					},
				}
			}
		}
	}()

	return chunks, nil
}
