package models

import (
	"time"
)

// IntegrationLog captures everything sent to and from all integrations.
// This is database-only logging for debugging, auditing, and monitoring.
// No UI or API exposure.
type IntegrationLog struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	WorkspaceID      *uint     `json:"workspace_id"`
	DiscussionID     *uint     `json:"discussion_id"`
	MessageID        *uint     `json:"message_id"`
	UserID           *uint     `json:"user_id"`
	IntegrationType  string    `gorm:"size:50;default:llm" json:"integration_type"`  // llm, db, discussion
	Direction        string    `gorm:"size:10;default:outgoing" json:"direction"`    // outgoing, incoming
	Provider         *string   `gorm:"size:50" json:"provider"`                       // openai, anthropic, ollama, mysql, etc.
	Model            *string   `gorm:"size:100" json:"model"`                         // gpt-3.5-turbo, etc.
	Endpoint         *string   `gorm:"size:255" json:"endpoint"`                      // API endpoint or DB table
	Status           string    `gorm:"size:20;default:success" json:"status"`         // success, error
	RequestSummary   *string   `gorm:"size:2000" json:"request_summary"`              // truncated summary
	ResponseSummary  *string   `gorm:"size:2000" json:"response_summary"`             // truncated summary
	FullRequest      *string   `gorm:"type:mediumtext" json:"full_request"`           // full payload
	FullResponse     *string   `gorm:"type:mediumtext" json:"full_response"`          // full response
	ErrorMessage     *string   `gorm:"type:text" json:"error_message"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	Metadata         *string   `gorm:"type:text" json:"metadata"` // JSON metadata
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (IntegrationLog) TableName() string {
	return "integration_logs"
}
