package services

import (
	"fmt"

	"YourQL/pkg/models"
)

// agentLoopDefaults holds every configurable field's hardcoded default.
// These are the current values from agentic_loop.go, frozen here as the
// system default. The map is never mutated at runtime.
var agentLoopDefaults = map[string]string{
	// -----------------------------------------------------------------------
	// Tool Descriptions (Phase 1)
	// -----------------------------------------------------------------------
	"tool.query_database.description":                       "Execute a read-only SELECT query against the connected database. Use this when the user's question requires querying data.",
	"tool.query_database.params.sql.description":            "A valid SELECT SQL query. Must be read-only. Always include a LIMIT clause.",
	"tool.query_database.params.is_exploration.description": "Set to true for behind-the-scenes queries the user should NOT see. These are your investigative tools — use them to understand the data, test assumptions, and gather context before committing to a final answer. Set to false ONLY for the single final query whose results should be shown to the user. Once you deliver a final query (is_exploration: false), you cannot run any more queries — use respond_to_user to explain your findings.",
	"tool.query_database.params.reasoning.description":      "Optional. Brief internal reasoning about why this query answers the question. This is NOT shown to the user — it exists only to improve query quality via chain-of-thought. Keep it short. Omit if not needed.",
	"tool.respond_to_user.description":                      "Provide a direct response to the user. Use this on its own for non-query answers, or alongside query_database to introduce, summarize, or add context to results. When presenting data that may need interpretation — trends, outliers, complex relationships, edge cases — include a brief analysis to help the user understand what the results mean, not just what they contain. Users appreciate when their data comes with an explanation — even a single sentence of context makes the experience feel more personal. If you're unsure whether to include commentary with a query result, err on the side of adding it. Also use this tool to ask targeted questions whenever you are uncertain — gaining confidence before your final answer matters more than answering quickly.",
	"tool.respond_to_user.params.text.description":          "The response text. Use markdown formatting for structure.",
	"tool.render_chart.description":                         "Attach a chart visualization to the results of the most recent final query_database call. ONLY call this when the user EXPLICITLY asks for a chart, graph, plot, or visualization. Do NOT call this just because the data is numeric or tabular — the default results table is preferred unless the user specifically requests a visual. Only call AFTER seeing the query result. Do not call this for exploration queries.",
	"tool.render_chart.params.chart_config.description":     "A JSON string describing a Chart.js configuration using $column references, in the same shape currently produced by the 'viz_config' field.",

	// -----------------------------------------------------------------------
	// System Instructions (Phase 2 — #1, #2, #2a–c, #4–#6; Phase 1 — #3)
	// -----------------------------------------------------------------------
	"instructions.1":  "1. Analyze the user's question and the database schema (if provided).\n",
	"instructions.2":  "2. You have three tools available:\n",
	"instructions.2a": "   - `query_database` — run a read-only SELECT query. Set `is_exploration: true` for exploratory queries, `false` for the final answer.\n",
	"instructions.2b": "   - `respond_to_user` — provide a direct text response using markdown.\n",
	"instructions.2c": "   - `render_chart` — attach a chart ONLY when the user explicitly requests one. Must be batched with `query_database` in the SAME message. Do NOT call alone or in a separate message.\n",
	"instructions.3":  "3. **Consider pairing `respond_to_user` with your final `query_database` (is_exploration: false).** When you do, your text appears above the results table — use it to introduce, explain, or analyze the results. If the data might need interpretation (e.g., trends, outliers, complex aggregations, edge cases), help the user understand what the numbers mean, not just what they show. You don't always need commentary (if the user just asked for 'a table of all orders,' skip it), but when in doubt, a brief sentence of context is appreciated. Call both tools in ONE message when you choose to pair them.\n",
	"instructions.4":  "4. **Confidence rule — before your final answer, check that you are confident in every part of it.** You must be confident that: (a) you understand what the user means, (b) your methodology and metric definitions are correct, (c) you have correctly interpreted your query results, and (d) you can complete the task within your exploration budget. If you are uncertain about ANY of these, use `respond_to_user` to ask the user a targeted question. Never guess — asking one question is always better than delivering a wrong answer.\n",
	"instructions.5":  "5. All SQL queries must be read-only (SELECT only). Follow the dialect rules in the Database Connection section above.\n",
	"instructions.6":  "6. Always include a LIMIT clause matching the row limit set in Database Connection. Do NOT end SQL queries with a semicolon (;).\n\n",

	// -----------------------------------------------------------------------
	// Exploration Safety Rules (Phase 2)
	// -----------------------------------------------------------------------
	"safety.preamble":        "## Exploration Safety Rules (CRITICAL)\n",
	"safety.preamble_detail": "Exploration queries (is_exploration: true) run in **%s** mode. Queries that violate these rules are REJECTED — you waste a round and learn nothing. Read carefully:\n\n",
	"safety.strict.rules":    "- Allowed: SELECT with LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA queries\n- Blocked: JOINs, subqueries, GROUP BY, ORDER BY\n",
	"safety.moderate.rules":  "- Allowed: everything in strict, plus single-table JOIN, GROUP BY, ORDER BY\n- Blocked: subqueries, UNION, multi-table JOINs\n",
	"safety.relaxed.rules":   "- Allowed: everything in moderate, plus subqueries and UNION\n- Blocked: INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE, and other DML/DDL\n",
	"safety.footer":          "- All modes: read-only only — no DML/DDL under any circumstances\n",
	"safety.footer_rounds":   "- You may run at most %d exploration queries total — each query_database call counts against this budget, regardless of how you spread them across rounds. Plan your queries to make each one count. Do not ask the user to change these limits.\n",
	"safety.footer_rejected": "- If an exploration query is rejected by safety mode, adapt within constraints OR use respond_to_user to tell the user what you cannot do. NEVER switch to is_exploration: false just to bypass safety — those queries surface to the user.\n",
	"safety.oneshot":         "- ONE-SHOT RULE: Once you deliver a final query (is_exploration: false), your database session closes immediately. You may ONLY call respond_to_user or render_chart after that — no more queries of any kind.\n\n",

	// -----------------------------------------------------------------------
	// Chart Guidance (Phase 2)
	// -----------------------------------------------------------------------
	"charts.intro":        "## Charts\n",
	"charts.intro_detail": "The `render_chart` tool lets you attach a Chart.js visualization — call it ONLY when the user EXPLICITLY asks for a chart, graph, plot, or visualization. Do NOT call render_chart because data looks numeric, has categories, or feels \"chartable.\" The default results table is the right answer for most queries.\n**Important:** render_chart must be called in the SAME message as query_database (is_exploration: false). It needs a pending query result from the current round — calling it alone or in a separate message will fail. Batch it with your final query: send query_database and render_chart as two tool calls in one response, or call render_chart immediately after query_database in the same tool-calls array.\n",
	"charts.format":       "The `chart_config` argument must be a JSON string with:\n- \"type\": one of bar, line, pie, doughnut, scatter, radar, polarArea\n- \"data.labels\": [\"$column_name\"] — the category/X axis column\n- \"data.datasets\": [{\"label\": \"...\", \"data\": [\"$column_name\"]}] — value/Y axis columns\n- For scatter: use `data: [{\"x\": \"$col1\", \"y\": \"$col2\"}]` format\n- If setting `backgroundColor`, use semi-transparent colors (e.g. `rgba(r,g,b,0.7)`) so bars don't look flat and opaque.\n",
	"charts.timing":       "- Batch render_chart with query_database (is_exploration: false) in the SAME response. Do NOT call render_chart in a separate message or before a final query — it will fail with no pending result.\n- Only call render_chart AFTER seeing the actual column names in the query result.\n- REMEMBER: only call this when the user explicitly requested a chart. If in doubt, skip it.\n\n",

	// -----------------------------------------------------------------------
	// Fallback Persona (Phase 2)
	// -----------------------------------------------------------------------
	"persona.fallback": "You are a helpful data analyst assistant. Your task is to help users query a database using natural language.\n\n",

	// -----------------------------------------------------------------------
	// Tool Response Messages (Phase 2)
	// -----------------------------------------------------------------------
	"response.parse_error":                        "Error parsing arguments: %v. Please retry with valid, complete JSON arguments.",
	"response.exploration_exhausted":              "Exploration budget exhausted. If you are completely confident in your answer, produce a final query_database call (is_exploration: false). If you are uncertain about any part of it, ask the user a targeted question via respond_to_user instead.",
	"response.safety_rejected":                    "Exploration query rejected: %s. Please revise it to comply with safety constraints.",
	"response.oneshot_violation":                  "Final result already delivered. Use respond_to_user if you need to explain limitations or next steps, or render_chart if the user requested a visualization.",
	"response.unknown_tool":                       "Unknown tool: %s",
	"response.render_chart_no_pending":            "render_chart failed: no pending query result to attach a chart to. Call query_database (is_exploration: false) first, then call render_chart in the SAME response (batch them as two tool calls).",
	"response.render_chart_parse_error":           "render_chart failed: error parsing arguments: %v.",
	"response.respond_parse_error":                "Error parsing arguments: %v. Please retry with valid, complete JSON arguments.",
	"response.loop_exhausted":                     "I wasn't able to complete this request due to repeated invalid responses. Could you try rephrasing your question?",
	"response.empty_truncated":                    "I received an incomplete response. Could you try rephrasing your question?",
	"response.context_overflow":                     "This question exceeds the model's context window. Try breaking it into smaller questions (ask one metric at a time), or start a new conversation so prior history doesn't consume space.",
	"response.summarization_requires_final_query": "Clarifying questions are always allowed. When summarization is enabled, prefer pairing respond_to_user with query_database (is_exploration: false) so the user receives data alongside your explanation — but if you are not confident in your interpretation, ask the user to clarify before delivering a final result.",
	"response.query_error_requires_retry":         "Your previous query returned an error. Fix the SQL error and call query_database again before responding to the user. The error message includes schema hints — use them to correct your query. Do not call respond_to_user until the query succeeds.",
	"response.too_many_tools_per_round":           "You issued more query_database calls in this single response than allowed (max %d per round). Only the first %d were executed. Spread additional queries across further rounds.",
}

