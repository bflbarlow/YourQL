package models

import (
	"time"
)

// LLMProvider represents a language model provider configured for a workspace.
type LLMProvider struct {
	ID          uint      `json:"id"`
	WorkspaceID uint      `json:"workspace_id"`
	Name        string    `json:"name"`
	Provider    string    `json:"provider"` // openai, anthropic, ollama, local
	APIKey      *string   `json:"-"`        // encrypted, never exposed in JSON
	Model       *string   `json:"model,omitempty"`
	BaseURL     *string   `json:"base_url,omitempty"`
	IsDefault   bool      `json:"is_default"`
	IsActive    bool      `json:"is_active"`
	Config      *string   `json:"config,omitempty"` // JSON string (temperature, max_tokens, etc.)
	CreatedBy   *uint     `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
