# YourQL

## Overview
**YourQL** is a desktop application built with [Wails](https://wails.io/), designed to be a stripped-down, standalone version of the "Discussion Engine" originally found in the `data_app` web application. It focuses entirely on the core functionality of conversational database querying and LLM integration, removing the complexity of user authentication, workspaces, and multi-tenancy.

The application bridges the gap between natural language and SQL, allowing users to interact with their databases via a chat-like interface powered by Large Language Models (LLMs).

## Architecture
YourQL follows the standard Wails architecture, combining a Go-based backend with a Svelte-based frontend:

1.  **Core Engine (`pkg/services/`)**: Contains the "Discussion Engine" logic. This includes the `ProcessUserMessage` function, which orchestrates the conversation loop: fetching context, calling the LLM, parsing JSON responses, and executing SQL.
2.  **Database Layer (`pkg/models/`)**: Handles all database interactions using GORM and the MySQL driver. It defines the data structures for conversations, messages, and database connections.
3.  **Wails Bindings (`app.go`)**: The gateway between the frontend and the backend. It exposes Go functions (like `ListDiscussions`) to the Svelte frontend.
4.  **LLM Integration (`pkg/services/llm_*.go`)**: Provides a unified interface for multiple LLM providers (OpenAI, Anthropic, Ollama, and local models).

## Project Structure
```text
YourQL/
├── app.go                      # Wails application struct and bindings
├── main.go                     # Application entry point
├── go.mod / go.sum             # Go dependencies
├── .env                        # Configuration (DB, LLM keys, etc.)
├── wails.json                  # Wails configuration
├── pkg/                        # Core business logic (migrated from data_app)
│   ├── models/                 # Data structures and DB schemas
│   ├── services/               # Business logic (Discussion Engine, SQL execution, etc.)
│   ├── controllers/            # API-like handlers (adapted for Wails)
│   ├── configuration/          # Config parsing utilities
│   └── utils/                  # Helper functions (sanitization, slugs)
└── frontend/                   # Svelte UI
    ├── src/
    │   ├── App.svelte          # Main application component
    │   └── main.js             # Entry point
    ├── wailsjs/                # Auto-generated Wails bindings
    └── package.json            # Frontend dependencies
```

## Backend Details

### Core Components
The backend of YourQL is built on a robust set of services migrated from the original `data_app`. These components work together to provide the "Discussion Engine" capabilities:

1. **Discussion Engine (`pkg/services/discussion_engine.go`)**:
   - The heart of the application. It manages the state of a conversation, interacts with the LLM, and handles the logic for SQL generation and execution.
   - **Exploration Mode**: Supports "exploration rounds" where the LLM can run intermediate queries to gather data before formulating a final answer.
   - **Safety Constraints**: Enforces read-only restrictions on exploration queries to prevent accidental data modification.

2. **LLM Client (`pkg/services/llm_client.go`)**:
   - A unified interface for interacting with different LLM providers.
   - **Supported Providers**:
     - **OpenAI** (`llm_openai.go`): Supports GPT-4 and other OpenAI models.
     - **Anthropic** (`llm_anthropic.go`): Supports Claude models.
     - **Ollama** (`llm_ollama.go`): For local, self-hosted models.
     - **Local** (`llm_local.go`): For custom local model paths.
     - **Mock** (`llm_mock.go`): For testing without an actual LLM.

3. **SQL Execution (`pkg/services/sql_execution.go`)**:
   - Handles the execution of SQL queries against the configured database connection.
   - Includes retry logic for transient errors and provides a mechanism to feed error messages back to the LLM for self-correction.

4. **Database Introspection (`pkg/services/database_introspection.go`)**:
   - Automatically fetches the schema of the connected database (tables, columns, indexes, foreign keys).
   - This schema is injected into the LLM's context to ensure generated SQL is accurate and compatible.

### Configuration
YourQL relies on a `.env` file located in the root directory for configuration. Key environment variables include:

- `DB_CONN_STR`: The MySQL connection string.
- `LLM_PROVIDER`: The default LLM to use (`openai`, `anthropic`, `ollama`, `local`).
- `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`: API keys for the respective providers.
- `OLLAMA_BASE_URL` / `OLLAMA_MODEL`: Configuration for local Ollama instances.

### Dependencies
The project uses the following major Go modules:
- **Wails v2**: For the desktop framework.
- **GORM**: For database ORM operations.
- **Gin**: Used in the migrated controllers for request/response handling.
- **go-sql-driver/mysql**: For MySQL connectivity.
- **godotenv**: For environment variable management.

## Frontend and Wails Integration

### Technology Stack
- **Svelte 5**: Used for building the reactive user interface.
- **Vite**: Used as the build tool and development server.
- **Wails Runtime**: Provides the bridge between the Go backend and the JavaScript frontend.

### Wails Bindings
Wails automatically generates TypeScript and JavaScript bindings that allow the Svelte frontend to call Go functions. These are located in `frontend/wailsjs/go/main/`.

Example of how a binding is defined in `app.go`:
```go
// ListDiscussions retrieves titles of discussions for a user in a workspace
func (a *App) ListDiscussions(workspaceID uint, userID uint) ([]string, error) {
    discussions, err := services.ListConversationsByUser(workspaceID, userID)
    if err != nil {
        return nil, err
    }

    var titles []string
    for _, d := range discussions {
        if d.Title != nil {
            titles = append(titles, *d.Title)
        } else {
            titles = append(titles, "Untitled")
        }
    }
    return titles, nil
}
```

In the Svelte frontend, this is called using the generated `App.js`:
```javascript
import {ListDiscussions} from '../wailsjs/go/main/App.js'

// Calling the Go function
const res = await ListDiscussions(1, 1);
```

### Current UI Implementation
The current `App.svelte` provides a simplified interface to test the backend:
1. **Status Box**: Displays the current state of the application (e.g., "Ready", "Loading", or error messages).
2. **Action Button**: Triggers the `loadDiscussions` function, which calls the backend to fetch conversations.
3. **Discussion List**: Displays the titles of the retrieved discussions in a clean, responsive list.

### Styling
The UI uses a clean, modern aesthetic with:
- **Global Resets**: Ensures consistent behavior across different OS environments.
- **Flexbox Layout**: Used for centering and responsive design.
- **Card-style Containers**: For status and list items to improve readability.

---

## Needed Changes and Potential Enhancements

### 1. Immediate Fixes and Refinements
- **Database Initialization**: 
  - The current `app.go` calls `models.ConnectDatabase()` during startup. For a desktop app, it would be better to make the database connection dynamic, allowing users to connect to different databases without restarting the app.
  - **Recommendation**: Create a "Settings" or "Connections" view in the frontend to manage multiple database connections.

- **Environment Variable Handling**: 
  - The `environment` package relies on a `.env` file in the working directory. In a packaged Wails app, the working directory might not be predictable.
  - **Recommendation**: Update `pkg/environment/env.go` to look for the `.env` file in the user's home directory or the app's data directory.

- **Frontend Error Handling**: 
  - While the current UI shows errors, it would be beneficial to provide more specific feedback for common issues (e.g., "Database unreachable" vs. "LLM API key invalid").

### 2. Feature Enhancements
- **Dynamic LLM Selection**: 
  - Allow users to select which LLM to use for a specific conversation directly from the UI.

- **SQL Query Editor**: 
  - Provide a code editor (e.g., Monaco Editor) for users to view and manually edit the SQL queries generated by the LLM before they are executed.

- **Query History and Persistence**: 
  - The backend already supports saving queries. The frontend should provide a more robust history view, allowing users to revisit past queries and their results.

- **Export Functionality**: 
  - Add the ability to export query results to CSV, JSON, or Excel formats.

### 3. Architectural Improvements
- **Modularization**: 
  - The `pkg/services` directory is quite large. Consider splitting the "Discussion Engine" into its own sub-package (e.g., `pkg/services/engine/`) to improve maintainability.

- **Configuration Management**: 
  - Move away from `.env` files for a desktop app. Implement a local JSON or SQLite-based configuration system to store LLM keys and database credentials securely.

- **Testing**: 
  - The project currently lacks a test suite. Adding unit tests for the `discussion_engine.go` and `sql_execution.go` modules would be highly beneficial.

### 4. UI/UX Polish
- **Markdown Rendering**: 
  - The LLM often returns responses in Markdown. The frontend should include a Markdown renderer to display these responses correctly.

- **Loading States**: 
  - Implement more granular loading indicators (e.g., "Thinking...", "Running SQL...") to give users better feedback during long operations.

- **Theme Support**: 
  - Add support for dark mode, which is a standard expectation for modern desktop applicationLet# YourQL
