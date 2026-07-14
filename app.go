package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"YourQL/pkg/models"
	"YourQL/pkg/services"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	models.ConnectDatabase()
}

// ==================== Discussions ====================

// ListDiscussions retrieves titles of discussions for a user in a workspace
func (a *App) ListDiscussions(workspaceID uint, userID uint) ([]string, error) {
	discussions, err := services.ListConversationsByUser(workspaceID, userID)
	if err != nil {
		return nil, err
	}

	var titles []string
	for _, d := range discussions {
		if d.Title != nil {
			titles = append(titles, *d.Title)
		} else {
			titles = append(titles, "Untitled")
		}
	}
	return titles, nil
}

// ListConversations retrieves all conversations for a user in a workspace
func (a *App) ListConversations(workspaceID uint, userID uint) ([]*models.Conversation, error) {
	return services.ListConversationsByUser(workspaceID, userID)
}

// CreateConversation creates a new discussion
func (a *App) CreateConversation(workspaceID, userID uint, title string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error) {
	return services.CreateConversation(workspaceID, userID, title, llmProviderID, dbConnectionID)
}

// GetConversationMessages retrieves all messages in a conversation
func (a *App) GetConversationMessages(conversationID uint) ([]*models.ConversationMessage, error) {
	return services.GetConversationMessages(conversationID)
}

// ProcessUserMessage processes a user message in a conversation
func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
	return services.ProcessUserMessage(conversationID, userMessage)
}

// DeleteConversation soft-deletes a conversation (sets deleted_at).
func (a *App) DeleteConversation(id uint) error {
	return services.SoftDeleteConversation(id)
}

// UpdateConversationTechDetails updates the tech_details toggle for a conversation.
func (a *App) UpdateConversationTechDetails(id uint, showTechDetails bool) error {
	return services.UpdateConversationTechDetails(id, showTechDetails)
}

// UpdateConversationSettings updates the LLM provider and DB connection for a conversation.
func (a *App) UpdateConversationSettings(id uint, llmProviderID *uint, dbConnectionID *uint) error {
	_, err := services.UpdateConversation(id, nil, nil, llmProviderID, dbConnectionID)
	return err
}

// ArchiveConversation archives a conversation.
func (a *App) ArchiveConversation(id uint) error {
	return services.ArchiveConversation(id)
}

// RestoreConversation restores an archived conversation.
func (a *App) RestoreConversation(id uint) error {
	return services.RestoreConversation(id)
}

// ==================== LLM Provider Settings ====================

// LLMProviderSetting represents an LLM provider configuration for the frontend
type LLMProviderSetting struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"` // openai, anthropic, ollama, local
	Model       string  `json:"model,omitempty"`
	BaseURL     string  `json:"base_url,omitempty"`
	IsDefault   bool    `json:"is_default"`
	IsActive    bool    `json:"is_active"`
}

// ListLLMProviders retrieves all configured LLM providers
func (a *App) ListLLMProviders() ([]LLMProviderSetting, error) {
	providers, err := services.ListLLMProvidersByWorkspace(1) // Using workspace ID 1
	if err != nil {
		return nil, err
	}

	var settings []LLMProviderSetting
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

// CreateLLMProvider creates a new LLM provider configuration
func (a *App) CreateLLMProvider(name, provider, model, baseURL, apiKey string) error {
	_, err := services.CreateLLMProvider(1, 1, name, provider, model, baseURL, apiKey, true, "")
	return err
}

// UpdateLLMProvider updates an existing LLM provider configuration
func (a *App) UpdateLLMProvider(id uint, name, model, baseURL, apiKey string) error {
	_, err := services.UpdateLLMProvider(id, &name, &model, &baseURL, &apiKey, nil)
	return err
}

// DeleteLLMProvider deletes an LLM provider configuration
func (a *App) DeleteLLMProvider(id uint) error {
	return services.DeleteLLMProvider(id)
}

// SetDefaultLLMProvider sets a provider as the default
func (a *App) SetDefaultLLMProvider(id uint) error {
	return services.SetDefaultLLMProvider(1, id)
}

// TestLLMProviderConnection tests if an LLM provider configuration is valid
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
	ID                  uint    `json:"id"`
	Name                string  `json:"name"`
	Type                string  `json:"type"` // mysql, postgres, sqlite
	Host                string  `json:"host,omitempty"`
	Port                int     `json:"port,omitempty"`
	Database            string  `json:"database,omitempty"`
	Username            string  `json:"username,omitempty"`
	SSLMode             string  `json:"ssl_mode,omitempty"`
	IsDefault           bool    `json:"is_default"`
	IsActive            bool    `json:"is_active"`
	ExplorationAllowed  bool    `json:"exploration_allowed"`
	MaxExplorationRounds int    `json:"max_exploration_rounds"`
	ExplorationSafety   string  `json:"exploration_safety"`
	Config              string  `json:"config,omitempty"` // JSON string
}

