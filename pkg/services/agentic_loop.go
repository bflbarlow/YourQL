package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

// buildTools returns the tool definitions for the agentic loop, reading
// descriptions from AgentLoopConfig (user overrides or hardcoded defaults).
func buildTools(cfg *models.AgentLoopConfig, vizEnabled bool) []Tool {
	tools := []Tool{
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "query_database",
				Description: cfg.ToolQueryDatabaseDesc,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"sql": map[string]any{
							"type":        "string",
							"description": cfg.ToolQueryDatabaseSQLDesc,
						},
						"is_exploration": map[string]any{
							"type":        "boolean",
							"description": cfg.ToolQueryDatabaseIsExplorationDesc,
						},
						"reasoning": map[string]any{
							"type":        "string",
							"description": cfg.ToolQueryDatabaseReasoningDesc,
						},
					},
					"required": []string{"sql"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "respond_to_user",
				Description: cfg.ToolRespondToUserDesc,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{
							"type":        "string",
							"description": cfg.ToolRespondToUserTextDesc,
						},
					},
					"required": []string{"text"},
				},
			},
		},
	}
	if vizEnabled {
		tools = append(tools, Tool{
			Type: "function",
			Function: FunctionDef{
				Name:        "render_chart",
				Description: cfg.ToolRenderChartDesc,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"chart_config": map[string]any{
							"type":        "string",
							"description": cfg.ToolRenderChartConfigDesc,
						},
					},
					"required": []string{"chart_config"},
				},
			},
		})
	}
	return tools
}

// ---------------------------------------------------------------------------
// Round accounting
// ---------------------------------------------------------------------------

type roundKind string

const (
	roundKindExploration roundKind = "exploration"
	roundKindErrorRetry  roundKind = "error_retry"
	roundKindChart       roundKind = "chart"
	roundKindFinal       roundKind = "final"
	roundKindOther       roundKind = "other"
)

// ---------------------------------------------------------------------------
// pendingFinalResult — held between a successful final query_database and
// the model's optional render_chart / respond_to_user follow-up
// ---------------------------------------------------------------------------

type pendingFinalResult struct {
	sql                string
	result             *QueryResult
	explorationResults []ExplorationResult
	transcript         *ToolTranscript
	// When the model batches respond_to_user alongside query_database (§3),
	// the text is stashed here and rendered above the results table in one
	// combined message — one round-trip, one chat bubble.
	respondText string
	// Carried through so render() can optionally call summarizeResults when
	// conversation.Summarize is enabled — the tool-calling path must honor
	// this setting exactly as the old protocol's executeFinalQueryWithRetry
	// did, or the toggle silently becomes a no-op.
	ctx           context.Context
	client        LLMClient
	userMessage   string
	skillsContent string
}

// render is the single choke point for delivering a pending final result.
// Every exit path in the loop calls through here, charted or chartless.
func (p *pendingFinalResult) render(query *models.Query, dbConnection *models.DataSource, conversation *models.Conversation, chartConfig string) error {
	var summary *string
	if conversation.Summarize && p.result != nil {
		s, err := summarizeResults(p.ctx, p.client, p.userMessage, p.sql, p.result, p.skillsContent, conversation.ID)
		if err != nil {
			log.Printf("[AgenticLoop] Summarization failed: %v", err)
			// Surface a small note to the user instead of silent failure.
			fallback := "⚠️ Summary unavailable — the model could not generate one for this result set."
			summary = &fallback
		} else if s != "" {
			summary = &s
		}
	}
	return renderToolQueryResults(query, p.sql, p.result, chartConfig, dbConnection, conversation, p.explorationResults, p.transcript, summary, p.respondText)
}

// ---------------------------------------------------------------------------
// ToolTranscript — canonical, provider-neutral record of tool activity
// ---------------------------------------------------------------------------

// ToolTranscript is the canonical, provider-neutral record of one
// assistant turn's tool activity. Never contains provider-specific IDs or
// wire-format shape — those are synthesized fresh by buildToolMessages
// every time history is replayed.
type ToolTranscript struct {
	Version int                `json:"version"`
	Actions []TranscriptAction `json:"actions"`
}

// TranscriptAction records a single tool call the model made during a turn.
type TranscriptAction struct {
	Tool          string `json:"tool"`                     // "query_database", "render_chart", "respond_to_user"
	Arguments     string `json:"arguments"`                // exact JSON arguments the model produced
	ResultPreview string `json:"result_preview,omitempty"` // truncated preview of the result
}

