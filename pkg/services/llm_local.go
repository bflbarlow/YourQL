package services

import (
	"bufio"
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

// LocalClient sends requests to a local HTTP API (e.g., llama.cpp server,
// LM Studio, text-generation-webui) that exposes an OpenAI-compatible
// /v1/chat/completions endpoint.
type LocalClient struct {
	baseURL    string
	model      string
	maxTokens  int
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
		maxTokens:  effectiveMaxTokens(provider),
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
	if c.maxTokens > 0 {
		openAIReq["max_tokens"] = c.maxTokens
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

// ChatCompletionWithTools uses the two-field JSON fallback protocol (§7.4).
// The model is instructed to emit a tiny JSON routing prefix with prose
// outside the JSON entirely, bypassing all the escaping/parsing bugs that
// the old JSON-response protocol suffered from.
func (c *LocalClient) ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error) {
	flattened := flattenFallbackHistory(messages)
	adapted := injectFallbackInstructions(flattened, tools)

	content, reqJSON, respJSON, err := c.ChatCompletionWithPayload(ctx, adapted)
	if err != nil {
		return nil, reqJSON, respJSON, err
	}

	log.Printf("[Local] Fallback response (%d chars): %s", len(content), truncateString(content, 200))
	msg := parseFallbackResponse(content)
	return msg, reqJSON, respJSON, nil
}

// ChatCompletionWithToolsStreaming sends tools with stream:true to a
// local OpenAI-compatible endpoint. If the backend supports SSE streaming,
// chunks are delivered via onEvent as they arrive. If it doesn't (returns
// plain JSON), the response is treated as a single content-delta chunk.
func (c *LocalClient) ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (*ChatMessage, string, string, error) {
	flattened := flattenFallbackHistory(messages)
	adapted := injectFallbackInstructions(flattened, tools)

	// Use OpenAI-compatible format
	openAIReq := map[string]interface{}{
		"model":    c.model,
		"messages": toOpenAIMessages(adapted),
		"stream":   true,
	}
	if c.maxTokens > 0 {
		openAIReq["max_tokens"] = c.maxTokens
	}

	jsonData, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", "", fmt.Errorf("local API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Read the first line to detect SSE (starts with "data: ") vs plain JSON.
	// Use a bufio.Scanner on resp.Body so chunks arrive incrementally
	// instead of blocking until the entire stream completes.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return nil, "", "", fmt.Errorf("empty response from local endpoint")
	}
	firstLine := scanner.Text()

	if strings.HasPrefix(firstLine, "data: ") {
		// SSE streaming — process incrementally
		var contentBuf strings.Builder
		sawDone := false

		processLine := func(line string) {
			if !strings.HasPrefix(line, "data: ") {
				return
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				sawDone = true
				return
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				contentBuf.WriteString(chunk.Choices[0].Delta.Content)
				if onEvent != nil {
					onEvent(StreamEvent{Type: StreamContentDelta, Content: chunk.Choices[0].Delta.Content})
				}
			}
		}

		processLine(firstLine)
		for scanner.Scan() {
			processLine(scanner.Text())
		}

		_ = sawDone // stream may end without explicit [DONE]
		msg := parseFallbackResponse(contentBuf.String())
		// Fallback: surface raw output when parsing produced nothing.
		if msg.Content == "" && len(msg.ToolCalls) == 0 && contentBuf.Len() > 0 {
			msg.Content = truncateString(contentBuf.String(), 2000)
		}
		if onEvent != nil {
			onEvent(StreamEvent{Type: StreamDone})
		}
		return msg, string(jsonData), contentBuf.String(), nil
	}

	// Non-streaming fallback: read remaining lines and parse as JSON.
	var full strings.Builder
	full.WriteString(firstLine)
	full.WriteString("\n")
	for scanner.Scan() {
		full.WriteString(scanner.Text())
		full.WriteString("\n")
	}
	reply := strings.TrimSpace(full.String())

	// Try OpenAI-compatible response format
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(reply), &response); err == nil && len(response.Choices) > 0 {
		reply = response.Choices[0].Message.Content
	}

	msg := parseFallbackResponse(reply)
	if onEvent != nil {
		onEvent(StreamEvent{Type: StreamContentDelta, Content: msg.Content})
		onEvent(StreamEvent{Type: StreamDone})
	}
	return msg, string(jsonData), reply, nil
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

	return "Model API connection successful", nil
}
