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

// OllamaClient implements LLMClient for local Ollama API.
type OllamaClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// ollamaChatRequest is the request payload for Ollama's chat API.
type ollamaChatRequest struct {
	Model     string             `json:"model"`
	Messages  []ollamaChatMessage `json:"messages"`
	Stream    bool               `json:"stream,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatResponse is the streaming response from Ollama's chat API.
type ollamaChatResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// NewOllamaClient creates a new Ollama client from a provider configuration.
func NewOllamaClient(provider *models.LLMProvider) (LLMClient, error) {
	model := "llama2"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}

	baseURL := "http://localhost:11434"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}

	// Ensure baseURL doesn't have trailing slash
	baseURL = removeTrailingSlash(baseURL)

	return &OllamaClient{
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}, nil
}

// ChatCompletion sends a conversation to Ollama's API and returns the assistant's reply.
func (c *OllamaClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	startTime := time.Now()

	ollamaMessages := make([]ollamaChatMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollamaChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	reqBody := ollamaChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		logOllamaIntegration(c.model, "outgoing", c.baseURL+"/api/chat", startTime, string(jsonData), "", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logOllamaIntegration(c.model, "outgoing", c.baseURL+"/api/chat", startTime, string(jsonData), "", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logOllamaIntegration(c.model, "incoming", c.baseURL+"/api/chat", startTime, string(jsonData), string(body), fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		return "", fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response ollamaChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Message.Content == "" {
		// Try to extract error info from the response body
		var errorInfo struct {
			Error string `json:"error"`
		}
		if unmarshalErr := json.Unmarshal(body, &errorInfo); unmarshalErr == nil && errorInfo.Error != "" {
			return "", fmt.Errorf("Ollama API error: %s. Make sure Ollama is running and the model is pulled.", errorInfo.Error)
		}
		return "", fmt.Errorf("empty response from Ollama. Make sure Ollama is running and the model '%s' is pulled (run: ollama pull %s).", c.model, c.model)
	}

	// Log successful integration
	logOllamaIntegration(c.model, "incoming", c.baseURL+"/api/chat", startTime, string(jsonData), response.Message.Content, nil)

	return response.Message.Content, nil
}

// ChatCompletionWithPayload sends a conversation and returns the full request/response payloads.
func (c *OllamaClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	ollamaMessages := make([]ollamaChatMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollamaChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	reqBody := ollamaChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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

	var response ollamaChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Message.Content == "" {
		var errorInfo struct {
			Error string `json:"error"`
		}
		if unmarshalErr := json.Unmarshal(body, &errorInfo); unmarshalErr == nil && errorInfo.Error != "" {
			return "", "", "", fmt.Errorf("Ollama API error: %s", errorInfo.Error)
		}
		return "", "", "", fmt.Errorf("empty response from Ollama")
	}

	content = response.Message.Content
	requestJSON = string(jsonData)
	responseJSON = string(body)
	return content, requestJSON, responseJSON, nil
}

// TestOllamaConnection tests if the Ollama API is reachable.
func TestOllamaConnection(baseURL, model string) (string, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	baseURL = removeTrailingSlash(baseURL)
	if model == "" {
		model = "llama2"
	}

	reqBody := map[string]interface{}{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(baseURL+"/api/chat", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama API error: status %d: %s", resp.StatusCode, string(body))
	}

	return "Ollama API connection successful", nil
}

// removeTrailingSlash removes any trailing slashes from a URL.
func removeTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// logOllamaIntegration logs Ollama integration events to the database.
func logOllamaIntegration(model string, direction string, endpoint string, startTime time.Time, request string, response string, err error) {
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
