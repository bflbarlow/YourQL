# TOTAL_EXPORT.md — Full Database Export Enhancement

> **Date:** 2026-08-07
> **Project:** YourQL — Export the entire `~/.yourql/yourql.db` to Excel or CSV
> **Status:** Design specification. Implementation pending.

---

## 1. Motivation

YourQL stores all user data in a single local SQLite database at
`~/.yourql/yourql.db`. This database contains:

| Table | What it holds |
|---|---|
| `llm_providers` | Provider names, models, API keys, configs |
| `data_sources` | Database connections, hostnames, credentials, file paths |
| `conversations` | All chat threads, settings, statuses |
| `conversation_messages` | Every message (user, assistant, system, exploration), rendered HTML content, LLM raw responses, SQL results |
| `conversation_skills` | Which skills are active per conversation |
| `skills` | User-authored Markdown prompt fragments |
| `queries` | Query execution log (SQL, timing, token counts, errors) |
| `app_settings` | Theme, accent, scale preferences |
| `discussion_defaults` | Defaults for new conversations |
| `agent_loop_config` | Custom prompts, tool descriptions, safety rules |
| `schema_migrations` | Migration tracking (reference only — read-only export) |

Currently there is no way to export this data in bulk. Users who want to:

- **Back up** their entire YourQL configuration and history before a major version
  upgrade
- **Migrate** to a different machine
- **Inspect** their usage patterns, query history, or conversation logs outside the
  app
- **Audit** which skills, prompts, and connection configs they have set up

…must copy the raw `yourql.db` SQLite file manually. For non-technical users this
is opaque, and even for technical users there's no way to browse the data without a
SQLite client. A built-in export gives users ownership of their data in a format
they can immediately use.

### What This Is NOT

- This is **not** exporting data from a connected data source (MySQL, PostgreSQL,
  etc.). That's already handled by per-result-table CSV downloads in the
  conversation view.
- This is **not** a conversation-level export. That's the `EXPORT_ENHANCEMENT.md`
  feature (Print to PDF, Export as HTML, Export as Markdown per conversation).
- This is a **full-database export** of the app's own internal SQLite database
  — the metadata that powers the app itself.

---

## 2. Design

### 2.1 Formats

| Format | Extension | What it produces |
|---|---|---|
| **Excel** | `.xlsx` | Single workbook with one sheet per table. Primary format. |
| **CSV** | `.csv` (zipped) | One `.csv` file per selected table, bundled into a single `.zip`. Secondary format. Supports per-table selection (§2.4.1) — the only format that does. |

Excel is the primary target because:
- It maps naturally to "one table per sheet" — clean and browsable without a
  database tool.
- The `excelize/v2` library is already a project dependency (used for data file
  ingestion of `.xlsx` files).
- CSV is useful for users who want plain-text files they can feed into other tools,
  but flattening 11 relational tables into a single CSV is nonsensical and
  producing 11 separate files is awkward. Offering both with Excel as the primary
  keeps the UX clean.

### 2.2 Entry Point

**Placement: Settings → General.** Add a new `.form-card` section to the
existing **General Settings** tab (`activeSettingsTab === 'general'` in
`SettingsView.svelte`), alongside the existing Theme, Accent Color, UI Scale,
and Advanced cards. This tab is already the home for application-wide
preferences that aren't tied to a specific conversation or data source —
export is squarely in that category, and putting it there keeps the About
page focused on "what is this app / is it up to date" rather than becoming a
catch-all settings surface.

The export action must show a save dialog (`runtime.SaveFileDialog`) so the
user picks the destination — both Excel and CSV target a single file (a
`.xlsx` or a `.zip` respectively, see §2.4.2), matching the pattern used by
`ExportConversationHTML` and `ExportConversationMarkdown`.

### 2.3 Excel Workbook Structure

Each table in `~/.yourql/yourql.db` maps to one worksheet in the `.xlsx` workbook:

