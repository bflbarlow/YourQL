package engine

// ---------------------------------------------------------------------------
// LLM protocol types (moved from pkg/services/llm_client.go).
// These are the canonical definitions; pkg/services re-exports them as
// type aliases so existing call sites continue to compile unchanged.
// ---------------------------------------------------------------------------

// StreamEventType enumerates the streaming event kinds.
type StreamEventType string

const (
	StreamContentDelta   StreamEventType = "content_delta"
	StreamToolCallDelta  StreamEventType = "tool_call_delta"
	StreamReasoningStart StreamEventType = "reasoning_start"
	StreamReasoningEnd   StreamEventType = "reasoning_end"
	StreamToolCallStart  StreamEventType = "tool_call_start"
	StreamToolCallEnd    StreamEventType = "tool_call_end"
	StreamDone           StreamEventType = "done"
)

// StreamEvent is emitted for each chunk of a streaming LLM response.
type StreamEvent struct {
	Type StreamEventType `json:"type"`

	// ContentDelta: incremental text from the model.
	Content string `json:"content,omitempty"`

	// ToolCallDelta: name and arguments for a tool call being built.
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`

	// ReasoningStart / ReasoningEnd: brackets around model reasoning.
	ReasoningStart bool `json:"reasoning_start,omitempty"`
	ReasoningEnd   bool `json:"reasoning_end,omitempty"`

	// ToolCallStart / ToolCallEnd: lifecycle markers for tool calls.
	ToolCallStart bool `json:"tool_call_start,omitempty"`
	ToolCallEnd   bool `json:"tool_call_end,omitempty"`

	// Done: the stream is complete.
	FinishReason string `json:"finish_reason,omitempty"`
}

// ChatMessage is a single message in an LLM conversation.
type ChatMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
	FinishReason     string     `json:"finish_reason,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
}

// ToolCall is a single function-call requested by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction identifies the tool and its JSON arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function definition exposed to the model.
type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef is the JSON-schema-like definition of a tool.
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ---------------------------------------------------------------------------
// Query result types (moved from pkg/services/sql_execution.go).
// ---------------------------------------------------------------------------

// QueryResult holds the results of a SQL query.
type QueryResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	RowCount int             `json:"row_count"`
}

// ---------------------------------------------------------------------------
// Schema types (moved from pkg/services/database_introspection.go).
// ---------------------------------------------------------------------------

// DataSchema represents the schema of a database.
type DataSchema struct {
	Tables []TableInfo `json:"tables"`
}

// TableInfo represents a table in the database.
type TableInfo struct {
	Name        string           `json:"name"`
	Columns     []ColumnInfo     `json:"columns"`
	RowCount    int64            `json:"row_count,omitempty"`
	Description string           `json:"description,omitempty"`
	Indexes     []IndexInfo      `json:"indexes,omitempty"`
	ForeignKeys []ForeignKeyInfo `json:"foreign_keys,omitempty"`
}

// ColumnInfo represents a column in a table.
type ColumnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsNullable   bool   `json:"is_nullable"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	DefaultValue string `json:"default_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

// IndexInfo represents an index on a table.
type IndexInfo struct {
	Name     string   `json:"name"`
	IsUnique bool     `json:"is_unique"`
	Columns  []string `json:"columns"`
}

// ForeignKeyInfo represents a foreign key constraint on a table.
type ForeignKeyInfo struct {
	Name      string `json:"name"`
	Column    string `json:"column"`
	RefTable  string `json:"ref_table"`
	RefColumn string `json:"ref_column"`
	OnDelete  string `json:"on_delete,omitempty"`
	OnUpdate  string `json:"on_update,omitempty"`
}

// ---------------------------------------------------------------------------
// Agentic-loop types (moved from pkg/services/agentic_loop.go).
// ---------------------------------------------------------------------------

// ExplorationResult holds the result of a single exploration round.
type ExplorationResult struct {
	SQL       string       `json:"sql"`
	Result    *QueryResult `json:"result,omitempty"`
	Round     int          `json:"round"`
	Explained string       `json:"explained,omitempty"`
}

