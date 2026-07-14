package services

import (
	"context"
	"encoding/json"
	"fmt"
	// "html"
	"log"
	"regexp"
	"strconv"
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
	// Trim whitespace and common wrapper characters
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	
	// Remove surrounding quotes if present
	if (strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) ||
		(strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) {
		trimmed = trimmed[1 : len(trimmed)-1]
		trimmed = strings.TrimSpace(trimmed)
	}
	
	if trimmed == "" {
		return false
	}
	
	upper := strings.ToUpper(trimmed)

	// Direct prefix match
	sqlPrefixes := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER", "DESCRIBE", "SHOW", "EXPLAIN", "TRUNCATE"}
	for _, prefix := range sqlPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}

	// CTE (WITH clause) queries
	if strings.HasPrefix(upper, "WITH") && len(upper) > 4 {
		// Check that after "WITH" there is whitespace or '(' (for CTE)
		next := upper[4]
		if next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '(' {
			return true
		}
	}

	// Parenthesized subquery or UNION/INTERSECT/EXCEPT
	if strings.HasPrefix(upper, "(") || strings.HasPrefix(upper, "UNION") ||
		strings.HasPrefix(upper, "INTERSECT") || strings.HasPrefix(upper, "EXCEPT") {
		return true
	}

	return false
}

// extractJSONFromResponse extracts JSON from various LLM output formats.
// Handles: markdown code blocks, Qwen thinking/response prefixes,
// and plain JSON. Also unescapes JSON string escapes.
func extractJSONFromResponse(response string) string {
	response = strings.TrimSpace(response)

	// Handle markdown code blocks (```sql ... ``` or ```json ... ```)
	startIdx := strings.Index(response, "```")
	if startIdx != -1 {
		remaining := response[startIdx+3:]
		remaining = strings.TrimLeft(remaining, " 	\n\r")
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

	// Handle Qwen-style "thinking ... response \n\n {json}" or "thinking ... response {json}" prefix.
	// Some models wrap the actual answer in a thinking/response block.
	lower := strings.ToLower(response)
	// Look for "</think> " marker (Qwen style) and take everything after it
	respMarker := "\nresponse\n"
	if idx := strings.Index(lower, respMarker); idx != -1 {
		candidate := strings.TrimSpace(response[idx+len(respMarker):])
		// If what follows looks like JSON, use it
		if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
			response = candidate
		}
	}

	// Fallback: if there's a "thinking" prefix and a JSON object exists, extract the JSON.
	// Find the first { or [ and check if there's substantial non-JSON text before it.
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

	// Unescape JSON string escapes so that \n → actual newline, \t → tab, etc.
	unescaped, err := strconv.Unquote(`"` + strings.ReplaceAll(response, `\"`, `"`) + `"`)
	if err == nil {
		return unescaped
	}
	return strings.TrimSpace(response)
}

