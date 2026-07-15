package services

import (
	"context"
	"fmt"
	"strings"

	"YourQL/pkg/models"
)

type LLMClient interface {
	ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
	ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
}

type ChatMessage struct {
	Role    string
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
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider.Provider)
	}
}

func toOpenAIMessages(messages []ChatMessage) []map[string]string {
	result := make([]map[string]string, len(messages))
	for i, msg := range messages {
		result[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	return result
}

func removeTrailingSlash(s string) string {
	return strings.TrimSuffix(s, "/")
}
