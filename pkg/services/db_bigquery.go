package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"YourQL/pkg/models"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func init() {
	RegisterDriver(&BigQueryDriver{})
}

// BigQueryDriver implements DBDriver for Google BigQuery.
// It also implements NativeQuerier since BigQuery doesn't use database/sql.
type BigQueryDriver struct{}

func (d *BigQueryDriver) TypeKey() string      { return "bigquery" }
func (d *BigQueryDriver) OpenDriver() string   { return "bigquery" }
func (d *BigQueryDriver) DisplayName() string  { return "BigQuery" }
func (d *BigQueryDriver) DefaultPort() int     { return 443 }
func (d *BigQueryDriver) SQLDialectHint() string {
	return "BigQuery — backtick `identifier` quoting, LIMIT, STRUCT/ARRAY types, STRING not VARCHAR, partition pruning"
}

// BigQueryExtra holds BigQuery-specific connection parameters.
type BigQueryExtra struct {
	ProjectID        string `json:"project_id"`
	Dataset          string `json:"dataset,omitempty"`
	ServiceAccountKey string `json:"service_account_key,omitempty"` // JSON key content
}

func parseBigQueryExtra(conn *models.DBConnection) BigQueryExtra {
	if conn.Extra == nil || *conn.Extra == "" {
		return BigQueryExtra{}
	}
	var extra BigQueryExtra
	if err := json.Unmarshal([]byte(*conn.Extra), &extra); err != nil {
		return BigQueryExtra{}
	}
	return extra
}

func (d *BigQueryDriver) BuildDSN(conn *models.DBConnection) (string, error) {
	// BigQuery doesn't use traditional DSNs. Return a placeholder.
	extra := parseBigQueryExtra(conn)
	log.Printf("[bigquery] BuildDSN for project %s, dataset %s", extra.ProjectID, extra.Dataset)
	return fmt.Sprintf("bigquery://%s/%s", extra.ProjectID, extra.Dataset), nil
}

func (d *BigQueryDriver) getClient(conn *models.DBConnection) (*bigquery.Client, error) {
	extra := parseBigQueryExtra(conn)
	if extra.ProjectID == "" {
		return nil, fmt.Errorf("BigQuery project_id is required (set in extra.project_id)")
	}

	var opts []option.ClientOption
	if extra.ServiceAccountKey != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(extra.ServiceAccountKey)))
	}

	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, extra.ProjectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
	}
	return client, nil
}

func (d *BigQueryDriver) PingNative(conn *models.DBConnection) error {
	client, err := d.getClient(conn)
	if err != nil {
		return err
	}
	defer client.Close()

	extra := parseBigQueryExtra(conn)
	dataset := extra.Dataset
	if dataset == "" {
		dataset = "INFORMATION_SCHEMA"
	}
	ctx := context.Background()
	ds := client.Dataset(dataset)
	if _, err := ds.Metadata(ctx); err != nil {
		return fmt.Errorf("failed to access BigQuery dataset: %w", err)
	}
	return nil
}

func (d *BigQueryDriver) CloseNative(conn *models.DBConnection) error {
	// BigQuery client is created per-query, closed by the caller
	return nil
}

func (d *BigQueryDriver) QueryRowsNative(conn *models.DBConnection, query string) ([]string, [][]interface{}, error) {
	client, err := d.getClient(conn)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	q := client.Query(query)
	q.UseLegacySQL = false
	q.MaxBytesBilled = 1e9 // 1 GB limit

	it, err := q.Read(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("BigQuery query failed: %w", err)
	}

	// Collect columns from the first page
	var columns []string
	var rows [][]interface{}

	for {
		var page [][]bigquery.Value
		err := it.Next(&page)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("BigQuery read failed: %w", err)
		}

		for _, row := range page {
			if columns == nil {
				// Get schema from iterator on first row
				cols := it.Schema
				for _, field := range cols {
					columns = append(columns, field.Name)
				}
			}

			var ifaceRow []interface{}
			for _, val := range row {
				ifaceRow = append(ifaceRow, formatBigQueryValue(val))
			}
			rows = append(rows, ifaceRow)
		}

		if len(rows) >= 50 {
			break // Cap at 50 rows
		}
	}

	if columns == nil {
		columns = []string{}
	}

	return columns, rows, nil
}

