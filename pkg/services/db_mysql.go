package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"YourQL/pkg/models"

	_ "github.com/go-sql-driver/mysql"
)

func init() {
	RegisterDriver(&MySQLDriver{})
}

// MySQLDriver implements DBDriver for MySQL.
type MySQLDriver struct{}

func (d *MySQLDriver) TypeKey() string     { return "mysql" }
func (d *MySQLDriver) OpenDriver() string  { return "mysql" }
func (d *MySQLDriver) DisplayName() string { return "MySQL" }
func (d *MySQLDriver) DefaultPort() int    { return 3306 }
func (d *MySQLDriver) SQLDialectHint() string {
	return "MySQL — backtick `identifier` quoting, LIMIT for row limits, INFORMATION_SCHEMA for metadata"
}

func (d *MySQLDriver) BuildDSN(conn *models.DataSource) (string, error) {
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

	log.Printf("[mysql] Built DSN for database %s, user %s, host %s:%d", database, username, host, port)
	return dsn, nil
}

func (d *MySQLDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	if conn.Database == nil {
		return nil, fmt.Errorf("database name is required")
	}

	dsn, err := d.BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	redactedDSN := dsn
	if idx := strings.Index(redactedDSN, ":"); idx != -1 {
		if atIdx := strings.Index(redactedDSN, "@"); atIdx != -1 && idx < atIdx {
			redactedDSN = redactedDSN[:idx+1] + "***" + redactedDSN[atIdx:]
		}
	}
	log.Printf("[mysql] Built DSN: %s", redactedDSN)

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
		table, err := getMySQLTableInfo(db, *conn.Database, tableName, config)
		if err != nil {
			continue
		}
		tables = append(tables, *table)
	}

	return &DataSchema{Tables: tables}, nil
}

// getMySQLTableInfo retrieves column information, row count, indexes, and foreign keys for a MySQL table.
func getMySQLTableInfo(db *sql.DB, dbName, tableName string, config *models.DataSourceConfig) (*TableInfo, error) {
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

	var tableComment sql.NullString
	err = db.QueryRow(`
		SELECT TABLE_COMMENT FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
	`, dbName, tableName).Scan(&tableComment)
	if err == nil && tableComment.Valid {
		tableComment.String = strings.TrimSpace(tableComment.String)
	}

	var rowCount int64
	db.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&rowCount)

	var indexes []IndexInfo
	if config != nil && config.IncludeIndexes {
		idxRows, idxErr := db.Query(`
			SELECT INDEX_NAME, NON_UNIQUE, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) as columns
			FROM INFORMATION_SCHEMA.STATISTICS
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			GROUP BY INDEX_NAME, NON_UNIQUE
		`, dbName, tableName)
		if idxErr == nil {
			defer idxRows.Close()
			for idxRows.Next() {
				var idxName string
				var nonUnique uint
				var colStr string
				if scanErr := idxRows.Scan(&idxName, &nonUnique, &colStr); scanErr == nil {
					indexes = append(indexes, IndexInfo{
						Name:     idxName,
						IsUnique: nonUnique == 0,
						Columns:  strings.Split(colStr, ","),
					})
				}
			}
		}
	}

	var foreignKeys []ForeignKeyInfo
	if config != nil && config.IncludeForeignKeys {
		fkRows, fkErr := db.Query(`
			SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME,
				rc.DELETE_RULE, rc.UPDATE_RULE
			FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
			JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
				ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
				AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
			WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
				AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		`, dbName, tableName)
		if fkErr == nil {
			defer fkRows.Close()
			for fkRows.Next() {
				var fkName, fkCol, refTable, refCol sql.NullString
				var onDelete, onUpdate sql.NullString
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