// ProcessUserMessage processes a user message in a conversation, generating an assistant response.
// This is the core discussion engine that coordinates LLM, database, and conversation state.
func ProcessUserMessage(conversationID uint, userMessage string) (err error) {
	log.Printf("[DiscussionEngine] Processing conversation %d, message: %s", conversationID, userMessage)
	// Step 1: Get conversation details
	conversation, err := GetConversationByID(conversationID)
	if err != nil {
		return fmt.Errorf("failed to get conversation: %w", err)
	}

	workspaceID := conversation.WorkspaceID
	userID := conversation.UserID

	// Step 2: Fetch conversation history BEFORE saving the current message,
	// so the LLM context does not include a duplicate of the user's input.
	history, err := GetConversationMessages(conversationID)
	if err != nil {
		return fmt.Errorf("failed to get conversation messages: %w", err)
	}

	// Step 3: Persist the user message in the conversation (for display)
	if _, err := CreateConversationMessage(conversationID, "user", userMessage, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Defer: if the engine errors out after this point, save an assistant error
	// message so the history remains balanced (no orphaned user messages).
	// Consecutive user messages break many chat templates (e.g. qwen).
	defer func() {
		if err != nil {
			errorMsg := fmt.Sprintf("I encountered an error: %s", err.Error())
			_, _ = CreateConversationMessage(conversationID, "assistant", errorMsg, nil, nil, nil)
		}
	}()

	// Step 4: Determine LLM provider (use conversation-specific or workspace default)
	var llmProvider *models.LLMProvider
	if conversation.LLMProviderID != nil {
		llmProvider, err = GetLLMProviderByID(*conversation.LLMProviderID)
		if err != nil {
			return fmt.Errorf("failed to get LLM provider: %w", err)
		}
	} else {
		llmProvider, err = GetDefaultLLMProvider(workspaceID)
		if err != nil {
			return fmt.Errorf("failed to get default LLM provider: %w", err)
		}
	}
	if llmProvider == nil {
		return fmt.Errorf("no LLM provider configured for this conversation or workspace")
	}

	// Step 5: Determine database connection (use conversation-specific or workspace default)
	var dbConnection *models.DBConnection
	if conversation.DBConnectionID != nil {
		dbConnection, err = GetDBConnectionByID(*conversation.DBConnectionID)
		if err != nil {
			return fmt.Errorf("failed to get DB connection: %w", err)
		}
	} else {
		dbConnection, err = GetDefaultDBConnection(workspaceID)
		if err != nil {
			return fmt.Errorf("failed to get default DB connection: %w", err)
		}
	}
	// Note: dbConnection may be nil; we can still answer general questions without a database.

	// Step 6: Create a query record for tracking
	query, err := CreateQuery(workspaceID, userID, &conversationID, userMessage, &llmProvider.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to create query record: %w", err)
	}
	if dbConnection != nil {
		query.DBConnectionID = &dbConnection.ID
		// Update query with DB connection ID
		_, err = models.DB.Exec("UPDATE queries SET db_connection_id = ? WHERE id = ?", dbConnection.ID, query.ID)
		if err != nil {
			// Non‑fatal
		}
	}

	// Step 7: Build context (database schema if available)
	var schema *DatabaseSchema
	if dbConnection != nil {
		schema, err = GetDatabaseSchema(dbConnection)
		if err != nil {
			// If we can't get schema, we can still proceed but log
			_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to fetch database schema: %v", err)), nil, nil, nil)
		}
	}

	// Step 8: Parse exploration config from DB connection
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

	// Step 9: Build LLM messages
	llmMessages := buildLlmMessages(userMessage, history, schema, dbConnection)
	log.Printf("[DiscussionEngine] Message count for LLM: %d", len(llmMessages))
	for i, m := range llmMessages {
		preview := m.Content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("[DiscussionEngine]   msg[%d] role=%s content_len=%d preview=%q", i, m.Role, len(m.Content), preview)
	}

	// Step 10: Call LLM
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

	// Exploration loop
	for round := 0; round < maxRounds; round++ {
		// Check context cancellation
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
			payloadMeta := map[string]interface{}{
				"round":          round + 1,
				"request_json":   requestJSON,
				"response_json":  responseJSON,
				"llm_messages":   llmMessages,
			}
			payloadJSON, _ := json.Marshal(payloadMeta)
			payloadJSONStr := string(payloadJSON)
			_, _ = CreateConversationMessage(conversationID, "exploration", fmt.Sprintf("[Round %d — Full Payload]", round+1), nil, nil, &payloadJSONStr)
		}

		log.Printf("[DiscussionEngine] Round %d — Raw LLM response: %s", round+1, responseText)
		cleanedResponse := extractJSONFromResponse(responseText)
		var parseErr error
		if parseErr = json.Unmarshal([]byte(cleanedResponse), &llmResp); parseErr != nil {
			// JSON parse failed — check if it looks like SQL
			if looksLikeSQL(cleanedResponse) {
				llmResp = LLMResponse{
					Action:   "sql_query",
					SQLQuery: cleanedResponse,
				}
				log.Printf("[DiscussionEngine] Round %d — inferred action: sql_query (raw SQL response)", round+1)
			} else {
				// Check if it looks like JSON (starts with { or [)
				trimmed := strings.TrimSpace(cleanedResponse)
				if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
					log.Printf("[DiscussionEngine] Round %d — JSON parse failed for JSON-like response: %v", round+1, parseErr)
					llmResp = LLMResponse{
						Action:                "clarification",
						ClarificationQuestion: fmt.Sprintf("The LLM returned invalid JSON (parse error: %v). Raw:\n\n```\n%s\n```", parseErr, truncateString(cleanedResponse, 300)),
					}
				} else {
					llmResp = LLMResponse{
						Action:                "clarification",
						ClarificationQuestion: cleanedResponse,
					}
				}
				log.Printf("[DiscussionEngine] Round %d — inferred action: clarification (non-JSON, non-SQL)", round+1)
			}
		}

		log.Printf("[DiscussionEngine] Round %d — action=%s", round+1, llmResp.Action)

		// Handle missing or unknown action
		if llmResp.Action == "" {
			// Missing action field — try to infer intent
			if llmResp.SQLQuery != "" {
				llmResp.Action = "sql_query"
				log.Printf("[DiscussionEngine] Round %d — inferred action: sql_query (SQL present)", round+1)
			} else {
				llmResp.Action = "clarification"
				llmResp.ClarificationQuestion = "I received your response but couldn't determine what you wanted me to do. Please use one of: sql_query, clarification, or sql_exploration."
				log.Printf("[DiscussionEngine] Round %d — inferred action: clarification (no SQL present)", round+1)
			}
		} else if llmResp.Action != "sql_query" && llmResp.Action != "clarification" && llmResp.Action != "sql_exploration" {
			// Unknown action — retry or fallback
			if actionRetries < maxActionRetries {
				log.Printf("[DiscussionEngine] Round %d — unknown action '%s', retry %d/%d", round+1, llmResp.Action, actionRetries+1, maxActionRetries)
				// Feed retry message to LLM
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("Your previous response was valid JSON but did not include a recognized action. You must use one of: \"sql_query\", \"clarification\", or \"sql_exploration\". Please respond again with the correct format."),
				})
				actionRetries++
				continue
			}
			// Retries exhausted — infer intent
			if llmResp.SQLQuery != "" {
				llmResp.Action = "sql_query"
				log.Printf("[DiscussionEngine] Round %d — retries exhausted, inferred action: sql_query", round+1)
			} else {
				llmResp.Action = "clarification"
				llmResp.ClarificationQuestion = fmt.Sprintf("I couldn't understand your last response (action: %q). Please rephrase your request.", llmResp.Action)
				log.Printf("[DiscussionEngine] Round %d — retries exhausted, inferred action: clarification", round+1)
			}
		}

		switch llmResp.Action {
		case "sql_query":
			// Final query found — break out of loop
			if dbConnection == nil {
				_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Cannot execute SQL without a database connection"), nil, nil, nil)
				return fmt.Errorf("no database connection for SQL query")
			}
			// Append exploration results to history for the final execution
			for _, er := range explorationResults {
				history = append(history, &models.ConversationMessage{
					Role:    "exploration",
					Content: er.ToMessageContent(),
				})
			}
			executeFinalQueryWithRetry(ctx, query, llmResp, client, llmMessages, dbConnection, conversationID, explorationResults, maxFinalRetries)
			return nil

		case "clarification":
			// Clarification needed — exit loop
			return handleClarification(query, llmResp, conversationID)

		case "sql_exploration":
			if !explorationAllowed {
				// Force sql_query if exploration is disabled
				llmResp = LLMResponse{
					Action: "clarification",
					ClarificationQuestion: "Exploration queries are not allowed for this connection. Please rephrase your request.",
				}
				return handleClarification(query, llmResp, conversationID)
			}

			// Validate the exploration query
			if err := validateExplorationQuery(llmResp.SQLQuery, safetyMode); err != nil {
				log.Printf("[DiscussionEngine] Round %d — Exploration query rejected: %v", round+1, err)
				// Feed error back to LLM so it can try again
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("The previous exploration query was rejected: %s. Please revise it to comply with safety constraints.", err.Error()),
				})
				continue
			}

			// Execute exploration query
			result, err := executeSQLWithMode(dbConnection, llmResp.SQLQuery, true)
			if err != nil {
				log.Printf("[DiscussionEngine] Round %d — Exploration query failed: %v", round+1, err)
				// Feed error back to LLM
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("The exploration query failed to execute: %s. Please try a different approach.", err.Error()),
				})
				continue
			}

			// Store exploration result
			er := ExplorationResult{
				SQL:      llmResp.SQLQuery,
				Result:   result,
				Round:    round + 1,
				Explained: llmResp.Explanation,
			}
			explorationResults = append(explorationResults, er)

			// Append result to history for the LLM
			history = append(history, &models.ConversationMessage{
				Role:    "exploration",
				Content: er.ToMessageContent(),
			})
			llmMessages = append(llmMessages, ChatMessage{
				Role:    "system",
				Content: er.ToMessageContent(),
			})

			// Add a system hint for the next round
			remaining := maxRounds - round - 1
			if remaining > 0 {
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("You have %d more exploration round(s) available. Use them wisely to gather the data you need to construct the correct final SQL query.", remaining),
				})
			}

		default:
			// Should not happen (handled above), but safety net
			_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Unexpected action after retry logic: %s", llmResp.Action)), nil, nil, nil)
			return fmt.Errorf("unexpected action after retry logic: %s", llmResp.Action)
		}
	}

	// Max rounds exhausted — force final sql_query
	log.Printf("[DiscussionEngine] Exploration limit reached (%d rounds). Forcing final query.", maxRounds)
	llmMessages = append(llmMessages, ChatMessage{
		Role:    "system",
		Content: fmt.Sprintf("You have reached the exploration limit of %d rounds. You must now produce a final 'sql_query' action. Use all the data you have gathered so far.", maxRounds),
	})

	responseText, requestJSON, responseJSON, err := client.ChatCompletionWithPayload(ctx, llmMessages)
	if err != nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("LLM request failed: %v", err)), nil, nil, nil)
		return fmt.Errorf("LLM request failed: %w", err)
	}

	// Store final payload for debugging
	if requestJSON != "" && responseJSON != "" {
		payloadMeta := map[string]interface{}{
			"round":          "final",
			"request_json":   requestJSON,
			"response_json":  responseJSON,
			"llm_messages":   llmMessages,
		}
		payloadJSON, _ := json.Marshal(payloadMeta)
		payloadJSONStr := string(payloadJSON)
		_, _ = CreateConversationMessage(conversationID, "exploration", "[Final Query — Full Payload]", nil, nil, &payloadJSONStr)
	}

	cleanedResponse := extractJSONFromResponse(responseText)
	var finalResp LLMResponse
	var parseErr error
	if parseErr = json.Unmarshal([]byte(cleanedResponse), &finalResp); parseErr != nil {
		if looksLikeSQL(cleanedResponse) {
			finalResp = LLMResponse{
				Action:   "sql_query",
				SQLQuery: cleanedResponse,
			}
		} else {
			// Check if it looks like JSON (starts with { or [)
			trimmed := strings.TrimSpace(cleanedResponse)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				log.Printf("[DiscussionEngine] JSON parse failed for JSON-like response: %v", parseErr)
				finalResp = LLMResponse{
					Action:                "clarification",
					ClarificationQuestion: fmt.Sprintf("The LLM returned invalid JSON (parse error: %v). Raw:\n\n```\n%s\n```", parseErr, truncateString(cleanedResponse, 300)),
				}
			} else {
				finalResp = LLMResponse{
					Action:                "clarification",
					ClarificationQuestion: cleanedResponse,
				}
			}
		}
	}

	if finalResp.Action == "clarification" {
		return handleClarification(query, finalResp, conversationID)
	}

	if finalResp.Action != "sql_query" {
		finalResp = LLMResponse{
			Action: "sql_query",
			SQLQuery: fmt.Sprintf("-- Exploration limit reached. The LLM produced an unrecognized action: %s. Please rephrase your request.", finalResp.Action),
		}
	}

	if dbConnection == nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Cannot execute SQL without a database connection"), nil, nil, nil)
		return fmt.Errorf("no database connection for SQL query")
	}

	executeFinalQueryWithRetry(ctx, query, finalResp, client, llmMessages, dbConnection, conversationID, explorationResults, maxFinalRetries)
	return nil
}

