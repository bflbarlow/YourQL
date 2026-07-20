# Files & Python Enhancement Plan

## Overview

Three interrelated changes to YourQL:

1. **Rebrand** "Database" → "Data" throughout the app
2. **Add file sources** — CSV and Excel files as first-class data sources
3. **Python analysis engine** — replace SQL generation with Python for richer analysis

---

## Phase A: Rebrand "Database" → "Data"

### Rationale

"Data source" better describes what users connect: databases, CSV files, Excel workbooks, and future sources (APIs, Parquet, JSON). "Database" implies SQL-only, which won't be true once files are supported.

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
| `handleViewSchema()` | unchanged (still schema) |
| Tab label "Database Connections" | "Data Sources" |
| `db_connection_id` in conversation | `data_source_id` |
| `.db-connection-*` CSS classes | `.data-source-*` |

### Migration Strategy

1. Create migration SQL to rename tables/columns
2. Update all Go code with new names (compile-time safety)
3. Update all frontend code with new names
4. Test migration on existing user data
5. Old builds will break on migrated DB — acceptable for v0.x

---

## Phase B: CSV & Excel File Support via DuckDB

### Why DuckDB

DuckDB is an embedded analytical database (like SQLite for analytics). Key features:

- **Native CSV reader**: `SELECT * FROM read_csv_auto('file.csv')`
- **Native Excel reader**: `SELECT * FROM st_read('file.xlsx')` (via spatial extension)
- **Embedded**: single C library, no server, cross-platform
- **Analytical SQL**: window functions, CTEs, columnar execution — faster than SQLite for analytics
- **Go bindings**: [`github.com/marcboeker/go-duckdb`](https://github.com/marcboeker/go-duckdb) — CGO wrapper, mature
- **Tiny addition**: ~5 MB to binary size

DuckDB replaces neither: it sits alongside existing DB drivers as the engine for file-based data.

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
│  │  • read_excel()              │     │
│  │  • SQL queries on files      │     │
│  └──────────────────────────────┘     │
│                                        │
│  ┌──────────────────────────────┐     │
│  │    DataSourceDriver (go)     │     │
│  │    interface unchanged       │     │
│  │    + "file" type             │     │
│  └──────────────────────────────┘     │
└────────────────────────────────────────┘
```

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
    FilePath          string `json:"file_path,omitempty"`       // absolute path to CSV/XLSX
    FileType          string `json:"file_type,omitempty"`       // "csv" | "xlsx"
    DuckDBTableName   string `json:"duckdb_table_name,omitempty"` // auto-generated table name
    
    // ... rest unchanged
}
```

### File Import Flow

```
1. User clicks "+ Add Data Source" → selects "CSV File" or "Excel File"
2. Native file picker opens (Wails dialog)
3. File path returned to Go backend
4. DuckDB imports file:
   
   // CSV
   db.Exec("CREATE TABLE data_123 AS SELECT * FROM read_csv_auto(?, header=true)", filePath)
   
   // Excel
   db.Exec("CREATE TABLE data_123 AS SELECT * FROM st_read(?, layer='Sheet1')", filePath)

5. Schema introspected:
   
   SELECT column_name, data_type 
   FROM information_schema.columns 
   WHERE table_name = 'data_123'

6. DataSource saved with type="file", FilePath, DuckDBTableName
7. DuckDB table persists for this session (or cached to DuckDB .db file on disk)
```

### DuckDB Lifecycle

- On app start: open/create `~/.yourql/duckdb_data.db`
- On file source creation: import file into DuckDB
- On app close: DuckDB file persists — file data survives restarts
- On file source deletion: `DROP TABLE data_123`
- On file source refresh: re-import from original file (if path still valid)

### Frontend UI Changes

**Create Data Source dialog** — new options in type dropdown:

| Before | After |
|--------|-------|
| MySQL | MySQL |
| MariaDB | MariaDB |
| PostgreSQL | PostgreSQL |
| Redshift (WIP) | Redshift (WIP) |
| SQLite | SQLite |
| SQL Server | SQL Server |
| Snowflake (WIP) | Snowflake (WIP) |
| BigQuery (WIP) | BigQuery (WIP) |
| | **---** |
| | **CSV File** |
| | **Excel File** |

**File form fields** (replaces host/port/database when type is "file"):

```
┌─────────────────────────────────────┐
│ Name: [Sales Data                    ]│
│ Type: [CSV File                    ▾]│
│                                     │
│ File: [Choose File...    ] [Browse] │
│       /Users/bob/sales_2024.csv     │
└─────────────────────────────────────┘
```

### Data Source Card Display

For file sources, the card details line changes:

```
DB source:    localhost:5432/mydb
SQLite:       /path/to/database.db
CSV file:     📄 sales_2024.csv (1,234 rows)
Excel file:   📊 budget.xlsx (Sheet1, 567 rows)
```

### DuckDB Go Integration

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
    // For file sources, data is already in DuckDB
    // Just verify the DuckDB table exists
    var count int
    duckConn.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_name = ?", 
        source.DuckDBTableName).Scan(&count)
    if count == 0 {
        return fmt.Errorf("file data not found")
    }
    return nil
}

