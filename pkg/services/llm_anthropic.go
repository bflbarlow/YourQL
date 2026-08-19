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

// ---------------------------------------------------------------------------
// Anthropic request/response types
// ---------------------------------------------------------------------------

// anthropicChatRequest is the request payload for Anthropic's messages API.
type anthropicChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []anthropicChatMessage `json:"messages"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature *float64               `json:"temperature,omitempty"`
	TopP        *float64               `json:"top_p,omitempty"`
	System      string                 `json:"system,omitempty"`
	Tools       []anthropicTool        `json:"tools,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
}

// anthropicChatMessage represents a single message. Content is either a
// plain string (simple text messages) or []anthropicContentBlock (messages
// with tool_use or tool_result content blocks). Anthropic's API requires
// that tool_use blocks live inside an assistant message's content array, and
// tool_result blocks live inside a user message's content array.
type anthropicChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string | []anthropicContentBlock
}

// anthropicContentBlock is one entry in a message's content array.
// It can be a "text", "tool_use", or "tool_result" block.
type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`        // "text" block
	ID        string         `json:"id,omitempty"`          // "tool_use" block
	Name      string         `json:"name,omitempty"`        // "tool_use" block
	Input     map[string]any `json:"input,omitempty"`       // "tool_use" block
	ToolUseID string         `json:"tool_use_id,omitempty"` // "tool_result" block
	// Content for tool_result: string or []map[string]any
	ToolResultContent interface{} `json:"content,omitempty"`
}

// anthropicTool defines a tool available to the model.
type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicChatResponse is the response from Anthropic's messages API.
// Content blocks can be "text", "tool_use", or "tool_result" types.
type anthropicChatResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text,omitempty"`
		ID    string         `json:"id,omitempty"`
		Name  string         `json:"name,omitempty"`
		Input map[string]any `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ---------------------------------------------------------------------------
// Client struct and constructor
// ---------------------------------------------------------------------------

// AnthropicClient implements LLMClient for Anthropic's Claude API.
type AnthropicClient struct {
	maxTokens  int
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
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
		maxTokens:  effectiveMaxTokens(provider),
		httpClient: &http.Client{Timeout: 300 * time.Second},
	}, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// extractSystem separates "system" role messages from the list and returns
// the system content plus the remaining non-system messages.
// Anthropic requires system prompts in a top-level field, not as a message.
func extractSystem(messages []ChatMessage) (systemContent string, filtered []ChatMessage) {
	for _, msg := range messages {
		if msg.Role == "system" {
			systemContent = msg.Content
		} else {
			filtered = append(filtered, msg)
		}
	}
	return
}

// anthropicHeaders sets the standard Anthropic API headers.
func anthropicHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

// doAnthropicRequest sends a POST to the messages endpoint and returns the
// raw body. The caller handles unmarshalling.
func (c *AnthropicClient) doAnthropicRequest(ctx context.Context, jsonData []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	anthropicHeaders(req, c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Anthropic] ERROR — HTTP %d from %s (model=%s): %s", resp.StatusCode, c.baseURL, c.model, string(body))
		return nil, fmt.Errorf("Anthropic API returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// ---------------------------------------------------------------------------
// Message builders — convert ChatMessage → anthropicChatMessage
// ---------------------------------------------------------------------------

// buildSimpleAnthropicMessages converts ChatMessages with string-only
// content (no tool calls, no tool results). Used by ChatCompletion and
// ChatCompletionWithPayload for the old JSON-response protocol.
func buildSimpleAnthropicMessages(messages []ChatMessage) []anthropicChatMessage {
	out := make([]anthropicChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		out = append(out, anthropicChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return out
}

// buildToolAnthropicMessages converts ChatMessages for the tool-calling
// protocol, translating tool_calls into tool_use content blocks and tool
// role messages into tool_result content blocks. Regular messages keep
// string content.
//
// Anthropic's wire format nests tool calls inside assistant messages as
// content blocks, and tool results inside user messages as content blocks.
// This is structurally different from OpenAI's flat tool_calls array.
//
// Anthropic's Messages API requires strict user/assistant role alternation.
// When a single assistant turn makes multiple tool calls, the agentic loop
// appends one "tool"-role ChatMessage per call — these MUST be merged into
// a single user message with multiple tool_result blocks, or the API
// rejects the request for having consecutive user messages.
func buildToolAnthropicMessages(messages []ChatMessage) []anthropicChatMessage {
	out := make([]anthropicChatMessage, 0, len(messages))

	i := 0
	for i < len(messages) {
		msg := messages[i]

		if msg.Role == "system" {
			i++
			continue
		}

		switch {
		case msg.Role == "tool":
			// Merge this and every immediately-following "tool" message into
			// a single user message with one tool_result block each.
			var blocks []anthropicContentBlock
			for i < len(messages) && messages[i].Role == "tool" {
				resultContent := messages[i].Content
				if resultContent == "" {
					resultContent = "(empty result)"
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:              "tool_result",
					ToolUseID:         messages[i].ToolCallID,
					ToolResultContent: resultContent,
				})
				i++
			}
			out = append(out, anthropicChatMessage{
				Role:    "user",
				Content: blocks,
			})
			continue

		case len(msg.ToolCalls) > 0:
			// Assistant message with tool_use blocks
			blocks := make([]anthropicContentBlock, 0, len(msg.ToolCalls)+1)
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var input map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
					log.Printf("[Anthropic] Failed to unmarshal tool arguments for %s: %v — using raw string", tc.Function.Name, err)
					input = map[string]any{"_raw_arguments": tc.Function.Arguments}
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			out = append(out, anthropicChatMessage{
				Role:    msg.Role,
				Content: blocks,
			})
			i++

		default:
			// Plain text user/assistant message
			out = append(out, anthropicChatMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
			i++
		}
	}

	return out
}

// convertTools maps the shared []Tool vocabulary to Anthropic's tool schema.
func convertTools(tools []Tool) []anthropicTool {
	out := make([]anthropicTool, len(tools))
	for i, t := range tools {
		out[i] = anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		}
	}
	return out
}

// parseAnthropicResponse parses the raw JSON response body into a ChatMessage
// with Content and/or ToolCalls extracted from the content blocks.
func parseAnthropicResponse(body []byte) (*ChatMessage, error) {
	var response anthropicChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Content) == 0 {
		var errorInfo struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if e := json.Unmarshal(body, &errorInfo); e == nil && errorInfo.Error.Message != "" {
			return nil, fmt.Errorf("API error: %s (type: %s)", errorInfo.Error.Message, errorInfo.Error.Type)
		}
		return nil, fmt.Errorf("API returned empty response (no content blocks)")
	}

	msg := &ChatMessage{Role: "assistant", FinishReason: response.StopReason, PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens}

	for _, block := range response.Content {
		switch block.Type {
		case "text":
			msg.Content += block.Text

		case "tool_use":
			argsJSON, err := json.Marshal(block.Input)
			if err != nil {
				log.Printf("[Anthropic] Failed to marshal tool_use input for %s: %v", block.Name, err)
				argsJSON = []byte("{}")
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID: block.ID,
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	if msg.Content == "" && len(msg.ToolCalls) == 0 {
		return nil, fmt.Errorf("no text or tool_use content in response")
	}

	return msg, nil
}

// ---------------------------------------------------------------------------
// Interface methods
// ---------------------------------------------------------------------------

// ChatCompletion sends a conversation to Anthropic's API and returns the
// assistant's reply as plain text. For tool-calling conversations, use
// ChatCompletionWithTools instead.
func (c *AnthropicClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	content, _, _, err := c.ChatCompletionWithPayload(ctx, messages)
	return content, err
}

// ChatCompletionWithPayload sends a conversation and returns the full
// request/response payloads alongside the assistant's text reply.
func (c *AnthropicClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	systemContent, filtered := extractSystem(messages)
	if len(filtered) == 0 {
		return "", "", "", fmt.Errorf("no user/assistant messages provided")
	}

	reqBody := anthropicChatRequest{
		Model:       c.model,
		Messages:    buildSimpleAnthropicMessages(filtered),
		MaxTokens:   c.maxTokens,
		Temperature: floatPtr(0.1),
	}
	if systemContent != "" {
		reqBody.System = systemContent
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	body, err := c.doAnthropicRequest(ctx, jsonData)
	if err != nil {
		return "", "", "", err
	}

	var response anthropicChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Content) == 0 {
		return "", "", "", fmt.Errorf("API returned empty response (no content)")
	}

	var reply string
	for _, block := range response.Content {
		if block.Type == "text" {
			reply += block.Text
		}
	}
	if reply == "" {
		return "", "", "", fmt.Errorf("no text content in response")
	}

	return reply, string(jsonData), string(body), nil
}

// ChatCompletionWithTools is the tool-aware entry point. It sends tools to
// Anthropic's API, translates our shared ChatMessage vocabulary into
// Anthropic's nested content-block format, and returns a ChatMessage with
// Content and ToolCalls extracted from the response.
func (c *AnthropicClient) ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error) {
	systemContent, filtered := extractSystem(messages)
	if len(filtered) == 0 {
		return nil, "", "", fmt.Errorf("no user/assistant messages provided")
	}

	reqBody := anthropicChatRequest{
		Model:       c.model,
		Messages:    buildToolAnthropicMessages(filtered),
		MaxTokens:   c.maxTokens,
		Temperature: floatPtr(0.1),
		Tools:       convertTools(tools),
	}
	if systemContent != "" {
		reqBody.System = systemContent
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[Anthropic] Sending tool request — model=%s, messages=%d, tools=%d",
		c.model, len(filtered), len(tools))

	body, err := c.doAnthropicRequest(ctx, jsonData)
	if err != nil {
		return nil, "", "", err
	}

	msg, err := parseAnthropicResponse(body)
	if err != nil {
		return nil, string(jsonData), string(body), err
	}

	return msg, string(jsonData), string(body), nil
}

// ChatCompletionWithToolsStreaming sends tools with stream:true and parses
// Anthropic's SSE response incrementally. Content blocks (text, tool_use,
// thinking) are accumulated by index and emitted via onEvent chunk by chunk.
// The assembled *ChatMessage is returned when the stream completes.
func (c *AnthropicClient) ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (*ChatMessage, string, string, error) {
	systemContent, filtered := extractSystem(messages)
	if len(filtered) == 0 {
		return nil, "", "", fmt.Errorf("no user/assistant messages provided")
	}

	reqBody := anthropicChatRequest{
		Model:       c.model,
		Messages:    buildToolAnthropicMessages(filtered),
		MaxTokens:   c.maxTokens,
		Temperature: floatPtr(0.1),
		Tools:       convertTools(tools),
		Stream:      true,
	}
	if systemContent != "" {
		reqBody.System = systemContent
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[Anthropic] Sending streaming tool request — model=%s, messages=%d, tools=%d",
		c.model, len(filtered), len(tools))

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create request: %w", err)
	}
	anthropicHeaders(req, c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Anthropic] ERROR — HTTP %d from %s (model=%s): %s", resp.StatusCode, c.baseURL, c.model, string(body))
		return nil, "", "", fmt.Errorf("Anthropic API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Per-block accumulators (indexed by content block index).
	type blockAccum struct {
		blockType string // "text", "tool_use", "thinking"
		text      strings.Builder
		toolID    string
		toolName  string
		toolInput strings.Builder // partial_json fragments for tool_use
		started   bool
	}
	blocks := make(map[int]*blockAccum)
	var blockOrder []int // preserve insertion order
	var thinkingActive bool
	var stopReason string
	var streamChunks, streamBytes int

	// Optional raw-SSE capture for debugging.
	var debugBuf bytes.Buffer
	var debugReader io.Reader = resp.Body
	if os.Getenv("YOURQL_DEBUG_STREAMS") == "1" {
		debugReader = io.TeeReader(resp.Body, &debugBuf)
	}

	scanner := bufio.NewScanner(debugReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()
		streamChunks++
		streamBytes += len(line)

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "content_block_start":
			var ev struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					Text string `json:"text"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			ba := &blockAccum{blockType: ev.ContentBlock.Type, started: true}
			blocks[ev.Index] = ba
			blockOrder = append(blockOrder, ev.Index)
			switch ev.ContentBlock.Type {
			case "tool_use":
				ba.toolID = ev.ContentBlock.ID
				ba.toolName = ev.ContentBlock.Name
				if onEvent != nil {
					onEvent(StreamEvent{Type: StreamToolCallStart, ToolCallID: ev.ContentBlock.ID, ToolName: ev.ContentBlock.Name})
				}
			case "thinking":
				thinkingActive = true
				if onEvent != nil {
					onEvent(StreamEvent{Type: StreamReasoningStart})
				}
			}

		case "content_block_delta":
			var ev struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
					Thinking    string `json:"thinking"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			ba, ok := blocks[ev.Index]
			if !ok || ba == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				ba.text.WriteString(ev.Delta.Text)
				if onEvent != nil && !thinkingActive {
					onEvent(StreamEvent{Type: StreamContentDelta, Content: ev.Delta.Text})
				}
			case "input_json_delta":
				ba.toolInput.WriteString(ev.Delta.PartialJSON)
				if onEvent != nil {
					onEvent(StreamEvent{
						Type:       StreamToolCallDelta,
						ToolCallID: ba.toolID,
						ToolName:   ba.toolName,
						Arguments:  ev.Delta.PartialJSON,
					})
				}
			case "thinking_delta":
				ba.text.WriteString(ev.Delta.Thinking)
				if onEvent != nil {
					onEvent(StreamEvent{Type: StreamContentDelta, Content: ev.Delta.Thinking})
				}
			}

		case "content_block_stop":
			var ev struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			ba, ok := blocks[ev.Index]
			if !ok || ba == nil {
				continue
			}
			if ba.blockType == "tool_use" && onEvent != nil {
				onEvent(StreamEvent{Type: StreamToolCallEnd, ToolCallID: ba.toolID})
			}
			if ba.blockType == "thinking" {
				thinkingActive = false
				if onEvent != nil {
					onEvent(StreamEvent{Type: StreamReasoningEnd})
				}
			}

		case "message_delta":
			var ev struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			stopReason = ev.Delta.StopReason

		case "message_stop":
			// Stream is complete — handled below
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		log.Printf("[Anthropic] ERROR — SSE stream error from %s (model=%s): %v", c.baseURL, c.model, scanErr)
		return nil, "", "", fmt.Errorf("SSE stream error: %w", scanErr)
	}

	// Emit done
	if onEvent != nil {
		onEvent(StreamEvent{Type: StreamDone, FinishReason: stopReason})
	}

	// Assemble the final ChatMessage from accumulated blocks.
	msg := &ChatMessage{Role: "assistant", FinishReason: stopReason}
	var hasToolCalls bool
	for _, ba := range blocks {
		if ba == nil || !ba.started {
			continue
		}
		switch ba.blockType {
		case "text":
			msg.Content += ba.text.String()
		case "tool_use":
			hasToolCalls = true
			inputStr := ba.toolInput.String()
			if inputStr == "" {
				inputStr = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID: ba.toolID,
				Function: ToolCallFunction{
					Name:      ba.toolName,
					Arguments: inputStr,
				},
			})
		}
		// thinking blocks are excluded from the final message.
	}
	// Build raw response as structured JSON (all blocks in order, including thinking)
	type rawBlock struct {
		Type      string `json:"type"`
		Content   string `json:"content,omitempty"`
		ToolName  string `json:"tool_name,omitempty"`
		ToolInput string `json:"tool_input,omitempty"`
	}
	var rawBlocks []rawBlock
	for _, idx := range blockOrder {
		ba := blocks[idx]
		if ba == nil || !ba.started {
			continue
		}
		rb := rawBlock{Type: ba.blockType}
		switch ba.blockType {
		case "text", "thinking":
			rb.Content = ba.text.String()
		case "tool_use":
			rb.ToolName = ba.toolName
			rb.ToolInput = ba.toolInput.String()
		}
		rawBlocks = append(rawBlocks, rb)
	}
	rawResponse := ""
	if rawJSON, err := json.Marshal(map[string]interface{}{
		"stop_reason": stopReason,
		"blocks":      rawBlocks,
	}); err == nil {
		rawResponse = string(rawJSON)
	}

	// Fallback: surface raw output when the parser got nothing usable.
	if msg.Content == "" && !hasToolCalls && len(rawBlocks) > 0 {
		allText := ""
		for _, rb := range rawBlocks {
			if rb.Content != "" {
				allText += rb.Content
			}
		}
		msg.Content = fmt.Sprintf("[%d chunks, %d bytes] %s", streamChunks, streamBytes, truncateString(allText, 2000))
	}

	// Write debug stream capture if enabled
	if debugBuf.Len() > 0 {
		dir := filepath.Join(os.Getenv("HOME"), ".yourql", "debug", "streams")
		if err := os.MkdirAll(dir, 0700); err == nil {
			filename := fmt.Sprintf("%d.txt", time.Now().UnixNano())
			path := filepath.Join(dir, filename)
			if err := os.WriteFile(path, debugBuf.Bytes(), 0600); err == nil {
				log.Printf("[Anthropic] Debug stream written: %s (%d bytes)", path, debugBuf.Len())
			}
		}
	}

	return msg, string(jsonData), rawResponse, nil
}

// ---------------------------------------------------------------------------
// Connection test
// ---------------------------------------------------------------------------

// TestAnthropicConnection tests if the Anthropic API key is valid.
func TestAnthropicConnection(apiKey, model, baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	if model == "" {
		model = "claude-3-haiku-20240307"
	}

	reqBody := map[string]interface{}{
		"model":      model,
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
	anthropicHeaders(req, apiKey)

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
		log.Printf("[Anthropic] ERROR — HTTP %d from %s (model=%s): %s", resp.StatusCode, baseURL, model, string(body))
		return "", fmt.Errorf("Anthropic API error: status %d: %s", resp.StatusCode, string(body))
	}

	return "Anthropic Claude API connection successful", nil
}

// floatPtr returns a pointer to the given float64 value.
func floatPtr(f float64) *float64 {
	return &f
}
