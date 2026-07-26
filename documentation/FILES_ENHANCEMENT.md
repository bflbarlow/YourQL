# Files Enhancement Plan

## Overview

Two interrelated changes to YourQL:

1. **Rebrand** "Database" → "Data" throughout the app
2. **Add file sources** — CSV and Excel files as first-class data sources via DuckDB

SQL remains the query language for all sources. DuckDB handles files using the same SQL interface the app already uses for databases.

---

## Phase A: Rebrand "Database" → "Data"

### Rationale

"Data source" better describes what users connect: databases, CSV files, Excel workbooks, and future sources (Parquet, JSON, APIs). "Database" implies SQL-only, which won't be true once files are supported.

### Files Touched (~25)

**Go Backend:**

| Current | New | Notes |
|---------|-----|-------|
| `pkg/models/db_connection.go` → `data_source.go` | `DBConnection` → `DataSource` | Struct rename |
| `pkg/services/db_connection.go` → `data_source.go` | `CreateDBConnection` → `CreateDataSource` | Service rename |
| `pkg/services/database_connection.go` → `data_source_service.go` | All DBConnection funcs → DataSource | Legacy service |
| `pkg/services/database_introspection.go` → `data_introspection.go` | Same pattern | Schema introspection |
| `pkg/services/db_registry.go` → `data_registry.go` | `DBDriverRegistry` → `DataSourceRegistry` | Registry rename |
| `pkg/services/db_driver.go` → `data_driver.go` | `DBDriver` → `DataSourceDriver` | Interface rename |
| `pkg/services/db_mysql.go` → `data_mysql.go` | Driver names unchanged, impl interfaces | Per-driver files |
| `pkg/services/db_postgres.go` → `data_postgres.go` | Same | |
| `pkg/services/db_sqlite.go` → `data_sqlite.go` | Same | |
| `pkg/services/db_sqlserver.go` → `data_sqlserver.go` | Same | |
| `pkg/services/db_mariadb.go` → `data_mariadb.go` | Same | |
| `pkg/services/db_snowflake.go` → `data_snowflake.go` | Same | |
| `pkg/services/db_bigquery.go` → `data_bigquery.go` | Same | |
| `pkg/services/db_redshift.go` → `data_redshift.go` | Same | |
| `pkg/models/database.go` | Update foreign key field names | Reference rewiring |
| `pkg/models/conversation.go` | `DBConnectionID` → `DataSourceID` | Field rename |
| `pkg/services/conversation.go` | Update all references | |
| `pkg/services/discussion_engine.go` | Update all references | |
| `pkg/services/sql_execution.go` → `data_execution.go` | `SQLExecution` → `DataExecution` | |
| `app.go` | Update method names and signatures | Public API |
| `main.go` | Likely no changes | Migration? |

**Database Migration (SQLite):**

```sql
-- Table rename
ALTER TABLE db_connections RENAME TO data_sources;
ALTER TABLE data_sources ADD COLUMN file_path TEXT DEFAULT '';
ALTER TABLE data_sources ADD COLUMN file_type TEXT DEFAULT '';

-- Conversations table: rename FK columns
ALTER TABLE conversations RENAME COLUMN db_connection_id TO data_source_id;

-- Discussion configs: rename FK columns 
ALTER TABLE discussion_db_configs RENAME TO discussion_data_configs;
ALTER TABLE discussion_data_configs RENAME COLUMN db_connection_id TO data_source_id;
```

**Frontend (Svelte):**

| Current | New |
|---------|-----|
| `dbConnections` | `dataSources` |
| `dbNameByID` | `dataSourceNameByID` |
| `ListDBConnections()` | `ListDataSources()` |
| `CreateDBConnection()` | `CreateDataSource()` |
| `DeleteDBConnection()` | `DeleteDataSource()` |
| `handleTestDB()` | `handleTestDataSource()` |
| Tab label "Database Connections" | "Data Sources" |
| `db_connection_id` in conversation | `data_source_id` |
| `.db-connection-*` CSS classes | `.data-source-*` |

### Migration Strategy

1. Create migration SQL to rename tables/columns and add file columns
2. Update all Go code with new names (compile-time safety)
3. Update all frontend code with new names
4. Test migration on existing user data
5. Old builds will break on migrated DB — acceptable for v0.x

---

## Phase B: CSV & Excel File Support via DuckDB

### Why DuckDB

DuckDB is an embedded analytical database (like SQLite for analytics). Key features:

- **Run SQL against files directly** — no import or table creation needed:
  ```sql
  SELECT * FROM read_csv_auto('sales_2024.csv');
  SELECT region, SUM(amount) FROM read_csv_auto('sales_2024.csv') GROUP BY region;
  SELECT * FROM st_read('budget.xlsx');
  ```
