package services

import (
	"fmt"

	"YourQL/pkg/models"
)

// DatabaseSchema represents the schema of a database.
type DatabaseSchema struct {
	Tables []TableInfo `json:"tables"`
}

// TableInfo represents a table in the database.
type TableInfo struct {
	Name        string           `json:"name"`
	Columns     []ColumnInfo     `json:"columns"`
	RowCount    int64            `json:"row_count,omitempty"`
	Description string           `json:"description,omitempty"`
	Indexes     []IndexInfo      `json:"indexes,omitempty"`
	ForeignKeys []ForeignKeyInfo `json:"foreign_keys,omitempty"`
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
	Name     string   `json:"name"`
	IsUnique bool     `json:"is_unique"`
	Columns  []string `json:"columns"`
}

// ForeignKeyInfo represents a foreign key constraint on a table.
type ForeignKeyInfo struct {
	Name      string `json:"name"`
	Column    string `json:"column"`
	RefTable  string `json:"ref_table"`
	RefColumn string `json:"ref_column"`
	OnDelete  string `json:"on_delete,omitempty"`
	OnUpdate  string `json:"on_update,omitempty"`
}

// GetDatabaseSchema introspects the database connected via the given DBConnection
// and returns its schema.
func GetDatabaseSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	driver, err := GetDriver(conn.Type)
	if err != nil {
		return nil, fmt.Errorf("unsupported database type: %s", conn.Type)
	}
	return driver.GetSchema(conn)
}