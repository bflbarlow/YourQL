package services

import "YourQL/pkg/models"

// DBDriver defines the interface that each database driver must implement.
type DBDriver interface {
	// TypeKey returns the database type identifier used in conn.Type and the frontend (e.g., "mysql", "postgresql").
	TypeKey() string

	// OpenDriver returns the name passed to sql.Open() (e.g., "mysql", "pgx", "sqlite").
	OpenDriver() string

	// BuildDSN builds a connection string from a DataSource.
	BuildDSN(conn *models.DataSource) (string, error)

	// GetSchema introspects the database and returns its schema.
	GetSchema(conn *models.DataSource) (*DataSchema, error)

	// DisplayName returns a human-readable name for UI and prompts (e.g., "PostgreSQL").
	DisplayName() string

	// SQLDialectHint returns a short description of SQL dialect quirks for LLM prompts.
	SQLDialectHint() string

	// DefaultPort returns the default port for this database type.
	DefaultPort() int
}

// NativeQuerier is an optional interface for drivers that execute queries
// without using database/sql (e.g., BigQuery). If a driver implements this,
// sql_execution.go will use QueryRowsNative instead of sql.Open/Query.
type NativeQuerier interface {
	QueryRowsNative(conn *models.DataSource, query string) ([]string, [][]interface{}, error)
	PingNative(conn *models.DataSource) error
	CloseNative(conn *models.DataSource) error
}