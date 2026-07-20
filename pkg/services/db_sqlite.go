package services

import (
	"database/sql"
	"fmt"

	"YourQL/pkg/models"

	_ "modernc.org/sqlite"
)

func init() {
	RegisterDriver(&SQLiteDriver{})
}

// SQLiteDriver implements DBDriver for SQLite.
type SQLiteDriver struct{}

func (d *SQLiteDriver) TypeKey() string     { return "sqlite" }
func (d *SQLiteDriver) OpenDriver() string  { return "sqlite" }
func (d *SQLiteDriver) DisplayName() string { return "SQLite" }
func (d *SQLiteDriver) DefaultPort() int    { return 0 }
func (d *SQLiteDriver) SQLDialectHint() string {
	return "SQLite — double-quote \"identifier\" quoting, LIMIT for row limits, limited ALTER TABLE support"
}

func (d *SQLiteDriver) BuildDSN(conn *models.DataSource) (string, error) {
	if conn.Database == nil || *conn.Database == "" {
		return "", fmt.Errorf("database path is required for SQLite")
	}
	return *conn.Database + "?_busy_timeout=5000&_journal_mode=WAL", nil
}

func (d *SQLiteDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	if conn.Database == nil || *conn.Database == "" {
		return nil, fmt.Errorf("database path is required for SQLite")
	}

	dsn, err := d.BuildDSN(conn)
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

	return &DataSchema{Tables: tables}, nil
}

// getSQLiteTableInfo retrieves column information for a SQLite table.
func getSQLiteTableInfo(db *sql.DB, tableName string, config *models.DataSourceConfig) (*TableInfo, error) {
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

	var rowCount int64
	err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&rowCount)
	if err != nil {
		rowCount = 0
	}

	var indexes []IndexInfo
	if config != nil && config.IncludeIndexes {
		idxRows, idxErr := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", tableName))
		if idxErr == nil {
			defer idxRows.Close()
			for idxRows.Next() {
				var idxName string
				var seqno, unique, partial int
				if scanErr := idxRows.Scan(&seqno, &idxName, &unique, &partial); scanErr == nil {
					colRows2, err2 := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", idxName))
					if err2 == nil {
						var idxCols []string
						for colRows2.Next() {
							var seqno2 string
							var cid2 string
							if scanErr2 := colRows2.Scan(&seqno2, &cid2, &idxCols); scanErr2 == nil {
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

	var foreignKeys []ForeignKeyInfo
	if config != nil && config.IncludeForeignKeys {
		fkRows, fkErr := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", tableName))
		if fkErr == nil {
			defer fkRows.Close()
			for fkRows.Next() {
				var id, seq int
				var table, from, to, onUpdate, onDelete, match string
				if scanErr := fkRows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); scanErr == nil {
					fk := ForeignKeyInfo{
						Column:    from,
						RefTable:  table,
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
		Name:        tableName,
		Columns:     columns,
		RowCount:    rowCount,
		Indexes:     indexes,
		ForeignKeys: foreignKeys,
	}, nil
}