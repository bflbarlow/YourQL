package services

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// LLMResponse defines the structured response expected from the LLM.
type LLMResponse struct {
	Action                string `json:"action"` // "sql_query", "clarification", "sql_exploration", or "answer"
	SQLQuery              string `json:"sql_query,omitempty"`
	ClarificationQuestion string `json:"clarification_question,omitempty"`
	Answer                string `json:"answer,omitempty"`
	Explanation           string `json:"explanation,omitempty"`
	VizConfig             string `json:"viz_config,omitempty"` // raw JSON for Chart.js (contains $column refs)
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

	// Handle markdown code blocks — but ONLY when the response is wrapped
	// in a code fence (not valid JSON). If the response starts with { or [,
	// it's already JSON — skip code block extraction to avoid accidentally
	// capturing code fences inside the JSON's string values (e.g. fenced
	// code blocks in the answer field's markdown content).
	if !strings.HasPrefix(response, "{") && !strings.HasPrefix(response, "[") {
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

	// Defer: save an assistant error message if we fail, but guard against duplicates.
	// recover() catches panics (e.g. parser bugs) so they don't leave the spinner stuck.
	assistantMessageSaved := false
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: %v", r)
			log.Printf("[DiscussionEngine] Panic recovered: %v", r)
		}
		if err != nil && !assistantMessageSaved {
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
	var safetyMode ExplorationSafetyMode = ExplorationStrict
	var explorationAllowed bool = true
	var maxActionRetries int = 1
	var maxFinalRetries int = 2
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
	skillsContent, _ := GetEnabledSkillsContent(conversation.ID)
	llmMessages := buildLlmMessages(userMessage, history, schema, dbConnection, conversation.VizEnabled, skillsContent)
	log.Printf("[DiscussionEngine] Message count for LLM: %d", len(llmMessages))

	// Step 11: Call LLM
	client, err := NewLLMClient(llmProvider)
	if err != nil {
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
		_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(err))
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
			_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
			_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(err))
			return fmt.Errorf("LLM request failed: %w", err)
		}

		// Store full payload for debugging
		if requestJSON != "" && responseJSON != "" {
			_ = storePayload(conversationID, round+1, "", requestJSON, responseJSON, llmMessages)
		}

		log.Printf("[DiscussionEngine] Round %d — Raw LLM response: %s", round+1, responseText)
		cleanedResponse := extractJSONFromResponse(responseText)
		llmResp, err = parseLLMResponse(cleanedResponse)

		log.Printf("[DiscussionEngine] Round %d — action=%s", round+1, llmResp.Action)

		// Handle malformed JSON — ask the LLM to fix its formatting.
		if llmResp.Action == "_parse_error" {
			if actionRetries < maxActionRetries {
				log.Printf("[DiscussionEngine] Round %d — parse error, retry %d/%d", round+1, actionRetries+1, maxActionRetries)
				llmMessages = append(llmMessages, ChatMessage{
					Role: "system",
					Content: "Your previous response was not valid JSON. " +
						"Make sure ALL double-quotes inside string values are escaped with backslash. " +
						"For example, write \"he said \\\"hello\\\"\" — NOT \"he said \"hello\"\". " +
						"Respond with a corrected JSON object.",
				})
				actionRetries++
				continue
			}
			// Max retries exhausted — show a friendly message, not raw JSON.
			llmResp.Action = "clarification"
			llmResp.ClarificationQuestion = "I received a response I couldn't understand. Could you try rephrasing your question?"
		}

		// Handle missing or unknown action
		if llmResp.Action == "" {
			if llmResp.SQLQuery != "" {
				llmResp.Action = "sql_query"
			} else {
				llmResp.Action = "clarification"
				llmResp.ClarificationQuestion = "I received your response but couldn't determine what you wanted me to do. Please use one of: sql_query, clarification, sql_exploration, or answer."
			}
		} else if llmResp.Action != "sql_query" && llmResp.Action != "clarification" && llmResp.Action != "sql_exploration" && llmResp.Action != "answer" && llmResp.Action != "_parse_error" {
			if actionRetries < maxActionRetries {
				log.Printf("[DiscussionEngine] Round %d — unknown action '%s', retry %d/%d", round+1, llmResp.Action, actionRetries+1, maxActionRetries)
				llmMessages = append(llmMessages, ChatMessage{
					Role:    "system",
					Content: fmt.Sprintf("Your previous response was valid JSON but did not include a recognized action. You must use one of: \"sql_query\", \"clarification\", or \"sql_exploration\", or \"answer\". Please respond again with the correct format."),
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

		if llmResp.Action == "answer" && llmResp.Answer == "" {
			llmResp.Action = "clarification"
			llmResp.ClarificationQuestion = "I tried to answer but received an empty response. Could you rephrase your question?"
		}

		switch llmResp.Action {
		case "sql_query":
			if dbConnection == nil {
				_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("No database connection configured. Please add a data source in Settings."), nil, nil, nil)
				_ = UpdateQueryErrorCategory(query.ID, "no_db_connection")
				return fmt.Errorf("no database connection for SQL query")
			}
			// Append exploration results to history for final execution
			for _, er := range explorationResults {
				history = append(history, &models.ConversationMessage{
					Role:    "exploration",
					Content: er.ToMessageContent(),
				})
			}
			executeFinalQueryWithRetry(ctx, query, llmResp, client, llmMessages, dbConnection, conversation, userMessage, explorationResults, maxFinalRetries, skillsContent, onPhase)
			assistantMessageSaved = true
			return nil

		case "clarification":
			err := handleClarification(query, llmResp, conversationID)
			if err == nil {
				assistantMessageSaved = true
			}
			return err

		case "answer":
			err := handleAnswer(query, llmResp, conversationID)
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
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
		_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(err))
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

	if finalResp.Action == "_parse_error" {
		return handleClarification(query, LLMResponse{
			Action:                "clarification",
			ClarificationQuestion: "I received a response I couldn't understand. Could you try rephrasing your question?",
		}, conversationID)
	}

	if finalResp.Action == "clarification" {
		return handleClarification(query, finalResp, conversationID)
	}

	if finalResp.Action == "answer" {
		return handleAnswer(query, finalResp, conversationID)
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
		_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("No database connection configured. Please add a data source in Settings."), nil, nil, nil)
		_ = UpdateQueryErrorCategory(query.ID, "no_db_connection")
		return fmt.Errorf("no database connection for SQL query")
	}

	// Phase: SQL execution
	if onPhase != nil {
		onPhase("Running query...")
	}

	executeFinalQueryWithRetry(ctx, query, finalResp, client, llmMessages, dbConnection, conversation, userMessage, explorationResults, maxFinalRetries, skillsContent, onPhase)
	assistantMessageSaved = true

	// Phase: finalizing
	if onPhase != nil {
		onPhase("Analyzing results...")
	}
	return nil
}

// parseLLMResponse parses a cleaned LLM response string into an LLMResponse.
func parseLLMResponse(cleanedResponse string) (LLMResponse, error) {
	// Fix double-encoded formatting newlines: the LLM response JSON is
	// embedded inside the API response JSON, which double-escapes \n in
	// the JSON structure (e.g. {\n  "action"...}). Repair these to real
	// newlines so json.Unmarshal can parse the structure. String value
	// \n escapes are left intact for the parser to handle normally.
	cleanedResponse = fixJSONFormatting(cleanedResponse)
	var resp LLMResponse
	if err := json.Unmarshal([]byte(cleanedResponse), &resp); err != nil {
		if looksLikeSQL(cleanedResponse) {
			resp := LLMResponse{Action: "sql_query", SQLQuery: cleanedResponse}
			resp.unescapeFields()
			return resp, nil
		}
		trimmed := strings.TrimSpace(cleanedResponse)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			// Try lenient extraction before showing a raw parse error.
			// LLMs frequently produce JSON with unescaped quotes in
			// string values (e.g. "he said "hello" and left").
			if lenient, ok := parseLenientJSON(trimmed); ok {
				lenient.unescapeFields()
				return lenient, nil
			}
			// Lenient parsing also failed — the JSON is truly broken.
			// Return a parse-error action so the caller can retry with
			// the LLM instead of showing raw JSON to the user.
			return LLMResponse{
				Action: "_parse_error",
				Explanation: fmt.Sprintf(
					"The LLM returned invalid JSON (parse error: %v). Raw:\n\n```\n%s\n```",
					err, truncateString(cleanedResponse, 300),
				),
			}, nil
		}
		resp := LLMResponse{
			Action: "answer",
			Answer: cleanedResponse,
		}
		resp.unescapeFields()
		return resp, nil
	}
	// Fix double-encoded newlines: the LLM response is embedded inside
	// the API response JSON, which double-escapes \n. After two rounds of
	// json.Unmarshal, string fields contain literal \n instead of real
	// newlines. Apply unescaping here, on each field individually, so we
	// don't corrupt the JSON with unescaped control characters.
	resp.unescapeFields()
	return resp, nil
}

