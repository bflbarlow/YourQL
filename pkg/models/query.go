package models

import (
	"time"
)

// Query represents a natural language to SQL query execution within a workspace.
type Query struct {
	ID              uint        `json:"id"`
	WorkspaceID     uint        `json:"workspace_id"`
	UserID          uint        `json:"user_id"`
	ConversationID  *uint       `json:"conversation_id,omitempty"`
	Question        string      `json:"question"`
	GeneratedSQL    *string     `json:"generated_sql,omitempty"`
	DBConnectionID  *uint       `json:"db_connection_id,omitempty"`
	LLMProviderID   *uint       `json:"llm_provider_id,omitempty"`
	Status          string      `json:"status"` // pending, running, success, error
	ResultSummary   *string     `json:"result_summary,omitempty"`
	ErrorMessage    *string     `json:"error_message,omitempty"`
	ExecutionTimeMS *int        `json:"execution_time_ms,omitempty"`
	TokensUsed      *int        `json:"tokens_used,omitempty"`
	CostEstimate    *string     `json:"cost_estimate,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}
