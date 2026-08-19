package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"YourQL/pkg/services"
)

// --- GET /api/health ---

func (s *headlessServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"version":   appVersion,
		"db_path":   s.currentDBPath(),
		"headless":  true,
		"uptime_ms": int(s.uptimeMs()),
	})
}

// --- POST /api/conversations ---

type settingsUpdateRequest struct {
	Settings map[string]string `json:"settings"`
}

type createConversationRequest struct {
	Title         *string `json:"title"`
	LLMProviderID *uint   `json:"llm_provider_id"`
	DataSourceID  *uint   `json:"data_source_id"`
}

func (s *headlessServer) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var req createConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	conv, err := services.CreateConversationWithDefaults(title, req.LLMProviderID, req.DataSourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

// --- GET /api/conversations/{id} ---

func (s *headlessServer) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	conv, err := services.GetConversationByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation_not_found", "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

// --- PATCH /api/conversations/{id} ---

func (s *headlessServer) handlePatchConversation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var patch conversationPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	conv, err := applyPatch(id, &patch)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "conversation_not_found", "conversation not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

// --- POST /api/conversations/{id}/messages ---

func (s *headlessServer) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var req messageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "message is required")
		return
	}

	// Recover from pipeline panics
	defer func() {
		if rcv := recover(); rcv != nil {
			log.Printf("[Headless] panic in message handler (conversation %d): %v", id, rcv)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		}
	}()

	var events []services.StreamEvent
	events = make([]services.StreamEvent, 0)
	resp, err := s.processMessage(id, req.Message,
		nil, // onPhase tracked internally
		func(ev services.StreamEvent) {
			events = append(events, ev)
		},
	)
	if err != nil {
		if strings.Contains(err.Error(), "processing_already_active") {
			writeError(w, http.StatusConflict, "processing_already_active", "a message is already processing for this conversation")
		} else {
			writeError(w, http.StatusNotFound, "conversation_not_found", "conversation not found")
		}
		return
	}
	resp.StreamEvents = events
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /api/conversations/{id}/messages/stream (SSE) ---

func (s *headlessServer) handleSendMessageSSE(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var req messageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "message is required")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Use a mutex to serialize writes to the ResponseWriter (onStream is
	// called from the LLM reader goroutine, not the handler goroutine).
	var writeMu sync.Mutex
	writeEvent := func(event, data string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	resp, err := s.processMessage(id, req.Message,
		func(phase string) {
			b, _ := json.Marshal(map[string]string{"phase": phase})
			writeEvent("phase", string(b))
		},
		func(ev services.StreamEvent) {
			b, _ := json.Marshal(ev)
			writeEvent("stream", string(b))
		},
	)
	if err != nil {
		b, _ := json.Marshal(apiError{Error: err.Error(), Code: "internal_error"})
		writeEvent("error", string(b))
		return
	}
	b, _ := json.Marshal(resp)
	writeEvent("result", string(b))
}

// --- POST /api/conversations/{id}/cancel ---

func (s *headlessServer) handleCancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.mu.Lock()
	cancel := s.cancels[id]
	s.mu.Unlock()
	if cancel == nil {
		writeError(w, http.StatusConflict, "no_active_processing", "no active processing for this conversation")
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

// --- GET /api/conversations/{id}/messages ---

func (s *headlessServer) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	msgs, err := services.GetConversationMessages(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation_not_found", "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// --- GET /api/conversations/{id}/queries ---

func (s *headlessServer) handleGetQueries(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	queries, err := services.GetQueriesByConversation(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation_not_found", "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queries": queries})
}

// --- Settings (app_settings) ---

// timeoutBounds pairs each timeout setting key with its allowed range. These
// match the GUI's input bounds in SettingsView.svelte so headless and GUI
// expose the same contract.
var timeoutBounds = map[string][2]int{
	"pipeline_timeout_seconds":     {30, 3600},
	"summarization_timeout_seconds": {10, 600},
}

// settingsResponse is the wire shape for GET/PUT /api/settings.
// "settings" is the raw app_settings table (verbatim key/value pairs).
// "timeouts" is the resolved/effective timeout values after defaults are
// applied, so a test harness can confirm what the pipeline will actually use.
type settingsResponse struct {
	Settings map[string]string `json:"settings"`
	Timeouts timeoutSettings   `json:"timeouts"`
}

type timeoutSettings struct {
	PipelineTimeout      int `json:"pipeline_timeout_seconds"`
	SummarizationTimeout int `json:"summarization_timeout_seconds"`
}

func effectiveSettingsResponse() settingsResponse {
	settings, _ := services.GetAllAppSettings()
	if settings == nil {
		settings = map[string]string{}
	}
	return settingsResponse{
		Settings: settings,
		Timeouts: timeoutSettings{
			PipelineTimeout:      services.GetTimeoutSetting("pipeline_timeout_seconds", 180),
			SummarizationTimeout: services.GetTimeoutSetting("summarization_timeout_seconds", 300),
		},
	}
}

// handleGetSettings returns the full app_settings table plus resolved timeout
// values. Read-only; never touches a data source.
func (s *headlessServer) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, effectiveSettingsResponse())
}

// handlePutSettings upserts app_settings key/value pairs. The two timeout keys
// are validated against their GUI bounds; empty values delete the key (which
// makes the pipeline fall back to its default), matching services.SetAppSetting.
// Other keys are stored verbatim.
func (s *headlessServer) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Settings == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "settings object is required")
		return
	}
	if len(req.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "settings object is empty")
		return
	}

	// Validate everything before persisting anything, so a bad value can't
	// leave a partial write behind.
	for key, value := range req.Settings {
		if err := validateSettingValue(key, value); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_setting", err.Error())
			return
		}
	}

	for key, value := range req.Settings {
		if err := services.SetAppSetting(key, value); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, effectiveSettingsResponse())
}

func validateSettingValue(key, value string) error {
	bounds, ok := timeoutBounds[key]
	if !ok {
		return nil // unknown key: stored verbatim (empty deletes it)
	}
	if value == "" {
		return nil // empty deletes the key → pipeline uses its default
	}
	secs, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s must be an integer number of seconds", key)
	}
	if secs < bounds[0] || secs > bounds[1] {
		return fmt.Errorf("%s must be between %d and %d seconds", key, bounds[0], bounds[1])
	}
	return nil
}

// --- helpers ---

func (s *headlessServer) currentDBPath() string {
	return s.dbPath
}

func (s *headlessServer) uptimeMs() int64 {
	return int64(time.Since(s.start).Milliseconds())
}