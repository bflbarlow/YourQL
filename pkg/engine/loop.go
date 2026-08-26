package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// AgenticLoop is the isolated, black-box tool-calling execution loop. It
// depends only on the injected interfaces (LLMClient, QueryExecutor,
// OutputHandler) and the engine's own value types. It has no knowledge of
// pkg/models, pkg/services, database/sql, the filesystem, or any global
// state, which makes it fully testable with mocks.
type AgenticLoop struct {
	LLMClient     LLMClient
	QueryExecutor QueryExecutor
	OutputHandler OutputHandler
}

type roundKind string

const (
	roundKindExploration roundKind = "exploration"
	roundKindErrorRetry  roundKind = "error_retry"
	roundKindChart       roundKind = "chart"
	roundKindFinal       roundKind = "final"
	roundKindOther       roundKind = "other"
)

// pendingFinalResult is held between a successful final query_database and
// the model's optional render_chart / respond_to_user follow-up.
type pendingFinalResult struct {
	sql                string
	result             *QueryResult
	explorationResults []ExplorationResult
	transcript         *ToolTranscript
	respondText        string
}

// Run executes the tool-calling loop. It is a pure function of its inputs
// plus the injected dependencies.
//
// Behavior guarantees (AGENT_READ_FIRST.md §1.3):
//   - One-shot finality: after the first successful is_exploration:false
//     query, further query_database calls are rejected.
//   - Exploration complexity modes are enforced via ValidateExplorationQuery
//     for every exploration query, in addition to the read-only invariant
//     enforced by QueryExecutor.
//   - The loop never exceeds config.TotalRoundCap iterations.
func (a *AgenticLoop) Run(ctx context.Context, input LoopInput, config LoopConfig) (*LoopOutput, error) {
	cfg := config.AgentConfig
	if cfg == nil {
		cfg = &AgentLoopConfig{}
	}

	messages := input.Messages
	tools := input.Tools
	schema := input.Schema
	conversation := input.Conversation
	maxExplorationRounds := config.MaxExplorationRounds
	maxToolsPerRound := config.MaxToolsPerRound
	maxErrorRetries := config.MaxErrorRetries
	safetyMode := config.SafetyMode
	contextWindow := config.ContextWindow

	var pendingFinal *pendingFinalResult
	var explorationResults []ExplorationResult
	var transcriptActions []TranscriptAction
	var explorationToolCallsUsed, errorRetriesUsed int
	var toolsThisRound int
	var lastQueryHadError bool
	var renderChartNeedsRetry bool
	totalRoundCap := config.TotalRoundCap
	if totalRoundCap <= 0 {
		totalRoundCap = maxExplorationRounds + maxErrorRetries + 4
	}
	loopStart := time.Now()
	log.Printf("[AgenticLoop] Starting loop — maxRounds=%d, maxTools=%d, maxRetries=%d, safety=%d, messages=%d",
		maxExplorationRounds, maxToolsPerRound, maxErrorRetries, safetyMode, len(messages))

	for round := 0; round < totalRoundCap; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		toolsThisRound = 0
		log.Printf("[AgenticLoop] Round %d — elapsed=%v, messages=%d, tools=%d",
			round, time.Since(loopStart).Round(time.Second), len(messages), len(tools))

		var response *ChatMessage
		var rawResponse string
		var llmErr error
		var streamChunks, streamBytes int
		if input.OnStream != nil {
			wrappedStream := func(ev StreamEvent) {
				streamChunks++
				if ev.Type == StreamDone {
					streamBytes = streamChunks
				}
				input.OnStream(ev)
			}
			response, _, rawResponse, llmErr = a.LLMClient.ChatCompletionWithToolsStreaming(ctx, messages, tools, wrappedStream)
		} else {
			response, _, rawResponse, llmErr = a.LLMClient.ChatCompletionWithTools(ctx, messages, tools)
		}
		if llmErr != nil {
			log.Printf("[AgenticLoop] LLM call FAILED round=%d elapsed=%v messages=%d: %v",
				round, time.Since(loopStart).Round(time.Second), len(messages), llmErr)
			return &LoopOutput{FatalError: fmt.Errorf("LLM call failed: %w", llmErr)}, nil
		}

		kind := roundKindOther
		td := TechDetail{Version: 1, Round: round}
		roundStart := time.Now()

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

		storedThisRound := false
		logRound := func(k roundKind) {
			kind = k
			td.Kind = string(k)
			td.Request.MessageCount = len(messages)
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					td.Request.LastUserMsg = TruncateString(messages[i].Content, 200)
					break
				}
			}
			if msgJSON, err := json.Marshal(messages); err == nil {
				td.Request.RawMessages = string(msgJSON)
			}
			td.DurationMs = int(time.Since(roundStart).Milliseconds())
			if !storedThisRound {
				storedThisRound = true
				_ = a.OutputHandler.StoreTechDetail(input.ConversationID, td)
			}
		}

		messages = append(messages, *response)

		// Plain text instead of a tool call — treat as respond_to_user.
		if response.Content != "" && len(response.ToolCalls) == 0 {
			logRound(roundKindOther)
			if pendingFinal != nil {
				argsJSON, _ := json.Marshal(map[string]string{"text": response.Content})
				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:      "respond_to_user",
					Arguments: string(argsJSON),
				})
				pendingFinal.transcript = BuildTranscript(transcriptActions)
				pendingFinal.respondText = response.Content
				return a.renderFinal(pendingFinal, "", input, config)
			}
			if config.Summarize && lastQueryHadError && pendingFinal == nil {
				logRound(roundKindOther)
				messages = append(messages, ChatMessage{
					Role:    "tool",
					Content: cfg.ResponseQueryErrorRequiresRetry,
				})
				continue
			}
			transcript := BuildTranscript(transcriptActions)
			return a.emitRespond(response.Content, explorationResults, transcript, input)
		}

		// Empty response — truncated or malformed.
		if len(response.ToolCalls) == 0 {
			logRound(roundKindOther)
			if pendingFinal != nil {
				if _, rerr := a.renderFinal(pendingFinal, "", input, config); rerr != nil {
					return nil, rerr
				}
				return nil, nil
			}
			category := "empty_response"
			detail := "model returned 0 tokens and no tool calls"
			clarMsg := cfg.ResponseEmptyTruncated
			if response.PromptTokens > 0 && response.CompletionTokens == 0 &&
				contextWindow != nil && *contextWindow > 0 &&
				response.PromptTokens >= (*contextWindow*9/10) {
				category = "context_overflow"
				detail = fmt.Sprintf("prompt %d tokens vs %d context limit; 0 completion tokens", response.PromptTokens, *contextWindow)
				clarMsg = cfg.ResponseContextOverflow
			}
			return a.emitClarification(category, detail, clarMsg, input)
		}

		for toolIdx, tc := range response.ToolCalls {
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

				if args.IsExploration && explorationToolCallsUsed >= maxExplorationRounds {
					kind = roundKindExploration
					logRound(kind)
					messages = append(messages, ChatMessage{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
						Content: cfg.ResponseExplorationExhausted,
					})
					continue
				}

				// Exploration safety gate (complexity mode). The read-only
				// invariant itself is additionally enforced by QueryExecutor.
				if args.IsExploration {
					if verr := ValidateExplorationQuery(args.SQL, safetyMode); verr != nil {
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

				result, execErr := a.QueryExecutor.Execute(args.SQL, args.IsExploration)
				toolContent := ""
				if execErr == nil {
					toolContent = FormatToolResult(result)
					lastQueryHadError = false
				} else {
					toolContent = fmt.Sprintf("Query failed: %s", AugmentSQLError(execErr.Error()))
				}

				transcriptActions = append(transcriptActions, TranscriptAction{
					Tool:          "query_database",
					Arguments:     tc.Function.Arguments,
					ResultPreview: toolContent,
				})

				if execErr != nil {
					lastQueryHadError = true
					if errors.Is(execErr, context.Canceled) {
						return nil, context.Canceled
					}
					if args.IsExploration {
						kind = roundKindExploration
						// Do NOT count a failed exploration against the budget.
						// The model wrote incorrect SQL (syntax error, wrong column,
						// reserved word, etc.) — the error feedback tells it to
						// correct the query, so penalizing it wastes a slot and
						// makes the experience punitive. Only successful and
						// safety-rejected explorations consume the budget.
					} else {
						kind = roundKindErrorRetry
						if errorRetriesUsed >= maxErrorRetries {
							logRound(kind)
							a.emitSQLWarning(args.SQL, execErr, input)
							return nil, nil
						}
						errorRetriesUsed++
						if !IsRetryableError(execErr) {
							logRound(kind)
							a.emitSQLWarning(args.SQL, execErr, input)
							return nil, nil
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
						transcript:         BuildTranscript(transcriptActions),
					}

					if response.Content != "" {
						argsJSON, _ := json.Marshal(map[string]string{"text": response.Content})
						transcriptActions = append(transcriptActions, TranscriptAction{
							Tool:      "respond_to_user",
							Arguments: string(argsJSON),
						})
						pendingFinal.transcript = BuildTranscript(transcriptActions)
						if !renderChartNeedsRetry {
							pendingFinal.respondText = response.Content
						}
					}

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
							return a.renderFinal(pendingFinal, "", input, config)
						}
					}
					continue
				}

				kind = roundKindExploration
				explorationToolCallsUsed++
				logRound(kind)

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
				pendingFinal.transcript = BuildTranscript(transcriptActions)
				return a.renderFinal(pendingFinal, args.ChartConfig, input, config)

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
					hasChartAfter := false
					for k := toolIdx + 1; k < len(response.ToolCalls); k++ {
						if response.ToolCalls[k].Function.Name == "render_chart" {
							hasChartAfter = true
							break
						}
					}
					if hasChartAfter {
						pendingFinal.respondText = args.Text
						continue
					}
					pendingFinal.respondText = args.Text
					pendingFinal.transcript = BuildTranscript(transcriptActions)
					return a.renderFinal(pendingFinal, "", input, config)
				}

				if config.Summarize && lastQueryHadError {
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    cfg.ResponseQueryErrorRequiresRetry,
					})
					continue
				}

				transcript := BuildTranscript(transcriptActions)
				return a.emitRespond(args.Text, explorationResults, transcript, input)

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

	if pendingFinal != nil {
		return a.renderFinal(pendingFinal, "", input, config)
	}
	return a.emitClarification("loop_exhausted", "agentic loop reached the round limit", cfg.ResponseLoopExhausted, input)
}

