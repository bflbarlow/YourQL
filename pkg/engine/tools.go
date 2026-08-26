package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	toolResultSampleRows   = 50
	toolResultMaxCellChars = 200
)

// FormatToolResult produces a compact digest of query results for tool
// messages: row count, columns, up to 50 sample rows, stats for numeric
// columns.
func FormatToolResult(result *QueryResult) string {
	if result == nil {
		return "(no results)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query returned %d row(s).", result.RowCount))

	if result.RowCount == 0 {
		return sb.String()
	}

	sb.WriteString("\nColumns:")
	for i, col := range result.Columns {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(" ")
		sb.WriteString(HumanizeColumnName(col))
	}

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

	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = HumanizeColumnName(col)
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

// BuildTranscript creates a new ToolTranscript from accumulated actions.
func BuildTranscript(actions []TranscriptAction) *ToolTranscript {
	if len(actions) == 0 {
		return nil
	}
	copied := make([]TranscriptAction, len(actions))
	copy(copied, actions)
	return &ToolTranscript{Version: 1, Actions: copied}
}

// BuildToolMessages converts a canonical ToolTranscript into the ChatMessage
// sequence the currently configured provider needs, with freshly minted IDs.
func BuildToolMessages(t ToolTranscript) []ChatMessage {
	if t.Version != 1 && t.Version != 0 {
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
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(action.Arguments), &args); err == nil {
				finalRespondText = args.Text
			}
			continue
		}
		calls = append(calls, ToolCall{
			ID:       fmt.Sprintf("hist_%d", i),
			Function: ToolCallFunction{Name: action.Tool, Arguments: StripReasoningFromArgs(action.Arguments)},
		})
	}

	assistantMsg := ChatMessage{Role: "assistant", Content: finalRespondText, ToolCalls: calls}
	out = append(out, assistantMsg)

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

// StripReasoningFromArgs removes the "reasoning" field from a JSON tool-call
// arguments string before it's replayed in conversation history.
func StripReasoningFromArgs(argsJSON string) string {
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
