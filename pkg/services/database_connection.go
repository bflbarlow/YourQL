package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"YourQL/pkg/models"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// BuildDSN builds a connection string based on the database type.
func BuildDSN(conn *models.DBConnection) (string, error) {
	switch conn.Type {
	case "mysql":
		return buildMySQLDSN(conn)
	case "sqlite":
		return buildSQLiteDSN(conn)
	default:
		return "", fmt.Errorf("unsupported database type: %s", conn.Type)
	}
}

// buildMySQLDSN builds a MySQL DSN from a DBConnection.
func buildMySQLDSN(conn *models.DBConnection) (string, error) {
	if conn.Database == nil {
		return "", fmt.Errorf("database name is required")
	}
	database := *conn.Database

	host := "localhost"
	if conn.Host != nil && *conn.Host != "" {
		host = *conn.Host
	}

	port := 3306
	if conn.Port != nil {
		port = *conn.Port
	}

	username := "root"
	if conn.Username != nil {
		username = *conn.Username
	}

	password := ""
	if conn.Password != nil {
		password = *conn.Password
	}

	// Build DSN with optional SSL mode
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true", username, password, host, port, database)

	if conn.SSLMode != nil && *conn.SSLMode != "" {
		sslMode := strings.ToLower(*conn.SSLMode)
		switch sslMode {
		case "required":
			dsn += "&tls=true"
		case "verify-ca", "verify-full":
			dsn += "&tls=skip-verify"
		}
	}

	log.Printf("[database_connection] Built DSN for database %s, user %s, host %s:%d", database, username, host, port)
	return dsn, nil
}

// buildSQLiteDSN builds a SQLite DSN from a DBConnection.
func buildSQLiteDSN(conn *models.DBConnection) (string, error) {
	if conn.Database == nil || *conn.Database == "" {
		return "", fmt.Errorf("database path is required for SQLite")
	}
	return *conn.Database + "?_busy_timeout=5000&_journal_mode=WAL", nil
}

// TestDBConnection tests if a database connection is valid and reachable.
func TestDBConnection(conn *models.DBConnection) error {
	dsn, err := BuildDSN(conn)
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open(conn.Type, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer db.Close()

	db.SetConnMaxLifetime(5 * time.Second)
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}