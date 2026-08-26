package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// truncateString truncates a string to maxLen with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ProcessUserMessageWithContext processes a user message with an externally
// cancellable context. When the context is cancelled, the pipeline stops at
// the next check point and returns context.Canceled.
func ProcessUserMessageWithContext(ctx context.Context, conversationID uint, userMessage string, onPhase func(string), onStream func(StreamEvent)) (err error) {
	log.Printf("[DiscussionEngine] Processing conversation %d, message: %s", conversationID, userMessage)

	// Step 1: Get conversation details
	conversation, err := GetConversationByID(conversationID)
	if err != nil {
		return fmt.Errorf("failed to get conversation: %w", err)
	}

	// Step 2: Fetch conversation history BEFORE saving the current message
	history, err := GetConversationMessages(conversationID)
	if err != nil {
		return fmt.Errorf("failed to get conversation messages: %w", err)
	}

	// Step 3: Persist the user message
	if _, err := CreateConversationMessage(conversationID, "user", userMessage, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Defer: save an assistant error message if we fail, but guard against
	// duplicates. recover() catches panics (e.g. parser bugs) so they don't
	// leave the spinner stuck. User-initiated cancellation (context.Canceled)
	// is excluded — the app layer handles that with a clean system message.
	assistantMessageSaved := false
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: %v", r)
			log.Printf("[DiscussionEngine] Panic recovered: %v", r)
		}
		if err != nil && !assistantMessageSaved && !errors.Is(err, context.Canceled) {
			friendlyMsg := formatUserError(err)
			_, _ = CreateConversationMessage(conversationID, "assistant", friendlyMsg, nil, nil, buildErrorMetadata(err.Error()))
		}
	}()

	// Step 4: Determine LLM provider
	var llmProvider *models.LLMProvider
	if conversation.LLMProviderID != nil {
		llmProvider, err = GetLLMProviderByID(*conversation.LLMProviderID)
		if err != nil {
			return fmt.Errorf("failed to get LLM provider: %w", err)
		}
	} else {
		llmProvider, err = GetDefaultLLMProvider()
		if err != nil {
			return fmt.Errorf("failed to get default LLM provider: %w", err)
		}
	}
	if llmProvider == nil {
		return fmt.Errorf("no LLM provider configured for this conversation")
	}

	// Step 5: Determine database connection
	var dbConnection *models.DataSource
	if conversation.DataSourceID != nil {
		dbConnection, err = GetDataSourceByID(*conversation.DataSourceID)
		if err != nil {
			return fmt.Errorf("failed to get DB connection: %w", err)
		}
	} else {
		dbConnection, err = GetDefaultDataSource()
		if err != nil {
			return fmt.Errorf("failed to get default DB connection: %w", err)
		}
	}

	// Step 6: Create a query record for tracking
	query, err := CreateQuery(&conversationID, userMessage, &llmProvider.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to create query record: %w", err)
	}
	if dbConnection != nil {
		query.DataSourceID = &dbConnection.ID
		_, _ = models.DB.Exec("UPDATE queries SET data_source_id = ? WHERE id = ?", dbConnection.ID, query.ID)
	}

	// Step 7: Build context (database schema)
	var schema *DataSchema
	if dbConnection != nil {
		schema, err = GetDataSchema(dbConnection)
		if err != nil {
			log.Printf("[DiscussionEngine] Failed to fetch schema for %s: %v", dbConnection.Type, err)
			_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
			_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(err))
			return fmt.Errorf("failed to fetch database schema: %w", err)
		}
		log.Printf("Fetched schema: %d tables", len(schema.Tables))
	}

	// Step 8: Parse exploration config
	var maxRounds int = 2
	var safetyMode ExplorationSafetyMode = ExplorationRelaxed
	var maxFinalRetries int = 2
	var maxToolsPerRound int
	if dbConnection != nil {
		config, cfgErr := dbConnection.ParseConfig()
		if cfgErr == nil {
			if config.MaxExplorationRounds > 0 {
				maxRounds = config.MaxExplorationRounds
			}
			safetyMode = ParseExplorationSafety(config.ExplorationSafety)
			if config.MaxFinalQueryRetries > 0 {
				maxFinalRetries = config.MaxFinalQueryRetries
			}
			maxToolsPerRound = config.MaxToolsPerRound
		}
	}

	// Step 9: Limit conversation history sent to the LLM.
	// A hard max of 15 prevents context-window exhaustion regardless of
	// what the user configured. Beyond ~15 messages, prior query results
	// balloon the prompt and cause empty/incomplete LLM responses.
	const maxContextHardCap = 15
	limit := conversation.MaxContextMessages
	if limit <= 0 || limit > maxContextHardCap {
		limit = maxContextHardCap
	}
	if len(history) > limit {
		history = history[len(history)-limit:]
	}

	// Step 10: Build tool-calling LLM messages and tool definitions.
	skillsContent, _ := GetEnabledSkillsContent(conversation.ID)
	agentCfg, cfgErr := GetAgentLoopConfig()
	if cfgErr != nil {
		agentCfg = &models.AgentLoopConfig{}
	}
	toolMessages := buildToolLlmMessages(userMessage, history, schema, dbConnection, conversation.VizEnabled, skillsContent)
	tools := buildTools(agentCfg, conversation.VizEnabled, schema, dbConnection)
	log.Printf("[DiscussionEngine] Message count for LLM: %d", len(toolMessages))

	// Step 11: Create the LLM client.
	client, err := NewLLMClient(llmProvider)
	if err != nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
		_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(err))
		return fmt.Errorf("failed to create LLM client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(GetTimeoutSetting("pipeline_timeout_seconds", 180))*time.Second)
	defer cancel()

	if onPhase != nil {
		onPhase("Thinking with LLM...")
	}

	// Only pass the stream callback when the conversation has streaming
	// enabled. When nil, the agentic loop falls back to the blocking path.
	var streamCallback func(StreamEvent)
	if conversation.StreamingEnabled {
		streamCallback = onStream
	}

	// Step 12: Run the isolated agentic loop via the engine package.
	loop := &engine.AgenticLoop{
		LLMClient:     client,
		QueryExecutor: &QueryExecutorAdapter{DataSource: dbConnection, SafetyMode: safetyMode},
		OutputHandler: &SqliteOutputHandler{},
	}
	loopInput := engine.LoopInput{
		UserMessage:    userMessage,
		ConversationID: conversationID,
		QueryID:        query.ID,
		Conversation: engine.ConversationMeta{
			ID:                 conversation.ID,
			LLMProviderID:      conversation.LLMProviderID,
			DataSourceID:       conversation.DataSourceID,
			MaxContextMessages: conversation.MaxContextMessages,
			VizEnabled:         conversation.VizEnabled,
			StreamingEnabled:   conversation.StreamingEnabled,
			Summarize:          conversation.Summarize,
		},
		Schema:        schema,
		SkillsContent: skillsContent,
		OnStream:      streamCallback,
		Messages:      toolMessages,
		Tools:         tools,
	}
	loopConfig := engine.LoopConfig{
		MaxExplorationRounds:        maxRounds,
		MaxToolsPerRound:            maxToolsPerRound,
		MaxErrorRetries:             maxFinalRetries,
		TotalRoundCap:               maxRounds + maxFinalRetries + 4,
		SafetyMode:                  safetyMode,
		ContextWindow:               llmProvider.ContextWindow,
		ModelName:                   llmProvider.Name,
		VizEnabled:                  conversation.VizEnabled,
		Summarize:                   conversation.Summarize,
		StreamingEnabled:            conversation.StreamingEnabled,
		SummarizationTimeoutSeconds: GetTimeoutSetting("summarization_timeout_seconds", 300),
		AgentConfig:                 agentCfg,
	}

	output, loopErr := loop.Run(ctx, loopInput, loopConfig)
	if loopErr == nil && output != nil && output.FatalError != nil {
		loopErr = output.FatalError
	}
	if loopErr != nil {
		if errors.Is(loopErr, context.Canceled) {
			_ = UpdateQueryStatus(query.ID, "cancelled", nil, nil, stringPtr("cancelled by user"), nil, nil, nil)
			_ = UpdateQueryErrorCategory(query.ID, "user_cancelled")
			_, _ = CreateConversationMessage(conversationID, "system", "⏹ Cancelled", nil, nil, nil)
		}
		return loopErr
	}
	assistantMessageSaved = true
	return nil
}