// ---------------------------------------------------------------------------
// TechDetail — structured per-round debugging data for the tech-details toggle
// ---------------------------------------------------------------------------

type TechDetail struct {
	Version int    `json:"version"` // 1
	Round   int    `json:"round"`   // 0-based
	Kind    string `json:"kind"`    // "final", "exploration", "error_retry", "chart", "other"

	Request struct {
		MessageCount int    `json:"message_count"`
		LastUserMsg  string `json:"last_user_msg,omitempty"`
		RawMessages  string `json:"raw_messages,omitempty"` // full conversation JSON, untruncated
	} `json:"request"`

	Response struct {
		FinishReason string            `json:"finish_reason,omitempty"`
		TextContent  string            `json:"text_content,omitempty"`
		RawOutput    string            `json:"raw_output,omitempty"` // full raw model response text
		ToolCalls    []ToolCallSummary `json:"tool_calls,omitempty"`
	} `json:"response"`

	SQL struct {
		Query      string `json:"query,omitempty"`
		ExecTimeMs int    `json:"exec_time_ms,omitempty"`
		RowCount   int    `json:"row_count,omitempty"`
		Error      string `json:"error,omitempty"`
	} `json:"sql,omitempty"`

	Stream struct {
		ChunkCount int    `json:"chunk_count,omitempty"`
		ByteCount  int    `json:"byte_count,omitempty"`
		DebugFile  string `json:"debug_file,omitempty"`
	} `json:"stream,omitempty"`

	DurationMs int `json:"duration_ms"`
}

type ToolCallSummary struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"` // full arguments JSON, untruncated
}

func storeTechDetail(conversationID uint, detail TechDetail) {
	detailJSON, _ := json.Marshal(detail)
	detailStr := string(detailJSON)
	label := fmt.Sprintf("[Round %d — %s]", detail.Round, detail.Kind)
	_, _ = CreateConversationMessage(conversationID, "exploration", label, nil, nil, &detailStr)
}

// ---------------------------------------------------------------------------
// formatToolResult — truncated preview of query results for tool messages
// ---------------------------------------------------------------------------

const (
	toolResultMaxRows      = 50
	toolResultMaxCellChars = 200
)

func formatToolResult(result *QueryResult) string {
	if result == nil {
		return "(no results)"
	}
	if result.RowCount == 0 {
		return "(0 rows returned)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query returned %d row(s).\n\n", result.RowCount))

	sb.WriteString("| ")
	for i, col := range result.Columns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(humanizeColumnName(col))
	}
	sb.WriteString(" |\n")
	sb.WriteString("|" + strings.Repeat("---|", len(result.Columns)) + "\n")

	for i, row := range result.Rows {
		if i >= toolResultMaxRows {
			sb.WriteString(fmt.Sprintf("\n… %d more row(s) not shown\n", result.RowCount-i))
			break
		}
		sb.WriteString("| ")
		for j, val := range row {
			if j > 0 {
				sb.WriteString(" | ")
			}
			cell := fmt.Sprintf("%v", val)
			if len(cell) > toolResultMaxCellChars {
				cell = cell[:toolResultMaxCellChars] + "..."
			}
			sb.WriteString(cell)
		}
		sb.WriteString(" |\n")
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// handleRespond — renders a respond_to_user tool call
// ---------------------------------------------------------------------------

func handleRespond(query *models.Query, text string, conversationID uint, explorationResults []ExplorationResult, transcript *ToolTranscript) error {
	if err := UpdateQueryStatus(query.ID, "answer", nil, nil, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}

	htmlContent := renderMarkdown(text)

	// Fallback: if markdown produced no usable content, show raw text as preformatted
	if strings.TrimSpace(stripHTMLTags(htmlContent)) == "" && text != "" {
		htmlContent = fmt.Sprintf("<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>", html.EscapeString(text))
	}

	// Render exploration trace if any
	var explorationHTML string
	if len(explorationResults) > 0 {
		explorationHTML = formatExplorationHTML(explorationResults)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<div class=\"markdown-content\">%s</div>", htmlContent))
	if explorationHTML != "" {
		sb.WriteString(explorationHTML)
	}

	metadataJSON, _ := json.Marshal(map[string]interface{}{"content_type": "html"})
	metadata := string(metadataJSON)

	_, err := CreateConversationMessage(conversationID, "assistant", sb.String(), nil, nil, &metadata)
	if err != nil {
		return fmt.Errorf("failed to create respond message: %w", err)
	}

	// Persist the tool transcript for history replay (§6.2)
	if transcript != nil {
		tj, _ := json.Marshal(transcript)
		ts := string(tj)
		_, _ = models.DB.Exec("UPDATE conversation_messages SET tool_transcript = ? WHERE id = (SELECT MAX(id) FROM conversation_messages WHERE conversation_id = ? AND role = 'assistant')", ts, conversationID)
	}

	return nil
}

