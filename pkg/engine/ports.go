package engine

import "context"

// LLMClient is the interface for communicating with an LLM provider.
// The canonical definition lives here; pkg/services re-exports it as a
// type alias and the four provider implementations satisfy it unchanged.
type LLMClient interface {
	ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
	ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
	ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (msg *ChatMessage, requestJSON, responseJSON string, err error)

	// ChatCompletionWithToolsStreaming sends tools and streams the response
	// via onEvent. The final assembled *ChatMessage is returned when the
	// stream completes (identical shape to the blocking path). The onEvent
	// callback is called synchronously from the SSE reader goroutine —
	// callers must not block it for long.
	ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (msg *ChatMessage, requestJSON, responseJSON string, err error)
}

// QueryExecutor runs SQL against a user's data source.
//
// SAFETY CONTRACT (AGENT_READ_FIRST.md §0, §1.4 — non-negotiable):
// every call to Execute, regardless of isExploration, MUST pass through
// ValidateReadOnlySQL (safety.go) before reaching a driver. In addition,
// when isExploration is true, Execute MUST also pass through
// ValidateExplorationQuery(sql, mode) — the complexity-mode gate
// (strict/moderate/relaxed) — using the SafetyMode configured for the
// data source. Today these are two separate checks at two separate call
// sites (validateReadOnlySQL inside executeSQLWithMode;
// validateExplorationQuery inside agentic_loop.go, called by the loop
// BEFORE executeSQLWithMode). The QueryExecutor implementation is
// responsible for preserving BOTH checks.
type QueryExecutor interface {
	Execute(sql string, isExploration bool) (*QueryResult, error)
}

// OutputHandler receives the loop's output and persists it. The loop calls
// these methods as it progresses through rounds. The implementation (in
// services/adapters/) writes conversation messages, updates query records,
// and stores tool transcripts in the app database.
type OutputHandler interface {
	// CreateQueryRecord creates a query-tracking record before the loop runs.
	// Returns the new query ID, which the loop passes to the other methods.
	CreateQueryRecord(conversationID uint, userMessage string, providerID *uint) (queryID uint, err error)

	// EmitFinalResponse is called exactly once per successful loop execution
	// when the model produces a final answer (with or without a query result).
	EmitFinalResponse(conversationID uint, queryID uint, resp FinalResponse) error

	// EmitSQLWarning is called when all error retries are exhausted.
	EmitSQLWarning(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error

	// EmitClarification is called when the loop cannot produce a final answer.
	EmitClarification(conversationID uint, queryID uint, category string, message string) error

	// StoreTechDetail records a structured tech-details entry for the toggle.
	StoreTechDetail(conversationID uint, detail TechDetail) error
}

// ConversationStore provides read/write access to conversations.
type ConversationStore interface {
	GetByID(id uint) (*ConversationMeta, error)
	GetMessages(conversationID uint) ([]*ConversationMessageMeta, error)
	CreateMessage(conversationID uint, msg NewMessage) (*ConversationMessageMeta, error)
}

// LLMProviderStore provides read access to LLM provider configs.
type LLMProviderStore interface {
	GetByID(id uint) (*LLMProviderMeta, error)
	GetDefault() (*LLMProviderMeta, error)
}

// DataSourceStore provides read access to data source configs.
type DataSourceStore interface {
	GetByID(id uint) (*DataSourceMeta, error)
	GetDefault() (*DataSourceMeta, error)
}

// SchemaIntrospector gathers database metadata.
type SchemaIntrospector interface {
	GetSchema(ds DataSourceMeta) (*DataSchema, error)
}

// ConfigurationProvider supplies runtime configuration.
type ConfigurationProvider interface {
	GetEnabledSkillsContent(conversationID uint) (string, error)
	GetAgentLoopConfig() (*AgentLoopConfig, error)
	GetTimeoutSeconds(key string, defaultSeconds int) int
}