// ProcessUserMessage is the original entry point for backward compatibility.
// It delegates to ProcessUserMessageWithContext with a background context.
func formatUserError(err error) string {
	if err == nil {
		return "I encountered an unexpected issue. Please try again."
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	// Auth / permission errors (LLM API keys and DB credentials)
	if strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") || strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "access denied") || strings.Contains(lower, "permission denied") {
		return "Authentication failed. Please check your API key and connection credentials in Settings."
	}

	// Rate limiting (LLM APIs)
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") {
		return "The AI model is receiving too many requests right now. Please wait a moment and try again."
	}

	// Schema / connection setup errors
	if strings.Contains(lower, "no database connection") || strings.Contains(lower, "cannot execute sql without") {
		return "No database connection configured. Please add a data source in Settings."
	}

	// Transient connection problems
	if strings.Contains(lower, "dial") || strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "tls") {
		return "I'm having trouble connecting to the database. Please check your connection settings and try again."
	}

	// Timeouts (LLM or DB)
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
		return "The request is taking longer than expected. Please try again."
	}

	// SQL / LLM-generated errors (the LLM produced a bad query)
	if strings.Contains(lower, "syntax error") || strings.Contains(lower, "unknown column") ||
		strings.Contains(lower, "unknown table") || strings.Contains(lower, "doesn't exist") ||
		strings.Contains(lower, "does not exist") || strings.Contains(lower, "you have an error in your") {
		return "I had trouble understanding your question. Could you try rephrasing it?"
	}

	// Query loop detection (same query repeated)
	if strings.Contains(lower, "twice in a row") || strings.Contains(lower, "loop detected") ||
		strings.Contains(lower, "query loop") {
		return "I'm having trouble generating a new query. Could you try rephrasing your question?"
	}

	// Schema fetch failure
	if strings.Contains(lower, "failed to fetch database schema") || strings.Contains(lower, "unable to load database schema") {
		return "Unable to load your database schema. Please check your connection settings."
	}

	// Fallback — generic, no raw error exposed
	return "I encountered an issue processing your request. Please try again or rephrase your question."
}

