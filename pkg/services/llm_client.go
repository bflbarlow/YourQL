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

// effectiveMaxTokens returns the max tokens to use for a provider, clamped
// by the detected model max and context window if available.
func effectiveMaxTokens(provider *models.LLMProvider) int {
	configured := 2000
	if provider.MaxTokens != nil && *provider.MaxTokens > 0 {
		configured = *provider.MaxTokens
	}
	// Clamp to context window if known
	if provider.ContextWindow != nil && *provider.ContextWindow > 0 {
		if configured > *provider.ContextWindow {
			configured = *provider.ContextWindow
		}
	}
	// Also clamp to detected model max if available
	if provider.ModelMaxTokens != nil && *provider.ModelMaxTokens > 0 {
		if configured > *provider.ModelMaxTokens {
			configured = *provider.ModelMaxTokens
		}
	}
	return configured
}