// renderFinal computes the summary (when enabled) and emits the final result.
func (a *AgenticLoop) renderFinal(p *pendingFinalResult, chartConfig string, input LoopInput, config LoopConfig) (*LoopOutput, error) {
	var summary *string
	if config.Summarize && p.result != nil {
		timeout := config.SummarizationTimeoutSeconds
		if timeout <= 0 {
			// 300s accommodates reasoning models (Qwen/Claude/OpenAI o-series)
			// that emit a long chain-of-thought before the summary text. The
			// value is overridable per deployment via app_settings
			// `summarization_timeout_seconds`.
			timeout = 300
		}
		sCtx, sCancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		s, err := a.summarize(sCtx, input.UserMessage, p.sql, p.result, input.SkillsContent, input.ConversationID, input.OnStream)
		sCancel()
		if err != nil {
			log.Printf("[AgenticLoop] Summarization failed: %v", err)
			fallback := "⚠️ Summary unavailable — the model could not generate one for this result set."
			summary = &fallback
		} else if s != "" {
			summary = &s
		}
	}

	resp := FinalResponse{
		Text:             p.respondText,
		SQL:              p.sql,
		QueryResult:      p.result,
		ChartConfig:      chartConfig,
		HasChart:         chartConfig != "",
		Summary:          summary,
		ExplorationTrace: p.explorationResults,
		ToolTranscript:   p.transcript,
	}
	if err := a.OutputHandler.EmitFinalResponse(input.ConversationID, input.QueryID, resp); err != nil {
		return nil, err
	}
	return nil, nil
}

