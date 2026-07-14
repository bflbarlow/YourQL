package services

import (
	"context"
	"fmt"
	"YourQL/pkg/models"
)

// LLMClient defines the interface for interacting with language models.
type LLMClient interface {
	// ChatCompletion sends a conversation to the LLM and returns the response.
	ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
	
	// ChatCompletionWithPayload sends a conversation and returns the full request/response payloads.
	ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// NewLLMClient creates an LLM client based on the provider configuration.
func NewLLMClient(provider *models.LLMProvider) (LLMClient, error) {
	switch provider.Provider {
	case "openai":
		return NewOpenAIClient(provider)
	case "anthropic":
		return NewAnthropicClient(provider)
	case "ollama":
		return NewOllamaClient(provider)
	case "local":
		return NewLocalClient(provider)
	case "mock":
		return NewMockClient(provider)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider.Provider)
	}
}