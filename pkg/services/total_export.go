package services

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"YourQL/pkg/models"

	"github.com/xuri/excelize/v2"
)

// ---------------------------------------------------------------------------
// Table metadata
// ---------------------------------------------------------------------------

// tableInfo holds metadata about the tables we export.
type tableInfo struct {
	Name       string // SQL table name (used in queries)
	Sheet      string // Excel sheet name / CSV filename prefix
	Credential bool   // true if this table contains credential columns we may strip
}

// exportTables defines every table in ~/.yourql/yourql.db that we export,
// in the order they appear in the workbook / zip.
var exportTables = []tableInfo{
	{Name: "llm_providers", Sheet: "providers", Credential: true},
	{Name: "data_sources", Sheet: "data_sources", Credential: true},
	{Name: "conversations", Sheet: "conversations", Credential: false},
	{Name: "conversation_messages", Sheet: "messages", Credential: false},
	{Name: "conversation_skills", Sheet: "conversation_skills", Credential: false},
	{Name: "queries", Sheet: "queries", Credential: false},
	{Name: "skills", Sheet: "skills", Credential: false},
	{Name: "app_settings", Sheet: "app_settings", Credential: false},
	{Name: "discussion_defaults", Sheet: "discussion_defaults", Credential: false},
	{Name: "agent_loop_config", Sheet: "agent_loop_config", Credential: false},
	{Name: "schema_migrations", Sheet: "schema_migrations", Credential: false},
}

// credentialColumns lists columns unconditionally stripped from models.DB
// exports when includeCredentials is false.
var credentialColumns = map[string][]string{
	"llm_providers": {"api_key"},
	"data_sources":  {"password", "auth_config", "extra"},
}

// secretKeyPatterns defines JSON key substrings that trigger redaction in
// JSON blob columns (defense-in-depth — see TOTAL_EXPORT.md §3).
var secretKeyPatterns = []string{
	"key", "token", "secret", "password", "credential", "service_account",
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func tableIsSelected(tables []string, name string) bool {
	if len(tables) == 0 {
		return true // empty = all
	}
	for _, t := range tables {
		if t == name {
			return true
		}
	}
	return false
}

// buildColumnList returns the column list for a table. When stripCredentials
// is true, credential columns for that table are omitted.
func buildColumnList(name string, stripCredentials bool) ([]string, error) {
	rows, err := models.DB.Query(fmt.Sprintf("SELECT * FROM %s LIMIT 1", name))
	if err != nil {
		return nil, fmt.Errorf("failed to introspect table %s: %w", name, err)
	}
	defer rows.Close()

	allCols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns for %s: %w", name, err)
	}

	if !stripCredentials {
		return allCols, nil
	}

	stripped := credentialColumns[name]
	skip := make(map[string]bool, len(stripped))
	for _, c := range stripped {
		skip[c] = true
	}

	var cols []string
	for _, c := range allCols {
		if !skip[c] {
			cols = append(cols, c)
		}
	}
	return cols, nil
}

// scanColumns scans a single row into a slice of interface{} using the
// provided column names.
func scanColumns(rows *sql.Rows, colNames []string) ([]interface{}, error) {
	vals := make([]interface{}, len(colNames))
	ptrs := make([]interface{}, len(colNames))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return vals, nil
}

// cellValue converts a scanned database value to its string representation.
func cellValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		// SQLite stores booleans as 0/1 (int64), so we rarely see float64,
		// but handle it just in case.
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%v", t), "0"), ".")
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", t)
	}
}

// redactSecretKeys scans a JSON blob and replaces any key whose name matches
// a secret pattern with "[REDACTED]". Returns the (possibly modified) JSON
// string and true if anything was redacted.
func redactSecretKeys(raw string) (string, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw, false // not valid JSON — leave as-is
	}
	redacted := false
	walkAndRedact(m, &redacted)
	if !redacted {
		return raw, false
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw, false
	}
	return string(out), true
}

func walkAndRedact(m map[string]interface{}, redacted *bool) {
	for key, val := range m {
		lower := strings.ToLower(key)
		for _, pat := range secretKeyPatterns {
			if strings.Contains(lower, pat) {
				// Only redact string values — don't touch nested objects/arrays,
				// since we can't know if they represent secrets without walking them.
				if _, ok := val.(string); ok {
					m[key] = "[REDACTED]"
					*redacted = true
				}
				break
			}
		}
	}
}

