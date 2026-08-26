package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockLLM struct {
	responses []*ChatMessage
	calls     int
}

func (m *mockLLM) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	return "", errors.New("not used")
}

func (m *mockLLM) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	return "summary", "{}", "{}", nil
}

func (m *mockLLM) ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error) {
	if m.calls >= len(m.responses) {
		return &ChatMessage{}, "", "", nil
	}
	resp := m.responses[m.calls]
	m.calls++
	return resp, "", "", nil
}

func (m *mockLLM) ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (*ChatMessage, string, string, error) {
	return m.ChatCompletionWithTools(ctx, messages, tools)
}

type mockQueryExec struct {
	results map[string]*QueryResult
	errs    map[string]error
	calls   []string
}

func (m *mockQueryExec) Execute(sql string, isExploration bool) (*QueryResult, error) {
	m.calls = append(m.calls, sql)
	if err, ok := m.errs[sql]; ok {
		return nil, err
	}
	if r, ok := m.results[sql]; ok {
		return r, nil
	}
	return &QueryResult{}, nil
}

type mockOutput struct {
	finalCalls     []FinalResponse
	sqlWarnings    []string
	clarifications []Clarification
	techDetails    []TechDetail
	queryID        uint
}

func (m *mockOutput) CreateQueryRecord(conversationID uint, userMessage string, providerID *uint) (uint, error) {
	m.queryID++
	return m.queryID, nil
}

func (m *mockOutput) EmitFinalResponse(conversationID uint, queryID uint, resp FinalResponse) error {
	m.finalCalls = append(m.finalCalls, resp)
	return nil
}

func (m *mockOutput) EmitSQLWarning(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error {
	m.sqlWarnings = append(m.sqlWarnings, sql)
	return nil
}

func (m *mockOutput) EmitClarification(conversationID uint, queryID uint, category string, message string) error {
	m.clarifications = append(m.clarifications, Clarification{Category: category, Message: message})
	return nil
}

func (m *mockOutput) StoreTechDetail(conversationID uint, detail TechDetail) error {
	m.techDetails = append(m.techDetails, detail)
	return nil
}

func toolCall(name, args string) ToolCall {
	return ToolCall{ID: "call_1", Function: ToolCallFunction{Name: name, Arguments: args}}
}

func baseInput() LoopInput {
	return LoopInput{
		UserMessage:    "how many orders?",
		ConversationID: 1,
		QueryID:        7,
		Conversation:   ConversationMeta{ID: 1, VizEnabled: true},
		Messages:       []ChatMessage{{Role: "system", Content: "sys"}},
		Tools:          []Tool{{Type: "function", Function: FunctionDef{Name: "query_database"}}},
	}
}

func baseConfig() LoopConfig {
	return LoopConfig{
		MaxExplorationRounds: 2,
		MaxErrorRetries:      1,
		TotalRoundCap:        7,
		SafetyMode:           ExplorationRelaxed,
		AgentConfig:          &AgentLoopConfig{},
	}
}

func newLoop(llm LLMClient, qe QueryExecutor, out OutputHandler) *AgenticLoop {
	return &AgenticLoop{LLMClient: llm, QueryExecutor: qe, OutputHandler: out}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRespondOnlyNoQuery(t *testing.T) {
	llm := &mockLLM{responses: []*ChatMessage{
		{Content: "Here is the answer"},
	}}
	out := &mockOutput{}
	loop := newLoop(llm, &mockQueryExec{}, out)

	if _, err := loop.Run(context.Background(), baseInput(), baseConfig()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(out.finalCalls) != 1 {
		t.Fatalf("expected 1 final response, got %d", len(out.finalCalls))
	}
	if out.finalCalls[0].Text != "Here is the answer" {
		t.Errorf("expected text, got %q", out.finalCalls[0].Text)
	}
	if out.finalCalls[0].QueryResult != nil {
		t.Errorf("expected nil query result for respond-only")
	}
}

func TestExplorationThenFinal(t *testing.T) {
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT count(*) FROM orders","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT * FROM orders","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{toolCall("respond_to_user", `{"text":"42 orders"}`)}},
	}}
	qe := &mockQueryExec{results: map[string]*QueryResult{
		"SELECT count(*) FROM orders": {Columns: []string{"count"}, Rows: [][]interface{}{{42}}, RowCount: 1},
		"SELECT * FROM orders":        {Columns: []string{"id"}, Rows: [][]interface{}{{1}, {2}}, RowCount: 2},
	}}
	out := &mockOutput{}
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), baseConfig()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(out.finalCalls) != 1 {
		t.Fatalf("expected 1 final response, got %d", len(out.finalCalls))
	}
	if out.finalCalls[0].SQL != "SELECT * FROM orders" {
		t.Errorf("expected final SQL, got %q", out.finalCalls[0].SQL)
	}
	if len(out.finalCalls[0].ExplorationTrace) != 1 {
		t.Errorf("expected 1 exploration trace, got %d", len(out.finalCalls[0].ExplorationTrace))
	}
}

