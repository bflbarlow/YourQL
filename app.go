package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"YourQL/pkg/models"
	"YourQL/pkg/services"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx               context.Context
	activeCancels     map[uint]context.CancelFunc
	activeCancelsMu   sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		activeCancels: make(map[uint]context.CancelFunc),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := models.ConnectDatabase(); err != nil {
		slog.Error("database error", "error", err)
	}
}

// registerCancel stores a cancel function for a conversation.
func (a *App) registerCancel(conversationID uint, cancel context.CancelFunc) {
	a.activeCancelsMu.Lock()
	if _, exists := a.activeCancels[conversationID]; exists {
		slog.Warn("overwriting active cancel for conversation", "id", conversationID)
	}
	a.activeCancels[conversationID] = cancel
	a.activeCancelsMu.Unlock()
}

// unregisterCancel removes a cancel function after processing completes.
func (a *App) unregisterCancel(conversationID uint) {
	a.activeCancelsMu.Lock()
	delete(a.activeCancels, conversationID)
	a.activeCancelsMu.Unlock()
}

// CancelProcessing cancels an in-progress message for the given conversation.
func (a *App) CancelProcessing(conversationID uint) error {
	a.activeCancelsMu.Lock()
	cancel, ok := a.activeCancels[conversationID]
	a.activeCancelsMu.Unlock()
	if !ok {
		return fmt.Errorf("no active processing for conversation %d", conversationID)
	}
	slog.Info("user cancelled processing", "conversation_id", conversationID)
	cancel()
	return nil
}

// shutdown is called when the app is about to quit.
func (a *App) shutdown(ctx context.Context) {
	slog.Info("shutting down")
	if models.DB != nil {
		models.DB.Close()
	}
}

// ==================== Discussions ====================

func (a *App) ListDiscussions() ([]string, error) {
	discussions, err := services.ListConversationsByUser()
	if err != nil {
		return nil, err
	}

	titles := make([]string, 0, len(discussions))
	for _, d := range discussions {
		if d.Title != nil {
			titles = append(titles, *d.Title)
		} else {
			titles = append(titles, "Untitled")
		}
	}
	return titles, nil
}

func (a *App) ListConversations() ([]*models.Conversation, error) {
	return services.ListConversationsByUser()
}

func (a *App) CreateConversation(title string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error) {
	return services.CreateConversationWithDefaults(title, llmProviderID, dbConnectionID)
}

func (a *App) GetConversationMessages(conversationID uint) ([]*models.ConversationMessage, error) {
	return services.GetConversationMessages(conversationID)
}

func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
	// Create a cancellable context so the user can interrupt processing.
	ctx, cancel := context.WithCancel(context.Background())
	a.registerCancel(conversationID, cancel)
	defer a.unregisterCancel(conversationID)

	onStream := func(ev services.StreamEvent) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "llm:stream", map[string]interface{}{
				"conversation_id": conversationID,
				"event":           ev,
			})
		}
	}
	err := services.ProcessUserMessageWithContext(ctx, conversationID, userMessage, func(phase string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "processingPhase", phase)
		}
	}, onStream)
	if a.ctx != nil {
		if errors.Is(err, context.Canceled) {
			runtime.EventsEmit(a.ctx, "processingCancelled", map[string]interface{}{
				"conversation_id": conversationID,
			})
			// Don't propagate cancellation as an error to the frontend.
			// The processingCancelled event handles all cleanup.
			return nil
		} else {
			runtime.EventsEmit(a.ctx, "processingComplete")
		}
	}
	return err
}

func (a *App) DeleteConversation(id uint) error {
	return services.SoftDeleteConversation(id)
}

func (a *App) UpdateConversationTechDetails(id uint, showTechDetails bool) error {
	return services.UpdateConversationTechDetails(id, showTechDetails)
}

func (a *App) UpdateConversationContextDetails(id uint, showContextDetails bool) error {
	return services.UpdateConversationContextDetails(id, showContextDetails)
}

