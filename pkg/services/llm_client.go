package services

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// The LLM protocol types are now defined canonically in pkg/engine and
// re-exported here as type aliases so the provider implementations and all
// existing call sites continue to compile unchanged.
type LLMClient = engine.LLMClient
type StreamEvent = engine.StreamEvent
type StreamEventType = engine.StreamEventType
type ChatMessage = engine.ChatMessage
type ToolCall = engine.ToolCall
type ToolCallFunction = engine.ToolCallFunction
type Tool = engine.Tool
type FunctionDef = engine.FunctionDef

const (
	StreamContentDelta   = engine.StreamContentDelta
	StreamToolCallDelta  = engine.StreamToolCallDelta
	StreamReasoningStart = engine.StreamReasoningStart
	StreamReasoningEnd   = engine.StreamReasoningEnd
	StreamToolCallStart  = engine.StreamToolCallStart
	StreamToolCallEnd    = engine.StreamToolCallEnd
	StreamDone           = engine.StreamDone
)

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
