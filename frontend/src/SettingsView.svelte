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
    GetSchemaPreview
  } from '../wailsjs/go/main/App.js'
  import { Search } from 'lucide-svelte'
  
  let { 
    llmProviders = [], 
    dbConnections = [], 
    onUpdate = () => {}
  } = $props()
  
  let activeSettingsTab = $state('models')
  
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
  let dbForm = $state({
    name: '',
    type: 'sqlite',
    host: '',
    port: 0,
    database: '',
    username: '',
    password: '',
    sslMode: ''
  })
  
  let editingDBId = $state(null)
  let showDBEditModal = $state(false)
  
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
    sslMode: 'false'
  })
  
  // DB Detail config state
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
  
  // Exploration settings
  let explorationAllowed = $state(true)
  let maxExplorationRounds = $state(2)
  let explorationSafety = $state('strict')

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

  // ==================== DB Connection List Modal Handlers ====================
  async function handleCreateDB() {
    dbStatus = ''
    if (!dbForm.name.trim()) {
      dbStatus = 'Error: Name is required'
      return
    }
    try {
      if (editingDBId) {
        await UpdateDBConnection(editingDBId, dbForm.name, dbForm.host, dbForm.database, dbForm.username, dbForm.password, dbForm.sslMode, dbForm.port, '')
        dbStatus = 'Connection updated successfully'
        editingDBId = null
      } else {
        await CreateDBConnection(dbForm.name, dbForm.type, dbForm.host, dbForm.port, dbForm.database, dbForm.username, dbForm.password, dbForm.sslMode, '')
        dbStatus = 'Connection created successfully'
      }
      cancelEditDB()
      onUpdate()
    } catch (e) {
      dbStatus = 'Error: ' + e.toString()
    }
  }

  function cancelEditDB() {
    showDBEditModal = false
    editingDBId = null
    dbForm = {
      name: '',
      type: 'sqlite',
      host: '',
      port: 0,
      database: '',
      username: '',
      password: '',
      sslMode: ''
    }
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
      sslMode: connection.sslMode || 'disable'
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
    schemaData = null
    dbStatus = ''
  }

  async function handleSaveDBDetail() {
    dbStatus = ''
    if (!dbDetailForm.name.trim()) {
      dbStatus = 'Error: Name is required'
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

      await UpdateDBConnection(
        selectedDBConnection.id,
        dbDetailForm.name,
        dbDetailForm.host,
        dbDetailForm.database,
        dbDetailForm.username,
        dbDetailForm.password,
        dbDetailForm.sslMode,
        dbDetailForm.port,
        configStr
      )
      dbStatus = 'Connection saved successfully'
      onUpdate()
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
          {#if showDBDetail && selectedDBConnection}
            <!-- Detail View -->
            <div class="db-detail-view">
              <div class="db-detail-header">
                <button class="btn btn-secondary btn-back" onclick={closeDBDetail}>
                  ← Back to List
                </button>
                <h3>{dbDetailForm.name || 'New Connection'}</h3>
                <span class="badge db-type">{dbDetailForm.type.toUpperCase()}</span>
              </div>
              
              <div class="db-detail-content">
                <!-- Connection Info Section -->
                <div class="db-section">
                  <h4>Connection Info</h4>
                  <div class="form-grid">
                    <div class="form-group">
                      <label>Name</label>
                      <input type="text" bind:value={dbDetailForm.name} placeholder="My Database" />
                    </div>
                    <div class="form-group">
                      <label>Type</label>
                      <select bind:value={dbDetailForm.type}>
                        <option value="mysql">MySQL</option>
                        <option value="sqlite">SQLite</option>
                      </select>
                    </div>
                    <div class="form-group">
                      <label>Host</label>
                      <input type="text" bind:value={dbDetailForm.host} />
                    </div>
                    <div class="form-group">
                      <label>Port</label>
                      <input type="number" bind:value={dbDetailForm.port} />
                    </div>
                    <div class="form-group">
                      <label>Database</label>
                      <input type="text" bind:value={dbDetailForm.database} />
                    </div>
                    <div class="form-group">
                      <label>Username</label>
                      <input type="text" bind:value={dbDetailForm.username} />
                    </div>
                    <div class="form-group">
                      <label>Password</label>
                      <input type="password" bind:value={dbDetailForm.password} />
                    </div>
                    <div class="form-group">
                      <label>SSL Mode</label>
                      <select bind:value={dbDetailForm.sslMode}>
                        <option value="disable">false</option>
                        <option value="require">true</option>
                        <option value="prefer">preferred</option>
                      </select>
                    </div>
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
                        <option value="strict">Strict — Basic SELECT only (no JOIN/UNION/ORDER BY)</option>
                        <option value="moderate">Moderate — Single‑table JOIN, GROUP BY, ORDER BY allowed</option>
                        <option value="relaxed">Relaxed — Subqueries and UNION allowed</option>
                      </select>
                      <div class="safety-hint">
                        {#if dbDetailConfig.exploration_safety === 'strict'}
                          <strong>Strict mode:</strong> Only SELECT with LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA queries. <strong>Blocked:</strong> JOINs, subqueries, UNION, GROUP BY, ORDER BY.
                        {:else if dbDetailConfig.exploration_safety === 'moderate'}
                          <strong>Moderate mode:</strong> Everything in strict, plus single‑table JOIN, GROUP BY, ORDER BY. <strong>Blocked:</strong> Subqueries, UNION, multi‑table JOINs.
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
                    <button class="btn btn-secondary" onclick={() => handleTestDB(selectedDBConnection.id)}>Test Connection</button>
                    <button class="btn btn-secondary" onclick={() => handleViewSchema(selectedDBConnection.id)}>Load Schema</button>
                  </div>
                  <button class="btn btn-danger" onclick={() => handleDeleteDB(selectedDBConnection.id)}>Delete</button>
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
                    <h4>Schema — {schemaData.connection_name} ({schemaData.total_tables} table(s))</h4>
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
              <button class="btn btn-primary" onclick={() => showDBEditModal = true}>+ Add Connection</button>
              
              {#if dbConnections.length === 0}
                <p class="empty-hint">No database connections configured</p>
              {:else}
                {#each dbConnections as connection}
                  <div class="db-connection-item">
                    <div class="db-connection-info">
                      <span class="db-connection-name">{connection.name}</span>
                      <span class="db-connection-type">{connection.type.toUpperCase()}</span>
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

{#if showDBEditModal}
  <div class="edit-modal-overlay" onclick={cancelEditDB}>
    <div class="edit-modal" onclick={(e) => e.stopPropagation()}>
      <div class="edit-modal-header">
        <h3>Edit Database Connection</h3>
        <button class="close-btn" onclick={cancelEditDB}>×</button>
      </div>
      <div class="edit-modal-body">
        <div class="form-group">
          <label>Name</label>
          <input type="text" bind:value={dbForm.name} placeholder="My Database" />
        </div>
        <div class="form-group">
          <label>Type</label>
          <select bind:value={dbForm.type} disabled>
            <option value="mysql">MySQL</option>
            <option value="sqlite">SQLite</option>
          </select>
        </div>
        <div class="form-group">
          <label>Host</label>
          <input type="text" bind:value={dbForm.host} placeholder="localhost" />
        </div>
        <div class="form-group">
          <label>Port</label>
          <input type="number" bind:value={dbForm.port} placeholder="3306" />
        </div>
        <div class="form-group">
          <label>Database</label>
          <input type="text" bind:value={dbForm.database} placeholder="mydb" />
        </div>
        <div class="form-group">
          <label>Username</label>
          <input type="text" bind:value={dbForm.username} placeholder="root" />
        </div>
        <div class="form-group">
          <label>Password</label>
          <input type="password" bind:value={dbForm.password} placeholder="Leave blank to keep current" />
        </div>
        <div class="form-group">
          <label>SSL Mode</label>
          <select bind:value={dbForm.sslMode}>
            <option value="disable">false</option>
            <option value="require">true</option>
            <option value="prefer">preferred</option>
          </select>
        </div>
        
        <div class="exploration-config">
          <h5><Search size={14} /> Exploration Queries</h5>
          <p class="hint">When enabled, the LLM can run intermediate queries to explore the database before producing a final answer.</p>
          
          <div class="form-group">
            <label>
              <input type="checkbox" bind:checked={explorationAllowed} />
              Allow Exploration Queries
            </label>
          </div>
          
          {#if explorationAllowed}
            <div class="form-group">
              <label>Max Exploration Rounds</label>
              <input type="number" bind:value={maxExplorationRounds} min="1" max="10" />
              <p class="hint">Number of intermediate queries the LLM can run before producing a final query.</p>
            </div>
            
            <div class="form-group">
              <label>Safety Mode</label>
              <select bind:value={explorationSafety}>
                <option value="strict">Strict — Basic SELECT only (no JOIN/UNION/ORDER BY)</option>
                <option value="moderate">Moderate — Single‑table JOIN, GROUP BY, ORDER BY allowed</option>
                <option value="relaxed">Relaxed — Subqueries and UNION allowed</option>
              </select>
              <div class="safety-hint">
                {#if explorationSafety === 'strict'}
                  <strong>Strict mode:</strong> Only SELECT with LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA queries. <strong>Blocked:</strong> JOINs, subqueries, UNION, GROUP BY, ORDER BY.
                {:else if explorationSafety === 'moderate'}
                  <strong>Moderate mode:</strong> Everything in strict, plus single‑table JOIN, GROUP BY, ORDER BY. <strong>Blocked:</strong> Subqueries, UNION, multi‑table JOINs.
                {:else if explorationSafety === 'relaxed'}
                  <strong>Relaxed mode:</strong> Everything in moderate, plus subqueries and UNION. <strong>Blocked:</strong> INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE (all DML/DDL).
                {:else}
                  Select a safety mode to see details.
                {/if}
              </div>
            </div>
          {/if}
        </div>
        
        {#if dbStatus}
          <div class="status-message {dbStatus.startsWith('Error') ? 'error' : 'success'}">
            {dbStatus}
          </div>
        {/if}
        <div class="form-actions">
          <button class="btn btn-primary" onclick={handleCreateDB}>Save Changes</button>
          <button class="btn btn-secondary" onclick={cancelEditDB}>Cancel</button>
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
    gap: 5px;
    padding: 20px 40px 0;
    border-bottom: 1px solid #1a1a1a;
  }
  
  .tab-btn {
    padding: 12px 20px;
    background: transparent;
    border: none;
    border-bottom: 3px solid transparent;
    color: #808080;
    font-size: 14px;
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
    padding: 30px 40px;
    background: #ffffff;
  }
  
  .settings-section {
    max-width: 800px;
  }
  
  h3 {
    margin: 0 0 10px;
    color: #000000;
    font-size: 24px;
  }
  
  .section-desc {
    color: #808080;
    margin: 0 0 30px;
  }
  
  .form-card {
    background: #f9f9f9;
    padding: 25px;
    border-radius: 6px;
    margin-bottom: 20px;
    border: 1px solid #1a1a1a;
  }
  
  .form-card h4 {
    margin: 0 0 20px;
    color: #0288d1;
    font-size: 18px;
  }
  
  .form-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 15px;
    margin-bottom: 20px;
  }
  
  .form-group {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  
  .form-group label {
    font-size: 13px;
    color: #333333;
    font-weight: 500;
  }
  
  .form-group input,
  .form-group select {
    padding: 10px 12px;
    background: #ffffff;
    border: 1px solid #1a1a1a;
    border-radius: 6px;
    color: #000000;
    font-size: 14px;
  }
  
  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: #0288d1;
  }
  
  .form-actions {
    display: flex;
    gap: 10px;
  }
  
  .btn {
    padding: 10px 20px;
    border: none;
    border-radius: 6px;
    font-size: 14px;
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
    padding: 6px 12px;
    font-size: 12px;
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
    padding: 12px 15px;
    border-radius: 6px;
    margin-bottom: 20px;
    font-size: 13px;
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
    margin-top: 20px;
  }
  
  .db-list-view .btn {
    margin-bottom: 12px;
  }
  
  .providers-list h4,
  .connections-list h4,
  .db-list-view h4 {
    margin: 0 0 15px;
    color: #0288d1;
    font-size: 16px;
  }
  
  .provider-card,
  .connection-card,
  .db-connection-item {
    background: #f9f9f9;
    padding: 15px;
    border-radius: 6px;
    margin-bottom: 10px;
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
    gap: 10px;
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
    font-size: 12px;
    color: #999999;
    background: #e0e0e0;
    padding: 3px 8px;
    border-radius: 6px;
  }
  
  .provider-details,
  .connection-details,
  .db-connection-details {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  
  .detail {
    font-size: 12px;
    color: #808080;
  }
  
  .badge {
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 6px;
    font-weight: 600;
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
    gap: 8px;
  }
  
  .empty-hint {
    color: #cccccc;
    font-style: italic;
    margin-top: 10px;
  }
  
  .sqlite-info {
    background: rgba(2, 136, 209, 0.05);
    border: 1px solid rgba(2, 136, 209, 0.2);
    padding: 15px 20px;
    border-radius: 6px;
    margin-top: 20px;
  }
  
  .sqlite-info p {
    margin: 0 0 8px;
    color: #0288d1;
    font-size: 14px;
  }
  
  .sqlite-info p:last-child {
    margin-bottom: 0;
  }
  
  .safety-hint {
    margin-top: 8px;
    padding: 12px 16px;
    margin-bottom: 8px;
    background: #f8f9fa;
    border: 1px solid #e8e8e8;
    border-radius: 6px;
    font-size: 13px;
    color: #666666;
    line-height: 1.5;
  }
  
  .safety-hint strong {
    color: #000000;
    font-weight: 600;
  }
  
  .sqlite-info .hint {
    color: #999999;
    font-size: 13px;
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
    border-radius: 6px;
    width: 90%;
    max-width: 500px;
    max-height: 80vh;
    overflow-y: auto;
    animation: slideUp 0.2s ease;
  }
  
  .edit-modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 20px 25px;
    border-bottom: 1px solid #e0e0e0;
  }
  
  .edit-modal-header h3 {
    margin: 0;
    color: #000000;
    font-size: 18px;
  }
  
  .close-btn {
    background: none;
    border: none;
    font-size: 24px;
    color: #808080;
    cursor: pointer;
    padding: 0;
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
    transition: all 0.2s ease;
  }
  
  .close-btn:hover {
    background: #f0f0f0;
    color: #000000;
  }
  
  .edit-modal-body {
    padding: 25px;
  }
  
  .edit-modal-body .form-group {
    margin-bottom: 15px;
  }
  
  .edit-modal-body .form-group label {
    font-size: 13px;
    color: #808080;
    font-weight: 500;
    margin-bottom: 5px;
    display: block;
  }
  
  .edit-modal-body .form-group input,
  .edit-modal-body .form-group select {
    width: 100%;
    padding: 10px 12px;
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    color: #000000;
    font-size: 14px;
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
    margin-top: 20px;
    display: flex;
    gap: 10px;
  }
  
  .exploration-config {
    margin-top: 20px;
    padding-top: 20px;
    border-top: 2px solid #e0e0e0;
  }
  
  .exploration-config h5 {
    margin: 0 0 8px;
    color: #0288d1;
    font-size: 16px;
  }
  
  .exploration-config .form-group {
    margin-bottom: 12px;
  }
  
  .exploration-config label {
    font-size: 13px;
    color: #333;
    font-weight: 500;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  
  .exploration-config input[type="checkbox"] {
    width: 16px;
    height: 16px;
    accent-color: #0288d1;
  }
  
  .exploration-config input[type="number"] {
    padding: 8px 10px;
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    color: #000000;
    font-size: 14px;
  }
  
  .exploration-config input[type="number"]:focus {
    outline: none;
    border-color: #0288d1;
  }
  
  .exploration-config select {
    padding: 8px 10px;
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    color: #000000;
    font-size: 14px;
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
    from { transform: translateY(20px); opacity: 0; }
    to { transform: translateY(0); opacity: 1; }
  }

  .schema-preview {
    background: #ffffff;
    border: 1px solid #1a1a1a;
    border-radius: 6px;
    padding: 20px;
    margin-top: 20px;
  }

  .schema-preview h4 {
    margin: 0 0 15px;
    color: #000;
    font-size: 16px;
  }

  .schema-sort-controls {
    display: flex;
    gap: 8px;
    margin-bottom: 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid #e0e0e0;
  }

  .sort-btn {
    padding: 6px 12px;
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    font-size: 12px;
    color: #666;
    cursor: pointer;
    transition: all 0.2s ease;
    display: flex;
    align-items: center;
    gap: 4px;
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
    border-radius: 6px;
    padding: 12px;
    margin-bottom: 12px;
  }

  .schema-table-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
    padding-bottom: 8px;
    border-bottom: 1px solid #e0e0e0;
  }

  .schema-table-header strong {
    color: #1a1a1a;
    font-size: 14px;
  }

  .row-count {
    font-size: 12px;
    color: #808080;
  }

  .schema-columns {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .schema-col {
    background: #ffffff;
    border: 1px solid #d0d0d0;
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
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
    margin-bottom: 24px;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    overflow: hidden;
  }
  
  .schema-table-desc {
    padding: 10px 16px;
    background: #f9f9f9;
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    align-items: center;
    gap: 12px;
  }
  
  .schema-table-desc label {
    font-size: 12px;
    font-weight: 600;
    color: #666;
    white-space: nowrap;
  }
  
  .schema-table-desc input {
    flex: 1;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    padding: 6px 10px;
    font-size: 13px;
  }
  
  .schema-columns-editable {
    padding: 0;
  }
  
  .schema-col-header {
    display: flex;
    padding: 8px 16px;
    background: #f5f5f5;
    border-bottom: 1px solid #e0e0e0;
    font-size: 12px;
    font-weight: 600;
    color: #666;
  }
  
  .col-name-header {
    flex: 0 0 160px;
  }
  
  .col-type-header {
    flex: 0 0 100px;
  }
  
  .col-desc-header {
    flex: 1;
  }
  
  .schema-col-row {
    display: flex;
    align-items: center;
    padding: 6px 16px;
    border-bottom: 1px solid #f0f0f0;
    font-size: 13px;
  }
  
  .schema-col-row:last-child {
    border-bottom: none;
  }
  
  .schema-col-row.pk {
    background: #f8fbff;
  }
  
  .col-name {
    flex: 0 0 160px;
    display: flex;
    align-items: center;
    gap: 6px;
    font-family: 'Courier New', monospace;
    font-size: 12px;
    color: #333;
  }
  
  .pk-badge {
    background: #2196f3;
    color: #fff;
    font-size: 9px;
    padding: 1px 5px;
    border-radius: 6px;
    font-weight: 600;
  }
  
  .null-badge {
    color: #999;
    font-size: 11px;
  }
  
  .col-type {
    flex: 0 0 100px;
    font-size: 12px;
    color: #808080;
    font-style: italic;
  }
  
  .col-desc-input {
    flex: 1;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    padding: 4px 8px;
    font-size: 12px;
    transition: border-color 0.2s ease;
  }
  
  .col-desc-input:focus {
    border-color: #1a73e8;
    outline: none;
  }

  /* Exploration Queries */
  .exploration-section {
    margin-top: 30px;
    padding-top: 30px;
    border-top: 2px solid #1a1a1a;
  }
  
  .exploration-section h4 {
    margin: 0 0 10px;
    color: #000000;
    font-size: 20px;
  }
  
  .query-form {
    display: flex;
    flex-direction: column;
    gap: 15px;
  }
  
  .query-input {
    font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
    font-size: 13px;
    line-height: 1.5;
    resize: vertical;
    min-height: 120px;
  }
  
  .query-input:focus {
    border-color: #0288d1;
    background: transparent;
  }
  
  .query-results {
    background: #ffffff;
    border: 1px solid #1a1a1a;
    border-radius: 6px;
    padding: 20px;
    margin-top: 20px;
  }
  
  .results-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 15px;
    padding-bottom: 10px;
    border-bottom: 1px solid #e0e0e0;
  }
  
  .results-header span {
    font-size: 14px;
    color: #0288d1;
    font-weight: 600;
  }
  
  .results-table {
    overflow-x: auto;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
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
    padding: 10px 12px;
    font-size: 13px;
    color: #333;
    border-right: 1px solid #e0e0e0;
    min-width: 100px;
    max-width: 300px;
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
    gap: 20px;
  }
  
  /* DB List View */
  
  /* DB Detail View */
  .db-detail-view {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  
  .db-detail-header {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  
  .btn-back {
    padding: 8px 12px;
    font-size: 13px;
  }
  
  .db-detail-header h3 {
    margin: 0;
    font-size: 20px;
    font-weight: 700;
    color: #1a1a1a;
  }
  
  .db-detail-content {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }
  
  .db-section {
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    padding: 20px;
  }
  
  .db-section h4 {
    margin: 0 0 16px 0;
    font-size: 16px;
    font-weight: 600;
    color: #1a1a1a;
  }
  
  .db-section textarea {
    width: 100%;
    font-family: 'Courier New', monospace;
    font-size: 13px;
    line-height: 1.6;
    resize: vertical;
  }
  
  .db-detail-actions {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-top: 16px;
    border-top: 1px solid #e0e0e0;
  }
  
  .db-actions-left {
    display: flex;
    gap: 8px;
  }
  
  .badge.db-type {
    background: #e3f2fd;
    color: #1565c0;
    font-size: 11px;
    padding: 4px 8px;
    border-radius: 6px;
    font-weight: 600;
  }
</style>
