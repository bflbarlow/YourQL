package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"YourQL/pkg/models"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func init() {
	RegisterDriver(&PostgresDriver{})
}

// PostgresDriver implements DBDriver for PostgreSQL.
type PostgresDriver struct{}

func (d *PostgresDriver) TypeKey() string      { return "postgresql" }
func (d *PostgresDriver) OpenDriver() string   { return "pgx" }
func (d *PostgresDriver) DisplayName() string  { return "PostgreSQL" }
func (d *PostgresDriver) DefaultPort() int     { return 5432 }
func (d *PostgresDriver) SQLDialectHint() string {
	return "PostgreSQL — double-quote \"identifier\" quoting, LIMIT/OFFSET, ILIKE, ::type casts"
}

// PostgresExtra holds PostgreSQL-specific connection parameters stored in the Extra JSON field.
type PostgresExtra struct {
	SSLMode    string `json:"sslmode,omitempty"`    // disable, require, verify-ca, verify-full
	SearchPath string `json:"search_path,omitempty"` // e.g., "public,my_schema"
}

func parsePostgresExtra(conn *models.DBConnection) PostgresExtra {
	if conn.Extra == nil || *conn.Extra == "" {
		return PostgresExtra{SSLMode: "require"}
	}
	var extra PostgresExtra
	if err := json.Unmarshal([]byte(*conn.Extra), &extra); err != nil {
		return PostgresExtra{SSLMode: "require"}
	}
	if extra.SSLMode == "" {
		extra.SSLMode = "require"
	}
	return extra
}

func (d *PostgresDriver) BuildDSN(conn *models.DBConnection) (string, error) {
	if conn.Database == nil {
		return "", fmt.Errorf("database name is required")
	}

	host := "localhost"
	if conn.Host != nil && *conn.Host != "" {
		host = *conn.Host
	}

	port := 5432
	if conn.Port != nil {
		port = *conn.Port
	}

	username := "postgres"
	if conn.Username != nil {
		username = *conn.Username
	}

	password := ""
	if conn.Password != nil {
		password = *conn.Password
	}

	extra := parsePostgresExtra(conn)

	// Build connection string URL-style for pgx
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   *conn.Database,
	}

	q := u.Query()
	if extra.SSLMode != "disable" {
		q.Set("sslmode", extra.SSLMode)
	}
	if extra.SearchPath != "" {
		q.Set("search_path", extra.SearchPath)
	}
	u.RawQuery = q.Encode()

	log.Printf("[postgres] Built DSN for database %s, user %s, host %s:%d", *conn.Database, username, host, port)
	return u.String(), nil
}

func (d *PostgresDriver) GetSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	if conn.Database == nil {
		return nil, fmt.Errorf("database name is required")
	}

	dsn, err := d.BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	config, _ := conn.ParseConfig()

	rows, err := db.Query(`
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		table, err := getPostgresTableInfo(db, tableName, config)
		if err != nil {
			continue
		}
		tables = append(tables, *table)
	}

	return &DatabaseSchema{Tables: tables}, nil
}

func getPostgresTableInfo(db *sql.DB, tableName string, config *models.DBConnectionConfig) (*TableInfo, error) {
	colRows, err := db.Query(`
		SELECT column_name, data_type, is_nullable,
			CASE WHEN column_name IN (
				SELECT kcu.column_name
				FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage kcu
					ON tc.constraint_name = kcu.constraint_name
					AND tc.table_schema = kcu.table_schema
				WHERE tc.constraint_type = 'PRIMARY KEY'
					AND tc.table_name = $1
			) THEN true ELSE false END AS is_pk,
			column_default
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position
	`, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer colRows.Close()

	var columns []ColumnInfo
	for colRows.Next() {
		var colName, dataType, nullable string
		var isPK bool
		var defVal sql.NullString
		if err := colRows.Scan(&colName, &dataType, &nullable, &isPK, &defVal); err != nil {
			continue
		}
		columns = append(columns, ColumnInfo{
			Name:         colName,
			DataType:     dataType,
			IsNullable:   nullable == "YES",
			IsPrimaryKey: isPK,
			DefaultValue: defVal.String,
		})
	}

	// Get table comment
	var tableComment sql.NullString
	err = db.QueryRow(`
		SELECT obj_description(($1::regclass)::oid, 'pg_class')
	`, tableName).Scan(&tableComment)
	if err == nil && tableComment.Valid {
		tableComment.String = strings.TrimSpace(tableComment.String)
	}

	// Row count
	var rowCount int64
	db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", tableName)).Scan(&rowCount)

	// Indexes
	var indexes []IndexInfo
	if config != nil && config.IncludeIndexes {
		idxRows, idxErr := db.Query(`
			SELECT indexname, indexdef
			FROM pg_indexes
			WHERE tablename = $1
		`, tableName)
		if idxErr == nil {
			defer idxRows.Close()
			for idxRows.Next() {
				var idxName, idxDef string
				if scanErr := idxRows.Scan(&idxName, &idxDef); scanErr == nil {
					isUnique := strings.Contains(strings.ToUpper(idxDef), "UNIQUE INDEX")
					indexes = append(indexes, IndexInfo{
						Name:     idxName,
						IsUnique: isUnique,
						Columns:  []string{},
					})
				}
			}
		}
	}

	// Foreign keys
	var foreignKeys []ForeignKeyInfo
	if config != nil && config.IncludeForeignKeys {
		fkRows, fkErr := db.Query(`
			SELECT tc.constraint_name, kcu.column_name,
				ccu.table_name AS foreign_table_name,
				ccu.column_name AS foreign_column_name,
				rc.delete_rule, rc.update_rule
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
			JOIN information_schema.constraint_column_usage ccu
				ON ccu.constraint_name = tc.constraint_name
				AND ccu.table_schema = tc.table_schema
			JOIN information_schema.referential_constraints rc
				ON rc.constraint_name = tc.constraint_name
			WHERE tc.constraint_type = 'FOREIGN KEY'
				AND tc.table_name = $1
		`, tableName)
		if fkErr == nil {
			defer fkRows.Close()
			for fkRows.Next() {
				var fkName, fkCol, refTable, refCol, onDelete, onUpdate sql.NullString
				if scanErr := fkRows.Scan(&fkName, &fkCol, &refTable, &refCol, &onDelete, &onUpdate); scanErr == nil {
					fk := ForeignKeyInfo{
						Name:      fkName.String,
						Column:    fkCol.String,
						RefTable:  refTable.String,
						RefColumn: refCol.String,
					}
					if onDelete.Valid {
						fk.OnDelete = onDelete.String
					}
					if onUpdate.Valid {
						fk.OnUpdate = onUpdate.String
					}
					foreignKeys = append(foreignKeys, fk)
				}
			}
		}
	}

	return &TableInfo{
		Name:        tableName,
		Columns:     columns,
		RowCount:    rowCount,
		Description: tableComment.String,
		Indexes:     indexes,
		ForeignKeys: foreignKeys,
	}, nil
}