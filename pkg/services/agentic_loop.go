package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"YourQL/pkg/engine"
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
func buildTools(cfg *models.AgentLoopConfig, vizEnabled bool, schema *DataSchema, dbConnection *models.DataSource) []Tool {
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

	// On-demand schema tools: register list_tables and describe_table
	// when the user has opted in via the ForceSchemaTools checkbox, or when
	// the data source's schema size meets the global threshold (10 tables).
	// Below the threshold without the checkbox, the full schema is sent in
	// the system prompt instead — these tools are not needed.
	const globalSchemaToolThreshold = 10
	if schema != nil && dbConnection != nil {
		forceTools := false
		if cfg2, err := dbConnection.ParseConfig(); err == nil && cfg2.ForceSchemaTools {
			forceTools = true
		}
		if forceTools || len(schema.Tables) >= globalSchemaToolThreshold {
			tools = append(tools, Tool{
				Type: "function",
				Function: FunctionDef{
					Name:        "list_tables",
					Description: "List all tables in the database with their row counts. Use this to discover what tables are available before describing specific ones.",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				},
			}, Tool{
				Type: "function",
				Function: FunctionDef{
					Name:        "describe_table",
					Description: "Return the full column-level schema for a single table (column names, types, nullability, primary/foreign keys). Call this after list_tables to get the detail you need before writing a query.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"table_name": map[string]any{
								"type":        "string",
								"description": "The exact name of the table to describe (as returned by list_tables).",
							},
						},
						"required": []string{"table_name"},
					},
				},
			})
		}
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
		// Summarization runs on its OWN context and timeout so a slow main
		// loop can never starve it of the shared pipeline deadline (see
		// TIMEOUT_RESOLUTION.md). The timeout is configurable in Settings.
		sumTimeout := GetTimeoutSetting("summarization_timeout_seconds", 300)
		sCtx, sCancel := context.WithTimeout(context.Background(), time.Duration(sumTimeout)*time.Second)
		s, err := summarizeResults(sCtx, p.client, p.userMessage, p.sql, p.result, p.skillsContent, conversation.ID)
		sCancel()
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
		FinishReason     string            `json:"finish_reason,omitempty"`
		TextContent      string            `json:"text_content,omitempty"`
		RawOutput        string            `json:"raw_output,omitempty"`
		ToolCalls        []ToolCallSummary `json:"tool_calls,omitempty"`
		PromptTokens     int               `json:"prompt_tokens,omitempty"`
		CompletionTokens int               `json:"completion_tokens,omitempty"`
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
// formatToolResult — compact digest of query results for tool messages.
// Produces a line-oriented summary (row count, columns, up to 50 sample rows,
// stats for numeric columns) instead of a full markdown table. This keeps
// tool result messages small when replayed in conversation history, while
// still giving the model enough awareness to decide whether to re-query for
// exact data. The higher sample count (was 3) ensures the model sees enough
// distinct values from categorical lookups (e.g., SELECT DISTINCT status)
// to reason about schema semantics without burning exploration rounds.
// ---------------------------------------------------------------------------

const (
	toolResultSampleRows   = 50
	toolResultMaxCellChars = 200
)

func formatToolResult(result *QueryResult) string {
	if result == nil {
		return "(no results)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query returned %d row(s).", result.RowCount))

	if result.RowCount == 0 {
		return sb.String()
	}

	// Column names
	sb.WriteString("\nColumns:")
	for i, col := range result.Columns {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(" ")
		sb.WriteString(humanizeColumnName(col))
	}

	// Sample rows (up to toolResultSampleRows)
	sampleCount := len(result.Rows)
	if sampleCount > toolResultSampleRows {
		sampleCount = toolResultSampleRows
	}
	if sampleCount > 0 {
		sb.WriteString(fmt.Sprintf("\nSample (%d of %d):", sampleCount, result.RowCount))
		for i := 0; i < sampleCount; i++ {
			var rowParts []string
			for _, val := range result.Rows[i] {
				cell := fmt.Sprintf("%v", val)
				if len(cell) > toolResultMaxCellChars {
					cell = cell[:toolResultMaxCellChars] + "..."
				}
				rowParts = append(rowParts, cell)
			}
			sb.WriteString("\n  ")
			sb.WriteString(strings.Join(rowParts, "|"))
		}
	}

	// Numeric column stats (avg, min, max)
	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = humanizeColumnName(col)
	}
	numericCols := detectNumericColumns(result.Columns, result.Rows)
	if len(numericCols) > 0 {
		sb.WriteString("\nColumn stats:")
		for _, ci := range numericCols {
			stats := computeToolResultColStats(ci, result.Rows)
			sb.WriteString(fmt.Sprintf("\n  %s: avg=%.2f min=%.2f max=%.2f",
				humanizedCols[ci], stats.avg, stats.min, stats.max))
		}
	}

	return sb.String()
}