| Sheet name | Table | Notes |
|---|---|---|
| `providers` | `llm_providers` | Excludes `api_key` column by default (see §3) |
| `data_sources` | `data_sources` | Excludes `password`, `auth_config`, and `extra` columns by default (see §3) |
| `conversations` | `conversations` | All columns; foreign keys resolve to names where possible |
| `messages` | `conversation_messages` | Full content, tool transcripts, SQL results |
| `queries` | `queries` | Execution log with SQL, timing, error categories |
| `skills` | `skills` | Markdown content included |
| `conversation_skills` | `conversation_skills` | Join table — maps skill IDs to conversation IDs |
| `app_settings` | `app_settings` | Key-value pairs (theme, accent, scale) |
| `discussion_defaults` | `discussion_defaults` | Defaults for new conversations |
| `agent_loop_config` | `agent_loop_config` | Custom prompt/instruction overrides |
| `schema_migrations` | `schema_migrations` | Reference — list of applied migrations with timestamps |

**Timestamp columns** (`created_at`, `updated_at`, `deleted_at`, `applied_at`) are
formatted as ISO 8601 strings — the same format already used in the database and
parsable by any spreadsheet tool.

**Boolean columns** (`is_default`, `is_active`, `pinned`, `tech_details`,
`summarize`, `viz_enabled`) are written as `1`/`0` (matches storage) and the sheet
header row includes a comment or note indicating the convention.

**NULL values** are rendered as empty cells.

#### Enrichment (nice to have)

Foreign key IDs can optionally be accompanied by a human-readable name column for
the most commonly referenced lookups:

- `llm_provider_id` → `llm_provider_name` column added next to it (resolved from
  `llm_providers`)
- `data_source_id` → `data_source_name` column added next to it (resolved from
  `data_sources`)
- `conversation_id` → `conversation_title` (resolved from `conversations`)

This is a "nice to have" — if it adds meaningful complexity, skip it and stick to
raw IDs. Users can VLOOKUP between sheets.

### 2.4 CSV Export

#### 2.4.1 Table Selection

**CSV export lets the user choose which tables to export**, unlike the Excel
path which always includes every table in one workbook. This is the one
meaningful asymmetry between the two formats in this spec, and it exists
for a concrete reason: Excel's "one workbook, one sheet per table" model
means the marginal cost of including an extra sheet is near zero — the user
just ignores sheets they don't care about. CSV's "one file per table" model
has the opposite property: every included table is a separate file the user
now has to deal with, so giving them control over which files actually get
produced is meaningfully more useful here than it would be for Excel.

The table selection UI is a checklist of the 11 tables (see §1), all checked
by default, with a "Select All" / "Select None" convenience toggle. Useful
groupings worth pre-defining as one-click presets (nice to have, not
required for v1):

| Preset | Tables included |
|---|---|
| **Everything** (default) | All 11 tables |
| **Conversations only** | `conversations`, `conversation_messages`, `conversation_skills` |
| **Configuration only** | `llm_providers`, `data_sources`, `skills`, `app_settings`, `discussion_defaults`, `agent_loop_config` |

If the user deselects every table, disable the Export button rather than
producing an empty zip.

**Table selection does not change the credential-handling behavior in §3.**
Whether a given table is included or excluded is orthogonal to whether
`api_key`/`password`/`auth_config`/`extra` are stripped from the tables that
*are* included — both controls are independent checkboxes in the same form.

#### 2.4.2 Output Packaging

