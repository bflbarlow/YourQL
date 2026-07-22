package services

import (
	"database/sql"
	"fmt"
	"strings"

	"YourQL/pkg/models"

	"google.golang.org/api/sheets/v4"

	_ "modernc.org/sqlite"
)

func init() {
	RegisterDriver(&GoogleSheetsDriver{})
}

// extractSpreadsheetID extracts the spreadsheet ID from a full Google Sheets URL
// or returns the input unchanged if it's already a bare ID.
func extractSpreadsheetID(raw string) string {
	// If it's already a bare ID (no slashes or dots besides .com), return as-is
	if !strings.Contains(raw, "/") && !strings.HasPrefix(raw, "http") {
		return raw
	}
	// Extract from URL like: https://docs.google.com/spreadsheets/d/{ID}/edit...
	// Find "/d/" and take the next segment up to "/" or end
	idx := strings.Index(raw, "/d/")
	if idx == -1 {
		return raw // not a recognizable URL, use as-is
	}
	rest := raw[idx+3:] // after "/d/"
	if slashIdx := strings.IndexByte(rest, '/'); slashIdx != -1 {
		return rest[:slashIdx]
	}
	// Strip query params if present
	if qIdx := strings.IndexByte(rest, '?'); qIdx != -1 {
		return rest[:qIdx]
	}
	return rest
}

// GoogleSheetsDriver implements DBDriver + NativeQuerier for Google Sheets.
// On each query it fetches all sheet data from the Google Sheets API (using OAuth2 token),
// creates in-memory SQLite tables (one per sheet), executes the SQL query, and returns results.
// No persistent connections, no CGO — pure Go google.golang.org/api/sheets/v4 client.
type GoogleSheetsDriver struct{}

func (d *GoogleSheetsDriver) TypeKey() string    { return "google_sheets" }
func (d *GoogleSheetsDriver) OpenDriver() string { return "sqlite" }
func (d *GoogleSheetsDriver) DisplayName() string { return "Google Sheets" }
func (d *GoogleSheetsDriver) DefaultPort() int    { return 0 }
func (d *GoogleSheetsDriver) SQLDialectHint() string {
	return "SQLite — data loaded from Google Sheets. Each sheet becomes a separate table named after the sheet."
}
func (d *GoogleSheetsDriver) BuildDSN(conn *models.DataSource) (string, error) {
	return "file::memory:?cache=shared", nil
}
func (d *GoogleSheetsDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	return introspectGoogleSheets(conn)
}

// NativeQuerier implementation

func (d *GoogleSheetsDriver) PingNative(conn *models.DataSource) error {
	if conn.FilePath == nil || *conn.FilePath == "" {
		return fmt.Errorf("no spreadsheet ID configured")
	}
	sid := extractSpreadsheetID(*conn.FilePath)
	svc, err := getSheetsClient(conn)
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	// Verify the spreadsheet exists
	_, err = svc.Spreadsheets.Get(sid).Do()
	return err
}

func (d *GoogleSheetsDriver) CloseNative(conn *models.DataSource) error {
	return nil // stateless: no persistent connections
}

func (d *GoogleSheetsDriver) QueryRowsNative(conn *models.DataSource, query string) ([]string, [][]interface{}, error) {
	return queryGoogleSheets(conn, query)
}

// ============================================================
// Schema Introspection
// ============================================================

// introspectGoogleSheets fetches all sheet names and their headers from the Google Sheets API.
func introspectGoogleSheets(conn *models.DataSource) (*DataSchema, error) {
	svc, err := getSheetsClient(conn)
	if err != nil {
		return nil, err
	}

	sid := extractSpreadsheetID(*conn.FilePath)
	spreadsheet, err := svc.Spreadsheets.Get(sid).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spreadsheet: %w", err)
	}

	schema := &DataSchema{Tables: make([]TableInfo, 0)}
	for _, sheet := range spreadsheet.Sheets {
		title := sheet.Properties.Title
		tableName := sanitizeColumnName(title)
		if tableName == "" {
			tableName = "sheet"
		}

		// Fetch up to 3 rows to infer column types. Use unbounded column range so sheets
		// with more than 26 columns aren't silently truncated.
		range_ := fmt.Sprintf("'%s'!1:3", title)
		resp, err := svc.Spreadsheets.Values.Get(sid, range_).Do()
		if err != nil || len(resp.Values) == 0 {
			// Sheet is empty — still include it with no columns
			schema.Tables = append(schema.Tables, TableInfo{
				Name:    tableName,
				Columns: []ColumnInfo{},
			})
			continue
		}

		headers := toStringSlice(resp.Values[0])
		cleanHeaders := make([]string, len(headers))
		for i, h := range headers {
			cleanHeaders[i] = sanitizeColumnName(h)
		}

		// Infer types from rows 2-3 (if available)
		sampleRows := resp.Values[1:]
		columns := make([]ColumnInfo, len(cleanHeaders))
		for i, h := range cleanHeaders {
			colType := inferColumnType(i, sampleRows)
			columns[i] = ColumnInfo{Name: h, DataType: colType, IsNullable: true}
		}

		schema.Tables = append(schema.Tables, TableInfo{
			Name:    tableName,
			Columns: columns,
		})
	}

	return schema, nil
}

