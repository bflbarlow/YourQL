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

// ---------------------------------------------------------------------------
// Ollama client — native chat API, fallback JSON protocol for tools
// ---------------------------------------------------------------------------

// OllamaClient implements LLMClient for local Ollama API.
type OllamaClient struct {
	baseURL    string
	model      string
	maxTokens  int
	httpClient *http.Client
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream,omitempty"`
	Options  *ollamaOptions      `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
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
		maxTokens:  effectiveMaxTokens(provider),
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
	if c.maxTokens > 0 {
		reqBody.Options = &ollamaOptions{NumPredict: c.maxTokens}
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

// ChatCompletionWithTools uses the two-field JSON fallback protocol (§7.4).
// The model is instructed to emit a tiny JSON routing prefix ({action, sql})
// with prose outside the JSON entirely. The fallback parser reads the prefix
// and constructs ToolCalls the agentic loop can consume.
func (c *OllamaClient) ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error) {
	flattened := flattenFallbackHistory(messages)
	adapted := injectFallbackInstructions(flattened, tools)

	content, reqJSON, respJSON, err := c.ChatCompletionWithPayload(ctx, adapted)
	if err != nil {
		return nil, reqJSON, respJSON, err
	}

	log.Printf("[Ollama] Fallback response (%d chars): %s", len(content), truncateString(content, 200))
	msg := parseFallbackResponse(content)
	return msg, reqJSON, respJSON, nil
}

// ChatCompletionWithToolsStreaming streams raw text from Ollama via NDJSON.
// Since Ollama models lack native tool calling, the model emits the fallback
// JSON routing prefix as raw text. Each content delta is forwarded to onEvent
// as it arrives, and parseFallbackResponse assembles the final *ChatMessage
// when the stream completes.
func (c *OllamaClient) ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (*ChatMessage, string, string, error) {
	flattened := flattenFallbackHistory(messages)
	adapted := injectFallbackInstructions(flattened, tools)

	ollamaMessages := make([]ollamaChatMessage, len(adapted))
	for i, msg := range adapted {
		ollamaMessages[i] = ollamaChatMessage{Role: msg.Role, Content: msg.Content}
	}

	reqBody := ollamaChatRequest{Model: c.model, Messages: ollamaMessages, Stream: true}
	if c.maxTokens > 0 {
		reqBody.Options = &ollamaOptions{NumPredict: c.maxTokens}
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", "", fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	var fullText strings.Builder
	var ollamaChunks, ollamaBytes int
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		ollamaChunks++
		ollamaBytes += len(line)
		if line == "" {
			continue
		}
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			fullText.WriteString(chunk.Message.Content)
			if onEvent != nil {
				onEvent(StreamEvent{Type: StreamContentDelta, Content: chunk.Message.Content})
			}
		}
		if chunk.Done {
			break
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, "", "", fmt.Errorf("NDJSON stream error: %w", scanErr)
	}

	// Parse the assembled text with the fallback protocol parser.
	msg := parseFallbackResponse(fullText.String())
	// Fallback: surface raw output when the parser got nothing usable.
	if msg.Content == "" && len(msg.ToolCalls) == 0 && fullText.Len() > 0 {
		msg.Content = fmt.Sprintf("[%d chunks, %d bytes] %s", ollamaChunks, ollamaBytes, truncateString(fullText.String(), 2000))
	}
	if onEvent != nil {
		onEvent(StreamEvent{Type: StreamDone})
	}

	return msg, string(jsonData), fullText.String(), nil
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

// ---------------------------------------------------------------------------
// Shared fallback protocol — used by Ollama and Local clients
// ---------------------------------------------------------------------------

const fallbackInstructions = `

## Response Format (Fallback)

You MUST use the following compact routing format for every response. This
is NOT full tool calling — it's a simple routing prefix.

For database queries:
{"action":"sql","sql":"SELECT ... LIMIT 10"}

For responses, explanations, or clarifying questions:
{"action":"respond"}
[your full response using markdown]

CRITICAL RULES:
1. Put the JSON object on its OWN LINE, as the FIRST thing in your response.
2. For "respond" actions, put your answer OUTSIDE the JSON — after the }.
   Do NOT put prose inside JSON string values.
