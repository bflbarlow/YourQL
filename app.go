package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"YourQL/pkg/models"
	"YourQL/pkg/services"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := models.ConnectDatabase(); err != nil {
		println("Database error:", err.Error())
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
	return services.CreateConversation(title, llmProviderID, dbConnectionID)
}

func (a *App) GetConversationMessages(conversationID uint) ([]*models.ConversationMessage, error) {
	return services.GetConversationMessages(conversationID)
}

func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
	err := services.ProcessUserMessage(conversationID, userMessage, func(phase string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "processingPhase", phase)
		}
	})
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "processingComplete")
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
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"baseURL,omitempty"`
	IsDefault bool   `json:"is_default"`
	IsActive  bool   `json:"is_active"`
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
		settings = append(settings, LLMProviderSetting{
			ID:        p.ID,
			Name:      p.Name,
			Provider:  p.Provider,
			Model:     model,
			BaseURL:   baseURL,
			IsDefault: p.IsDefault,
			IsActive:  p.IsActive,
		})
	}
	return settings, nil
}

func (a *App) CreateLLMProvider(name, provider, model, baseURL, apiKey string) error {
	_, err := services.CreateLLMProvider(name, provider, model, baseURL, apiKey, true, "")
	return err
}

func (a *App) UpdateLLMProvider(id uint, name, model, baseURL, apiKey string) error {
	_, err := services.UpdateLLMProvider(id, &name, &model, &baseURL, &apiKey, nil)
	return err
}

func (a *App) DeleteLLMProvider(id uint) error {
	return services.DeleteLLMProvider(id)
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

type GeneralSettings struct {
	AppName            string `json:"app_name"`
	AppVersion         string `json:"app_version"`
	DefaultLLMProvider string `json:"default_llm_provider"`
	Theme              string `json:"theme"`
	Language           string `json:"language"`
}

// GetGeneralSettings returns hard-coded defaults (not persisted)
func (a *App) GetGeneralSettings() GeneralSettings {
	return GeneralSettings{
		AppName:            "YourQL",
		AppVersion:         "0.1.0",
		DefaultLLMProvider: "openai",
		Theme:              "light",
		Language:           "en",
	}
}

// UpdateGeneralSettings is a no-op — settings are not persisted (§4.6)
func (a *App) UpdateGeneralSettings(settings GeneralSettings) error {
	return nil
}
