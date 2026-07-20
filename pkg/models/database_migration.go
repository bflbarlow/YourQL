package models

import "log"

// migrateDbToDataSources handles the Phase A data rebrand for existing databases.
// Called from migrate(); uses ADD COLUMN + UPDATE instead of ALTER TABLE RENAME
// because modernc.org/sqlite does not support RENAME COLUMN.
func migrateDbToDataSources() error {
	var count int

	// Check if old table exists
	DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='db_connections'").Scan(&count)

	// Create data_sources table if it doesn't exist
	DB.Exec(`CREATE TABLE IF NOT EXISTS data_sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		host TEXT,
		port INTEGER,
		database_name TEXT,
		username TEXT,
		password TEXT,
		ssl_mode TEXT,
		is_default INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 1,
		config TEXT,
		extra TEXT,
		file_path TEXT DEFAULT '',
		file_type TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// Copy from db_connections if old table still exists
	if count > 0 {
		log.Println("Migrating: copying db_connections → data_sources")
		DB.Exec(`INSERT OR IGNORE INTO data_sources (id, name, type, host, port, database_name, username, password, ssl_mode, is_default, is_active, config, extra, created_at, updated_at)
			SELECT id, name, type, host, port, "database", username, password, ssl_mode, is_default, is_active, config, extra, created_at, updated_at
			FROM db_connections`)
	}

	// Add data_source_id column to conversations if needed
	dsExists, _ := columnExists("conversations", "data_source_id")
	if !dsExists {
		log.Println("Migrating: adding data_source_id to conversations")
		ensureColumn("conversations", "data_source_id", "INTEGER")
		DB.Exec("UPDATE conversations SET data_source_id = db_connection_id")
	}

	// Add data_source_id to queries if needed
	qDsExists, _ := columnExists("queries", "data_source_id")
	if !qDsExists {
		log.Println("Migrating: adding data_source_id to queries")
		ensureColumn("queries", "data_source_id", "INTEGER")
		DB.Exec("UPDATE queries SET data_source_id = db_connection_id")
	}

	log.Println("Migration complete: db → data_sources")
	return nil
}