func (a *App) UpdateConversationSettings(id uint, llmProviderID *uint, dbConnectionID *uint) error {
	_, err := services.UpdateConversation(id, nil, nil, llmProviderID, dbConnectionID)
	return err
}

func (a *App) UpdateConversationTitle(id uint, title string) (*models.Conversation, error) {
	return services.UpdateConversationTitle(id, title)
}

func (a *App) UpdateConversationMaxMessages(id uint, maxMessages int) error {
	return services.UpdateConversationMaxMessages(id, maxMessages)
}

func (a *App) UpdateConversationMaxContextMessages(id uint, maxContextMessages int) error {
	return services.UpdateConversationMaxContextMessages(id, maxContextMessages)
}

func (a *App) UpdateConversationPinned(id uint, pinned bool) error {
	return services.UpdateConversationPinned(id, pinned)
}

func (a *App) UpdateConversationSummarize(id uint, summarize bool) error {
	return services.UpdateConversationSummarize(id, summarize)
}

func (a *App) UpdateConversationVizEnabled(id uint, vizEnabled bool) error {
	return services.UpdateConversationVizEnabled(id, vizEnabled)
}

func (a *App) UpdateConversationStreamingEnabled(id uint, enabled bool) error {
	return services.UpdateConversationStreamingEnabled(id, enabled)
}

func (a *App) DuplicateConversation(id uint) (*models.Conversation, error) {
	return services.DuplicateConversation(id)
}

func (a *App) ClearConversationMessages(id uint) error {
	return services.DeleteConversationMessages(id)
}

func (a *App) ArchiveConversation(id uint) error {
	return services.ArchiveConversation(id)
}

func (a *App) RestoreConversation(id uint) error {
	return services.RestoreConversation(id)
}

// ==================== Skills ====================

func (a *App) ListSkills() ([]models.Skill, error) {
	return services.ListSkills()
}

func (a *App) CreateSkill(name, markdownContent string) (*models.Skill, error) {
	return services.CreateSkill(name, markdownContent)
}

func (a *App) UpdateSkill(id uint, name, markdownContent string) (*models.Skill, error) {
	return services.UpdateSkill(id, name, markdownContent)
}

func (a *App) DeleteSkill(id uint) error {
	return services.DeleteSkill(id)
}

func (a *App) SetSkillActive(id uint, active bool) error {
	return services.SetSkillActive(id, active)
}

func (a *App) GetConversationSkillIDs(conversationID uint) ([]uint, error) {
	return services.GetConversationSkillIDs(conversationID)
}

func (a *App) SetConversationSkill(conversationID uint, skillID uint, enabled bool) error {
	return services.SetConversationSkill(conversationID, skillID, enabled)
}

// ==================== LLM Provider Settings ====================