// unescapeFields converts literal \n sequences (from double JSON encoding)
// back to actual newline characters on all string fields of the response.
func (r *LLMResponse) unescapeFields() {
	r.Answer = unescapeNewlines(r.Answer)
	r.Explanation = unescapeNewlines(r.Explanation)
	r.ClarificationQuestion = unescapeNewlines(r.ClarificationQuestion)
	r.SQLQuery = unescapeNewlines(r.SQLQuery)
}

// fixJSONFormatting converts literal \n sequences to real newlines, but
// ONLY outside of JSON string values. This repairs formatting newlines that
// the LLM API double-encodes (\n in raw JSON → literal \n in content)
// without corrupting \\n inside string values (which must remain for the
// JSON parser to correctly produce literal \n in field values, which
// unescapeNewlines then converts to real newlines).
func fixJSONFormatting(raw string) string {
	var sb strings.Builder
	sb.Grow(len(raw))
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			if escaped {
				escaped = false
				sb.WriteByte(c)
			} else if c == '\\' {
				escaped = true
				sb.WriteByte(c)
			} else if c == '"' {
				inString = false
				sb.WriteByte(c)
			} else {
				sb.WriteByte(c)
			}
		} else {
			if c == '\\' && i+1 < len(raw) && raw[i+1] == 'n' {
				sb.WriteByte('\n')
				i++ // skip the 'n'
			} else if c == '"' {
				inString = true
				sb.WriteByte(c)
			} else {
				sb.WriteByte(c)
			}
		}
	}
	return sb.String()
}

