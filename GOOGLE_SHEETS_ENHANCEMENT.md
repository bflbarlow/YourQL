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
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "strings"
    "sync"
    "time"

    "YourQL/pkg/models"

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
//
// Note: `access_type=offline` and `prompt=consent` are included to force Google to
// return a refresh_token even on re-authorization. Without these, Google only issues
// a refresh_token on the very first authorization, so reconnects would silently omit it.
func RequestDeviceCode() (*DeviceAuthResponse, error) {
    cfg := googleOAuthConfig()
    data := fmt.Sprintf(
        "client_id=%s&scope=%s&access_type=offline&prompt=consent",
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
// ExchangeDeviceCode polls Google's token endpoint until the user authorizes, the code expires,
// or the user denies access. Must be called with the original DeviceAuthResponse's Interval
// (Google's recommended polling cadence) and ExpiresIn (expiration timeout).
//
// The ctx parameter allows cancellation (e.g., when the user closes the settings panel
// or clicks Cancel). Callers should typically pass the App's context.
func ExchangeDeviceCode(ctx context.Context, deviceCode string, interval time.Duration, expiresAt time.Time) (*oauth2.Token, error) {
    cfg := googleOAuthConfig()
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if time.Now().After(expiresAt) {
                return nil, fmt.Errorf("device authorization code expired")
            }
        case <-ctx.Done():
            return nil, fmt.Errorf("polling cancelled: %w", ctx.Err())
        }

        tok, err := cfg.Exchange(ctx, deviceCode,
            oauth2.SetAuthURLParam("grant_type", "urn:ietf:params:oauth:grant-type:device_code"),
        )
        if err == nil {
            return tok, nil
        }

        // "authorization_pending" means the user hasn't authorized yet — keep polling
        if strings.Contains(err.Error(), "authorization_pending") {
            continue
        }

        // "slow_down" means we're polling too fast — back off by extending the ticker
        if strings.Contains(err.Error(), "slow_down") {
            interval += 5 * time.Second
            ticker.Reset(interval)
            continue
        }

        // "expired_device_code" means the user took too long to authorize
        if strings.Contains(err.Error(), "expired_device_code") {
            return nil, fmt.Errorf("authorization code expired — please start the OAuth flow again")
        }

        // "access_denied" means the user explicitly denied access
        if strings.Contains(err.Error(), "access_denied") {
            return nil, fmt.Errorf("user denied access to Google Sheets")
        }

        return nil, fmt.Errorf("token exchange failed: %w", err)
    }
}
```

> **Important**: The caller must pass `expiresAt` (derived from `resp.ExpiresIn` seconds from the device code request) and `interval` (from `resp.Interval`, Google's recommended polling cadence) to avoid `slow_down` errors. See Section 5.1 for how the App layer stores these values in `PendingEntry` alongside the device code and passes them into `ExchangeDeviceCode` from a background goroutine.

> **Architecture note**: `ExchangeDeviceCode` blocks in a polling loop. It should be called from a goroutine — not directly from a Wails-exposed method — and the result should be delivered to the frontend via Wails events (`googleAuthComplete` / `googleAuthError`). See Section 5.1 for the full pattern.

### 2.3 Token Storage & Refresh

Both functions are **exported** so `app.go` can call them via `services.StoreAuthConfig` / `services.LoadAuthConfig`.

```go
// StoreAuthConfig serializes an OAuth2 token into the data source's auth_config column.
func StoreAuthConfig(connID uint, token *oauth2.Token) error {
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

// LoadAuthConfig deserializes the OAuth2 token from the data source's auth_config column.
func LoadAuthConfig(connID uint) (*oauth2.Token, error) {
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
// It detects expired tokens and returns a specific error that the caller should handle
// by prompting the user to re-authenticate.
func getSheetsClient(conn *models.DataSource) (*sheets.Service, error) {
    tok, err := LoadAuthConfig(conn.ID)
    if err != nil {
        return nil, err
    }

    // Detect expired token — if the access token is expired and there's no refresh token,
    // we can't recover automatically. Return a sentinel error.
    if tok.Expiry.Before(time.Now()) {
        if tok.RefreshToken == "" {
            return nil, fmt.Errorf("oauth_token_expired")
        }
        // If a refresh token exists, oauth2.Config.Client() will auto-refresh.
        // But if the refresh token is also expired, the HTTP request will fail with 401.
        // The Sheets API client will surface this as an error; the frontend should
        // detect it and prompt re-authentication.
    }

    cfg := googleOAuthConfig()
    client := cfg.Client(context.Background(), tok)
    return sheets.New(client)
}
```

### 2.4 Pending Device Code Storage

The device code must be stored temporarily so the background polling goroutine can retrieve it, along with Google's recommended polling `Interval` and the code's `ExpiresAt`. Use a thread-safe in-memory map with automatic cleanup:

```go
var pendingDeviceCodes = struct {
    sync.Mutex
    m map[uint]PendingEntry
}{m: make(map[uint]PendingEntry)}

// PendingEntry holds everything needed to poll for the token exchange.
type PendingEntry struct {
    Code      string        // Google device_code from the device auth response
    Interval  time.Duration // Google-recommended polling cadence
    ExpiresAt time.Time     // When the code becomes invalid
}

// StorePendingDeviceCode saves a device code + polling params for a data source.
func StorePendingDeviceCode(dataSourceID uint, entry PendingEntry) {
    pendingDeviceCodes.Lock()
    defer pendingDeviceCodes.Unlock()
    pendingDeviceCodes.m[dataSourceID] = entry
}

// GetPendingDeviceCode retrieves the pending entry WITHOUT deleting it.
// The polling loop calls this once at the start; the entry is removed via
// ClearPendingDeviceCode after success, failure, or cancellation.
func GetPendingDeviceCode(dataSourceID uint) (PendingEntry, error) {
    pendingDeviceCodes.Lock()
    defer pendingDeviceCodes.Unlock()
    entry, ok := pendingDeviceCodes.m[dataSourceID]
    if !ok {
        return PendingEntry{}, fmt.Errorf("no pending device code for data source %d", dataSourceID)
    }
    return entry, nil
}

// ClearPendingDeviceCode removes a pending device code (on cancel, success, or failure).
func ClearPendingDeviceCode(dataSourceID uint) {
    pendingDeviceCodes.Lock()
    defer pendingDeviceCodes.Unlock()
    delete(pendingDeviceCodes.m, dataSourceID)
}

// CleanupPendingCodes removes all entries whose ExpiresAt was more than 5 minutes ago.
// Call this periodically (e.g., every 5 min) from a goroutine — see Section 5.1.1.
func CleanupPendingCodes() {
    pendingDeviceCodes.Lock()
    defer pendingDeviceCodes.Unlock()
    now := time.Now()
    for id, entry := range pendingDeviceCodes.m {
        if now.After(entry.ExpiresAt.Add(5 * time.Minute)) {
            delete(pendingDeviceCodes.m, id)
        }
    }
}
```

> **Why peek, not delete?** The token-exchange polling loop needs to re-check the entry (or at least, holding the reference for the duration of polling requires stable state). Ownership is: the App layer creates the entry, spawns the polling goroutine, and is responsible for calling `ClearPendingDeviceCode` on completion or cancellation.

### 2.5 Token Revocation

```go
// ClearAuthConfig removes OAuth tokens for a data source.
func ClearAuthConfig(connID uint) error {
    _, err := models.DB.Exec(
        "UPDATE data_sources SET auth_config = NULL WHERE id = ?", connID,
    )
    return err
}

// RevokeToken revokes the access token with Google's revocation endpoint.
func RevokeToken(token *oauth2.Token) error {
    cfg := googleOAuthConfig()
    return cfg.Revoke(token.AccessToken)
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
        resp, err := svc.Spreadsheets.Values.Get(*conn.FilePath, range_).Do()
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

        // Use pagination for large sheets (see Section 3.2.1).
        values, err := fetchSheetWithPagination(svc, *conn.FilePath, title)
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
```

#### 3.2.1 Pagination for Large Sheets

Google Sheets API `Values.Get` has a soft cap of roughly **100K cells per response**. For large sheets we fetch in row-based chunks and stop when the API returns fewer rows than requested (signaling the end of data). Called by `queryGoogleSheets` above.

```go
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
```

> **Note**: For sheets exceeding ~50K rows, consider adding a progress indicator in the UI (emit a Wails event per batch with `startRow` progress).

### 3.3 Helpers

The Google Sheets driver reuses the existing shared helpers `createTable`, `insertRow`, `runQuery`, `sanitizeColumnName`, and `inferColumnType` (extracted to `pkg/services/sqlite_helpers.go` — see Section 9). The only Sheets-specific helper is a converter from the API's `[]interface{}` row format to `[]string`:

```go
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
```

> **Signature compatibility**: The existing `insertRow(db, table, columns, values []string)` in `data_file.go` already takes `[]string`, so `toStringSlice` bridges the API's `[]interface{}` output to the shared helper's input. No changes to the shared helper are needed.

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

Update `ListDataSourcesByWorkspace` — no change needed (auth_config is excluded from listing via `json:"-"` tag).

> **Naming note**: The column is `auth_config` (snake_case) to match the existing `config` and `extra` column naming convention in the `data_sources` table. It stores raw `oauth2.Token` JSON. The `json:"-"` tag on the Go struct ensures it's never serialized to the frontend.

### 4.4 Token JSON Structure

Google's `oauth2.Token` struct serializes to:
```json
{
  "access_token": "ya29.a0AfH6SM...",
  "token_type": "Bearer",
  "expiry": "2026-07-20T17:00:00Z",
  "refresh_token": "1//0g..."
}
```

The `Expiry` field is a `time.Time` — `json.Unmarshal` handles the RFC3339 format correctly. Always check `tok.Expiry.Before(time.Now())` before making API calls to detect expired tokens.

---

## 5. API Layer Changes — `app.go`

### 5.1 OAuth Flow Endpoints

The OAuth device flow polls Google's token endpoint until the user authorizes (up to 15 minutes). Since Wails method calls block the JS caller until they return, we **cannot** synchronously poll from within a Wails method — the frontend would be frozen with no progress or cancel affordance.

Instead, `StartGoogleSheetsAuth` returns immediately with the user code + verification URL, and spawns a background goroutine that emits Wails events when the exchange completes. This mirrors the existing pattern in `ProcessUserMessage` (which uses `runtime.EventsEmit` for phase updates).

**Events emitted:**
- `googleAuthComplete` — payload: `{dataSourceID: uint}` — token stored, ready to use
- `googleAuthError` — payload: `{dataSourceID: uint, error: string}` — user denied, code expired, or cancelled

```go
// Auth cancellation: one context per data source in flight. Cancelling the
// context stops the polling goroutine.
var (
    authCancelMu sync.Mutex
    authCancels  = map[uint]context.CancelFunc{}
)

// StartGoogleSheetsAuth begins the device authorization flow for a data source.
// Returns the user_code and verification_url immediately so the frontend can
// display them. The actual token exchange runs in a background goroutine and
// emits Wails events on completion or failure.
func (a *App) StartGoogleSheetsAuth(dataSourceID uint) (map[string]interface{}, error) {
    resp, err := services.RequestDeviceCode()
    if err != nil {
        return nil, err
    }

    expiresAt := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
    interval := time.Duration(resp.Interval) * time.Second
    if interval <= 0 {
        interval = 5 * time.Second // safety fallback if Google omits the field
    }

    services.StorePendingDeviceCode(dataSourceID, services.PendingEntry{
        Code:      resp.DeviceCode,
        Interval:  interval,
        ExpiresAt: expiresAt,
    })

    // Set up a cancellable context so CancelGoogleSheetsAuth can stop polling.
    ctx, cancel := context.WithCancel(a.ctx)
    authCancelMu.Lock()
    if prev, ok := authCancels[dataSourceID]; ok {
        prev() // cancel any previous in-flight auth for this data source
    }
    authCancels[dataSourceID] = cancel
    authCancelMu.Unlock()

    go func() {
        defer func() {
            authCancelMu.Lock()
            delete(authCancels, dataSourceID)
            authCancelMu.Unlock()
            services.ClearPendingDeviceCode(dataSourceID)
        }()

        entry, err := services.GetPendingDeviceCode(dataSourceID)
        if err != nil {
            runtime.EventsEmit(a.ctx, "googleAuthError", map[string]interface{}{
                "dataSourceID": dataSourceID,
                "error":        err.Error(),
            })
            return
        }

        tok, err := services.ExchangeDeviceCode(ctx, entry.Code, entry.Interval, entry.ExpiresAt)
        if err != nil {
            runtime.EventsEmit(a.ctx, "googleAuthError", map[string]interface{}{
                "dataSourceID": dataSourceID,
                "error":        err.Error(),
            })
            return
        }

        if err := services.StoreAuthConfig(dataSourceID, tok); err != nil {
            runtime.EventsEmit(a.ctx, "googleAuthError", map[string]interface{}{
                "dataSourceID": dataSourceID,
                "error":        "failed to store token: " + err.Error(),
            })
            return
        }

        runtime.EventsEmit(a.ctx, "googleAuthComplete", map[string]interface{}{
            "dataSourceID": dataSourceID,
        })
    }()

    return map[string]interface{}{
        "user_code":        resp.UserCode,
        "verification_url": resp.VerificationURL,
        "expires_in":       resp.ExpiresIn,
        "interval":         resp.Interval,
    }, nil
}

// CancelGoogleSheetsAuth stops an in-flight auth flow (user clicked Cancel or
// closed the settings panel). Safe to call even if no auth is in flight.
func (a *App) CancelGoogleSheetsAuth(dataSourceID uint) error {
    authCancelMu.Lock()
    cancel, ok := authCancels[dataSourceID]
    authCancelMu.Unlock()
    if ok {
        cancel() // triggers ctx.Done() in ExchangeDeviceCode
    }
    services.ClearPendingDeviceCode(dataSourceID)
    return nil
}

// RevokeGoogleSheetsAuth removes OAuth tokens for a data source and revokes
// them with Google (best-effort).
func (a *App) RevokeGoogleSheetsAuth(dataSourceID uint) error {
    tok, _ := services.LoadAuthConfig(dataSourceID)
    if tok != nil {
        _ = services.RevokeToken(tok) // best-effort; ignore errors
    }
    return services.ClearAuthConfig(dataSourceID)
}
```

> **Frontend flow**: Call `StartGoogleSheetsAuth(id)` → display returned `user_code` + `verification_url` → subscribe to `googleAuthComplete` / `googleAuthError` events via `EventsOn(...)` → update UI when event arrives. On Cancel button: call `CancelGoogleSheetsAuth(id)` and unsubscribe.

> **Note on method rename**: `RequestGoogleSheetsAuth` + `PollGoogleSheetsAuth` (from earlier draft) are replaced by `StartGoogleSheetsAuth` + `CancelGoogleSheetsAuth`. The frontend no longer polls a Wails method; it subscribes to events.

### 5.1.1 Background Cleanup Goroutine

Start a cleanup goroutine in `startup()` to evict expired pending device codes:

```go
// In app.go startup:
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        services.CleanupPendingCodes()
    }
}()
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

### 5.3 Update `TestNewDataSource` & Schema Loading

The existing `TestNewDataSource` should work for Google Sheets — it calls `driver.PingNative()` which checks auth + spreadsheet existence via `svc.Spreadsheets.Get(...).Do()`.

Schema loading also works unchanged: `app.go` already exposes `GetSchemaPreview(id uint)` which calls `services.GetDataSchema(conn)` → `driver.GetSchema(conn)` → our new `introspectGoogleSheets`. No new endpoint needed.

> **Edge case**: If `PingNative` fails with `oauth_token_expired` (from `getSheetsClient`), the frontend should treat it as a re-auth prompt (Section 6.4.1) rather than a hard connection failure.

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
   - **Note**: For Google Sheets, `filePath` = spreadsheet ID, not a file path. This is consistent with the existing CSV/Excel driver pattern where `filePath` = data source identifier.

2. **Auth Status + Button** — three states:
   - **Unauthenticated**: "Connect Google Account" button
   - **Authorizing**: Shows user code + verification URL with copy button, then polls
   - **Authorized**: Shows green checkmark + "Reconnect" / "Disconnect" buttons
   - **Expired**: Shows "Reconnect Google Account" button (detected on 401 from Sheets API)

### 6.4 OAuth Flow UI

```
[Connect Google Account]
    ↓ click → StartGoogleSheetsAuth(id)
┌─────────────────────────────────────┐
│ 1. Go to google.com/device          │
│ 2. Enter code: XXXX-XXXX            │  [Copy code]
│                                      │
│ Waiting for authorization... ⏳      │
│                                      │
│ [Cancel]                             │
└─────────────────────────────────────┘
    ↓ user authorizes in browser
    ↓ backend emits `googleAuthComplete` event
✅ Connected
   [Disconnect]
```

**Svelte sketch:**
```svelte
<script>
  import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime';
  import { StartGoogleSheetsAuth, CancelGoogleSheetsAuth } from '../wailsjs/go/main/App';

  let authState = $state('idle'); // idle | pending | success | error
  let userCode = $state('');
  let verificationURL = $state('');
  let errorMsg = $state('');

  async function connect(id) {
    authState = 'pending';
    const resp = await StartGoogleSheetsAuth(id);
    userCode = resp.user_code;
    verificationURL = resp.verification_url;

    EventsOn('googleAuthComplete', (payload) => {
      if (payload.dataSourceID === id) {
        authState = 'success';
        EventsOff('googleAuthComplete');
        EventsOff('googleAuthError');
      }
    });
    EventsOn('googleAuthError', (payload) => {
      if (payload.dataSourceID === id) {
        authState = 'error';
        errorMsg = payload.error;
        EventsOff('googleAuthComplete');
        EventsOff('googleAuthError');
      }
    });
  }

  async function cancel(id) {
    await CancelGoogleSheetsAuth(id);
    EventsOff('googleAuthComplete');
    EventsOff('googleAuthError');
    authState = 'idle';
  }
</script>
```

> **Important**: On Cancel or panel-close, always call `CancelGoogleSheetsAuth(id)` (which internally clears the pending device code AND cancels the polling goroutine's context) and `EventsOff(...)` for both event names. This prevents leaked event listeners across successive auth attempts.

#### 6.4.1 Token Expiration Handling

The frontend must detect when the stored access token has expired and prompt re-authentication:

1. **On API call failure**: Backend returns the sentinel error string `oauth_token_expired` (from `getSheetsClient`) or a 401 wrapped in a Sheets API error.
2. **Frontend detection**: Match on the error message substring `oauth_token_expired` or HTTP 401.
3. **If expired**: Show a "Reconnect Google Account" button in the data source card.
4. **On reconnect**: Call `StartGoogleSheetsAuth(id)` again (same flow as initial auth). Google will re-issue a refresh token because `prompt=consent` is set (Section 2.1).
5. **On disconnect**: Call `RevokeGoogleSheetsAuth(id)` to revoke the token with Google and clear local storage.

```svelte
{#if authStatus === 'expired'}
  <button onclick={() => reconnectGoogle(id)}>Reconnect Google Account</button>
{/if}
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
   - Go to **APIs & Services → Library**
   - Search "Google Sheets API" → Enable
4. Go to **APIs & Services → Credentials**
5. Click **Create Credentials → OAuth client ID**
6. Application type: **Desktop app**
7. Name it (e.g., "YourQL Desktop")
8. Note the **Client ID** and **Client Secret**
9. Set as constants in `google_auth.go`:
   ```go
   const defaultGoogleClientID = "YOUR_CLIENT_ID.apps.googleusercontent.com"
   const defaultGoogleClientSecret = "GOCSPX-YOUR_CLIENT_SECRET"
   ```
10. Users can override via `YOURQL_GOOGLE_CLIENT_ID` and `YOURQL_GOOGLE_CLIENT_SECRET` env vars

### 8.1 OAuth Consent Screen Setup

Before the app can authenticate users, you must configure the OAuth consent screen:

1. Go to **APIs & Services → OAuth consent screen**
2. User type: **External** (for public distribution) or **Internal** (for Google Workspace org only)
3. Fill in:
   - **App name**: "YourQL"
   - **User support email**: Your support contact
   - **Developer contact email**: Your email
4. Add the scope `https://www.googleapis.com/auth/spreadsheets.readonly`
   - Go to **APIs & Services → Credentials → OAuth scopes → Add Scope**
   - Search and add the read-only Sheets scope
5. Add test users (your email addresses) for development
   - For production: you'll need to submit for verification
6. Save and publish the consent screen

> **Important**: Without a published consent screen, only the test users you added can authenticate. For public distribution, submit the app for Google's security review.

### 8.2 Authorized Origins (Optional)

For the device flow, you don't need to configure redirect URIs. However, if you add a web dashboard later, add these to **Authorized JavaScript origins**:
```
http://localhost
https://yourql.app
```

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
| 9 | `app.go` | ~120 | `StartGoogleSheetsAuth` (goroutine + events), `CancelGoogleSheetsAuth`, `RevokeGoogleSheetsAuth`, cleanup goroutine in `startup()`, `DataSourceSetting` updates |
| 10 | `frontend/src/SettingsView.svelte` | ~100 | Google Sheets type + OAuth UI with `EventsOn`/`EventsOff` |
| 11 | Wails bindings regenerate | — | `wails generate module` |
| **Total** | | **~510** | |

---

## 11. Non-Goals (Future)

- **Write support**: Read-only. Sheets API write requires broader scopes.
- **Real-time sync**: Data is fetched per-query. Future: add `modifiedTime` check for caching.
- **Multiple spreadsheets per source**: One spreadsheet per data source. Connect multiple sources for multiple sheets.
- **Service account auth**: Only OAuth2 user flow for MVP.
- **Sheet picker dropdown**: Manual spreadsheet ID input for MVP. Sheet picker is a nice-to-have.