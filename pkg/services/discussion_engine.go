package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// LLMResponse defines the structured response expected from the LLM.
type LLMResponse struct {
	Action                string `json:"action"` // "sql_query", "clarification", or "sql_exploration"
	SQLQuery              string `json:"sql_query,omitempty"`
	ClarificationQuestion string `json:"clarification_question,omitempty"`
	Explanation           string `json:"explanation,omitempty"`
}

// looksLikeSQL checks if a string appears to be a SQL query.
func looksLikeSQL(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if (strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) ||
		(strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) {
		trimmed = trimmed[1 : len(trimmed)-1]
		trimmed = strings.TrimSpace(trimmed)
	}
	if trimmed == "" {
		return false
	}
	return regexp.MustCompile(`(?i)^\s*(SELECT|INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|DESCRIBE|SHOW|EXPLAIN|TRUNCATE|WITH|UNION|INTERSECT|EXCEPT|\()`).MatchString(trimmed)
}

// extractJSONFromResponse extracts JSON from various LLM output formats.
func extractJSONFromResponse(response string) string {
	response = strings.TrimSpace(response)

	// Handle markdown code blocks
	startIdx := strings.Index(response, "```")
	if startIdx != -1 {
		remaining := response[startIdx+3:]
		remaining = strings.TrimLeft(remaining, " \t\n\r")
		langEnd := strings.Index(remaining, "\n")
		if langEnd != -1 {
			remaining = remaining[langEnd+1:]
		}
		endIdx := strings.Index(remaining, "```")
		if endIdx != -1 {
			response = remaining[:endIdx]
		} else {
			response = remaining
		}
	}

	// Handle Qwen-style thinking/response prefixes
	lower := strings.ToLower(response)
	respMarker := "\nresponse\n"
	if idx := strings.Index(lower, respMarker); idx != -1 {
		candidate := strings.TrimSpace(response[idx+len(respMarker):])
		if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
			response = candidate
		}
	}

	// Strip </think> and similar markers
	reThink := regexp.MustCompile(`(?i)</think>\s*`)
	response = reThink.ReplaceAllString(response, "")

	// Find the first balanced JSON object with a brace counter
	if !strings.HasPrefix(response, "{") && !strings.HasPrefix(response, "[") {
		braceIdx := strings.Index(response, "{")
		bracketIdx := strings.Index(response, "[")
		jsonStart := braceIdx
		if jsonStart == -1 || (bracketIdx != -1 && bracketIdx < jsonStart) {
			jsonStart = bracketIdx
		}
		if jsonStart > 0 {
			prefix := response[:jsonStart]
			if strings.Contains(strings.ToLower(prefix), "thinking") || len(prefix) > 100 {
				response = response[jsonStart:]
			}
		}
	}

	// Return verbatim – json.Unmarshal handles escapes natively
	return strings.TrimSpace(response)
}