// parseLenientJSON attempts to extract the action and main content field
// from a JSON response that failed strict parsing (e.g. unescaped quotes
// inside a string value). It returns the extracted response and true on
// success, or zero value and false if nothing could be salvaged.
func parseLenientJSON(raw string) (LLMResponse, bool) {
	// Extract the action field — it's short, always first, and rarely
	// contains characters that break JSON.
	actionRe := regexp.MustCompile(`"action"\s*:\s*"([^"]+)"`)
	actionMatch := actionRe.FindStringSubmatch(raw)
	if actionMatch == nil {
		return LLMResponse{}, false
	}
	action := actionMatch[1]

	// For each action type, extract the corresponding text field using
	// a boundary-based approach that tolerates unescaped inner quotes.
	switch action {
	case "clarification":
		if q := extractFieldValue(raw, "clarification_question"); q != "" {
			e := extractFieldValue(raw, "explanation")
			return LLMResponse{Action: "clarification", ClarificationQuestion: q, Explanation: e}, true
		}
	case "answer":
		if a := extractFieldValue(raw, "answer"); a != "" {
			e := extractFieldValue(raw, "explanation")
			return LLMResponse{Action: "answer", Answer: a, Explanation: e}, true
		}
	case "sql_query":
		if s := extractFieldValue(raw, "sql_query"); s != "" {
			e := extractFieldValue(raw, "explanation")
			vc := extractFieldValue(raw, "viz_config")
			return LLMResponse{Action: "sql_query", SQLQuery: s, Explanation: e, VizConfig: vc}, true
		}
	case "sql_exploration":
		if s := extractFieldValue(raw, "sql_query"); s != "" {
			e := extractFieldValue(raw, "explanation")
			return LLMResponse{Action: "sql_exploration", SQLQuery: s, Explanation: e}, true
		}
	}
	return LLMResponse{}, false
}

