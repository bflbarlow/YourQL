package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"YourQL/pkg/models"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// DatabaseSchema represents the schema of a database.
type DatabaseSchema struct {
	Tables []TableInfo `json:"tables"`
}

// TableInfo represents a table in the database.
type TableInfo struct {
	Name         string        `json:"name"`
	Columns      []ColumnInfo  `json:"columns"`
	RowCount     int64         `json:"row_count,omitempty"`
	Description  string        `json:"description,omitempty"`
	Indexes      []IndexInfo   `json:"indexes,omitempty"`
	ForeignKeys  []ForeignKeyInfo `json:"foreign_keys,omitempty"`
}

// ColumnInfo represents a column in a table.
type ColumnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsNullable   bool   `json:"is_nullable"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	DefaultValue string `json:"default_value,omitempty"`
	Description  string `json:"description,omitempty"`
}

// IndexInfo represents an index on a table.
type IndexInfo struct {
	Name       string   `json:"name"`
	IsUnique   bool     `json:"is_unique"`
	Columns    []string `json:"columns"`
}

// ForeignKeyInfo represents a foreign key constraint on a table.
type ForeignKeyInfo struct {
	Name         string `json:"name"`
	Column       string `json:"column"`
	RefTable     string `json:"ref_table"`
	RefColumn    string `json:"ref_column"`
	OnDelete     string `json:"on_delete,omitempty"`
	OnUpdate     string `json:"on_update,omitempty"`
}

// GetDatabaseSchema introspects the database connected via the given DBConnection
// and returns its schema. Supports MySQL and SQLite.
func GetDatabaseSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	switch conn.Type {
	case "mysql":
		return getMySQLSchema(conn)
	case "sqlite":
		return getSQLiteSchema(conn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", conn.Type)
	}
}

// GetSchemaForConnection returns schema info for a connection (for editing table descriptions).
func GetSchemaForConnection(conn *models.DBConnection) (*DatabaseSchema, error) {
	switch conn.Type {
	case "mysql":
		return getMySQLSchema(conn)
	case "sqlite":
		return getSQLiteSchema(conn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", conn.Type)
	}
}

// getMySQLSchema introspects a MySQL database.
func getMySQLSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	if conn.Database == nil {
		return nil, fmt.Errorf("database name is required")
	}

	dsn, err := buildMySQLDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}
	redactedDSN := dsn
	if idx := strings.Index(redactedDSN, ":"); idx != -1 {
		if atIdx := strings.Index(redactedDSN, "@"); atIdx != -1 && idx < atIdx {
			redactedDSN = redactedDSN[:idx+1] + "***" + redactedDSN[atIdx:]
		}
	}
	log.Printf("[database_introspection] Built DSN: %s", redactedDSN)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	config, _ := conn.ParseConfig()

	rows, err := db.Query("SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = ?", *conn.Database)
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
		table, err := getTableInfo(db, *conn.Database, tableName, config)
		if err != nil {
			continue
		}
		tables = append(tables, *table)
	}

	return &DatabaseSchema{Tables: tables}, nil
}

// getSQLiteSchema introspects a SQLite database.
func getSQLiteSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	if conn.Database == nil || *conn.Database == "" {
		return nil, fmt.Errorf("database path is required for SQLite")
	}

	dsn, err := buildSQLiteDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping SQLite: %w", err)
	}

	config, _ := conn.ParseConfig()

	// Get table names from sqlite_master
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
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
		table, err := getSQLiteTableInfo(db, tableName, config)
		if err != nil {
			continue
		}
		tables = append(tables, *table)
	}

	return &DatabaseSchema{Tables: tables}, nil
}

// getTableInfo retrieves column information, row count, indexes, and foreign keys for a table.
func getTableInfo(db *sql.DB, dbName, tableName string, config *models.DBConnectionConfig) (*TableInfo, error) {
	// Get columns
	colRows, err := db.Query(`
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`, dbName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer colRows.Close()

	var columns []ColumnInfo
	for colRows.Next() {
		var colName, dataType, nullable, colKey, defVal sql.NullString
		if err := colRows.Scan(&colName, &dataType, &nullable, &colKey, &defVal); err != nil {
			continue
		}
		isNullable := nullable.Valid && nullable.String == "YES"
		isPrimaryKey := colKey.Valid && colKey.String == "PRI"
		columns = append(columns, ColumnInfo{
			Name:         colName.String,
			DataType:     dataType.String,
			IsNullable:   isNullable,
			IsPrimaryKey: isPrimaryKey,
			DefaultValue: defVal.String,
		})
	}

	// Get table comment (description)
	var tableComment sql.NullString
	err = db.QueryRow(`
		SELECT TABLE_COMMENT FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
	`, dbName, tableName).Scan(&tableComment)
	if err == nil && tableComment.Valid {
		tableComment.String = strings.TrimSpace(tableComment.String)
	}

	// Get row count (approximate for performance)
	var rowCount int64
	db.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&rowCount)

	// Get indexes if configured
	var indexes []IndexInfo
	if config != nil && config.IncludeIndexes {
		idxRows, err := db.Query(`
			SELECT INDEX_NAME, NON_UNIQUE, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) as columns
			FROM INFORMATION_SCHEMA.STATISTICS
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			GROUP BY INDEX_NAME, NON_UNIQUE
		`, dbName, tableName)
		if err == nil {
			defer idxRows.Close()
			for idxRows.Next() {
				var idxName string
				var nonUnique uint
				var colStr string
				if err := idxRows.Scan(&idxName, &nonUnique, &colStr); err == nil {
					indexes = append(indexes, IndexInfo{
						Name:     idxName,
						IsUnique: nonUnique == 0,
						Columns:  strings.Split(colStr, ","),
					})
				}
			}
		}
	}

	// Get foreign keys if configured
	var foreignKeys []ForeignKeyInfo
	if config != nil && config.IncludeForeignKeys {
		fkRows, err := db.Query(`
			SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME,
				rc.DELETE_RULE, rc.UPDATE_RULE
			FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
			JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
				ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
				AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
			WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
				AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		`, dbName, tableName)
		if err == nil {
			defer fkRows.Close()
			for fkRows.Next() {
				var fkName, fkCol, refTable, refCol sql.NullString
				var onDelete, onUpdate sql.NullString
				if err := fkRows.Scan(&fkName, &fkCol, &refTable, &refCol, &onDelete, &onUpdate); err == nil {
					fk := ForeignKeyInfo{
						Name:     fkName.String,
						Column:   fkCol.String,
						RefTable: refTable.String,
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

// getSQLiteTableInfo retrieves column information for a SQLite table.
func getSQLiteTableInfo(db *sql.DB, tableName string, config *models.DBConnectionConfig) (*TableInfo, error) {
	// Get columns from pragma
	colRows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer colRows.Close()

	var columns []ColumnInfo
	for colRows.Next() {
		var cid int
		var colName, dataType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := colRows.Scan(&cid, &colName, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			continue
		}
		columns = append(columns, ColumnInfo{
			Name:         colName,
			DataType:     dataType,
			IsNullable:   notNull == 0,
			IsPrimaryKey: primaryKey != 0,
			DefaultValue: defaultValue.String,
		})
	}

	// Get row count
	var rowCount int64
	err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&rowCount)
	if err != nil {
		rowCount = 0
	}

	// Get indexes if configured
	var indexes []IndexInfo
	if config != nil && config.IncludeIndexes {
		idxRows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", tableName))
		if err == nil {
			defer idxRows.Close()
			for idxRows.Next() {
				var idxName string
				var seqno, unique, partial int
				if err := idxRows.Scan(&seqno, &idxName, &unique, &partial); err == nil {
					// Get columns for this index
					colRows2, err2 := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", idxName))
					if err2 == nil {
						var idxCols []string
						for colRows2.Next() {
							var seqno, cid2 string
							if err2 := colRows2.Scan(&seqno, &cid2, &idxCols); err2 == nil {
								idxCols = append(idxCols, cid2)
							}
						}
						colRows2.Close()
						indexes = append(indexes, IndexInfo{
							Name:     idxName,
							IsUnique: unique != 0,
							Columns:  idxCols,
						})
					}
				}
			}
		}
	}

	// Get foreign keys if configured
	var foreignKeys []ForeignKeyInfo
	if config != nil && config.IncludeForeignKeys {
		fkRows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", tableName))
		if err == nil {
			defer fkRows.Close()
			for fkRows.Next() {
				var id, seq, table, from, to string
				var onDelete, onUpdate, match string
				if err := fkRows.Scan(&id, &seq, &table, &from, &to, &onDelete, &onUpdate, &match); err == nil {
					fk := ForeignKeyInfo{
						Column:   from,
						RefTable: table,
						RefColumn: to,
					}
					if onDelete != "" {
						fk.OnDelete = onDelete
					}
					if onUpdate != "" {
						fk.OnUpdate = onUpdate
					}
					foreignKeys = append(foreignKeys, fk)
				}
			}
		}
	}

	return &TableInfo{
		Name:     tableName,
		Columns:  columns,
		RowCount: rowCount,
		Indexes:  indexes,
		ForeignKeys: foreignKeys,
	}, nil
}