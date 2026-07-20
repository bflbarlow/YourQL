# Google Sheets Enhancement

Connect to Google Sheets spreadsheets and query them with SQL just like a database.
Each sheet becomes a table, the first row is column headers, and the data loads into
in-memory SQLite on every query (same architecture as CSV / Excel file drivers).

---

## Architecture Overview

```
┌──────────────┐     OAuth2 Device Flow      ┌───────────────┐
│  YourQL App  │ ◄──────────────────────────► │ Google OAuth2  │
│  (desktop)   │   user_code + polling       │   Endpoint     │
└──────┬───────┘                              └───────┬───────┘
       │  access + refresh tokens                     │
       ▼                                              │
┌──────────────┐     Sheets API v4                    │
│ GoogleSheets │ ◄──────────────────────────────────  │
│   Driver     │   GET spreadsheets/{id}/values/      │
└──────┬───────┘                                      │
       │  sheet data (rows + headers)                 │
       ▼                                              │
┌──────────────┐                                      │
│  In-memory   │                                      │
│   SQLite     │                                      │
└──────────────┘
```

The driver implements `NativeQuerier` (like CSV/Excel drivers). On each query it:
1. Fetches all sheet data from the Google Sheets API (using OAuth2 token)
2. Creates in-memory SQLite tables (one per sheet)
3. Executes the SQL query
4. Returns results and closes the DB

No persistent connections, no CGO — pure Go `google.golang.org/api/sheets/v4` client.

---

## 1. Dependencies

Add to `go.mod`:

```
golang.org/x/oauth2           (pure Go, no CGO)
google.golang.org/api/sheets/v4  (pure Go, no CGO)
```

Both are pure Go and compile on all 4 platforms (macOS arm64/amd64, Linux amd64, Windows amd64).

---

## 2. OAuth2 Device Flow — New File: `pkg/services/google_auth.go`

This file handles the entire OAuth2 lifecycle:

```go
package services

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"

    "golang.org/x/oauth2"
    "golang.org/x/oauth2/google"
    "google.golang.org/api/sheets/v4"
)

// Default Google OAuth2 client ID (bundled with the app).
// Users can override via YOURQL_GOOGLE_CLIENT_ID / YOURQL_GOOGLE_CLIENT_SECRET env vars.
const defaultGoogleClientID = "YOUR_CLIENT_ID.apps.googleusercontent.com"
const defaultGoogleClientSecret = "GOCSPX-YOUR_CLIENT_SECRET"

// Google OAuth2 scopes — read-only access to spreadsheets.
var googleSheetsScopes = []string{
    "https://www.googleapis.com/auth/spreadsheets.readonly",
}

// googleOAuthConfig builds the OAuth2 config using bundled or env-overridden credentials.
func googleOAuthConfig() *oauth2.Config {
    clientID := defaultGoogleClientID
    clientSecret := defaultGoogleClientSecret
    if v := os.Getenv("YOURQL_GOOGLE_CLIENT_ID"); v != "" {
        clientID = v
    }
    if v := os.Getenv("YOURQL_GOOGLE_CLIENT_SECRET"); v != "" {
        clientSecret = v
    }
    return &oauth2.Config{
        ClientID:     clientID,
        ClientSecret: clientSecret,
        Endpoint:     google.Endpoint,
        Scopes:       googleSheetsScopes,
    }
}
```

### 2.1 Device Auth Request

```go
// DeviceAuthResponse is the response from Google's device code endpoint.
type DeviceAuthResponse struct {
    DeviceCode      string `json:"device_code"`
    UserCode        string `json:"user_code"`
    VerificationURL string `json:"verification_url"`
    ExpiresIn       int    `json:"expires_in"`
    Interval        int    `json:"interval"`
}

// RequestDeviceCode initiates the device authorization flow.
// Returns a user_code the user enters at google.com/device, and a DeviceAuthResponse
// the frontend can poll with.
func RequestDeviceCode() (*DeviceAuthResponse, error) {
    cfg := googleOAuthConfig()
    data := fmt.Sprintf(
        "client_id=%s&scope=%s",
        cfg.ClientID,
        strings.Join(cfg.Scopes, " "),
    )

    resp, err := http.Post(
        "https://oauth2.googleapis.com/device/code",
        "application/x-www-form-urlencoded",
        strings.NewReader(data),
    )
    if err != nil {
        return nil, fmt.Errorf("device code request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        var errResp struct{ Error, ErrorDescription string }
        json.NewDecoder(resp.Body).Decode(&errResp)
        return nil, fmt.Errorf("device code error: %s — %s", errResp.Error, errResp.ErrorDescription)
    }

    var result DeviceAuthResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("failed to decode device auth response: %w", err)
    }
    return &result, nil
}
```