// truncateString truncates a string to maxLen with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ProcessUserMessage processes a user message in a conversation.
func ProcessUserMessage(conversationID uint, userMessage string, onPhase func(string)) (err error) {
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

	// Defer: save an assistant error message if we fail, but guard against duplicates (§5.3)
	assistantMessageSaved := false
	defer func() {
		if err != nil && !assistantMessageSaved {
			errorMsg := fmt.Sprintf("I encountered an error: %s", err.Error())
			_, _ = CreateConversationMessage(conversationID, "assistant", errorMsg, nil, nil, nil)
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
			log.Printf("Failed to fetch schema for %s: %v", dbConnection.Type, err)
			_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to fetch database schema: %v", err)), nil, nil, nil)
		} else if schema != nil {
			log.Printf("Fetched schema: %d tables", len(schema.Tables))
		}
	}

	// Step 8: Parse exploration config
	var maxRounds int = 2
	var safetyMode ExplorationSafetyMode = ExplorationStrict
	var explorationAllowed bool = true
	var maxActionRetries int = 1
	var maxFinalRetries int = 1
	if dbConnection != nil {
		config, cfgErr := dbConnection.ParseConfig()
		if cfgErr == nil {
			if config.MaxExplorationRounds > 0 {
				maxRounds = config.MaxExplorationRounds
			}
			safetyMode = ParseExplorationSafety(config.ExplorationSafety)
			explorationAllowed = config.ExplorationAllowed
			if config.MaxActionRetries >= 0 {
				maxActionRetries = config.MaxActionRetries
			}
			if config.MaxFinalQueryRetries > 0 {
				maxFinalRetries = config.MaxFinalQueryRetries
			}
		}
	}

	// Step 9: Limit conversation history sent to the LLM
	if conversation.MaxContextMessages > 0 && len(history) > conversation.MaxContextMessages {
		history = history[len(history)-conversation.MaxContextMessages:]
	}

	// Step 10: Build LLM messages
	llmMessages := buildLlmMessages(userMessage, history, schema, dbConnection)
	log.Printf("[DiscussionEngine] Message count for LLM: %d", len(llmMessages))

	// Step 11: Call LLM
	client, err := NewLLMClient(llmProvider)
	if err != nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to create LLM client: %v", err)), nil, nil, nil)
		return fmt.Errorf("failed to create LLM client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var explorationResults []ExplorationResult
	var llmResp LLMResponse
	var actionRetries int

	// Phase: LLM processing
	if onPhase != nil {
		onPhase("Thinking with LLM...")
	}

	// Exploration loop
	for round := 0; round < maxRounds; round++ {
		if ctx.Err() != nil {
			return fmt.Errorf("exploration cancelled: %w", ctx.Err())
		}

		responseText, requestJSON, responseJSON, err := client.ChatCompletionWithPayload(ctx, llmMessages)
		if err != nil {
			_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("LLM request failed: %v", err)), nil, nil, nil)
			return fmt.Errorf("LLM request failed: %w", err)
		}

		// Store full payload for debugging
		if requestJSON != "" && responseJSON != "" {
			_ = storePayload(conversationID, round+1, "", requestJSON, responseJSON, llmMessages)
		}

		log.Printf("[DiscussionEngine] Round %d — Raw LLM response: %s", round+1, responseText)
		cleanedResponse := extractJSONFromResponse(responseText)
		llmResp, err = parseLLMResponse(cleanedResponse)
		if err != nil {
			return fmt.Errorf("failed to parse LLM response: %w", err)
		}

		log.Printf("[DiscussionEngine] Round %d — action=%s", round+1, llmResp.Action)

		// Handle missing or unknown action
		if llmResp.Action == "" {
			if llmResp.SQLQuery != "" {
				llmResp.Action = "sql_query"
			} else {
				llmResp.Action = "clarification"
				llmResp.ClarificationQuestion = "I received your response but couldn't determine what you wanted me to do. Please use one of: sql_query, clarification, or sql_exploration."
			}
		} else if llmResp.Action != "sql_query" && llmResp.Action != "clarification" && llmResp.Action != "sql_exploration" {
			if actionRetries < maxActionRetries {
				log.Printf("[DiscussionEngine] Round %d — unknown action '%s', retry %d/%d", round+1, llmResp.Action, actionRetries+1, maxActionRetries)
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("Your previous response was valid JSON but did not include a recognized action. You must use one of: \"sql_query\", \"clarification\", or \"sql_exploration\". Please respond again with the correct format."),
				})
				actionRetries++
				continue
			}
			if llmResp.SQLQuery != "" {
				llmResp.Action = "sql_query"
			} else {
				llmResp.Action = "clarification"
				llmResp.ClarificationQuestion = fmt.Sprintf("I couldn't understand your last response (action: %q). Please rephrase your question.", llmResp.Action)
			}
		}

		switch llmResp.Action {
		case "sql_query":
			if dbConnection == nil {
				_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Cannot execute SQL without a database connection"), nil, nil, nil)
				return fmt.Errorf("no database connection for SQL query")
			}
			// Append exploration results to history for final execution
			for _, er := range explorationResults {
				history = append(history, &models.ConversationMessage{
					Role:    "exploration",
					Content: er.ToMessageContent(),
				})
			}
			executeFinalQueryWithRetry(ctx, query, llmResp, client, llmMessages, dbConnection, conversation, userMessage, explorationResults, maxFinalRetries)
			assistantMessageSaved = true
			return nil

		case "clarification":
			err := handleClarification(query, llmResp, conversationID)
			if err == nil {
				assistantMessageSaved = true
			}
			return err

		case "sql_exploration":
			if !explorationAllowed {
				llmResp = LLMResponse{
					Action: "clarification",
					ClarificationQuestion: "Exploration queries are not allowed for this connection. Please rephrase your request.",
				}
				return handleClarification(query, llmResp, conversationID)
			}

			if err := validateExplorationQuery(llmResp.SQLQuery, safetyMode); err != nil {
				log.Printf("[DiscussionEngine] Round %d — Exploration query rejected: %v", round+1, err)
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("The previous exploration query was rejected: %s. Please revise it to comply with safety constraints.", err.Error()),
				})
				continue
			}

			result, err := executeSQLWithMode(dbConnection, llmResp.SQLQuery, true)
			if err != nil {
				log.Printf("[DiscussionEngine] Round %d — Exploration query failed: %v", round+1, err)
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("The exploration query failed to execute: %s. Please try a different approach.", err.Error()),
				})
				continue
			}

			if onPhase != nil {
				onPhase("Exploring data...")
			}

			er := ExplorationResult{
				SQL:     llmResp.SQLQuery,
				Result:  result,
				Round:   round + 1,
				Explained: llmResp.Explanation,
			}
			explorationResults = append(explorationResults, er)

			history = append(history, &models.ConversationMessage{
				Role:    "exploration",
				Content: er.ToMessageContent(),
			})
			llmMessages = append(llmMessages, ChatMessage{
				Role:    "system",
				Content: er.ToMessageContent(),
			})

			remaining := maxRounds - round - 1
			if remaining > 0 {
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("You have %d more exploration round(s) available. Use them wisely.", remaining),
				})
			}
		}
	}

	// Max rounds exhausted — force final sql_query
	log.Printf("[DiscussionEngine] Exploration limit reached (%d rounds). Forcing final query.", maxRounds)
	llmMessages = append(llmMessages, ChatMessage{
		Role:    "system",
		Content: fmt.Sprintf("You have reached the exploration limit of %d rounds. You must now produce a final 'sql_query' action.", maxRounds),
	})

	// Phase: final LLM call
	if onPhase != nil {
		onPhase("Thinking with LLM...")
	}

	responseText, requestJSON, responseJSON, err := client.ChatCompletionWithPayload(ctx, llmMessages)
	if err != nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("LLM request failed: %v", err)), nil, nil, nil)
		return fmt.Errorf("LLM request failed: %w", err)
	}

	if requestJSON != "" && responseJSON != "" {
		_ = storePayload(conversationID, 0, "final", requestJSON, responseJSON, llmMessages)
	}

	cleanedResponse := extractJSONFromResponse(responseText)
	finalResp, parseErr := parseLLMResponse(cleanedResponse)
	if parseErr != nil {
		return fmt.Errorf("failed to parse final LLM response: %w", parseErr)
	}

	if finalResp.Action == "clarification" {
		return handleClarification(query, finalResp, conversationID)
	}

	if finalResp.Action != "sql_query" {
		// BUG FIX §5.2: Don't send comment-only SQL; convert to clarification
		finalResp = LLMResponse{
			Action:                "clarification",
			ClarificationQuestion: fmt.Sprintf("I reached the exploration limit but couldn't produce a final query (got action: %s). Please rephrase your question.", finalResp.Action),
		}
		return handleClarification(query, finalResp, conversationID)
	}

	if dbConnection == nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Cannot execute SQL without a database connection"), nil, nil, nil)
		return fmt.Errorf("no database connection for SQL query")
	}

	// Phase: SQL execution
	if onPhase != nil {
		onPhase("Running query...")
	}

	executeFinalQueryWithRetry(ctx, query, finalResp, client, llmMessages, dbConnection, conversation, userMessage, explorationResults, maxFinalRetries)
	assistantMessageSaved = true

	// Phase: finalizing
	if onPhase != nil {
		onPhase("Analyzing results...")
	}
	return nil
}

