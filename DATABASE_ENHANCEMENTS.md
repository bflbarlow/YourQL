# Database Enhancements — Multi-Database Support

## Overview

YourQL currently supports **MySQL** and **SQLite**. This document describes the technical work needed to add support for PostgreSQL, Snowflake, BigQuery, SQL Server, and other databases. The goal is a clean abstraction that makes each new database a ~1-file addition.

---

## Current Architecture

### Where database type is coupled to code

| File | Coupling |
|------|----------|
| `pkg/models/db_connection.go` | `Type string` field — no enum, stringly-typed |
| `pkg/services/database_connection.go` | `BuildDSN()` switch on `conn.Type` → MySQL/SQLite DSN builders |
| `pkg/services/database_introspection.go` | `GetDatabaseSchema()` switch → MySQL/SQLite schema functions |
| `pkg/services/sql_execution.go` | `sql.Open(conn.Type, dsn)` — driver name == conn.Type string |
| `pkg/services/discussion_engine.go` | `switch dbConnection.Type` → human-readable name for prompt |
| `app.go` | `CreateDBConnection(name, dbType, ...)` — flat params, no per-db fields |
| `frontend/src/SettingsView.svelte` | Hard-coded `<option value="mysql">`, `<option value="sqlite">` |

### Current connection fields (one-size-fits-all)

```
DBConnection {
    Type     string   // "mysql" | "sqlite"
    Host     *string
    Port     *int
    Database *string
    Username *string
    Password *string
    SSLMode  *string
    Config   *string  // JSON blob for advanced settings
}
```

This flat structure works for MySQL/SQLite but breaks down for:
- **BigQuery** — needs `project_id`, `dataset`, service-account JSON, no host/port
- **Snowflake** — needs `account`, `warehouse`, `role`, `schema`, authenticator type
- **SQL Server** — needs `instance` name, Windows auth, encrypted connection flags
- **PostgreSQL** — mostly fits but needs `sslmode` options (disable, require, verify-ca, verify-full) and `search_path`

---

## Proposed Architecture

### 1. Interface-based driver system

Define an interface that each database driver implements:

```go
// pkg/services/db_driver.go

type DBDriver interface {
    // Returns the driver name for sql.Open() (e.g., "pgx", "mysql", "snowflake")
    DriverName() string

    // Builds the connection string (DSN) from a DBConnection
    BuildDSN(conn *models.DBConnection) (string, error)

    // Introspects the database and returns its schema
    GetSchema(conn *models.DBConnection) (*DatabaseSchema, error)

    // Returns a human-readable name for system prompts ("PostgreSQL", "BigQuery")
    DisplayName() string

    // Returns a short SQL dialect hint for the LLM prompt
    SQLDialectHint() string

    // Tests connectivity (ping)
    TestConnection(conn *models.DBConnection) error
}
```

### 2. Registry pattern

```go
// pkg/services/db_registry.go

var driverRegistry = map[string]DBDriver{}

func RegisterDriver(driver DBDriver) {
    driverRegistry[driver.DriverName()] = driver
}

func GetDriver(dbType string) (DBDriver, error) {
    d, ok := driverRegistry[dbType]
    if !ok {
        return nil, fmt.Errorf("unsupported database type: %s", dbType)
    }
    return d, nil
}
```

Each driver registers itself in an `init()` function in its own file:

```go
// pkg/services/db_postgres.go
func init() {
    RegisterDriver(&PostgresDriver{})
}
```

### 3. Refactor existing code to use drivers

**database_connection.go** — replace switch with registry lookup:

```go
func BuildDSN(conn *models.DBConnection) (string, error) {
    driver, err := GetDriver(conn.Type)
    if err != nil {
        return "", err
    }
    return driver.BuildDSN(conn)
}
```

**database_introspection.go** — same pattern:

```go
func GetDatabaseSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
    driver, err := GetDriver(conn.Type)
    if err != nil {
        return nil, err
    }
    return driver.GetSchema(conn)
}
```

**discussion_engine.go** — use `driver.DisplayName()` / `driver.SQLDialectHint()`.

### 4. DBConnection model changes

Add a `json`-typed `Extra` field for database-specific parameters:

```go
type DBConnection struct {
    // ... existing fields ...
    Extra *string `json:"extra,omitempty"` // JSON blob for per-db params
}
```

Each driver parses `Extra` into its own config struct:

```go
type PostgresExtra struct {
    SearchPath string `json:"search_path,omitempty"`
    SSLMode    string `json:"sslmode,omitempty"` // "disable", "require", etc.
}
```

### 5. Frontend changes

**SettingsView.svelte** — dynamic form fields based on selected type:

```svelte
<select bind:value={dbForm.type}>
    <option value="mysql">MySQL</option>
    <option value="sqlite">SQLite</option>
    <option value="postgresql">PostgreSQL</option>
    <option value="sqlserver">SQL Server</option>
    <option value="snowflake">Snowflake</option>
    <option value="bigquery">BigQuery</option>
</select>

{#if dbForm.type === 'bigquery'}
    <!-- Show project_id, dataset, service account key fields -->
    <input bind:value={dbForm.extra.projectId} placeholder="my-project" />
    <input bind:value={dbForm.extra.dataset} placeholder="my_dataset" />
    <textarea bind:value={dbForm.extra.serviceAccountKey} placeholder="Paste service account JSON..." />
{/if}
```

A backend endpoint should return the list of registered driver types and their required extra fields:

```go
// app.go
func (a *App) GetSupportedDBTypes() []DBTypeInfo { ... }

type DBTypeInfo struct {
    Type        string              `json:"type"`
    DisplayName string              `json:"display_name"`
    Fields      []DBFieldDefinition `json:"fields"`
}

type DBFieldDefinition struct {
    Key         string `json:"key"`
    Label       string `json:"label"`
    Type        string `json:"type"` // "text", "password", "number", "select"
    Required    bool   `json:"required"`
    Placeholder string `json:"placeholder,omitempty"`
    Options     []string `json:"options,omitempty"`
}
```

---

## Per-Database Implementation Details

### PostgreSQL (`postgresql`)

| Aspect | Detail |
|--------|--------|
| **Go driver** | `github.com/jackc/pgx/v5` (recommended) or `github.com/lib/pq` |
| **Connection string** | `postgres://user:pass@host:port/dbname?sslmode=require&search_path=public` |
| **Default port** | 5432 |
| **Schema introspection** | Query `information_schema.columns`, `information_schema.tables`, `pg_catalog.pg_indexes`, `information_schema.table_constraints` |
| **Extra fields** | `sslmode` (disable/require/verify-ca/verify-full), `search_path` |
| **SQL dialect quirks** | `"identifier"` quoting, `LIMIT/OFFSET`, `ILIKE`, `::type` casts, `RETURNING` clauses — but read-only SELECTs are standard |
| **Complexity** | **Low** — very similar to MySQL |

### SQL Server (`sqlserver`)

| Aspect | Detail |
|--------|--------|
| **Go driver** | `github.com/microsoft/go-mssqldb` |
| **Connection string** | `sqlserver://user:pass@host:port?database=dbname&encrypt=true` |
| **Default port** | 1433 |
| **Schema introspection** | Query `INFORMATION_SCHEMA.COLUMNS`, `sys.tables`, `sys.indexes`, `sys.foreign_keys` |
| **Extra fields** | `encrypt` (bool, default true), `trust_server_certificate` (bool), `instance` (named instance) |
| **SQL dialect quirks** | `[identifier]` quoting, `TOP N` instead of `LIMIT`, `GETDATE()`, `SELECT ... INTO` |
| **Complexity** | **Medium** — different system catalog, TOP-vs-LIMIT in generated hints |

### Snowflake (`snowflake`)

| Aspect | Detail |
|--------|--------|
| **Go driver** | `github.com/snowflakedb/gosnowflake` |
| **Connection string** | `user:pass@account.snowflakecomputing.com:443/db/schema?warehouse=WH&role=ROLE` |
| **Default port** | 443 |
| **Schema introspection** | Query `INFORMATION_SCHEMA.COLUMNS`, `INFORMATION_SCHEMA.TABLES` (Snowflake has standard INFORMATION_SCHEMA) |
| **Extra fields** | `account` (e.g. `xy12345.us-east-1`), `warehouse`, `role`, `schema_name`, `authenticator` (snowflake/oauth/externalbrowser) |
| **SQL dialect quirks** | `"identifier"` quoting, `LIMIT`, `ILIKE`, `QUALIFY`, semi-structured data types (VARIANT, ARRAY) |
| **Complexity** | **Medium** — extra auth fields, but SQL dialect is close to PostgreSQL |

### BigQuery (`bigquery`)

| Aspect | Detail |
|--------|--------|
| **Go driver** | `github.com/go-gorm/bigquery/driver` or raw `cloud.google.com/go/bigquery` + `database/sql` adapter |
| **Connection string** | BigQuery doesn't use traditional DSN. Use service account JSON + project/dataset. Needs a custom `database/sql` driver wrapper or direct BigQuery client. |
| **Schema introspection** | Query `INFORMATION_SCHEMA.COLUMNS` or use BigQuery API's `TableMetadata` |
| **Extra fields** | `project_id` (required), `dataset` (required), `service_account_key` (JSON blob, required) |
| **SQL dialect quirks** | Backtick quoting, `STRUCT`/`ARRAY` types, `STRING` not `VARCHAR`, `LIMIT`, no indexes, partition-aware queries |
| **Complexity** | **High** — no traditional DSN, needs Google Cloud SDK, service account auth, different `database/sql` integration |

### SQLite (`sqlite`)

| Aspect | Detail |
|--------|--------|
| **Status** | Already supported |
| **Go driver** | `modernc.org/sqlite` (pure Go, no CGO) |
| **Changes needed** | Extract into driver interface, no new features needed |

