package models

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func getDBPath() string {
	usr, err := user.Current()
	if err != nil {
		return "yourql.db"
	}
	dir := filepath.Join(usr.HomeDir, ".yourql")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Warning: could not create .yourql directory: %v", err)
		return "yourql.db"
	}
	return filepath.Join(dir, "yourql.db")
}

func ConnectDatabase() error {
	dbPath := getDBPath()
	var err error
	DB, err = sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}
	DB.SetMaxOpenConns(1)
	DB.SetMaxIdleConns(1)

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	fmt.Printf("Connected to SQLite database at: %s\n", dbPath)

	if err := migrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

func migrate() error {
	tables := []string{
		// LLM Providers
		`CREATE TABLE IF NOT EXISTS llm_providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT,
			base_url TEXT,
			api_key TEXT,
			is_default INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			config TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Database Connections
		`CREATE TABLE IF NOT EXISTS db_connections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Conversations
		`CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			db_connection_id INTEGER,
			llm_provider_id INTEGER,
			title TEXT,
			status TEXT DEFAULT 'active',
			max_messages INTEGER DEFAULT 0,
			max_context_messages INTEGER DEFAULT 10,
			pinned INTEGER DEFAULT 0,
			tech_details INTEGER DEFAULT 0,
		context_details INTEGER DEFAULT 0,
			deleted_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
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

		// Queries (query tracking log)
		`CREATE TABLE IF NOT EXISTS queries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
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
	}

	for _, ddl := range tables {
		if _, err := DB.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Ensure columns exist on older databases
	ensureColumn("conversations", "deleted_at", "DATETIME")
	ensureColumn("conversations", "tech_details", "INTEGER DEFAULT 0")
	ensureColumn("conversations", "context_details", "INTEGER DEFAULT 0")
	ensureColumn("conversations", "max_messages", "INTEGER DEFAULT 0")
	ensureColumn("conversations", "max_context_messages", "INTEGER DEFAULT 10")
	// Migrate existing conversations: set default to 10 if still 0
	_, _ = DB.Exec("UPDATE conversations SET max_context_messages = 10 WHERE max_context_messages = 0")
	ensureColumn("conversations", "pinned", "INTEGER DEFAULT 0")
	ensureColumn("conversation_messages", "llm_content", "TEXT")
	ensureColumn("conversation_messages", "sql_results", "TEXT")
	ensureColumn("conversation_messages", "metadata", "TEXT")

	return nil
}

func ensureColumn(tableName, columnName, columnDef string) error {
	exists, err := columnExists(tableName, columnName)
	if err != nil {
		return err
	}
	if !exists {
		_, err = DB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnDef))
		if err != nil {
			return fmt.Errorf("failed to add column %s.%s: %w", tableName, columnName, err)
		}
	}
	return nil
}

func columnExists(tableName, columnName string) (bool, error) {
	rows, err := DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, nil
}
