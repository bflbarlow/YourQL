package services

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"YourQL/pkg/models"
)

// openDriverName resolves the sql.Open driver name for a given db type.
func openDriverName(dbType string) string {
	driver, err := GetDriver(dbType)
	if err != nil {
		return dbType // fallback
	}
	return driver.OpenDriver()
}

// BuildDSN builds a connection string based on the database type.
func BuildDSN(conn *models.DataSource) (string, error) {
	driver, err := GetDriver(conn.Type)
	if err != nil {
		return "", err
	}
	return driver.BuildDSN(conn)
}

// TestDataSource tests if a database connection is valid and reachable.
func TestDataSource(conn *models.DataSource) error {
	// Use NativeQuerier if available (e.g., BigQuery)
	driver, _ := GetDriver(conn.Type)
	if nq, ok := driver.(NativeQuerier); ok {
		return nq.PingNative(conn)
	}

	dsn, err := BuildDSN(conn)
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open(openDriverName(conn.Type), dsn)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer db.Close()

	// Run ping in a goroutine with a hard timeout, since some drivers
	// (e.g. Snowflake/gosnowflake) may not respect context cancellation
	// during initial authentication handshake.
	errChan := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		var v int
		errChan <- db.QueryRowContext(ctx, "SELECT 1").Scan(&v)
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		return nil
	case <-time.After(15 * time.Second):
		return fmt.Errorf("connection test timed out after 15 seconds")
	}
}