// extractFieldValue extracts the value of a named string field from
// broken JSON. It finds the last occurrence of `"}` after the field,
// which correctly handles unescaped inner quotes because the field
// value typically ends right before the closing brace (or before a
// subsequent field like `,"explanation"`).
func extractFieldValue(raw, fieldName string) string {
	// Find the field key.
	idx := strings.Index(raw, `"`+fieldName+`"`)
	if idx < 0 {
		return ""
	}

	// Find the opening quote of the value (after `:"` with possible spaces).
	colon := strings.Index(raw[idx:], ":")
	if colon < 0 {
		return ""
	}
	valStart := strings.Index(raw[idx+colon:], `"`)
	if valStart < 0 {
		return ""
	}
	absStart := idx + colon + valStart + 1

	// Find the closing quote. Search for `,"` (boundary to next field)
	// first, and only fall back to `"}` (end of JSON object) if no next
	// field exists. Use LastIndex so we find the actual JSON boundary,
	// not an accidental `,"` or `"}` inside the field value.
	valEnd := strings.LastIndex(raw[absStart:], "\",\"")
	if valEnd < 0 {
		valEnd = strings.LastIndex(raw[absStart:], "\"}")
	}
	if valEnd < 0 {
		return ""
	}

	val := raw[absStart : absStart+valEnd]
	// Unescape standard JSON escapes (\", \\, \n, \t)
	val = strings.ReplaceAll(val, `\\`, `\`)
	val = strings.ReplaceAll(val, `\"`, `"`)
	val = strings.ReplaceAll(val, `\n`, "\n")
	val = strings.ReplaceAll(val, `\t`, "\t")
	return val
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

// formatUserError maps an error to a user-friendly message.
// Raw error details are preserved in UpdateQueryStatus (for debugging) —
// this function produces only the text shown in the chat bubble.
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
		"raw_error":   rawError,
		"is_error":    true,
	})
	metadata := string(metadataJSON)
	return &metadata
}

// classifyErrorCategory maps an error to a stable error_category value for
// tracking in the queries table (Phase 3).
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

// buildLlmMessages constructs the message list for the LLM.
func buildLlmMessages(userMessage string, history []*models.ConversationMessage, schema *DataSchema, dbConnection *models.DataSource, vizEnabled bool, skillsContent string) []ChatMessage {
	messages := []ChatMessage{}

	hasDB := dbConnection != nil
	systemPrompt := buildSystemPrompt(schema, hasDB, dbConnection, vizEnabled, skillsContent)
	messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})

	for _, msg := range history {
		role := msg.Role
		if role == "user" || role == "assistant" || role == "exploration" {
			// Skip error messages — the user can see them in the UI, but the
			// LLM shouldn't see raw error text in its context window.
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

// formatSkillsContext formats active skills into a prompt-friendly block.
func formatSkillsContext(skillsContent string) string {
	if skillsContent == "" {
		return ""
	}
	return "\n## Additional Context (from Skills)\n" + skillsContent + "\n"
}

// executeFinalQueryWithRetry wraps SQL execution in a retry loop.
func executeFinalQueryWithRetry(ctx context.Context, query *models.Query, resp LLMResponse, client LLMClient, llmMessages []ChatMessage, dbConnection *models.DataSource, conversation *models.Conversation, userMessage string, explorationResults []ExplorationResult, maxRetries int, skillsContent string, onPhase func(string)) {
	lastSQL := ""
	var lastErr error

	// Get the dialect hint for retry prompts so the LLM knows what
	// SQL dialect to target when correcting errors.
	dialectHint := ""
	if dbConnection != nil {
		if driver, driverErr := GetDriver(dbConnection.Type); driverErr == nil {
			dialectHint = driver.SQLDialectHint()
		}
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			lastErr = fmt.Errorf("context cancelled: %w", ctx.Err())
			break
		}

		if attempt > 0 {
			// Remove the most recent assistant message from the LLM context if it
			// contains the failing SQL, so the model doesn't see its own wrong
			// answer as established context and repeat it.
			if len(llmMessages) >= 2 && llmMessages[len(llmMessages)-2].Role == "assistant" &&
				strings.Contains(llmMessages[len(llmMessages)-2].Content, lastSQL) {
				llmMessages = append(llmMessages[:len(llmMessages)-2], llmMessages[len(llmMessages)-1])
			}

			// Build the correction prompt with dialect-specific guidance.
			dialectNote := ""
			if dialectHint != "" {
				dialectNote = fmt.Sprintf("\n\nImportant: this database uses %s. "+
					"Do not use functions or syntax from other database dialects.", dialectHint)
			}

			llmMessages = append(llmMessages, ChatMessage{
				Role: "system",
				Content: fmt.Sprintf(
					"Your previous query was REJECTED by the database and MUST NOT be repeated. "+
						"You must produce a COMPLETELY DIFFERENT query. "+
						"If you repeat the same query or a trivially modified version, the system will give up.\n\n"+
						"The failing query was:\n"+
						"```sql\n%s\n```\n\n"+
						"The database error was: %s"+
						"%s\n\n"+
						"Respond with a new 'sql_query' action using a fundamentally different approach. "+
						"Do not use any function or syntax from the failing query.",
					lastSQL, lastErr.Error(), dialectNote,
				),
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
				if newResp.Action == "answer" {
					_ = handleAnswer(query, newResp, conversation.ID)
					return
				}
				if newResp.Action == "_parse_error" {
					newResp = LLMResponse{
						Action:                "clarification",
						ClarificationQuestion: "I received a response I couldn't understand. Could you try rephrasing your question?",
					}
				}
				llmContentJSON, _ := json.Marshal(newResp)
				llmContent := string(llmContentJSON)
				llmContentPtr := &llmContent

				var displayMsg string
				switch newResp.Action {
				case "clarification":
					displayMsg = newResp.ClarificationQuestion
				default:
					displayMsg = "I'm not sure how to help with that. Could you try rephrasing your question?"
				}

				_ = UpdateQueryStatus(query.ID, "error", &lastSQL, nil, stringPtr(lastErr.Error()), nil, nil, nil)
				_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(lastErr))
				_, _ = CreateConversationMessage(conversation.ID, "assistant", displayMsg, llmContentPtr, nil, buildErrorMetadata(lastErr.Error()))
				return
			}
			resp = newResp
		}

		if resp.SQLQuery == lastSQL {
			friendlyMsg := "I'm having trouble generating a new query. Could you try rephrasing your question?"
			llmContentJSON, _ := json.Marshal(resp)
			llmContent := string(llmContentJSON)
			llmContentPtr := &llmContent
			_, _ = CreateConversationMessage(conversation.ID, "assistant", friendlyMsg, llmContentPtr, nil, buildErrorMetadata("query loop detected"))
			_ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr("query loop detected"), nil, nil, nil)
			_ = UpdateQueryErrorCategory(query.ID, "query_loop")
			return
		}
		lastSQL = resp.SQLQuery

		results, err := executeSQL(dbConnection, resp.SQLQuery)
		if err == nil {
			var summary *string
			if conversation.Summarize {
				if onPhase != nil {
					onPhase("Summarizing with LLM...")
				}
				s := summarizeResults(ctx, client, userMessage, resp.SQLQuery, results, skillsContent, conversation.ID)
				if s != "" {
					summary = &s
				}
			}
			renderSQLResults(query, resp, dbConnection, conversation, explorationResults, results, summary)
			return
		}

		if !isRetryableError(err) {
			// Give unknown non-auth errors one LLM correction attempt
			// as a safety net before giving up.
			if attempt == 0 {
				lastErr = err
				log.Printf("[DiscussionEngine] Unknown error on attempt 1, trying one LLM correction: %v", err)
				time.Sleep(backoffDuration(attempt))
				// continue — the retry loop will send a correction prompt
			} else {
				lastErr = err
				break
			}
		} else {
			lastErr = err
			log.Printf("[DiscussionEngine] SQL execution failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
			time.Sleep(backoffDuration(attempt))
		}
	}

	renderSQLError(query, resp, dbConnection, conversation.ID, explorationResults, lastErr)
}

// handleAnswer creates an assistant message with a direct markdown response.
func handleAnswer(query *models.Query, resp LLMResponse, conversationID uint) error {
	if err := UpdateQueryStatus(query.ID, "answer", nil, nil, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}

	htmlContent := renderMarkdown(resp.Answer)

	// Fallback: if markdown produced no usable content (common with malformed
	// code blocks from LLMs), show the raw answer as preformatted text.
	if strings.TrimSpace(stripHTMLTags(htmlContent)) == "" && resp.Answer != "" {
		htmlContent = fmt.Sprintf("<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>", html.EscapeString(resp.Answer))
	}

	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)

	metadataJSON, _ := json.Marshal(map[string]interface{}{"content_type": "html"})
	metadata := string(metadataJSON)

	_, err := CreateConversationMessage(conversationID, "assistant",
		fmt.Sprintf("<div class=\"markdown-content\">%s</div>", htmlContent),
		&llmContent, nil, &metadata)
	if err != nil {
		return fmt.Errorf("failed to create answer message: %w", err)
	}
	return nil
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

// unescapeNewlines converts literal \n sequences (from double JSON encoding)
// back to actual newline characters so renderMarkdown can parse line-oriented
// constructs like fenced code blocks and multi-paragraph text.
func unescapeNewlines(s string) string {
	return strings.ReplaceAll(s, "\\n", "\n")
}

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
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, vizEnabled bool, skillsContent string) string {
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
	sb.WriteString("2. Decide whether you can answer directly, generate a SQL query, request clarification, or first explore the data.\n")
	sb.WriteString("3. Respond with a JSON object containing exactly the following fields:\n")
	sb.WriteString("   - \"action\": one of \"sql_query\", \"clarification\", \"sql_exploration\", or \"answer\"\n")
	sb.WriteString("   - \"sql_query\": if action is \"sql_query\" or \"sql_exploration\", provide a valid SELECT query to execute against the database. If action is \"answer\", omit this field or leave it empty — put any example SQL inside the \"answer\" markdown instead.\n")
	sb.WriteString("   - \"clarification_question\": if action is \"clarification\", ask a concise clarifying question.\n")
	sb.WriteString("   - \"answer\": if action is \"answer\", provide a direct response to the user. Use \"answer\" when the question does not require querying the database — e.g., follow-ups about previously returned data, general knowledge questions, or formatting/narrative requests. Do NOT use \"answer\" when the database has the definitive answer — prefer \"sql_query\" instead.\n")
	sb.WriteString("   - \"explanation\": internal reasoning (not shown to user — used to improve your query quality). Keep it brief.\n")
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

	if vizEnabled {
		sb.WriteString("## Data Visualization\n")
		sb.WriteString("You can generate charts when the user asks for visualizations (bar chart, line graph, pie chart, scatter plot, trend line, etc.).\n")
		sb.WriteString("To create a chart, include a \"viz_config\" field in your JSON response with a Chart.js configuration object.\n")
		sb.WriteString("Example \"viz_config\" value (as a JSON string):\n")
		sb.WriteString(`{"type":"bar","data":{"labels":["$column_name"],"datasets":[{"label":"Data","data":["$column_name"]}]}}` + "\n\n")
		sb.WriteString("Rules:\n")
		sb.WriteString(`- "type" must be one of: bar, line, pie, doughnut, scatter, radar, polarArea` + "\n")
		sb.WriteString(`- "data.labels" is an array of ONE column name from your SQL result (the category/X axis)` + "\n")
		sb.WriteString(`- "data.datasets[].data" is an array of ONE column name (the value/Y axis)` + "\n")
		sb.WriteString(`- Use "$column_name" syntax to reference SQL result columns (the system replaces them with real data)` + "\n")
		sb.WriteString("- You can define MULTIPLE datasets (as separate objects in the datasets array) for grouped/stacked charts\n")
		sb.WriteString("- For pie/doughnut: labels = category column, data = single value column (limit to 8 or fewer categories)\n")
		sb.WriteString(`- For scatter: use data: [{"x": "$col1", "y": "$col2"}] format` + "\n")
		sb.WriteString("- Choose chart type intelligently:\n")
		sb.WriteString("  * bar = comparisons, rankings, categories\n")
		sb.WriteString("  * line = time series, trends, sequential data\n")
		sb.WriteString("  * pie/doughnut = proportions, composition (<=8 categories)\n")
		sb.WriteString("  * scatter = correlation, relationship between two numeric variables\n")
		sb.WriteString("- Use visually distinct, non-gray colors for datasets (e.g. blue, red, green, orange, purple, teal) to make charts readable\n")
		sb.WriteString("- Do NOT include viz_config unless the user explicitly asks for a chart or the data clearly benefits from one\n")
		sb.WriteString("- The viz_config must be a valid JSON string (double-quote all keys and values, escape internal quotes)\n\n")
	}

	// User-defined skill context (appended after viz instructions, before the final instruction)
	if skillsContent != "" {
		sb.WriteString("\n## Additional Context (from Skills)\n")
		sb.WriteString(skillsContent)
		sb.WriteString("\n")
	}

	sb.WriteString("Your response must be a valid JSON object with only the fields listed above.\n")

	prompt := sb.String()
	if len(prompt) > 16384 {
		prompt = prompt[:16000] + "\n\n[Note: schema truncated due to context limits. The user's question follows below.]\n"
	}
	return prompt
}

// renderSQLResults renders a successful SQL query result as an assistant message.
func renderSQLResults(query *models.Query, resp LLMResponse, dbConnection *models.DataSource, conversation *models.Conversation, explorationResults []ExplorationResult, results *QueryResult, summary *string) {
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

	// Resolve chart visualization config if present and enabled
	if conversation.VizEnabled && resp.VizConfig != "" && results != nil && len(results.Columns) > 0 {
		resolved, err := resolveChartConfig(resp.VizConfig, results.Columns, results.Rows)
		if err == nil && resolved != "" {
			*metadataPtr = string(mustMarshalJSON(map[string]interface{}{
				"content_type": "html",
				"chart_config": json.RawMessage(resolved),
			}))
		}
	}

	execTime := 0
	tokensUsed := 0
	if err := UpdateQueryStatus(query.ID, "success", &resp.SQLQuery, &resultSummary, nil, &execTime, &tokensUsed, nil); err != nil {
		log.Printf("[DiscussionEngine] Failed to update query status: %v", err)
	}

	_, err = CreateConversationMessage(conversation.ID, "assistant", assistantMessageHTML, llmContentPtr, sqlResultsPtr, metadataPtr)
	if err != nil {
		log.Printf("[DiscussionEngine] Failed to create assistant message: %v", err)
	}
}

// summarizeResults sends query results back to the LLM for a natural-language summary.
func summarizeResults(ctx context.Context, client LLMClient, userQuestion, sqlQuery string, results *QueryResult, skillsContent string, conversationID uint) string {
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
- Do NOT suggest next steps — just answer the question.%s`, userQuestion, sqlQuery, formatted, formatSkillsContext(skillsContent))

	summaryMessages := []ChatMessage{
		{Role: "user", Content: prompt},
	}

	summary, requestJSON, responseJSON, err := client.ChatCompletionWithPayload(ctx, summaryMessages)
	if err != nil {
		log.Printf("[DiscussionEngine] Summarization failed: %v", err)
		return ""
	}

	// Store the summarization payload for technical details
	if requestJSON != "" && responseJSON != "" {
		payloadMeta := map[string]interface{}{
			"request_json":   requestJSON,
			"response_json":  responseJSON,
			"llm_messages":   summaryMessages,
		}
		payloadJSON, _ := json.Marshal(payloadMeta)
		payloadJSONStr := string(payloadJSON)
		_, _ = CreateConversationMessage(conversationID, "exploration", "[Summarization — Full Payload]", nil, nil, &payloadJSONStr)
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
		_ = UpdateQueryErrorCategory(query.ID, classifyErrorCategory(lastErr))
	}

	msg := formatUserError(lastErr)

	llmContentJSON, _ := json.Marshal(resp)
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	_, err := CreateConversationMessage(conversationID, "assistant", msg, llmContentPtr, nil, buildErrorMetadata(lastErr.Error()))
	if err != nil {
		log.Printf("[DiscussionEngine] Failed to create error message: %v", err)
	}
}

