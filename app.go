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

// DBConnectionSetting represents a database connection configuration for the frontend
type DBConnectionSetting struct {
	ID                   uint   `json:"id"`
	Name                 string `json:"name"`
	Type                 string `json:"type"` // mysql, sqlite
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
}

func (a *App) ListDBConnections() ([]DBConnectionSetting, error) {
	connections, err := services.ListDBConnectionsByWorkspace()
	if err != nil {
		return nil, err
	}

	settings := make([]DBConnectionSetting, 0, len(connections))
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
			var config models.DBConnectionConfig
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

		settings = append(settings, DBConnectionSetting{
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
		})
	}
	return settings, nil
}

func (a *App) CreateDBConnection(name, dbType, host string, port int, database, username, password, sslMode, config string) error {
	_, err := services.CreateDBConnection(name, dbType, host, port, database, username, password, sslMode, config)
	return err
}

func (a *App) UpdateDBConnection(id uint, name, host, database, username, password, sslMode string, port int, config string) error {
	_, err := services.UpdateDBConnection(id, &name, &host, &port, &database, &username, &password, &sslMode, &config)
	return err
}

func (a *App) DeleteDBConnection(id uint) error {
	return services.DeleteDBConnection(id)
}

func (a *App) SetDefaultDBConnection(id uint) error {
	return services.SetDefaultDBConnection(id)
}

func (a *App) TestDBConnection(id uint) (string, error) {
	conn, err := services.GetDBConnectionByID(id)
	if err != nil {
		return "", err
	}

	schema, err := services.GetDatabaseSchema(conn)
	if err != nil {
		return fmt.Sprintf("Connection failed: %v", err), nil
	}

	tableCount := len(schema.Tables)
	totalRows := int64(0)
	for _, t := range schema.Tables {
		totalRows += t.RowCount
	}

	return fmt.Sprintf("Connected. Found %d table(s) with ~%d rows.", tableCount, totalRows), nil
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
	conn, err := services.GetDBConnectionByID(id)
	if err != nil {
		return nil, err
	}

	schema, err := services.GetDatabaseSchema(conn)
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
	conn, err := services.GetDBConnectionByID(connID)
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
