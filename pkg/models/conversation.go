package models

import (
	"time"
)

// Conversation represents a chat thread.
type Conversation struct {
	ID             uint       `json:"id"`
	Title          *string    `json:"title,omitempty"`
	LLMProviderID  *uint      `json:"llm_provider_id,omitempty"`
	DBConnectionID *uint      `json:"db_connection_id,omitempty"`
	Status         string     `json:"status"` // active, archived, deleted
	MaxMessages      int        `json:"max_messages"`
	MaxContextMessages int      `json:"max_context_messages"`
	Pinned           bool       `json:"pinned"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	TechDetails    bool       `json:"tech_details"`
	ContextDetails bool       `json:"context_details"`
}
