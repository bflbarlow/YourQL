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
	"os"
	"path/filepath"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// OpenAIClient implements LLMClient for OpenAI's API.
type OpenAIClient struct {
	maxTokens  int
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenAIClient(provider *models.LLMProvider) (LLMClient, error) {
	model := "gpt-3.5-turbo"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}

	baseURL := "https://api.openai.com/v1"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}

	localEndpoint := baseURL != "https://api.openai.com/v1"
	var apiKey string
	if provider.APIKey != nil {
		apiKey = *provider.APIKey
	}
	if !localEndpoint && apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required for https://api.openai.com/v1")
	}

	return &OpenAIClient{baseURL: baseURL, apiKey: apiKey, model: model, maxTokens: effectiveMaxTokens(provider), httpClient: &http.Client{Timeout: 300 * time.Second}}, nil
}

type openAIChatRequest struct {
	Model            string              `json:"model"`
	Messages         []openAIChatMessage `json:"messages"`
	Stream           bool                `json:"stream,omitempty"`
	MaxTokens        int                 `json:"max_tokens,omitempty"`
	Temperature      float64             `json:"temperature,omitempty"`
	TopP             float64             `json:"top_p,omitempty"`
	FrequencyPenalty float64             `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64             `json:"presence_penalty,omitempty"`
	Tools            []Tool              `json:"tools,omitempty"`
}

type openAIChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *OpenAIClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	content, _, _, err := c.ChatCompletionWithPayload(ctx, messages)
	return content, err
}

func (c *OpenAIClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	openAIMessages := make([]openAIChatMessage, len(messages))
	for i, msg := range messages {
		oaiMsg := openAIChatMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			oaiMsg.ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				oaiMsg.ToolCalls[j] = openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
		openAIMessages[i] = oaiMsg
	}

	reqBody := openAIChatRequest{
		Model:       c.model,
		Messages:    openAIMessages,
		Temperature: 0.1,
		MaxTokens:   c.maxTokens,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[OpenAI] Sending request to %s/chat/completions — model=%s, messages=%d",
		c.baseURL, c.model, len(openAIMessages))

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
		return "", "", "", fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response openAIChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 {
		var errInfo struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if e := json.Unmarshal(body, &errInfo); e == nil && errInfo.Error.Message != "" {
			if strings.Contains(strings.ToLower(errInfo.Error.Message), "endpoint") || strings.Contains(strings.ToLower(errInfo.Error.Message), "unexpected") {
				return "", "", "", fmt.Errorf("API endpoint error: %s. For OpenAI-compatible local APIs, the base URL should point to the server root (e.g., http://localhost:1234), NOT include /v1.", errInfo.Error.Message)
			}
			return "", "", "", fmt.Errorf("API error: %s", errInfo.Error.Message)
		}
		rawBody := string(body)
		if len(rawBody) > 500 {
			rawBody = rawBody[:500] + "..."
		}
		return "", "", "", fmt.Errorf("API returned empty response (no choices). Raw: %s", rawBody)
	}

	return response.Choices[0].Message.Content, string(jsonData), string(body), nil
}

func (c *OpenAIClient) ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error) {
	openAIMessages := make([]openAIChatMessage, len(messages))
	for i, msg := range messages {
		oaiMsg := openAIChatMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			oaiMsg.ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				oaiMsg.ToolCalls[j] = openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
		openAIMessages[i] = oaiMsg
	}

	reqBody := openAIChatRequest{
		Model:       c.model,
		Messages:    openAIMessages,
		Temperature: 0.1,
		MaxTokens:   c.maxTokens,
		Tools:       tools,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[OpenAI] Sending tool request to %s/chat/completions — model=%s, messages=%d, tools=%d",
		c.baseURL, c.model, len(openAIMessages), len(tools))

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response openAIChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 {
		var errInfo struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if e := json.Unmarshal(body, &errInfo); e == nil && errInfo.Error.Message != "" {
			if strings.Contains(strings.ToLower(errInfo.Error.Message), "endpoint") || strings.Contains(strings.ToLower(errInfo.Error.Message), "unexpected") {
				return nil, "", "", fmt.Errorf("API endpoint error: %s. For OpenAI-compatible local APIs, the base URL should point to the server root (e.g., http://localhost:1234), NOT include /v1.", errInfo.Error.Message)
			}
			return nil, "", "", fmt.Errorf("API error: %s", errInfo.Error.Message)
		}
		rawBody := string(body)
		if len(rawBody) > 500 {
			rawBody = rawBody[:500] + "..."
		}
		return nil, "", "", fmt.Errorf("API returned empty response (no choices). Raw: %s", rawBody)
	}

	choice := response.Choices[0]
	msg := &ChatMessage{
		Role:    choice.Message.Role,
		Content: choice.Message.Content,
	}
	if len(choice.Message.ToolCalls) > 0 {
		msg.ToolCalls = make([]ToolCall, len(choice.Message.ToolCalls))
		for j, tc := range choice.Message.ToolCalls {
			msg.ToolCalls[j] = ToolCall{
				ID: tc.ID,
				Function: ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	return msg, string(jsonData), string(body), nil
}

// ChatCompletionWithToolsStreaming sends tools with stream:true and parses
// the SSE response incrementally. Each chunk is delivered via onEvent.
// The assembled *ChatMessage is returned when the stream completes.
func (c *OpenAIClient) ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (*ChatMessage, string, string, error) {
	openAIMessages := make([]openAIChatMessage, len(messages))
	for i, msg := range messages {
		oaiMsg := openAIChatMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			oaiMsg.ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				oaiMsg.ToolCalls[j] = openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
		openAIMessages[i] = oaiMsg
	}

	reqBody := openAIChatRequest{
		Model:       c.model,
		Messages:    openAIMessages,
		Temperature: 0.1,
		MaxTokens:   c.maxTokens,
		Tools:       tools,
		Stream:      true,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[OpenAI] Sending streaming tool request — model=%s, messages=%d, tools=%d",
		c.model, len(openAIMessages), len(tools))

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", "", fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Accumulate deltas into a final ChatMessage.
	// rawText captures every successfully-parsed text delta so we can show
	// the model's actual output even when downstream parsing of tool calls
	// or JSON structure fails. streamBytes / streamChunks provide stats.
	var contentBuf, rawText strings.Builder
	var toolCallIDs []string
	var toolCallNames []string
	var toolCallArgs []string
	var finishReason string
	var streamChunks, streamBytes int

	// Optional raw-SSE capture for debugging (gated behind YOURQL_DEBUG_STREAMS=1).
	// Writes the raw response body to ~/.yourql/debug/streams/ when enabled.
	var debugBuf bytes.Buffer
	var debugReader io.Reader = resp.Body
	if os.Getenv("YOURQL_DEBUG_STREAMS") == "1" {
		debugReader = io.TeeReader(resp.Body, &debugBuf)
	}

	scanner := bufio.NewScanner(debugReader)
	// Max token size for a single SSE line — bump from default 64KB to 1MB
	// for large delta chunks (some providers batch aggressively).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		streamChunks++
		streamBytes += len(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			log.Printf("[OpenAI] Failed to parse SSE chunk: %v — line: %s", err, truncateString(data, 200))
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		// Reasoning content (o-series models)
		if delta.ReasoningContent != "" {
			rawText.WriteString(delta.ReasoningContent)
			if onEvent != nil {
				onEvent(StreamEvent{Type: StreamReasoningStart})
				onEvent(StreamEvent{Type: StreamContentDelta, Content: delta.ReasoningContent})
			}
			continue
		}

		// Regular text content
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			rawText.WriteString(delta.Content)
			if onEvent != nil {
				onEvent(StreamEvent{Type: StreamContentDelta, Content: delta.Content})
			}
		}

		// Tool call deltas
		for _, tc := range delta.ToolCalls {
			// Grow slices if needed
			for len(toolCallIDs) <= tc.Index {
				toolCallIDs = append(toolCallIDs, "")
				toolCallNames = append(toolCallNames, "")
				toolCallArgs = append(toolCallArgs, "")
			}

			if tc.ID != "" {
				toolCallIDs[tc.Index] = tc.ID
				if onEvent != nil {
					onEvent(StreamEvent{Type: StreamToolCallStart, ToolCallID: tc.ID})
				}
			}
			if tc.Function.Name != "" {
				toolCallNames[tc.Index] = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				toolCallArgs[tc.Index] += tc.Function.Arguments
				if onEvent != nil {
					onEvent(StreamEvent{
						Type:       StreamToolCallDelta,
						ToolCallID: toolCallIDs[tc.Index],
						ToolName:   toolCallNames[tc.Index],
						Arguments:  tc.Function.Arguments,
					})
				}
			}
		}

		// Finish reason
		if chunk.Choices[0].FinishReason != nil {
			finishReason = *chunk.Choices[0].FinishReason
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, "", "", fmt.Errorf("SSE stream error: %w", scanErr)
	}

	// Emit tool_call_end for each completed tool call, then done
	if onEvent != nil {
		for i := range toolCallIDs {
			if toolCallIDs[i] != "" {
				onEvent(StreamEvent{Type: StreamToolCallEnd, ToolCallID: toolCallIDs[i]})
			}
		}
		onEvent(StreamEvent{Type: StreamDone, FinishReason: finishReason})
	}

	// Assemble the final ChatMessage
	msg := &ChatMessage{
		Role:    "assistant",
		Content: contentBuf.String(),
	}
	// Fallback: if the parser produced nothing usable but the model did
	// emit text (common with partial/broken tool-call JSON), surface the
	// raw output so the tech-details toggle has something to show.
	if msg.Content == "" && len(toolCallIDs) == 0 && rawText.Len() > 0 {
		msg.Content = fmt.Sprintf("[%d chunks, %d bytes] %s", streamChunks, streamBytes, truncateString(rawText.String(), 2000))
	}
	for i := range toolCallIDs {
		if toolCallIDs[i] == "" {
			continue
		}
		args := toolCallArgs[i]
		if args == "" {
			args = "{}"
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID: toolCallIDs[i],
			Function: ToolCallFunction{
				Name:      toolCallNames[i],
				Arguments: args,
			},
		})
	}

	// Write debug stream capture if enabled
	if debugBuf.Len() > 0 {
		dir := filepath.Join(os.Getenv("HOME"), ".yourql", "debug", "streams")
		if err := os.MkdirAll(dir, 0700); err == nil {
			filename := fmt.Sprintf("%d.txt", time.Now().UnixNano())
			path := filepath.Join(dir, filename)
			if err := os.WriteFile(path, debugBuf.Bytes(), 0600); err == nil {
				log.Printf("[OpenAI] Debug stream written: %s (%d bytes)", path, debugBuf.Len())
			}
		}
	}

	return msg, string(jsonData), rawText.String(), nil
}
