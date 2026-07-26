# YourQL

**Talk to your database in plain English.**

YourQL is a desktop application that lets you query your databases using natural language. Instead of writing SQL by hand, you ask questions the way you'd ask a colleague — and YourQL translates them into accurate, safe queries using your preferred AI model. The results come back as interactive tables, charts, and plain-English summaries.

Everything runs locally on your machine. Your database credentials, conversation history, and API keys never leave your computer unless you explicitly configure a cloud LLM provider.

---

## What You Can Do

- **Ask questions in plain English** — "How many orders shipped last month?" or "Show me the top 10 customers by revenue" — and get answers backed by real queries against your database.
- **Work across multiple databases** — Connect to MySQL, PostgreSQL, SQLite, SQL Server, Snowflake, BigQuery, Redshift, MariaDB, CSV files, Excel files, and Google Sheets.
- **Use your own AI model** — Bring your own OpenAI, Anthropic Claude, Ollama, or any OpenAI-compatible HTTP endpoint (LM Studio, llama.cpp, cloud-hosted models).
- **Explore data safely** — Before writing a final query, YourQL can run read-only exploration queries to understand your schema's actual data, all within configurable safety constraints.
- **Visualize results** — Bar charts, line graphs, pie charts, scatter plots, and more, generated automatically when you ask for a visualization or when the data calls for one.
- **Summaries you can read** — Long tables of numbers are automatically summarized into plain-English answers so you don't have to decipher raw results.
- **Keep everything organized** — Pin, archive, duplicate, rename, or clear discussions. Limit how much history is sent to the LLM. Set a custom system prompt or business rules per database.
- **Define reusable Skills** — Write Markdown snippets with domain knowledge, business rules, or preferred query patterns and activate them per-conversation.

---

## Supported Connections

### AI Providers
| Provider | Description |
|---|---|
| **OpenAI** | GPT-4, GPT-4 Turbo, GPT-3.5 Turbo, and compatible APIs (OpenRouter, etc.) |
| **Anthropic** | Claude 3 Opus, Claude 3 Sonnet, Claude 3 Haiku |
| **Ollama** | Local, self-hosted models via Ollama |
| **Custom Endpoint** | Any OpenAI-compatible HTTP API — LM Studio, llama.cpp server, vLLM, cloud-hosted models (qwen, deepseek, etc.) |

### Database & Data Source Types
| Type | Status |
|---|---|
| MySQL | ✅ Supported |
| MariaDB | ✅ Supported |
| PostgreSQL | ✅ Supported |
| SQLite | ✅ Supported |
| SQL Server | ✅ Supported |
| Snowflake | ✅ Supported |
| BigQuery | ✅ Supported |
| Redshift | ✅ Supported |
| CSV Files | ✅ Supported |
| Excel Files (.xlsx) | ✅ Supported |
| Google Sheets | ✅ Supported (OAuth) |

---

## Screenshots

<p align="center">
  <img src="screenshots/discussions-view.png" alt="Discussions View" width="45%" />
  <img src="screenshots/conversation-view.png" alt="Conversation View" width="45%" />
</p>

<p align="center">
  <img src="screenshots/settings-llm-providers.png" alt="LLM Provider Settings" width="45%" />
  <img src="screenshots/settings-db-connections.png" alt="Database Connection Settings" width="45%" />
</p>

<p align="center">
  <img src="screenshots/discussion-settings-popover.png" alt="Discussion Settings" width="45%" />
  <img src="screenshots/discussion-settings-popover-2.png" alt="Discussion Settings Expanded" width="45%" />
</p>

---

## Getting Started

### Installation