- **Embedded**: single C library, no server, cross-platform
- **Analytical SQL**: window functions, CTEs, columnar execution — faster than SQLite for analytics
- **Go bindings**: [`github.com/marcboeker/go-duckdb`](https://github.com/marcboeker/go-duckdb) — CGO wrapper, mature
- **Tiny**: ~5 MB added to binary size
- **Cross-source joins**: join a CSV against a PostgreSQL table in one query

### Architecture

```
┌────────────────────────────────────────┐
│              YourQL App                │
│                                        │
│  ┌──────────┐  ┌──────────┐  ┌──────┐ │
│  │  MySQL   │  │ Postgres │  │SQLite│ │  ← existing DB drivers (unchanged)
│  │  Driver  │  │  Driver  │  │Driver│ │
│  └──────────┘  └──────────┘  └──────┘ │
│                                        │
│  ┌──────────────────────────────┐     │
│  │         DuckDB (new)         │     │
│  │  • read_csv_auto()           │     │
│  │  • st_read() for Excel       │     │
│  │  • SQL queries on files      │     │
│  │  • Cross-source joins        │     │
│  └──────────────────────────────┘     │
│                                        │
│  ┌──────────────────────────────┐     │
│  │    DataSourceDriver (go)     │     │
│  │    interface unchanged       │     │
│  │    + "file" type             │     │
│  └──────────────────────────────┘     │
└────────────────────────────────────────┘
```

DuckDB replaces nothing. It sits alongside existing DB drivers as the engine for file-based data and cross-source queries.

### New DataSource Type: `file`

```go
type DataSource struct {
    ID                uint   `json:"id"`
    Name              string `json:"name"`
    Type              string `json:"type"` // "mysql" | "postgresql" | "file" | etc.
    
    // Database fields (existing)
    Host              string `json:"host,omitempty"`
    Port              int    `json:"port,omitempty"`
    Database          string `json:"database,omitempty"`
    Username          string `json:"username,omitempty"`
    Password          string `json:"password,omitempty"`
    
    // File fields (new)
    FilePath          string `json:"file_path,omitempty"`   // absolute path to CSV/XLSX
    FileType          string `json:"file_type,omitempty"`   // "csv" | "xlsx"
    
    // ... rest unchanged
}
```

### File Source Driver

```go
// pkg/services/data_file.go (new file)

import "github.com/marcboeker/go-duckdb"

var duckConn *sql.DB // shared DuckDB connection

func InitDuckDB() error {
    home, _ := os.UserHomeDir()
    dbPath := filepath.Join(home, ".yourql", "duckdb_data.db")
    
    conn, err := sql.Open("duckdb", dbPath)
    if err != nil {
        return err
    }
    duckConn = conn
    
    // Install spatial extension for Excel support
    duckConn.Exec("INSTALL spatial; LOAD spatial;")
    return nil
}

type FileSourceDriver struct {
    // implements DataSourceDriver interface
}

func (d *FileSourceDriver) Connect(source *models.DataSource) error {
    // Verify the file exists
    if _, err := os.Stat(source.FilePath); err != nil {
        return fmt.Errorf("file not found: %s", source.FilePath)
    }
    return nil
}

func (d *FileSourceDriver) Introspect(source *models.DataSource) (*Schema, error) {
    var query string
    switch source.FileType {
    case "csv":
        query = fmt.Sprintf("DESCRIBE SELECT * FROM read_csv_auto('%s')", source.FilePath)
    case "xlsx":
        query = fmt.Sprintf("DESCRIBE SELECT * FROM st_read('%s')", source.FilePath)
    }
    rows, err := duckConn.Query(query)
    // ... parse column names, types, return Schema ...
}

func (d *FileSourceDriver) Execute(source *models.DataSource, query string) (*QueryResult, error) {
    // Execute SQL against the file via DuckDB
    // LLM-generated queries use read_csv_auto() or st_read() to reference the file
    rows, err := duckConn.Query(query)
    // ... return results same as other drivers ...
}
```

### LLM Prompt for File Sources

The LLM prompt includes the source type. When type is "file", the schema tells the LLM what columns exist and it generates DuckDB-compatible SQL:

```
Data source: sales_2024.csv (file)
Columns: date (DATE), region (VARCHAR), product (VARCHAR), amount (DECIMAL), quantity (INTEGER)

User question: "What were total sales by region?"

LLM generates:
SELECT region, SUM(amount) as total_sales
FROM read_csv_auto('/path/to/sales_2024.csv')
GROUP BY region
ORDER BY total_sales DESC
```

The Go backend wraps the user's query in the appropriate `read_csv_auto()` or `st_read()` call automatically, so the LLM doesn't need to know file paths — it just writes standard SQL against the schema columns, and the driver inserts the file reference.

### How File Queries Actually Work

The LLM generates standard SQL like:

```sql
SELECT region, SUM(amount) as total FROM data GROUP BY region
```

The `FileSourceDriver.Execute()` method wraps this into a CTE or subquery:

```sql
WITH data AS (
    SELECT * FROM read_csv_auto('/path/to/file.csv')
)
SELECT region, SUM(amount) as total FROM data GROUP BY region
```

This keeps the LLM prompt clean — it never sees file paths or DuckDB-specific functions.

### Frontend Changes

**Create Data Source dialog** — new options in type dropdown:

```
MySQL
MariaDB
PostgreSQL
Redshift (WIP)
SQLite
SQL Server
Snowflake (WIP)
BigQuery (WIP)
─────────────
CSV File
Excel File
```

**File form fields** (replaces host/port/database when type is "csv" or "xlsx"):

```
┌─────────────────────────────────────┐
│ Name: [Sales Data                    ]│
│ Type: [CSV File                    ▾]│
│                                     │
│ File: [Choose File...    ] [Browse] │
│       /Users/bob/sales_2024.csv     │
│                                     │
│ Preview:                            │
│ ┌──────────────────────────────┐    │
│ │ date       │ region  │ amt   │    │
│ │ 2024-01-01 │ West    │ 500   │    │
│ │ 2024-01-02 │ East    │ 320   │    │
│ │ ...                            │    │
│ └──────────────────────────────┘    │
│ 1,234 rows, 5 columns               │
└─────────────────────────────────────┘
```

**Data Source Card display:**

```
DB source:    localhost:5432/mydb
SQLite:       /path/to/database.db
CSV file:     📄 sales_2024.csv (1,234 rows)
Excel file:   📊 budget.xlsx (Sheet1, 567 rows)
```

---

## Build & Bundle Changes

### Go Dependencies

```
go.mod additions:
  github.com/marcboeker/go-duckdb v1.7.0
```

Requires CGO for DuckDB C library:

| Platform | Library | Install |
|----------|---------|---------|
| macOS arm64/amd64 | libduckdb | `brew install duckdb` |
| Linux amd64 | libduckdb-dev | `apt install libduckdb-dev` |
| Windows amd64 | duckdb.dll | Bundle as DLL |

DuckDB can also be statically linked to avoid system dependency. The `go-duckdb` package supports `-tags=duckdb_use_lib` for static builds.

### Linux Build Dockerfile Update

```dockerfile
# Add DuckDB library
RUN apt-get install -y libduckdb-dev
```

### Binary Size Impact

| Component | Size |
|-----------|------|
| DuckDB C library | ~3 MB |
| Go bindings overhead | ~2 MB |
| **Total increase** | **~5 MB** |

---

## Data Source Schema Migration

```sql
-- Step 1: Create new table with file columns
CREATE TABLE data_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'mysql',
    host TEXT DEFAULT '',
    port INTEGER DEFAULT 0,
    database_name TEXT DEFAULT '',
    username TEXT DEFAULT '',
    password TEXT DEFAULT '',
    file_path TEXT DEFAULT '',
    file_type TEXT DEFAULT '',
    ssl_mode TEXT DEFAULT 'disable',
    extra TEXT DEFAULT '{}',
    is_default BOOLEAN DEFAULT FALSE,
    exploration_allowed BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- Step 2: Copy data from old table
INSERT INTO data_sources SELECT 
    id, name, type, host, port, database_name, 
    username, password, '', '',
    ssl_mode, extra, is_default, exploration_allowed,
    created_at, updated_at, deleted_at
FROM db_connections;

-- Step 3: Drop old table
DROP TABLE db_connections;

-- Step 4: Recreate conversations with new FK
ALTER TABLE conversations RENAME TO conversations_old;
CREATE TABLE conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT,
    data_source_id INTEGER REFERENCES data_sources(id),
    llm_provider_id INTEGER REFERENCES llm_providers(id),
    status TEXT DEFAULT 'active',
    max_messages INTEGER DEFAULT 0,
    max_context_messages INTEGER DEFAULT 0,
    pinned BOOLEAN DEFAULT FALSE,
    tech_details BOOLEAN DEFAULT FALSE,
    context_details BOOLEAN DEFAULT FALSE,
    summarize BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);
INSERT INTO conversations SELECT 
    id, title, db_connection_id, llm_provider_id,
    status, max_messages, max_context_messages, pinned,
    tech_details, context_details, summarize,
    created_at, updated_at, deleted_at
FROM conversations_old;
DROP TABLE conversations_old;

-- Repeat for discussion configs
ALTER TABLE discussion_db_configs RENAME TO discussion_data_configs_old;
CREATE TABLE discussion_data_configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER REFERENCES conversations(id),
    data_source_id INTEGER REFERENCES data_sources(id),
    -- ... rest of config columns
);
INSERT INTO discussion_data_configs SELECT * FROM discussion_data_configs_old;
DROP TABLE discussion_data_configs_old;
```

### Go Migration Runner

```go
func runMigrations(db *sql.DB) error {
    var count int
    db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='db_connections'").Scan(&count)
    if count > 0 {
        log.Println("Migrating db_connections → data_sources...")
        // Execute migration SQL above
    }
    return nil
}
```

---

## Summary

| Phase | Go files | Frontend files | New deps | Binary size Δ |
|-------|----------|---------------|----------|---------------|
| A: Rebrand | ~20 | ~5 | None | 0 MB |
| B: File support | ~3 new, ~5 mod | ~2 mod | DuckDB CGO | +5 MB |

**Total: ~5 MB binary size increase. No Python required. SQL handles everything.**