package services

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"YourQL/pkg/models"

	"github.com/xuri/excelize/v2"
	_ "modernc.org/sqlite"
)

// sanitizeColumnName is kept here for CSV/Excel driver compatibility.
// See pkg/services/sqlite_helpers.go for the shared implementation.
var sanitizeColumnName = func(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return "column"
	}
	n = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, n)
	if len(n) > 0 && n[0] >= '0' && n[0] <= '9' {
		n = "_" + n
	}
	if n == "" {
		n = "column"
	}
	return strings.ToLower(n)
}

func init() {
	RegisterDriver(&CSVFileDriver{})
	RegisterDriver(&ExcelFileDriver{})
}

func filePathStr(conn *models.DataSource) string {
	if conn.FilePath == nil {
		return ""
	}
	return *conn.FilePath
}

// ——— CSV File Driver ———

type CSVFileDriver struct{}

func (d *CSVFileDriver) TypeKey() string    { return "csv_file" }
func (d *CSVFileDriver) OpenDriver() string { return "sqlite" }
func (d *CSVFileDriver) DisplayName() string { return "CSV File" }
func (d *CSVFileDriver) DefaultPort() int   { return 0 }
func (d *CSVFileDriver) SQLDialectHint() string {
	return "SQLite — data loaded from CSV file. The table is named 'data'."
}
func (d *CSVFileDriver) BuildDSN(conn *models.DataSource) (string, error) {
	return "file::memory:?cache=shared", nil
}
func (d *CSVFileDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	return introspectCSV(filePathStr(conn))
}

func (d *CSVFileDriver) PingNative(conn *models.DataSource) error {
	return checkFile(filePathStr(conn))
}
func (d *CSVFileDriver) CloseNative(conn *models.DataSource) error { return nil }
func (d *CSVFileDriver) QueryRowsNative(conn *models.DataSource, query string) ([]string, [][]interface{}, error) {
	return queryCSVFile(filePathStr(conn), query)
}

// ——— Excel File Driver ———

type ExcelFileDriver struct{}

func (d *ExcelFileDriver) TypeKey() string    { return "excel_file" }
func (d *ExcelFileDriver) OpenDriver() string { return "sqlite" }
func (d *ExcelFileDriver) DisplayName() string { return "Excel File" }
func (d *ExcelFileDriver) DefaultPort() int   { return 0 }
func (d *ExcelFileDriver) SQLDialectHint() string {
	return "SQLite — data loaded from Excel file. Each sheet becomes a separate table named after the sheet."
}
func (d *ExcelFileDriver) BuildDSN(conn *models.DataSource) (string, error) {
	return "file::memory:?cache=shared", nil
}
func (d *ExcelFileDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	return introspectExcel(filePathStr(conn))
}

func (d *ExcelFileDriver) PingNative(conn *models.DataSource) error {
	return checkFile(filePathStr(conn))
}
func (d *ExcelFileDriver) CloseNative(conn *models.DataSource) error { return nil }
func (d *ExcelFileDriver) QueryRowsNative(conn *models.DataSource, query string) ([]string, [][]interface{}, error) {
	return queryExcelFile(filePathStr(conn), query)
}

// ——— Implementation ———

func checkFile(path string) error {
	if path == "" {
		return fmt.Errorf("no file specified")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("file not found: %s", path)
	}
	return nil
}

// introspectCSV reads the CSV file and returns a DataSchema describing it.
func introspectCSV(filePath string) (*DataSchema, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	cleanHeaders := make([]string, len(headers))
	for i, h := range headers {
		cleanHeaders[i] = sanitizeColumnName(h)
	}

	// Read a sample of rows to infer types
	const sampleSize = 1000
	rows := make([][]string, 0, sampleSize)
	totalRows := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		totalRows++
		if len(rows) < sampleSize {
			rows = append(rows, record)
		}
	}

	colTypes := inferTypes(cleanHeaders, rows)

	columns := make([]ColumnInfo, len(headers))
	for i, h := range cleanHeaders {
		columns[i] = ColumnInfo{
			Name:       h,
			DataType:   colTypes[i],
			IsNullable: true,
		}
	}

	return &DataSchema{
		Tables: []TableInfo{
			{
				Name:     "data",
				Columns:  columns,
				RowCount: int64(totalRows),
			},
		},
	}, nil
}

