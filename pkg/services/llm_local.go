package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"YourQL/pkg/models"
)

// LocalClient implements LLMClient for a locally loaded model.
// Supports two modes:
// 1. HTTP endpoint: sends requests to a local HTTP API (e.g., llama.cpp server, text-generation-webui)
// 2. GGUF model file: uses llama.cpp CLI directly (requires llama.cpp binary)
type LocalClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
	useHTTP    bool
}

// localChatRequest is the request payload for local model APIs.
type localChatRequest struct {
	Model     string              `json:"model,omitempty"`
	Messages  []localChatMessage  `json:"messages,omitempty"`
	Prompt    string              `json:"prompt,omitempty"`
	Stream    bool                `json:"stream,omitempty"`
	Options   map[string]float64  `json:"options,omitempty"`
}

type localChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// localChatResponse is the response from a local model API.
type localChatResponse struct {
	Response  string `json:"response,omitempty"`
	Content   string `json:"content,omitempty"`
	Text      string `json:"text,omitempty"`
	Generated string `json:"generated,omitempty"`
}

// NewLocalClient creates a new local model client from a provider configuration.
func NewLocalClient(provider *models.LLMProvider) (LLMClient, error) {
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		// HTTP mode: use the provided base URL
		baseURL := removeTrailingSlash(*provider.BaseURL)
		return &LocalClient{
			baseURL:    baseURL,
			model:      getModelName(provider),
			httpClient: &http.Client{Timeout: 180 * time.Second},
			useHTTP:    true,
		}, nil
	}

	// File mode: use GGUF model file path
	if provider.Model == nil || *provider.Model == "" {
		return nil, fmt.Errorf("local model requires either a base_url (HTTP endpoint) or a model file path")
	}

	return &LocalClient{
		model:      *provider.Model,
		httpClient: &http.Client{Timeout: 180 * time.Second},
		useHTTP:    false,
	}, nil
}

// ChatCompletion sends a conversation to the local model and returns the assistant's reply.
func (c *LocalClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	startTime := time.Now()

	if c.useHTTP {
		return c.chatViaHTTP(ctx, messages, startTime)
	}
	return c.chatViaCLI(ctx, messages, startTime)
}

