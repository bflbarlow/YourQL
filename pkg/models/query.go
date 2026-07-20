package models

import (
	"time"
)

// Query represents a natural language to SQL query execution.
// This table is used for query tracking but has no UI reading path.
// All useful data is already stored in conversation_messages.
type Query struct {
	ID              uint      `json:"id"`
	ConversationID  *uint     `json:"conversation_id,omitempty"`
	Question        string    `json:"question"`
	GeneratedSQL    *string   `json:"generated_sql,omitempty"`
	DataSourceID   *uint     `json:"data_source_id,omitempty"`
	LLMProviderID   *uint     `json:"llm_provider_id,omitempty"`
	Status          string    `json:"status"` // pending, running, success, error
	ResultSummary   *string   `json:"result_summary,omitempty"`
	ErrorMessage    *string   `json:"error_message,omitempty"`
	ExecutionTimeMS *int      `json:"execution_time_ms,omitempty"`
	TokensUsed      *int      `json:"tokens_used,omitempty"`
	CostEstimate    *string   `json:"cost_estimate,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
