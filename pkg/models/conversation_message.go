package models

import (
	"time"
)

// ConversationMessage represents a single message in a conversation thread.
type ConversationMessage struct {
	ID             uint      `json:"id"`
	ConversationID uint      `json:"conversation_id"`
	Role           string    `json:"role"` // user, assistant, system, exploration, exploration_error, exploration_hint
	Content        string    `json:"content"`
	LLMContent     *string   `json:"llm_content,omitempty"` // LLM-friendly version (plain text or JSON), optional
	SQLResults     *string   `json:"sql_results,omitempty"` // JSON-serialized QueryResult from a previously executed SQL query
	Metadata       *string   `json:"metadata,omitempty"` // JSON string (token usage, model used, etc.)
	CreatedAt      time.Time `json:"created_at"`
}
