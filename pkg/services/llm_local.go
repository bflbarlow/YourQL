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

// LocalClient sends requests to a local HTTP API (e.g., llama.cpp server,
// LM Studio, text-generation-webui) that exposes an OpenAI-compatible
// /v1/chat/completions endpoint.
type LocalClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewLocalClient creates a new local model client. Expects a base_url pointing
// to an OpenAI-compatible HTTP endpoint. CLI/GGUF mode removed (§3.10).
func NewLocalClient(provider *models.LLMProvider) (LLMClient, error) {
	if provider.BaseURL == nil || *provider.BaseURL == "" {
		return nil, fmt.Errorf("local provider requires a base_url pointing to an OpenAI-compatible HTTP endpoint (e.g., LM Studio, llama-server)")
	}

	model := "local-model"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}

	return &LocalClient{
		baseURL:    removeTrailingSlash(*provider.BaseURL),
		model:      model,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}, nil
}

func (c *LocalClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	content, _, _, err := c.ChatCompletionWithPayload(ctx, messages)
	return content, err
}

func (c *LocalClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	// Try OpenAI-compatible format first
	openAIReq := map[string]interface{}{
		"model":    c.model,
		"messages": toOpenAIMessages(messages),
		"stream":   false,
	}

	jsonData, err := json.Marshal(openAIReq)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewBuffer(jsonData))
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

	if resp.StatusCode == http.StatusOK {
		var response struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &response); err == nil && len(response.Choices) > 0 {
			reply := response.Choices[0].Message.Content
			if reply != "" {
				return reply, string(jsonData), string(body), nil
			}
		}
		return "", string(jsonData), string(body), nil
	}

	// Try legacy /completion endpoint
	legacyReq := map[string]interface{}{
		"prompt":    buildPromptFromMessages(messages),
		"stream":    false,
		"n_predict": 2000,
	}
	legacyJSON, _ := json.Marshal(legacyReq)

	req, err = http.NewRequestWithContext(ctx, "POST", c.baseURL+"/completion", bytes.NewBuffer(legacyJSON))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create legacy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to send legacy request: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", string(legacyJSON), string(body), fmt.Errorf("local API returned status %d: %s", resp.StatusCode, string(body))
	}

	var legacyResp struct {
		Response  string `json:"response,omitempty"`
		Content   string `json:"content,omitempty"`
		Text      string `json:"text,omitempty"`
		Generated string `json:"generated,omitempty"`
	}
	if err := json.Unmarshal(body, &legacyResp); err != nil {
		return "", string(legacyJSON), string(body), fmt.Errorf("failed to unmarshal response: %w", err)
	}

	reply := legacyResp.Response
	if reply == "" {
		reply = legacyResp.Content
	}
	if reply == "" {
		reply = legacyResp.Text
	}
	if reply == "" {
		reply = legacyResp.Generated
	}

	return reply, string(legacyJSON), string(body), nil
}

func buildPromptFromMessages(messages []ChatMessage) string {
	var prompt string
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			prompt += "<s>[INST] " + msg.Content + " [/INST]\n"
		case "user":
			prompt += "[INST] " + msg.Content + " [/INST]\n"
		case "assistant":
			prompt += msg.Content + "\n"
		default:
			prompt += msg.Content + "\n"
		}
	}
	return prompt
}

// TestLocalConnection tests if the local HTTP endpoint is reachable.
func TestLocalConnection(baseURL, model string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required for local provider (use an OpenAI-compatible HTTP endpoint)")
	}

	url := removeTrailingSlash(baseURL)
	reqBody := map[string]interface{}{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}
	jsonData, _ := json.Marshal(reqBody)

	resp, err := http.Post(url+"/v1/chat/completions", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("local model API error: status %d: %s", resp.StatusCode, string(body))
	}

	return "Local model API connection successful", nil
}
