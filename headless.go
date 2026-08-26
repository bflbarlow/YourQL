package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"YourQL/pkg/models"
	"YourQL/pkg/services"
)

// headlessServer holds state shared by all headless HTTP handlers.
type headlessServer struct {
	mu       sync.Mutex
	inflight map[uint]bool               // conversation ID → actively processing?
	cancels  map[uint]context.CancelFunc // conversation ID → cancel func
	start    time.Time                   // process start time (for uptime)
	dbPath   string                      // resolved database path
}

func newHeadlessServer(dbPath string) *headlessServer {
	return &headlessServer{
		inflight: make(map[uint]bool),
		cancels:  make(map[uint]context.CancelFunc),
		start:    time.Now(),
		dbPath:   dbPath,
	}
}

// runHeadless connects to the database (the given path or the default),
// starts the HTTP server, and blocks until shutdown.
func runHeadless(port int, dbPath string) error {
	if dbPath != "" {
		if err := models.ConnectDatabaseAt(dbPath); err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
	} else if err := models.ConnectDatabase(); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer models.DB.Close()

	setupLogging()
	services.SetAppVersionGetter(func() string { return appVersion })

	resolved := dbPath
	if resolved == "" {
		resolved = models.DefaultDBPath()
	}
	hs := newHeadlessServer(resolved)
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /api/health", hs.handleHealth)

	// Conversations
	mux.HandleFunc("POST /api/conversations", hs.handleCreateConversation)
	mux.HandleFunc("GET /api/conversations/{id}", hs.handleGetConversation)
	mux.HandleFunc("PATCH /api/conversations/{id}", hs.handlePatchConversation)

	// Messages
	mux.HandleFunc("POST /api/conversations/{id}/messages", hs.handleSendMessage)
	mux.HandleFunc("POST /api/conversations/{id}/messages/stream", hs.handleSendMessageSSE)
	mux.HandleFunc("GET /api/conversations/{id}/messages", hs.handleGetMessages)
	mux.HandleFunc("POST /api/conversations/{id}/cancel", hs.handleCancel)

	// Queries
	mux.HandleFunc("GET /api/conversations/{id}/queries", hs.handleGetQueries)

	// Settings (app_settings table — app-local, never data sources)
	mux.HandleFunc("GET /api/settings", hs.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", hs.handlePutSettings)

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	slog.Info("headless server listening", "addr", srv.Addr, "db", resolved)
	return srv.ListenAndServe()
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

func parseID(r *http.Request) (uint, error) {
	s := r.PathValue("id")
	if s == "" {
		return 0, fmt.Errorf("missing id")
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}
	return uint(v), nil
}

// --- message envelope ---

type messageRequest struct {
	Message string `json:"message"`
}

type messageResponse struct {
	ConversationID uint                          `json:"conversation_id"`
	Status         string                        `json:"status"`
	Error          *string                       `json:"error"`
	DurationMS     int64                         `json:"duration_ms"`
	Phase          string                        `json:"phase"`
	MessagesAdded  []*models.ConversationMessage `json:"messages_added"`
	Query          *models.Query                 `json:"query,omitempty"`
	StreamEvents   []services.StreamEvent        `json:"stream_events"`
}

// processMessage runs the full pipeline for a message and returns an
// envelope (blocking). processMessage handles single-flight guarding, cancel
// registration, pre/post message/query diffing, and status mapping.
// Caller-provided callbacks are invoked synchronously (onStream is called
// from the LLM streaming goroutine — callers must not block it long).
func (s *headlessServer) processMessage(
	conversationID uint, userMessage string,
	onPhase func(string), onStream func(services.StreamEvent),
) (*messageResponse, error) {
	// Validate conversation exists
	if _, err := services.GetConversationByID(conversationID); err != nil {
		return nil, err
	}

	// Single-flight guard
	s.mu.Lock()
	if s.inflight[conversationID] {
		s.mu.Unlock()
		return nil, fmt.Errorf("processing_already_active")
	}
	s.inflight[conversationID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, conversationID)
		delete(s.cancels, conversationID)
		s.mu.Unlock()
	}()

	// Snapshot pre-existing state
	beforeMsgs, _ := services.GetConversationMessages(conversationID)
	beforeIDs := map[uint]bool{}
	for _, m := range beforeMsgs {
		beforeIDs[m.ID] = true
	}
	beforeQueries, _ := services.GetQueriesByConversation(conversationID)
	var maxBeforeQuery uint
	for _, q := range beforeQueries {
		if q.ID > maxBeforeQuery {
			maxBeforeQuery = q.ID
		}
	}

	// Run the pipeline
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[conversationID] = cancel
	s.mu.Unlock()

	var lastPhase string
	start := time.Now()
	pipelineErr := services.ProcessUserMessageWithContext(ctx, conversationID, userMessage,
		func(phase string) {
			lastPhase = phase
			if onPhase != nil {
				onPhase(phase)
			}
		},
		onStream,
	)
	elapsed := time.Since(start).Milliseconds()

	// Status mapping
	var status string
	var errText *string
	switch {
	case pipelineErr == nil:
		status = "completed"
	case errors.Is(pipelineErr, context.Canceled):
		status = "cancelled"
	case errors.Is(pipelineErr, context.DeadlineExceeded):
		status = "timeout"
	default:
		status = "error"
		s := pipelineErr.Error()
		errText = &s
	}

	// Diff messages added
	afterMsgs, _ := services.GetConversationMessages(conversationID)
	var added []*models.ConversationMessage
	for _, m := range afterMsgs {
		if !beforeIDs[m.ID] {
			added = append(added, m)
		}
	}

	// Find the query row created during this message
	var latest *models.Query
	if afterQueries, err := services.GetQueriesByConversation(conversationID); err == nil {
		for _, q := range afterQueries {
			if q.ID > maxBeforeQuery {
				latest = q
			}
		}
	}

	return &messageResponse{
		ConversationID: conversationID,
		Status:         status,
		Error:          errText,
		DurationMS:     elapsed,
		Phase:          lastPhase,
		MessagesAdded:  added,
		Query:          latest,
	}, nil
}

// --- conversation patch ---

type conversationPatch struct {
	Title              *string `json:"title"`
	Status             *string `json:"status"`
	LLMProviderID      *uint   `json:"llm_provider_id"`
	DataSourceID       *uint   `json:"data_source_id"`
	MaxMessages        *int    `json:"max_messages"`
	MaxContextMessages *int    `json:"max_context_messages"`
	Pinned             *bool   `json:"pinned"`
	TechDetails        *bool   `json:"tech_details"`
	ContextDetails     *bool   `json:"context_details"`
	Summarize          *bool   `json:"summarize"`
	VizEnabled         *bool   `json:"viz_enabled"`
	StreamingEnabled   *bool   `json:"streaming_enabled"`
}

// applyPatch calls the appropriate UpdateConversation* service functions
// for each non-nil field in the patch body.
func applyPatch(id uint, p *conversationPatch) (*models.Conversation, error) {
	// Bulk update: title, status, llm_provider_id, data_source_id
	if p.Title != nil || p.Status != nil || p.LLMProviderID != nil || p.DataSourceID != nil {
		if _, err := services.UpdateConversation(id, p.Title, p.Status, p.LLMProviderID, p.DataSourceID); err != nil {
			return nil, err
		}
	}
	if p.MaxMessages != nil {
		if err := services.UpdateConversationMaxMessages(id, *p.MaxMessages); err != nil {
			return nil, err
		}
	}
	if p.MaxContextMessages != nil {
		if err := services.UpdateConversationMaxContextMessages(id, *p.MaxContextMessages); err != nil {
			return nil, err
		}
	}
	if p.Pinned != nil {
		if err := services.UpdateConversationPinned(id, *p.Pinned); err != nil {
			return nil, err
		}
	}
	if p.TechDetails != nil {
		if err := services.UpdateConversationTechDetails(id, *p.TechDetails); err != nil {
			return nil, err
		}
	}
	if p.ContextDetails != nil {
		if err := services.UpdateConversationContextDetails(id, *p.ContextDetails); err != nil {
			return nil, err
		}
	}
	if p.Summarize != nil {
		if err := services.UpdateConversationSummarize(id, *p.Summarize); err != nil {
			return nil, err
		}
	}
	if p.VizEnabled != nil {
		if err := services.UpdateConversationVizEnabled(id, *p.VizEnabled); err != nil {
			return nil, err
		}
	}
	if p.StreamingEnabled != nil {
		if err := services.UpdateConversationStreamingEnabled(id, *p.StreamingEnabled); err != nil {
			return nil, err
		}
	}
	return services.GetConversationByID(id)
}