// detectNumericColumns returns the indices of columns where all non-NULL
// row values can be parsed as float64.
func detectNumericColumns(columns []string, rows [][]interface{}) []int {
	var numeric []int
	for ci := range columns {
		if len(rows) == 0 {
			continue
		}
		allNumeric := true
		hasValue := false
		for ri := range rows {
			val := rows[ri][ci]
			if val == nil {
				continue
			}
			hasValue = true
			switch v := val.(type) {
			case float64, float32:
			case int, int8, int16, int32, int64:
			case []byte:
				if _, err := strconv.ParseFloat(string(v), 64); err != nil {
					allNumeric = false
					break
				}
			case string:
				if _, err := strconv.ParseFloat(v, 64); err != nil {
					allNumeric = false
					break
				}
			default:
				allNumeric = false
				break
			}
			if !allNumeric {
				break
			}
		}
		if hasValue && allNumeric {
			numeric = append(numeric, ci)
		}
	}
	return numeric
}

type toolResultColStats struct {
	avg, min, max float64
}

func computeToolResultColStats(ci int, rows [][]interface{}) toolResultColStats {
	var sum float64
	var count int
	var min = math.MaxFloat64
	var max = -math.MaxFloat64
	for ri := range rows {
		v := toFloat64ForStats(rows[ri][ci])
		if v == nil {
			continue
		}
		sum += *v
		count++
		if *v < min {
			min = *v
		}
		if *v > max {
			max = *v
		}
	}
	if count == 0 {
		return toolResultColStats{}
	}
	return toolResultColStats{avg: sum / float64(count), min: min, max: max}
}

func toFloat64ForStats(val interface{}) *float64 {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case float64:
		return &v
	case float32:
		f := float64(v)
		return &f
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case int32:
		f := float64(v)
		return &f
	case []byte:
		if f, err := strconv.ParseFloat(string(v), 64); err == nil {
			return &f
		}
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
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
		HasChart:        chartConfig != "",
	}

	// When summarization is enabled, the summary text and the results
	// table with collapsible wrapper are both rendered by ToHTML(). Only
	// use textAboveTableHTML for respondText when no summary is present.
	var textAboveTableHTML string
	if summary == nil || *summary == "" {
		if respondText != "" {
			htmlText := renderMarkdown(respondText)
			if strings.TrimSpace(stripHTMLTags(htmlText)) == "" && respondText != "" {
				htmlText = fmt.Sprintf("<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>", html.EscapeString(respondText))
			}
			textAboveTableHTML = fmt.Sprintf("<div class=\"markdown-content\">%s</div>", htmlText)
		}
	}

	assistantMessageHTML := assistantResp.ToHTML()
	if textAboveTableHTML != "" {
		assistantMessageHTML = textAboveTableHTML + assistantMessageHTML
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
		} else if err != nil {
			log.Printf("[AgenticLoop] Chart config resolution failed: %v", err)
		} else {
			log.Printf("[AgenticLoop] Chart config resolution returned empty result")
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
			Function: ToolCallFunction{Name: action.Tool, Arguments: stripReasoningFromArgs(action.Arguments)},
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

// stripReasoningFromArgs removes the "reasoning" field from a JSON tool-call
// arguments string before it's replayed in conversation history. The reasoning
// is the model's own chain-of-thought justification for the query — valuable
// within the current turn, but dead weight on subsequent turns. If the
// arguments can't be parsed as JSON, the original string is returned unchanged.
func stripReasoningFromArgs(argsJSON string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return argsJSON
	}
	delete(m, "reasoning")
	trimmed, err := json.Marshal(m)
	if err != nil {
		return argsJSON
	}
	return string(trimmed)
}