// resolveChartConfig replaces "$column_name" references in a Chart.js JSON config
// with actual data arrays from the query results.
func resolveChartConfig(vizConfig string, columns []string, rows [][]interface{}) (string, error) {
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(vizConfig), &config); err != nil {
		return "", fmt.Errorf("invalid viz_config JSON: %w", err)
	}

	// Build column index (case-insensitive)
	colIndex := make(map[string]int)
	for i, col := range columns {
		colIndex[strings.ToLower(col)] = i
	}

	resolved := resolveRefs(config, colIndex, rows)
	resolvedMap, _ := resolved.(map[string]interface{})

	// Post-process: handle scatter plots - zip $x/$y columns into point objects
	if typeStr, _ := resolvedMap["type"].(string); typeStr == "scatter" {
		if data, ok := resolvedMap["data"].(map[string]interface{}); ok {
			if datasets, ok := data["datasets"].([]interface{}); ok {
				for _, ds := range datasets {
					if dsMap, ok := ds.(map[string]interface{}); ok {
						if dsData, ok := dsMap["data"].([]interface{}); ok {
							points := zipScatterPoints(dsData, colIndex, rows)
							if points != nil {
								dsMap["data"] = points
							}
						}
					}
				}
			}
		}
	}

	out, err := json.Marshal(resolved)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// zipScatterPoints converts [{"x":"$col1","y":"$col2"}] into [{x:v1,y:v2}, ...]