func (a *AgenticLoop) emitRespond(text string, explorationResults []ExplorationResult, transcript *ToolTranscript, input LoopInput) (*LoopOutput, error) {
	resp := FinalResponse{
		Text:             text,
		ExplorationTrace: explorationResults,
		ToolTranscript:   transcript,
	}
	if err := a.OutputHandler.EmitFinalResponse(input.ConversationID, input.QueryID, resp); err != nil {
		return nil, err
	}
	return nil, nil
}

func (a *AgenticLoop) emitSQLWarning(sql string, err error, input LoopInput) {
	_ = a.OutputHandler.EmitSQLWarning(input.ConversationID, input.QueryID, sql, err, IsRetryableError(err))
}

func (a *AgenticLoop) emitClarification(category, detail, message string, input LoopInput) (*LoopOutput, error) {
	if err := a.OutputHandler.EmitClarification(input.ConversationID, input.QueryID, category, message); err != nil {
		return nil, err
	}
	return nil, nil
}

// summarize sends query results back to the LLM for a natural-language summary.
// When onStream is non-nil, the summarization uses the same streaming path as
// the main pipeline so the model's reasoning and content chunks reach the UI
// in real time (otherwise it falls back to the blocking path).
func (a *AgenticLoop) summarize(ctx context.Context, userQuestion, sqlQuery string, results *QueryResult, skillsContent string, conversationID uint, onStream func(StreamEvent)) (string, error) {
	if results == nil || results.RowCount == 0 {
		return "", nil
	}

	formatted := FormatResultsDigestForSummarization(results)
	if formatted == "" {
		return "", nil
	}

	prompt := fmt.Sprintf(`You are a helpful data analyst. Summarize the following SQL query results in plain English, directly answering the user's question.

**User's question**: %s

**SQL executed**:
`+"```sql\n%s\n```"+`

**Query result digest**:
%s
**Instructions**:
- Answer the user's question directly and thoroughly, referencing specific numbers and facts from the digest.
- If the results are empty, clearly state that no data matched the query.
- Use markdown formatting for structure — headings, lists, and markdown tables are all fine. The user will see this as rich formatted text above their full results table.
- Feel free to suggest next steps, actionable takeaways, or follow-up questions if they add value — the user appreciates a complete analysis.
- Frame your summary as an analysis of the overall result. Do not hedge with phrases like "based on the sample" or "the subset shows" — you are summarizing the full result, which the user sees below.%s`, userQuestion, sqlQuery, formatted, FormatSkillsContext(skillsContent))

	summaryMessages := []ChatMessage{
		{Role: "user", Content: prompt},
	}

	var summary string
	var requestJSON string
	var err error
	if onStream != nil {
		var msg *ChatMessage
		// Use the streaming path (with no tools) so reasoning/content chunks
		// are emitted to the UI as they arrive. This mirrors the main loop's
		// behavior and gives the user visible activity during the summary.
		msg, requestJSON, _, err = a.LLMClient.ChatCompletionWithToolsStreaming(ctx, summaryMessages, nil, onStream)
		if err == nil && msg != nil {
			summary = msg.Content
		}
	} else {
		summary, requestJSON, _, err = a.LLMClient.ChatCompletionWithPayload(ctx, summaryMessages)
	}
	if err != nil {
		return "", fmt.Errorf("LLM call for summarization failed: %w", err)
	}

	if requestJSON != "" {
		td := TechDetail{
			Version: 1,
			Round:   0,
			Kind:    "summarization",
			Request: struct {
				MessageCount int    `json:"message_count"`
				LastUserMsg  string `json:"last_user_msg,omitempty"`
				RawMessages  string `json:"raw_messages,omitempty"`
			}{
				MessageCount: 1,
				LastUserMsg:  TruncateString(prompt, 200),
			},
		}
		td.Response.TextContent = TruncateString(summary, 500)
		_ = a.OutputHandler.StoreTechDetail(conversationID, td)
	}

	return strings.TrimSpace(summary), nil
}
