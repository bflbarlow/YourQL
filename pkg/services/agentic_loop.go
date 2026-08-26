package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// Wire-format types are canonically defined in pkg/engine and aliased here
// (AGENT_READ_FIRST.md §1.6 — never duplicate structurally identical types).
type ToolTranscript = engine.ToolTranscript
type TranscriptAction = engine.TranscriptAction
type TechDetail = engine.TechDetail
type ToolCallSummary = engine.ToolCallSummary

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