// ListDBConnections retrieves all configured database connections
func (a *App) ListDBConnections() ([]DBConnectionSetting, error) {
	connections, err := services.ListDBConnectionsByWorkspace(1) // Using workspace ID 1
	if err != nil {
		return nil, err
	}

	var settings []DBConnectionSetting
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
		
		// Parse exploration settings from config
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
			ID:                  c.ID,
			Name:                c.Name,
			Type:                c.Type,
			Host:                host,
			Port:                port,
			Database:            database,
			Username:            username,
			SSLMode:             sslMode,
			IsDefault:           c.IsDefault,
			IsActive:            c.IsActive,
			ExplorationAllowed:  explorationAllowed,
			MaxExplorationRounds: maxExplorationRounds,
			ExplorationSafety:   explorationSafety,
			Config:              configStr,
		})
	}
	return settings, nil
}

// CreateDBConnection creates a new database connection configuration
func (a *App) CreateDBConnection(name, dbType, host string, port int, database, username, password, sslMode, config string) error {
	_, err := services.CreateDBConnection(1, 1, name, dbType, host, port, database, username, password, sslMode, config)
	return err
}

// UpdateDBConnection updates an existing database connection configuration
func (a *App) UpdateDBConnection(id uint, name, host, database, username, password, sslMode string, port int, config string) error {
	_, err := services.UpdateDBConnection(id, &name, &host, &port, &database, &username, &password, &sslMode, &config)
	return err
}

// DeleteDBConnection deletes a database connection configuration
func (a *App) DeleteDBConnection(id uint) error {
	return services.DeleteDBConnection(id)
}

// SetDefaultDBConnection sets a connection as the default
func (a *App) SetDefaultDBConnection(id uint) error {
	return services.SetDefaultDBConnection(1, id)
}

// TestDBConnection tests if a database connection is valid by actually pinging it.
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

// SchemaTablePreview represents a single table preview for the Settings UI.
type SchemaTablePreview struct {
	Name      string              `json:"name"`
	RowCount  int64               `json:"row_count"`
	Columns   []SchemaColumnPreview `json:"columns"`
	Indexes   int                 `json:"indexes"`
	ForeignKeys int               `json:"foreign_keys"`
}

// SchemaColumnPreview represents a single column preview for the Settings UI.
type SchemaColumnPreview struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	IsNullable   bool   `json:"is_nullable"`
}

// GetSchemaPreview fetches and returns schema metadata for a DB connection.
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
			Name:         t.Name,
			RowCount:     t.RowCount,
			Columns:      cols,
			Indexes:      len(t.Indexes),
			ForeignKeys:  len(t.ForeignKeys),
		})
	}

	return preview, nil
}

// QueryResult represents a row from a query execution

type QueryResult struct {
	Columns []string      `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	TotalRows int        `json:"total_rows"`
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

		// Convert values to JSON-safe types
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

// GeneralSettings represents the general application settings
type GeneralSettings struct {
	AppName            string `json:"app_name"`
	AppVersion         string `json:"app_version"`
	DefaultLLMProvider string `json:"default_llm_provider"`
	Theme              string `json:"theme"` // light, dark, system
	Language           string `json:"language"`
}

// GetGeneralSettings retrieves the general application settings
func (a *App) GetGeneralSettings() GeneralSettings {
	return GeneralSettings{
		AppName:            "YourQL",
		AppVersion:         "0.1.0",
		DefaultLLMProvider: "openai",
		Theme:              "light",
		Language:           "en",
	}
}

// UpdateGeneralSettings updates the general application settings
func (a *App) UpdateGeneralSettings(settings GeneralSettings) error {
	// In a real implementation, this would save to a config file or database
	// For now, we just return nil
	return nil
}