Download the latest release for your operating system from the [Releases page](https://github.com/yourorg/yourql/releases).

#### macOS

Open the `.dmg` file and drag YourQL into your Applications folder. On first launch, you may need to right-click → **Open** to bypass Gatekeeper if the app isn't notarized.

#### Linux

Download the AppImage, make it executable, and run:

```bash
chmod +x YourQL-*.AppImage
./YourQL-*.AppImage
```

#### Windows

Run the `.exe` installer and follow the prompts.

### First-Time Setup

1. **Add an AI provider** — Open Settings → Model Configurations and click **Add New Provider**. Enter a name, choose the provider type, specify the model, and provide your API key or base URL. Click **Create Provider**, then **Set as Default**.

2. **Connect a database** — Open Settings → Data Sources and click **+ Add Connection**. Fill in the connection details for your database, then click **Test Connection** to verify it works. Click **Save**, then **Set as Default**.

3. **Start a discussion** — Click **New Discussion** in the sidebar, give it a title, and start asking questions about your data.

---

## How It Works

When you ask a question, YourQL does the following behind the scenes:

1. **Reads your schema** — Tables, columns, types, primary keys, foreign keys, and row counts are gathered from your database.
2. **Builds a prompt** — The schema, your question, any custom system prompt, business rules, and active skills are assembled into a structured prompt for the LLM.
3. **Calls the LLM** — The prompt is sent to your configured AI provider. The LLM responds with a JSON action: `sql_query` (execute this SQL), `clarification` (ask the user for more detail), or `sql_exploration` (run a read-only query to understand the data better before writing the final query).
4. **Explores if needed** — If the LLM chooses exploration, it runs safe, read-only queries (constrained by configurable safety modes) and uses the results to refine its understanding.
5. **Executes the final query** — The generated SQL is run against your database. If it fails, YourQL sends the error back to the LLM for automatic correction and retry.
6. **Renders the results** — Results appear as an interactive, sortable table. If you've enabled summaries or visualizations, those are generated and displayed above the table.

---

## Conversation Settings

Every discussion has its own settings panel (click the gear icon):

- **LLM Provider** — Which AI model powers this discussion
- **Data Source** — Which database this discussion queries
- **Messages in Context** — How many recent messages are sent to the LLM (prevents context-window overflow with long conversations)
- **Summarize Results** — Let the LLM write a plain-English summary above each result table
- **Data Visualization** — Let the LLM generate charts (bar, line, pie, scatter, etc.) when appropriate
- **Pin** — Keep this discussion at the top of your list
- **Duplicate, Clear, Archive, Delete** — Standard conversation management

---

## Data Source Configuration

Each database connection supports deep customization:

- **Custom System Prompt** — Override the default prompt for a specific database (inject domain knowledge, naming conventions, etc.)
- **Business Rules** — One-per-line rules injected into every query prompt (e.g., "Never expose customer SSN", "Always use ISO date format")
- **Table & Column Descriptions** — After loading the schema, add human-readable descriptions for tables and columns so the LLM understands your domain
- **Exploration Settings** — Control when and how the LLM can explore your data:
  - **Strict** — Simple SELECT only; no JOINs, subqueries, UNION, GROUP BY, or ORDER BY
  - **Moderate** — Single-table JOIN, GROUP BY, and ORDER BY allowed
  - **Relaxed** — Subqueries and UNION allowed (still read-only)
- **Query Limits** — Set default and exploration row limits, query length thresholds

---

## Skills

Skills are reusable Markdown prompt fragments you define in Settings. Write a skill for your organization's naming conventions, common query patterns, or any domain knowledge you want the LLM to reference. Then enable one or more skills per conversation — the content is injected into the system prompt for discussions where they're active.

---

## Security & Privacy

- **Your data stays local** — All conversations, settings, and connection details are stored in a local SQLite database at `~/.yourql/yourql.db`. Nothing is sent to the cloud unless you configure a cloud LLM provider.
- **API keys are stored locally** — Provider API keys are saved in the same local database. They are never sent to any server other than the LLM provider you configure.
- **Queries are read-only by default** — Exploration queries are validated against configurable safety modes to prevent data modification. Only SELECT queries (and CTEs) are allowed.
- **No telemetry, no analytics, no phoning home** — YourQL does not collect usage data or communicate with any server other than the LLM API endpoint you configure and the databases you connect to.

---

## Technical Architecture

YourQL is built with [Wails v2](https://wails.io/), combining a Go backend with a Svelte 5 frontend.

| Layer | Technology |
|---|---|
| Desktop framework | Wails v2 |
| Backend | Go 1.25 |
| Frontend | Svelte 5 + Vite |
| Charts | Chart.js 4 |
| App database | SQLite (via `modernc.org/sqlite`, pure Go) |
| External databases | Native drivers per type (see list above) |
| LLM APIs | OpenAI, Anthropic, Ollama, custom HTTP endpoints |

### Data Storage

All application data — conversations, messages, provider configs, connection configs, and skills — is stored in `~/.yourql/yourql.db`. The database is created automatically on first run. Schema migrations are handled non-destructively with automated column addition and tracking.

### LLM Integration

The LLM interface is provider-agnostic. Each provider implements a common `LLMClient` interface supporting chat completion with full request/response payload capture for debugging. The system prompt is dynamically built per-conversation from schema metadata, custom configuration, and active skills.

### Multi-Database Support

Database drivers implement a common `DBDriver` interface with methods for DSN construction, schema introspection, and dialect-aware prompt generation. Drivers for native-API databases (BigQuery, Google Sheets) also implement an optional `NativeQuerier` interface for query execution without `database/sql`.

---

## Development

### Prerequisites

- Go 1.21+
- Node.js 18+
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### Building

```bash
# Generate Wails bindings
~/go/bin/wails generate module

# Build the full application
wails build

# Run in development mode with hot reload
wails dev
```

### Project Structure

```text
YourQL/
├── app.go                       # Wails bindings (Go ↔ frontend bridge)
├── main.go                      # Application entry point
├── pkg/
│   ├── models/                  # Data structures, DB schemas, migrations
│   └── services/                # Core logic — discussion engine, LLM clients,
│                                #   SQL execution, schema introspection, drivers
├── frontend/                    # Svelte 5 UI
│   └── src/
│       ├── App.svelte           # Sidebar, navigation, discussion list
│       ├── ConversationView.svelte  # Chat interface, message display
│       ├── SettingsView.svelte  # LLM providers, data sources, skills
│       └── VizChart.svelte      # Chart.js visualization component
└── screenshots/                 # UI screenshots
```

---

## License

YourQL is open-source software. See the [LICENSE](LICENSE) file for details.

---

## Disclaimer

The AI models you configure will have access to the databases you configure. Use responsibly with databases containing sensitive data. YourQL can be deployed in an air-gapped or local-network-only environment so that models and databases remain isolated, but this requires technical knowledge and deliberate configuration.
