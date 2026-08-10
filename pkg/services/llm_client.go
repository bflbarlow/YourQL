package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"YourQL/pkg/models"
)

type LLMClient interface {
	ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
	ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
	ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (msg *ChatMessage, requestJSON, responseJSON string, err error)

	// ChatCompletionWithToolsStreaming sends tools and streams the response
	// via onEvent. The final assembled *ChatMessage is returned when the
	// stream completes (identical shape to the blocking path). The onEvent
	// callback is called synchronously from the SSE reader goroutine —
	// callers must not block it for long.
	ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (msg *ChatMessage, requestJSON, responseJSON string, err error)
}

// -------------------------------------------------------------------------
// Streaming types
// -------------------------------------------------------------------------

// StreamEvent is emitted for each chunk of a streaming LLM response.
type StreamEvent struct {
	Type StreamEventType `json:"type"`

	// ContentDelta: incremental text from the model.
	Content string `json:"content,omitempty"`

	// ToolCallDelta: name and arguments for a tool call being built.
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`

	// ReasoningStart / ReasoningEnd: brackets around model reasoning.
	ReasoningStart bool `json:"reasoning_start,omitempty"`
	ReasoningEnd   bool `json:"reasoning_end,omitempty"`

	// ToolCallStart / ToolCallEnd: lifecycle markers for tool calls.
	ToolCallStart bool `json:"tool_call_start,omitempty"`
	ToolCallEnd   bool `json:"tool_call_end,omitempty"`

	// Done: the stream is complete.
	FinishReason string `json:"finish_reason,omitempty"`
}

type StreamEventType string

const (
	StreamContentDelta   StreamEventType = "content_delta"
	StreamToolCallDelta  StreamEventType = "tool_call_delta"
	StreamReasoningStart StreamEventType = "reasoning_start"
	StreamReasoningEnd   StreamEventType = "reasoning_end"
	StreamToolCallStart  StreamEventType = "tool_call_start"
	StreamToolCallEnd    StreamEventType = "tool_call_end"
	StreamDone           StreamEventType = "done"
)

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// NewLLMClient creates an LLM client based on the provider configuration.
func NewLLMClient(provider *models.LLMProvider) (LLMClient, error) {
	switch provider.Provider {
	case "openai":
		return NewOpenAIClient(provider)
	case "anthropic":
		return NewAnthropicClient(provider)
	case "ollama":
		return NewOllamaClient(provider)
	case "local":
		return NewLocalClient(provider)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", provider.Provider)
	}
}

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

func removeTrailingSlash(s string) string {
	return strings.TrimSuffix(s, "/")
}

// effectiveMaxTokens returns the max tokens to use for a provider.
// The user's explicit MaxTokens setting is respected. ContextWindow is an
// absolute ceiling (you can't output more tokens than the model's context).
// ModelMaxTokens is NOT used for clamping — it's a detected suggestion
// from the API, not a hard limit, and must not override the user's choice.
func effectiveMaxTokens(provider *models.LLMProvider) int {
	configured := 2000
	if provider.MaxTokens != nil && *provider.MaxTokens > 0 {
		configured = *provider.MaxTokens
	}
	// Clamp to context window if known — this is a hard physical ceiling.
	if provider.ContextWindow != nil && *provider.ContextWindow > 0 {
		if configured > *provider.ContextWindow {
			configured = *provider.ContextWindow
		}
	}
	return configured
}

// streamDebugCapture optionally tees an SSE response body to a debug file.
// Gated behind YOURQL_DEBUG_STREAMS=1 — never active in production.
// Files are written to ~/.yourql/debug/streams/ and must be cleaned
// manually by the developer.
type streamDebugCapture struct {
	enabled bool
	buf     bytes.Buffer
	convID  uint
	round   int
}

func newStreamDebugCapture(conversationID uint, round int) *streamDebugCapture {
	if os.Getenv("YOURQL_DEBUG_STREAMS") != "1" {
		return &streamDebugCapture{}
	}
	return &streamDebugCapture{enabled: true, convID: conversationID, round: round}
}

// reader wraps r so reads are also copied into the capture buffer.
func (c *streamDebugCapture) reader(r io.Reader) io.Reader {
	if !c.enabled {
		return r
	}
	return io.TeeReader(r, &c.buf)
}

// finish writes the captured buffer to disk and returns the file path.
// Returns "" when capture is disabled or the buffer is empty.
func (c *streamDebugCapture) finish() string {
	if !c.enabled || c.buf.Len() == 0 {
		return ""
	}
	dir := filepath.Join(os.Getenv("HOME"), ".yourql", "debug", "streams")
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("[DebugStream] Failed to create dir %s: %v", dir, err)
		return ""
	}
	filename := fmt.Sprintf("%d_%d_%d.txt", c.convID, c.round, time.Now().Unix())
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, c.buf.Bytes(), 0600); err != nil {
		log.Printf("[DebugStream] Failed to write %s: %v", path, err)
		return ""
	}
	log.Printf("[DebugStream] Wrote %d bytes to %s", c.buf.Len(), path)
	return path
}