**CSV export produces a single `.zip` file** containing one `.csv` per
selected table plus a `README.txt` listing which tables were included
(and, if the user didn't select everything, which were skipped). This was
weighed against saving loose `.csv` files into a user-picked directory
(via `runtime.OpenDirectoryDialog`), but the zip is strictly better UX — one
file to save, move, or share, and it uses the same `runtime.SaveFileDialog`
flow already used everywhere else in the export feature (Excel, conversation
HTML/Markdown exports), so there's no second dialog type or code path to
maintain. `runtime.OpenDirectoryDialog` is not used by this feature.

CSV encoding:
- UTF-8 with BOM (Excel on Windows reads it correctly)
- Header row with column names
- All cells quoted if they contain commas, newlines, or double quotes
- NULL represented as empty (no quoting)

### 2.5 Export Metadata

Include an `export_info` sheet (Excel) or `export_info.txt` (CSV zip) with:

| Field | Value |
|---|---|
| `exported_at` | ISO 8601 timestamp |
| `app_version` | Running YourQL version at time of export |
| `db_tables` | Count of tables exported |
| `db_rows_total` | Total row count across all tables |
| `credentials_included` | `true` or `false` (see §3) |

---

## 3. Security: Sensitive Data Handling

The `yourql.db` stores several categories of sensitive data in plaintext:

| Column | Table | Sensitivity |
|---|---|---|
| `api_key` | `llm_providers` | LLM provider API keys (OpenAI, Anthropic, etc.) |
| `password` | `data_sources` | Database user passwords |
| `auth_config` | `data_sources` | OAuth2 token JSON for Google Sheets / BigQuery |
| `extra` | `data_sources` | Driver-specific JSON blob — **for BigQuery connections this includes `service_account_key`, a full GCP service account key.** See below. |

**`extra` is not a fixed-shape column — it's driver-specific JSON, and it is easy
to miss a secret hiding inside it.** Concretely, `pkg/services/db_bigquery.go`
defines:

```go
type BigQueryExtra struct {
	ProjectID         string `json:"project_id"`
	Dataset           string `json:"dataset,omitempty"`
	ServiceAccountKey string `json:"service_account_key,omitempty"` // JSON key content
}
```

A GCP service account key is arguably **more dangerous than a database
password** — it can grant broad cloud API access well beyond the single
BigQuery dataset the user configured in YourQL. A "safe by default" export
that forgets to strip `extra` would leak this key verbatim in the
`data_sources` sheet, defeating the entire purpose of §3.1.

**Implementation requirement, not just a hardcoded column list:** because
`extra` (and potentially other JSON blob columns added by future drivers)
can carry secrets under driver-specific key names, the default export path
must not rely solely on a static list of "sensitive columns." In addition to
unconditionally excluding `api_key`, `password`, `auth_config`, and `extra`
by column name, the export code should defensively scan any remaining JSON
blob columns (`config`, and any future driver-specific JSON field) for keys
whose names match a secret-shaped pattern (case-insensitive substring match
on `key`, `token`, `secret`, `password`, `credential`, `service_account`) and
redact those values too, rather than passing the blob through untouched.
This is a defense-in-depth measure — the explicit column exclusions above are
the primary control, and the JSON-key scan is the backstop for what the
column allowlist inevitably misses as new drivers are added.

These must be handled with care during export.

### 3.1 Default: Exclude (Redacted Export)

**The default export excludes these columns entirely.** The exported sheets
for `providers` and `data_sources` simply omit the `api_key`, `password`,
`auth_config`, and `extra` columns, plus any keys inside `config` (or other
JSON blob columns) that match the secret-shaped pattern described above. The
`export_info` metadata shows `credentials_included: false`.

This is the safe default. Most users exporting for backup, audit, or sharing
purposes do not need raw credentials in the export file, and silently including
them in an `.xlsx` that the user then emails or stores in cloud storage is a trust
violation.

### 3.2 Opt-in: Include (Full Export)

Add a checkbox / toggle in the export UI labeled **"Include credentials (API keys
and passwords)"** — unchecked by default. Toggling it requires an explicit
acknowledgment (e.g., the checkbox label changes to include a warning: "⚠️ This
will include your API keys and database passwords in plaintext" and the "Export"
button text changes to "Export with Credentials").