// parseLLMResponse parses a cleaned LLM response string into an LLMResponse.
func parseLLMResponse(cleanedResponse string) (LLMResponse, error) {
	var resp LLMResponse
	if err := json.Unmarshal([]byte(cleanedResponse), &resp); err != nil {
		if looksLikeSQL(cleanedResponse) {
			return LLMResponse{Action: "sql_query", SQLQuery: cleanedResponse}, nil
		}
		trimmed := strings.TrimSpace(cleanedResponse)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			return LLMResponse{
				Action:                "clarification",
				ClarificationQuestion: fmt.Sprintf("The LLM returned invalid JSON (parse error: %v). Raw:\n\n```\n%s\n```", err, truncateString(cleanedResponse, 300)),
			}, nil
		}
		return LLMResponse{
			Action:                "clarification",
			ClarificationQuestion: cleanedResponse,
		}, nil
	}
	return resp, nil
}

// storePayload creates a conversation message with the full request/response payload.
func storePayload(conversationID uint, round any, label, requestJSON, responseJSON string, llmMessages []ChatMessage) error {
	payloadMeta := map[string]interface{}{
		"round":          round,
		"request_json":   requestJSON,
		"response_json":  responseJSON,
		"llm_messages":   llmMessages,
	}
	payloadJSON, _ := json.Marshal(payloadMeta)
	payloadJSONStr := string(payloadJSON)
	var msgLabel string
	if label != "" {
		msgLabel = fmt.Sprintf("[%s — Full Payload]", label)
	} else {
		msgLabel = fmt.Sprintf("[Round %v — Full Payload]", round)
	}
	_, err := CreateConversationMessage(conversationID, "exploration", msgLabel, nil, nil, &payloadJSONStr)
	return err
}

