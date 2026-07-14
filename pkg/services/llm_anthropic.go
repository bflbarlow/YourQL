package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"YourQL/pkg/models"
)

// AnthropicClient implements LLMClient for Anthropic's Claude API.
type AnthropicClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// anthropicChatRequest is the request payload for Anthropic's messages API.
type anthropicChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []anthropicChatMessage `json:"messages"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature *float64               `json:"temperature,omitempty"`
	TopP        *float64               `json:"top_p,omitempty"`
	System      string                 `json:"system,omitempty"`
}

type anthropicChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicChatResponse is the response from Anthropic's messages API.
type anthropicChatResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// NewAnthropicClient creates a new Anthropic client from a provider configuration.
func NewAnthropicClient(provider *models.LLMProvider) (LLMClient, error) {
	if provider.APIKey == nil || *provider.APIKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required")
	}

	model := "claude-3-haiku-20240307"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}

	baseURL := "https://api.anthropic.com"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}

	return &AnthropicClient{
		baseURL:    baseURL,
		apiKey:     *provider.APIKey,
		model:      model,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// ChatCompletion sends a conversation to Anthropic's API and returns the assistant's reply.
func (c *AnthropicClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	startTime := time.Now()

	// Extract system message if present
	var systemContent string
	filteredMessages := make([]anthropicChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			systemContent = msg.Content
			continue
		}
		filteredMessages = append(filteredMessages, anthropicChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	if len(filteredMessages) == 0 {
		return "", fmt.Errorf("no user/assistant messages provided")
	}

	reqBody := anthropicChatRequest{
		Model:       c.model,
		Messages:    filteredMessages,
		MaxTokens:   2000,
		Temperature: floatPtr(0.1),
	}
	if systemContent != "" {
		reqBody.System = systemContent
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		logAnthropicIntegration(c.model, "outgoing", c.baseURL+"/v1/messages", startTime, string(jsonData), "", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logAnthropicIntegration(c.model, "outgoing", c.baseURL+"/v1/messages", startTime, string(jsonData), "", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logAnthropicIntegration(c.model, "incoming", c.baseURL+"/v1/messages", startTime, string(jsonData), string(body), fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		return "", fmt.Errorf("Anthropic API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response anthropicChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Content) == 0 {
		// Try to extract error info from the response body
		var errorInfo struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if unmarshalErr := json.Unmarshal(body, &errorInfo); unmarshalErr == nil && errorInfo.Error.Message != "" {
			return "", fmt.Errorf("API error: %s (type: %s). Please check your API key and model in Settings → Model Configurations.", errorInfo.Error.Message, errorInfo.Error.Type)
		}
		return "", fmt.Errorf("API returned empty response (no content). Please check your API key and model in Settings → Model Configurations.")
	}

	// Anthropic can return multiple content blocks; concatenate them
	var reply string
	for _, block := range response.Content {
		if block.Type == "text" {
			reply += block.Text
		}
	}

	if reply == "" {
		return "", fmt.Errorf("no text content in response")
	}

	// Log successful integration
	logAnthropicIntegration(c.model, "incoming", c.baseURL+"/v1/messages", startTime, string(jsonData), reply, nil)

	return reply, nil
}

// ChatCompletionWithPayload sends a conversation and returns the full request/response payloads.
func (c *AnthropicClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	// Extract system message if present
	var systemContent string
	filteredMessages := make([]anthropicChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			systemContent = msg.Content
			continue
		}
		filteredMessages = append(filteredMessages, anthropicChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	if len(filteredMessages) == 0 {
		return "", "", "", fmt.Errorf("no user/assistant messages provided")
	}

	reqBody := anthropicChatRequest{
		Model:       c.model,
		Messages:    filteredMessages,
		MaxTokens:   2000,
		Temperature: floatPtr(0.1),
	}
	if systemContent != "" {
		reqBody.System = systemContent
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response anthropicChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Content) == 0 {
		return "", "", "", fmt.Errorf("API returned empty response (no content)")
	}

	// Anthropic can return multiple content blocks; concatenate them
	var reply string
	for _, block := range response.Content {
		if block.Type == "text" {
			reply += block.Text
		}
	}

	if reply == "" {
		return "", "", "", fmt.Errorf("no text content in response")
	}

	content = reply
	requestJSON = string(jsonData)
	responseJSON = string(body)
	return content, requestJSON, responseJSON, nil
}

// TestAnthropicConnection tests if the Anthropic API key is valid.
func TestAnthropicConnection(apiKey, model, baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	if model == "" {
		model = "claude-3-haiku-20240307"
	}

	reqBody := map[string]interface{}{
		"model":     model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API error: status %d: %s", resp.StatusCode, string(body))
	}

	return "Anthropic Claude API connection successful", nil
}

// floatPtr returns a pointer to the given float64 value.
func floatPtr(f float64) *float64 {
	return &f
}

// logAnthropicIntegration logs Anthropic integration events to the database.
func logAnthropicIntegration(model string, direction string, endpoint string, startTime time.Time, request string, response string, err error) {
	latencyMs := int(time.Since(startTime).Milliseconds())

	entry := NewLLMLog().
		WithModel(model).
		WithEndpoint(endpoint).
		WithDirection(direction).
		WithLatency(latencyMs)

	if err != nil {
		entry.WithStatus("error").
			WithErrorMessage(err.Error())
	} else {
		entry.WithStatus("success")
	}

	if request != "" {
		entry.WithFullRequest(request)
	}
	if response != "" {
		entry.WithFullResponse(response)
	}

	GlobalLog(entry)
}