func (d *FileSourceDriver) Execute(source *models.DataSource, query string) (*QueryResult, error) {
    // Execute SQL against the DuckDB table
    rows, err := duckConn.Query(query)
    // ... return results same as other drivers
}
```

---

## Phase C: LLM Prompt Changes — SQL → Python

### Current Prompt (SQL-focused)

```
You are a SQL expert. Given the following database schema:
{schema}

User question: {question}

Write a SQL query that answers the question.
Return ONLY the SQL query, no explanation.
```

### New Prompt (Python-focused)

```
You are a data analyst. Given the following data source schema:
{schema}

Data source type: {type}  // "mysql", "file_csv", "file_xlsx", etc.

User question: {question}

Write a Python script using pandas that answers the question.
The data is already loaded in a DataFrame called `df`.
Return ONLY the Python code, no explanation.

Rules:
- Use pandas operations only (no SQL)
- Assign the final result to a variable called `result`
- For tables, result should be a DataFrame
- For summary stats, result can be a dict or DataFrame
- Keep it concise
```

### LLM Output Examples

**Question**: "What were total sales by region last quarter?"

```python
import pandas as pd

# df already contains the data
df['date'] = pd.to_datetime(df['date'])
last_q = df[df['date'] >= '2024-01-01']
result = last_q.groupby('region')['sales'].sum().reset_index()
result.columns = ['Region', 'Total Sales']
```

**Question**: "Show me a summary of customer ages"

```python
result = df['age'].describe().reset_index()
result.columns = ['Statistic', 'Value']
```

**Question**: "Are there any duplicate orders?"

```python
dupes = df[df.duplicated(subset=['order_id'], keep=False)]
result = dupes.sort_values('order_id')
```

### Prompt Granularity

The prompt includes:
- **Schema**: column names + types (same as today, from schema introspection)
- **Data source type**: lets the LLM know whether it's a DB or file
- **Sample rows**: first 5 rows of data (helps LLM understand content)
- **Context**: previous messages in the conversation

---

## Phase D: Pyodide — Python in the Browser

### Why Pyodide

Pyodide is CPython compiled to WebAssembly. It runs in the browser — the same WebView your app already uses. No Python installation required on the user's machine.

| Feature | Value |
|---------|-------|
| Size | ~9 MB (pyodide.js + core) |
| Packages | pandas, numpy, matplotlib, openpyxl all available |
| Speed | ~3-5x slower than native Python (acceptable for analysis) |
| Memory | Limited to browser memory (~512 MB typical) |
| Integration | Runs in WebView you already have |

### Architecture

```
┌──────────────────────────────────────────────────┐
│                   Frontend (Svelte)               │
│                                                   │
│  ┌─────────────┐     ┌──────────────────────┐    │
│  │  LLM returns │────▶│ Pyodide Web Worker   │    │
│  │  Python code │     │                      │    │
│  └─────────────┘     │  import pandas as pd │    │
│                       │  df = load_data()    │    │
│  ┌─────────────┐     │  result = ...        │    │
│  │  Go backend  │────▶│                      │    │
│  │  (DB queries │     │  return result.to_   │    │
│  │   or DuckDB) │     │  json()              │    │
│  └─────────────┘     └──────────┬───────────┘    │
│                                  │                │
│  ┌───────────────────────────────▼───────────┐   │
│  │         Result Renderer                   │   │
│  │  • DataTables (tables)                    │   │
│  │  • Charts (matplotlib → canvas)           │   │
│  │  • Summary cards (dicts)                  │   │
│  └───────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

