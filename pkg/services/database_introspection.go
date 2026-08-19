package services

import (
	"fmt"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// DataSchema types — canonical definitions now in pkg/engine.
type DataSchema = engine.DataSchema
type TableInfo = engine.TableInfo
type ColumnInfo = engine.ColumnInfo
type IndexInfo = engine.IndexInfo
type ForeignKeyInfo = engine.ForeignKeyInfo

// GetDataSchema introspects the data source connected via the given DataSource
// and returns its schema.
func GetDataSchema(conn *models.DataSource) (*DataSchema, error) {
	driver, err := GetDriver(conn.Type)
	if err != nil {
		return nil, fmt.Errorf("unsupported database type: %s", conn.Type)
	}
	return driver.GetSchema(conn)
}