// valueWithRedaction returns the cell string for v, applying JSON-key
// redaction if stripCredentials is true and this column might contain a JSON
// blob (config, extra, etc.). colName is the column name for context.
func valueWithRedaction(v interface{}, colName string, stripCredentials bool) string {
	s := cellValue(v)
	if !stripCredentials || s == "" {
		return s
	}
	// Only attempt redaction on columns whose name suggests they hold JSON.
	lowerCol := strings.ToLower(colName)
	if lowerCol == "config" || lowerCol == "extra" || lowerCol == "auth_config" ||
		lowerCol == "metadata" || lowerCol == "tool_transcript" || lowerCol == "sql_results" {
		if redacted, changed := redactSecretKeys(s); changed {
			return redacted
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// ExportDatabaseXLSX
// ---------------------------------------------------------------------------

// ExportDatabaseXLSX writes the full yourql.db to an Excel workbook at path.
func ExportDatabaseXLSX(path string, includeCredentials bool) error {
	f := excelize.NewFile()
	defer f.Close()

	// Remove the default Sheet1 — we'll add named sheets ourselves.
	defaultSheet := f.GetSheetName(0)
	f.SetSheetName(defaultSheet, exportTables[0].Sheet)

	sheetIdx := 0
	var totalRows int

	for _, t := range exportTables {
		if sheetIdx > 0 {
			idx, err := f.NewSheet(t.Sheet)
			if err != nil {
				return fmt.Errorf("failed to create sheet %s: %w", t.Sheet, err)
			}
			_ = idx
		}

		cols, err := buildColumnList(t.Name, !includeCredentials)
		if err != nil {
			return err
		}

		// Write header row.
		headerRow := make([]interface{}, len(cols))
		for i, c := range cols {
			headerRow[i] = c
		}
		if err := f.SetSheetRow(t.Sheet, "A1", &headerRow); err != nil {
			return fmt.Errorf("failed to write header for %s: %w", t.Sheet, err)
		}

		// Write data rows.
		rows, err := models.DB.Query(fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), t.Name))
		if err != nil {
			return fmt.Errorf("failed to query %s: %w", t.Name, err)
		}

		rowNum := 2
		for rows.Next() {
			vals, err := scanColumns(rows, cols)
			if err != nil {
				_ = rows.Close()
				return fmt.Errorf("failed to scan row from %s: %w", t.Name, err)
			}
			rowSlice := make([]interface{}, len(vals))
			for i, v := range vals {
				rowSlice[i] = valueWithRedaction(v, cols[i], !includeCredentials)
			}
			cellRef := fmt.Sprintf("A%d", rowNum)
			if err := f.SetSheetRow(t.Sheet, cellRef, &rowSlice); err != nil {
				_ = rows.Close()
				return fmt.Errorf("failed to write row %d for %s: %w", rowNum, t.Sheet, err)
			}
			rowNum++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("error iterating %s: %w", t.Name, err)
		}
		rows.Close()

		totalRows += rowNum - 2
		sheetIdx++
	}

	// Write export_info metadata sheet.
	infoSheet, _ := f.NewSheet("export_info")
	infoRows := [][]interface{}{
		{"exported_at", time.Now().Format(time.RFC3339)},
		{"app_version", appVersion()},
		{"db_tables", fmt.Sprintf("%d", len(exportTables))},
		{"db_rows_total", fmt.Sprintf("%d", totalRows)},
		{"credentials_included", fmt.Sprintf("%t", includeCredentials)},
	}
	for i, row := range infoRows {
		cellRef := fmt.Sprintf("A%d", i+1)
		if err := f.SetSheetRow("export_info", cellRef, &row); err != nil {
			return fmt.Errorf("failed to write export_info: %w", err)
		}
	}
	_ = infoSheet

	// Write to a temp file, then rename atomically. We use .xlsx extension
	// because excelize's SaveAs validates the file extension and rejects
	// anything it doesn't recognise (including .tmp).
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "yourql-export-*.xlsx")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()

	if err := f.SaveAs(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write Excel file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize export: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// ExportDatabaseCSV
// ---------------------------------------------------------------------------

// ExportDatabaseCSV writes the selected tables from yourql.db into a single
// .zip file at zipPath. If tables is empty, all tables are exported.
func ExportDatabaseCSV(zipPath string, tables []string, includeCredentials bool) error {
	// Build an in-memory zip.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	var totalRows int
	var exported []string
	var skipped []string

	for _, t := range exportTables {
		if !tableIsSelected(tables, t.Name) {
			skipped = append(skipped, t.Name)
			continue
		}
		exported = append(exported, t.Name)

		cols, err := buildColumnList(t.Name, !includeCredentials)
		if err != nil {
			return err
		}

		fh := &zip.FileHeader{
			Name:   t.Name + ".csv",
			Method: zip.Deflate,
		}
		fh.SetModTime(time.Now())
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return fmt.Errorf("failed to create zip entry for %s: %w", t.Name, err)
		}

		// CSV with UTF-8 BOM so Excel on Windows reads it correctly.
		w.Write([]byte{0xEF, 0xBB, 0xBF})

		cw := csv.NewWriter(w)

		if err := cw.Write(cols); err != nil {
			return fmt.Errorf("failed to write CSV header for %s: %w", t.Name, err)
		}

		rows, err := models.DB.Query(fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), t.Name))
		if err != nil {
			return fmt.Errorf("failed to query %s: %w", t.Name, err)
		}

		rowCount := 0
		for rows.Next() {
			vals, err := scanColumns(rows, cols)
			if err != nil {
				_ = rows.Close()
				return fmt.Errorf("failed to scan row from %s: %w", t.Name, err)
			}
			rowSlice := make([]string, len(vals))
			for i, v := range vals {
				rowSlice[i] = valueWithRedaction(v, cols[i], !includeCredentials)
			}
			if err := cw.Write(rowSlice); err != nil {
				_ = rows.Close()
				return fmt.Errorf("failed to write CSV row for %s: %w", t.Name, err)
			}
			rowCount++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("error iterating %s: %w", t.Name, err)
		}
		rows.Close()
		cw.Flush()
		if err := cw.Error(); err != nil {
			return fmt.Errorf("CSV writer error for %s: %w", t.Name, err)
		}
		totalRows += rowCount
	}

	// Write README.txt with export metadata.
	readmeHeader := &zip.FileHeader{
		Name:   "README.txt",
		Method: zip.Deflate,
	}
	readmeHeader.SetModTime(time.Now())
	rw, err := zw.CreateHeader(readmeHeader)
	if err != nil {
		return fmt.Errorf("failed to create README.txt in zip: %w", err)
	}

	readme := fmt.Sprintf("YourQL Database Export\n")
	readme += fmt.Sprintf("========================\n")
	readme += fmt.Sprintf("exported_at:           %s\n", time.Now().Format(time.RFC3339))
	readme += fmt.Sprintf("app_version:           %s\n", appVersion())
	readme += fmt.Sprintf("credentials_included:  %t\n", includeCredentials)
	readme += fmt.Sprintf("tables_exported:       %d\n", len(exported))
	readme += fmt.Sprintf("tables_skipped:        %d\n", len(skipped))
	readme += fmt.Sprintf("total_data_rows:       %d\n", totalRows)
	if len(exported) > 0 {
		readme += fmt.Sprintf("exported_tables:       %s\n", strings.Join(exported, ", "))
	}
	if len(skipped) > 0 {
		readme += fmt.Sprintf("skipped_tables:        %s\n", strings.Join(skipped, ", "))
	}
	if _, err := rw.Write([]byte(readme)); err != nil {
		return fmt.Errorf("failed to write README.txt: %w", err)
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to finalize zip: %w", err)
	}

	// Write the zip to disk — single write to the final path since the whole
	// thing was built in memory.
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write zip file: %w", err)
	}
	return nil
}

// appVersion returns the running app version, as injected at build time.
// We can't import main.appVersion from services, so we read it from
// the GeneralSettings struct that the app layer provides. This is a
// fallback — the binding in app.go overrides it with the real value.
var appVersion func() string = func() string { return "unknown" }

// SetAppVersion allows app.go to inject the real appVersion for export
// metadata. Call this from startup after models.ConnectDatabase().
func SetAppVersionGetter(fn func() string) {
	appVersion = fn
}
