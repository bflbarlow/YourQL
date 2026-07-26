package services

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// createTable creates an in-memory SQLite table with TEXT columns.
func createTable(db *sql.DB, name string, columns []string) error {
	cols := make([]string, len(columns))
	for i, c := range columns {
		cols[i] = fmt.Sprintf("\"%s\" TEXT", c)
	}
	sqlStmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS \"%s\" (%s)", name, strings.Join(cols, ", "))
	_, err := db.Exec(sqlStmt)
	return err
}

// insertRow inserts a single row into the table.
// columns: cleaned column names (for the INSERT column list)
// values: values to insert (must be length == len(columns))
func insertRow(db *sql.DB, table string, columns, values []string) error {
	quotedCols := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	args := make([]interface{}, len(columns))
	for i, c := range columns {
		quotedCols[i] = fmt.Sprintf("\"%s\"", c)
		placeholders[i] = "?"
		v := ""
		if i < len(values) {
			v = values[i]
		}
		args[i] = v
	}
	sqlStmt := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s)",
		table,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "))
	_, err := db.Exec(sqlStmt, args...)
	return err
}

// runQuery executes a SQL query and returns columns and rows.
func runQuery(db *sql.DB, query string) ([]string, [][]interface{}, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var results [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		results = append(results, values)
	}

	return cols, results, nil
}

// inferTypes infers column types from sample data rows.
// All columns default to TEXT; if every non-empty value in a column is
// consistently numeric/boolean, the type is narrowed.
func inferTypes(headers []string, rows [][]string) []string {
	types := make([]string, len(headers))
	for i := range types {
		types[i] = "TEXT"
	}
	if len(rows) == 0 {
		return types
	}
	for col := 0; col < len(headers); col++ {
		intCount, floatCount, boolCount := 0, 0, 0
		nonEmpty := 0
		for _, row := range rows {
			if col >= len(row) || strings.TrimSpace(row[col]) == "" {
				continue
			}
			nonEmpty++
			val := strings.TrimSpace(row[col])
			if val == "true" || val == "TRUE" || val == "false" || val == "FALSE" {
				boolCount++
				continue
			}
			if _, err := strconv.ParseInt(val, 10, 64); err == nil {
				intCount++
				continue
			}
			if _, err := strconv.ParseFloat(val, 64); err == nil {
				floatCount++
				continue
			}
		}
		if nonEmpty == 0 {
			continue
		}
		if boolCount == nonEmpty {
			types[col] = "BOOLEAN"
		} else if intCount == nonEmpty {
			types[col] = "INTEGER"
		} else if intCount+floatCount == nonEmpty {
			types[col] = "REAL"
		}
	}
	return types
}