// GetAgentLoopConfig returns a merged config — user overrides on top of
// hardcoded defaults. Queries the agent_loop_config table on every call
// (sub-millisecond with ~40 rows max; no caching layer needed).
func GetAgentLoopConfig() (*models.AgentLoopConfig, error) {
	rows, err := models.DB.Query("SELECT key, value FROM agent_loop_config")
	if err != nil {
		return nil, fmt.Errorf("failed to query agent loop config: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		overrides[k] = v
	}

	c := &models.AgentLoopConfig{}

	// Tool Descriptions (Phase 1)
	c.ToolQueryDatabaseDesc = coalesceConfig(overrides, "tool.query_database.description")
	c.ToolQueryDatabaseSQLDesc = coalesceConfig(overrides, "tool.query_database.params.sql.description")
	c.ToolQueryDatabaseIsExplorationDesc = coalesceConfig(overrides, "tool.query_database.params.is_exploration.description")
	c.ToolQueryDatabaseReasoningDesc = coalesceConfig(overrides, "tool.query_database.params.reasoning.description")
	c.ToolRespondToUserDesc = coalesceConfig(overrides, "tool.respond_to_user.description")
	c.ToolRespondToUserTextDesc = coalesceConfig(overrides, "tool.respond_to_user.params.text.description")
	c.ToolRenderChartDesc = coalesceConfig(overrides, "tool.render_chart.description")
	c.ToolRenderChartConfigDesc = coalesceConfig(overrides, "tool.render_chart.params.chart_config.description")

	// Instructions (Phase 1: #3; Phase 2: #1, #2, #2a–c, #4–#6)
	c.Instruction1 = coalesceConfig(overrides, "instructions.1")
	c.Instruction2 = coalesceConfig(overrides, "instructions.2")
	c.Instruction2a = coalesceConfig(overrides, "instructions.2a")
	c.Instruction2b = coalesceConfig(overrides, "instructions.2b")
	c.Instruction2c = coalesceConfig(overrides, "instructions.2c")
	c.Instruction3 = coalesceConfig(overrides, "instructions.3")
	c.Instruction4 = coalesceConfig(overrides, "instructions.4")
	c.Instruction5 = coalesceConfig(overrides, "instructions.5")
	c.Instruction6 = coalesceConfig(overrides, "instructions.6")

	// Exploration Safety Rules (Phase 2)
	c.SafetyPreamble = coalesceConfig(overrides, "safety.preamble")
	c.SafetyStrict = coalesceConfig(overrides, "safety.strict.rules")
	c.SafetyModerate = coalesceConfig(overrides, "safety.moderate.rules")
	c.SafetyRelaxed = coalesceConfig(overrides, "safety.relaxed.rules")
	c.SafetyFooter = coalesceConfig(overrides, "safety.footer")
	c.SafetyOneshot = coalesceConfig(overrides, "safety.oneshot")

	// Chart Guidance (Phase 2)
	c.ChartsIntro = coalesceConfig(overrides, "charts.intro")
	c.ChartsFormat = coalesceConfig(overrides, "charts.format")
	c.ChartsTiming = coalesceConfig(overrides, "charts.timing")

	// Fallback Persona (Phase 2)
	c.PersonaFallback = coalesceConfig(overrides, "persona.fallback")

	// Tool Response Messages (Phase 2)
	c.ResponseParseError = coalesceConfig(overrides, "response.parse_error")
	c.ResponseExplorationExhausted = coalesceConfig(overrides, "response.exploration_exhausted")
	c.ResponseSafetyRejected = coalesceConfig(overrides, "response.safety_rejected")
	c.ResponseOneshotViolation = coalesceConfig(overrides, "response.oneshot_violation")
	c.ResponseUnknownTool = coalesceConfig(overrides, "response.unknown_tool")
	c.ResponseRenderChartNoPending = coalesceConfig(overrides, "response.render_chart_no_pending")
	c.ResponseRenderChartParseError = coalesceConfig(overrides, "response.render_chart_parse_error")
	c.ResponseRespondParseError = coalesceConfig(overrides, "response.respond_parse_error")
	c.ResponseLoopExhausted = coalesceConfig(overrides, "response.loop_exhausted")
	c.ResponseEmptyTruncated = coalesceConfig(overrides, "response.empty_truncated")
	c.ResponseContextOverflow = coalesceConfig(overrides, "response.context_overflow")
	c.ResponseSummarizationRequiresFinalQuery = coalesceConfig(overrides, "response.summarization_requires_final_query")
	c.ResponseQueryErrorRequiresRetry = coalesceConfig(overrides, "response.query_error_requires_retry")
	c.ResponseTooManyToolsPerRound = coalesceConfig(overrides, "response.too_many_tools_per_round")

	return c, nil
}

// coalesceConfig returns the user's override for key, or the hardcoded
// default if no override exists (or if override is empty string).
func coalesceConfig(overrides map[string]string, key string) string {
	if v, ok := overrides[key]; ok && v != "" {
		return v
	}
	return agentLoopDefaults[key]
}

// SetAgentLoopConfigKey upserts a single config key. Passing an empty value
// or a value identical to the hardcoded default deletes the row (reverts to
// default). Returns an error if the key is not recognized.
func SetAgentLoopConfigKey(key, value string) error {
	defaultValue, exists := agentLoopDefaults[key]
	if !exists {
		return fmt.Errorf("unknown agent loop config key: %s", key)
	}
	if value == "" || value == defaultValue {
		// Revert to default — delete the override
		_, err := models.DB.Exec("DELETE FROM agent_loop_config WHERE key = ?", key)
		return err
	}
	_, err := models.DB.Exec(
		"INSERT INTO agent_loop_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set agent loop config key %s: %w", key, err)
	}
	return nil
}