When checked:
- `api_key`, `password`, `auth_config`, and `extra` columns are included in full,
  with no JSON-key redaction applied.
- `export_info` metadata shows `credentials_included: true`.
- A warning banner is displayed above the exported data saying "⚠️ This export
  contains plaintext credentials — handle with care."

### 3.3 Security Review

Per `AGENT_READ_FIRST.md`:

| Principle | Compliance |
|---|---|
| Read-only invariant (§0) | ✅ Export is read-only — no writes to any data source or to `yourql.db` |
| Never log API keys or passwords (§3.5) | ✅ Backend export code never logs column values — it streams directly to the file |
| Data safety | ✅ Credentials excluded by default; opt-in requires explicit acknowledgment |
| User trust | ✅ Transparency about what's included; metadata reflects the choice |

**Risk:** User accidentally emails or uploads an export that includes credentials
because they clicked through the warning without reading it.

**Mitigations:**
- The opt-in requires a separate, explicit action (checkbox + different button
  label), not a default that can be accidentally triggered.
- The `export_info` sheet includes `credentials_included: true` — automated
  scanners or downstream processes can detect this and raise an alert.
- The warning is part of the checkbox label, not a one-time dialog that users
  habitually click past.

---

## 4. Implementation

### 4.1 Backend: `pkg/services/total_export.go`

A new service file with two exported functions:

```go
// ExportDatabaseXLSX writes the full yourql.db to an Excel workbook at the
// given path. If includeCredentials is false, api_key, password, auth_config,
// and extra columns are stripped, plus any secret-shaped keys inside
// remaining JSON blob columns (see §3).
func ExportDatabaseXLSX(path string, includeCredentials bool) error

// ExportDatabaseCSV writes the selected tables from yourql.db into a .zip at
// the given path, one .csv file per selected table plus a README.txt. If
// tables is empty, all tables are included (defensive fallback — the
// frontend should never actually send an empty selection, see §2.4.1). If
// includeCredentials is false, the same columns are stripped from whichever
// tables were selected.
func ExportDatabaseCSV(zipPath string, tables []string, includeCredentials bool) error
```

Both functions:
1. Query every table via `models.DB` using `SELECT *` (or explicit column lists
   when credentials are excluded)
2. Write the export metadata (timestamp, version, credentials flag)
3. Format and stream rows into the target file — never hold the full dataset in
   memory

For Excel, use `excelize/v2`:
- `excelize.NewFile()` creates a workbook
- `File.SetSheetRow(sheet, cell, &rowSlice)` per row (this is the actual
  `excelize/v2` API — there is no `Sheet.WriteRow()` method)
- Rename the default `Sheet1` to the first table name, then `NewSheet()` for each
  subsequent table
- **Write to a temp path, then rename on success.** `File.SaveAs()` and
  `File.Write()` do not do this internally — verified against the vendored
  `excelize/v2` API surface, neither method writes to a temp file and renames
  atomically. If the export is interrupted mid-`SaveAs` (crash, disk full,
  process killed), a truncated, corrupt `.xlsx` will be left sitting at the
  user's chosen path. The service function itself must implement the
  temp-file-then-rename pattern: call `SaveAs()` against a temp path in the
  same directory (e.g. `<target>.tmp`), and only `os.Rename()` it into the
  final target path after `SaveAs()` returns successfully. This mirrors the
  same atomic-replacement principle already required for the auto-updater in
  `VERSION_UPGRADE.md`.

For CSV, use standard library `encoding/csv`:
- One `csv.Writer` per table, created fresh for each `.csv` file
- Write to `dirPath/<table_name>.csv`
- Package all CSVs + `export_info.txt` into a `.zip` using `archive/zip` and
  return the `.zip` path

### 4.2 Wails Bindings: `app.go`