3. The JSON must be valid (double-quote all keys and string values).
4. Do NOT wrap the JSON in ` + "```" + ` markers.
5. Do NOT include any text before the opening {.
6. For "sql" actions, the "sql" field MUST contain a complete, valid SELECT
   query with a LIMIT clause.
`

// flattenFallbackHistory rewrites ToolCalls-bearing assistant messages and
// "tool"-role messages into plain {role, content} turns that a fallback
// model's chat template can actually understand. Ollama/local backends
// generally have no concept of a "tool" role or structured tool_calls —
// the underlying transport (ollamaChatMessage / toOpenAIMessages) only ever
// serializes Role+Content. Without this step, an assistant turn that made a
// query_database call renders as an EMPTY turn on the next round (its
// Content is empty because the call lived in ToolCalls), and the tool's
// result is sent under a role the model's template silently drops —
// meaning exploration and error-retry are invisible to fallback models on
// round 2+. This function re-encodes both using the same JSON-prefix
// vocabulary the model already produces, so its own prior turn round-trips
// through history correctly.
func flattenFallbackHistory(messages []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		switch {
		case msg.Role == "tool":
			// Represent the tool result as a system-role observation so the
			// model sees it as context, not something it needs to route.
			content := msg.Content
			if content == "" {
				content = "(empty result)"
			}
			out = append(out, ChatMessage{
				Role:    "system",
				Content: fmt.Sprintf("[Tool result for %s]\n%s", msg.Name, content),
			})

		case len(msg.ToolCalls) > 0:
			// Re-encode the tool call using the model's own JSON-prefix format
			// so its history shows exactly what it said last time.
			var sb strings.Builder
			for _, tc := range msg.ToolCalls {
				if tc.Function.Name != "query_database" {
					// render_chart or unknown tool — represent generically
					sb.WriteString(fmt.Sprintf(`{"action":%q,"tool":%q}`, "sql", tc.Function.Name))
					continue
				}
				var args struct {
					SQL string `json:"sql"`
				}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				sqlJSON, _ := json.Marshal(map[string]string{"action": "sql", "sql": args.SQL})
				sb.Write(sqlJSON)
			}
			out = append(out, ChatMessage{Role: "assistant", Content: sb.String()})

		default:
			out = append(out, msg)
		}
	}
	return out
}

// injectFallbackInstructions appends the fallback protocol instructions to
// the system message so the model knows the routing format.
func injectFallbackInstructions(messages []ChatMessage, tools []Tool) []ChatMessage {
	modified := make([]ChatMessage, len(messages))
	copy(modified, messages)

	// Build a brief tool description so the model knows about render_chart
	var toolNames []string
	hasRenderChart := false
	for _, t := range tools {
		toolNames = append(toolNames, t.Function.Name)
		if t.Function.Name == "render_chart" {
			hasRenderChart = true
		}
	}

	chartNote := ""
	if hasRenderChart {
		chartNote = `
After a successful SQL query, you may optionally request a chart by responding
with just {"action":"sql","sql":"..."} — charts are automatically generated from
your query results when visualization is enabled.`
	}

	for i := range modified {
		if modified[i].Role == "system" {
			modified[i].Content = modified[i].Content +
				fmt.Sprintf("\n\nAvailable tools: %s.%s", strings.Join(toolNames, ", "), chartNote) +
				fallbackInstructions
			return modified
		}
	}

	// No system message found — prepend one
	sysMsg := ChatMessage{
		Role:    "system",
		Content: fmt.Sprintf("Available tools: %s.%s%s", strings.Join(toolNames, ", "), chartNote, fallbackInstructions),
	}
	return append([]ChatMessage{sysMsg}, modified...)
}

// parseFallbackResponse parses the two-field fallback JSON protocol.
// It reads a leading JSON object with an "action" field, then treats
// everything after the closing brace as unescaped prose (for "respond"
// actions). No double-encoding, no escape-sequence recovery, no
// code-fence extraction — prose was never inside a JSON string to begin
// with.
func parseFallbackResponse(text string) *ChatMessage {
	text = strings.TrimSpace(text)

	// Find and balance the first JSON object
	start := strings.Index(text, "{")
	if start == -1 {
		// No JSON — treat entire response as prose
		return &ChatMessage{Role: "assistant", Content: text}
	}

	depth := 0
	end := -1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				goto found
			}
		}
	}
found:

	if end == -1 {
		// Unbalanced braces — treat as prose
		return &ChatMessage{Role: "assistant", Content: text}
	}

	jsonPart := text[start : end+1]
	restPart := strings.TrimSpace(text[end+1:])

	var fallback struct {
		Action string `json:"action"`
		SQL    string `json:"sql,omitempty"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &fallback); err != nil {
		// Invalid JSON in the prefix — treat whole thing as prose
		log.Printf("[Fallback] JSON parse error in prefix '%s': %v", jsonPart, err)
		return &ChatMessage{Role: "assistant", Content: text}
	}

	switch fallback.Action {
	case "sql":
		if fallback.SQL == "" {
			return &ChatMessage{Role: "assistant", Content: restPart}
		}
		argsJSON, err := json.Marshal(map[string]interface{}{
			"sql":            fallback.SQL,
			"is_exploration": false,
		})
		if err != nil {
			argsJSON = []byte(fmt.Sprintf(`{"sql":%q,"is_exploration":false}`, fallback.SQL))
		}
		return &ChatMessage{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID: "fallback_0",
					Function: ToolCallFunction{
						Name:      "query_database",
						Arguments: string(argsJSON),
					},
				},
			},
		}

	case "respond":
		return &ChatMessage{
			Role:    "assistant",
			Content: restPart,
		}

	default:
		// Unknown action — treat everything after JSON as prose
		if restPart != "" {
			return &ChatMessage{Role: "assistant", Content: restPart}
		}
		return &ChatMessage{Role: "assistant", Content: text}
	}
}