func TestErrorRetryThenSuccess(t *testing.T) {
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT bad FROM t","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT good FROM t","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{toolCall("respond_to_user", `{"text":"done"}`)}},
	}}
	qe := &mockQueryExec{
		errs: map[string]error{"SELECT bad FROM t": errors.New("Error 1054: Unknown column 'bad' in 'field list'")},
		results: map[string]*QueryResult{
			"SELECT good FROM t": {Columns: []string{"good"}, Rows: [][]interface{}{{"x"}}, RowCount: 1},
		},
	}
	out := &mockOutput{}
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), baseConfig()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(out.finalCalls) != 1 {
		t.Fatalf("expected 1 final response after retry, got %d", len(out.finalCalls))
	}
	if len(qe.calls) < 2 {
		t.Errorf("expected retry (2 queries), got %d", len(qe.calls))
	}
}

func TestErrorRetryExhausted(t *testing.T) {
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT bad FROM t","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT bad FROM t","is_exploration":false}`)}},
	}}
	qe := &mockQueryExec{errs: map[string]error{"SELECT bad FROM t": errors.New("Error 1054: Unknown column 'bad'")}}
	out := &mockOutput{}
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), baseConfig()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(out.sqlWarnings) != 1 {
		t.Fatalf("expected 1 SQL warning, got %d", len(out.sqlWarnings))
	}
}

func TestFailedExplorationDoesNotConsumeBudget(t *testing.T) {
	// MaxExplorationRounds=1. The first exploration query fails with a
	// syntax error; it must NOT consume the budget. The model then retries
	// with a corrected query (which consumes the single slot), then produces
	// a final query and answer.
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT * FROM WRONG_SYNTAX","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT count(*) FROM orders","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT * FROM orders","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{toolCall("respond_to_user", `{"text":"done"}`)}},
	}}
	qe := &mockQueryExec{
		errs: map[string]error{"SELECT * FROM WRONG_SYNTAX": errors.New("syntax error near WRONG_SYNTAX")},
		results: map[string]*QueryResult{
			"SELECT count(*) FROM orders": {Columns: []string{"count"}, Rows: [][]interface{}{{10}}, RowCount: 1},
			"SELECT * FROM orders":        {Columns: []string{"id"}, Rows: [][]interface{}{{1}}, RowCount: 1},
		},
	}
	out := &mockOutput{}
	cfg := baseConfig()
	cfg.MaxExplorationRounds = 1
	cfg.MaxErrorRetries = 1
	cfg.TotalRoundCap = 8
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(out.finalCalls) != 1 {
		t.Fatalf("expected 1 final response, got %d", len(out.finalCalls))
	}
	// The successful exploration should have executed (the failed one should
	// not have blocked the budget).
	if len(qe.calls) != 3 {
		t.Errorf("expected 3 queries (1 failed + 1 successful exploration + 1 final), got %d", len(qe.calls))
	}
}

func TestOneShotFinality(t *testing.T) {
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT * FROM t","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{
			toolCall("query_database", `{"sql":"SELECT * FROM t2","is_exploration":false}`),
			toolCall("respond_to_user", `{"text":"done"}`),
		}},
	}}
	qe := &mockQueryExec{results: map[string]*QueryResult{
		"SELECT * FROM t": {Columns: []string{"a"}, Rows: [][]interface{}{{1}}, RowCount: 1},
	}}
	out := &mockOutput{}
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), baseConfig()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// The second query_database must be rejected; only the first query executes.
	if len(qe.calls) != 1 {
		t.Errorf("one-shot finality violated: %d queries executed, want 1", len(qe.calls))
	}
	if len(out.finalCalls) != 1 {
		t.Errorf("expected final response, got %d", len(out.finalCalls))
	}
}

func TestExplorationSafetyModeStrictRejectsJoin(t *testing.T) {
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT a FROM t JOIN u ON t.id=u.id","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT * FROM t","is_exploration":false}`)}},
		{ToolCalls: []ToolCall{toolCall("respond_to_user", `{"text":"done"}`)}},
	}}
	qe := &mockQueryExec{results: map[string]*QueryResult{
		"SELECT * FROM t": {Columns: []string{"a"}, Rows: [][]interface{}{{1}}, RowCount: 1},
	}}
	out := &mockOutput{}
	cfg := baseConfig()
	cfg.SafetyMode = ExplorationStrict
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// The strict-mode exploration JOIN must be rejected before execution.
	for _, c := range qe.calls {
		if strings.Contains(c, "JOIN") {
			t.Errorf("strict mode allowed a JOIN exploration query: %q", c)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loop := newLoop(&mockLLM{}, &mockQueryExec{}, &mockOutput{})
	_, err := loop.Run(ctx, baseInput(), baseConfig())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRoundCapExhaustion(t *testing.T) {
	// The model keeps returning the same query forever; loop must stop.
	llm := &mockLLM{responses: []*ChatMessage{
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
		{ToolCalls: []ToolCall{toolCall("query_database", `{"sql":"SELECT 1","is_exploration":true}`)}},
	}}
	qe := &mockQueryExec{results: map[string]*QueryResult{
		"SELECT 1": {Columns: []string{"?"}, Rows: [][]interface{}{{1}}, RowCount: 1},
	}}
	out := &mockOutput{}
	cfg := baseConfig()
	cfg.MaxExplorationRounds = 1
	cfg.MaxErrorRetries = 1
	cfg.TotalRoundCap = 2
	loop := newLoop(llm, qe, out)

	if _, err := loop.Run(context.Background(), baseInput(), cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(out.clarifications) != 1 {
		t.Fatalf("expected 1 clarification on exhaustion, got %d", len(out.clarifications))
	}
}