// ResetAgentLoopConfig deletes all user overrides, returning every field to
// its hardcoded default.
func ResetAgentLoopConfig() error {
	_, err := models.DB.Exec("DELETE FROM agent_loop_config")
	return err
}

// AgentLoopConfigField describes a single field for the frontend.
type AgentLoopConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Section     string `json:"section"` // grouping hint for the UI
}

// AgentLoopConfigFields returns metadata for every configurable field so
// the frontend can render the correct labels and tooltips per section.
func AgentLoopConfigFields() []AgentLoopConfigField {
	return []AgentLoopConfigField{
		// Tool Descriptions
		{Key: "tool.query_database.description", Section: "tool_descriptions", Label: "query_database", Description: "The main description of the query_database tool — controls when the model decides to run a database query."},
		{Key: "tool.query_database.params.sql.description", Section: "tool_descriptions", Label: "query_database → sql parameter", Description: "Describes the sql parameter for the query_database tool. Tells the model what constitutes a valid SQL query."},
		{Key: "tool.query_database.params.is_exploration.description", Section: "tool_descriptions", Label: "query_database → is_exploration parameter", Description: "Controls visibility: true = hidden from user (exploration), false = surfaced as the final answer."},
		{Key: "tool.query_database.params.reasoning.description", Section: "tool_descriptions", Label: "query_database → reasoning parameter", Description: "Optional chain-of-thought reasoning parameter. This text is NOT shown to the user — it exists to improve query quality."},
		{Key: "tool.respond_to_user.description", Section: "tool_descriptions", Label: "respond_to_user", Description: "The description of the respond_to_user tool. Controls when the model provides natural language responses — including data interpretation, analysis, and clarifying questions when uncertain — alongside or instead of queries."},
		{Key: "tool.respond_to_user.params.text.description", Section: "tool_descriptions", Label: "respond_to_user → text parameter", Description: "Describes the text parameter for the respond_to_user tool."},
		{Key: "tool.render_chart.description", Section: "tool_descriptions", Label: "render_chart", Description: "The description of the render_chart tool. Controls when the model generates chart visualizations."},
		{Key: "tool.render_chart.params.chart_config.description", Section: "tool_descriptions", Label: "render_chart → chart_config parameter", Description: "Describes the chart_config JSON parameter for the render_chart tool."},

		// System Instructions
		{Key: "instructions.1", Section: "instructions", Label: "Instruction #1 — Analyze", Description: "Tells the model to analyze the user's question against the database schema."},
		{Key: "instructions.2", Section: "instructions", Label: "Instruction #2 — Tools intro", Description: "Introduces the available tools heading."},
		{Key: "instructions.2a", Section: "instructions", Label: "Instruction #2a — query_database bullet", Description: "The bullet point describing the query_database tool in the instructions list."},
		{Key: "instructions.2b", Section: "instructions", Label: "Instruction #2b — respond_to_user bullet", Description: "The bullet point describing the respond_to_user tool in the instructions list."},
		{Key: "instructions.2c", Section: "instructions", Label: "Instruction #2c — render_chart bullet", Description: "The bullet point describing the render_chart tool in the instructions list. Only shown when viz is enabled."},
		{Key: "instructions.3", Section: "instructions", Label: "Instruction #3 — Pairing Guidance", Description: "Tells the model when to combine respond_to_user text with query results. The most impactful behavioral instruction."},
		{Key: "instructions.4", Section: "instructions", Label: "Instruction #4 — Confidence rule", Description: "The core confidence rule: tells the model to ask a targeted question whenever it is uncertain about meaning, methodology, results, or budget before delivering a final answer."},
		{Key: "instructions.5", Section: "instructions", Label: "Instruction #5 — Read-only SQL", Description: "Reminds the model that all queries must be read-only (SELECT only). This does NOT affect Go-side enforcement."},
		{Key: "instructions.6", Section: "instructions", Label: "Instruction #6 — LIMIT clause", Description: "Reminds the model to include a LIMIT clause matching the data source's row limit."},

		// Exploration Safety Rules
		{Key: "safety.preamble", Section: "safety", Label: "Safety — Preamble heading", Description: "The ## Exploration Safety Rules heading. Displayed before the mode-specific rules."},
		{Key: "safety.strict.rules", Section: "safety", Label: "Safety — Strict mode rules", Description: "The allowed/blocked rules for strict exploration mode. Changing this does NOT change Go-side enforcement — it only changes what the model is told."},
		{Key: "safety.moderate.rules", Section: "safety", Label: "Safety — Moderate mode rules", Description: "The allowed/blocked rules for moderate exploration mode. Changing this does NOT change Go-side enforcement."},
		{Key: "safety.relaxed.rules", Section: "safety", Label: "Safety — Relaxed mode rules", Description: "The allowed/blocked rules for relaxed exploration mode. Changing this does NOT change Go-side enforcement."},
		{Key: "safety.footer", Section: "safety", Label: "Safety — Footer (read-only reminder)", Description: "The 'all modes: read-only only' reminder. Displayed after the mode-specific rules."},
		{Key: "safety.oneshot", Section: "safety", Label: "Safety — One-shot rule", Description: "The one-shot finality rule — tells the model it cannot run more queries after delivering a final result."},

		// Chart Guidance
		{Key: "charts.intro", Section: "charts", Label: "Charts — Section heading", Description: "The ## Charts heading and intro text about when to use render_chart."},
		{Key: "charts.format", Section: "charts", Label: "Charts — Format specification", Description: "The chart_config JSON format specification showing supported chart types and data formats."},
		{Key: "charts.timing", Section: "charts", Label: "Charts — Timing & restraint", Description: "Reminds the model to only call render_chart after seeing results and only when explicitly requested."},

		// Fallback Persona
		{Key: "persona.fallback", Section: "persona", Label: "Fallback Persona", Description: "The default 'You are a helpful data analyst assistant' line used when the data source has no custom system prompt."},

		// Tool Response Messages
		{Key: "response.parse_error", Section: "responses", Label: "Response — Parse error (bad JSON arguments)", Description: "Sent to the LLM when tool call arguments are malformed JSON. Includes %v placeholder for the parse error detail."},
		{Key: "response.exploration_exhausted", Section: "responses", Label: "Response — Exploration budget exhausted", Description: "Sent to the LLM when it has used all exploration queries. Tells it to produce a final result only if fully confident, otherwise to ask a targeted question."},
		{Key: "response.safety_rejected", Section: "responses", Label: "Response — Safety rejection", Description: "Sent to the LLM when an exploration query is rejected by safety mode. Includes %s placeholder for the reason."},
		{Key: "response.oneshot_violation", Section: "responses", Label: "Response — One-shot violation", Description: "Sent to the LLM when it attempts query_database after a final result was already delivered."},
		{Key: "response.unknown_tool", Section: "responses", Label: "Response — Unknown tool", Description: "Sent to the LLM when it calls a tool name that doesn't exist. Includes %s placeholder."},
		{Key: "response.render_chart_no_pending", Section: "responses", Label: "Response — render_chart with no pending result", Description: "Sent when render_chart is called but no query result is pending."},
		{Key: "response.render_chart_parse_error", Section: "responses", Label: "Response — render_chart parse error", Description: "Sent when render_chart arguments can't be parsed. Includes %v placeholder."},
		{Key: "response.respond_parse_error", Section: "responses", Label: "Response — respond_to_user parse error", Description: "Sent when respond_to_user arguments can't be parsed. Includes %v placeholder."},
		{Key: "response.loop_exhausted", Section: "responses", Label: "Response — Loop exhausted (max rounds)", Description: "Shown to the user when the agentic loop exhausts all rounds without converging on an answer."},
		{Key: "response.empty_truncated", Section: "responses", Label: "Response — Empty/truncated response", Description: "Fallback shown when the LLM returns no content and no tool calls for an unclassified reason."},
		{Key: "response.context_overflow", Section: "responses", Label: "Response — Context overflow", Description: "Shown when prompt tokens are over 90%% of the model's context window and the LLM returns 0 tokens — the question exceeded capacity."},
		{Key: "response.query_error_requires_retry", Section: "responses", Label: "Response — Query error requires retry", Description: "Feedback injected when summarization is on and the model calls respond_to_user after a failed query instead of retrying."},
	}
}