### Integration Code (Frontend)

```javascript
// frontend/src/lib/pyodide.js

let pyodide = null;

export async function initPyodide() {
    if (pyodide) return pyodide;
    
    // Load Pyodide from CDN (or bundle in assets)
    pyodide = await loadPyodide({
        indexURL: "https://cdn.jsdelivr.net/pyodide/v0.25.0/full/"
    });
    
    // Load required packages
    await pyodide.loadPackage(['pandas', 'numpy', 'matplotlib']);
    
    return pyodide;
}

export async function executePython(code, data) {
    const py = await initPyodide();
    
    // Inject data as a pandas DataFrame
    const dataJson = JSON.stringify(data);
    py.runPython(`
import pandas as pd
import json

# Load data provided by Go backend
_data = json.loads('''${dataJson}''')
df = pd.DataFrame(_data['rows'], columns=_data['columns'])
    `);
    
    // Run user's analysis code
    const result = py.runPython(`
${code}

# Convert result to JSON
if isinstance(result, pd.DataFrame):
    result.to_json(orient='records')
elif isinstance(result, dict):
    json.dumps(result)
else:
    json.dumps({'value': str(result)})
    `);
    
    return JSON.parse(result);
}
```

### Data Flow for Analysis

```
1. User asks question
2. LLM generates Python
3. Go backend executes SQL (DB sources) or DuckDB query (file sources)
4. Query results returned to frontend as JSON
5. Frontend passes JSON + Python code to Pyodide
6. Pyodide loads JSON into pandas DataFrame
7. Executes user's Python analysis
8. Returns result (DataFrame, dict, chart) to frontend
9. Frontend renders result in conversation view
```

### For Database Sources (No Python Needed for Data Fetching)

```
User: "Show me sales by region"
  ↓
LLM: "OK, the data has columns [date, region, sales, product]. I'll write a SQL query."
  ↓
LLM generates SQL: SELECT region, SUM(sales) as total FROM orders GROUP BY region
  ↓
Go backend executes SQL against PostgreSQL/MySQL/etc.
  ↓
Results come back as rows: [{region: "West", total: 50000}, ...]
  ↓
Go sends rows to frontend
  ↓
Frontend passes to Pyodide? NO — for simple aggregations, render directly
  ↓
BUT if user asks "do a regression analysis on sales trends" → THAT goes to Pyodide
```

### When to Use Python vs SQL

| Question Type | Engine | Example |
|--------------|--------|---------|
| Simple aggregation | SQL/DuckDB | "Total sales by region" |
| Filter + sort | SQL/DuckDB | "Top 10 customers by spend" |
| Join across tables | SQL/DuckDB | "Orders with customer names" |
| Statistical analysis | **Python** | "Correlation between price and sales" |
| Data transformations | **Python** | "Normalize the date column to quarters" |
| Regression | **Python** | "Predict next month's sales" |
| Charts | **Python** | "Plot sales over time" |
| Pivot tables | Either | "Sales by region by quarter" |

### LLM Decides Engine

Update the system prompt so the LLM chooses:

```
You are a data analyst. Choose the best approach:

For simple queries (filter, sort, group, join):
  → Write SQL
  
For analysis (statistics, regression, complex transformations, charts):
  → Write Python

Indicate your choice with a tag: [SQL] or [PYTHON]
```

---

## Phase E: Rich Output Rendering

### Result Types

| Type | Renderer | Example |
|------|----------|---------|
| `DataFrame` (rows ≤ 20) | HTML table (existing) | "Top 10 customers" |
| `DataFrame` (rows > 20) | Scrollable DataTable with search | "All orders this month" |
| `dict` (summary stats) | Key-value card grid | "describe()" output |
| `matplotlib.figure` | Canvas-rendered chart | "Sales trend plot" |
| `str` (single value) | Large number card | "Total revenue: $1.2M" |
| `list` (of values) | Bullet list or bar chart | "Unique regions" |

### Chart Rendering