### 2.2 Token Exchange

```go
// ExchangeDeviceCode polls Google's token endpoint until the user authorizes or the code expires.
func ExchangeDeviceCode(deviceCode string) (*oauth2.Token, error) {
    cfg := googleOAuthConfig()
    for {
        tok, err := cfg.Exchange(context.Background(), deviceCode,
            oauth2.SetAuthURLParam("grant_type", "urn:ietf:params:oauth:grant-type:device_code"),
        )
        if err == nil {
            return tok, nil
        }

        // "authorization_pending" means the user hasn't authorized yet — keep polling
        if strings.Contains(err.Error(), "authorization_pending") {
            time.Sleep(5 * time.Second)
            continue
        }

        // "slow_down" means we're polling too fast
        if strings.Contains(err.Error(), "slow_down") {
            time.Sleep(10 * time.Second)
            continue
        }

        return nil, fmt.Errorf("token exchange failed: %w", err)
    }
}
```

### 2.3 Token Storage & Refresh

```go
// storeAuthConfig serializes an OAuth2 token into the data source's auth_config column.
func storeAuthConfig(connID uint, token *oauth2.Token) error {
    data, err := json.Marshal(token)
    if err != nil {
        return err
    }
    _, err = models.DB.Exec(
        "UPDATE data_sources SET auth_config = ? WHERE id = ?",
        string(data), connID,
    )
    return err
}

// loadAuthConfig deserializes the OAuth2 token from the data source's auth_config column.
func loadAuthConfig(connID uint) (*oauth2.Token, error) {
    var raw sql.NullString
    err := models.DB.QueryRow(
        "SELECT auth_config FROM data_sources WHERE id = ?", connID,
    ).Scan(&raw)
    if err != nil || !raw.Valid || raw.String == "" {
        return nil, fmt.Errorf("no auth config for data source %d", connID)
    }
    var tok oauth2.Token
    if err := json.Unmarshal([]byte(raw.String), &tok); err != nil {
        return nil, fmt.Errorf("failed to parse auth config: %w", err)
    }
    return &tok, nil
}

// getSheetsClient returns an authenticated Google Sheets API client for a data source.
func getSheetsClient(conn *models.DataSource) (*sheets.Service, error) {
    tok, err := loadAuthConfig(conn.ID)
    if err != nil {
        return nil, err
    }
    cfg := googleOAuthConfig()
    client := cfg.Client(context.Background(), tok)
    return sheets.New(client)
}
```

---

## 3. Google Sheets Driver — New File: `pkg/services/db_google_sheets.go`

```go
package services

import (
    "database/sql"
    "fmt"
    "regexp"
    "strings"

    "YourQL/pkg/models"
    _ "modernc.org/sqlite"
)

func init() {
    RegisterDriver(&GoogleSheetsDriver{})
}

type GoogleSheetsDriver struct{}

func (d *GoogleSheetsDriver) TypeKey() string     { return "google_sheets" }
func (d *GoogleSheetsDriver) OpenDriver() string  { return "sqlite" }
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
    svc, err := getSheetsClient(conn)
    if err != nil {
        return fmt.Errorf("auth failed: %w", err)
    }
    // Verify the spreadsheet exists
    _, err = svc.Spreadsheets.Get(*conn.FilePath).Do()
    return err
}

func (d *GoogleSheetsDriver) CloseNative(conn *models.DataSource) error {
    return nil // stateless: no persistent connections
}

func (d *GoogleSheetsDriver) QueryRowsNative(conn *models.DataSource, query string) ([]string, [][]interface{}, error) {
    return queryGoogleSheets(conn, query)
}
```

### 3.1 Schema Introspection

