package services

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// The functions in this file implement the persistence half of the engine's
// OutputHandler interface. They are called from pkg/services/adapters and
// render engine.FinalResponse values into conversation messages using the
// same HTML + metadata shape as the pre-extraction handleRespond /
// renderToolQueryResults functions.

// EmitFinalResponseDB persists a successful loop response.
//   - When resp.QueryResult is nil, this is the respond-only path
//     (equivalent to the old handleRespond).
//   - When resp.QueryResult is non-nil, this is the final-query path
//     (equivalent to the old renderToolQueryResults).
func EmitFinalResponseDB(conversationID uint, queryID uint, resp engine.FinalResponse) error {
	if resp.QueryResult == nil {
		return emitRespondDB(conversationID, queryID, resp)
	}
	return emitQueryResultsDB(conversationID, queryID, resp)
}

func emitRespondDB(conversationID uint, queryID uint, resp engine.FinalResponse) error {
	if err := UpdateQueryStatus(queryID, "answer", nil, nil, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}

	htmlContent := engine.RenderMarkdown(resp.Text)
	if strings.TrimSpace(engine.StripHTMLTags(htmlContent)) == "" && resp.Text != "" {
		htmlContent = fmt.Sprintf("<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>", html.EscapeString(resp.Text))
	}

	var explorationHTML string
	if len(resp.ExplorationTrace) > 0 {
		explorationHTML = engine.FormatExplorationHTML(resp.ExplorationTrace)
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

	persistTranscript(conversationID, resp.ToolTranscript)
	return nil
}

func emitQueryResultsDB(conversationID uint, queryID uint, resp engine.FinalResponse) error {
	var explorationHTML string
	if len(resp.ExplorationTrace) > 0 {
		explorationHTML = engine.FormatExplorationHTML(resp.ExplorationTrace)
	}

	assistantResp := engine.AssistantResponse{
		SQL:             resp.SQL,
		Result:          resp.QueryResult,
		ExplorationHTML: explorationHTML,
		Summary:         resp.Summary,
		HasChart:        resp.HasChart,
	}

	var textAboveTableHTML string
	if resp.Summary == nil || *resp.Summary == "" {
		if resp.Text != "" {
			htmlText := engine.RenderMarkdown(resp.Text)
			if strings.TrimSpace(engine.StripHTMLTags(htmlText)) == "" && resp.Text != "" {
				htmlText = fmt.Sprintf("<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>", html.EscapeString(resp.Text))
			}
			textAboveTableHTML = fmt.Sprintf("<div class=\"markdown-content\">%s</div>", htmlText)
		}
	}

	assistantMessageHTML := assistantResp.ToHTML()
	if textAboveTableHTML != "" {
		assistantMessageHTML = textAboveTableHTML + assistantMessageHTML
	}

	llmContent := ""
	llmContentPtr := &llmContent

	sqlResultsJSON, err := json.Marshal(resp.QueryResult)
	if err != nil {
		sqlResultsJSON = nil
	}
	sqlResultsPtr := stringPtr(string(sqlResultsJSON))

	metadataMap := map[string]interface{}{"content_type": "html"}

	if resp.HasChart && resp.QueryResult != nil && len(resp.QueryResult.Columns) > 0 {
		resolved, err := engine.ResolveChartConfig(resp.ChartConfig, resp.QueryResult.Columns, resp.QueryResult.Rows)
		if err == nil && resolved != "" {
			metadataMap["chart_config"] = json.RawMessage(resolved)
		}
	}

	metadataJSON, _ := json.Marshal(metadataMap)
	metadata := string(metadataJSON)

	resultSummary := engine.FormatResults(resp.QueryResult)

	if err := UpdateQueryStatus(queryID, "success", &resp.SQL, &resultSummary, nil, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query status: %w", err)
	}

	_, err = CreateConversationMessage(conversationID, "assistant", assistantMessageHTML, llmContentPtr, sqlResultsPtr, &metadata)
	if err != nil {
		return fmt.Errorf("failed to create result message: %w", err)
	}

	persistTranscript(conversationID, resp.ToolTranscript)
	return nil
}

// EmitSQLWarningDB persists a failed-query warning message.
func EmitSQLWarningDB(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error {
	rawErr := ""
	if err != nil {
		rawErr = err.Error()
	}
	_ = UpdateQueryStatus(queryID, "error", &sql, nil, stringPtr(rawErr), nil, nil, nil)
	_ = UpdateQueryErrorCategory(queryID, engine.ClassifyErrorCategory(err))

	msg := engine.FormatUserError(err)

	llmContentJSON, _ := json.Marshal(map[string]interface{}{
		"sql_query": sql,
	})
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	_, cerr := CreateConversationMessage(conversationID, "assistant", msg, llmContentPtr, nil, engine.BuildErrorMetadata(rawErr))
	return cerr
}

// EmitClarificationDB persists a clarification (non-answer) response.
func EmitClarificationDB(conversationID uint, queryID uint, category string, message string) error {
	if err := UpdateQueryStatus(queryID, "clarification", nil, nil, stringPtr(category), nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update query: %w", err)
	}
	if category != "" {
		_ = UpdateQueryErrorCategory(queryID, category)
	}

	llmContentJSON, _ := json.Marshal(map[string]interface{}{
		"action":                 "clarification",
		"clarification_question": message,
		"failure_category":       category,
	})
	llmContent := string(llmContentJSON)
	llmContentPtr := &llmContent

	_, err := CreateConversationMessage(conversationID, "assistant", message, llmContentPtr, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create clarification message: %w", err)
	}
	return nil
}

// StoreTechDetailDB persists a tech-details debug entry.
func StoreTechDetailDB(conversationID uint, detail engine.TechDetail) {
	detailJSON, _ := json.Marshal(detail)
	detailStr := string(detailJSON)
	label := fmt.Sprintf("[Round %d — %s]", detail.Round, detail.Kind)
	_, _ = CreateConversationMessage(conversationID, "exploration", label, nil, nil, &detailStr)
}

func persistTranscript(conversationID uint, transcript *engine.ToolTranscript) {
	if transcript == nil {
		return
	}
	tj, _ := json.Marshal(transcript)
	ts := string(tj)
	_, _ = models.DB.Exec("UPDATE conversation_messages SET tool_transcript = ? WHERE id = (SELECT MAX(id) FROM conversation_messages WHERE conversation_id = ? AND role = 'assistant')", ts, conversationID)
}