// buildLlmMessages constructs the message list for the LLM.
func buildLlmMessages(userMessage string, history []*models.ConversationMessage, schema *DataSchema, dbConnection *models.DataSource) []ChatMessage {
	messages := []ChatMessage{}

	hasDB := dbConnection != nil
	systemPrompt := buildSystemPrompt(schema, hasDB, dbConnection)
	messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})

	for _, msg := range history {
		role := msg.Role
		if role == "user" || role == "assistant" || role == "exploration" {
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

	// Merge consecutive user messages
	merged := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if len(merged) > 0 && merged[len(merged)-1].Role == "user" && msg.Role == "user" {
			merged[len(merged)-1].Content += "\n\n" + msg.Content
		} else {
			merged = append(merged, msg)
		}
	}

	return merged
}

// ExplorationResult holds the result of a single exploration round.
type ExplorationResult struct {
	SQL       string
	Result    *QueryResult
	Round     int
	Explained string
}

func (er *ExplorationResult) ToMessageContent() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Exploration Round %d]\n", er.Round))
	sb.WriteString(fmt.Sprintf("Query: %s\n", er.SQL))
	if er.Explained != "" {
		sb.WriteString(fmt.Sprintf("Reason: %s\n", er.Explained))
	}
	if er.Result != nil && er.Result.RowCount > 0 {
		sb.WriteString(fmt.Sprintf("Result: %d row(s) returned\n\n", er.Result.RowCount))
		limit := 10
		if len(er.Result.Rows) < limit {
			limit = len(er.Result.Rows)
		}
		for i := 0; i < limit; i++ {
			sb.WriteString("  [")
			for j, col := range er.Result.Columns {
				if j > 0 {
					sb.WriteString(" | ")
				}
				val := "<nil>"
				if i < len(er.Result.Rows) {
					val = fmt.Sprintf("%v", er.Result.Rows[i][j])
					if len(val) > 50 {
						val = val[:50] + "..."
					}
				}
				sb.WriteString(fmt.Sprintf("%s: %s", col, val))
			}
			sb.WriteString("]\n")
		}
		if len(er.Result.Rows) > limit {
			sb.WriteString(fmt.Sprintf("  ... and %d more row(s)\n", len(er.Result.Rows)-limit))
		}
	} else if er.Result != nil {
		sb.WriteString("Result: 0 rows returned\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// executeFinalQueryWithRetry wraps SQL execution in a retry loop.
func executeFinalQueryWithRetry(ctx context.Context, query *models.Query, resp LLMResponse, client LLMClient, llmMessages []ChatMessage, dbConnection *models.DataSource, conversation *models.Conversation, userMessage string, explorationResults []ExplorationResult, maxRetries int) {
	lastSQL := ""
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			lastErr = fmt.Errorf("context cancelled: %w", ctx.Err())
			break
		}

		if attempt > 0 {
			llmMessages = append(llmMessages, ChatMessage{
				Role: "system",
				Content: fmt.Sprintf("The previous SQL query failed:\n\n```sql\n%s\n```\n\nError: %s\n\nPlease correct the query and respond with a new 'sql_query' action.", lastSQL, lastErr.Error()),
			})

			responseText, requestJSON, responseJSON, err := client.ChatCompletionWithPayload(ctx, llmMessages)
			if err != nil {
				lastErr = fmt.Errorf("LLM request failed during retry: %w", err)
				break
			}

			if requestJSON != "" && responseJSON != "" {
				_ = storePayload(conversation.ID, 0, fmt.Sprintf("retry-%d", attempt), requestJSON, responseJSON, llmMessages)
			}

			cleanedResponse := extractJSONFromResponse(responseText)
			var newResp LLMResponse
			newResp, err = parseLLMResponse(cleanedResponse)
			if err != nil {
				lastErr = fmt.Errorf("failed to parse retry response: %w", err)
				break
			}

			if newResp.Action != "sql_query" {
				llmContentJSON, _ := json.Marshal(newResp)
				llmContent := string(llmContentJSON)
				llmContentPtr := &llmContent

				var displayMsg string
				if newResp.ClarificationQuestion != "" {
					displayMsg = newResp.ClarificationQuestion
				} else if newResp.Explanation != "" {
					displayMsg = newResp.Explanation
				} else {
					displayMsg = fmt.Sprintf("The query failed and the LLM responded with '%s'. Please rephrase your question.", newResp.Action)
				}

				_ = UpdateQueryStatus(query.ID, "error", &lastSQL, nil, stringPtr(fmt.Sprintf("%s: %v", newResp.Action, lastErr)), nil, nil, nil)
				_, _ = CreateConversationMessage(conversation.ID, "assistant", displayMsg, llmContentPtr, nil, nil)
				return
			}
			resp = newResp
		}

		if resp.SQLQuery == lastSQL {
			lastErr = fmt.Errorf("LLM produced the same query (%s) twice in a row; cannot make further progress", truncateString(resp.SQLQuery, 100))
			break
		}
		lastSQL = resp.SQLQuery

		results, err := executeSQL(dbConnection, resp.SQLQuery)
		if err == nil {
			var summary *string
			if conversation.Summarize {
				s := summarizeResults(ctx, client, userMessage, resp.SQLQuery, results)
				if s != "" {
					summary = &s
				}
			}
			renderSQLResults(query, resp, dbConnection, conversation.ID, explorationResults, results, summary)
			return
		}

		if !isRetryableError(err) {
			lastErr = err
			break
		}

		lastErr = err
		log.Printf("[DiscussionEngine] SQL execution failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	renderSQLError(query, resp, dbConnection, conversation.ID, explorationResults, lastErr)
}

// handleClarification creates an assistant message asking for clarification.
func handleClarification(query *models.Query, resp LLMResponse, conversationID uint) error {
	if err := UpdateQueryStatus(query.ID, "clarification", nil, nil, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}

	message := resp.ClarificationQuestion
	if resp.Explanation != "" {
		message = fmt.Sprintf("%s\n\n*(%s)*", resp.ClarificationQuestion, resp.Explanation)
	}

	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	_, err := CreateConversationMessage(conversationID, "assistant", message, llmContentPtr, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create clarification message: %w", err)
	}

	return nil
}

func stringPtr(s string) *string { return &s }

// stripHTMLTags removes HTML tags from a string.
func stripHTMLTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// formatSQLResultsForLLM converts JSON-serialized QueryResult into compact text for the LLM.
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

	var sb strings.Builder
	sb.WriteString("[PREVIOUS QUERY RESULTS]\n")
	sb.WriteString("Columns: " + strings.Join(result.Columns, ", ") + "\n")

	maxRows := 200
	if len(result.Rows) > maxRows {
		sb.WriteString(fmt.Sprintf("Showing %d of %d rows:\n\n", maxRows, len(result.Rows)))
	} else {
		sb.WriteString(fmt.Sprintf("(%d rows):\n\n", len(result.Rows)))
	}

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
		if i >= maxRows {
			break
		}
		sb.WriteString("| ")
		for j, val := range row {
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
	sb.WriteString("\n[/PREVIOUS QUERY RESULTS]\n")

	return sb.String()
}

// buildSystemPrompt creates the system prompt with schema and instructions.
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource) string {
	var sb strings.Builder
	
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
		sb.WriteString("You are a helpful data analyst assistant. Your task is to help users query a database using natural language.\n\n")
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

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Analyze the user's question and the database schema (if provided).\n")
	sb.WriteString("2. Decide whether you can answer directly by generating a SQL query, if you need clarification, or if you should first explore the data.\n")
	sb.WriteString("3. Respond with a JSON object containing exactly the following fields:\n")
	sb.WriteString("   - \"action\": one of \"sql_query\", \"clarification\", or \"sql_exploration\"\n")
	sb.WriteString("   - \"sql_query\": if action is \"sql_query\" or \"sql_exploration\", provide a valid SELECT query.\n")
	sb.WriteString("   - \"clarification_question\": if action is \"clarification\", ask a concise clarifying question.\n")
	sb.WriteString("   - \"explanation\": optional short explanation of your reasoning.\n")
	dbTypeHint := "SQL"
	if dbConnection != nil {
		driver, driverErr := GetDriver(dbConnection.Type)
		if driverErr == nil {
			dbTypeHint = driver.DisplayName()
		} else {
			dbTypeHint = dbConnection.Type
		}
	}
	sb.WriteString(fmt.Sprintf("4. The SQL query must be safe, read-only, and compatible with %s.\n", dbTypeHint))
	sb.WriteString("5. If the user asks a general question not related to the database, you may answer directly.\n")
	sb.WriteString("6. Always include a LIMIT clause in your SQL queries to prevent unbounded result sets.\n\n")

	if dbConnection != nil {
		config, cfgErr := dbConnection.ParseConfig()
		if cfgErr == nil && config.ExplorationAllowed {
			sb.WriteString("## Exploration Mode\n")
			sb.WriteString("If you know the schema but need to see actual data values to construct the correct final query, use action \"sql_exploration\".\n")
			sb.WriteString(fmt.Sprintf("Your exploration queries are constrained to: **%s** mode.\n", config.ExplorationSafety))
			switch config.ExplorationSafety {
			case "strict":
				sb.WriteString("- Allowed: SELECT with LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA queries\n")
				sb.WriteString("- Blocked: JOINs, subqueries, GROUP BY, ORDER BY\n")
			case "moderate":
				sb.WriteString("- Allowed: everything in strict, plus single-table JOIN, GROUP BY, ORDER BY\n")
				sb.WriteString("- Blocked: subqueries, UNION, multi-table JOINs\n")
			case "relaxed":
				sb.WriteString("- Allowed: everything in moderate, plus subqueries and UNION\n")
				sb.WriteString("- Blocked: INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE, and other DML/DDL\n")
			}
			sb.WriteString("- All modes: read-only only — no DML/DDL under any circumstances\n")
			sb.WriteString("- After each exploration, you will see the results and should use them to refine your final query.\n")
			sb.WriteString(fmt.Sprintf("- You have up to %d exploration round(s) before being forced to produce a final query.\n\n", config.MaxExplorationRounds))
		}
	}

	sb.WriteString("Your response must be a valid JSON object, no additional text.\n")

	prompt := sb.String()
	if len(prompt) > 16384 {
		prompt = prompt[:16000] + "\n\n[Note: schema truncated due to context limits. The user's question follows below.]\n"
	}
	return prompt
}

// renderSQLResults renders a successful SQL query result as an assistant message.
func renderSQLResults(query *models.Query, resp LLMResponse, dbConnection *models.DataSource, conversationID uint, explorationResults []ExplorationResult, results *QueryResult, summary *string) {
	resultSummary := formatResults(results)

	var explanation string
	if resp.Explanation != "" {
		explanation = resp.Explanation
	}

	var explorationHTML string
	if len(explorationResults) > 0 {
		explorationHTML = formatExplorationHTML(explorationResults)
		if len(explorationResults) == 1 {
			explanation = fmt.Sprintf("I explored the data before formulating this query. %s", explanation)
		} else if explanation == "" {
			explanation = fmt.Sprintf("I ran %d intermediate query(ies) to explore the data before formulating this query.", len(explorationResults))
		}
	}

	assistantResp := AssistantResponse{
		Explanation:     explanation,
		SQL:             resp.SQLQuery,
		Result:          results,
		ExplorationHTML: explorationHTML,
		Summary:         summary,
	}
	assistantMessageHTML := assistantResp.ToHTML()

	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	sqlResultsJSON, err := json.Marshal(results)
	if err != nil {
		sqlResultsJSON = nil
	}
	sqlResultsPtr := stringPtr(string(sqlResultsJSON))

	metadataJSON, _ := json.Marshal(map[string]interface{}{"content_type": "html"})
	metadataPtr := new(string)
	*metadataPtr = string(metadataJSON)

	execTime := 0
	tokensUsed := 0
	if err := UpdateQueryStatus(query.ID, "success", &resp.SQLQuery, &resultSummary, nil, &execTime, &tokensUsed, nil); err != nil {
		log.Printf("[DiscussionEngine] Failed to update query status: %v", err)
	}

	_, err = CreateConversationMessage(conversationID, "assistant", assistantMessageHTML, llmContentPtr, sqlResultsPtr, metadataPtr)
	if err != nil {
		log.Printf("[DiscussionEngine] Failed to create assistant message: %v", err)
	}
}

// summarizeResults sends query results back to the LLM for a natural-language summary.
func summarizeResults(ctx context.Context, client LLMClient, userQuestion, sqlQuery string, results *QueryResult) string {
	if results == nil || results.RowCount == 0 {
		return ""
	}

	formatted := formatSQLResultsForLLMFromQueryResult(results)
	if formatted == "" {
		return ""
	}

	prompt := fmt.Sprintf(`You are a helpful data analyst. Summarize the following SQL query results in plain English, directly answering the user's question.

**User's question**: %s

**SQL executed**:
`+"```sql\n%s\n```"+`

**Query results**:
%s

**Instructions**:
- Answer the user's question directly, referencing specific numbers and facts from the data.
- Keep it concise — 3-5 sentences is ideal.
- If the results are empty, clearly state that no data matched the query.
- Do NOT include a markdown table — this is a prose summary.
- Do NOT suggest next steps — just answer the question.`, userQuestion, sqlQuery, formatted)

	summaryMessages := []ChatMessage{
		{Role: "user", Content: prompt},
	}

	summary, err := client.ChatCompletion(ctx, summaryMessages)
	if err != nil {
		log.Printf("[DiscussionEngine] Summarization failed: %v", err)
		return ""
	}

	return strings.TrimSpace(summary)
}

// formatSQLResultsForLLMFromQueryResult formats a QueryResult for LLM consumption.
func formatSQLResultsForLLMFromQueryResult(result *QueryResult) string {
	if result == nil || len(result.Columns) == 0 || len(result.Rows) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Columns: " + strings.Join(result.Columns, ", ") + "\n")

	maxRows := 50
	if len(result.Rows) > maxRows {
		sb.WriteString(fmt.Sprintf("Showing %d of %d rows:\n\n", maxRows, len(result.Rows)))
	} else {
		sb.WriteString(fmt.Sprintf("(%d rows):\n\n", len(result.Rows)))
	}

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
		if i >= maxRows {
			break
		}
		sb.WriteString("| ")
		for j, val := range row {
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

// renderSQLError renders a failed SQL query as an assistant message.
func renderSQLError(query *models.Query, resp LLMResponse, dbConnection *models.DataSource, conversationID uint, explorationResults []ExplorationResult, lastErr error) {
	if lastErr != nil {
		_ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr(lastErr.Error()), nil, nil, nil)
	}

	var sqlBlock string
	if resp.SQLQuery != "" {
		sqlBlock = fmt.Sprintf("\n```sql\n%s\n```", resp.SQLQuery)
	}

	errorMsg := fmt.Sprintf("I tried to execute the SQL query but encountered an error:%s\n\n**Error**: %s", sqlBlock, lastErr.Error())

	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	_, err := CreateConversationMessage(conversationID, "assistant", errorMsg, llmContentPtr, nil, nil)
	if err != nil {
		log.Printf("[DiscussionEngine] Failed to create error message: %v", err)
	}
}
