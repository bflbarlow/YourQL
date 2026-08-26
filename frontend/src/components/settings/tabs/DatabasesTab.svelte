<script>
  // Data Sources tab — extracted verbatim from SettingsView.
  import {
    CreateDataSource, UpdateDataSource, DeleteDataSource, SetDefaultDataSource,
    TestDataSource, TestNewDataSource, GetSchemaPreview,
    StartGoogleSheetsAuth, StartGoogleSheetsAuthTemp,
    CancelGoogleSheetsAuth, CancelGoogleSheetsAuthTemp, RevokeGoogleSheetsAuth,
    MigrateGoogleAuthConfig
  } from '../../../../wailsjs/go/main/App.js'
  import { EventsOn, EventsOff, BrowserOpenURL } from '../../../../wailsjs/runtime/runtime.js'
  import { confirm } from '../../../lib/confirm.svelte.js'

  let { dataSources = [], onUpdate = () => {} } = $props()

  let isNewConnection = $state(false)
  let selectedDataSource = $state(null)
  let showDBDetail = $state(false)

  let dbDetailForm = $state({
    name: '', type: 'mysql', host: 'localhost', port: 3306,
    database: '', username: '', password: '',
    sslMode: 'false', extra: '{}', filePath: ''
  })

  const dbRequiredFields = {
    mysql:      { host: true, port: true, database: true, username: true, password: true },
    mariadb:    { host: true, port: true, database: true, username: true, password: true },
    postgresql: { host: true, port: true, database: true, username: true, password: true },
    redshift:   { host: true, port: true, database: true, username: true, password: true },
    sqlserver:  { host: true, port: true, database: true, username: true, password: true },
    sqlite:     { database: true },
    snowflake:  { database: true, username: true, password: true },
    bigquery:       { database: true },
    csv_file:        {},
    excel_file:      {},
    google_sheets:   { filePath: true }
  }

  function isDBFieldRequired(type, field) {
    return dbRequiredFields[type]?.[field] ?? false
  }

  function defaultConfig() {
    return {
      system_prompt: '',
      business_rules: [],
      table_descriptions: {},
      column_descriptions: {},
      include_indexes: false,
      include_foreign_keys: false,
      include_table_comments: false,
      exploration_allowed: true,
      max_exploration_rounds: 2,
      max_tools_per_round: 0,
      exploration_safety: 'strict',
      max_action_retries: 3,
      max_final_query_retries: 2,
      default_limit: 0,
      exploration_default_limit: 0,
      query_length_threshold: 0,
      compact_prompts: false,
      force_schema_tools: false
    }
  }

  let dbDetailConfig = $state(defaultConfig())
  let tempBusinessRules = $state('')
  let dbStatus = $state('')
  let schemaData = $state(null)
  let schemaLoading = $state(false)

  // Google Sheets OAuth state
  let googleAuthState = $state('idle')
  let googleError = $state('')
  let googleAuthDataSourceID = $state(null)
  let googleTempSessionID = $state('')

  // Schema tables sorting
  let schemaSortColumn = $state('name')
  let schemaSortDirection = $state('asc')

  let sortedSchemaTables = $derived(
    schemaData && schemaData.tables
      ? [...schemaData.tables].sort((a, b) => {
          let valA, valB
          if (schemaSortColumn === 'name') {
            valA = a.name
            valB = b.name
          } else if (schemaSortColumn === 'row_count') {
            valA = a.row_count || 0
            valB = b.row_count || 0
          } else if (schemaSortColumn === 'columns') {
            valA = a.columns?.length || 0
            valB = b.columns?.length || 0
          } else {
            return 0
          }
          let comparison = 0
          if (typeof valA === 'number' && typeof valB === 'number') {
            comparison = valA - valB
          } else {
            comparison = String(valA).localeCompare(String(valB))
          }
          return schemaSortDirection === 'asc' ? comparison : -comparison
        })
      : (schemaData?.tables || [])
  )

  function handleSchemaSort(column) {
    if (schemaSortColumn === column) {
      schemaSortDirection = schemaSortDirection === 'asc' ? 'desc' : 'asc'
    } else {
      schemaSortColumn = column
      schemaSortDirection = 'asc'
    }
  }

  function schemaSortIndicator(column) {
    if (schemaSortColumn !== column) return ' ↕'
    return schemaSortDirection === 'asc' ? ' ↑' : ' ↓'
  }

  async function handleSetDefaultDataSource(id) {
    try {
      await SetDefaultDataSource(id)
      dbStatus = 'Default data source updated'
      onUpdate()
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

  function openNewConnection() {
    dbDetailForm = {
      name: '', type: 'mysql', host: 'localhost', port: 3306,
      database: '', username: '', password: '',
      sslMode: '', extra: '{}', filePath: ''
    }
    dbDetailConfig = defaultConfig()
    tempBusinessRules = ''
    isNewConnection = true
    selectedDataSource = null
    showDBDetail = true
    schemaData = null
    dbStatus = ''
  }

  function openSourceDetail(connection) {
    const config = defaultConfig()

    if (connection.config) {
      try {
        const parsed = JSON.parse(connection.config)
        if (parsed.system_prompt) config.system_prompt = parsed.system_prompt
        if (parsed.business_rules) config.business_rules = parsed.business_rules
        if (parsed.table_descriptions) config.table_descriptions = parsed.table_descriptions
        if (parsed.column_descriptions) config.column_descriptions = parsed.column_descriptions
        if (typeof parsed.exploration_allowed === 'boolean') config.exploration_allowed = parsed.exploration_allowed
        if (parsed.max_exploration_rounds) config.max_exploration_rounds = parsed.max_exploration_rounds
        if (parsed.max_tools_per_round) config.max_tools_per_round = parsed.max_tools_per_round
        if (parsed.exploration_safety) config.exploration_safety = parsed.exploration_safety
        if (parsed.max_action_retries) config.max_action_retries = parsed.max_action_retries
        if (parsed.max_final_query_retries) config.max_final_query_retries = parsed.max_final_query_retries
        if (parsed.default_limit) config.default_limit = parsed.default_limit
        if (parsed.exploration_default_limit) config.exploration_default_limit = parsed.exploration_default_limit
        if (parsed.query_length_threshold) config.query_length_threshold = parsed.query_length_threshold
        if (typeof parsed.compact_prompts === 'boolean') config.compact_prompts = parsed.compact_prompts
        if (typeof parsed.force_schema_tools === 'boolean') config.force_schema_tools = parsed.force_schema_tools
      } catch (e) {
        console.error('Failed to parse config:', e)
      }
    }

    dbDetailForm = {
      name: connection.name,
      type: connection.type,
      host: connection.host || 'localhost',
      port: connection.port || 0,
      database: connection.database || '',
      username: connection.username || '',
      password: '',
      sslMode: connection.sslMode || 'disable',
      extra: connection.extra || '{}',
      filePath: connection.file_path || ''
    }
    googleAuthState = 'idle'
    googleAuthDataSourceID = null
    googleTempSessionID = ''

    dbDetailConfig = config
    tempBusinessRules = (config.business_rules || []).join('\n')

    selectedDataSource = connection
    showDBDetail = true
    schemaData = null
    dbStatus = ''
  }

  function closeDBDetail() {
    showDBDetail = false
    selectedDataSource = null
    isNewConnection = false
    schemaData = null
    dbStatus = ''
  }

  async function handleSaveDBDetail() {
    dbStatus = ''
    if (!dbDetailForm.name.trim()) {
      dbStatus = 'Error: Name is required'
      return
    }
    const isFile = dbDetailForm.type === 'csv_file' || dbDetailForm.type === 'excel_file'
    const isGoogleSheets = dbDetailForm.type === 'google_sheets'
    if (isFile && !dbDetailForm.filePath.trim()) {
      dbStatus = 'Error: File path is required'
      return
    }
    if (isGoogleSheets && !dbDetailForm.filePath.trim()) {
      dbStatus = 'Error: Spreadsheet ID is required'
      return
    }
    if (!isFile && !isGoogleSheets && dbDetailForm.type !== 'sqlite' && !dbDetailForm.database.trim()) {
      dbStatus = 'Error: Database name is required'
      return
    }
    try {
      const config = {
        system_prompt: dbDetailConfig.system_prompt,
        business_rules: tempBusinessRules.split('\n').filter(r => r.trim()),
        table_descriptions: dbDetailConfig.table_descriptions,
        column_descriptions: dbDetailConfig.column_descriptions,
        exploration_allowed: dbDetailConfig.exploration_allowed,
        max_exploration_rounds: dbDetailConfig.max_exploration_rounds,
        max_tools_per_round: dbDetailConfig.max_tools_per_round,
        exploration_safety: dbDetailConfig.exploration_safety,
        max_action_retries: dbDetailConfig.max_action_retries,
        max_final_query_retries: dbDetailConfig.max_final_query_retries,
        default_limit: dbDetailConfig.default_limit,
        exploration_default_limit: dbDetailConfig.exploration_default_limit,
        query_length_threshold: dbDetailConfig.query_length_threshold,
        compact_prompts: dbDetailConfig.compact_prompts,
        force_schema_tools: dbDetailConfig.force_schema_tools
      }
      const configStr = JSON.stringify(config)

      if (isNewConnection) {
        await CreateDataSource(
          dbDetailForm.name,
          dbDetailForm.type,
          dbDetailForm.host,
          dbDetailForm.port,
          dbDetailForm.database,
          dbDetailForm.username,
          dbDetailForm.password,
          dbDetailForm.sslMode,
          configStr,
          dbDetailForm.extra,
          dbDetailForm.filePath,
          dbDetailForm.type === 'csv_file' ? 'csv' : dbDetailForm.type === 'excel_file' ? 'xlsx' : dbDetailForm.type === 'google_sheets' ? 'gsheet' : ''
        )
        dbStatus = 'Connection created successfully'
        await onUpdate()
        const newConn = dataSources.find(c => c.name === dbDetailForm.name)
        if (newConn) {
          selectedDataSource = newConn
          isNewConnection = false
          if (googleTempSessionID && googleAuthState === 'success') {
            try {
              await MigrateGoogleAuthConfig(googleTempSessionID, newConn.id)
            } catch (e) {
              // best-effort; token will be lost if migration fails
            }
            googleTempSessionID = ''
          }
        }
      } else {
        await UpdateDataSource(
          selectedDataSource.id,
          dbDetailForm.name,
          dbDetailForm.host,
          dbDetailForm.database,
          dbDetailForm.username,
          dbDetailForm.password,
          dbDetailForm.sslMode,
          dbDetailForm.filePath,
          dbDetailForm.type === 'csv_file' ? 'csv' : dbDetailForm.type === 'excel_file' ? 'xlsx' : dbDetailForm.type === 'google_sheets' ? 'gsheet' : '',
          dbDetailForm.port,
          configStr,
          dbDetailForm.extra
        )
        dbStatus = 'Connection saved successfully'
      }
      onUpdate()
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

  async function handleTestNewConnection() {
    const isFile = dbDetailForm.type === 'csv_file' || dbDetailForm.type === 'excel_file'
    if (isFile && !dbDetailForm.filePath.trim()) {
      dbStatus = 'Error: File path is required'
      return
    }
    if (!isFile && dbDetailForm.type !== 'sqlite' && !dbDetailForm.database.trim()) {
      dbStatus = 'Error: Database name is required'
      return
    }
    dbStatus = 'Testing connection...'
    try {
      const result = await TestNewDataSource(
        dbDetailForm.name,
        dbDetailForm.type,
        dbDetailForm.host,
        dbDetailForm.port,
        dbDetailForm.database,
        dbDetailForm.username,
        dbDetailForm.password,
        dbDetailForm.sslMode,
        dbDetailForm.extra,
        dbDetailForm.filePath,
        dbDetailForm.type === 'csv_file' ? 'csv' : dbDetailForm.type === 'excel_file' ? 'xlsx' : dbDetailForm.type === 'google_sheets' ? 'gsheet' : ''
      )
      dbStatus = result
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

  async function handleTestSource(id) {
    dbStatus = 'Testing connection...'
    try {
      const result = await TestDataSource(id)
      dbStatus = result
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

  async function handleViewSchema(id) {
    schemaLoading = true
    schemaData = null
    dbStatus = ''
    try {
      schemaData = await GetSchemaPreview(id)
    } catch (e) {
      dbStatus = 'Error loading schema: ' + e.toString()
    } finally {
      schemaLoading = false
    }
  }

  // ==================== Google Sheets OAuth ====================
  async function startGoogleAuth(dataSourceID) {
    if (dataSourceID === null || dataSourceID === undefined) return

    if (dataSourceID === 0 || dataSourceID === null) {
      googleTempSessionID = crypto.randomUUID()
      googleAuthState = 'pending'
      googleError = ''
      try {
        const resp = await StartGoogleSheetsAuthTemp(googleTempSessionID)
        BrowserOpenURL(resp.auth_url)

        EventsOn('googleAuthComplete', (payload) => {
          if (payload.sessionID === googleTempSessionID) {
            googleAuthState = 'success'
            EventsOff('googleAuthComplete')
            EventsOff('googleAuthError')
          }
        })
        EventsOn('googleAuthError', (payload) => {
          if (payload.sessionID === googleTempSessionID) {
            googleAuthState = 'error'
            googleError = payload.error
            EventsOff('googleAuthComplete')
            EventsOff('googleAuthError')
          }
        })
      } catch (e) {
        googleAuthState = 'error'
        googleError = e.toString()
      }
      return
    }

    googleAuthDataSourceID = dataSourceID
    googleAuthState = 'pending'
    googleError = ''
    try {
      const resp = await StartGoogleSheetsAuth(dataSourceID)
      BrowserOpenURL(resp.auth_url)

      EventsOn('googleAuthComplete', (payload) => {
        if (payload.dataSourceID === dataSourceID) {
          googleAuthState = 'success'
          EventsOff('googleAuthComplete')
          EventsOff('googleAuthError')
        }
      })
      EventsOn('googleAuthError', (payload) => {
        if (payload.dataSourceID === dataSourceID) {
          googleAuthState = 'error'
          googleError = payload.error
          EventsOff('googleAuthComplete')
          EventsOff('googleAuthError')
        }
      })
    } catch (e) {
      googleAuthState = 'error'
      googleError = e.toString()
    }
  }

  async function cancelGoogleAuth() {
    if (googleTempSessionID) {
      await CancelGoogleSheetsAuthTemp(googleTempSessionID)
    } else if (googleAuthDataSourceID) {
      await CancelGoogleSheetsAuth(googleAuthDataSourceID)
    }
    EventsOff('googleAuthComplete')
    EventsOff('googleAuthError')
    googleAuthState = 'idle'
    googleAuthDataSourceID = null
    googleTempSessionID = ''
  }

  async function disconnectGoogle() {
    if (googleTempSessionID) {
      googleTempSessionID = ''
    } else if (selectedDataSource && selectedDataSource.id > 0) {
      await RevokeGoogleSheetsAuth(selectedDataSource.id)
    }
    googleAuthState = 'idle'
    googleError = ''
  }

  async function handleDeleteSource(id) {
    const ok = await confirm({
      title: 'Delete Connection',
      body: 'Are you sure you want to delete this connection?',
      confirmLabel: 'Delete',
      danger: true
    })
    if (!ok) return
    try {
      await DeleteDataSource(id)
      dbStatus = 'Connection deleted'
      if (showDBDetail && selectedDataSource && selectedDataSource.id === id) {
        closeDBDetail()
      }
      onUpdate()
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }
</script>

<div class="settings-section">
  <div class="db-tab-content">
    {#if showDBDetail && (selectedDataSource || isNewConnection)}
      <div class="db-detail-view">
        <div class="db-detail-header">
          <button class="btn btn-secondary btn-back" onclick={closeDBDetail}>
            ← Back to List
          </button>
          <h3>{isNewConnection ? 'New Connection' : (dbDetailForm.name || 'New Connection')}</h3>
          <span class="badge db-type">{dbDetailForm.type.toUpperCase()}</span>
        </div>

        <div class="db-detail-content">
          <!-- Connection Info Section -->
          <div class="db-section">
            <h4>Connection Info</h4>
            <div class="form-grid">
              <div class="form-group">
                <label for="ds-name">Name <span class="required">*</span></label>
                <input type="text" id="ds-name" bind:value={dbDetailForm.name} placeholder="My Data Source" />
              </div>
              <div class="form-group">
                <label for="ds-type">Type</label>
                <select id="ds-type" bind:value={dbDetailForm.type}>
                  <option value="mysql">MySQL</option>
                  <option value="mariadb">MariaDB</option>
                  <option value="postgresql">PostgreSQL</option>
                  <option value="redshift">Redshift (WIP)</option>
                  <option value="sqlite">SQLite</option>
                  <option value="sqlserver">SQL Server</option>
                  <option value="snowflake">Snowflake (WIP)</option>
                  <option value="bigquery">BigQuery (WIP)</option>
                  <option disabled>─────────────</option>
                  <option value="csv_file">CSV File</option>
                  <option value="excel_file">Excel File</option>
                  <option disabled>─────────────</option>
                  <option value="google_sheets">Google Sheets (Test Users Only)</option>
                </select>
              </div>
              {#if dbDetailForm.type === 'csv_file' || dbDetailForm.type === 'excel_file'}
              <div class="form-group">
                <label for="ds-file">File <span class="required">*</span></label>
                <input type="text" id="ds-file" bind:value={dbDetailForm.filePath} placeholder="/path/to/file.csv" />
              </div>
              {:else if dbDetailForm.type === 'google_sheets'}
              <div class="form-group">
                <label for="ds-sheet-id">Spreadsheet ID / URL <span class="required">*</span></label>
                <input type="text" id="ds-sheet-id" bind:value={dbDetailForm.filePath} placeholder="ABC123 or https://docs.google.com/spreadsheets/d/ABC123/edit" />
              </div>
              {/if}
              {#if dbDetailForm.type === 'google_sheets'}
              <div class="db-section">
                <h4>Google Account</h4>
                {#if googleAuthState === 'idle'}
                  <div class="form-group">
                    <button class="btn btn-primary" onclick={() => startGoogleAuth(selectedDataSource?.id ?? 0)}>Connect Google Account</button>
                  </div>
                {:else if googleAuthState === 'pending'}
                  <div class="form-group">
                    <p class="hint">Waiting for authorization in your browser...</p>
                    <button class="btn btn-secondary" onclick={() => cancelGoogleAuth()}>Cancel</button>
                  </div>
                {:else if googleAuthState === 'success'}
                  <div class="form-group">
                    <p style="color: var(--color-success);">✅ Connected</p>
                    <button class="btn btn-small btn-danger" onclick={() => disconnectGoogle()}>Disconnect</button>
                  </div>
                {:else if googleAuthState === 'error'}
                  <div class="form-group">
                    <p style="color: var(--color-danger);">❌ {googleError}</p>
                    <button class="btn btn-secondary" onclick={() => { googleAuthState = 'idle'; googleError = '' }}>Dismiss</button>
                  </div>
                {/if}
              </div>
              {:else if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite'}
              <div class="form-group">
                <label for="ds-host">Host {#if isDBFieldRequired(dbDetailForm.type, 'host')}<span class="required">*</span>{/if}</label>
                <input type="text" id="ds-host" bind:value={dbDetailForm.host} placeholder="localhost" />
              </div>
              {/if}
              {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite' && dbDetailForm.type !== 'google_sheets'}
              <div class="form-group">
                <label for="ds-port">Port {#if isDBFieldRequired(dbDetailForm.type, 'port')}<span class="required">*</span>{/if}</label>
                <input type="number" id="ds-port" bind:value={dbDetailForm.port} />
              </div>
              {/if}
              <div class="form-group">
                <label for="ds-database">Database {#if isDBFieldRequired(dbDetailForm.type, 'database')}<span class="required">*</span>{/if}</label>
                {#if dbDetailForm.type === 'google_sheets'}
                  <!-- Google Sheets uses spreadsheet ID above, no database field -->
                {:else if dbDetailForm.type === 'sqlite'}
                  <input type="text" id="ds-database" bind:value={dbDetailForm.database} placeholder="/path/to/database.db" />
                {:else if dbDetailForm.type === 'bigquery'}
                  <input type="text" id="ds-database" bind:value={dbDetailForm.database} placeholder="Project ID" />
                {:else}
                  <input type="text" id="ds-database" bind:value={dbDetailForm.database} placeholder="e.g. classicmodels" />
                {/if}
              </div>
              {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite' && dbDetailForm.type !== 'google_sheets'}
              <div class="form-group">
                <label for="ds-username">Username {#if isDBFieldRequired(dbDetailForm.type, 'username')}<span class="required">*</span>{/if}</label>
                <input type="text" id="ds-username" bind:value={dbDetailForm.username} placeholder="e.g. root" />
              </div>
              <div class="form-group">
                <label for="ds-password">Password {#if isDBFieldRequired(dbDetailForm.type, 'password')}<span class="required">*</span>{/if}</label>
                <input type="password" id="ds-password" bind:value={dbDetailForm.password} placeholder={isNewConnection ? '' : '(leave blank to keep current)'} />
              </div>
              <div class="form-group">
                <label for="ds-ssl">SSL Mode</label>
                <select id="ds-ssl" bind:value={dbDetailForm.sslMode}>
                  <option value="disable">false</option>
                  <option value="require">true</option>
                  <option value="prefer">preferred</option>
                </select>
              </div>
              {/if}
              {#if dbDetailForm.type === 'postgresql' || dbDetailForm.type === 'redshift'}
                {@const pgExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                <div class="form-group">
                  <label for="ds-pg-ssl">PostgreSQL SSL Mode</label>
                  <select id="ds-pg-ssl" value={pgExtra.sslmode || 'require'} onchange={(e) => { pgExtra.sslmode = e.target.value; dbDetailForm.extra = JSON.stringify(pgExtra) }}>
                    <option value="disable">disable</option>
                    <option value="require">require</option>
                    <option value="verify-ca">verify-ca</option>
                    <option value="verify-full">verify-full</option>
                  </select>
                </div>
                <div class="form-group">
                  <label for="ds-pg-search-path">Search Path</label>
                  <input type="text" id="ds-pg-search-path" value={pgExtra.search_path || ''} placeholder="public" oninput={(e) => { pgExtra.search_path = e.target.value; dbDetailForm.extra = JSON.stringify(pgExtra) }} />
                </div>
              {/if}
              {#if dbDetailForm.type === 'sqlserver'}
                {@const msExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                <div class="form-group">
                  <label><input type="checkbox" checked={msExtra.encrypt !== false} onchange={(e) => { msExtra.encrypt = e.target.checked; dbDetailForm.extra = JSON.stringify(msExtra) }} /> Encrypt Connection</label>
                </div>
                <div class="form-group">
                  <label><input type="checkbox" checked={!!msExtra.trust_server_certificate} onchange={(e) => { msExtra.trust_server_certificate = e.target.checked; dbDetailForm.extra = JSON.stringify(msExtra) }} /> Trust Server Certificate</label>
                </div>
                <div class="form-group">
                  <label for="ds-ms-instance">Named Instance</label>
                  <input type="text" id="ds-ms-instance" value={msExtra.instance || ''} placeholder="SQLEXPRESS" oninput={(e) => { msExtra.instance = e.target.value; dbDetailForm.extra = JSON.stringify(msExtra) }} />
                </div>
              {/if}
              {#if dbDetailForm.type === 'snowflake'}
                {@const sfExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                <div class="form-group">
                  <label for="ds-sf-account">Account *</label>
                  <input type="text" id="ds-sf-account" value={sfExtra.account || ''} placeholder="xy12345.us-east-1" oninput={(e) => { sfExtra.account = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                </div>
                <div class="form-group">
                  <label for="ds-sf-warehouse">Warehouse</label>
                  <input type="text" id="ds-sf-warehouse" value={sfExtra.warehouse || ''} placeholder="COMPUTE_WH" oninput={(e) => { sfExtra.warehouse = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                </div>
                <div class="form-group">
                  <label for="ds-sf-role">Role</label>
                  <input type="text" id="ds-sf-role" value={sfExtra.role || ''} placeholder="ANALYST" oninput={(e) => { sfExtra.role = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                </div>
                <div class="form-group">
                  <label for="ds-sf-schema">Schema</label>
                  <input type="text" id="ds-sf-schema" value={sfExtra.schema_name || ''} placeholder="PUBLIC" oninput={(e) => { sfExtra.schema_name = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                </div>
              {/if}
              {#if dbDetailForm.type === 'bigquery'}
                {@const bqExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                <div class="form-group">
                  <label for="ds-bq-project">Project ID *</label>
                  <input type="text" id="ds-bq-project" value={bqExtra.project_id || ''} placeholder="my-gcp-project" oninput={(e) => { bqExtra.project_id = e.target.value; dbDetailForm.extra = JSON.stringify(bqExtra) }} />
                </div>
                <div class="form-group">
                  <label for="ds-bq-dataset">Dataset *</label>
                  <input type="text" id="ds-bq-dataset" value={bqExtra.dataset || ''} placeholder="my_dataset" oninput={(e) => { bqExtra.dataset = e.target.value; dbDetailForm.extra = JSON.stringify(bqExtra) }} />
                </div>
                <div class="form-group">
                  <label for="ds-bq-sa-key">Service Account Key (JSON)</label>
                  <textarea id="ds-bq-sa-key" value={bqExtra.service_account_key || ''} placeholder="Paste service account JSON key" rows="4" oninput={(e) => { bqExtra.service_account_key = e.target.value; dbDetailForm.extra = JSON.stringify(bqExtra) }}></textarea>
                </div>
              {/if}
            </div>
          </div>

          <!-- System Prompt Section -->
          <div class="db-section">
            <h4>Custom System Prompt</h4>
            <div class="form-group">
              <label for="ds-system-prompt">Override Default System Prompt</label>
              <textarea
                id="ds-system-prompt"
                bind:value={dbDetailConfig.system_prompt}
                placeholder="Enter a custom system prompt for this data source. Leave empty to use the default."
                rows="6"
              ></textarea>
              <p class="hint">This prompt will be injected into the discussion engine's system message when this data source is selected.</p>
            </div>
          </div>

          <!-- Business Rules Section -->
          <div class="db-section">
            <h4>Business Rules</h4>
            <div class="form-group">
              <label for="ds-rules">Rules (one per line)</label>
              <textarea
                id="ds-rules"
                bind:value={tempBusinessRules}
                placeholder="e.g., Always include WHERE clause&#10;Never expose customer SSN&#10;Use ISO date format"
                rows="4"
              ></textarea>
              <p class="hint">Each line becomes a business rule injected into the system prompt.</p>
            </div>
          </div>

          <!-- Exploration Settings Section -->
          <div class="db-section">
            <h4>Exploration Settings</h4>
            <div class="form-grid">
              <div class="form-group">
                <label>
                  <input type="checkbox" bind:checked={dbDetailConfig.exploration_allowed} />
                  Allow Exploration Queries
                </label>
              </div>
              <div class="form-group">
                <label>Max Exploration Rounds</label>
                <input type="number" bind:value={dbDetailConfig.max_exploration_rounds} />
              </div>
              <div class="form-group">
                <label>Max Tools Per Round</label>
                <input type="number" bind:value={dbDetailConfig.max_tools_per_round} placeholder="0 = unbounded" />
                <p class="hint">Limits how many query_database calls the model can batch into a single response. 0 = unbounded.</p>
              </div>
              <div class="form-group">
                <label>Safety Mode</label>
                <select bind:value={dbDetailConfig.exploration_safety}>
                  <option value="strict">Strict - Basic SELECT only (no JOIN/UNION/ORDER BY)</option>
                  <option value="moderate">Moderate - Single-table JOIN, GROUP BY, ORDER BY allowed</option>
                  <option value="relaxed">Relaxed - Subqueries and UNION allowed</option>
                </select>
                <div class="safety-hint">
                  {#if dbDetailConfig.exploration_safety === 'strict'}
                    <strong>Strict mode:</strong> Only SELECT with LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA queries. <strong>Blocked:</strong> JOINs, subqueries, UNION, GROUP BY, ORDER BY.
                  {:else if dbDetailConfig.exploration_safety === 'moderate'}
                    <strong>Moderate mode:</strong> Everything in strict, plus single-table JOIN, GROUP BY, ORDER BY. <strong>Blocked:</strong> Subqueries, UNION, multi-table JOINs.
                  {:else if dbDetailConfig.exploration_safety === 'relaxed'}
                    <strong>Relaxed mode:</strong> Everything in moderate, plus subqueries and UNION. <strong>Blocked:</strong> INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE (all DML/DDL).
                  {:else}
                    Select a safety mode to see details.
                  {/if}
                </div>
              </div>
              <div class="form-group">
                <label>Default Limit</label>
                <input type="number" bind:value={dbDetailConfig.default_limit} />
              </div>
              <div class="form-group">
                <label>Exploration Default Limit</label>
                <input type="number" bind:value={dbDetailConfig.exploration_default_limit} />
              </div>
              <div class="form-group">
                <label>Query Length Threshold</label>
                <input type="number" bind:value={dbDetailConfig.query_length_threshold} />
              </div>
              <div class="form-group">
                <label>
                  <input type="checkbox" bind:checked={dbDetailConfig.compact_prompts} />
                  Compact System Prompt
                </label>
                <p class="hint">Reduces prompt size for local/small models. Disables skill personas and chart suggestions for conversations on this connection.</p>
              </div>
              <div class="form-group">
                <label>
                  <input type="checkbox" bind:checked={dbDetailConfig.force_schema_tools} />
                  Force Schema Tools
                </label>
                <p class="hint">Always use on-demand list_tables/describe_table tools for schema discovery instead of sending the full schema in every prompt. Recommended for local/small models and large schemas.</p>
              </div>
            </div>
          </div>

          <!-- Actions -->
          <div class="db-detail-actions">
            {#if dbStatus}
              <div class="status-message {dbStatus.startsWith('Error') ? 'error' : 'success'}" style="width:100%; margin-bottom:var(--space-xl);">
                {dbStatus}
              </div>
            {/if}
            <div class="db-actions-left">
              <button class="btn btn-primary" onclick={handleSaveDBDetail}>Save</button>
              <button class="btn btn-secondary" onclick={() => isNewConnection ? handleTestNewConnection() : handleTestSource(selectedDataSource.id)}>Test Connection</button>
              {#if !isNewConnection}
                <button class="btn btn-secondary" onclick={() => handleViewSchema(selectedDataSource.id)}>Load Schema</button>
              {/if}
            </div>
            {#if !isNewConnection}
              <button class="btn btn-danger" onclick={() => handleDeleteSource(selectedDataSource.id)}>Delete</button>
            {/if}
          </div>
          {#if schemaLoading}
            <div class="db-section">
              <h4>Schema</h4>
              <p class="loading">Loading schema...</p>
            </div>
          {:else if schemaData && schemaData.tables}
            <div class="db-section">
              <h4>Schema - {schemaData.connection_name} ({schemaData.total_tables} table(s))</h4>
              <p class="hint">Enter descriptions for tables and columns. These are saved as part of the connection config.</p>

              <div class="schema-sort-controls">
                <button class="sort-btn {schemaSortColumn === 'name' ? 'active' : ''}" onclick={() => handleSchemaSort('name')}>
                  Name{schemaSortIndicator('name')}
                </button>
                <button class="sort-btn {schemaSortColumn === 'row_count' ? 'active' : ''}" onclick={() => handleSchemaSort('row_count')}>
                  Rows{schemaSortIndicator('row_count')}
                </button>
                <button class="sort-btn {schemaSortColumn === 'columns' ? 'active' : ''}" onclick={() => handleSchemaSort('columns')}>
                  Columns{schemaSortIndicator('columns')}
                </button>
              </div>

              {#each sortedSchemaTables as table, i}
                <div class="schema-table-editable">
                  <div class="schema-table-header">
                    <strong>{table.name}</strong>
                    <span class="row-count">({table.row_count} rows, {table.columns.length} columns)</span>
                  </div>
                  <div class="schema-table-desc">
                    <label for="table-desc-{table.name}">Table Description:</label>
                    <input
                      id="table-desc-{table.name}"
                      type="text"
                      value={dbDetailConfig.table_descriptions[table.name] || ''}
                      placeholder="Describe this table..."
                      oninput={(e) => {
                        dbDetailConfig.table_descriptions = { ...dbDetailConfig.table_descriptions, [table.name]: e.target.value }
                      }}
                    />
                  </div>
                  <div class="schema-columns-editable">
                    <div class="schema-col-header">
                      <span class="col-name-header">Column</span>
                      <span class="col-type-header">Type</span>
                      <span class="col-desc-header">Description</span>
                    </div>
                    {#each table.columns as col}
                      <div class="schema-col-row" class:pk={col.is_primary_key}>
                        <span class="col-name">
                          {col.name}
                          {#if col.is_primary_key}<span class="pk-badge">PK</span>{/if}
                          {#if col.is_nullable}<span class="null-badge">?</span>{/if}
                        </span>
                        <span class="col-type">{col.data_type}</span>
                        <input
                          class="col-desc-input"
                          type="text"
                          value={dbDetailConfig.column_descriptions[table.name + '.' + col.name] || ''}
                          placeholder="Describe this column..."
                          oninput={(e) => {
                            dbDetailConfig.column_descriptions = { ...dbDetailConfig.column_descriptions, [table.name + '.' + col.name]: e.target.value }
                          }}
                        />
                      </div>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {:else}
      <div class="db-list-view">
        <div class="safety-hint">The models you configure will have access to the databases you configure.</div>
        <h4>Configured Connections</h4>
        <button class="btn btn-primary" onclick={openNewConnection}>+ Add Connection</button>

        {#if dbStatus}
          <div class="status-message {dbStatus.startsWith('Error') ? 'error' : 'success'}">
            {dbStatus}
          </div>
        {/if}

        {#if dataSources.length === 0}
          <p class="empty-hint">No data sources configured</p>
        {:else}
          {#each dataSources as connection}
            <div class="data-source-item">
              <div class="data-source-left">
                <div class="data-source-info">
                  <span class="data-source-name">{connection.name}</span>
                  <span class="data-source-type">{connection.type.toUpperCase()}</span>
                  {#if ['snowflake', 'bigquery', 'redshift'].includes(connection.type)}
                    <span class="badge wip">WIP</span>
                  {/if}
                  {#if connection.type === 'google_sheets'}
                    <span class="badge google">Google Sheets</span>
                  {/if}
                </div>
                <div class="data-source-details">
                  {#if connection.type === 'csv_file' || connection.type === 'excel_file'}
                    <span class="detail">📄 {connection.file_path || 'No file selected'}</span>
                  {:else if connection.type === 'google_sheets'}
                    <span class="detail">📊 {connection.file_path || 'No spreadsheet ID'}</span>
                  {:else if connection.type === 'sqlite'}
                    <span class="detail">{connection.database}</span>
                  {:else}
                    <span class="detail">{connection.host}:{connection.port}/{connection.database}</span>
                  {/if}
                  {#if connection.is_default}
                    <span class="badge default">Default</span>
                  {/if}
                  {#if connection.exploration_allowed}
                    <span class="badge exploration">Exploration</span>
                  {/if}
                </div>
              </div>
              <div class="data-source-actions">
                <button class="btn btn-small" onclick={() => handleSetDefaultDataSource(connection.id)}>Set as Default</button>
                <button class="btn btn-small" onclick={() => openSourceDetail(connection)}>Edit</button>
                <button class="btn btn-small" onclick={() => handleTestSource(connection.id)}>Test</button>
                <button class="btn btn-small" onclick={() => handleViewSchema(connection.id)}>Schema</button>
                <button class="btn btn-small btn-danger" onclick={() => handleDeleteSource(connection.id)}>Delete</button>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    {/if}
  </div>
</div>