// ChatCompletionWithPayload sends a conversation and returns the full request/response payloads.
func (c *LocalClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	if c.useHTTP {
		// Try OpenAI-compatible format first
		openAIReq := map[string]interface{}{
			"model": c.model,
			"messages": toOpenAIMessages(messages),
			"stream":  false,
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

		// Try legacy API format
		legacyReq := map[string]interface{}{
			"prompt":   buildPromptFromMessages(messages),
			"stream":   false,
			"n_predict": 2000,
		}
		legacyJSON, err := json.Marshal(legacyReq)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to marshal legacy request: %w", err)
		}

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

		var legacyResp localChatResponse
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

	// CLI mode: not supported for payload capture
	return "", "", "", fmt.Errorf("ChatCompletionWithPayload not supported for CLI mode")
}

// chatViaHTTP sends a chat request to a local HTTP API (e.g., llama.cpp server, text-generation-webui).
func (c *LocalClient) chatViaHTTP(ctx context.Context, messages []ChatMessage, startTime time.Time) (string, error) {
	// Try OpenAI-compatible format first
	openAIReq := map[string]interface{}{
		"model": c.model,
		"messages": toOpenAIMessages(messages),
		"stream":  false,
	}

	jsonData, err := json.Marshal(openAIReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Try OpenAI-compatible endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		// Try OpenAI-compatible response format
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
				logLocalIntegration(c.model, "incoming", c.baseURL+"/v1/chat/completions", startTime, string(jsonData), reply, nil)
				return reply, nil
			}
		}

		// Try generic response format
		var genericResp localChatResponse
		if err := json.Unmarshal(body, &genericResp); err == nil {
			reply := genericResp.Response
			if reply == "" {
				reply = genericResp.Content
			}
			if reply == "" {
				reply = genericResp.Text
			}
			if reply == "" {
				reply = genericResp.Generated
			}
			if reply != "" {
				logLocalIntegration(c.model, "incoming", c.baseURL+"/v1/chat/completions", startTime, string(jsonData), reply, nil)
				return reply, nil
			}
		}

		return "", fmt.Errorf("unrecognized response format from local API: %s", string(body))
	}

	// If OpenAI endpoint fails, try the legacy API format
	legacyReq := map[string]interface{}{
		"prompt":   buildPromptFromMessages(messages),
		"stream":   false,
		"n_predict": 2000,
	}
	legacyJSON, err := json.Marshal(legacyReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal legacy request: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, "POST", c.baseURL+"/completion", bytes.NewBuffer(legacyJSON))
	if err != nil {
		logLocalIntegration(c.model, "outgoing", c.baseURL+"/completion", startTime, string(legacyJSON), "", err)
		return "", fmt.Errorf("failed to create legacy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = c.httpClient.Do(req)
	if err != nil {
		logLocalIntegration(c.model, "outgoing", c.baseURL+"/completion", startTime, string(legacyJSON), "", err)
		return "", fmt.Errorf("failed to send legacy request: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logLocalIntegration(c.model, "incoming", c.baseURL+"/completion", startTime, string(legacyJSON), string(body), fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		return "", fmt.Errorf("local API returned status %d: %s", resp.StatusCode, string(body))
	}

	var legacyResp localChatResponse
	if err := json.Unmarshal(body, &legacyResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
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

	logLocalIntegration(c.model, "incoming", c.baseURL+"/completion", startTime, string(legacyJSON), reply, nil)
	return reply, nil
}

// chatViaCLI uses the llama.cpp CLI to run a local model.
func (c *LocalClient) chatViaCLI(ctx context.Context, messages []ChatMessage, startTime time.Time) (string, error) {
	modelPath := c.model
	if !filepath.IsAbs(modelPath) {
		absPath, err := filepath.Abs(modelPath)
		if err == nil {
			modelPath = absPath
		}
	}

	// Check if the model file exists
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return "", fmt.Errorf("model file not found: %s", modelPath)
	}

	// Find llama.cpp binary
	llamaBinary := "llama-cli"
	if path, err := exec.LookPath(llamaBinary); err == nil {
		llamaBinary = path
	} else {
		// Try common paths
		paths := []string{
			"./llama-cli",
			"../llama.cpp/llama-cli",
			"/usr/local/bin/llama-cli",
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				llamaBinary = p
				break
			}
		}
	}

	if _, err := os.Stat(llamaBinary); os.IsNotExist(err) {
		return "", fmt.Errorf("llama.cpp binary not found. Please install llama.cpp or configure a base_url for HTTP mode")
	}

	prompt := buildPromptFromMessages(messages)

	cmd := exec.CommandContext(ctx, llamaBinary,
		"-m", modelPath,
		"--prompt", prompt,
		"-n", "2000",
		"--temp", "0.1",
		"--top-p", "0.9",
		"--threads", "4",
		"--no-context",
		"--verbose",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logLocalIntegration(c.model, "outgoing", "cli:"+llamaBinary, startTime, prompt, "", err)
		return "", fmt.Errorf("llama.cpp execution failed: %w (output: %s)", err, string(output))
	}

	// Extract the response from llama.cpp output (last line that's not a status message)
	lines := string(output)
	// llama.cpp outputs status messages to stderr and text to stdout
	// For simplicity, return the full output
	logLocalIntegration(c.model, "incoming", "cli:"+llamaBinary, startTime, prompt, lines, nil)
	return lines, nil
}

// TestLocalConnection tests if a local model or HTTP endpoint is reachable.
func TestLocalConnection(baseURL, model string) (string, error) {
	if baseURL != "" {
		// Test HTTP endpoint
		url := removeTrailingSlash(baseURL)
		reqBody := map[string]interface{}{
			"model":  model,
			"stream": false,
			"prompt": "Hello",
		}
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return "", err
		}

		resp, err := http.Post(url+"/completion", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("local model API error: status %d: %s", resp.StatusCode, string(body))
		}

		return "Local model API connection successful", nil
	}

	if model == "" {
		return "", fmt.Errorf("model path is required for local mode")
	}

	// Test model file exists
	if _, err := os.Stat(model); os.IsNotExist(err) {
		return "", fmt.Errorf("model file not found: %s", model)
	}

	return fmt.Sprintf("Model file found: %s", model), nil
}

// toOpenAIMessages converts our ChatMessage format to OpenAI-compatible format.
func toOpenAIMessages(messages []ChatMessage) []map[string]string {
	result := make([]map[string]string, len(messages))
	for i, msg := range messages {
		result[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	return result
}

// buildPromptFromMessages builds a prompt string from ChatMessages for CLI mode.
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

// getModelName extracts the model name from a provider config.
func getModelName(provider *models.LLMProvider) string {
	if provider.Model != nil && *provider.Model != "" {
		return *provider.Model
	}
	return "local-model"
}

// logLocalIntegration logs local model integration events to the database.
func logLocalIntegration(model string, direction string, endpoint string, startTime time.Time, request string, response string, err error) {
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
