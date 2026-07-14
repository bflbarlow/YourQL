package models

import (
	"time"
)

// Conversation represents a chat thread within a workspace.
type Conversation struct {
	ID              uint       `json:"id"`
	WorkspaceID     uint       `json:"workspace_id"`
	UserID          uint       `json:"user_id"`
	Title           *string    `json:"title,omitempty"`
	LLMProviderID   *uint      `json:"llm_provider_id,omitempty"`
	DBConnectionID  *uint      `json:"db_connection_id,omitempty"`
	Status          string     `json:"status"` // active, archived, deleted
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
	TechDetails     bool       `json:"tech_details"`
}
