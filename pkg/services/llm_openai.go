package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// OpenAIClient implements LLMClient for OpenAI's API.
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAIClient creates a new OpenAI client from a provider configuration.
func NewOpenAIClient(provider *models.LLMProvider) (LLMClient, error) {
	model := "gpt-3.5-turbo"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}

	baseURL := "https://api.openai.com/v1"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}

	// For local endpoints, API key is optional
	localEndpoint := baseURL != "https://api.openai.com/v1"
	var apiKey string
	if provider.APIKey != nil {
		apiKey = *provider.APIKey
	}
	if !localEndpoint && apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required for https://api.openai.com/v1")
	}

	return &OpenAIClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 300 * time.Second},
	}, nil
}

// openAIChatRequest is the request payload for OpenAI's chat completion API.
type openAIChatRequest struct {
	Model    string                  `json:"model"`
	Messages []openAIChatMessage     `json:"messages"`
	Stream   bool                    `json:"stream,omitempty"`
	MaxTokens int                    `json:"max_tokens,omitempty"`
	Temperature float64              `json:"temperature,omitempty"`
	TopP       float64              `json:"top_p,omitempty"`
	FrequencyPenalty float64         `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64         `json:"presence_penalty,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatResponse is the response from OpenAI's chat completion API.
type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int                 `json:"index"`
		Message      openAIChatMessage   `json:"message"`
		FinishReason string              `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ChatCompletion sends a conversation to OpenAI's API and returns the assistant's reply.
func (c *OpenAIClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	startTime := time.Now()

	// Convert messages to OpenAI format
	openAIMessages := make([]openAIChatMessage, len(messages))
	for i, msg := range messages {
		openAIMessages[i] = openAIChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	reqBody := openAIChatRequest{
		Model:    c.model,
		Messages: openAIMessages,
		Temperature: 0.1,
		MaxTokens: 2000,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Diagnostic: log the full outgoing request
	log.Printf("[OpenAI] Sending chat completion request to %s/chat/completions:\n%s", c.baseURL, string(jsonData))

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logOpenAIIntegration(c.model, "outgoing", c.baseURL+"/chat/completions", startTime, string(jsonData), "", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logOpenAIIntegration(c.model, "incoming", c.baseURL+"/chat/completions", startTime, string(jsonData), string(body), fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		return "", fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response openAIChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 {
		// Try to extract error info from the response body
		var errorInfo struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if unmarshalErr := json.Unmarshal(body, &errorInfo); unmarshalErr == nil && errorInfo.Error.Message != "" {
			// Check if this is a path/endpoint error
			if strings.Contains(strings.ToLower(errorInfo.Error.Message), "endpoint") ||
				strings.Contains(strings.ToLower(errorInfo.Error.Message), "unexpected") {
				return "", fmt.Errorf("API endpoint error: %s. For OpenAI-compatible local APIs, the base URL should point to the server root (e.g., http://localhost:1234 or http://192.168.0.176:1234), NOT include /v1. The code appends /chat/completions automatically.", errorInfo.Error.Message)
			}
			return "", fmt.Errorf("API error: %s (type: %s, code: %s). Please check your API key and model in Settings → Model Configurations.", errorInfo.Error.Message, errorInfo.Error.Type, errorInfo.Error.Code)
		}
		// Return raw response for diagnostic purposes
		rawBody := string(body)
		if len(rawBody) > 500 {
			rawBody = rawBody[:500] + "..."
		}
		return "", fmt.Errorf("API returned empty response (no choices). This usually means the local API returned a different format than expected. Raw response: %s. Please check your local API endpoint in Settings → Model Configurations.", rawBody)
	}

	// Log successful integration
	logOpenAIIntegration(c.model, "incoming", c.baseURL+"/chat/completions", startTime, string(jsonData), string(body), nil)

	return response.Choices[0].Message.Content, nil
}

// ChatCompletionWithPayload sends a conversation and returns the full request/response payloads.
func (c *OpenAIClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	// Convert messages to OpenAI format
	openAIMessages := make([]openAIChatMessage, len(messages))
	for i, msg := range messages {
		openAIMessages[i] = openAIChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	reqBody := openAIChatRequest{
		Model:    c.model,
		Messages: openAIMessages,
		Temperature: 0.1,
		MaxTokens: 2000,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

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

	var response openAIChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", "", "", fmt.Errorf("API returned empty response (no choices)")
	}

	content = response.Choices[0].Message.Content
	requestJSON = string(jsonData)
	responseJSON = string(body)
	return content, requestJSON, responseJSON, nil
}

// logOpenAIIntegration logs OpenAI integration events to the database.
// This is database-only logging - no UI or API exposure.
func logOpenAIIntegration(model string, direction string, endpoint string, startTime time.Time, request string, response string, err error) {
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

	// Add request/response summaries
	if request != "" {
		entry.WithFullRequest(request)
	}
	if response != "" {
		entry.WithFullResponse(response)
	}

	// Log to database
	GlobalLog(entry)
}