// ---------------------------------------------------------------------------
// buildCompactSystemPrompt — minimal system prompt for compact mode
// ---------------------------------------------------------------------------

func buildCompactSystemPrompt(schema *DataSchema, dbConnection *models.DataSource, vizEnabled bool) string {
	var sb strings.Builder

	// Database identity
	sb.WriteString(dbConnection.Type)
	if dbConnection.Database != nil && *dbConnection.Database != "" {
		sb.WriteString(fmt.Sprintf(" — %s", *dbConnection.Database))
	}
	sb.WriteString("\n")

	// Compact table list (names + row counts only, no column detail)
	if schema != nil && len(schema.Tables) > 0 {
		sb.WriteString("Tables:")
		for i, table := range schema.Tables {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf(" %s(%d)", table.Name, table.RowCount))
		}
		sb.WriteString("\n")
	}

	// One-line dialect rule
	switch dbConnection.Type {
	case "MySQL", "mariadb":
		sb.WriteString("Dialect: backtick quoting, LIMIT 1000, INFORMATION_SCHEMA\n")
	case "PostgreSQL", "redshift":
		sb.WriteString("Dialect: double-quote identifiers, LIMIT 1000, information_schema\n")
	case "SQLite":
		sb.WriteString("Dialect: double-quote identifiers, LIMIT 1000, sqlite_master\n")
	case "SQL Server":
		sb.WriteString("Dialect: bracket [identifier] quoting, SELECT TOP 1000, INFORMATION_SCHEMA\n")
	default:
		sb.WriteString("Dialect: standard SQL quoting, LIMIT 1000\n")
	}

	// Tools (terse)
	if vizEnabled {
		sb.WriteString("Tools: query_database (SELECT only, is_exploration flag), respond_to_user, render_chart\n")
	} else {
		sb.WriteString("Tools: query_database (SELECT only, is_exploration flag), respond_to_user\n")
	}

	return sb.String()
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

	// Compact prompt mode: when enabled, return a minimal system prompt
	// that omits full column-level schemas, persona text, chart guidance,
	// and verbose instructions. Intended for small/local models where the
	// full prompt alone can exhaust context. Skills are not injected in
	// this mode (the caller handles that separately).
	if dbConnection != nil {
		config, err := dbConnection.ParseConfig()
		if err == nil && config.CompactPrompts {
			return buildCompactSystemPrompt(schema, dbConnection, vizEnabled)
		}
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
		// On-demand schema mode: when the user has opted in via
		// ForceSchemaTools or the schema meets the global threshold,
		// omit the full column-level schema and instruct the model
		// to use list_tables/describe_table tools instead.
		const globalSchemaToolThreshold = 10
		forceTools := false
		tmpCfg, tmpErr := dbConnection.ParseConfig()
		if tmpErr == nil && tmpCfg.ForceSchemaTools {
			forceTools = true
		}
		if forceTools || (len(schema.Tables) >= globalSchemaToolThreshold && tmpErr == nil && !tmpCfg.CompactPrompts) {
			// Skip the full schema section — the model will use tools instead.
			// Still include the table names + row counts so the model has
			// enough context to decide whether to call describe_table.
			var names []string
			for _, t := range schema.Tables {
				names = append(names, fmt.Sprintf("`%s` (%d rows)", t.Name, t.RowCount))
			}
			sb.WriteString(fmt.Sprintf("## Available Tables\n%s\n\n", strings.Join(names, "\n")))
			sb.WriteString("Use list_tables and describe_table tools to explore the schema before writing queries.\n")
		} else {
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
				if config.MaxToolsPerRound > 0 {
					sb.WriteString(fmt.Sprintf("- **Exploration:** enabled (max %d exploration queries total, max %d per round, %s mode)\n", config.MaxExplorationRounds, config.MaxToolsPerRound, safety))
				} else {
					sb.WriteString(fmt.Sprintf("- **Exploration:** enabled (max %d exploration queries total, %s mode)\n", config.MaxExplorationRounds, safety))
				}
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
			// Exploration messages are tech-detail debug data ("[Round N — ...]").
			// They must never be injected as system messages into LLM context —
			// models with strict chat templates (Qwen3, Llama 3, Mistral)
			// enforce that system messages must be at the very first position,
			// and will reject the entire request with a Jinja exception.
			if role == "exploration" {
				continue
			}
			if role == "assistant" && isErrorMessage(msg.Metadata) {
				continue
			}
			content := msg.Content
			if msg.LLMContent != nil && *msg.LLMContent != "" {
				content = *msg.LLMContent
			} else if role == "assistant" && strings.Contains(content, "<") {
				content = stripHTMLTags(content)
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
	schema *DataSchema,
	conversation *models.Conversation,
	maxExplorationRounds int,
	maxToolsPerRound int,
	maxErrorRetries int,
	safetyMode ExplorationSafetyMode,
	contextWindow *int,
	userMessage string,
	skillsContent string,
	onStream func(StreamEvent),
) error {
	cfg, cfgErr := GetAgentLoopConfig()
	if cfgErr != nil {
		return fmt.Errorf("failed to load agent loop config: %w", cfgErr)
	}
	tools := buildTools(cfg, conversation.VizEnabled, schema, dbConnection)

	var pendingFinal *pendingFinalResult
	var explorationResults []ExplorationResult
	var transcriptActions []TranscriptAction
	var explorationToolCallsUsed, errorRetriesUsed int
	var toolsThisRound int
	var lastQueryHadError bool
	var renderChartNeedsRetry bool // set when render_chart fails with "no pending" — gives model one more turn after query
	totalRoundCap := maxExplorationRounds + maxErrorRetries + 4
	loopStart := time.Now()
	log.Printf("[AgenticLoop] Starting loop — maxRounds=%d, maxTools=%d, maxRetries=%d, safety=%d, messages=%d",
		maxExplorationRounds, maxToolsPerRound, maxErrorRetries, safetyMode, len(messages))

	for round := 0; round < totalRoundCap; round++ {
		// Check for user-initiated cancellation before each round.
		if err := ctx.Err(); err != nil {
			return err
		}
		toolsThisRound = 0
		log.Printf("[AgenticLoop] Round %d — elapsed=%v, messages=%d, tools=%d",
			round, time.Since(loopStart).Round(time.Second), len(messages), len(tools))

		var response *ChatMessage
		var rawResponse string
		var llmErr error
		var streamChunks, streamBytes int
		if onStream != nil {
			wrappedStream := func(ev StreamEvent) {
				streamChunks++
				if ev.Type == StreamDone {
					streamBytes = streamChunks // conservative: 1 chunk ≈ 1 event line
				}
				onStream(ev)
			}
			response, _, rawResponse, llmErr = client.ChatCompletionWithToolsStreaming(ctx, messages, tools, wrappedStream)
		} else {
			response, _, rawResponse, llmErr = client.ChatCompletionWithTools(ctx, messages, tools)
		}
		if llmErr != nil {
			log.Printf("[AgenticLoop] LLM call FAILED round=%d elapsed=%v messages=%d: %v",
				round, time.Since(loopStart).Round(time.Second), len(messages), llmErr)
			return fmt.Errorf("LLM call failed: %w", llmErr)
		}

		kind := roundKindOther
		td := TechDetail{Version: 1, Round: round}
		roundStart := time.Now()

		// Snapshot response-level metadata once per round. logRound is
		// invoked once per tool call below, so anything that appends must
		// happen here — otherwise each logRound call re-appends the same
		// tool calls and the count shown in the tech-details toggle
		// double-counts them.
		td.Response.FinishReason = response.FinishReason
		if td.Response.FinishReason == "" {
			td.Response.FinishReason = "stop"
		}
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
		td.Response.RawOutput = rawResponse
		td.Response.PromptTokens = response.PromptTokens
		td.Response.CompletionTokens = response.CompletionTokens
		if streamChunks > 0 {
			td.Stream.ChunkCount = streamChunks
			td.Stream.ByteCount = streamBytes
		}

		// Store at most one tech-detail message per round. logRound fires
		// once per tool call (and once per event), so the store is guarded
		// by a per-round flag — otherwise a single round produces one
		// duplicate "[Round N — …]" message per tool call.
		storedThisRound := false
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
			td.DurationMs = int(time.Since(roundStart).Milliseconds())
			if !storedThisRound {
				storedThisRound = true
				storeTechDetail(conversation.ID, td)
			}
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
			// When summarization is on and the last query failed, reject
			// respond_to_user — the model must fix the error and retry.
			if conversation.Summarize && lastQueryHadError && pendingFinal == nil {
				logRound(roundKindOther)
				messages = append(messages, ChatMessage{
					Role:    "tool",
					Content: cfg.ResponseQueryErrorRequiresRetry,
				})
				continue
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
			// Classify the empty response for the harness: context_overflow
			// when prompt tokens are within 10%% of the model's context window
			// and 0 completion tokens were produced; empty_response otherwise.
			category := "empty_response"
			detail := "model returned 0 tokens and no tool calls"
			clarMsg := cfg.ResponseEmptyTruncated
			if response.PromptTokens > 0 && response.CompletionTokens == 0 &&
				contextWindow != nil && *contextWindow > 0 &&
				response.PromptTokens >= (*contextWindow * 9 / 10) {
				category = "context_overflow"
				detail = fmt.Sprintf("prompt %d tokens vs %d context limit; 0 completion tokens", response.PromptTokens, *contextWindow)
				clarMsg = cfg.ResponseContextOverflow
			}
			return handleClarification(query, LLMResponse{
				Action:                "clarification",
				ClarificationQuestion: clarMsg,
				FailureCategory:       category,
				FailureDetail:         detail,
			}, conversation.ID)
		}

		for toolIdx, tc := range response.ToolCalls {
			// Tools-per-round cap: reject excess query_database calls before
			// they reach the switch. Only the first maxToolsPerRound calls
			// execute; the rest get feedback and continue without running.
			if maxToolsPerRound > 0 && tc.Function.Name == "query_database" {
				toolsThisRound++
				if toolsThisRound > maxToolsPerRound {
					kind = roundKindOther
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    fmt.Sprintf(cfg.ResponseTooManyToolsPerRound, maxToolsPerRound, maxToolsPerRound),
					})
					continue
				}
			}

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
				if args.IsExploration && explorationToolCallsUsed >= maxExplorationRounds {
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
					if verr := engine.ValidateExplorationQuery(args.SQL, safetyMode); verr != nil {
						kind = roundKindExploration
						explorationToolCallsUsed++
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
					lastQueryHadError = false
				} else {
					toolContent = fmt.Sprintf("Query failed: %s", augmentSQLError(execErr.Error()))
				}

				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:          "query_database",
					Arguments:     tc.Function.Arguments,
					ResultPreview: toolContent,
				})

				if execErr != nil {
					lastQueryHadError = true
					// User-initiated cancellation — bail out immediately, do not render an error.
					if errors.Is(execErr, context.Canceled) {
						return context.Canceled
					}
					if args.IsExploration {
						kind = roundKindExploration
						explorationToolCallsUsed++
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

					// Capture the assistant's text content as a respond_to_user
					// action in the transcript for history replay. When the model
					// pairs explanatory text alongside query_database in the same
					// message, that text must be preserved — without it the
					// reconstructed history confuses the model on the next turn.
					//
					// When this query is a retry after a failed render_chart
					// (renderChartNeedsRetry), the model's text is typically
					// self-debugging monologue ("let me re-run this...") — not
					// user-facing content. Suppress respondText in that case.
					if response.Content != "" {
						argsJSON, _ := json.Marshal(map[string]string{"text": response.Content})
						transcriptActions = append(transcriptActions, TranscriptAction{
							Tool:      "respond_to_user",
							Arguments: string(argsJSON),
						})
						pendingFinal.transcript = buildTranscript(transcriptActions)
						if !renderChartNeedsRetry {
							pendingFinal.respondText = response.Content
						}
					}

					// Only skip the return if the model batched a render_chart
					// call alongside query_database in this same response —
					// process it inline on the next tool-call iteration.
					// If no chart call is present, return immediately to avoid
					// wasting a full extra LLM round-trip on an empty response.
					//
					// Exception: if the model previously attempted render_chart and
					// got a "no pending query result" error, give it one more turn
					// so it can call render_chart now that a result is available.
					hasChartInBatch := false
					if conversation.VizEnabled {
						for j := range response.ToolCalls {
							if response.ToolCalls[j].Function.Name == "render_chart" {
								hasChartInBatch = true
								break
							}
						}
					}
					if !hasChartInBatch {
						if renderChartNeedsRetry {
							renderChartNeedsRetry = false
						} else {
							return pendingFinal.render(query, dbConnection, conversation, "")
						}
					}
					continue
				}

				kind = roundKindExploration
				explorationToolCallsUsed++
				logRound(kind)

				// Accumulate for user-facing exploration trace
				explorationResults = append(explorationResults, ExplorationResult{
					SQL:       args.SQL,
					Result:    result,
					Round:     round + 1,
					Explained: args.Reasoning,
				})

			case "list_tables":
				kind = roundKindExploration
				logRound(kind)
				if schema == nil {
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    "Schema not available.",
					})
					continue
				}
				var parts []string
				for _, t := range schema.Tables {
					parts = append(parts, fmt.Sprintf("%s(%d)", t.Name, t.RowCount))
				}
				messages = append(messages, ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    fmt.Sprintf("Tables: %s", strings.Join(parts, ", ")),
				})
				continue

			case "describe_table":
				kind = roundKindExploration
				var describeArgs struct {
					TableName string `json:"table_name"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &describeArgs); err != nil || describeArgs.TableName == "" {
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    "Error: table_name is required.",
					})
					continue
				}
				logRound(kind)
				var found *TableInfo
				if schema != nil {
					for i := range schema.Tables {
						if strings.EqualFold(schema.Tables[i].Name, describeArgs.TableName) {
							found = &schema.Tables[i]
							break
						}
					}
				}
				if found == nil {
					var available []string
					if schema != nil {
						for _, t := range schema.Tables {
							available = append(available, t.Name)
						}
					}
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    fmt.Sprintf("Table '%s' not found. Available tables: %s", describeArgs.TableName, strings.Join(available, ", ")),
					})
					continue
				}
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("Table: `%s` (%d rows)\n", found.Name, found.RowCount))
				if found.Description != "" {
					sb.WriteString(fmt.Sprintf("  [comment: %s]\n", found.Description))
				}
				for _, col := range found.Columns {
					nullable := ""
					if col.IsNullable {
						nullable = " NULL"
					}
					pk := ""
					if col.IsPrimaryKey {
						pk = " PRIMARY KEY"
					}
					sb.WriteString(fmt.Sprintf("  - `%s`: %s%s%s\n", col.Name, col.DataType, nullable, pk))
				}
				messages = append(messages, ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    sb.String(),
				})
				continue

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
						renderChartNeedsRetry = true
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
					// However, if render_chart exists later in the same batch,
					// defer so it can consume pendingFinal with the real chart
					// config instead of an empty one (§SUMMARIZE_DATA_VIZ_CONFLICT).
					hasChartAfter := false
					for k := toolIdx + 1; k < len(response.ToolCalls); k++ {
						if response.ToolCalls[k].Function.Name == "render_chart" {
							hasChartAfter = true
							break
						}
					}
					if hasChartAfter {
						// Stash respondText for the transcript but do not
						// consume pendingFinal — let render_chart do it.
						pendingFinal.respondText = args.Text
						continue
					}
					pendingFinal.respondText = args.Text
					return pendingFinal.render(query, dbConnection, conversation, "")
				}

				// When summarization is on and the last query failed,
				// reject respond_to_user — the model must fix the error.
				if conversation.Summarize && lastQueryHadError {
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    cfg.ResponseQueryErrorRequiresRetry,
					})
					continue
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
		FailureCategory:       "loop_exhausted",
		FailureDetail:         "agentic loop reached the round limit",
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
