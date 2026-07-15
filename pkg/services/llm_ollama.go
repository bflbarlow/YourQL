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

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

func NewOllamaClient(provider *models.LLMProvider) (LLMClient, error) {
	model := "llama2"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}
	baseURL := "http://localhost:11434"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}
	baseURL = removeTrailingSlash(baseURL)
	return &OllamaClient{
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}, nil
}

func (c *OllamaClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	content, _, _, err := c.ChatCompletionWithPayload(ctx, messages)
	return content, err
}

func (c *OllamaClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	ollamaMessages := make([]ollamaChatMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollamaChatMessage{Role: msg.Role, Content: msg.Content}
	}

	reqBody := ollamaChatRequest{Model: c.model, Messages: ollamaMessages, Stream: false}
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
		return "", "", "", fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response ollamaChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Message.Content == "" {
		var errorInfo struct {
			Error string `json:"error"`
		}
		if e := json.Unmarshal(body, &errorInfo); e == nil && errorInfo.Error != "" {
			return "", "", "", fmt.Errorf("Ollama API error: %s", errorInfo.Error)
		}
		return "", "", "", fmt.Errorf("empty response from Ollama")
	}

	return response.Message.Content, string(jsonData), string(body), nil
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
