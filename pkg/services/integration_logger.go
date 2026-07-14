package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"YourQL/pkg/models"

	"gorm.io/gorm"
)

// IntegrationLogger provides database-only logging for all integrations.
// No UI or API exposure - logs are written directly to the database.
type IntegrationLogger struct {
	db *gorm.DB
}

// NewIntegrationLogger creates a new integration logger.
func NewIntegrationLogger(db *gorm.DB) *IntegrationLogger {
	return &IntegrationLogger{db: db}
}

// LogIntegration writes an integration log entry to the database.
// This is a fire-and-forget operation - errors are logged to stderr but don't propagate.
func (l *IntegrationLogger) LogIntegration(ctx context.Context, entry *IntegrationLogEntry) {
	// Convert to model
	modelEntry := &models.IntegrationLog{
		WorkspaceID:     entry.WorkspaceID,
		DiscussionID:    entry.DiscussionID,
		MessageID:       entry.MessageID,
		UserID:          entry.UserID,
		IntegrationType: entry.IntegrationType,
		Direction:       entry.Direction,
		Status:          entry.Status,
		PromptTokens:    entry.PromptTokens,
		CompletionTokens: entry.CompletionTokens,
		TotalTokens:     entry.TotalTokens,
		LatencyMs:       entry.LatencyMs,
	}

	// Set optional fields
	if entry.Provider != nil {
		modelEntry.Provider = stringPtr(*entry.Provider)
	}
	if entry.Model != nil {
		modelEntry.Model = stringPtr(*entry.Model)
	}
	if entry.Endpoint != nil {
		modelEntry.Endpoint = stringPtr(*entry.Endpoint)
	}

	// Summaries (truncated)
	if entry.RequestSummary != "" {
		modelEntry.RequestSummary = stringPtr(truncateString(entry.RequestSummary, 2000))
	}
	if entry.ResponseSummary != "" {
		modelEntry.ResponseSummary = stringPtr(truncateString(entry.ResponseSummary, 2000))
	}

	// Full payloads
	if entry.FullRequest != "" {
		modelEntry.FullRequest = stringPtr(entry.FullRequest)
	}
	if entry.FullResponse != "" {
		modelEntry.FullResponse = stringPtr(entry.FullResponse)
	}
	if entry.ErrorMessage != "" {
		modelEntry.ErrorMessage = stringPtr(entry.ErrorMessage)
	}

	// Metadata
	if len(entry.Metadata) > 0 {
		metaJSON, err := json.Marshal(entry.Metadata)
		if err == nil {
			modelEntry.Metadata = stringPtr(string(metaJSON))
		}
	}

	// Write to database (async, fire-and-forget)
	go func() {
		if err := l.db.Create(modelEntry).Error; err != nil {
			fmt.Printf("[IntegrationLogger] Failed to write log: %v\n", err)
		}
	}()
}

// IntegrationLogEntry is the builder for integration log entries.
type IntegrationLogEntry struct {
	WorkspaceID     *uint
	DiscussionID    *uint
	MessageID       *uint
	UserID          *uint
	IntegrationType string // llm, db, discussion
	Direction       string // outgoing, incoming
	Provider        *string
	Model           *string
	Endpoint        *string
	Status          string
	RequestSummary  string
	ResponseSummary string
	FullRequest     string
	FullResponse    string
	ErrorMessage    string
	PromptTokens    int
	CompletionTokens int
	TotalTokens     int
	LatencyMs       int
	Metadata        map[string]interface{}
}

// NewLLMLog creates a new LLM integration log entry.
func NewLLMLog() *IntegrationLogEntry {
	return &IntegrationLogEntry{
		IntegrationType: "llm",
		Status:          "success",
	}
}

// NewDBLog creates a new database integration log entry.
func NewDBLog() *IntegrationLogEntry {
	return &IntegrationLogEntry{
		IntegrationType: "db",
		Status:          "success",
	}
}

// NewDiscussionLog creates a new discussion engine log entry.
func NewDiscussionLog() *IntegrationLogEntry {
	return &IntegrationLogEntry{
		IntegrationType: "discussion",
		Status:          "success",
	}
}

// WithWorkspaceID sets the workspace ID.
func (e *IntegrationLogEntry) WithWorkspaceID(id uint) *IntegrationLogEntry {
	e.WorkspaceID = &id
	return e
}