// buildErrorMetadata creates metadata JSON for error messages with the raw error
// string preserved for the tech-details panel.
func buildErrorMetadata(rawError string) *string {
	metadataJSON, _ := json.Marshal(map[string]interface{}{
		"content_type": "html",
		"raw_error":    sanitizeSQLError(rawError),
		"is_error":     true,
	})
	metadata := string(metadataJSON)
	return &metadata
}

// classifyErrorCategory maps an error to a stable error_category value for
// tracking in the queries table.
func classifyErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "api key") || strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") || strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "access denied") || strings.Contains(msg, "permission denied") {
		return "auth_failure"
	}
	if strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests") {
		return "rate_limit"
	}
	if strings.Contains(msg, "dial") || strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") || strings.Contains(msg, "tls") ||
		strings.Contains(msg, "handshake") {
		return "transient_network"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return "llm_timeout"
	}
	if strings.Contains(msg, "syntax error") || strings.Contains(msg, "unknown column") ||
		strings.Contains(msg, "unknown table") || strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist") || strings.Contains(msg, "you have an error in your") {
		return "bad_query"
	}
	if strings.Contains(msg, "twice in a row") || strings.Contains(msg, "loop detected") ||
		strings.Contains(msg, "query loop") {
		return "query_loop"
	}
	if strings.Contains(msg, "failed to fetch database schema") || strings.Contains(msg, "unable to load database schema") {
		return "schema_failure"
	}
	if strings.Contains(msg, "no database connection") || strings.Contains(msg, "cannot execute sql without") {
		return "no_db_connection"
	}
	if strings.Contains(msg, "cancelled") {
		return "user_cancelled"
	}
	return "internal_error"
}

// isErrorMessage checks whether a message's metadata flags it as an error
// response that should be excluded from the LLM context window. Error
// messages remain visible in the UI but the model never sees them.
func isErrorMessage(metadata *string) bool {
	if metadata == nil {
		return false
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(*metadata), &meta); err != nil {
		return false
	}
	if isErr, ok := meta["is_error"]; ok {
		if b, ok := isErr.(bool); ok {
			return b
		}
	}
	return false
}

// ExplorationResult holds the result of a single exploration round.
type ExplorationResult struct {
	SQL       string
	Result    *QueryResult
	Round     int
	Explained string
}

func stringPtr(s string) *string { return &s }

// stripHTMLTags removes HTML tags from a string.
func stripHTMLTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// formatSQLResultsForLLM converts JSON-serialized QueryResult into a compact
// digest for LLM context replay. It sends column stats plus a representative
// row sample — the full table dump (200 rows) was causing context-window
// exhaustion on longer conversations. The tool transcript replay already
// carries the live result (max 50 rows), so this system-message digest only
// needs enough signal for schema/structure awareness on follow-up turns.
func formatSQLResultsForLLM(sqlResultsJSON string) string {
	type qr struct {
		Columns  []string        `json:"columns"`
		Rows     [][]interface{} `json:"rows"`
		RowCount int             `json:"row_count"`
	}

	var result qr
	if err := json.Unmarshal([]byte(sqlResultsJSON), &result); err != nil {
		return ""
	}

	if len(result.Columns) == 0 || len(result.Rows) == 0 {
		return ""
	}

	// Reuse the digest formatter — same approach as summarization.
	qrResult := &QueryResult{
		Columns:  result.Columns,
		Rows:     result.Rows,
		RowCount: result.RowCount,
	}

	return "[PREVIOUS QUERY RESULTS]\n" + formatResultsDigestForSummarization(qrResult) + "\n[/PREVIOUS QUERY RESULTS]\n"
}