// LLMProviderSetting represents an LLM provider configuration for the frontend
type LLMProviderSetting struct {
	ID             uint   `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Model          string `json:"model,omitempty"`
	BaseURL        string `json:"baseURL,omitempty"`
	IsDefault      bool   `json:"is_default"`
	IsActive       bool   `json:"is_active"`
	MaxTokens      int    `json:"maxTokens"`
	ModelMaxTokens int    `json:"modelMaxTokens"`
	ContextWindow  int    `json:"contextWindow"`
}

func (a *App) ListLLMProviders() ([]LLMProviderSetting, error) {
	providers, err := services.ListLLMProvidersByWorkspace()
	if err != nil {
		return nil, err
	}

	settings := make([]LLMProviderSetting, 0, len(providers))
	for _, p := range providers {
		model := ""
		if p.Model != nil {
			model = *p.Model
		}
		baseURL := ""
		if p.BaseURL != nil {
			baseURL = *p.BaseURL
		}
		maxTokens := 0
		if p.MaxTokens != nil {
			maxTokens = *p.MaxTokens
		}
		modelMaxTokens := 0
		if p.ModelMaxTokens != nil {
			modelMaxTokens = *p.ModelMaxTokens
		}
		contextWindow := 0
		if p.ContextWindow != nil {
			contextWindow = *p.ContextWindow
		}
		settings = append(settings, LLMProviderSetting{
			ID:             p.ID,
			Name:           p.Name,
			Provider:       p.Provider,
			Model:          model,
			BaseURL:        baseURL,
			IsDefault:      p.IsDefault,
			IsActive:       p.IsActive,
			MaxTokens:      maxTokens,
			ModelMaxTokens: modelMaxTokens,
			ContextWindow:  contextWindow,
		})
	}
	return settings, nil
}

func (a *App) CreateLLMProvider(name, provider, model, baseURL, apiKey string, maxTokens int) error {
	var mt *int
	if maxTokens > 0 {
		mt = &maxTokens
	}
	_, err := services.CreateLLMProvider(name, provider, model, baseURL, apiKey, true, "", mt)
	return err
}

func (a *App) UpdateLLMProvider(id uint, name, model, baseURL, apiKey string, maxTokens int) error {
	var mt *int
	if maxTokens > 0 {
		mt = &maxTokens
	}
	var ak *string
	if apiKey != "" {
		ak = &apiKey
	}
	_, err := services.UpdateLLMProvider(id, &name, &model, &baseURL, ak, nil, mt)
	return err
}

func (a *App) DeleteLLMProvider(id uint) error {
	return services.DeleteLLMProvider(id)
}

// DetectModelMaxTokens attempts to detect the model's maximum output tokens.
// Returns 0 if detection failed or is not supported.
func (a *App) DetectModelMaxTokens(id uint) (int, error) {
	provider, err := services.GetLLMProviderByID(id)
	if err != nil {
		return 0, err
	}
	detected, err := services.DetectModelMaxTokens(provider)
	if err != nil || detected == nil {
		return 0, nil
	}
	// Persist the detected value
	_ = services.UpdateModelMaxTokens(id, detected)
	return *detected, nil
}

func (a *App) SetDefaultLLMProvider(id uint) error {
	return services.SetDefaultLLMProvider(id)
}

func (a *App) TestLLMProviderConnection(id uint) (string, error) {
	provider, err := services.GetLLMProviderByID(id)
	if err != nil {
		return "", err
	}
	result, err := services.TestLLMProvider(provider)
	if err != nil {
		return result, err
	}
	// Also attempt model max token detection as a side effect
	if detected, _ := services.DetectModelMaxTokens(provider); detected != nil {
		_ = services.UpdateModelMaxTokens(id, detected)
		result += fmt.Sprintf("\nModel max output: %d tokens.", *detected)
	}
	return result, err
}

// ==================== Database Connection Settings ====================

// DataSourceSetting represents a database connection configuration for the frontend
type DataSourceSetting struct {
	ID                   uint   `json:"id"`
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	Host                 string `json:"host,omitempty"`
	Port                 int    `json:"port,omitempty"`
	Database             string `json:"database,omitempty"`
	Username             string `json:"username,omitempty"`
	SSLMode              string `json:"sslMode,omitempty"`
	IsDefault            bool   `json:"is_default"`
	IsActive             bool   `json:"is_active"`
	ExplorationAllowed   bool   `json:"exploration_allowed"`
	MaxExplorationRounds int    `json:"max_exploration_rounds"`
	ExplorationSafety    string `json:"exploration_safety"`
	Config               string `json:"config,omitempty"`
	Extra                string `json:"extra,omitempty"`
	FilePath             string `json:"file_path,omitempty"`
	FileType             string `json:"file_type,omitempty"`
}

// filePathStr returns the file path string for a data source.
func filePathStr(c *models.DataSource) string {
	if c.FilePath != nil {
		return *c.FilePath
	}
	return ""
}

// fileTypeStr returns the file type string for a data source.
func fileTypeStr(c *models.DataSource) string {
	if c.FileType != nil {
		return *c.FileType
	}
	return ""
}

func (a *App) ListDataSources() ([]DataSourceSetting, error) {
	connections, err := services.ListDataSourcesByWorkspace()
	if err != nil {
		return nil, err
	}

	settings := make([]DataSourceSetting, 0, len(connections))
	for _, c := range connections {
		host := ""
		if c.Host != nil {
			host = *c.Host
		}
		port := 0
		if c.Port != nil {
			port = *c.Port
		}
		database := ""
		if c.Database != nil {
			database = *c.Database
		}
		username := ""
		if c.Username != nil {
			username = *c.Username
		}
		sslMode := ""
		if c.SSLMode != nil {
			sslMode = *c.SSLMode
		}

		explorationAllowed := true
		maxExplorationRounds := 2
		explorationSafety := "strict"
		var configStr string
		if c.Config != nil && *c.Config != "" {
			configStr = *c.Config
			var config models.DataSourceConfig
			if err := json.Unmarshal([]byte(*c.Config), &config); err == nil {
				explorationAllowed = config.ExplorationAllowed
				if config.MaxExplorationRounds > 0 {
					maxExplorationRounds = config.MaxExplorationRounds
				}
				if config.ExplorationSafety != "" {
					explorationSafety = config.ExplorationSafety
				}
			}
		}

		var extraStr string
		if c.Extra != nil {
			extraStr = *c.Extra
		}

		settings = append(settings, DataSourceSetting{
			ID:                   c.ID,
			Name:                 c.Name,
			Type:                 c.Type,
			Host:                 host,
			Port:                 port,
			Database:             database,
			Username:             username,
			SSLMode:              sslMode,
			IsDefault:            c.IsDefault,
			IsActive:             c.IsActive,
			ExplorationAllowed:   explorationAllowed,
			MaxExplorationRounds: maxExplorationRounds,
			ExplorationSafety:    explorationSafety,
			Config:               configStr,
			Extra:                extraStr,
			FilePath:             filePathStr(c),
			FileType:             fileTypeStr(c),
		})
	}
	return settings, nil
}

func (a *App) CreateDataSource(name, dbType, host string, port int, database, username, password, sslMode, config, extra, filePath, fileType string) error {
	_, err := services.CreateDataSource(name, dbType, host, port, database, username, password, sslMode, config, extra, filePath, fileType)
	return err
}

func (a *App) UpdateDataSource(id uint, name, host, database, username, password, sslMode, filePath, fileType string, port int, config, extra string) error {
	_, err := services.UpdateDataSource(id, &name, &host, &port, &database, &username, &password, &sslMode, &config, &extra, &filePath, &fileType)
	return err
}

// GetSupportedDBTypes returns metadata about all registered database types.
func (a *App) GetSupportedDBTypes() []services.DBTypeInfo {
	return services.GetSupportedDBTypes()
}

func (a *App) DeleteDataSource(id uint) error {
	return services.DeleteDataSource(id)
}

func (a *App) SetDefaultDataSource(id uint) error {
	return services.SetDefaultDataSource(id)
}

func (a *App) TestDataSource(id uint) (string, error) {
	conn, err := services.GetDataSourceByID(id)
	if err != nil {
		return "", err
	}

	if err := services.TestDataSource(conn); err != nil {
		return fmt.Sprintf("Connection failed: %v", err), nil
	}

	return "Connection successful.", nil
}

// TestNewDataSource tests a new connection using form fields without saving first.
func (a *App) TestNewDataSource(name, dbType, host string, port int, database, username, password, sslMode, extra, filePath, fileType string) (string, error) {
	conn := &models.DataSource{
		Name:     name,
		Type:     dbType,
		Host:     &host,
		Port:     &port,
		Database: &database,
		Username: &username,
		Password: &password,
		SSLMode:  &sslMode,
		FilePath: &filePath,
		FileType: &fileType,
		Extra:    &extra,
	}
	if err := services.TestDataSource(conn); err != nil {
		return fmt.Sprintf("Connection failed: %v", err), nil
	}
	return "Connection successful.", nil
}

// SchemaPreview represents a preview of a database schema for the Settings UI.
type SchemaPreview struct {
	ConnectionName string               `json:"connection_name"`
	TotalTables    int                  `json:"total_tables"`
	Tables         []SchemaTablePreview `json:"tables"`
}

type SchemaTablePreview struct {
	Name        string                  `json:"name"`
	RowCount    int64                   `json:"row_count"`
	Columns     []SchemaColumnPreview   `json:"columns"`
	Indexes     int                     `json:"indexes"`
	ForeignKeys int                     `json:"foreign_keys"`
}

type SchemaColumnPreview struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	IsNullable   bool   `json:"is_nullable"`
}

func (a *App) GetSchemaPreview(id uint) (*SchemaPreview, error) {
	conn, err := services.GetDataSourceByID(id)
	if err != nil {
		return nil, err
	}

	schema, err := services.GetDataSchema(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	preview := &SchemaPreview{
		ConnectionName: conn.Name,
		TotalTables:    len(schema.Tables),
		Tables:         make([]SchemaTablePreview, 0),
	}

	for _, t := range schema.Tables {
		cols := make([]SchemaColumnPreview, 0)
		for _, c := range t.Columns {
			cols = append(cols, SchemaColumnPreview{
				Name:         c.Name,
				DataType:     c.DataType,
				IsPrimaryKey: c.IsPrimaryKey,
				IsNullable:   c.IsNullable,
			})
		}
		preview.Tables = append(preview.Tables, SchemaTablePreview{
			Name:        t.Name,
			RowCount:    t.RowCount,
			Columns:     cols,
			Indexes:      len(t.Indexes),
			ForeignKeys: len(t.ForeignKeys),
		})
	}

	return preview, nil
}

// QueryResult represents a row from a query execution (§2.14 – kept for ExecuteQuery)
type QueryResult struct {
	Columns   []string         `json:"columns"`
	Rows      [][]interface{}  `json:"rows"`
	TotalRows int              `json:"total_rows"`
}

// ExecuteQuery runs a SQL query against a configured database connection
func (a *App) ExecuteQuery(connID uint, query string) (*QueryResult, error) {
	conn, err := services.GetDataSourceByID(connID)
	if err != nil {
		return nil, err
	}

	dsn, err := services.BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open(conn.Type, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var result QueryResult
	result.Columns = columns
	result.Rows = make([][]interface{}, 0)

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]interface{}, len(columns))
		for i, v := range values {
			switch val := v.(type) {
			case []byte:
				row[i] = string(val)
			default:
				row[i] = val
			}
		}
		result.Rows = append(result.Rows, row)
	}

	result.TotalRows = len(result.Rows)
	return &result, nil
}

// ==================== Google Sheets OAuth ====================

// StartGoogleSheetsAuth begins the OAuth2 loopback flow for a saved data source.
// Opens the default browser to Google's consent screen; Google redirects to
// localhost and we catch the token. Returns the auth URL for the browser.
func (a *App) StartGoogleSheetsAuth(dataSourceID uint) (map[string]interface{}, error) {
	authURL, resultCh, err := services.StartLoopbackServer(a.ctx)
	if err != nil {
		return nil, err
	}

	go func() {
		result := <-resultCh
		if result.Error != nil {
			runtime.EventsEmit(a.ctx, "googleAuthError", map[string]interface{}{
				"dataSourceID": dataSourceID,
				"error":        result.Error.Error(),
			})
			return
		}
		if err := services.StoreAuthConfig(dataSourceID, result.Token); err != nil {
			runtime.EventsEmit(a.ctx, "googleAuthError", map[string]interface{}{
				"dataSourceID": dataSourceID,
				"error":        fmt.Sprintf("failed to store token: %v", err),
			})
			return
		}
		runtime.EventsEmit(a.ctx, "googleAuthComplete", map[string]interface{}{
			"dataSourceID": dataSourceID,
		})
	}()

	return map[string]interface{}{
		"auth_url": authURL,
	}, nil
}

// CancelGoogleSheetsAuth cancels an in-flight auth flow.
// The loopback server self-terminates when the callback arrives or the app exits.
func (a *App) CancelGoogleSheetsAuth(dataSourceID uint) error {
	return nil
}

// RevokeGoogleSheetsAuth removes OAuth tokens for a data source and revokes
// them with Google (best-effort).
func (a *App) RevokeGoogleSheetsAuth(dataSourceID uint) error {
	tok, _ := services.LoadAuthConfig(dataSourceID)
	if tok != nil {
		_ = services.RevokeToken(tok) // best-effort; ignore errors
	}
	return services.ClearAuthConfig(dataSourceID)
}

// StartGoogleSheetsAuthTemp is like StartGoogleSheetsAuth but for unsaved
// connections. Uses a string sessionID for in-memory storage.
func (a *App) StartGoogleSheetsAuthTemp(sessionID string) (map[string]interface{}, error) {
	authURL, resultCh, err := services.StartLoopbackServer(a.ctx)
	if err != nil {
		return nil, err
	}

	go func() {
		result := <-resultCh
		if result.Error != nil {
			runtime.EventsEmit(a.ctx, "googleAuthError", map[string]interface{}{
				"sessionID": sessionID,
				"error":     result.Error.Error(),
			})
			return
		}
		services.StoreTempAuthConfig(sessionID, result.Token)
		runtime.EventsEmit(a.ctx, "googleAuthComplete", map[string]interface{}{
			"sessionID": sessionID,
		})
	}()

	return map[string]interface{}{
		"auth_url": authURL,
	}, nil
}

// MigrateGoogleAuthConfig moves a temp session's OAuth token into the real
// data source DB record. Call this after saving a new Google Sheets connection.
func (a *App) MigrateGoogleAuthConfig(sessionID string, dataSourceID uint) error {
	return services.MigrateTempAuthToDB(sessionID, dataSourceID)
}

// CancelGoogleSheetsAuthTemp cancels an in-flight temp-session auth flow.
func (a *App) CancelGoogleSheetsAuthTemp(sessionID string) error {
	return nil
}

// ==================== General Settings ====================

// ExportConversationPDF triggers the native print dialog for PDF export.
func (a *App) ExportConversationPDF() {
	runtime.WindowPrint(a.ctx)
}

// ExportConversationHTML generates a standalone HTML file for the conversation
// and opens a save dialog. Returns an error string if something goes wrong, or
// empty string on success / user cancel.
func (a *App) ExportConversationHTML(conversationID uint) string {
	conv, err := services.GetConversationByID(conversationID)
	if err != nil {
		return err.Error()
	}
	messages, err := services.GetConversationMessages(conversationID)
	if err != nil {
		return err.Error()
	}

	// Resolve provider and data source display names
	providerName, dataSourceName := resolveExportNames(conv)

	htmlContent := services.BuildConversationHTML(conv, messages, providerName, dataSourceName)

	// Suggest a filename based on the conversation title
	title := "Untitled"
	if conv.Title != nil && *conv.Title != "" {
		title = *conv.Title
	}
	sanitized := sanitizeFilename(title)

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: sanitized + ".html",
		Title:           "Export as HTML",
		Filters: []runtime.FileFilter{
			{DisplayName: "HTML Files (*.html)", Pattern: "*.html"},
		},
	})
	if err != nil {
		return err.Error()
	}
	if path == "" {
		// User cancelled — not an error
		return ""
	}

	if err := writeFile(path, htmlContent); err != nil {
		return err.Error()
	}
	return ""
}

// ExportConversationMarkdown generates a Markdown file for the conversation
// and opens a save dialog. Returns an error string if something goes wrong, or
// empty string on success / user cancel.
func (a *App) ExportConversationMarkdown(conversationID uint) string {
	conv, err := services.GetConversationByID(conversationID)
	if err != nil {
		return err.Error()
	}
	messages, err := services.GetConversationMessages(conversationID)
	if err != nil {
		return err.Error()
	}

	providerName, dataSourceName := resolveExportNames(conv)

	mdContent := services.BuildConversationMarkdown(conv, messages, providerName, dataSourceName)

	title := "Untitled"
	if conv.Title != nil && *conv.Title != "" {
		title = *conv.Title
	}
	sanitized := sanitizeFilename(title)

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: sanitized + ".md",
		Title:           "Export as Markdown",
		Filters: []runtime.FileFilter{
			{DisplayName: "Markdown Files (*.md)", Pattern: "*.md"},
		},
	})
	if err != nil {
		return err.Error()
	}
	if path == "" {
		return ""
	}

	if err := writeFile(path, mdContent); err != nil {
		return err.Error()
	}
	return ""
}

// resolveExportNames looks up the display names for the conversation's LLM
// provider and data source. Both fields are optional; returns empty strings
// for nil IDs.
func resolveExportNames(conv *models.Conversation) (string, string) {
	var providerName, dataSourceName string
	if conv.LLMProviderID != nil {
		if p, err := services.GetLLMProviderByID(*conv.LLMProviderID); err == nil && p != nil {
			providerName = p.Name
		}
	}
	if conv.DataSourceID != nil {
		if ds, err := services.GetDataSourceByID(*conv.DataSourceID); err == nil && ds != nil {
			dataSourceName = ds.Name
		}
	}
	return providerName, dataSourceName
}

// sanitizeFilename replaces characters unsafe for filenames with a safe
// alternative, keeping the result readable.
func sanitizeFilename(name string) string {
	re := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "", "<", "-", ">", "-", "|", "-",
	)
	return strings.TrimSpace(re.Replace(name))
}

// writeFile is a small helper to write string content to a file path.
func writeFile(path, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// GetAppSetting returns the value for a single app setting key.
func (a *App) GetAppSetting(key string) (string, error) {
	return services.GetAppSetting(key)
}

// SetAppSetting upserts a single app setting key.
func (a *App) SetAppSetting(key, value string) error {
	return services.SetAppSetting(key, value)
}

// ==================== Agent Loop Config ====================

// AgentLoopConfigField is metadata for one configurable field.
type AgentLoopConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Section     string `json:"section"`
}

// AgentLoopConfigJSON holds the merged config plus field metadata for the frontend.
type AgentLoopConfigJSON struct {
	Config *models.AgentLoopConfig `json:"config"`
	Fields []AgentLoopConfigField  `json:"fields"`
}

// GetAgentLoopConfig returns the merged config (defaults + user overrides)
// plus field metadata so the frontend can render labels and tooltips.
func (a *App) GetAgentLoopConfig() (*AgentLoopConfigJSON, error) {
	cfg, err := services.GetAgentLoopConfig()
	if err != nil {
		return nil, err
	}
	svcFields := services.AgentLoopConfigFields()
	fields := make([]AgentLoopConfigField, len(svcFields))
	for i, f := range svcFields {
		fields[i] = AgentLoopConfigField{
			Key:         f.Key,
			Label:       f.Label,
			Description: f.Description,
			Section:     f.Section,
		}
	}
	return &AgentLoopConfigJSON{
		Config: cfg,
		Fields: fields,
	}, nil
}

// SetAgentLoopConfigKey upserts a single agent loop config key.
func (a *App) SetAgentLoopConfigKey(key, value string) error {
	return services.SetAgentLoopConfigKey(key, value)
}

// ResetAgentLoopConfigKey resets a single field to its hardcoded default.
func (a *App) ResetAgentLoopConfigKey(key string) error {
	return services.SetAgentLoopConfigKey(key, "") // empty value triggers default revert
}

// ResetAllAgentLoopConfig deletes all user overrides.
func (a *App) ResetAllAgentLoopConfig() error {
	return services.ResetAgentLoopConfig()
}

type GeneralSettings struct {
	AppName            string `json:"app_name"`
	AppVersion         string `json:"app_version"`
	DefaultLLMProvider string `json:"default_llm_provider"`
	Theme              string `json:"theme"`
	Accent             string `json:"accent"`
	Scale              string `json:"scale"`
	Language           string `json:"language"`
}

// GetAppVersion returns the running app version, injected at build time via ldflags.
func (a *App) GetAppVersion() string {
	return appVersion
}

// GetGeneralSettings returns persisted settings from the local database.
func (a *App) GetGeneralSettings() GeneralSettings {
	settings := GeneralSettings{
		AppName:            "YourQL",
		AppVersion:         appVersion,
		DefaultLLMProvider: "openai",
		Theme:              "system",
		Accent:             "#0288d1",
		Scale:              "medium",
		Language:           "en",
	}
	if v, err := services.GetAppSetting("theme"); err == nil && v != "" {
		settings.Theme = v
	}
	if v, err := services.GetAppSetting("accent"); err == nil && v != "" {
		settings.Accent = v
	}
	if v, err := services.GetAppSetting("scale"); err == nil && v != "" {
		settings.Scale = v
	}
	return settings
}

// UpdateGeneralSettings persists the provided settings to the local database.
func (a *App) UpdateGeneralSettings(settings GeneralSettings) error {
	if settings.Theme != "" {
		if err := services.SetAppSetting("theme", settings.Theme); err != nil {
			return err
		}
	}
	if settings.Accent != "" {
		if err := services.SetAppSetting("accent", settings.Accent); err != nil {
			return err
		}
	}
	if settings.Scale != "" {
		if err := services.SetAppSetting("scale", settings.Scale); err != nil {
			return err
		}
	}
	return nil
}

// GetDiscussionDefaults returns the user's configured defaults for new discussions.
func (a *App) GetDiscussionDefaults() (*models.DiscussionDefaults, error) {
	return services.GetDiscussionDefaults()
}

// UpdateDiscussionDefaults persists a batch of default discussion settings.
// Only non-nil fields are updated; pass nil to leave a field unchanged.
func (a *App) UpdateDiscussionDefaults(defaults models.DiscussionDefaults) error {
	if defaults.LLMProviderID != nil {
		if err := services.SetDiscussionDefault("llm_provider_id",
			fmt.Sprintf("%d", *defaults.LLMProviderID)); err != nil {
			return err
		}
	}
	if defaults.DataSourceID != nil {
		if err := services.SetDiscussionDefault("data_source_id",
			fmt.Sprintf("%d", *defaults.DataSourceID)); err != nil {
			return err
		}
	}
	if defaults.MaxContextMessages != nil {
		if err := services.SetDiscussionDefault("max_context_messages",
			fmt.Sprintf("%d", *defaults.MaxContextMessages)); err != nil {
			return err
		}
	}
	if defaults.MaxMessages != nil {
		if err := services.SetDiscussionDefault("max_messages",
			fmt.Sprintf("%d", *defaults.MaxMessages)); err != nil {
			return err
		}
	}
	if defaults.Summarize != nil {
		if err := services.SetDiscussionDefault("summarize",
			fmt.Sprintf("%t", *defaults.Summarize)); err != nil {
			return err
		}
	}
	if defaults.VizEnabled != nil {
		if err := services.SetDiscussionDefault("viz_enabled",
			fmt.Sprintf("%t", *defaults.VizEnabled)); err != nil {
			return err
		}
	}
	if defaults.TechDetails != nil {
		if err := services.SetDiscussionDefault("tech_details",
			fmt.Sprintf("%t", *defaults.TechDetails)); err != nil {
			return err
		}
	}
	if defaults.ContextDetails != nil {
		if err := services.SetDiscussionDefault("context_details",
			fmt.Sprintf("%t", *defaults.ContextDetails)); err != nil {
			return err
		}
	}
	if defaults.StreamingEnabled != nil {
		if err := services.SetDiscussionDefault("streaming_enabled",
			fmt.Sprintf("%t", *defaults.StreamingEnabled)); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Auto-updater
// ---------------------------------------------------------------------------

// CheckForUpdate queries the GitHub Releases API for a newer version.
func (a *App) CheckForUpdate() (*services.UpdateInfo, error) {
	return services.CheckForUpdate(appVersion)
}

// DownloadUpdate downloads and verifies the new release asset.
func (a *App) DownloadUpdate(downloadURL, expectedSHA256 string) error {
	return services.DownloadUpdate(downloadURL, expectedSHA256)
}

// PerformUpgradeRestart runs the OS-specific upgrade script and exits the app.
func (a *App) PerformUpgradeRestart() error {
	return services.PerformUpgradeRestart()
}
