package models

// AgentLoopConfig holds all user-configurable text for the agentic loop's
// system prompt and tool definitions. Every field ships with a hardcoded
// default (managed in services/agent_loop_config.go). User overrides are
// stored in the agent_loop_config table; nil/empty fields mean "use default."
type AgentLoopConfig struct {
	// Tool Descriptions (Phase 1)
	ToolQueryDatabaseDesc              string `json:"tool_query_database_desc"`
	ToolQueryDatabaseSQLDesc           string `json:"tool_query_database_sql_desc"`
	ToolQueryDatabaseIsExplorationDesc string `json:"tool_query_database_is_exploration_desc"`
	ToolQueryDatabaseReasoningDesc     string `json:"tool_query_database_reasoning_desc"`
	ToolRespondToUserDesc              string `json:"tool_respond_to_user_desc"`
	ToolRespondToUserTextDesc          string `json:"tool_respond_to_user_text_desc"`
	ToolRenderChartDesc                string `json:"tool_render_chart_desc"`
	ToolRenderChartConfigDesc          string `json:"tool_render_chart_config_desc"`

	// Instructions (Phase 1: #3; Phase 2: #1, #2, #2a–c, #4–#6)
	Instruction1  string `json:"instruction_1"`
	Instruction2  string `json:"instruction_2"`
	Instruction2a string `json:"instruction_2a"`
	Instruction2b string `json:"instruction_2b"`
	Instruction2c string `json:"instruction_2c"`
	Instruction3  string `json:"instruction_3"`
	Instruction4  string `json:"instruction_4"`
	Instruction5  string `json:"instruction_5"`
	Instruction6  string `json:"instruction_6"`

	// Exploration Safety Rules (Phase 2)
	SafetyPreamble string `json:"safety_preamble"`
	SafetyStrict   string `json:"safety_strict"`
	SafetyModerate string `json:"safety_moderate"`
	SafetyRelaxed  string `json:"safety_relaxed"`
	SafetyFooter   string `json:"safety_footer"`
	SafetyOneshot  string `json:"safety_oneshot"`

	// Chart Guidance (Phase 2)
	ChartsIntro  string `json:"charts_intro"`
	ChartsFormat string `json:"charts_format"`
	ChartsTiming string `json:"charts_timing"`

	// Fallback Persona (Phase 2)
	PersonaFallback string `json:"persona_fallback"`

	// Tool Response Messages (Phase 2)
	ResponseParseError            string `json:"response_parse_error"`
	ResponseExplorationExhausted  string `json:"response_exploration_exhausted"`
	ResponseSafetyRejected        string `json:"response_safety_rejected"`
	ResponseOneshotViolation      string `json:"response_oneshot_violation"`
	ResponseUnknownTool           string `json:"response_unknown_tool"`
	ResponseRenderChartNoPending  string `json:"response_render_chart_no_pending"`
	ResponseRenderChartParseError string `json:"response_render_chart_parse_error"`
	ResponseRespondParseError     string `json:"response_respond_parse_error"`
	ResponseLoopExhausted         string `json:"response_loop_exhausted"`
	ResponseEmptyTruncated        string `json:"response_empty_truncated"`
}