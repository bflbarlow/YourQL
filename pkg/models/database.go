package models

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

// getDBPath returns the path to the local SQLite database file
func getDBPath() string {
	usr, err := user.Current()
	if err != nil {
		// Fallback to current directory
		return "yourql.db"
	}

	// Use ~/.yourql/ directory
	dir := filepath.Join(usr.HomeDir, ".yourql")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Warning: could not create .yourql directory: %v", err)
		return "yourql.db"
	}

	return filepath.Join(dir, "yourql.db")
}

// ConnectDatabase initializes the SQLite database and runs migrations
func ConnectDatabase() {
	dbPath := getDBPath()
	var err error
	DB, err = sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		log.Fatalf("Failed to open SQLite database: %v", err)
	}

	// Configure connection pool
	DB.SetMaxOpenConns(1) // SQLite is file-based, single writer
	DB.SetMaxIdleConns(1)

	// Test the connection
	if err := DB.Ping(); err != nil {
		log.Fatalf("Failed to ping SQLite database: %v", err)
	}

	fmt.Printf("Connected to SQLite database at: %s\n", dbPath)

	// Run migrations
	if err := migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
}

// migrate creates all necessary tables if they don't exist
func migrate() error {
	// Create tables
	tables := []string{
		// Workspaces
		`CREATE TABLE IF NOT EXISTS workspaces (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT,
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS workspace_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			role TEXT DEFAULT 'member',
			is_active INTEGER DEFAULT 1,
			joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS workspace_settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL,
			key TEXT NOT NULL,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			UNIQUE(workspace_id, key)
		)`,

		// LLM Providers
		`CREATE TABLE IF NOT EXISTS llm_providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL DEFAULT 1,
			name TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT,
			base_url TEXT,
			api_key TEXT,
			is_default INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			config TEXT,
			created_by INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,

		// Database Connections
		`CREATE TABLE IF NOT EXISTS db_connections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL DEFAULT 1,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			host TEXT,
			port INTEGER,
			database TEXT,
			username TEXT,
			password TEXT,
			ssl_mode TEXT,
			is_default INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			config TEXT,
			created_by INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,

		// Conversations/Discussions
		`CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL DEFAULT 1,
			user_id INTEGER NOT NULL,
			db_connection_id INTEGER,
			llm_provider_id INTEGER,
			title TEXT,
			status TEXT DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY (db_connection_id) REFERENCES db_connections(id) ON DELETE SET NULL,
			FOREIGN KEY (llm_provider_id) REFERENCES llm_providers(id) ON DELETE SET NULL
		)`,

		// Conversation Messages
		`CREATE TABLE IF NOT EXISTS conversation_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			llm_content TEXT,
			sql_results TEXT,
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
		)`,

		// Queries
		`CREATE TABLE IF NOT EXISTS queries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL DEFAULT 1,
			user_id INTEGER NOT NULL DEFAULT 1,
			conversation_id INTEGER,
			question TEXT NOT NULL,
			llm_provider_id INTEGER,
			db_connection_id INTEGER,
			original_query TEXT,
			generated_sql TEXT,
			db_connection_name TEXT,
			status TEXT DEFAULT 'pending',
			result_summary TEXT,
			error_message TEXT,
			execution_time_ms INTEGER,
			tokens_used INTEGER,
			cost_estimate TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
		)`,

		// Saved Queries
		`CREATE TABLE IF NOT EXISTS saved_queries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL DEFAULT 1,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			sql_text TEXT NOT NULL,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,

		// Integration Logs
		`CREATE TABLE IF NOT EXISTS integration_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL DEFAULT 1,
			level TEXT DEFAULT 'info',
			message TEXT,
			data TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,

		// Users
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT,
			name TEXT,
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Login attempts
		`CREATE TABLE IF NOT EXISTS logins (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			email TEXT,
			success INTEGER DEFAULT 0,
			ip_address TEXT,
			user_agent TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Workspace invitations
		`CREATE TABLE IF NOT EXISTS workspace_invitations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL,
			email TEXT NOT NULL,
			role TEXT DEFAULT 'member',
			token TEXT UNIQUE NOT NULL,
			expires_at DATETIME,
			created_by INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,

		// Workspace roles
		`CREATE TABLE IF NOT EXISTS workspace_roles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			permissions TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			UNIQUE(workspace_id, name)
		)`,

		// Workspace user roles mapping
		`CREATE TABLE IF NOT EXISTS workspace_user_roles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			role_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY (role_id) REFERENCES workspace_roles(id) ON DELETE CASCADE,
			UNIQUE(workspace_id, user_id, role_id)
		)`,
	}

	for _, ddl := range tables {
		if _, err := DB.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Create default workspace if none exists
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM workspaces").Scan(&count)
	if count == 0 {
		DB.Exec("INSERT INTO workspaces (name, is_active) VALUES (?, 1)", "Default Workspace")
	}

	// Create default user if none exists
	DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		DB.Exec("INSERT INTO users (email, name, is_active) VALUES (?, ?, 1)", "admin@yourql.local", "Admin")
	}

	// Ensure default user is a member of default workspace
	DB.QueryRow("SELECT COUNT(*) FROM workspace_users WHERE workspace_id = ? AND user_id = ?", 1, 1).Scan(&count)
	if count == 0 {
		DB.Exec("INSERT INTO workspace_users (workspace_id, user_id, role, is_active) VALUES (?, ?, 'owner', 1)", 1, 1)
	}

	// Ensure workspace_users has is_active column (migration from old schema)
	addColumnIfNotExists("workspace_users", "is_active", "INTEGER DEFAULT 1")
	addColumnIfNotExists("workspace_users", "joined_at", "DATETIME DEFAULT CURRENT_TIMESTAMP")
	addColumnIfNotExists("workspace_users", "updated_at", "DATETIME DEFAULT CURRENT_TIMESTAMP")

	// Ensure workspaces has is_active column
	addColumnIfNotExists("workspaces", "is_active", "INTEGER DEFAULT 1")

	// Ensure conversation_messages has required columns
	addColumnIfNotExists("conversation_messages", "llm_content", "TEXT")
	addColumnIfNotExists("conversation_messages", "sql_results", "TEXT")
	addColumnIfNotExists("conversation_messages", "metadata", "TEXT")

	// Ensure queries table has all required columns
	addColumnIfNotExists("queries", "workspace_id", "INTEGER NOT NULL DEFAULT 1")
	addColumnIfNotExists("queries", "user_id", "INTEGER NOT NULL DEFAULT 1")
	addColumnIfNotExists("queries", "question", "TEXT NOT NULL")
	addColumnIfNotExists("queries", "llm_provider_id", "INTEGER")
	addColumnIfNotExists("queries", "db_connection_id", "INTEGER")
	addColumnIfNotExists("queries", "original_query", "TEXT")
	addColumnIfNotExists("queries", "db_connection_name", "TEXT")
	addColumnIfNotExists("queries", "result_summary", "TEXT")
	addColumnIfNotExists("queries", "execution_time_ms", "INTEGER")
	addColumnIfNotExists("queries", "tokens_used", "INTEGER")
	addColumnIfNotExists("queries", "cost_estimate", "TEXT")

	// Soft‑delete support for conversations
	addColumnIfNotExists("conversations", "deleted_at", "DATETIME")
	
	// Technical details toggle for conversations
	addColumnIfNotExists("conversations", "tech_details", "INTEGER DEFAULT 0")

	// Ensure workspace_settings exists (created above but checking)
	DB.Exec(`CREATE TABLE IF NOT EXISTS workspace_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workspace_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		value TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
		UNIQUE(workspace_id, key)
	)`)

	return nil
}

// addColumnIfNotExists adds a column to a table if it doesn't already exist.
// SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN.
// We try the ALTER TABLE and ignore "duplicate column" errors.
func addColumnIfNotExists(tableName, columnName, columnDef string) error {
	_, err := DB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnDef))
	if err != nil {
		errStr := err.Error()
		// SQLite errors for duplicate column contain "duplicate column name"
		if strings.Contains(errStr, "duplicate column") {
			return nil
		}
		return err
	}
	return nil
}