// buildLlmMessages constructs the message list for the LLM.
func buildLlmMessages(userMessage string, history []*models.ConversationMessage, schema *DatabaseSchema, dbConnection *models.DBConnection) []ChatMessage {
	messages := []ChatMessage{}

	hasDB := dbConnection != nil
	// System prompt
	systemPrompt := buildSystemPrompt(schema, hasDB, dbConnection)
	messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})

	// History (previous user/assistant/exploration exchanges)
	for _, msg := range history {
		role := msg.Role
		if role == "user" || role == "assistant" || role == "exploration" {
			content := msg.Content
			// Prefer LLM-friendly content if available (for assistant messages with HTML)
			if msg.LLMContent != nil && *msg.LLMContent != "" {
				content = *msg.LLMContent
			} else if role == "assistant" && strings.Contains(content, "<") {
				// Fallback: strip HTML tags from assistant messages
				content = stripHTMLTags(content)
			}
			// Map exploration role to system role for LLM API compatibility
			if role == "exploration" {
				role = "system"
			}
			messages = append(messages, ChatMessage{Role: role, Content: content})

			// Inject SQL query results into context for assistant messages
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

	// Current user message
	messages = append(messages, ChatMessage{Role: "user", Content: userMessage})

	// Merge consecutive user messages to avoid breaking chat templates
	// (e.g. qwen models require alternating user/assistant turns)
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

// buildSystemPrompt creates the system prompt with schema and instructions.
func buildSystemPrompt(schema *DatabaseSchema, hasDB bool, dbConnection *models.DBConnection) string {
	var sb strings.Builder
	
	// Custom system prompt from database connection config
	if dbConnection != nil {
		config, err := dbConnection.ParseConfig()
		if err == nil && config.SystemPrompt != "" {
			sb.WriteString(config.SystemPrompt)
			sb.WriteString("\n\n")
		}
		// Add business rules section if any
		if err == nil && len(config.BusinessRules) > 0 {
			sb.WriteString("## Business Rules\n")
			for _, rule := range config.BusinessRules {
				sb.WriteString(fmt.Sprintf("- %s\n", rule))
			}
			sb.WriteString("\n")
		}
	}
	
	// Default introduction (if no custom prompt provided)
	if sb.Len() == 0 {
		sb.WriteString("You are a helpful data analyst assistant. Your task is to help users query a database using natural language.\n\n")
	} else {
		// Ensure there's a clear separation before schema
		sb.WriteString("\n")
	}

	if hasDB && schema != nil && len(schema.Tables) > 0 {
		sb.WriteString("## Database Schema\n")
		// Parse config for custom descriptions and options
		var config *models.DBConnectionConfig
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
			// Table header: name, row count, comment, custom description
			sb.WriteString(fmt.Sprintf("Table: `%s` (%d rows)", table.Name, table.RowCount))
			if table.Description != "" {
				sb.WriteString(fmt.Sprintf(" [comment: %s]", table.Description))
			}
			if desc, ok := tableDescriptions[table.Name]; ok {
				sb.WriteString(fmt.Sprintf(" [description: %s]", desc))
			}
			sb.WriteString("\n")

			// Columns with custom descriptions
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

			// Indexes
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

			// Foreign keys
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
	// Determine hint for database type
	dbTypeHint := "SQL"
	if dbConnection != nil {
		switch dbConnection.Type {
		case "sqlite":
			dbTypeHint = "SQLite"
		case "mysql":
			dbTypeHint = "MySQL"
		case "postgresql":
			dbTypeHint = "PostgreSQL"
		default:
			dbTypeHint = dbConnection.Type
		}
	}
	sb.WriteString(fmt.Sprintf("4. The SQL query must be safe, read‑only, and compatible with %s.\n", dbTypeHint))
	sb.WriteString("5. If the user asks a general question not related to the database, you may answer directly (use clarification action with a friendly response).\n")
	sb.WriteString("6. Always include a LIMIT clause in your SQL queries to prevent returning unbounded result sets. For example: `SELECT ... LIMIT 1000`.\n")
	sb.WriteString("\n")

	// Inject exploration safety constraints if applicable
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
			sb.WriteString("- All modes: read‑only only — no DML/DDL under any circumstances\n")
			sb.WriteString("- After each exploration, you will see the results and should use them to refine your final query.\n")
			sb.WriteString(fmt.Sprintf("- You have up to %d exploration round(s) before being forced to produce a final query.\n", config.MaxExplorationRounds))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("Your response must be a valid JSON object, no additional text.\n")

	prompt := sb.String()
	// Safety: if prompt is extremely long (>16KB), the small model's jinja template
	// may fail. Truncate with a note so the user message is never lost.
	if len(prompt) > 16384 {
		prompt = prompt[:16000] + "\n\n[Note: schema truncated due to context limits. The user's question follows below.]\n"
	}
	return prompt
}

// ExplorationResult holds the result of a single exploration round.
type ExplorationResult struct {
	SQL       string
	Result    *QueryResult
	Round     int
	Explained string
}

// ToMessageContent formats the exploration result as a message string for the LLM.
func (er *ExplorationResult) ToMessageContent() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Exploration Round %d]\n", er.Round))
	sb.WriteString(fmt.Sprintf("Query: %s\n", er.SQL))
	if er.Explained != "" {
		sb.WriteString(fmt.Sprintf("Reason: %s\n", er.Explained))
	}
	if er.Result != nil && er.Result.RowCount > 0 {
		sb.WriteString(fmt.Sprintf("Result: %d row(s) returned\n\n", er.Result.RowCount))
		// Show first 10 rows compactly
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
// On success, it renders results and returns nil.
// On failure, it creates an error assistant message and returns nil (the error is logged).
func executeFinalQueryWithRetry(ctx context.Context, query *models.Query, resp LLMResponse, client LLMClient, llmMessages []ChatMessage, dbConnection *models.DBConnection, conversationID uint, explorationResults []ExplorationResult, maxRetries int) {
	lastSQL := ""
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check context cancellation
		if ctx.Err() != nil {
			lastErr = fmt.Errorf("context cancelled: %w", ctx.Err())
			break
		}

		if attempt > 0 {
			// Feed error back to LLM
			llmMessages = append(llmMessages, ChatMessage{
				Role: "system",
				Content: fmt.Sprintf("The previous SQL query failed to execute:\n\n```sql\n%s\n```\n\nError: %s\n\nPlease correct the query and respond with a new 'sql_query' action.", lastSQL, lastErr.Error()),
			})

			// Get new response from LLM
			responseText, requestJSON, responseJSON, err := client.ChatCompletionWithPayload(ctx, llmMessages)
			if err != nil {
				lastErr = fmt.Errorf("LLM request failed during retry: %w", err)
				break
			}

			// Store retry payload for debugging
			if requestJSON != "" && responseJSON != "" {
				payloadMeta := map[string]interface{}{
					"round":          fmt.Sprintf("retry-%d", attempt),
					"request_json":   requestJSON,
					"response_json":  responseJSON,
					"llm_messages":   llmMessages,
				}
				payloadJSON, _ := json.Marshal(payloadMeta)
				payloadJSONStr := string(payloadJSON)
				_, _ = CreateConversationMessage(conversationID, "exploration", fmt.Sprintf("[Retry %d — Full Payload]", attempt), nil, nil, &payloadJSONStr)
			}

			cleanedResponse := extractJSONFromResponse(responseText)
			var newResp LLMResponse
			var parseErr error
			if parseErr = json.Unmarshal([]byte(cleanedResponse), &newResp); parseErr != nil {
				if looksLikeSQL(cleanedResponse) {
					newResp = LLMResponse{Action: "sql_query", SQLQuery: cleanedResponse}
				} else {
					// Check if it looks like JSON (starts with { or [)
					trimmed := strings.TrimSpace(cleanedResponse)
					if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
						log.Printf("[DiscussionEngine] JSON parse failed for JSON-like response: %v", parseErr)
						newResp = LLMResponse{Action: "clarification", ClarificationQuestion: fmt.Sprintf("The LLM returned invalid JSON (parse error: %v). Raw: %s", parseErr, truncateString(cleanedResponse, 200))}
					} else {
						newResp = LLMResponse{Action: "clarification", ClarificationQuestion: cleanedResponse}
					}
				}
			}

			if newResp.Action != "sql_query" {
				// Use the LLM's non-sql_query response directly instead of generic error
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
				_, _ = CreateConversationMessage(conversationID, "assistant", displayMsg, llmContentPtr, nil, nil)
				return
			}
			resp = newResp
		}

		// No-progress check: if the LLM produced the same query twice, stop
		if resp.SQLQuery == lastSQL {
			lastErr = fmt.Errorf("LLM produced the same query (%s) twice in a row; cannot make further progress", truncateString(resp.SQLQuery, 100))
			break
		}
		lastSQL = resp.SQLQuery

		// Execute SQL
		results, err := executeSQL(dbConnection, resp.SQLQuery)
		if err == nil {
			// Success — render results
			renderSQLResults(query, resp, dbConnection, conversationID, explorationResults, results)
			return
		}

		// Classify error
		if !isRetryableError(err) {
			lastErr = err
			break
		}

		lastErr = err
		log.Printf("[DiscussionEngine] SQL execution failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	// Exhausted retries or fatal error — show error to user
	renderSQLError(query, resp, dbConnection, conversationID, explorationResults, lastErr)
}

// handleSQLQuery executes the generated SQL and creates an assistant message.
func handleSQLQuery(query *models.Query, resp LLMResponse, dbConnection *models.DBConnection, conversationID uint, explorationResults []ExplorationResult) error {
	// Update query with generated SQL
	if err := UpdateQueryStatus(query.ID, "running", &resp.SQLQuery, nil, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}

	// Execute SQL
	results, err := executeSQL(dbConnection, resp.SQLQuery)
	if err != nil {
		_ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr(err.Error()), nil, nil, nil)
		// Create assistant message with error
		errorMsg := fmt.Sprintf("I tried to execute the SQL query but encountered an error:\n```sql\n%s\n```\n\n**Error**: %s", resp.SQLQuery, err.Error())
		_, err2 := CreateConversationMessage(conversationID, "assistant", errorMsg, nil, nil, nil)
		return fmt.Errorf("SQL execution failed: %w (message created: %v)", err, err2)
	}

	// Format results as markdown (for query status) and HTML (for display)
	resultSummary := formatResults(results)

	// Build assistant message using the structured AssistantResponse
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
	}
	assistantMessageHTML := assistantResp.ToHTML()

	// LLM-friendly content: store the original LLM response as JSON
	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	// Serialize query results for LLM context on follow-up questions
	sqlResultsJSON, err := json.Marshal(results)
	if err != nil {
		sqlResultsJSON = nil
	}
	sqlResultsPtr := stringPtr(string(sqlResultsJSON))

	// Metadata to indicate HTML content
	metadataJSON, _ := json.Marshal(map[string]interface{}{"content_type": "html"})
	metadata := string(metadataJSON)
	metadataPtr := &metadata

	// Create assistant message
	_, err = CreateConversationMessage(conversationID, "assistant", assistantMessageHTML, llmContentPtr, sqlResultsPtr, metadataPtr)
	if err != nil {
		return fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Update query as successful
	execTime := 0 // placeholder; we could measure actual execution time
	tokensUsed := 0 // placeholder
	if err := UpdateQueryStatus(query.ID, "success", &resp.SQLQuery, &resultSummary, nil, &execTime, &tokensUsed, nil); err != nil {
		return fmt.Errorf("failed to update query status: %w", err)
	}

	return nil
}

// renderSQLResults renders a successful SQL query result as an assistant message.
func renderSQLResults(query *models.Query, resp LLMResponse, dbConnection *models.DBConnection, conversationID uint, explorationResults []ExplorationResult, results *QueryResult) {
	// Format results as markdown (for query status) and HTML (for display)
	resultSummary := formatResults(results)

	// Build assistant message using the structured AssistantResponse
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
	}
	assistantMessageHTML := assistantResp.ToHTML()

	// LLM-friendly content: store the original LLM response as JSON
	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	// Serialize query results for LLM context on follow-up questions
	sqlResultsJSON, err := json.Marshal(results)
	if err != nil {
		sqlResultsJSON = nil
	}
	sqlResultsPtr := stringPtr(string(sqlResultsJSON))

	// Metadata to indicate HTML content
	metadataJSON, _ := json.Marshal(map[string]interface{}{"content_type": "html"})
	metadata := string(metadataJSON)
	metadataPtr := &metadata

	// Update query as successful
	execTime := 0
	tokensUsed := 0
	if err := UpdateQueryStatus(query.ID, "success", &resp.SQLQuery, &resultSummary, nil, &execTime, &tokensUsed, nil); err != nil {
		log.Printf("[DiscussionEngine] Failed to update query status: %v", err)
	}

	// Create assistant message
	_, err = CreateConversationMessage(conversationID, "assistant", assistantMessageHTML, llmContentPtr, sqlResultsPtr, metadataPtr)
	if err != nil {
		log.Printf("[DiscussionEngine] Failed to create assistant message: %v", err)
	}
}

// renderSQLError renders a failed SQL query as an assistant message.
func renderSQLError(query *models.Query, resp LLMResponse, dbConnection *models.DBConnection, conversationID uint, explorationResults []ExplorationResult, lastErr error) {
	// Update query as error
	if lastErr != nil {
		_ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr(lastErr.Error()), nil, nil, nil)
	}

	// Build error message
	var sqlBlock string
	if resp.SQLQuery != "" {
		sqlBlock = fmt.Sprintf("\n```sql\n%s\n```", resp.SQLQuery)
	}

	errorMsg := fmt.Sprintf("I tried to execute the SQL query but encountered an error:%s\n\n**Error**: %s", sqlBlock, lastErr.Error())

	// LLM-friendly content
	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	_, err := CreateConversationMessage(conversationID, "assistant", errorMsg, llmContentPtr, nil, nil)
	if err != nil {
		log.Printf("[DiscussionEngine] Failed to create error message: %v", err)
	}
}