```go
// introspectGoogleSheets fetches all sheet names and their headers from the Google Sheets API.
func introspectGoogleSheets(conn *models.DataSource) (*DataSchema, error) {
    svc, err := getSheetsClient(conn)
    if err != nil {
        return nil, err
    }

    spreadsheet, err := svc.Spreadsheets.Get(*conn.FilePath).Do()
    if err != nil {
        return nil, fmt.Errorf("failed to fetch spreadsheet: %w", err)
    }

    schema := &DataSchema{Tables: make([]TableSchema, 0)}
    for _, sheet := range spreadsheet.Sheets {
        title := sheet.Properties.Title
        tableName := sanitizeColumnName(title)
        if tableName == "" {
            tableName = "sheet"
        }

        // Fetch up to 3 rows to infer column types
        range_ := fmt.Sprintf("'%s'!A1:Z3", title)
        resp, err := svc.Spreadsheets.Values.Get(*conn.FilePath, range_).Do()
        if err != nil || len(resp.Values) == 0 {
            // Sheet is empty — still include it with no columns
            schema.Tables = append(schema.Tables, TableSchema{
                Name:    tableName,
                Columns: []ColumnSchema{},
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
        columns := make([]ColumnSchema, len(cleanHeaders))
        for i, h := range cleanHeaders {
            colType := inferColumnType(i, sampleRows)
            columns[i] = ColumnSchema{Name: h, Type: colType}
        }

        schema.Tables = append(schema.Tables, TableSchema{
            Name:    tableName,
            Columns: columns,
        })
    }

    return schema, nil
}
```

### 3.2 Query Execution

```go
// queryGoogleSheets fetches all sheets, loads them into in-memory SQLite, and runs the query.
func queryGoogleSheets(conn *models.DataSource, query string) ([]string, [][]interface{}, error) {
    svc, err := getSheetsClient(conn)
    if err != nil {
        return nil, nil, err
    }

    spreadsheet, err := svc.Spreadsheets.Get(*conn.FilePath).Do()
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
        range_ := fmt.Sprintf("'%s'", title) // fetch entire sheet
        resp, err := svc.Spreadsheets.Values.Get(*conn.FilePath, range_).Do()
        if err != nil || len(resp.Values) == 0 {
            continue
        }

        headers := toStringSlice(resp.Values[0])
        cleanHeaders := make([]string, len(headers))
        for i, h := range headers {
            cleanHeaders[i] = sanitizeColumnName(h)
        }

        tableName := sanitizeColumnName(title)
        if tableName == "" {
            tableName = "sheet"
        }

        if err := createTableGS(db, tableName, cleanHeaders); err != nil {
            continue
        }

        // Insert data rows
        for i := 1; i < len(resp.Values); i++ {
            row := resp.Values[i]
            // Pad row to match header count
            for len(row) < len(headers) {
                row = append(row, "")
            }
            _ = insertRowGS(db, tableName, cleanHeaders, row[:len(headers)])
        }
    }

    return runQuery(db, query)
}
```

### 3.3 Helpers

```go
// createTableGS creates an in-memory SQLite table with TEXT columns.
func createTableGS(db *sql.DB, name string, columns []string) error {
    cols := make([]string, len(columns))
    for i, c := range columns {
        cols[i] = fmt.Sprintf("\"%s\" TEXT", c)
    }
    sqlStmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS \"%s\" (%s)", name, strings.Join(cols, ", "))
    _, err := db.Exec(sqlStmt)
    return err
}

// insertRowGS inserts a single row into the table.
func insertRowGS(db *sql.DB, table string, columns []string, values []interface{}) error {
    quotedCols := make([]string, len(columns))
    placeholders := make([]string, len(columns))
    for i, c := range columns {
        quotedCols[i] = fmt.Sprintf("\"%s\"", c)
        placeholders[i] = "?"
    }
    sqlStmt := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s)",
        table, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))
    _, err := db.Exec(sqlStmt, values...)
    return err
}

// toStringSlice converts []interface{} to []string.
func toStringSlice(vals []interface{}) []string {
    result := make([]string, len(vals))
    for i, v := range vals {
        result[i] = fmt.Sprintf("%v", v)
    }
    return result
}
```

**Reuse pattern**: The `createTable`, `insertRow`, `runQuery`, `sanitizeColumnName`, and `inferColumnType` functions already exist in `data_file.go`. They should be extracted to a shared `pkg/services/sqlite_helpers.go` file and used by both CSV/Excel and Google Sheets drivers.

---

## 4. Data Source Model Changes

### 4.1 New Field: `AuthConfig`

Add to `pkg/models/db_connection.go` `DataSource` struct:

```go
AuthConfig *string `json:"-"` // OAuth2 token JSON (never sent to frontend)
```

The `json:"-"` tag ensures auth tokens are never serialized in API responses.

### 4.2 Database Migration

In `pkg/models/database.go`, add a migration:

```go
{Name: "add_auth_config_column", SQL: "ALTER TABLE data_sources ADD COLUMN auth_config TEXT"},
```

### 4.3 Read/Write in `db_connection.go`

Update `GetDataSourceByID` to scan `auth_config`:

```go
// In the Scan() call, add &c.AuthConfig after the last scanned field
```

Update `CreateDataSource` — no change needed (auth_config is NULL on creation, populated by OAuth flow later).

Update `ListDataSourcesByWorkspace` — no change needed (auth_config is excluded from listing).

---

## 5. API Layer Changes — `app.go`

### 5.1 OAuth Flow Endpoints

Add three new exported methods that the frontend can call via Wails bindings:

```go
// RequestGoogleSheetsAuth starts the device authorization flow for a data source.
// Returns the user code and verification URL for the frontend to display.
func (a *App) RequestGoogleSheetsAuth(dataSourceID uint) (map[string]interface{}, error) {
    resp, err := services.RequestDeviceCode()
    if err != nil {
        return nil, err
    }
    // Store device_code temporarily for polling
    // (could use a simple in-memory map or the data_source itself)
    services.StorePendingDeviceCode(dataSourceID, resp.DeviceCode)
    return map[string]interface{}{
        "user_code":        resp.UserCode,
        "verification_url": resp.VerificationURL,
        "expires_in":       resp.ExpiresIn,
    }, nil
}

// PollGoogleSheetsAuth polls the token endpoint until the user authorizes.
func (a *App) PollGoogleSheetsAuth(dataSourceID uint) (map[string]interface{}, error) {
    deviceCode, err := services.GetPendingDeviceCode(dataSourceID)
    if err != nil {
        return nil, err
    }
    tok, err := services.ExchangeDeviceCode(deviceCode)
    if err != nil {
        return nil, err
    }
    // Store the token
    if err := services.StoreAuthConfig(dataSourceID, tok); err != nil {
        return nil, err
    }
    return map[string]interface{}{
        "status": "authorized",
    }, nil
}

// RevokeGoogleSheetsAuth removes OAuth tokens for a data source.
func (a *App) RevokeGoogleSheetsAuth(dataSourceID uint) error {
    return services.ClearAuthConfig(dataSourceID)
}
```

### 5.2 Update `DataSourceSetting` struct

Add `FilePath` and `FileType` fields so the frontend can show sheet ID:

```go
type DataSourceSetting struct {
    // ... existing fields ...
    FilePath string `json:"file_path,omitempty"`
    FileType string `json:"file_type,omitempty"`
    // AuthConfig is intentionally excluded
}
```

Update `ListDataSources` to populate these fields from the model.

### 5.3 Update `TestNewDataSource` & List Schema

The existing `TestNewDataSource` should work for Google Sheets — it calls `driver.PingNative()` which checks auth + spreadsheet existence.

Add a `LoadSchema` endpoint (if not already existing) that returns the schema for a data source:

```go
func (a *App) LoadSchema(dataSourceID uint) (*services.DataSchema, error) {
    // ... calls driver.GetSchema()
}
```

---

## 6. Frontend Changes

### 6.1 Data Source Type Dropdown (`SettingsView.svelte`)

Add new option:

```svelte
<option value="google_sheets">Google Sheets</option>
```

### 6.2 Connection Fields Config

Add to `connectionFields` map:

```js
google_sheets: { filePath: true, auth: true },
```

Where `auth: true` means the form shows an OAuth button instead of host/port fields.

### 6.3 Google Sheets Form Fields

When `dbDetailForm.type === 'google_sheets'`, show:

1. **Spreadsheet ID/URL input** — maps to `filePath` field
   - Accepts: full URL (`https://docs.google.com/spreadsheets/d/ABC123/edit`) or raw ID (`ABC123`)
   - Parse on save: extract ID from URL if full URL is pasted

2. **Auth Status + Button** — three states:
   - **Unauthenticated**: "Connect Google Account" button
   - **Authorizing**: Shows user code + verification URL with copy button, then polls
   - **Authorized**: Shows green checkmark + "Reconnect" / "Disconnect" buttons

### 6.4 OAuth Flow UI

```
[Connect Google Account]
    ↓ click
┌─────────────────────────────────────┐
│ 1. Go to google.com/device          │
│ 2. Enter code: XXXX-XXXX            │  [Copy code]
│                                      │
│ Waiting for authorization... ⏳      │
│                                      │
│ [Cancel]                             │
└─────────────────────────────────────┘
    ↓ user authorizes in browser
    ↓ polling succeeds
✅ Connected — ben@example.com
   [Disconnect]
```

