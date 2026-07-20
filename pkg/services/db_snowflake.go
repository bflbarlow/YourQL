package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"YourQL/pkg/models"

	sf "github.com/snowflakedb/gosnowflake"
)

func init() {
	RegisterDriver(&SnowflakeDriver{})
}

// SnowflakeDriver implements DBDriver for Snowflake.
type SnowflakeDriver struct{}

func (d *SnowflakeDriver) TypeKey() string      { return "snowflake" }
func (d *SnowflakeDriver) OpenDriver() string   { return "snowflake" }
func (d *SnowflakeDriver) DisplayName() string  { return "Snowflake" }
func (d *SnowflakeDriver) DefaultPort() int     { return 443 }
func (d *SnowflakeDriver) SQLDialectHint() string {
	return "Snowflake — double-quote identifier quoting, LIMIT, ILIKE, QUALIFY clause, VARIANT/ARRAY types"
}

// SnowflakeExtra holds Snowflake-specific connection parameters.
type SnowflakeExtra struct {
	Account       string `json:"account"`
	Warehouse     string `json:"warehouse,omitempty"`
	Role          string `json:"role,omitempty"`
	SchemaName    string `json:"schema_name,omitempty"`
	Authenticator string `json:"authenticator,omitempty"` // snowflake, oauth, externalbrowser
}

func parseSnowflakeExtra(conn *models.DataSource) SnowflakeExtra {
	if conn.Extra == nil || *conn.Extra == "" {
		return SnowflakeExtra{}
	}
	var extra SnowflakeExtra
	if err := json.Unmarshal([]byte(*conn.Extra), &extra); err != nil {
		return SnowflakeExtra{}
	}
	return extra
}

func (d *SnowflakeDriver) BuildDSN(conn *models.DataSource) (string, error) {
	if conn.Database == nil {
		return "", fmt.Errorf("database name is required")
	}

	username := ""
	if conn.Username != nil {
		username = *conn.Username
	}

	password := ""
	if conn.Password != nil {
		password = *conn.Password
	}

	extra := parseSnowflakeExtra(conn)
	if extra.Account == "" {
		return "", fmt.Errorf("Snowflake account identifier is required (set in extra.account)")
	}

	cfg := sf.Config{
		Account:  extra.Account,
		User:     username,
		Password: password,
		Database: *conn.Database,
	}

	if conn.Host != nil && *conn.Host != "" {
		cfg.Host = *conn.Host
	}
	if conn.Port != nil && *conn.Port != 0 {
		cfg.Port = *conn.Port
	}
	if extra.Warehouse != "" {
		cfg.Warehouse = extra.Warehouse
	}
	if extra.Role != "" {
		cfg.Role = extra.Role
	}
	if extra.SchemaName != "" {
		cfg.Schema = extra.SchemaName
	}

	dsn, err := sf.DSN(&cfg)
	if err != nil {
		return "", fmt.Errorf("failed to build Snowflake DSN: %w", err)
	}

	log.Printf("[snowflake] Built DSN for account %s, database %s, user %s", extra.Account, *conn.Database, username)
	return dsn, nil
}

func (d *SnowflakeDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	if conn.Database == nil {
		return nil, fmt.Errorf("database name is required")
	}

	dsn, err := d.BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	config, _ := conn.ParseConfig()

	extra := parseSnowflakeExtra(conn)

	// Use SHOW TERSE TABLES which works even for shared databases
	// where INFORMATION_SCHEMA access may be restricted.
	query := fmt.Sprintf("SHOW TERSE TABLES IN DATABASE %s", *conn.Database)
	if extra.SchemaName != "" {
		query = fmt.Sprintf("SHOW TERSE TABLES IN SCHEMA %s.%s", *conn.Database, extra.SchemaName)
	}
	log.Printf("[snowflake] Schema query: %s", query)

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	tableCount := 0
	var tables []TableInfo
	for rows.Next() {
		tableCount++
		// SHOW TERSE TABLES returns: created_on, name, kind, database_name, schema_name
		var createdOn, tableName, kind, dbName, schemaName string
		if err := rows.Scan(&createdOn, &tableName, &kind, &dbName, &schemaName); err != nil {
			log.Printf("[snowflake] Warning: failed to scan table row: %v", err)
			continue
		}
		// Skip views
		if strings.ToUpper(kind) == "VIEW" {
			continue
		}
		log.Printf("[snowflake] Found table: %s.%s (kind=%s)", schemaName, tableName, kind)
		table, err := getSnowflakeTableInfo(db, *conn.Database, schemaName, tableName, config)
		if err != nil {
			continue
		}
		table.Name = schemaName + "." + tableName
		tables = append(tables, *table)
	}

	log.Printf("[snowflake] GetSchema found %d tables in %d scanned", len(tables), tableCount)

	return &DataSchema{Tables: tables}, nil
}

func getSnowflakeTableInfo(db *sql.DB, dbName, schemaName, tableName string, config *models.DataSourceConfig) (*TableInfo, error) {
	fullName := fmt.Sprintf("%s.%s.%s", dbName, schemaName, tableName)

	// Use SELECT * LIMIT 0 to get column names and types via result metadata.
	// This bypasses INFORMATION_SCHEMA which is restricted on shared databases.
	colRows, err := db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT 0", fullName))
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer colRows.Close()

	colTypes, err := colRows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	var columns []ColumnInfo
	for _, ct := range colTypes {
		nullable, _ := ct.Nullable()
		columns = append(columns, ColumnInfo{
			Name:       ct.Name(),
			DataType:   ct.DatabaseTypeName(),
			IsNullable: nullable,
		})
	}

	// Row count
	var rowCount int64
	err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", fullName)).Scan(&rowCount)
	if err != nil {
		log.Printf("[snowflake] Warning: could not get row count for %s: %v", fullName, err)
	}

	// PKs and FKs cannot be introspected without INFORMATION_SCHEMA access.
	// Shared Snowflake databases don't expose INFORMATION_SCHEMA to non-owner roles.

	return &TableInfo{
		Name:     tableName,
		Columns:  columns,
		RowCount: rowCount,
	}, nil
}