// handleClarification creates an assistant message asking for clarification.
func handleClarification(query *models.Query, resp LLMResponse, conversationID uint) error {
	// Update query status
	if err := UpdateQueryStatus(query.ID, "clarification", nil, nil, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}

	// Build clarification message
	message := resp.ClarificationQuestion
	if resp.Explanation != "" {
		message = fmt.Sprintf("%s\n\n*(%s)*", resp.ClarificationQuestion, resp.Explanation)
	}

	// LLM-friendly content: store the original LLM response as JSON
	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	// Create assistant message
	_, err := CreateConversationMessage(conversationID, "assistant", message, llmContentPtr, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create clarification message: %w", err)
	}

	return nil
}

// Helper to convert string pointer.
func stringPtr(s string) *string {
	return &s
}

// stripHTMLTags removes HTML tags from a string, leaving plain text.
func stripHTMLTags(s string) string {
	// Very basic regex – sufficient for the simple HTML we generate
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// formatSQLResultsForLLM converts JSON-serialized QueryResult into a compact
// text format that the LLM can reference in follow-up questions.
func formatSQLResultsForLLM(sqlResultsJSON string) string {
	type queryResult struct {
		Columns  []string        `json:"columns"`
		Rows     [][]interface{} `json:"rows"`
		RowCount int             `json:"row_count"`
	}

	var qr queryResult
	if err := json.Unmarshal([]byte(sqlResultsJSON), &qr); err != nil {
		return ""
	}

	if len(qr.Columns) == 0 || len(qr.Rows) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[PREVIOUS QUERY RESULTS]\nSQL: %s\n\n", qr.Columns))
	sb.WriteString("Columns: " + strings.Join(qr.Columns, ", ") + "\n")

	// Cap rows to avoid bloating context for large result sets
	maxRows := 200
	if len(qr.Rows) > maxRows {
		sb.WriteString(fmt.Sprintf("Showing %d of %d rows:\n\n", maxRows, len(qr.Rows)))
	} else {
		sb.WriteString(fmt.Sprintf("(%d rows):\n\n", len(qr.Rows)))
	}

	// Header row
	sb.WriteString("| ")
	for i, col := range qr.Columns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(humanizeColumnName(col))
	}
	sb.WriteString(" |\n")
	sb.WriteString("|" + strings.Repeat("---|", len(qr.Columns)) + "\n")

	// Data rows
	for i, row := range qr.Rows {
		if i >= maxRows {
			break
		}
		sb.WriteString("| ")
		for j, val := range row {
			if j > 0 {
				sb.WriteString(" | ")
			}
			cell := fmt.Sprintf("%v", val)
			// Truncate long values
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