// ToolTranscript is the canonical, provider-neutral record of one
// assistant turn's tool activity. Never contains provider-specific IDs or
// wire-format shape.
type ToolTranscript struct {
	Version int                `json:"version"`
	Actions []TranscriptAction `json:"actions"`
}

// TranscriptAction records a single tool call the model made during a turn.
type TranscriptAction struct {
	Tool          string `json:"tool"`                     // "query_database", "render_chart", "respond_to_user"
	Arguments     string `json:"arguments"`                // exact JSON arguments the model produced
	ResultPreview string `json:"result_preview,omitempty"` // truncated preview of the result
}

// TechDetail is structured per-round debugging data for the tech-details toggle.
type TechDetail struct {
	Version int    `json:"version"` // 1
	Round   int    `json:"round"`   // 0-based
	Kind    string `json:"kind"`    // "final", "exploration", "error_retry", "chart", "other"

	Request struct {
		MessageCount int    `json:"message_count"`
		LastUserMsg  string `json:"last_user_msg,omitempty"`
		RawMessages  string `json:"raw_messages,omitempty"` // full conversation JSON, untruncated
	} `json:"request"`

	Response struct {
		FinishReason     string            `json:"finish_reason,omitempty"`
		TextContent      string            `json:"text_content,omitempty"`
		RawOutput        string            `json:"raw_output,omitempty"`
		ToolCalls        []ToolCallSummary `json:"tool_calls,omitempty"`
		PromptTokens     int               `json:"prompt_tokens,omitempty"`
		CompletionTokens int               `json:"completion_tokens,omitempty"`
	} `json:"response"`

	SQL struct {
		Query      string `json:"query,omitempty"`
		ExecTimeMs int    `json:"exec_time_ms,omitempty"`
		RowCount   int    `json:"row_count,omitempty"`
		Error      string `json:"error,omitempty"`
	} `json:"sql,omitempty"`

	Stream struct {
		ChunkCount int    `json:"chunk_count,omitempty"`
		ByteCount  int    `json:"byte_count,omitempty"`
		DebugFile  string `json:"debug_file,omitempty"`
	} `json:"stream,omitempty"`

	DurationMs int `json:"duration_ms"`
}

// ToolCallSummary is a compact record of one tool call for tech details.
type ToolCallSummary struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"` // full arguments JSON, untruncated
}

// ---------------------------------------------------------------------------
// AgentLoopConfig — user-configurable text for the agentic loop's system
// prompt and tool definitions (moved from pkg/models/agent_loop_config.go).
// pkg/models re-exports it as a type alias.
// ---------------------------------------------------------------------------

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
	ResponseParseError                      string `json:"response_parse_error"`
	ResponseExplorationExhausted            string `json:"response_exploration_exhausted"`
	ResponseSafetyRejected                  string `json:"response_safety_rejected"`
	ResponseOneshotViolation                string `json:"response_oneshot_violation"`
	ResponseUnknownTool                     string `json:"response_unknown_tool"`
	ResponseRenderChartNoPending            string `json:"response_render_chart_no_pending"`
	ResponseRenderChartParseError           string `json:"response_render_chart_parse_error"`
	ResponseRespondParseError               string `json:"response_respond_parse_error"`
	ResponseLoopExhausted                   string `json:"response_loop_exhausted"`
	ResponseEmptyTruncated                  string `json:"response_empty_truncated"`
	ResponseContextOverflow                 string `json:"response_context_overflow"`
	ResponseSummarizationRequiresFinalQuery string `json:"response_summarization_requires_final_query"`
	ResponseQueryErrorRequiresRetry         string `json:"response_query_error_requires_retry"`
	ResponseTooManyToolsPerRound            string `json:"response_too_many_tools_per_round"`
}

// ---------------------------------------------------------------------------
// Black-box loop types.
// ---------------------------------------------------------------------------