// formatResultsDigestForSummarization produces a compact digest of query
// results for the summarization LLM call. When the result set is small, it
// includes all rows. When large, it includes representative rows plus
// column-level statistics so the LLM has enough signal without blowing tokens.
// A transparency note tells the model this is a subset — the user always sees
// the complete result table.
func formatResultsDigestForSummarization(result *QueryResult) string {
	if result == nil || len(result.Columns) == 0 || len(result.Rows) == 0 {
		return ""
	}

	var sb strings.Builder
	totalRows := len(result.Rows)

	sb.WriteString(fmt.Sprintf("Query returned %d rows across %d columns.\n\n", totalRows, len(result.Columns)))

	// Column-level statistics (computed from full result set).
	sb.WriteString("Column summary:\n")
	for colIdx, col := range result.Columns {
		sb.WriteString(fmt.Sprintf("  %s: ", humanizeColumnName(col)))
		stats := computeColumnStats(result.Rows, colIdx)
		if stats.isNumeric {
			sb.WriteString(fmt.Sprintf("numeric — min: %s, max: %s", stats.min, stats.max))
			if stats.mean != "" {
				sb.WriteString(fmt.Sprintf(", mean: %s", stats.mean))
			}
		} else {
			sb.WriteString(fmt.Sprintf("%d distinct values", stats.distinct))
		}
		if stats.nullCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d null)", stats.nullCount))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Representative rows: first 5 and last 5 when > 10 total.
	const sampleSize = 5
	if totalRows <= sampleSize*2 {
		sb.WriteString(fmt.Sprintf("All %d rows:\n\n", totalRows))
		sb.WriteString(formatRowsTable(result.Columns, result.Rows, 0, totalRows))
	} else {
		sb.WriteString(fmt.Sprintf("First %d rows (of %d total):\n\n", sampleSize, totalRows))
		sb.WriteString(formatRowsTable(result.Columns, result.Rows, 0, sampleSize))
		sb.WriteString(fmt.Sprintf("\nLast %d rows:\n\n", sampleSize))
		sb.WriteString(formatRowsTable(result.Columns, result.Rows, totalRows-sampleSize, totalRows))
	}

	sb.WriteString("\nNote: The user sees the complete result table. The column statistics and row samples above represent the full data — base your analysis on them. Your summary will be displayed above the full results table in the user's interface.\n")

	return sb.String()
}

// columnStats holds computed statistics for a single result column.
type columnStats struct {
	isNumeric bool
	min       string
	max       string
	mean      string
	distinct  int
	nullCount int
}

func computeColumnStats(rows [][]interface{}, colIdx int) columnStats {
	seen := make(map[string]bool)
	var nums []float64
	var cs columnStats

	for _, row := range rows {
		if colIdx >= len(row) || row[colIdx] == nil {
			cs.nullCount++
			continue
		}
		val := fmt.Sprintf("%v", row[colIdx])
		seen[val] = true
		if f, err := parseFloat(row[colIdx]); err == nil {
			nums = append(nums, f)
		}
	}

	cs.distinct = len(seen)
	if len(nums) > 0 && float64(len(nums)) >= float64(len(rows))*0.5 {
		cs.isNumeric = true
		min, max := nums[0], nums[0]
		var sum float64
		for _, n := range nums {
			sum += n
			if n < min {
				min = n
			}
			if n > max {
				max = n
			}
		}
		cs.min = formatFloat(min)
		cs.max = formatFloat(max)
		cs.mean = formatFloat(sum / float64(len(nums)))
	}
	return cs
}

func parseFloat(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case []byte:
		return strconv.ParseFloat(string(t), 64)
	case string:
		return strconv.ParseFloat(t, 64)
	}
	return 0, fmt.Errorf("not numeric")
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// formatRowsTable formats a slice of rows as a markdown table.
func formatRowsTable(columns []string, rows [][]interface{}, start, end int) string {
	var sb strings.Builder
	sb.WriteString("| ")
	for i, col := range columns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(humanizeColumnName(col))
	}
	sb.WriteString(" |\n")
	sb.WriteString("|" + strings.Repeat("---|", len(columns)) + "\n")

	for i := start; i < end && i < len(rows); i++ {
		sb.WriteString("| ")
		for j, val := range rows[i] {
			if j > 0 {
				sb.WriteString(" | ")
			}
			cell := fmt.Sprintf("%v", val)
			if len(cell) > 80 {
				cell = cell[:80] + "..."
			}
			sb.WriteString(cell)
		}
		sb.WriteString(" |\n")
	}
	return sb.String()
}

// summarizeResults sends query results back to the LLM for a natural-language
// summary. Large result sets are digested to a compact subset with column
// statistics to keep token usage low and avoid summarization failures.