```python
# LLM generates this Python
import matplotlib.pyplot as plt
import io, base64

fig, ax = plt.subplots(figsize=(8, 4))
result.groupby('region')['sales'].sum().plot(kind='bar', ax=ax)
ax.set_title('Sales by Region')

buf = io.BytesIO()
fig.savefig(buf, format='png', dpi=100, bbox_inches='tight')
buf.seek(0)
chart = base64.b64encode(buf.read()).decode()
```

Frontend receives base64 PNG and renders as `<img>`.

### Existing LLM Output (Today)

```
Current: SQL text → Go executes → table rows → render table

File import: SQL text → Go executes DuckDB → table rows → render table

Python analysis: Python text → Pyodide executes → DataFrame/chart → render
```

All three flows converge on the same result renderer, which already handles tables. Only charts are net-new.

---

## Build & Bundle Changes

### Go Dependencies Added

```
go.mod additions:
  github.com/marcboeker/go-duckdb v1.7.0
```

Requires CGO for DuckDB C library. Build verified on all platforms:

| Platform | CGO lib | Notes |
|----------|---------|-------|
| macOS arm64 | duckdb (Homebrew) | `brew install duckdb` |
| macOS amd64 | duckdb (Homebrew) | Same |
| Linux amd64 | libduckdb | `apt install libduckdb-dev` |
| Windows amd64 | duckdb.dll | Bundled as DLL |

DuckDB can also be statically linked to avoid system dependency. The `go-duckdb` package supports `-tags=duckdb_use_lib` for static builds.

### Pyodide Distribution

Option 1: **CDN** (development default)
```html
<script src="https://cdn.jsdelivr.net/pyodide/v0.25.0/full/pyodide.js"></script>
```

Option 2: **Bundled** (production)
- Download Pyodide (~25 MB full, ~9 MB core)
- Include in `frontend/public/pyodide/`
- Wails embeds in `frontend/dist/` → embedded in binary via `//go:embed`
- Adds ~9-25 MB to binary size

Recommendation: bundle core Pyodide + pandas + numpy. The total is ~15 MB compressed, acceptable for a desktop app.

### Linux Build Dockerfile Update

```dockerfile
# Add DuckDB library
RUN apt-get install -y libduckdb-dev
```

---

## Data Source Schema Cleanup

### SQLite Migration for Table Rename

```sql
-- Step 1: Create new table
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
    duckdb_table_name TEXT DEFAULT '',
    ssl_mode TEXT DEFAULT 'disable',
    extra TEXT DEFAULT '{}',
    is_default BOOLEAN DEFAULT FALSE,
    exploration_allowed BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- Step 2: Copy data
INSERT INTO data_sources SELECT *, '', '', '' FROM db_connections;

-- Step 3: Drop old table
DROP TABLE db_connections;

-- Step 4: Recreate conversations with new FK
ALTER TABLE conversations RENAME TO conversations_old;
CREATE TABLE conversations (
    -- ... same columns ...
    data_source_id INTEGER REFERENCES data_sources(id),
    -- ... rest ...
);
INSERT INTO conversations SELECT * FROM conversations_old;
DROP TABLE conversations_old;

-- Repeat for discussion_data_configs
```

### Go Migration Runner

Add to `main.go` or `app.go`:

```go
func runMigrations(db *sql.DB) error {
    // Check if old table exists
    var count int
    db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='db_connections'").Scan(&count)
    if count > 0 {
        // Run rename migration
        log.Println("Migrating db_connections → data_sources...")
        // ... execute migration SQL ...
    }
    return nil
}
```

---

## Summary of All Changes

| Phase | Go files | Frontend files | New deps | Binary size Δ | Effort |
|-------|----------|---------------|----------|---------------|--------|
| A: Rebrand | ~20 | ~5 | None | 0 MB | 2-3 hours |
| B: File support | ~3 new, ~5 mod | ~2 mod | DuckDB CGO | +5 MB | 1 day |
| C: LLM prompts | ~1 mod | 0 | None | 0 MB | 1 hour |
| D: Pyodide | 0 | ~2 new, ~2 mod | Pyodide WASM | +15 MB | 1-2 days |
| E: Rich output | 0 | ~2 mod | matplotlib (bundled) | +2 MB | 1 day |

**Total: ~25 MB binary size increase, ~4-5 days of work for full implementation.**