// ---------------------------------------------------------------------------
// renderToolQueryResults — renders a final query result, optionally charted
// ---------------------------------------------------------------------------

func renderToolQueryResults(query *models.Query, sql string, results *QueryResult, chartConfig string, dbConnection *models.DataSource, conversation *models.Conversation, explorationResults []ExplorationResult, transcript *ToolTranscript, summary *string, respondText string) error {
	var explorationHTML string
	if len(explorationResults) > 0 {
		explorationHTML = formatExplorationHTML(explorationResults)
	}

	assistantResp := AssistantResponse{
		SQL:             sql,
		Result:          results,
		ExplorationHTML: explorationHTML,
		Summary:         summary,
	}
	assistantMessageHTML := assistantResp.ToHTML()

	// When the model batches respond_to_user alongside query_database,
	// render the markdown text above the results table in a single combined
	// message — saves one full round-trip.
	if respondText != "" {
		htmlText := renderMarkdown(respondText)
		if strings.TrimSpace(stripHTMLTags(htmlText)) == "" && respondText != "" {
			htmlText = fmt.Sprintf("<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>", html.EscapeString(respondText))
		}
		assistantMessageHTML = fmt.Sprintf("<div class=\"markdown-content\">%s</div>%s", htmlText, assistantMessageHTML)
	}

	llmContent := "" // no raw LLM response JSON in the tool-calling path
	llmContentPtr := &llmContent

	sqlResultsJSON, err := json.Marshal(results)
	if err != nil {
		sqlResultsJSON = nil
	}
	sqlResultsPtr := stringPtr(string(sqlResultsJSON))

	metadataMap := map[string]interface{}{"content_type": "html"}

	// Resolve chart config if present and enabled
	if conversation.VizEnabled && chartConfig != "" && results != nil && len(results.Columns) > 0 {
		resolved, err := resolveChartConfig(chartConfig, results.Columns, results.Rows)
		if err == nil && resolved != "" {
			metadataMap["chart_config"] = json.RawMessage(resolved)
		}
	}

	metadataJSON, _ := json.Marshal(metadataMap)
	metadata := string(metadataJSON)

	resultSummary := formatResults(results)

	if err := UpdateQueryStatus(query.ID, "success", &sql, &resultSummary, nil, nil, nil, nil); err != nil {
		log.Printf("[AgenticLoop] Failed to update query status: %v", err)
	}

	_, err = CreateConversationMessage(conversation.ID, "assistant", assistantMessageHTML, llmContentPtr, sqlResultsPtr, &metadata)
	if err != nil {
		return fmt.Errorf("failed to create result message: %w", err)
	}

	// Note: the exploration trace is already embedded as collapsible HTML
	// inside the assistant message above (ExplorationHTML) — matching the
	// old protocol's behavior. It is intentionally NOT also persisted as
	// separate role:"exploration" rows here: those rows exist in the old
	// protocol purely for debug-payload logging and are filtered out of the
	// UI unless tech details are enabled. Duplicating them here would feed
	// the model redundant context on replay (on top of the tool_transcript,
	// which already captures the exact query_database calls).

	// Persist the tool transcript for history replay (§6.2)
	if transcript != nil {
		tj, _ := json.Marshal(transcript)
		ts := string(tj)
		_, _ = models.DB.Exec("UPDATE conversation_messages SET tool_transcript = ? WHERE id = (SELECT MAX(id) FROM conversation_messages WHERE conversation_id = ? AND role = 'assistant')", ts, conversation.ID)
	}

	return nil
}

