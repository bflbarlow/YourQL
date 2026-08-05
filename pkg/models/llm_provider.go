package models

import (
	"time"
)

// LLMProvider represents a language model provider configuration.
type LLMProvider struct {
	ID             uint      `json:"id"`
	Name           string    `json:"name"`
	Provider       string    `json:"provider"` // openai, anthropic, ollama
	APIKey         *string   `json:"-"`
	Model          *string   `json:"model,omitempty"`
	BaseURL        *string   `json:"base_url,omitempty"`
	IsDefault      bool      `json:"is_default"`
	IsActive       bool      `json:"is_active"`
	MaxTokens      *int      `json:"max_tokens,omitempty"`
	ModelMaxTokens *int      `json:"model_max_tokens,omitempty"`
	ContextWindow  *int      `json:"context_window,omitempty"`
	Config         *string   `json:"config,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