func zipScatterPoints(items []interface{}, colIndex map[string]int, rows [][]interface{}) []interface{} {
	if len(items) == 0 {
		return nil
	}
	// Only process if the first item looks like it had $refs (now resolved to arrays)
	first, ok := items[0].(map[string]interface{})
	if !ok {
		return nil
	}
	xArr, xOk := first["x"].([]interface{})
	yArr, yOk := first["y"].([]interface{})
	if !xOk || !yOk {
		return nil
	}
	n := len(xArr)
	if len(yArr) < n {
		n = len(yArr)
	}
	points := make([]interface{}, n)
	for i := 0; i < n; i++ {
		points[i] = map[string]interface{}{"x": xArr[i], "y": yArr[i]}
	}
	return points
}

// resolveRefs walks a JSON-like tree and replaces "$column_name" strings
// with the actual column data arrays from the query rows.
func resolveRefs(node interface{}, colIndex map[string]int, rows [][]interface{}) interface{} {
	switch v := node.(type) {
	case string:
		if strings.HasPrefix(v, "$") {
			colName := strings.ToLower(v[1:])
			if idx, ok := colIndex[colName]; ok {
				data := make([]interface{}, len(rows))
				for i, row := range rows {
					if idx < len(row) {
						data[i] = row[idx]
					}
				}
				return data
			}
		}
		return v
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = resolveRefs(val, colIndex, rows)
		}
		return result
	case []interface{}:
		result := make([]interface{}, 0, len(v))
		for _, val := range v {
			resolved := resolveRefs(val, colIndex, rows)
			// Flatten: if "$col" in an array resolved to a data array, splice it in
			if str, ok := val.(string); ok && strings.HasPrefix(str, "$") {
				if arr, ok := resolved.([]interface{}); ok && len(arr) > 0 {
					result = append(result, arr...)
					continue
				}
			}
			result = append(result, resolved)
		}
		return result
	default:
		return v
	}
}

// mustMarshalJSON marshals a value to JSON or returns an empty array on error.
func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return data
}