func formatBigQueryValue(v bigquery.Value) interface{} {
	switch val := v.(type) {
	case nil:
		return nil
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", val)
	}
}

// GetSchema for BigQuery uses the INFORMATION_SCHEMA views.
func (d *BigQueryDriver) GetSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	extra := parseBigQueryExtra(conn)
	if extra.ProjectID == "" {
		return nil, fmt.Errorf("BigQuery project_id is required")
	}

	dataset := extra.Dataset
	if dataset == "" {
		return nil, fmt.Errorf("BigQuery dataset is required for schema introspection")
	}

	client, err := d.getClient(conn)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	ctx := context.Background()
	config, _ := conn.ParseConfig()

	query := fmt.Sprintf(`
		SELECT table_name
		FROM %s.%s.INFORMATION_SCHEMA.TABLES
		WHERE table_type = 'BASE TABLE'
		ORDER BY table_name
	`, extra.ProjectID, dataset)

	q := client.Query(query)
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}

	var tables []TableInfo
	for {
		var page [][]bigquery.Value
		err := it.Next(&page)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tables: %w", err)
		}

		for _, row := range page {
			if len(row) == 0 {
				continue
			}
			tableName := fmt.Sprintf("%v", row[0])

			table, err := getBigQueryTableInfo(client, extra, tableName, config, ctx)
			if err != nil {
				log.Printf("[bigquery] Warning: failed to introspect table %s: %v", tableName, err)
				continue
			}
			tables = append(tables, *table)
		}
	}

	return &DatabaseSchema{Tables: tables}, nil
}

func getBigQueryTableInfo(client *bigquery.Client, extra BigQueryExtra, tableName string, config *models.DBConnectionConfig, ctx context.Context) (*TableInfo, error) {
	columnsQuery := fmt.Sprintf(`
		SELECT column_name, data_type, is_nullable,
			CASE WHEN column_name IN (
				SELECT column_name
				FROM %s.%s.INFORMATION_SCHEMA.KEY_COLUMN_USAGE
				WHERE table_name = '%s' AND constraint_name LIKE '%%pk%%'
			) THEN true ELSE false END,
			IFNULL(column_default, '')
		FROM %s.%s.INFORMATION_SCHEMA.COLUMNS
		WHERE table_name = '%s'
		ORDER BY ordinal_position
	`, extra.ProjectID, extra.Dataset, tableName, extra.ProjectID, extra.Dataset, tableName)

	q := client.Query(columnsQuery)
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}

	var columns []ColumnInfo
	for {
		var page [][]bigquery.Value
		err := it.Next(&page)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read columns: %w", err)
		}

		for _, row := range page {
			if len(row) < 5 {
				continue
			}
			columns = append(columns, ColumnInfo{
				Name:         fmt.Sprintf("%v", row[0]),
				DataType:     fmt.Sprintf("%v", row[1]),
				IsNullable:   strings.ToUpper(fmt.Sprintf("%v", row[2])) == "YES",
				IsPrimaryKey: fmt.Sprintf("%v", row[3]) == "true",
				DefaultValue: fmt.Sprintf("%v", row[4]),
			})
		}
	}

	// Row count (approximate — BigQuery stores this in meta)
	var rowCount int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM `%s.%s.%s`", extra.ProjectID, extra.Dataset, tableName)
	countQ := client.Query(countQuery)
	countIt, err := countQ.Read(ctx)
	if err == nil {
		for {
			var page [][]bigquery.Value
			err := countIt.Next(&page)
			if err == iterator.Done {
				break
			}
			if err == nil && len(page) > 0 && len(page[0]) > 0 {
				rowCount = bigQueryValueToInt64(page[0][0])
			}
			break
		}
	}

	return &TableInfo{
		Name:     tableName,
		Columns:  columns,
		RowCount: rowCount,
	}, nil
}

func bigQueryValueToInt64(v bigquery.Value) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	default:
		return 0
	}
}