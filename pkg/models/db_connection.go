package models

import (
	"encoding/json"
	"time"
)

// DBConnection represents a database connection.
type DBConnection struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // mysql, postgres, sqlite
	Host       *string   `json:"host,omitempty"`
	Port       *int      `json:"port,omitempty"`
	Database   *string   `json:"database,omitempty"`
	Username   *string   `json:"username,omitempty"`
	Password   *string   `json:"-"`
	SSLMode    *string   `json:"ssl_mode,omitempty"`
	IsDefault  bool      `json:"is_default"`
	IsActive   bool      `json:"is_active"`
	Config     *string   `json:"config,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DBConnectionConfig holds additional configuration for a database connection.
type DBConnectionConfig struct {
	SystemPrompt         string            `json:"system_prompt,omitempty"`
	BusinessRules        []string          `json:"business_rules,omitempty"`
	TableDescriptions    map[string]string `json:"table_descriptions,omitempty"`
	ColumnDescriptions   map[string]string `json:"column_descriptions,omitempty"`
	IncludeIndexes       bool              `json:"include_indexes,omitempty"`
	IncludeForeignKeys   bool              `json:"include_foreign_keys,omitempty"`
	IncludeTableComments bool              `json:"include_table_comments,omitempty"`
	MaxExplorationRounds int               `json:"max_exploration_rounds,omitempty"`
	ExplorationSafety    string            `json:"exploration_safety,omitempty"`
	ExplorationAllowed   bool              `json:"exploration_allowed,omitempty"`
	MaxActionRetries     int               `json:"max_action_retries,omitempty"`
	MaxFinalQueryRetries int               `json:"max_final_query_retries,omitempty"`
	DefaultLimit         int               `json:"default_limit,omitempty"`
	ExplorationDefaultLimit int            `json:"exploration_default_limit,omitempty"`
	QueryLengthThreshold int               `json:"query_length_threshold,omitempty"`
}

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