// WithDiscussionID sets the discussion ID.
func (e *IntegrationLogEntry) WithDiscussionID(id uint) *IntegrationLogEntry {
	e.DiscussionID = &id
	return e
}

// WithMessageID sets the message ID.
func (e *IntegrationLogEntry) WithMessageID(id uint) *IntegrationLogEntry {
	e.MessageID = &id
	return e
}

// WithUserID sets the user ID.
func (e *IntegrationLogEntry) WithUserID(id uint) *IntegrationLogEntry {
	e.UserID = &id
	return e
}

// WithProvider sets the provider.
func (e *IntegrationLogEntry) WithProvider(p string) *IntegrationLogEntry {
	e.Provider = &p
	return e
}

// WithModel sets the model.
func (e *IntegrationLogEntry) WithModel(m string) *IntegrationLogEntry {
	e.Model = &m
	return e
}

// WithEndpoint sets the endpoint.
func (e *IntegrationLogEntry) WithEndpoint(ep string) *IntegrationLogEntry {
	e.Endpoint = &ep
	return e
}

// WithDirection sets the direction.
func (e *IntegrationLogEntry) WithDirection(d string) *IntegrationLogEntry {
	e.Direction = d
	return e
}

// WithStatus sets the status.
func (e *IntegrationLogEntry) WithStatus(s string) *IntegrationLogEntry {
	e.Status = s
	return e
}

// WithRequestSummary sets the request summary.
func (e *IntegrationLogEntry) WithRequestSummary(s string) *IntegrationLogEntry {
	e.RequestSummary = s
	return e
}

// WithResponseSummary sets the response summary.
func (e *IntegrationLogEntry) WithResponseSummary(s string) *IntegrationLogEntry {
	e.ResponseSummary = s
	return e
}

// WithFullRequest sets the full request payload.
func (e *IntegrationLogEntry) WithFullRequest(r string) *IntegrationLogEntry {
	e.FullRequest = r
	return e
}

// WithFullResponse sets the full response payload.
func (e *IntegrationLogEntry) WithFullResponse(r string) *IntegrationLogEntry {
	e.FullResponse = r
	return e
}

// WithErrorMessage sets the error message.
func (e *IntegrationLogEntry) WithErrorMessage(msg string) *IntegrationLogEntry {
	e.ErrorMessage = msg
	e.Status = "error"
	return e
}

// WithTokens sets the token counts.
func (e *IntegrationLogEntry) WithTokens(prompt, completion, total int) *IntegrationLogEntry {
	e.PromptTokens = prompt
	e.CompletionTokens = completion
	e.TotalTokens = total
	return e
}

// WithLatency sets the latency in milliseconds.
func (e *IntegrationLogEntry) WithLatency(ms int) *IntegrationLogEntry {
	e.LatencyMs = ms
	return e
}

// WithMetadata sets additional metadata.
func (e *IntegrationLogEntry) WithMetadata(m map[string]interface{}) *IntegrationLogEntry {
	e.Metadata = m
	return e
}

// Log writes the entry to the database.
func (e *IntegrationLogEntry) Log(logger *IntegrationLogger) {
	logger.LogIntegration(context.Background(), e)
}

// Helper functions

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetIntegrationLogger returns the global integration logger instance.
// This is initialized in main.go or wherever the database is set up.
var globalIntegrationLogger *IntegrationLogger

// SetGlobalIntegrationLogger sets the global integration logger.
func SetGlobalIntegrationLogger(logger *IntegrationLogger) {
	globalIntegrationLogger = logger
}

// GlobalLog is a convenience function to log using the global logger.
func GlobalLog(entry *IntegrationLogEntry) {
	if globalIntegrationLogger != nil {
		entry.Log(globalIntegrationLogger)
	}
}

// BuildLLMRequestSummary creates a summary of LLM messages for logging.
func BuildLLMRequestSummary(messages []ChatMessage) string {
	var parts []string
	for i, msg := range messages {
		role := msg.Role
		content := truncateString(msg.Content, 200)
		parts = append(parts, fmt.Sprintf("[%s] %s", role, content))
		if i >= 4 { // Only show first 5 messages
			parts = append(parts, fmt.Sprintf("... (%d more messages)", len(messages)-5))
			break
		}
	}
	return strings.Join(parts, " | ")
}

// BuildLLMResponseSummary creates a summary of LLM response for logging.
func BuildLLMResponseSummary(response string) string {
	return truncateString(strings.TrimSpace(response), 500)
}
