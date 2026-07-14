package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"YourQL/pkg/models"
)

// MockClient implements LLMClient for testing without real API calls.
type MockClient struct {
	responses []string // pre‑defined responses (cycled)
	index     int
}

// NewMockClient creates a mock LLM client.
func NewMockClient(provider *models.LLMProvider) (LLMClient, error) {
	return &MockClient{
		responses: []string{
			`{"action": "sql_query", "sql_query": "SELECT COUNT(*) FROM users", "explanation": "Counting total users."}`,
			`{"action": "clarification", "clarification_question": "Which time period are you interested in?", "explanation": "Need date range."}`,
		},
	}, nil
}

// ChatCompletion returns a mock response.
func (c *MockClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	// Determine if user is asking for a count
	userMessage := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			userMessage = messages[i].Content
			break
		}
	}

	// Simple heuristic: if question contains "count", generate a SQL count query
	if strings.Contains(strings.ToLower(userMessage), "count") {
		return `{"action": "sql_query", "sql_query": "SELECT COUNT(*) FROM users", "explanation": "Counting rows."}`, nil
	}
	if strings.Contains(strings.ToLower(userMessage), "how many") {
		return `{"action": "sql_query", "sql_query": "SELECT COUNT(*) FROM users", "explanation": "Counting rows."}`, nil
	}
	if strings.Contains(strings.ToLower(userMessage), "list") || strings.Contains(strings.ToLower(userMessage), "show") {
		return `{"action": "sql_query", "sql_query": "SELECT * FROM users LIMIT 10", "explanation": "Listing sample rows."}`, nil
	}

	// Default: ask for clarification
	return `{"action": "clarification", "clarification_question": "Could you please clarify what you are looking for?", "explanation": "General clarification."}`, nil
}

// ChatCompletionWithPayload returns a mock response with full request/response payloads.
func (c *MockClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	content, err = c.ChatCompletion(ctx, messages)
	if err != nil {
		return "", "", "", err
	}
	
	// Build mock request JSON
	req := map[string]interface{}{
		"model":    "mock-model",
		"messages": messages,
		"provider": "mock",
	}
	reqJSON, _ := json.Marshal(req)
	
	// Build mock response JSON
	resp := map[string]interface{}{
		"id":      "mock-response-1",
		"model":   "mock-model",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	respJSON, _ := json.Marshal(resp)
	
	return content, string(reqJSON), string(respJSON), nil
}

// ValidateMockResponse validates that the mock response is valid JSON.
func ValidateMockResponse(response string) error {
	var resp LLMResponse
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		return fmt.Errorf("invalid mock response JSON: %w", err)
	}
	if resp.Action != "sql_query" && resp.Action != "clarification" {
		return fmt.Errorf("invalid action: %s", resp.Action)
	}
	return nil
}