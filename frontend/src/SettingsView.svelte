<script>
  import {
    CreateLLMProvider,
    UpdateLLMProvider,
    DeleteLLMProvider,
    SetDefaultLLMProvider,
    TestLLMProviderConnection,
    CreateDBConnection,
    UpdateDBConnection,
    DeleteDBConnection,
    SetDefaultDBConnection,
    TestDBConnection,
    GetSchemaPreview,
    GetSupportedDBTypes
  } from '../wailsjs/go/main/App.js'

  let {
    llmProviders = [],
    dbConnections = [],
    onUpdate = () => {}
  } = $props()

  let activeSettingsTab = $state('models')
  let generalScale = $state(typeof localStorage !== 'undefined' ? (localStorage.getItem('yourql-ui-scale') || 'medium') : 'medium')

  function applyScale(scale) {
    document.documentElement.setAttribute('data-ui-scale', scale)
    localStorage.setItem('yourql-ui-scale', scale)
  }

  // LLM Provider Form State
  let llmForm = $state({
    name: '',
    provider: 'openai',
    model: '',
    baseURL: '',
    apiKey: ''
  })

  let editingLLMId = $state(null)
  let showLLMEditModal = $state(false)

  // DB Connection Form State
  let isNewConnection = $state(false)

  // DB List → Detail navigation
  let selectedDBConnection = $state(null)
  let showDBDetail = $state(false)

  // DB Detail form state
  let dbDetailForm = $state({
    name: '',
    type: 'mysql',
    host: 'localhost',
    port: 3306,
    database: '',
    username: '',
    password: '',
    sslMode: 'false',
    extra: '{}'
  })

  // DB Detail config state

  // Which fields are required per database type (verified against driver BuildDSN).
  const dbRequiredFields = {
    mysql:      { host: true, port: true, database: true, username: true, password: true },
    mariadb:    { host: true, port: true, database: true, username: true, password: true },
    postgresql: { host: true, port: true, database: true, username: true, password: true },
    redshift:   { host: true, port: true, database: true, username: true, password: true },
    sqlserver:  { host: true, port: true, database: true, username: true, password: true },
    sqlite:     { database: true },
    snowflake:  { database: true, username: true, password: true },
    bigquery:   { database: true }
  }

  function isDBFieldRequired(type, field) {
    return dbRequiredFields[type]?.[field] ?? false
  }

  let dbDetailConfig = $state({
    system_prompt: '',
    business_rules: [],
    table_descriptions: {},
    column_descriptions: {},
    include_indexes: false,
    include_foreign_keys: false,
    include_table_comments: false,
    exploration_allowed: false,
    max_exploration_rounds: 2,
    exploration_safety: 'strict',
    max_action_retries: 3,
    max_final_query_retries: 2,
    default_limit: 0,
    exploration_default_limit: 0,
    query_length_threshold: 0
  })

  // Temporary business rules for editing
  let tempBusinessRules = $state('')


  let llmStatus = $state('')
  let dbStatus = $state('')
  let schemaData = $state(null)
  let schemaLoading = $state(false)

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

  // ==================== LLM Provider Handlers ====================
  async function handleCreateLLM() {
    llmStatus = ''
    if (!llmForm.name.trim() || !llmForm.model.trim()) {
      llmStatus = 'Error: Name and Model are required'
      return
    }
    try {
      if (editingLLMId) {
        await UpdateLLMProvider(editingLLMId, llmForm.name, llmForm.model, llmForm.baseURL, llmForm.apiKey)
        llmStatus = 'Provider updated successfully'
        editingLLMId = null
      } else {
        await CreateLLMProvider(llmForm.name, llmForm.provider, llmForm.model, llmForm.baseURL, llmForm.apiKey)
        llmStatus = 'Provider created successfully'
      }
      resetLLMForm()
      onUpdate()
    } catch (e) {
      llmStatus = 'Error: ' + e.toString()
    }
  }

  function resetLLMForm() {
    llmForm = {
      name: '',
      provider: 'openai',
      model: '',
      baseURL: '',
      apiKey: ''
    }
  }

  function startEditLLM(provider) {
    llmForm = {
      name: provider.name,
      provider: provider.provider,
      model: provider.model || '',
      baseURL: provider.baseURL || '',
      apiKey: ''
    }
    editingLLMId = provider.id
    showLLMEditModal = true
    llmStatus = ''
  }

  function cancelEditLLM() {
    showLLMEditModal = false
    editingLLMId = null
    resetLLMForm()
  }

  async function handleDeleteLLM(id) {
    if (!confirm('Are you sure you want to delete this provider?')) return
    try {
      await DeleteLLMProvider(id)
      llmStatus = 'Provider deleted'
      onUpdate()
    } catch (e) {
      llmStatus = 'Error: ' + e.toString()
    }
  }

  async function handleTestLLM(id) {
    llmStatus = 'Testing connection...'
    try {
      const result = await TestLLMProviderConnection(id)
      llmStatus = result
    } catch (e) {
      llmStatus = 'Error: ' + e.toString()
    }
  }

  // ==================== DB Connection List Handlers ====================
  function openNewConnection() {
    dbDetailForm = {
      name: '',
      type: 'mysql',
      host: 'localhost',
      port: 3306,
      database: '',
      username: '',
      password: '',
      sslMode: '',
      extra: '{}'
    }
    dbDetailConfig = {
      system_prompt: '',
      business_rules: [],
      table_descriptions: {},
      column_descriptions: {},
      include_indexes: false,
      include_foreign_keys: false,
      include_table_comments: false,
      exploration_allowed: true,
      max_exploration_rounds: 2,
      exploration_safety: 'strict',
      max_action_retries: 3,
      max_final_query_retries: 2,
      default_limit: 0,
      exploration_default_limit: 0,
      query_length_threshold: 0
    }
    tempBusinessRules = ''
    isNewConnection = true
    selectedDBConnection = null
    showDBDetail = true
    schemaData = null
    dbStatus = ''
  }

  // ==================== DB Detail View Handlers ====================
  function openDBDetail(connection) {
    // Build default config
    let config = {
      system_prompt: '',
      business_rules: [],
      table_descriptions: {},
      column_descriptions: {},
      include_indexes: false,
      include_foreign_keys: false,
      include_table_comments: false,
      exploration_allowed: true,
      max_exploration_rounds: 2,
      exploration_safety: 'strict',
      max_action_retries: 3,
      max_final_query_retries: 2,
      default_limit: 0,
      exploration_default_limit: 0,
      query_length_threshold: 0
    }

    // Parse existing config from connection
    if (connection.config) {
      try {
        const parsed = JSON.parse(connection.config)
        if (parsed.system_prompt) config.system_prompt = parsed.system_prompt
        if (parsed.business_rules) config.business_rules = parsed.business_rules
        if (parsed.table_descriptions) config.table_descriptions = parsed.table_descriptions
        if (parsed.column_descriptions) config.column_descriptions = parsed.column_descriptions
        if (typeof parsed.exploration_allowed === 'boolean') config.exploration_allowed = parsed.exploration_allowed
        if (parsed.max_exploration_rounds) config.max_exploration_rounds = parsed.max_exploration_rounds
        if (parsed.exploration_safety) config.exploration_safety = parsed.exploration_safety
        if (parsed.max_action_retries) config.max_action_retries = parsed.max_action_retries
        if (parsed.max_final_query_retries) config.max_final_query_retries = parsed.max_final_query_retries
        if (parsed.default_limit) config.default_limit = parsed.default_limit
        if (parsed.exploration_default_limit) config.exploration_default_limit = parsed.exploration_default_limit
        if (parsed.query_length_threshold) config.query_length_threshold = parsed.query_length_threshold
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
      extra: connection.extra || '{}'
    }

    dbDetailConfig = config
    tempBusinessRules = (config.business_rules || []).join('\n')

    selectedDBConnection = connection
    showDBDetail = true
    schemaData = null
    dbStatus = ''
  }

  function closeDBDetail() {
    showDBDetail = false
    selectedDBConnection = null
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
    // Require database for all types except SQLite
    if (dbDetailForm.type !== 'sqlite' && !dbDetailForm.database.trim()) {
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
        exploration_safety: dbDetailConfig.exploration_safety,
        max_action_retries: dbDetailConfig.max_action_retries,
        max_final_query_retries: dbDetailConfig.max_final_query_retries,
        default_limit: dbDetailConfig.default_limit,
        exploration_default_limit: dbDetailConfig.exploration_default_limit,
        query_length_threshold: dbDetailConfig.query_length_threshold
      }
      const configStr = JSON.stringify(config)

      if (isNewConnection) {
        await CreateDBConnection(
          dbDetailForm.name,
          dbDetailForm.type,
          dbDetailForm.host,
          dbDetailForm.port,
          dbDetailForm.database,
          dbDetailForm.username,
          dbDetailForm.password,
          dbDetailForm.sslMode,
          configStr,
          dbDetailForm.extra
        )
        dbStatus = 'Connection created successfully'
        // Refresh connection list and find the new connection to switch to edit mode
        await onUpdate()
        // Find the newly created connection by name
        const newConn = dbConnections.find(c => c.name === dbDetailForm.name)
        if (newConn) {
          selectedDBConnection = newConn
          isNewConnection = false
        }
      } else {
        await UpdateDBConnection(
          selectedDBConnection.id,
          dbDetailForm.name,
          dbDetailForm.host,
          dbDetailForm.database,
          dbDetailForm.username,
          dbDetailForm.password,
          dbDetailForm.sslMode,
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
    if (dbDetailForm.type !== 'sqlite' && !dbDetailForm.database.trim()) {
      dbStatus = 'Error: Database name is required'
      return
    }
    dbStatus = 'Testing connection...'
    try {
      const result = await TestNewDBConnection(
        dbDetailForm.name,
        dbDetailForm.type,
        dbDetailForm.host,
        dbDetailForm.port,
        dbDetailForm.database,
        dbDetailForm.username,
        dbDetailForm.password,
        dbDetailForm.sslMode,
        dbDetailForm.extra
      )
      dbStatus = result
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

  async function handleTestDB(id) {
    dbStatus = 'Testing connection...'
    try {
      const result = await TestDBConnection(id)
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

  async function handleDeleteDB(id) {
    if (!confirm('Are you sure you want to delete this connection?')) return
    try {
      await DeleteDBConnection(id)
      dbStatus = 'Connection deleted'
      if (showDBDetail && selectedDBConnection && selectedDBConnection.id === id) {
        closeDBDetail()
      }
      onUpdate()
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

</script>

<div class="settings-container">
  <div class="settings-tabs">
    <button
      class="tab-btn {activeSettingsTab === 'models' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'models'}
    >
      Model Configurations
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'databases' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'databases'}
    >
      Database Configurations
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'general' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'general'}
    >
      General
    </button>

  </div>

  <div class="settings-content">
    {#if activeSettingsTab === 'models'}
      <div class="settings-section">
        <h3>Model Configurations</h3>
        <p class="section-desc">Configure your LLM providers (OpenAI, Anthropic, Ollama, etc.)</p>
        <div class="safety-hint">The models you configure will have access to the databases you configure.</div>

        <div class="form-card">
          <h4>Add New Provider</h4>
          <div class="form-grid">
            <div class="form-group">
              <label>Name</label>
              <input type="text" bind:value={llmForm.name} placeholder="My GPT-4" />
            </div>
            <div class="form-group">
              <label>Provider</label>
              <select bind:value={llmForm.provider}>
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
                <option value="ollama">Ollama</option>
                <option value="local">Local</option>
              </select>
            </div>
            <div class="form-group">
              <label>Model</label>
              <input type="text" bind:value={llmForm.model} placeholder="gpt-4-turbo" />
            </div>
            <div class="form-group">
              <label>Base URL (optional)</label>
              <input type="text" bind:value={llmForm.baseURL} placeholder="https://api.openai.com" />
            </div>
            <div class="form-group">
              <label>API Key</label>
              <input type="password" bind:value={llmForm.apiKey} placeholder="sk-..." />
            </div>
          </div>
          <div class="form-actions">
            <button class="btn btn-primary" onclick={handleCreateLLM}>Create Provider</button>
            <button class="btn btn-secondary" onclick={resetLLMForm}>Clear</button>
          </div>
        </div>

        {#if llmStatus}
          <div class="status-message {llmStatus.startsWith('Error') ? 'error' : 'success'}">
            {llmStatus}
          </div>
        {/if}

        <div class="providers-list">
          <h4>Configured Providers</h4>
          {#if llmProviders.length === 0}
            <p class="empty-hint">No providers configured yet</p>
          {:else}
            {#each llmProviders as provider}
              <div class="provider-card">
                <div class="provider-info">
                  <span class="provider-name">{provider.name}</span>
                  <span class="provider-type">{provider.provider}</span>
                </div>
                <div class="provider-details">
                  <span class="detail">Model: {provider.model || 'N/A'}</span>
                  {#if provider.is_default}
                    <span class="badge default">Default</span>
                  {/if}
                </div>
                <div class="provider-actions">
                  <button class="btn btn-small" onclick={() => startEditLLM(provider)}>Edit</button>
                  <button class="btn btn-small" onclick={() => handleTestLLM(provider.id)}>Test</button>
                  <button class="btn btn-small btn-danger" onclick={() => handleDeleteLLM(provider.id)}>Delete</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>
      </div>
    {:else if activeSettingsTab === 'databases'}
      <div class="settings-section">
        <div class="db-tab-content">
          {#if showDBDetail && (selectedDBConnection || isNewConnection)}
            <!-- Detail View -->
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
                      <label>Name <span class="required">*</span></label>
                      <input type="text" bind:value={dbDetailForm.name} placeholder="My Database" />
                    </div>
                    <div class="form-group">
                      <label>Type</label>
                      <select bind:value={dbDetailForm.type}>
                        <option value="mysql">MySQL</option>
                        <option value="mariadb">MariaDB</option>
                        <option value="postgresql">PostgreSQL</option>
                        <option value="redshift">Redshift (WIP)</option>
                        <option value="sqlite">SQLite</option>
                        <option value="sqlserver">SQL Server</option>
                        <option value="snowflake">Snowflake (WIP)</option>
                        <option value="bigquery">BigQuery (WIP)</option>
                      </select>
                    </div>
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite'}
                    <div class="form-group">
                      <label>Host {#if isDBFieldRequired(dbDetailForm.type, 'host')}<span class="required">*</span>{/if}</label>
                      <input type="text" bind:value={dbDetailForm.host} placeholder="localhost" />
                    </div>
                    {/if}
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite'}
                    <div class="form-group">
                      <label>Port {#if isDBFieldRequired(dbDetailForm.type, 'port')}<span class="required">*</span>{/if}</label>
                      <input type="number" bind:value={dbDetailForm.port} />
                    </div>
                    {/if}
                    <div class="form-group">
                      <label>Database {#if isDBFieldRequired(dbDetailForm.type, 'database')}<span class="required">*</span>{/if}</label>
                      {#if dbDetailForm.type === 'sqlite'}
                        <input type="text" bind:value={dbDetailForm.database} placeholder="/path/to/database.db" />
                      {:else if dbDetailForm.type === 'bigquery'}
                        <input type="text" bind:value={dbDetailForm.database} placeholder="Project ID" />
                      {:else}
                        <input type="text" bind:value={dbDetailForm.database} placeholder="e.g. classicmodels" />
                      {/if}
                    </div>
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite'}
                    <div class="form-group">
                      <label>Username {#if isDBFieldRequired(dbDetailForm.type, 'username')}<span class="required">*</span>{/if}</label>
                      <input type="text" bind:value={dbDetailForm.username} placeholder="e.g. root" />
                    </div>
                    {/if}
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite'}
                    <div class="form-group">
                      <label>Password {#if isDBFieldRequired(dbDetailForm.type, 'password')}<span class="required">*</span>{/if}</label>
                      <input type="password" bind:value={dbDetailForm.password} />
                    </div>
                    {/if}
                    {#if dbDetailForm.type !== 'bigquery'}
                    <div class="form-group">
                      <label>SSL Mode</label>
                      <select bind:value={dbDetailForm.sslMode}>
                        <option value="disable">false</option>
                        <option value="require">true</option>
                        <option value="prefer">preferred</option>
                      </select>
                    </div>
                    {/if}
                    {#if dbDetailForm.type === 'postgresql' || dbDetailForm.type === 'redshift'}
                      {@const pgExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                      <div class="form-group">
                        <label>PostgreSQL SSL Mode</label>
                        <select value={pgExtra.sslmode || 'require'} onchange={(e) => { pgExtra.sslmode = e.target.value; dbDetailForm.extra = JSON.stringify(pgExtra) }}>
                          <option value="disable">disable</option>
                          <option value="require">require</option>
                          <option value="verify-ca">verify-ca</option>
                          <option value="verify-full">verify-full</option>
                        </select>
                      </div>
                      <div class="form-group">
                        <label>Search Path</label>
                        <input type="text" value={pgExtra.search_path || ''} placeholder="public" oninput={(e) => { pgExtra.search_path = e.target.value; dbDetailForm.extra = JSON.stringify(pgExtra) }} />
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
                        <label>Named Instance</label>
                        <input type="text" value={msExtra.instance || ''} placeholder="SQLEXPRESS" oninput={(e) => { msExtra.instance = e.target.value; dbDetailForm.extra = JSON.stringify(msExtra) }} />
                      </div>
                    {/if}
                    {#if dbDetailForm.type === 'snowflake'}
                      {@const sfExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                      <div class="form-group">
                        <label>Account *</label>
                        <input type="text" value={sfExtra.account || ''} placeholder="xy12345.us-east-1" oninput={(e) => { sfExtra.account = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                      </div>
                      <div class="form-group">
                        <label>Warehouse</label>
                        <input type="text" value={sfExtra.warehouse || ''} placeholder="COMPUTE_WH" oninput={(e) => { sfExtra.warehouse = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                      </div>
                      <div class="form-group">
                        <label>Role</label>
                        <input type="text" value={sfExtra.role || ''} placeholder="ANALYST" oninput={(e) => { sfExtra.role = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                      </div>
                      <div class="form-group">
                        <label>Schema</label>
                        <input type="text" value={sfExtra.schema_name || ''} placeholder="PUBLIC" oninput={(e) => { sfExtra.schema_name = e.target.value; dbDetailForm.extra = JSON.stringify(sfExtra) }} />
                      </div>
                    {/if}
                    {#if dbDetailForm.type === 'bigquery'}
                      {@const bqExtra = (() => { try { return JSON.parse(dbDetailForm.extra || '{}') } catch(e) { return {} } })()}
                      <div class="form-group">
                        <label>Project ID *</label>
                        <input type="text" value={bqExtra.project_id || ''} placeholder="my-gcp-project" oninput={(e) => { bqExtra.project_id = e.target.value; dbDetailForm.extra = JSON.stringify(bqExtra) }} />
                      </div>
                      <div class="form-group">
                        <label>Dataset *</label>
                        <input type="text" value={bqExtra.dataset || ''} placeholder="my_dataset" oninput={(e) => { bqExtra.dataset = e.target.value; dbDetailForm.extra = JSON.stringify(bqExtra) }} />
                      </div>
                      <div class="form-group">
                        <label>Service Account Key (JSON)</label>
                        <textarea value={bqExtra.service_account_key || ''} placeholder="Paste service account JSON key" rows="4" oninput={(e) => { bqExtra.service_account_key = e.target.value; dbDetailForm.extra = JSON.stringify(bqExtra) }}></textarea>
                      </div>
                    {/if}
                  </div>
                </div>

                <!-- System Prompt Section -->
                <div class="db-section">
                  <h4>Custom System Prompt</h4>
                  <div class="form-group">
                    <label>Override Default System Prompt</label>
                    <textarea
                      bind:value={dbDetailConfig.system_prompt}
                      placeholder="Enter a custom system prompt for this database connection. Leave empty to use the default."
                      rows="6"
                    ></textarea>
                    <p class="hint">This prompt will be injected into the discussion engine's system message when this database is selected.</p>
                  </div>
                </div>

                <!-- Business Rules Section -->
                <div class="db-section">
                  <h4>Business Rules</h4>
                  <div class="form-group">
                    <label>Rules (one per line)</label>
                    <textarea
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
                  </div>
                </div>

                <!-- Actions -->
                <div class="db-detail-actions">
                  <div class="db-actions-left">
                    <button class="btn btn-primary" onclick={handleSaveDBDetail}>Save</button>
                    <button class="btn btn-secondary" onclick={() => isNewConnection ? handleTestNewConnection() : handleTestDB(selectedDBConnection.id)}>Test Connection</button>
                    {#if !isNewConnection}
                      <button class="btn btn-secondary" onclick={() => handleViewSchema(selectedDBConnection.id)}>Load Schema</button>
                    {/if}
                  </div>
                  {#if !isNewConnection}
                    <button class="btn btn-danger" onclick={() => handleDeleteDB(selectedDBConnection.id)}>Delete</button>
                  {/if}
                </div>

                {#if dbStatus}
                  <div class="status-message {dbStatus.startsWith('Error') ? 'error' : 'success'}">
                    {dbStatus}
                  </div>
                {/if}

                <!-- Schema Preview (editable) -->
                {#if schemaLoading}
                  <div class="db-section">
                    <h4>Schema</h4>
                    <p class="loading">Loading schema...</p>
                  </div>
                {:else if schemaData && schemaData.tables}
                  <div class="db-section">
                    <h4>Schema - {schemaData.connection_name} ({schemaData.total_tables} table(s))</h4>
                    <p class="hint">Enter descriptions for tables and columns. These are saved as part of the connection config.</p>

                    <!-- Sort controls -->
                    <div class="schema-sort-controls">
                      <button
                        class="sort-btn {schemaSortColumn === 'name' ? 'active' : ''}"
                        onclick={() => handleSchemaSort('name')}
                      >
                        Name{schemaSortIndicator('name')}
                      </button>
                      <button
                        class="sort-btn {schemaSortColumn === 'row_count' ? 'active' : ''}"
                        onclick={() => handleSchemaSort('row_count')}
                      >
                        Rows{schemaSortIndicator('row_count')}
                      </button>
                      <button
                        class="sort-btn {schemaSortColumn === 'columns' ? 'active' : ''}"
                        onclick={() => handleSchemaSort('columns')}
                      >
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
                          <label>Table Description:</label>
                          <input
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
            <!-- List View -->
            <div class="db-list-view">
              <div class="safety-hint">The models you configure will have access to the databases you configure.</div>
              <h4>Configured Connections</h4>
              <button class="btn btn-primary" onclick={openNewConnection}>+ Add Connection</button>

              {#if dbConnections.length === 0}
                <p class="empty-hint">No database connections configured</p>
              {:else}
                {#each dbConnections as connection}
                  <div class="db-connection-item">
                    <div class="db-connection-info">
                      <span class="db-connection-name">{connection.name}</span>
                      <span class="db-connection-type">{connection.type.toUpperCase()}</span>
                      {#if ['snowflake', 'bigquery', 'redshift'].includes(connection.type)}
                        <span class="badge wip">WIP</span>
                      {/if}
                    </div>
                    <div class="db-connection-details">
                      <span class="detail">{connection.host}:{connection.port}/{connection.database}</span>
                      {#if connection.is_default}
                        <span class="badge default">Default</span>
                      {/if}
                      {#if connection.exploration_allowed}
                        <span class="badge exploration">Exploration</span>
                      {/if}
                    </div>
                    <div class="db-connection-actions">
                      <button class="btn btn-small" onclick={() => openDBDetail(connection)}>Edit</button>
                      <button class="btn btn-small" onclick={() => handleTestDB(connection.id)}>Test</button>
                      <button class="btn btn-small" onclick={() => handleViewSchema(connection.id)}>Schema</button>
                      <button class="btn btn-small btn-danger" onclick={() => handleDeleteDB(connection.id)}>Delete</button>
                    </div>
                  </div>
                {/each}
              {/if}
            </div>
          {/if}
        </div>
      </div>
    {:else if activeSettingsTab === 'general'}
      <div class="settings-section">
        <h3>General Settings</h3>
        <p class="section-desc">Application-wide preferences</p>

        <div class="form-card">
          <h4>UI Scale</h4>
          <p style="color:#808080; margin-bottom:var(--space-2xl);">Adjust the size of text and interface elements across the application.</p>
          <div class="scale-options">
            <button
              class="scale-option-btn {generalScale === 'small' ? 'active' : ''}"
              onclick={() => { generalScale = 'small'; applyScale('small') }}
            >
              <span class="scale-option-label">Small</span>
              <span class="scale-option-desc">Compact layout, more content visible</span>
            </button>
            <button
              class="scale-option-btn {generalScale === 'medium' ? 'active' : ''}"
              onclick={() => { generalScale = 'medium'; applyScale('medium') }}
            >
              <span class="scale-option-label">Medium</span>
              <span class="scale-option-desc">Default size, balanced for desktop</span>
            </button>
            <button
              class="scale-option-btn {generalScale === 'large' ? 'active' : ''}"
              onclick={() => { generalScale = 'large'; applyScale('large') }}
            >
              <span class="scale-option-label">Large</span>
              <span class="scale-option-desc">Larger text, easier to read</span>
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>

{#if showLLMEditModal}
  <div class="edit-modal-overlay" onclick={cancelEditLLM}>
    <div class="edit-modal" onclick={(e) => e.stopPropagation()}>
      <div class="edit-modal-header">
        <h3>Edit LLM Provider</h3>
        <button class="close-btn" onclick={cancelEditLLM}>×</button>
      </div>
      <div class="edit-modal-body">
        <div class="form-group">
          <label>Name</label>
          <input type="text" bind:value={llmForm.name} placeholder="My Provider" />
        </div>
        <div class="form-group">
          <label>Provider Type</label>
          <select bind:value={llmForm.provider} disabled>
            <option value="openai">OpenAI</option>
            <option value="anthropic">Anthropic</option>
            <option value="ollama">Ollama</option>
            <option value="local">Local</option>
          </select>
        </div>
        <div class="form-group">
          <label>Model</label>
          <input type="text" bind:value={llmForm.model} placeholder="gpt-4-turbo" />
        </div>
        <div class="form-group">
          <label>Base URL</label>
          <input type="text" bind:value={llmForm.baseURL} placeholder="https://api.openai.com/v1" />
        </div>
        <div class="form-group">
          <label>API Key</label>
          <input type="password" bind:value={llmForm.apiKey} placeholder="sk-... (leave blank to keep current)" />
        </div>
        {#if llmStatus}
          <div class="status-message {llmStatus.startsWith('Error') ? 'error' : 'success'}">
            {llmStatus}
          </div>
        {/if}
        <div class="form-actions">
          <button class="btn btn-primary" onclick={handleCreateLLM}>Save Changes</button>
          <button class="btn btn-secondary" onclick={cancelEditLLM}>Cancel</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .settings-container {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .settings-tabs {
    display: flex;
    gap: var(--space-xs);
    padding: var(--space-4xl) var(--space-7xl) 0;
    border-bottom: 1px solid #1a1a1a;
  }

  .tab-btn {
    padding: var(--space-xl) var(--space-4xl);
    background: transparent;
    border: none;
    border-bottom: 3px solid transparent;
    color: #808080;
    font-size: var(--font-md);
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .tab-btn:hover {
    color: #000000;
  }

  .tab-btn.active {
    color: #0288d1;
    border-bottom-color: #0288d1;
    font-weight: 600;
  }

  .settings-content {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-6xl) var(--space-7xl);
    background: #ffffff;
  }

  .settings-section {
    max-width: var(--content-max-width);
  }

  h3 {
    margin: 0 0 var(--space-lg);
    color: #000000;
    font-size: var(--font-4xl);
  }

  .section-desc {
    color: #808080;
    margin: 0 0 var(--space-6xl);
  }

  .form-card {
    background: #f9f9f9;
    padding: var(--space-5xl);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-4xl);
    border: 1px solid #1a1a1a;
  }

  .form-card h4 {
    margin: 0 0 var(--space-4xl);
    color: #0288d1;
    font-size: var(--font-2xl);
  }

  .form-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: var(--space-2xl);
    margin-bottom: var(--space-4xl);
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: var(--space-xs);
  }

  .form-group label {
    font-size: var(--font-base);
    color: #333333;
    font-weight: 500;
  }

  .form-group label .required {
    color: #d32f2f;
    margin-left: 2px;
  }

  .form-group input,
  .form-group select {
    padding: var(--space-lg) var(--space-xl);
    background: #ffffff;
    border: 1px solid #1a1a1a;
    border-radius: var(--radius-md);
    color: #000000;
    font-size: var(--font-md);
  }

  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: #0288d1;
  }

  .form-actions {
    display: flex;
    gap: var(--space-lg);
  }

  .btn {
    padding: var(--space-lg) var(--space-4xl);
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-primary {
    background: #0288d1;
    color: #ffffff;
    font-weight: 600;
  }

  .btn-primary:hover {
    background: #29b6f6;
  }

  .btn-secondary {
    background: #e0e0e0;
    color: #333333;
    border: 1px solid #2a2a2a;
  }

  .btn-secondary:hover {
    background: #d0d0d0;
  }

  .btn-small {
    padding: var(--space-sm) var(--space-xl);
    font-size: var(--font-sm);
    background: #e0e0e0;
    color: #333333;
    border: 1px solid #2a2a2a;
  }

  .btn-small:hover {
    background: #d0d0d0;
  }

  .btn-danger {
    background: rgba(239, 83, 80, 0.1);
    color: #ef5350;
    border: 1px solid rgba(239, 83, 80, 0.3);
  }

  .btn-danger:hover {
    background: rgba(239, 83, 80, 0.2);
  }

  .status-message {
    padding: var(--space-xl) var(--space-2xl);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-4xl);
    font-size: var(--font-base);
  }

  .status-message.success {
    background: rgba(2, 136, 209, 0.1);
    color: #0288d1;
    border: 1px solid rgba(2, 136, 209, 0.3);
  }

  .status-message.error {
    background: rgba(244, 67, 54, 0.2);
    color: #ef5350;
    border: 1px solid rgba(244, 67, 54, 0.3);
  }

  .providers-list,
  .connections-list,
  .db-list-view {
    margin-top: var(--space-4xl);
  }

  .db-list-view .btn {
    margin-bottom: var(--space-xl);
  }

  .providers-list h4,
  .connections-list h4,
  .db-list-view h4 {
    margin: 0 0 var(--space-2xl);
    color: #0288d1;
    font-size: var(--font-xl);
  }

  .provider-card,
  .connection-card,
  .db-connection-item {
    background: #f9f9f9;
    padding: var(--space-2xl);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-lg);
    display: flex;
    align-items: center;
    justify-content: space-between;
    border: 1px solid #e0e0e0;
  }

  .provider-info,
  .connection-info,
  .db-connection-info {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
  }

  .provider-name,
  .connection-name,
  .db-connection-name {
    font-weight: 600;
    color: #000000;
  }

  .provider-type,
  .connection-type,
  .db-connection-type {
    font-size: var(--font-sm);
    color: #999999;
    background: #e0e0e0;
    padding: 3px var(--space-md);
    border-radius: var(--radius-md);
  }

  .provider-details,
  .connection-details,
  .db-connection-details {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
  }

  .detail {
    font-size: var(--font-sm);
    color: #808080;
  }

  .badge {
    font-size: var(--font-xs);
    padding: 3px var(--space-md);
    border-radius: var(--radius-md);
    font-weight: 600;
  }

  .badge.wip {
    background: #fff3e0;
    color: #e65100;
    border: 1px solid #ffcc80;
    margin-left: var(--space-md);
  }

  .badge.default {
    background: rgba(2, 136, 209, 0.15);
    color: #0288d1;
    border: 1px solid rgba(2, 136, 209, 0.3);
  }

  .badge.exploration {
    background: rgba(129, 236, 236, 0.15);
    color: #02b8b8;
    border: 1px solid rgba(129, 236, 236, 0.3);
  }

  .provider-actions,
  .connection-actions,
  .db-connection-actions {
    display: flex;
    gap: var(--space-md);
  }

  .empty-hint {
    color: #cccccc;
    font-style: italic;
    margin-top: var(--space-lg);
  }

  .sqlite-info {
    background: rgba(2, 136, 209, 0.05);
    border: 1px solid rgba(2, 136, 209, 0.2);
    padding: var(--space-2xl) var(--space-4xl);
    border-radius: var(--radius-md);
    margin-top: var(--space-4xl);
  }

  .sqlite-info p {
    margin: 0 0 var(--space-md);
    color: #0288d1;
    font-size: var(--font-md);
  }

  .sqlite-info p:last-child {
    margin-bottom: 0;
  }

  .safety-hint {
    margin-top: var(--space-md);
    padding: var(--space-xl) var(--space-3xl);
    margin-bottom: var(--space-md);
    background: #f8f9fa;
    border: 1px solid #e8e8e8;
    border-radius: var(--radius-md);
    font-size: var(--font-base);
    color: #666666;
    line-height: 1.5;
  }

  .safety-hint strong {
    color: #000000;
    font-weight: 600;
  }

  .sqlite-info .hint {
    color: #999999;
    font-size: var(--font-base);
  }

  .edit-modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    animation: fadeIn 0.2s ease;
  }

  .edit-modal {
    background: #ffffff;
    border-radius: var(--radius-md);
    width: 90%;
    max-width: var(--modal-width);
    max-height: 80vh;
    overflow-y: auto;
    animation: slideUp 0.2s ease;
  }

  .edit-modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4xl) var(--space-5xl);
    border-bottom: 1px solid #e0e0e0;
  }

  .edit-modal-header h3 {
    margin: 0;
    color: #000000;
    font-size: var(--font-2xl);
  }

  .close-btn {
    background: none;
    border: none;
    font-size: var(--font-4xl);
    color: #808080;
    cursor: pointer;
    padding: 0;
    width: 2rem;
    height: 2rem;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
    transition: all 0.2s ease;
  }

  .close-btn:hover {
    background: #f0f0f0;
    color: #000000;
  }

  .edit-modal-body {
    padding: var(--space-5xl);
  }

  .edit-modal-body .form-group {
    margin-bottom: var(--space-2xl);
  }

  .edit-modal-body .form-group label {
    font-size: var(--font-base);
    color: #808080;
    font-weight: 500;
    margin-bottom: var(--space-xs);
    display: block;
  }

  .edit-modal-body .form-group input,
  .edit-modal-body .form-group select {
    width: 100%;
    padding: var(--space-lg) var(--space-xl);
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    color: #000000;
    font-size: var(--font-md);
  }

  .edit-modal-body .form-group input:focus,
  .edit-modal-body .form-group select:focus {
    outline: none;
    border-color: #0288d1;
  }

  .edit-modal-body .form-group input:disabled,
  .edit-modal-body .form-group select:disabled {
    background: #f5f5f5;
    color: #808080;
    cursor: not-allowed;
  }

  .edit-modal-body .form-actions {
    margin-top: var(--space-4xl);
    display: flex;
    gap: var(--space-lg);
  }

  .exploration-config {
    margin-top: var(--space-4xl);
    padding-top: var(--space-4xl);
    border-top: 2px solid #e0e0e0;
  }

  .exploration-config h5 {
    margin: 0 0 var(--space-md);
    color: #0288d1;
    font-size: var(--font-xl);
  }

  .exploration-config .form-group {
    margin-bottom: var(--space-xl);
  }

  .exploration-config label {
    font-size: var(--font-base);
    color: #333;
    font-weight: 500;
    display: flex;
    align-items: center;
    gap: var(--space-md);
  }

  .exploration-config input[type="checkbox"] {
    width: var(--space-3xl);
    height: var(--space-3xl);
    accent-color: #0288d1;
  }

  .exploration-config input[type="number"] {
    padding: var(--space-md) var(--space-lg);
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    color: #000000;
    font-size: var(--font-md);
  }

  .exploration-config input[type="number"]:focus {
    outline: none;
    border-color: #0288d1;
  }

  .exploration-config select {
    padding: var(--space-md) var(--space-lg);
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    color: #000000;
    font-size: var(--font-md);
  }

  .exploration-config select:focus {
    outline: none;
    border-color: #0288d1;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes slideUp {
    from { transform: translateY(1.25rem); opacity: 0; }
    to { transform: translateY(0); opacity: 1; }
  }

  .schema-preview {
    background: #ffffff;
    border: 1px solid #1a1a1a;
    border-radius: var(--radius-md);
    padding: var(--space-4xl);
    margin-top: var(--space-4xl);
  }

  .schema-preview h4 {
    margin: 0 0 var(--space-2xl);
    color: #000;
    font-size: var(--font-xl);
  }

  .schema-sort-controls {
    display: flex;
    gap: var(--space-md);
    margin-bottom: var(--space-3xl);
    padding-bottom: var(--space-xl);
    border-bottom: 1px solid #e0e0e0;
  }

  .sort-btn {
    padding: var(--space-sm) var(--space-xl);
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
    color: #666;
    cursor: pointer;
    transition: all 0.2s ease;
    display: flex;
    align-items: center;
    gap: var(--space-xs);
  }

  .sort-btn:hover {
    background: #e0e0e0;
    color: #333;
  }

  .sort-btn.active {
    background: #0288d1;
    color: #fff;
    border-color: #0288d1;
  }

  .sort-btn.active:hover {
    background: #29b6f6;
  }

  .schema-preview .loading {
    text-align: center;
    color: #808080;
  }

  .schema-table {
    background: #f9f9f9;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    padding: var(--space-xl);
    margin-bottom: var(--space-xl);
  }

  .schema-table-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: var(--space-md);
    padding-bottom: var(--space-md);
    border-bottom: 1px solid #e0e0e0;
  }

  .schema-table-header strong {
    color: #1a1a1a;
    font-size: var(--font-md);
  }

  .row-count {
    font-size: var(--font-sm);
    color: #808080;
  }

  .schema-columns {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-sm);
  }

  .schema-col {
    background: #ffffff;
    border: 1px solid #d0d0d0;
    border-radius: var(--radius-md);
    padding: 3px var(--space-md);
    font-size: var(--font-sm);
    color: #333;
  }

  .schema-col.pk {
    border-color: #2196f3;
    background: #e3f2fd;
  }

  .schema-col em {
    color: #808080;
    font-style: normal;
  }

  /* Editable Schema */
  .schema-table-editable {
    margin-bottom: var(--space-5xl);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .schema-table-desc {
    padding: var(--space-lg) var(--space-3xl);
    background: #f9f9f9;
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    align-items: center;
    gap: var(--space-xl);
  }

  .schema-table-desc label {
    font-size: var(--font-sm);
    font-weight: 600;
    color: #666;
    white-space: nowrap;
  }

  .schema-table-desc input {
    flex: 1;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    padding: var(--space-sm) var(--space-lg);
    font-size: var(--font-base);
  }

  .schema-columns-editable {
    padding: 0;
  }

  .schema-col-header {
    display: flex;
    padding: var(--space-md) var(--space-3xl);
    background: #f5f5f5;
    border-bottom: 1px solid #e0e0e0;
    font-size: var(--font-sm);
    font-weight: 600;
    color: #666;
  }

  .col-name-header {
    flex: 0 0 10rem;
  }

  .col-type-header {
    flex: 0 0 6.25rem;
  }

  .col-desc-header {
    flex: 1;
  }

  .schema-col-row {
    display: flex;
    align-items: center;
    padding: var(--space-sm) var(--space-3xl);
    border-bottom: 1px solid #f0f0f0;
    font-size: var(--font-base);
  }

  .schema-col-row:last-child {
    border-bottom: none;
  }

  .schema-col-row.pk {
    background: #f8fbff;
  }

  .col-name {
    flex: 0 0 10rem;
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    font-family: 'Courier New', monospace;
    font-size: var(--font-sm);
    color: #333;
  }

  .pk-badge {
    background: #2196f3;
    color: #fff;
    font-size: var(--font-2xs);
    padding: 1px var(--space-xs);
    border-radius: var(--radius-md);
    font-weight: 600;
  }

  .null-badge {
    color: #999;
    font-size: var(--font-xs);
  }

  .col-type {
    flex: 0 0 6.25rem;
    font-size: var(--font-sm);
    color: #808080;
    font-style: italic;
  }

  .col-desc-input {
    flex: 1;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    padding: var(--space-xs) var(--space-md);
    font-size: var(--font-sm);
    transition: border-color 0.2s ease;
  }

  .col-desc-input:focus {
    border-color: #1a73e8;
    outline: none;
  }

  /* Exploration Queries */
  .exploration-section {
    margin-top: var(--space-6xl);
    padding-top: var(--space-6xl);
    border-top: 2px solid #1a1a1a;
  }

  .exploration-section h4 {
    margin: 0 0 var(--space-lg);
    color: #000000;
    font-size: var(--font-3xl);
  }

  .query-form {
    display: flex;
    flex-direction: column;
    gap: var(--space-2xl);
  }

  .query-input {
    font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
    font-size: var(--font-base);
    line-height: 1.5;
    resize: vertical;
    min-height: 7.5rem;
  }

  .query-input:focus {
    border-color: #0288d1;
    background: transparent;
  }

  .query-results {
    background: #ffffff;
    border: 1px solid #1a1a1a;
    border-radius: var(--radius-md);
    padding: var(--space-4xl);
    margin-top: var(--space-4xl);
  }

  .results-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: var(--space-2xl);
    padding-bottom: var(--space-lg);
    border-bottom: 1px solid #e0e0e0;
  }

  .results-header span {
    font-size: var(--font-md);
    color: #0288d1;
    font-weight: 600;
  }

  .results-table {
    overflow-x: auto;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
  }

  .table-header {
    display: flex;
    background: #f5f5f5;
    border-bottom: 2px solid #1a1a1a;
  }

  .table-row {
    display: flex;
    border-bottom: 1px solid #e0e0e0;
  }

  .table-row:last-child {
    border-bottom: none;
  }

  .table-cell {
    flex: 1;
    padding: var(--space-lg) var(--space-xl);
    font-size: var(--font-base);
    color: #333;
    border-right: 1px solid #e0e0e0;
    min-width: 6.25rem;
    max-width: 18.75rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .table-cell:last-child {
    border-right: none;
  }

  /* DB Tab Layout */
  .db-tab-content {
    display: flex;
    flex-direction: column;
    gap: var(--space-4xl);
  }

  /* DB List View */

  /* DB Detail View */
  .db-detail-view {
    display: flex;
    flex-direction: column;
    gap: var(--space-4xl);
  }

  .db-detail-header {
    display: flex;
    align-items: center;
    gap: var(--space-xl);
  }

  .btn-back {
    padding: var(--space-md) var(--space-xl);
    font-size: var(--font-base);
  }

  .db-detail-header h3 {
    margin: 0;
    font-size: var(--font-3xl);
    font-weight: 700;
    color: #1a1a1a;
  }

  .db-detail-content {
    display: flex;
    flex-direction: column;
    gap: var(--space-5xl);
  }

  .db-section {
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    padding: var(--space-4xl);
  }

  .db-section h4 {
    margin: 0 0 var(--space-3xl) 0;
    font-size: var(--font-xl);
    font-weight: 600;
    color: #1a1a1a;
  }

  .db-section textarea {
    width: 100%;
    font-family: 'Courier New', monospace;
    font-size: var(--font-base);
    line-height: 1.6;
    resize: vertical;
  }

  .db-detail-actions {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-top: var(--space-3xl);
    border-top: 1px solid #e0e0e0;
  }

  .db-actions-left {
    display: flex;
    gap: var(--space-md);
  }

  .badge.db-type {
    background: #e3f2fd;
    color: #1565c0;
    font-size: var(--font-xs);
    padding: var(--space-xs) var(--space-md);
    border-radius: var(--radius-md);
    font-weight: 600;
  }

  .scale-options {
    display: flex;
    flex-direction: column;
    gap: var(--space-md);
  }

  .scale-option-btn {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    width: 100%;
    padding: var(--space-xl) var(--space-3xl);
    background: #f9f9f9;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
  }

  .scale-option-btn:hover {
    background: #f0f0f0;
    border-color: #0288d1;
  }

  .scale-option-btn.active {
    background: rgba(2, 136, 209, 0.08);
    border-color: #0288d1;
    border-width: 2px;
  }

  .scale-option-label {
    font-size: var(--font-md);
    font-weight: 600;
    color: #000000;
    margin-bottom: var(--space-2xs);
  }

  .scale-option-desc {
    font-size: var(--font-sm);
    color: #999999;
  }
</style>