```go
// ExportDatabase exports the full YourQL database to an Excel workbook, or
// the selected tables to a CSV .zip. tables is ignored when format is "xlsx"
// (Excel export always includes every table — see §2.4.1); when format is
// "csv", an empty tables slice is treated as "export everything."
// Returns an empty string on success or user-cancel, or an error message string
// on failure. This matches the established convention of the existing export
// bindings (ExportConversationHTML, ExportConversationMarkdown in app.go) —
// both return string rather than error, specifically because a cancelled save
// dialog (SaveFileDialog returning path == "") must be treated as "nothing
// happened," not as a real error, and the string-return convention makes that
// distinction explicit and consistent across every export binding in the app.
func (a *App) ExportDatabase(format string, tables []string, includeCredentials bool) string
```

- `format` is `"xlsx"` or `"csv"`.
- `tables` is the list of table names to include — relevant for CSV only (see
  §2.4.1); pass `nil`/empty for Excel, or for "export everything" on CSV.
- Opens a `runtime.SaveFileDialog` for both formats — CSV now targets a
  single `.zip` file (per §2.4.2's Option B), so both paths use the same
  dialog type; `runtime.OpenDirectoryDialog` is not needed once Option B is
  the only packaging choice.
- Calls the appropriate service function.
- On success or user-cancel, returns `""`.
- On failure, returns a descriptive error string (disk full, permission denied,
  etc.) — not a Go `error`, consistent with the sibling export bindings.

**File dialogs must be triggered from the Go side** because `runtime.SaveFileDialog`
is a Wails runtime function, not available from the service layer. The binding
method in `app.go` handles the dialog flow, then delegates to the service.

### 4.3 Frontend: `SettingsView.svelte`

Add a new `.form-card` section to the existing **General Settings** tab
(`activeSettingsTab === 'general'`), following the same card pattern as the
adjacent Theme / Accent Color / UI Scale / Advanced cards, with:

- A dropdown or radio group for format: **Excel (.xlsx)** | **CSV (.zip)**
- **When CSV is selected:** a table checklist appears (§2.4.1) — all 11
  tables checked by default, a "Select All" / "Select None" toggle, and
  optionally the preset buttons (Everything / Conversations only /
  Configuration only). This checklist is hidden entirely when Excel is
  selected, since Excel export always includes every table.
- The credentials checkbox: **"Include credentials (API keys and passwords)"** —
  unchecked by default, with the warning label pattern from §3.2
- An **"Export"** button — disabled if CSV is selected and zero tables are
  checked
- A status area showing progress (for large databases this may take a moment —
  emit a phase event)

**State flow:**

```
Idle → select format → (if CSV) select tables → click "Export" → save dialog
  → ExportDatabase(format, tables, includeCredentials)
  → backend writes file → UI shows "Exported to <path>" → Idle
```

Error states: permission denied, disk full, database read error, zero tables
selected (CSV only — caught client-side before the call, per above). All
surfaced as the standard error state in the export section.

### 4.4 Files to Create or Modify

| File | Action |
|---|---|
| `pkg/services/total_export.go` | **New** — service functions |
| `app.go` | **Modify** — add `ExportDatabase` binding |
| `frontend/src/SettingsView.svelte` | **Modify** — add export `.form-card` to the General Settings tab, including the CSV table checklist |
| `documentation/TOTAL_EXPORT.md` | **New** — this document |

### 4.5 Dependencies

No new dependencies. `excelize/v2` is already in `go.mod`. Standard library
handles CSV and zip.

---

## 5. UX Design

### 5.1 Export Section (General Settings)

**Format = Excel:**

```
┌─────────────────────────────────────────────────────────┐
│ Export Your Data                                         │
│                                                         │
│ Download a complete copy of your YourQL database —       │
│ all conversations, settings, skills, and configurations  │
│ in a portable format you can open in any spreadsheet app.│
│                                                         │
│ Format:  [Excel (.xlsx) ▼]                              │
│                                                         │
│ [ ] Include credentials (API keys and passwords)         │
│     ⚠️ This will include your API keys and database      │
│     passwords in plaintext. Do not share this file.       │
│                                                         │
│ [  Export Database  ]                                    │
│                                                         │
│ ✓ Exported to /Users/anon/Desktop/YourQL-export.xlsx     │
└─────────────────────────────────────────────────────────┘
```

**Format = CSV** (a table checklist replaces the implicit "everything"):

```
┌─────────────────────────────────────────────────────────┐
│ Export Your Data                                         │
│                                                         │
│ Format:  [CSV (.zip) ▼]                                 │
│                                                         │
│ Tables to export:      [Select All] [Select None]        │
│   [✓] conversations          [✓] queries                │
│   [✓] conversation_messages  [✓] app_settings            │
│   [✓] conversation_skills    [✓] discussion_defaults     │
│   [✓] llm_providers          [✓] agent_loop_config       │
│   [✓] data_sources           [✓] schema_migrations       │
│   [✓] skills                                            │
│                                                         │
│ [ ] Include credentials (API keys and passwords)         │
│     ⚠️ This will include your API keys and database      │
│     passwords in plaintext. Do not share this file.       │
│                                                         │
│ [  Export Database  ]                                    │
│                                                         │
│ ✓ Exported to /Users/anon/Desktop/YourQL-export.zip      │
└─────────────────────────────────────────────────────────┘
```

### 5.2 States

| State | What the user sees |
|---|---|
| **Ready** | The export form as shown above. "Export Database" button is enabled. |
| **In progress** | Button shows "Exporting…" and is disabled. A small spinner or progress indicator. |
| **Success** | "✓ Exported to `<path>`" message below the button. Button re-enabled for another export. |
| **Error** | "⚠️ Failed to export: `<reason>`" — with specific reason (disk full, permission denied, etc.). |
| **Credentials warning shown** | Only when checkbox is checked — the warning text appears below the checkbox (not inline, so it's harder to miss). |
| **CSV table checklist shown** | Only when format = CSV. Switching back to Excel hides it (Excel always exports everything). |
| **Zero tables selected (CSV only)** | "Export Database" button is disabled; no error message needed since the disabled state is itself the feedback. |

---

## 6. Implementation Plan

| Phase | What | Effort |
|---|---|---|
| 1 | **Backend:** `ExportDatabaseXLSX()` — Excel workbook with one sheet per table, credential stripping, metadata sheet | Medium |
| 2 | **Backend:** `ExportDatabaseCSV()` — CSV-per-selected-table in a zip, accepts a `tables []string` selection (§2.4.1), same credential-stripping logic as Phase 1 | Small (largely same logic, different writer, plus the selection filter) |
| 3 | **Wails binding:** `ExportDatabase()` in `app.go` with save dialog integration | Small |
| 4 | **Frontend:** Export `.form-card` on the General Settings tab — format selector, CSV table checklist (with Select All/None), credentials toggle, status states | Small |
| 5 | **Testing:** Export a database with real data, verify all tables, verify partial-table CSV selections produce only the chosen files, verify credential stripping, open in Excel/Numbers/Google Sheets | Small |

Total effort: **1–2 work sessions.** Phases 2–4 can be done in a single session;
Phase 1 is the bulk of the work.

---

## 7. Risk Assessment

Per `AGENT_READ_FIRST.md` §4.

| Priority | Impact |
|---|---|
| **#1 Correctness** | **None.** Export is pure read-only — no data is transformed, the LLM pipeline and SQL execution are untouched. |
| **#2 Safety** | **None.** Export reads from `~/.yourql/yourql.db` (the app's own local database, which is fully mutable by design per §0). No data source is touched. The only risk vector is credentials being exported inadvertently — mitigated by §3. |
| **#3 Reliability** | **Low risk.** Export function is a single-shot operation. If it fails, the file is incomplete and the error is surfaced. No partial state is left behind that could confuse the user. |
| **#4 Appeal / Comfort** | **Improved.** Users feel ownership of their data — export is a trust signal. |

### Failure Modes

1. **Excel row limit exceeded.** Excel sheets max at 1,048,576 rows. The
   `conversation_messages` table is the only one that could plausibly approach
   this (a very heavy user with many long conversations). Mitigation: check row
   count before writing; if any table exceeds 1M rows, split across multiple
   sheets (e.g., `messages_1`, `messages_2`). In practice, this is extremely
   unlikely — 1M messages would require years of heavy daily use.

2. **Disk full during export.** `excelize/v2`'s `SaveAs()`/`Write()` do **not**
   write to a temp file and rename atomically on their own — verified against
   the library's API surface. Left unhandled, a disk-full or interrupted export
   would leave a truncated, corrupt `.xlsx` at the user's chosen path.
   Mitigation: the service function must implement temp-file-then-rename
   itself (see §4.1) — `SaveAs()` against `<target>.tmp` in the same
   directory, then `os.Rename()` into the final path only after `SaveAs()`
   succeeds. For CSV, the zip is built in memory then flushed in one write; if
   the flush fails, the target path is untouched because nothing was ever
   written to it.

3. **Large message content causes slow export.** Some `conversation_messages`
   rows contain full HTML rendering + LLM content + SQL results + tool
   transcripts — multiple kilobytes per row. Mitigation: stream rows one at a
   time rather than loading all into memory; Excelize's `SetSheetRow` with array
   writes is efficient for this.

### Decision

**Proceed.** Risk is minimal — export is additive, read-only against the app's own
SQLite database, and uses an already-present dependency. The credential-stripping
default and explicit opt-in warning address the primary safety concern. The feature
has clear user value and a small implementation surface (~300–400 lines of Go, ~60
lines of Svelte).

---

## 8. Charter Compliance

Per `AGENT_READ_FIRST.md`:

- [ ] **Is this change aligned with the fundamental goal?** Yes — export is a
      read-only operation that serves the user's ownership of their data. It does
      not affect answer accuracy or delivery.
- [ ] **Read-only invariant preserved?** Yes. The export reads from `yourql.db`
      (the app's own local database), not from any configured data source.
- [ ] **SQL execution safety preserved?** Yes — SQL execution is not involved
      in export.
- [ ] **Migration safety preserved?** Yes — no schema changes needed.
- [ ] **API keys/passwords not logged?** Yes — the export code streams data to file
      without logging cell values.
- [ ] **Existing conversations backward compatible?** Yes — no schema changes.
- [ ] **UI responsive?** Export is a fast, one-shot operation (<2 seconds for a
      typical database with hundreds of conversations). A progress indicator is
      provided for edge cases.
- [ ] **Dark/light mode?** The new UI section uses existing CSS variable tokens
      and works in both themes.
- [ ] **Documentation updated?** This document is the spec.

---

## 9. Future Considerations

- **Selective export for Excel:** §2.4.1 already brings table selection into
  v1 for CSV. Excel currently always includes every table as separate sheets
  (§2.1's rationale: near-zero marginal cost per sheet). If usage shows people
  want a filtered Excel export too, extend the same `tables []string`
  parameter to `ExportDatabaseXLSX()` — the plumbing already exists.
- **Restore / import:** The inverse operation — load a previously exported Excel
  or CSV back into `yourql.db`. This is a significantly harder feature (foreign
  key reconciliation, conflict handling, migration compatibility) and should be
  a separate enhancement document.
- **Scheduled auto-backup:** Periodic export triggered by the app, targeting a
  user-configured directory. Out of scope for v1 — keep it manual and explicit.
- **Compressed SQLite dump:** For power users, an option to export a direct
  `.sql` dump or a byte-for-byte copy of `yourql.db`. This already works by
  copying the file — documenting that in the export UI is sufficient for v1.