// ============================================================
// Query Execution
// ============================================================

// queryGoogleSheets fetches all sheets, loads them into in-memory SQLite, and runs the query.
func queryGoogleSheets(conn *models.DataSource, query string) ([]string, [][]interface{}, error) {
	svc, err := getSheetsClient(conn)
	if err != nil {
		return nil, nil, err
	}

	sid := extractSpreadsheetID(*conn.FilePath)
	spreadsheet, err := svc.Spreadsheets.Get(sid).Do()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch spreadsheet: %w", err)
	}

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	for _, sheet := range spreadsheet.Sheets {
		title := sheet.Properties.Title

		// Use pagination for large sheets (see fetchSheetWithPagination).
		values, err := fetchSheetWithPagination(svc, sid, title)
		if err != nil || len(values) == 0 {
			continue
		}

		headers := toStringSlice(values[0])
		cleanHeaders := make([]string, len(headers))
		for i, h := range headers {
			cleanHeaders[i] = sanitizeColumnName(h)
		}

		tableName := sanitizeColumnName(title)
		if tableName == "" {
			tableName = "sheet"
		}

		if err := createTable(db, tableName, cleanHeaders); err != nil {
			continue
		}

		// Insert data rows. Google Sheets API returns []interface{} per row; convert
		// to []string to match the shared insertRow signature from sqlite_helpers.go.
		for i := 1; i < len(values); i++ {
			row := toStringSlice(values[i])
			// Pad row to match header count
			for len(row) < len(headers) {
				row = append(row, "")
			}
			_ = insertRow(db, tableName, cleanHeaders, row[:len(headers)])
		}
	}

	return runQuery(db, query)
}

// ============================================================
// Pagination for Large Sheets
// ============================================================

// fetchSheetWithPagination fetches a sheet's data with pagination support.
// Returns all rows (including the header row at index 0), splitting across multiple
// API calls if the sheet is large. Uses an unbounded column range so wide sheets
// aren't truncated at column Z.
func fetchSheetWithPagination(svc *sheets.Service, spreadsheetID, sheetTitle string) ([][]interface{}, error) {
	const rowsPerBatch = 10000 // rows per API call (~100K cells assuming ~10 cols)

	var allValues [][]interface{}
	startRow := 1
	for {
		endRow := startRow + rowsPerBatch - 1
		// Unbounded column range: rows only, no column letters.
		range_ := fmt.Sprintf("'%s'!%d:%d", sheetTitle, startRow, endRow)
		resp, err := svc.Spreadsheets.Values.Get(spreadsheetID, range_).Do()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch range %s: %w", range_, err)
		}
		if len(resp.Values) == 0 {
			break // no more data
		}
		allValues = append(allValues, resp.Values...)
		if len(resp.Values) < rowsPerBatch {
			break // last (partial) batch
		}
		startRow += rowsPerBatch
	}

	return allValues, nil
}

// ============================================================
// Helpers
// ============================================================

// toStringSlice converts []interface{} (Google Sheets API row format) to []string.
// nil values become empty strings; all other values use fmt.Sprintf("%v", ...).
func toStringSlice(vals []interface{}) []string {
	result := make([]string, len(vals))
	for i, v := range vals {
		if v == nil {
			result[i] = ""
			continue
		}
		result[i] = fmt.Sprintf("%v", v)
	}
	return result
}