// LoopInput is everything the agentic loop needs to start processing.
type LoopInput struct {
	UserMessage    string
	ConversationID uint
	QueryID        uint
	Conversation   ConversationMeta
	History        []*ConversationMessageMeta
	Schema         *DataSchema
	DBConnection   DataSourceMeta
	SkillsContent  string
	OnStream       func(StreamEvent) // nil if streaming disabled

	// Messages and Tools are built by the orchestrator (which has access to
	// the driver registry and full data-source config) and passed in. The
	// loop only consumes them — it never builds prompts or tool definitions
	// itself, which is what keeps it free of pkg/services.
	Messages []ChatMessage
	Tools    []Tool
}

// LoopOutput is everything the agentic loop produces.
type LoopOutput struct {
	// FinalResponse is populated when the loop successfully produces an answer.
	FinalResponse *FinalResponse

	// Clarification is populated when the loop cannot produce an answer
	// (context overflow, exhaustion, empty response). Nil on success.
	Clarification *Clarification

	// FatalError is populated when the loop cannot recover (LLM call failure).
	FatalError error
}

// FinalResponse is the completed assistant response.
type FinalResponse struct {
	Text             string              // respond_to_user text (markdown)
	SQL              string              // final query (empty for respond-only)
	QueryResult      *QueryResult        // nil for respond-only
	ChartConfig      string              // raw chart config JSON (empty if none)
	HasChart         bool                // true when a chart is attached
	Summary          *string             // LLM-generated summary, if enabled
	ExplorationTrace []ExplorationResult // exploration rounds
	ToolTranscript   *ToolTranscript     // full tool call transcript
}

// Clarification is a non-answer response.
type Clarification struct {
	Category string // "context_overflow", "loop_exhausted", "empty_truncated"
	Message  string // user-facing text
}

// LoopConfig is all configuration needed by the agentic loop.
type LoopConfig struct {
	MaxExplorationRounds        int
	MaxToolsPerRound            int
	MaxErrorRetries             int
	TotalRoundCap               int
	SafetyMode                  ExplorationSafetyMode
	ContextWindow               *int
	ModelName                   string
	VizEnabled                  bool
	Summarize                   bool
	StreamingEnabled            bool
	SummarizationTimeoutSeconds int
	AgentConfig                 *AgentLoopConfig
}

// ---------------------------------------------------------------------------
// Meta types — lightweight views of persisted models. The engine never
// imports pkg/models; adapters convert between these and the model structs.
// ---------------------------------------------------------------------------

// ConversationMeta is a lightweight view of a conversation.
type ConversationMeta struct {
	ID                 uint
	LLMProviderID      *uint
	DataSourceID       *uint
	MaxContextMessages int
	VizEnabled         bool
	StreamingEnabled   bool
	Summarize          bool
}

// ConversationMessageMeta is a lightweight view of a message.
type ConversationMessageMeta struct {
	ID             uint
	Role           string
	Content        string
	LLMContent     *string
	SQLResults     *string
	Metadata       *string
	ToolTranscript *string
}

// NewMessage represents a message to be persisted.
type NewMessage struct {
	Role           string
	Content        string
	LLMContent     *string
	SQLResults     *string
	Metadata       *string
	ToolTranscript *string
}

// LLMProviderMeta is a lightweight view of an LLM provider config.
type LLMProviderMeta struct {
	ID             uint
	Name           string
	Provider       string
	APIKey         string
	Model          string
	BaseURL        string
	ContextWindow  *int
	MaxTokens      *int
	ModelMaxTokens *int
}

// DataSourceMeta is a lightweight view of a data source config.
type DataSourceMeta struct {
	ID                   uint
	Name                 string
	Type                 string
	Host                 string
	Port                 int
	Database             string
	Username             string
	Password             string
	SSLMode              string
	ConfigJSON           string
	Extra                string
	FilePath             string
	FileType             string
	MaxExplorationRounds int
	ExplorationSafety    string
	MaxFinalRetries      int
	MaxToolsPerRound     int
}
