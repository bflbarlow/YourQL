package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"YourQL/pkg/models"

	_ "github.com/microsoft/go-mssqldb"
)

func init() {
	RegisterDriver(&SQLServerDriver{})
}

// SQLServerDriver implements DBDriver for Microsoft SQL Server.
type SQLServerDriver struct{}

func (d *SQLServerDriver) TypeKey() string      { return "sqlserver" }
func (d *SQLServerDriver) OpenDriver() string   { return "sqlserver" }
func (d *SQLServerDriver) DisplayName() string  { return "SQL Server" }
func (d *SQLServerDriver) DefaultPort() int     { return 1433 }
func (d *SQLServerDriver) SQLDialectHint() string {
	return "SQL Server — bracket [identifier] quoting, TOP N instead of LIMIT, GETDATE() for current time"
}

// SQLServerExtra holds SQL Server-specific parameters.
type SQLServerExtra struct {
	Encrypt                bool   `json:"encrypt,omitempty"`
	TrustServerCertificate bool   `json:"trust_server_certificate,omitempty"`
	Instance               string `json:"instance,omitempty"`
}

func parseSQLServerExtra(conn *models.DBConnection) SQLServerExtra {
	if conn.Extra == nil || *conn.Extra == "" {
		return SQLServerExtra{Encrypt: true}
	}
	var extra SQLServerExtra
	if err := json.Unmarshal([]byte(*conn.Extra), &extra); err != nil {
		return SQLServerExtra{Encrypt: true}
	}
	return extra
}

func (d *SQLServerDriver) BuildDSN(conn *models.DBConnection) (string, error) {
	if conn.Database == nil {
		return "", fmt.Errorf("database name is required")
	}

	host := "localhost"
	if conn.Host != nil && *conn.Host != "" {
		host = *conn.Host
	}

	port := 1433
	if conn.Port != nil {
		port = *conn.Port
	}

	username := ""
	if conn.Username != nil {
		username = *conn.Username
	}

	password := ""
	if conn.Password != nil {
		password = *conn.Password
	}

	extra := parseSQLServerExtra(conn)

	u := url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
	}

	q := u.Query()
	q.Set("database", *conn.Database)

	if !extra.Encrypt {
		q.Set("encrypt", "false")
	}
	if extra.TrustServerCertificate {
		q.Set("trustservercertificate", "true")
	}
	if extra.Instance != "" {
		q.Set("instance", extra.Instance)
	}

	u.RawQuery = q.Encode()

	log.Printf("[sqlserver] Built DSN for database %s, user %s, host %s:%d", *conn.Database, username, host, port)
	return u.String(), nil
}

func (d *SQLServerDriver) GetSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	if conn.Database == nil {
		return nil, fmt.Errorf("database name is required")
	}

	dsn, err := d.BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	config, _ := conn.ParseConfig()

	rows, err := db.Query(`
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
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
		table, err := getSQLServerTableInfo(db, tableName, config)
		if err != nil {
			continue
		}
		tables = append(tables, *table)
	}

	return &DatabaseSchema{Tables: tables}, nil
}

func getSQLServerTableInfo(db *sql.DB, tableName string, config *models.DBConnectionConfig) (*TableInfo, error) {
	colRows, err := db.Query(`
		SELECT
			c.COLUMN_NAME,
			c.DATA_TYPE,
			CASE c.IS_NULLABLE WHEN 'YES' THEN 'YES' ELSE 'NO' END,
			CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END,
			c.COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN (
			SELECT ku.TABLE_NAME, ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
				ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		) pk ON pk.TABLE_NAME = c.TABLE_NAME AND pk.COLUMN_NAME = c.COLUMN_NAME
		WHERE c.TABLE_NAME = @p1
		ORDER BY c.ORDINAL_POSITION
	`, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer colRows.Close()

	var columns []ColumnInfo
	for colRows.Next() {
		var colName, dataType, nullable string
		var isPK int
		var defVal sql.NullString
		if err := colRows.Scan(&colName, &dataType, &nullable, &isPK, &defVal); err != nil {
			continue
		}
		columns = append(columns, ColumnInfo{
			Name:         colName,
			DataType:     dataType,
			IsNullable:   nullable == "YES",
			IsPrimaryKey: isPK != 0,
			DefaultValue: defVal.String,
		})
	}

	// Table description from extended properties (if any)
	var tableComment sql.NullString
	err = db.QueryRow(`
		SELECT CAST(ep.value AS NVARCHAR(MAX))
		FROM sys.extended_properties ep
		JOIN sys.tables t ON ep.major_id = t.object_id
		WHERE ep.name = 'MS_Description' AND t.name = @p1
	`, tableName).Scan(&tableComment)
	if err != nil || !tableComment.Valid {
		tableComment.String = ""
	}

	var rowCount int64
	db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM [%s]", tableName)).Scan(&rowCount)

	// Indexes
	var indexes []IndexInfo
	if config != nil && config.IncludeIndexes {
		idxRows, idxErr := db.Query(`
			SELECT i.name, i.is_unique
			FROM sys.indexes i
			JOIN sys.tables t ON i.object_id = t.object_id
			WHERE t.name = @p1 AND i.name IS NOT NULL
		`, tableName)
		if idxErr == nil {
			defer idxRows.Close()
			for idxRows.Next() {
				var idxName string
				var isUnique bool
				if scanErr := idxRows.Scan(&idxName, &isUnique); scanErr == nil {
					indexes = append(indexes, IndexInfo{
						Name:     idxName,
						IsUnique: isUnique,
					})
				}
			}
		}
	}

	// Foreign keys
	var foreignKeys []ForeignKeyInfo
	if config != nil && config.IncludeForeignKeys {
		fkRows, fkErr := db.Query(`
			SELECT
				fk.name AS constraint_name,
				pc.name AS column_name,
				rt.name AS ref_table,
				rc.name AS ref_column,
				fk.delete_referential_action_desc,
				fk.update_referential_action_desc
			FROM sys.foreign_keys fk
			JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id
			JOIN sys.tables pt ON fk.parent_object_id = pt.object_id
			JOIN sys.columns pc ON fkc.parent_object_id = pc.object_id AND fkc.parent_column_id = pc.column_id
			JOIN sys.tables rt ON fk.referenced_object_id = rt.object_id
			JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id
			WHERE pt.name = @p1
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
		Description: strings.TrimSpace(tableComment.String),
		Indexes:     indexes,
		ForeignKeys: foreignKeys,
	}, nil
}