// introspectExcel reads all sheets of an Excel file and returns a DataSchema.
func introspectExcel(filePath string) (*DataSchema, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel file: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("Excel file has no sheets")
	}

	var tables []TableInfo
	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil || len(rows) == 0 {
			continue
		}

		headers := rows[0]
		cleanHeaders := make([]string, len(headers))
		for i, h := range headers {
			cleanHeaders[i] = sanitizeColumnName(h)
		}

		dataRows := make([][]string, 0, len(rows)-1)
		for i := 1; i < len(rows); i++ {
			row := rows[i]
			for len(row) < len(headers) {
				row = append(row, "")
			}
			dataRows = append(dataRows, row)
		}

		colTypes := inferTypes(cleanHeaders, dataRows)
		columns := make([]ColumnInfo, len(headers))
		for i, h := range cleanHeaders {
			columns[i] = ColumnInfo{
				Name:       h,
				DataType:   colTypes[i],
				IsNullable: true,
			}
		}

		tableName := sanitizeColumnName(sheet)
		if tableName == "" {
			tableName = "sheet"
		}
		tables = append(tables, TableInfo{
			Name:     tableName,
			Columns:  columns,
			RowCount: int64(len(dataRows)),
		})
	}

	return &DataSchema{Tables: tables}, nil
}

// queryCSVFile loads a CSV into in-memory SQLite and runs the query.
func queryCSVFile(filePath, query string) ([]string, [][]interface{}, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open CSV: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	headers, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	cleanHeaders := make([]string, len(headers))
	for i, h := range headers {
		cleanHeaders[i] = sanitizeColumnName(h)
	}

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	if err := createTable(db, "data", cleanHeaders); err != nil {
		return nil, nil, err
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		_ = insertRow(db, "data", cleanHeaders, record)
	}

	return runQuery(db, query)
}

// queryExcelFile loads an Excel sheet into in-memory SQLite and runs the query.
func queryExcelFile(filePath, query string) ([]string, [][]interface{}, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open Excel: %w", err)
	}
	defer f.Close()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	sheets := f.GetSheetList()
	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil || len(rows) == 0 {
			continue
		}

		headers := rows[0]
		cleanHeaders := make([]string, len(headers))
		for i, h := range headers {
			cleanHeaders[i] = sanitizeColumnName(h)
		}

		tableName := sanitizeColumnName(sheet)
		if tableName == "" {
			tableName = "sheet"
		}

		if err := createTable(db, tableName, cleanHeaders); err != nil {
			continue
		}

		for i := 1; i < len(rows); i++ {
			row := rows[i]
			for len(row) < len(headers) {
				row = append(row, "")
			}
			_ = insertRow(db, tableName, cleanHeaders, row[:len(headers)])
		}
	}

	return runQuery(db, query)
}

// inferColumnType is used by db_google_sheets.go for per-column type inference.
// Defined here to avoid import cycle with sqlite_helpers.go.
var inferColumnType = func(colIndex int, sampleRows [][]interface{}) string {
	intCount, floatCount, boolCount, nonEmpty := 0, 0, 0, 0
	for _, row := range sampleRows {
		if colIndex >= len(row) {
			continue
		}
		val := row[colIndex]
		if val == nil {
			continue
		}
		nonEmpty++
		s := fmt.Sprintf("%v", val)
		if s == "true" || s == "TRUE" || s == "false" || s == "FALSE" {
			boolCount++
			continue
		}
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			intCount++
			continue
		}
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			floatCount++
			continue
		}
	}
	if nonEmpty == 0 {
		return "TEXT"
	}
	if boolCount == nonEmpty {
		return "BOOLEAN"
	} else if intCount == nonEmpty {
		return "INTEGER"
	} else if intCount+floatCount == nonEmpty {
		return "REAL"
	}
	return "TEXT"
}