### 6.5 Sheet Picker (Future Enhancement)

After connecting, the frontend could show a dropdown of available sheets in the spreadsheet for quick reference in the settings UI. MVP can skip this — the system prompt already shows available tables.

### 6.6 Data Source Card in Discussion View

For `google_sheets` type, show 📊 icon + spreadsheet name + "Google Sheets" badge. The `filePath` field would display the spreadsheet ID (truncated).

---

## 7. System Prompt Changes

The `SQLDialectHint()` already returns appropriate text (step 3 above). The schema introspection adds tables to the system prompt automatically. No changes needed in `discussion_engine.go` beyond what the existing driver pattern already handles.

Example system prompt fragment for a connected sheet:

```
Database Schema:

Connected to Google Sheets spreadsheet (ID: ABC123).
Tables available:

Table: sales_data (columns: region TEXT, revenue TEXT, quarter TEXT)
Table: inventory (columns: product_id TEXT, stock TEXT, warehouse TEXT)

SQL Dialect: SQLite — data loaded from Google Sheets.
Each sheet becomes a separate table named after the sheet.
```

---

## 8. Google Cloud Console Setup

One-time setup for the developer:

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or use existing
3. Enable the **Google Sheets API**
4. Go to **APIs & Services → Credentials**
5. Create **OAuth 2.0 Client ID** → Application type: **Desktop app**
6. Note the Client ID and Client Secret
7. Set as constants in `google_auth.go`:
   ```go
   const defaultGoogleClientID = "YOUR_CLIENT_ID.apps.googleusercontent.com"
   const defaultGoogleClientSecret = "GOCSPX-YOUR_CLIENT_SECRET"
   ```
8. Users can override via `YOURQL_GOOGLE_CLIENT_ID` and `YOURQL_GOOGLE_CLIENT_SECRET` env vars

---

## 9. Refactoring: Extract SQLite Helpers

Currently `data_file.go` contains SQLite helper functions (`createTable`, `insertRow`, `runQuery`, `sanitizeColumnName`, `inferColumnType`) used by both CSV and Excel drivers. These should be moved to a new file `pkg/services/sqlite_helpers.go` so the Google Sheets driver can use them too.

Affected:
- `data_file.go` — imports `sqlite_helpers` functions, ~60 lines removed
- `sqlite_helpers.go` — new file, ~80 lines
- `db_google_sheets.go` — imports helpers

---

## 10. Implementation Order

| Step | File(s) | Lines | Description |
|------|---------|-------|-------------|
| 1 | `go.mod` | +2 | Add `x/oauth2` + `google.golang.org/api/sheets/v4` |
| 2 | `pkg/services/sqlite_helpers.go` | ~80 | Extract shared SQLite helpers from `data_file.go` |
| 3 | `pkg/services/data_file.go` | ~-60 | Remove extracted helpers, import from `sqlite_helpers` |
| 4 | `pkg/models/db_connection.go` | +2 | Add `AuthConfig *string` to `DataSource` |
| 5 | `pkg/models/database.go` | +1 | Migration: `add_auth_config_column` |
| 6 | `pkg/services/db_connection.go` | +3 | Scan `auth_config` in `GetDataSourceByID` |
| 7 | `pkg/services/google_auth.go` | ~120 | OAuth2 device flow (request, exchange, store, load) |
| 8 | `pkg/services/db_google_sheets.go` | ~220 | Driver + schema + query execution |
| 9 | `app.go` | ~80 | OAuth endpoints + `DataSourceSetting` updates |
| 10 | `frontend/src/SettingsView.svelte` | ~80 | Google Sheets type + OAuth UI |
| 11 | Wails bindings regenerate | — | `wails generate module` |
| **Total** | | **~470** | |

---

## 11. Non-Goals (Future)

- **Write support**: Read-only. Sheets API write requires broader scopes.
- **Real-time sync**: Data is fetched per-query. Future: add `modifiedTime` check for caching.
- **Multiple spreadsheets per source**: One spreadsheet per data source. Connect multiple sources for multiple sheets.
- **Service account auth**: Only OAuth2 user flow for MVP.
- **Sheet picker dropdown**: Manual spreadsheet ID input for MVP. Sheet picker is a nice-to-have.