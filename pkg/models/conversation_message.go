package models

import (
	"time"
)

// ConversationMessage represents a single message in a conversation thread.
type ConversationMessage struct {
	ID             uint      `json:"id"`
	ConversationID uint      `json:"conversation_id"`
	Role           string    `json:"role"` // user, assistant, system, exploration
	Content        string    `json:"content"`
	LLMContent     *string   `json:"llm_content,omitempty"`
	SQLResults     *string   `json:"sql_results,omitempty"`
	Metadata       *string   `json:"metadata,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
