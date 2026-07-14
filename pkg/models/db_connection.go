package models

import (
	"encoding/json"
	"time"
)

// DBConnection represents a database connection configured for a workspace.
type DBConnection struct {
	ID          uint      `json:"id"`
	WorkspaceID uint      `json:"workspace_id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"` // mysql, postgres, sqlite
	Host        *string   `json:"host,omitempty"`
	Port        *int      `json:"port,omitempty"`
	Database    *string   `json:"database,omitempty"`
	Username    *string   `json:"username,omitempty"`
	Password    *string   `json:"-"`        // encrypted, never exposed in JSON
	SSLMode     *string   `json:"ssl_mode,omitempty"`
	IsDefault   bool      `json:"is_default"`
	IsActive    bool      `json:"is_active"`
	Config      *string   `json:"config,omitempty"` // JSON string (pool settings, timeouts, system prompt)
	CreatedBy   *uint     `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DBConnectionConfig holds additional configuration for a database connection.
type DBConnectionConfig struct {
	SystemPrompt       string            `json:"system_prompt,omitempty"`
	BusinessRules      []string          `json:"business_rules,omitempty"`
	TableDescriptions  map[string]string `json:"table_descriptions,omitempty"`
	ColumnDescriptions map[string]string `json:"column_descriptions,omitempty"` // "table.column" -> description
	IncludeIndexes     bool              `json:"include_indexes,omitempty"`
	IncludeForeignKeys bool              `json:"include_foreign_keys,omitempty"`
	IncludeTableComments bool            `json:"include_table_comments,omitempty"`

	// Exploration settings
	MaxExplorationRounds int    `json:"max_exploration_rounds,omitempty"`
	ExplorationSafety    string `json:"exploration_safety,omitempty"`
	ExplorationAllowed   bool   `json:"exploration_allowed,omitempty"`

	// Retry settings
	MaxActionRetries int `json:"max_action_retries,omitempty"`

	// Final query retry budget (after all exploration rounds or on first try)
	MaxFinalQueryRetries int `json:"max_final_query_retries,omitempty"`

	// DefaultLimit overrides the app-level default for queries without a LIMIT clause.
	// Set to 0 to disable (no limit applied).
	DefaultLimit int `json:"default_limit,omitempty"`

	// ExplorationDefaultLimit overrides the app-level exploration limit.
	// Set to 0 to disable (no limit applied).
	ExplorationDefaultLimit int `json:"exploration_default_limit,omitempty"`

	// QueryLengthThreshold overrides the app-level threshold.
	// If the LLM's SQL query length exceeds this value, the default limit is applied.
	// Set to 0 to use the app-level default. Set to -1 to always apply the limit.
	QueryLengthThreshold int `json:"query_length_threshold,omitempty"`
}

// ParseConfig parses the Config JSON field into a DBConnectionConfig.
func (c *DBConnection) ParseConfig() (*DBConnectionConfig, error) {
	if c.Config == nil || *c.Config == "" {
		return &DBConnectionConfig{}, nil
	}
	var config DBConnectionConfig
	if err := json.Unmarshal([]byte(*c.Config), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// SetConfig updates the Config JSON field from a DBConnectionConfig.
func (c *DBConnection) SetConfig(config *DBConnectionConfig) error {
	if config == nil {
		c.Config = nil
		return nil
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	s := string(data)
	c.Config = &s
	return nil
}