// ---------------------------------------------------------------------------
// buildToolMessages — converts a canonical ToolTranscript into the ChatMessage
// sequence the currently configured provider needs, with freshly minted IDs
// ---------------------------------------------------------------------------

func buildToolMessages(t ToolTranscript) []ChatMessage {
	if t.Version != 1 && t.Version != 0 {
		// Unrecognized version — degrade to plain-text summary
		var sb strings.Builder
		for _, a := range t.Actions {
			if a.Tool == "respond_to_user" {
				sb.WriteString(a.Arguments)
				break
			}
		}
		if sb.Len() == 0 {
			sb.WriteString("(prior conversation turn)")
		}
		return []ChatMessage{{Role: "assistant", Content: sb.String()}}
	}

	var out []ChatMessage
	var calls []ToolCall
	var finalRespondText string

	for i, action := range t.Actions {
		if action.Tool == "respond_to_user" {
			// Extract text from the arguments JSON
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(action.Arguments), &args); err == nil {
				finalRespondText = args.Text
			}
			continue // rides on the assistant message's Content, not a tool call
		}
		calls = append(calls, ToolCall{
			ID:       fmt.Sprintf("hist_%d", i),
			Function: ToolCallFunction{Name: action.Tool, Arguments: action.Arguments},
		})
	}

	// Assistant message with any tool calls
	assistantMsg := ChatMessage{Role: "assistant", Content: finalRespondText, ToolCalls: calls}
	out = append(out, assistantMsg)

	// One tool result message per non-respond_to_user action
	for i, action := range t.Actions {
		if action.Tool == "respond_to_user" {
			continue
		}
		out = append(out, ChatMessage{
			Role:       "tool",
			Content:    action.ResultPreview,
			ToolCallID: fmt.Sprintf("hist_%d", i),
			Name:       action.Tool,
		})
	}

	return out
}

// ---------------------------------------------------------------------------
// buildToolSystemPrompt — system prompt for the tool-calling protocol
// ---------------------------------------------------------------------------

func buildToolSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, vizEnabled bool, skillsContent string) string {
	var sb strings.Builder

	// Load agent loop config for user-overridable prompt sections.
	cfg, cfgErr := GetAgentLoopConfig()
	if cfgErr != nil {
		cfg = &models.AgentLoopConfig{} // fall back to zero values (hardcoded defaults will be used)
	}

	if dbConnection != nil {
		config, err := dbConnection.ParseConfig()
		if err == nil && config.SystemPrompt != "" {
			sb.WriteString(config.SystemPrompt)
			sb.WriteString("\n\n")
		}
		if err == nil && len(config.BusinessRules) > 0 {
			sb.WriteString("## Business Rules\n")
			for _, rule := range config.BusinessRules {
				sb.WriteString(fmt.Sprintf("- %s\n", rule))
			}
			sb.WriteString("\n")
		}
	}

	if sb.Len() == 0 {
		sb.WriteString(cfg.PersonaFallback)
	} else {
		sb.WriteString("\n")
	}

	if hasDB && schema != nil && len(schema.Tables) > 0 {
		sb.WriteString("## Database Schema\n")
		var config *models.DataSourceConfig
		var configErr error
		if dbConnection != nil {
			config, configErr = dbConnection.ParseConfig()
		}
		var tableDescriptions map[string]string
		var columnDescriptions map[string]string
		if configErr == nil && config != nil {
			tableDescriptions = config.TableDescriptions
			columnDescriptions = config.ColumnDescriptions
		}
		for _, table := range schema.Tables {
			sb.WriteString(fmt.Sprintf("Table: `%s` (%d rows)", table.Name, table.RowCount))
			if table.Description != "" {
				sb.WriteString(fmt.Sprintf(" [comment: %s]", table.Description))
			}
			if desc, ok := tableDescriptions[table.Name]; ok {
				sb.WriteString(fmt.Sprintf(" [description: %s]", desc))
			}
			sb.WriteString("\n")

			for _, col := range table.Columns {
				nullable := ""
				if col.IsNullable {
					nullable = " NULL"
				}
				pk := ""
				if col.IsPrimaryKey {
					pk = " PRIMARY KEY"
				}
				colDesc := ""
				if desc, ok := columnDescriptions[table.Name+"."+col.Name]; ok {
					colDesc = fmt.Sprintf(" [description: %s]", desc)
				}
				sb.WriteString(fmt.Sprintf("  - `%s`: %s%s%s%s\n", col.Name, col.DataType, nullable, pk, colDesc))
			}

			if len(table.Indexes) > 0 {
				sb.WriteString("  Indexes:\n")
				for _, idx := range table.Indexes {
					unique := ""
					if idx.IsUnique {
						unique = " UNIQUE"
					}
					sb.WriteString(fmt.Sprintf("    - %s%s: (%s)\n", idx.Name, unique, strings.Join(idx.Columns, ", ")))
				}
			}

			if len(table.ForeignKeys) > 0 {
				sb.WriteString("  Foreign Keys:\n")
				for _, fk := range table.ForeignKeys {
					sb.WriteString(fmt.Sprintf("    - %s: %s -> `%s`.`%s`", fk.Name, fk.Column, fk.RefTable, fk.RefColumn))
					if fk.OnDelete != "" {
						sb.WriteString(fmt.Sprintf(" ON DELETE %s", fk.OnDelete))
					}
					if fk.OnUpdate != "" {
						sb.WriteString(fmt.Sprintf(" ON UPDATE %s", fk.OnUpdate))
					}
					sb.WriteString("\n")
				}
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if hasDB && dbConnection != nil {
		driver, driverErr := GetDriver(dbConnection.Type)
		dbName := dbConnection.Name
		if dbName == "" {
			dbName = dbConnection.Type
		}

		sb.WriteString("## Database Connection\n")
		sb.WriteString(fmt.Sprintf("- **Connection name:** %s\n", dbName))

		if driverErr == nil {
			displayName := driver.DisplayName()
			sb.WriteString(fmt.Sprintf("- **Database type:** %s\n", displayName))
			dialect := driver.SQLDialectHint()
			if dialect != "" {
				sb.WriteString(fmt.Sprintf("- **Dialect rules:** %s\n", dialect))
			}
		} else {
			sb.WriteString(fmt.Sprintf("- **Database type:** %s\n", dbConnection.Type))
		}

		config, cfgErr := dbConnection.ParseConfig()
		if cfgErr == nil {
			if config.DefaultLimit > 0 {
				sb.WriteString(fmt.Sprintf("- **Row limit:** %d rows maximum per query (a LIMIT clause is automatically added if missing)\n", config.DefaultLimit))
			} else {
				sb.WriteString("- **Row limit:** 1000 rows maximum per query\n")
			}
			if config.ExplorationAllowed {
				safety := config.ExplorationSafety
				if safety == "" {
					safety = "strict"
				}
				sb.WriteString(fmt.Sprintf("- **Exploration:** enabled (max %d rounds, %s mode)\n", config.MaxExplorationRounds, safety))
			} else {
				sb.WriteString("- **Exploration:** disabled\n")
			}
		}

		// Surface database-specific connection details (host, port, database name)
		// to help the model understand the environment without exposing credentials.
		if dbConnection.Host != nil && *dbConnection.Host != "" {
			sb.WriteString(fmt.Sprintf("- **Host:** %s", *dbConnection.Host))
			if dbConnection.Port != nil && *dbConnection.Port > 0 {
				sb.WriteString(fmt.Sprintf(":%d", *dbConnection.Port))
			}
			sb.WriteString("\n")
		}
		if dbConnection.Database != nil && *dbConnection.Database != "" {
			sb.WriteString(fmt.Sprintf("- **Database name:** %s\n", *dbConnection.Database))
		}

		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString(cfg.Instruction1)
	sb.WriteString(cfg.Instruction2)
	sb.WriteString(cfg.Instruction2a)
	sb.WriteString(cfg.Instruction2b)
	if vizEnabled {
		sb.WriteString(cfg.Instruction2c)
	}
	sb.WriteString(cfg.Instruction3)
	sb.WriteString(cfg.Instruction4)
	sb.WriteString(cfg.Instruction5)
	sb.WriteString(cfg.Instruction6)

	if dbConnection != nil {
		config, cfgErr := dbConnection.ParseConfig()
		if cfgErr == nil && config.ExplorationAllowed {
			sb.WriteString(cfg.SafetyPreamble)
			sb.WriteString(fmt.Sprintf(coalesceConfig(nil, "safety.preamble_detail"), config.ExplorationSafety))
			switch config.ExplorationSafety {
			case "strict":
				sb.WriteString(cfg.SafetyStrict)
			case "moderate":
				sb.WriteString(cfg.SafetyModerate)
			case "relaxed":
				sb.WriteString(cfg.SafetyRelaxed)
			}
			sb.WriteString(cfg.SafetyFooter)
			sb.WriteString(fmt.Sprintf(coalesceConfig(nil, "safety.footer_rounds"), config.MaxExplorationRounds))
			sb.WriteString(coalesceConfig(nil, "safety.footer_rejected"))
			sb.WriteString(cfg.SafetyOneshot)
		}
	}

	if vizEnabled {
		sb.WriteString(cfg.ChartsIntro)
		sb.WriteString(coalesceConfig(nil, "charts.intro_detail"))
		sb.WriteString(cfg.ChartsFormat)
		sb.WriteString(cfg.ChartsTiming)
	}

	if skillsContent != "" {
		sb.WriteString("\n## Additional Context (from Skills)\n")
		sb.WriteString(skillsContent)
		sb.WriteString("\n")
	}

	prompt := sb.String()
	if len(prompt) > 16384 {
		prompt = prompt[:16000] + "\n\n[Note: schema truncated due to context limits. The user's question follows below.]\n"
	}
	return prompt
}

// ---------------------------------------------------------------------------
// buildToolLlmMessages — builds the message array for the tool-calling protocol
// ---------------------------------------------------------------------------

func buildToolLlmMessages(userMessage string, history []*models.ConversationMessage, schema *DataSchema, dbConnection *models.DataSource, vizEnabled bool, skillsContent string) []ChatMessage {
	messages := []ChatMessage{}

	hasDB := dbConnection != nil
	systemPrompt := buildToolSystemPrompt(schema, hasDB, dbConnection, vizEnabled, skillsContent)
	messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})

	for _, msg := range history {
		// If this message has a tool_transcript, replay it as tool-call
		// messages instead of the plain-content path
		if msg.ToolTranscript != nil && *msg.ToolTranscript != "" {
			var t ToolTranscript
			if err := json.Unmarshal([]byte(*msg.ToolTranscript), &t); err == nil {
				messages = append(messages, buildToolMessages(t)...)
				continue
			}
		}

		role := msg.Role
		if role == "user" || role == "assistant" || role == "exploration" {
			if role == "assistant" && isErrorMessage(msg.Metadata) {
				continue
			}
			content := msg.Content
			if msg.LLMContent != nil && *msg.LLMContent != "" {
				content = *msg.LLMContent
			} else if role == "assistant" && strings.Contains(content, "<") {
				content = stripHTMLTags(content)
			}
			if role == "exploration" {
				role = "system"
			}
			messages = append(messages, ChatMessage{Role: role, Content: content})

			if role == "assistant" && msg.SQLResults != nil && *msg.SQLResults != "" {
				sqlContext := formatSQLResultsForLLM(*msg.SQLResults)
				if sqlContext != "" {
					messages = append(messages, ChatMessage{
						Role:    "system",
						Content: sqlContext,
					})
				}
			}
		}
	}

	messages = append(messages, ChatMessage{Role: "user", Content: userMessage})
	return messages
}

// ---------------------------------------------------------------------------
// runAgenticLoop — the core tool-calling execution loop
// ---------------------------------------------------------------------------

func runAgenticLoop(
	ctx context.Context,
	query *models.Query,
	client LLMClient,
	messages []ChatMessage,
	dbConnection *models.DataSource,
	conversation *models.Conversation,
	maxExplorationRounds int,
	maxErrorRetries int,
	safetyMode ExplorationSafetyMode,
	userMessage string,
	skillsContent string,
	onStream func(StreamEvent),
) error {
	cfg, cfgErr := GetAgentLoopConfig()
	if cfgErr != nil {
		return fmt.Errorf("failed to load agent loop config: %w", cfgErr)
	}
	tools := buildTools(cfg, conversation.VizEnabled)

	var pendingFinal *pendingFinalResult
	var explorationResults []ExplorationResult
	var transcriptActions []TranscriptAction
	var explorationRoundsUsed, errorRetriesUsed int
	totalRoundCap := maxExplorationRounds + maxErrorRetries + 4

	for round := 0; round < totalRoundCap; round++ {
		// Check for user-initiated cancellation before each round.
		if err := ctx.Err(); err != nil {
			return err
		}

		var response *ChatMessage
		var rawResponse string
		var llmErr error
		if onStream != nil {
			response, _, rawResponse, llmErr = client.ChatCompletionWithToolsStreaming(ctx, messages, tools, onStream)
		} else {
			response, _, rawResponse, llmErr = client.ChatCompletionWithTools(ctx, messages, tools)
		}
		if llmErr != nil {
			return fmt.Errorf("LLM call failed: %w", llmErr)
		}

		kind := roundKindOther
		td := TechDetail{Version: 1, Round: round}
		roundStart := time.Now()
		logRound := func(k roundKind) {
			kind = k
			td.Kind = string(k)
			td.Request.MessageCount = len(messages)
			// Find the last user message for context
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					td.Request.LastUserMsg = truncateString(messages[i].Content, 200)
					break
				}
			}
			// Store the full conversation messages as JSON (untruncated — §TODO full transparency)
			if msgJSON, err := json.Marshal(messages); err == nil {
				td.Request.RawMessages = string(msgJSON)
			}
			td.Response.FinishReason = "stop"
			if len(response.ToolCalls) > 0 {
				td.Response.FinishReason = "tool_calls"
				for _, tc := range response.ToolCalls {
					td.Response.ToolCalls = append(td.Response.ToolCalls, ToolCallSummary{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					})
				}
			} else if response.Content != "" {
				td.Response.TextContent = response.Content
			}
			td.DurationMs = int(time.Since(roundStart).Milliseconds())
			td.Response.RawOutput = rawResponse
			storeTechDetail(conversation.ID, td)
		}

		// Append the assistant turn before any "tool" role replies
		messages = append(messages, *response)

		// Plain text instead of a tool call — treat as respond_to_user.
		// When there's a pending final result, merge into one combined
		// message (text above table) instead of creating two separate
		// messages, consistent with the batched tool-call path.
		if response.Content != "" && len(response.ToolCalls) == 0 {
			logRound(roundKindOther)
			if pendingFinal != nil {
				argsJSON, _ := json.Marshal(map[string]string{"text": response.Content})
				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:      "respond_to_user",
					Arguments: string(argsJSON),
				})
				pendingFinal.transcript = buildTranscript(transcriptActions)
				pendingFinal.respondText = response.Content
				return pendingFinal.render(query, dbConnection, conversation, "")
			}
			transcript := buildTranscript(transcriptActions)
			return handleRespond(query, response.Content, conversation.ID, explorationResults, transcript)
		}

		// Empty response — truncated or malformed
		if len(response.ToolCalls) == 0 {
			logRound(roundKindOther)
			if pendingFinal != nil {
				if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
					return rerr
				}
				return nil
			}
			return handleClarification(query, LLMResponse{
				Action:                "clarification",
				ClarificationQuestion: cfg.ResponseEmptyTruncated,
			}, conversation.ID)
		}

		for _, tc := range response.ToolCalls {
			switch tc.Function.Name {
			case "query_database":
				if pendingFinal != nil {
					// One-shot finality: a surfaced result already exists.
					// Reject all further query_database calls — the session is closed.
					kind = roundKindOther
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
						Content: cfg.ResponseOneshotViolation,
					})
					continue
				}

				var args struct {
					SQL           string `json:"sql"`
					IsExploration bool   `json:"is_exploration"`
					Reasoning     string `json:"reasoning,omitempty"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					kind = roundKindOther
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
						Content: fmt.Sprintf(cfg.ResponseParseError, err),
					})
					continue
				}

				// Exploration budget check
				if args.IsExploration && explorationRoundsUsed >= maxExplorationRounds {
					kind = roundKindExploration
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
						Content: cfg.ResponseExplorationExhausted,
					})
					continue
				}

				// Exploration safety gate
				if args.IsExploration {
					if verr := validateExplorationQuery(args.SQL, safetyMode); verr != nil {
						kind = roundKindExploration
						explorationRoundsUsed++
						logRound(kind)
						messages = append(messages, ChatMessage{
							Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
							Content: fmt.Sprintf(cfg.ResponseSafetyRejected, verr.Error()),
						})
						continue
					}
				}

				result, execErr := executeSQLWithMode(dbConnection, args.SQL, args.IsExploration)
				toolContent := ""
				if execErr == nil {
					toolContent = formatToolResult(result)
				} else {
					toolContent = fmt.Sprintf("Query failed: %v", execErr)
				}

				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:          "query_database",
					Arguments:     tc.Function.Arguments,
					ResultPreview: toolContent,
				})

				if execErr != nil {
					// User-initiated cancellation — bail out immediately, do not render an error.
					if errors.Is(execErr, context.Canceled) {
						return context.Canceled
					}
					if args.IsExploration {
						kind = roundKindExploration
						explorationRoundsUsed++
					} else {
						kind = roundKindErrorRetry
						if errorRetriesUsed >= maxErrorRetries {
							logRound(kind)
							renderSQLError(query, LLMResponse{SQLQuery: args.SQL}, dbConnection, conversation.ID, explorationResults, execErr)
							return nil
						}
						errorRetriesUsed++
						if !isRetryableError(execErr) {
							logRound(kind)
							renderSQLError(query, LLMResponse{SQLQuery: args.SQL}, dbConnection, conversation.ID, explorationResults, execErr)
							return nil
						}
					}
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: toolContent,
					})
					continue
				}

				messages = append(messages, ChatMessage{
					Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: toolContent,
				})

				if !args.IsExploration {
					kind = roundKindFinal
					logRound(kind)
					pendingFinal = &pendingFinalResult{
						sql:                args.SQL,
						result:             result,
						explorationResults: explorationResults,
						transcript:         buildTranscript(transcriptActions),
						ctx:                ctx,
						client:             client,
						userMessage:        userMessage,
						skillsContent:      skillsContent,
					}
					if !conversation.VizEnabled {
						return pendingFinal.render(query, dbConnection, conversation, "")
					}
					continue // give the model a round to optionally call render_chart
				}

				kind = roundKindExploration
				explorationRoundsUsed++
				logRound(kind)

				// Accumulate for user-facing exploration trace
				explorationResults = append(explorationResults, ExplorationResult{
					SQL:       args.SQL,
					Result:    result,
					Round:     round + 1,
					Explained: args.Reasoning,
				})

			case "render_chart":
				kind = roundKindChart
				var args struct {
					ChartConfig string `json:"chart_config"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || pendingFinal == nil {
					if err != nil {
						messages = append(messages, ChatMessage{
							Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
							Content: fmt.Sprintf(cfg.ResponseRenderChartParseError, err),
						})
					} else {
						messages = append(messages, ChatMessage{
							Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
							Content: cfg.ResponseRenderChartNoPending,
						})
					}
					continue
				}
				logRound(kind)
				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:      "render_chart",
					Arguments: tc.Function.Arguments,
				})
				result := pendingFinal.render(query, dbConnection, conversation, args.ChartConfig)
				pendingFinal = nil
				return result

			case "respond_to_user":
				kind = roundKindOther
				var args struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					messages = append(messages, ChatMessage{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
						Content: fmt.Sprintf(cfg.ResponseRespondParseError, err),
					})
					continue
				}
				logRound(kind)
				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:      "respond_to_user",
					Arguments: tc.Function.Arguments,
				})
				if pendingFinal != nil {
					// Batched: render the respond text above the results table
					// in a single combined message — one round-trip, one bubble.
					pendingFinal.respondText = args.Text
					return pendingFinal.render(query, dbConnection, conversation, "")
				}
				transcript := buildTranscript(transcriptActions)
				return handleRespond(query, args.Text, conversation.ID, explorationResults, transcript)

			default:
				kind = roundKindOther
				logRound(kind)
				messages = append(messages, ChatMessage{
					Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
					Content: fmt.Sprintf(cfg.ResponseUnknownTool, tc.Function.Name),
				})
			}
		}
	}

	// Max rounds exhausted — render any pending result, then surface the error
	if pendingFinal != nil {
		return pendingFinal.render(query, dbConnection, conversation, "")
	}
	return handleClarification(query, LLMResponse{
		Action:                "clarification",
		ClarificationQuestion: cfg.ResponseLoopExhausted,
	}, conversation.ID)
}

// buildTranscript creates a new ToolTranscript from accumulated actions.
func buildTranscript(actions []TranscriptAction) *ToolTranscript {
	if len(actions) == 0 {
		return nil
	}
	copied := make([]TranscriptAction, len(actions))
	copy(copied, actions)
	return &ToolTranscript{Version: 1, Actions: copied}
}