### MySQL (`mysql`)

| Aspect | Detail |
|--------|--------|
| **Status** | Already supported |
| **Go driver** | `github.com/go-sql-driver/mysql` |
| **Changes needed** | Extract into driver interface, no new features needed |

---

## Migration Plan

### Phase 1 — Refactor (no new DBs, no user-facing changes)

1. Define `DBDriver` interface in `pkg/services/db_driver.go`
2. Create `pkg/services/db_registry.go` with `RegisterDriver` / `GetDriver`
3. Extract MySQL code into `pkg/services/db_mysql.go` implementing `DBDriver`
4. Extract SQLite code into `pkg/services/db_sqlite.go` implementing `DBDriver`
5. Refactor `BuildDSN()`, `GetDatabaseSchema()`, `TestDBConnection()`, discussion engine prompt building to use registry
6. Verify all existing functionality still works

### Phase 2 — Add PostgreSQL

1. Add `github.com/jackc/pgx/v5` dependency
2. Create `pkg/services/db_postgres.go` implementing `DBDriver`
3. Add `Extra` field to `DBConnection` model + DB migration
4. Add `postgresql` to frontend type dropdown
5. Test with local PostgreSQL instance

### Phase 3 — Add SQL Server, Snowflake

1. Add respective Go driver dependencies
2. Create driver files
3. Update frontend with per-type extra fields
4. Add `GetSupportedDBTypes()` endpoint for dynamic form rendering

### Phase 4 — Add BigQuery

1. Add Google Cloud SDK dependency
2. Implement custom `database/sql` adapter if needed, or bypass `database/sql` and use BigQuery client directly
3. Create driver file
4. Add service account key upload/management in frontend

---

## Files to Create / Modify

### New files

| File | Purpose |
|------|---------|
| `pkg/services/db_driver.go` | `DBDriver` interface definition |
| `pkg/services/db_registry.go` | Driver registry + `GetDriver()` |
| `pkg/services/db_mysql.go` | MySQL driver (extracted from existing) |
| `pkg/services/db_sqlite.go` | SQLite driver (extracted from existing) |
| `pkg/services/db_postgres.go` | PostgreSQL driver |
| `pkg/services/db_sqlserver.go` | SQL Server driver |
| `pkg/services/db_snowflake.go` | Snowflake driver |
| `pkg/services/db_bigquery.go` | BigQuery driver |

### Modified files

| File | Changes |
|------|---------|
| `pkg/models/db_connection.go` | Add `Extra *string` field |
| `pkg/models/database.go` | Migration for `extra` column |
| `pkg/services/database_connection.go` | Replace switch with `GetDriver()` |
| `pkg/services/database_introspection.go` | Replace switch with `GetDriver()` |
| `pkg/services/sql_execution.go` | No changes (already uses `sql.Open(conn.Type, ...)`) |
| `pkg/services/discussion_engine.go` | Use `driver.DisplayName()` / `driver.SQLDialectHint()` |
| `pkg/services/db_connection.go` | Add `extra` to CRUD operations |
| `app.go` | Add `GetSupportedDBTypes()`, update `CreateDBConnection`/`UpdateDBConnection` to handle `extra` |
| `frontend/src/SettingsView.svelte` | Dynamic type-specific fields, `GetSupportedDBTypes()` call |

---

## LLM Prompt Considerations

Each database has dialect-specific quirks that must be communicated to the LLM:

| Database | Should mention in prompt |
|----------|--------------------------|
| MySQL | Backtick quoting, `LIMIT`, `INFORMATION_SCHEMA` |
| PostgreSQL | Double-quote identifiers, `LIMIT/OFFSET`, `::type` casts, `ILIKE` |
| SQLite | Double-quote identifiers, `LIMIT`, limited `ALTER TABLE` |
| SQL Server | Bracket `[identifier]`, `TOP N` (no `LIMIT`), `GETDATE()` |
| Snowflake | Double-quote identifiers, `LIMIT`, `QUALIFY`, `VARIANT`/`ARRAY` types |
| BigQuery | Backtick quoting, `STRUCT`/`ARRAY` types, `STRING` type, partition pruning |

The `SQLDialectHint()` method on each driver should return a concise 2-3 line summary for the system prompt.

---

## Security Considerations

- **BigQuery service account keys** — must be stored encrypted at rest (the `Password` field already uses `json:"-"` to prevent serialization; apply same to sensitive extra fields)
- **Snowflake authenticator** — OAuth and external browser auth flows need special handling
- **SQL Server Windows auth** — needs integrated security support, may not work in all environments
- All drivers must enforce **read-only SELECT enforcement** (already done in `executeSQLWithMode`)

---

## Testing Strategy

1. **Unit tests per driver** — `TestMySQLDriver`, `TestPostgresDriver`, etc., run against Docker containers in CI
2. **Integration tests** — `TestDatabaseIntrospection` with each driver
3. **Manual test checklist** — create connection → test connection → introspect schema → ask a natural language question → verify SQL runs