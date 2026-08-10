<script>
  import { onMount } from 'svelte'
  import {
    CreateLLMProvider,
    UpdateLLMProvider,
    DeleteLLMProvider,
    SetDefaultLLMProvider,
    TestLLMProviderConnection,
    CreateDataSource,
    UpdateDataSource,
    DeleteDataSource,
    SetDefaultDataSource,
    TestDataSource,
    GetSchemaPreview,
    GetSupportedDBTypes,
    ListSkills,
    CreateSkill,
    UpdateSkill,
    DeleteSkill,
    SetSkillActive,
    StartGoogleSheetsAuth,
    StartGoogleSheetsAuthTemp,
    CancelGoogleSheetsAuth,
    CancelGoogleSheetsAuthTemp,
    RevokeGoogleSheetsAuth,
    MigrateGoogleAuthConfig,
    DetectModelMaxTokens,
    GetDiscussionDefaults,
    UpdateDiscussionDefaults,
    GetAgentLoopConfig,
    SetAgentLoopConfigKey,
    ResetAgentLoopConfigKey,
    ResetAllAgentLoopConfig,
    GetAppSetting,
    SetAppSetting
  } from '../wailsjs/go/main/App.js'
  import { EventsOn, EventsOff, BrowserOpenURL } from '../wailsjs/runtime/runtime.js'

  let {
    llmProviders = [],
    dataSources = [],
    onUpdate = () => {}
  } = $props()

  let activeSettingsTab = $state('models')
  let generalScale = $state(typeof localStorage !== 'undefined' ? (localStorage.getItem('yourql-ui-scale') || 'medium') : 'medium')
  let currentTheme = $state(typeof localStorage !== 'undefined' ? (localStorage.getItem('yourql-theme') || 'system') : 'system')
  let currentAccent = $state(typeof localStorage !== 'undefined' ? (localStorage.getItem('yourql-accent') || '#0288d1') : '#0288d1')

  // ==================== Discussion Defaults ====================
  let defaultsForm = $state({
    llm_provider_id: null,
    data_source_id: null,
    max_context_messages: 5,
    max_messages: 0,
    summarize: false,
    viz_enabled: true,
    tech_details: false,
    context_details: false,
    streaming_enabled: false
  })

  async function loadDefaults() {
    try {
      const d = await GetDiscussionDefaults()
      if (d) {
        defaultsForm.llm_provider_id = d.llm_provider_id ?? null
        defaultsForm.data_source_id = d.data_source_id ?? null
        defaultsForm.max_context_messages = d.max_context_messages ?? 5
        defaultsForm.max_messages = d.max_messages ?? 0
        defaultsForm.summarize = d.summarize ?? false
        defaultsForm.viz_enabled = d.viz_enabled ?? true
        defaultsForm.tech_details = d.tech_details ?? false
        defaultsForm.context_details = d.context_details ?? false
        defaultsForm.streaming_enabled = d.streaming_enabled ?? false
      }
    } catch (e) {
      console.error('Failed to load defaults:', e)
    }
  }

  async function saveDefaults() {
    try {
      await UpdateDiscussionDefaults(defaultsForm)
    } catch (e) {
      console.error('Failed to save defaults:', e)
    }
  }

  async function resetDefaults() {
    defaultsForm.llm_provider_id = null
    defaultsForm.data_source_id = null
    defaultsForm.max_context_messages = 5
    defaultsForm.max_messages = 0
    defaultsForm.summarize = false
    defaultsForm.viz_enabled = true
    defaultsForm.tech_details = false
    defaultsForm.context_details = false
    defaultsForm.streaming_enabled = false
    await saveDefaults()
  }

  // ==================== Agent Loop Config ====================
  let agentLoopEnabled = $state(false)
  let agentLoopConfigValues = $state({})
  let agentLoopFields = $state([])
  let agentLoopStatus = $state('')

  const sectionNames = ['tool_descriptions', 'instructions', 'safety', 'charts', 'persona', 'responses']
  const sectionLabels = {
    tool_descriptions: 'Tool Descriptions',
    instructions: 'System Instructions',
    safety: 'Exploration Safety Rules',
    charts: 'Chart Guidance',
    persona: 'Fallback Persona',
    responses: 'Tool Response Messages'
  }

  async function loadAgentLoopEnabled() {
    try {
      const val = await GetAppSetting('agent_loop_advanced_enabled')
      agentLoopEnabled = val === 'true'
    } catch (e) {
      agentLoopEnabled = false
    }
  }

  async function toggleAgentLoop() {
    agentLoopEnabled = !agentLoopEnabled
    try {
      await SetAppSetting('agent_loop_advanced_enabled', agentLoopEnabled ? 'true' : 'false')
    } catch (e) {
      console.error('Failed to save agent loop toggle:', e)
    }
  }

  async function loadAgentLoopConfig() {
    try {
      const result = await GetAgentLoopConfig()
      agentLoopFields = result.fields || []
      const cfg = result.config || {}
      // Map config keys to a flat lookup for the UI
      const vals = {}
      for (const f of agentLoopFields) {
        // Derive camelCase field name from the config struct
        const fieldName = configKeyToFieldName(f.key)
        vals[f.key] = cfg[fieldName] || ''
      }
      agentLoopConfigValues = vals
    } catch (e) {
      console.error('Failed to load agent loop config:', e)
    }
  }

  function configKeyToFieldName(key) {
    // Map dotted keys to AgentLoopConfig struct field names.
    const map = {
      'tool.query_database.description': 'tool_query_database_desc',
      'tool.query_database.params.sql.description': 'tool_query_database_sql_desc',
      'tool.query_database.params.is_exploration.description': 'tool_query_database_is_exploration_desc',
      'tool.query_database.params.reasoning.description': 'tool_query_database_reasoning_desc',
      'tool.respond_to_user.description': 'tool_respond_to_user_desc',
      'tool.respond_to_user.params.text.description': 'tool_respond_to_user_text_desc',
      'tool.render_chart.description': 'tool_render_chart_desc',
      'tool.render_chart.params.chart_config.description': 'tool_render_chart_config_desc',
      'instructions.1': 'instruction_1',
      'instructions.2': 'instruction_2',
      'instructions.2a': 'instruction_2a',
      'instructions.2b': 'instruction_2b',
      'instructions.2c': 'instruction_2c',
      'instructions.3': 'instruction_3',
      'instructions.4': 'instruction_4',
      'instructions.5': 'instruction_5',
      'instructions.6': 'instruction_6',
      'safety.preamble': 'safety_preamble',
      'safety.strict.rules': 'safety_strict',
      'safety.moderate.rules': 'safety_moderate',
      'safety.relaxed.rules': 'safety_relaxed',
      'safety.footer': 'safety_footer',
      'safety.oneshot': 'safety_oneshot',
      'charts.intro': 'charts_intro',
      'charts.format': 'charts_format',
      'charts.timing': 'charts_timing',
      'persona.fallback': 'persona_fallback',
      'response.parse_error': 'response_parse_error',
      'response.exploration_exhausted': 'response_exploration_exhausted',
      'response.safety_rejected': 'response_safety_rejected',
      'response.oneshot_violation': 'response_oneshot_violation',
      'response.unknown_tool': 'response_unknown_tool',
      'response.render_chart_no_pending': 'response_render_chart_no_pending',
      'response.render_chart_parse_error': 'response_render_chart_parse_error',
      'response.respond_parse_error': 'response_respond_parse_error',
      'response.loop_exhausted': 'response_loop_exhausted',
      'response.empty_truncated': 'response_empty_truncated'
    }
    return map[key] || key
  }

  async function saveAgentLoopField(key, value) {
    agentLoopStatus = ''
    try {
      await SetAgentLoopConfigKey(key, value)
      agentLoopStatus = 'Saved'
      setTimeout(() => { agentLoopStatus = '' }, 1500)
    } catch (e) {
      agentLoopStatus = 'Error: ' + e.toString()
    }
  }

  async function resetAgentLoopField(key) {
    try {
      await ResetAgentLoopConfigKey(key)
      // Reload to get the default value back
      await loadAgentLoopConfig()
    } catch (e) {
      console.error('Failed to reset field:', e)
    }
  }

  async function resetAllAgentLoop() {
    if (!confirm('This will reset all Agent Loop settings to their defaults. This cannot be undone. Continue?')) return
    try {
      await ResetAllAgentLoopConfig()
      await loadAgentLoopConfig()
      agentLoopStatus = 'All settings restored to defaults'
      setTimeout(() => { agentLoopStatus = '' }, 3000)
    } catch (e) {
      agentLoopStatus = 'Error: ' + e.toString()
    }
  }

  const accentPresets = [
    { hex: '#0288d1', name: 'Blue' },
    { hex: '#388e3c', name: 'Green' },
    { hex: '#f57c00', name: 'Orange' },
    { hex: '#7b1fa2', name: 'Purple' },
    { hex: '#c62828', name: 'Red' },
    { hex: '#00695c', name: 'Teal' }
  ]

  function hexToLuminance(hex) {
    const r = parseInt(hex.slice(1, 3), 16) / 255
    const g = parseInt(hex.slice(3, 5), 16) / 255
    const b = parseInt(hex.slice(5, 7), 16) / 255
    const linearize = (c) => c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)
    return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b)
  }

  let accentLuminanceWarning = $derived(
    currentAccent && hexToLuminance(currentAccent) > 0.55
      ? '⚠ This color may have poor contrast on light backgrounds. Consider a darker shade.'
      : ''
  )

  // Skills state
  let skills = $state([])
  let skillEditor = $state(null)
  let showSkillEditor = $state(false)

  onMount(() => {
    loadSkills()
    loadAgentLoopEnabled()
  })

  function applyScale(scale) {
    document.documentElement.setAttribute('data-ui-scale', scale)
    localStorage.setItem('yourql-ui-scale', scale)
  }

  // Skills handlers
  async function loadSkills() {
    try {
      skills = await ListSkills()
    } catch (e) {
      console.error('Failed to load skills:', e)
    }
  }

  async function handleSaveSkill() {
    if (!skillEditor.name.trim()) return
    try {
      if (skillEditor.id) {
        await UpdateSkill(skillEditor.id, skillEditor.name, skillEditor.markdown_content)
      } else {
        await CreateSkill(skillEditor.name, skillEditor.markdown_content)
      }
      showSkillEditor = false
      skillEditor = null
      await loadSkills()
      onUpdate()
    } catch (e) {
      console.error('Failed to save skill:', e)
    }
  }

  async function handleDeleteSkill(id) {
    try {
      await DeleteSkill(id)
      await loadSkills()
      onUpdate()
    } catch (e) {
      console.error('Failed to delete skill:', e)
    }
  }

  async function handleToggleSkillActive(id, active) {
    try {
      await SetSkillActive(id, active)
      const skill = skills.find(s => s.id === id)
      if (skill) skill.is_active = active
      skills = [...skills]
      onUpdate()
    } catch (e) {
      console.error('Failed to toggle skill:', e)
    }
  }

  // LLM Provider Form State
  let llmForm = $state({
    name: '',
    provider: 'openai',
    model: '',
    baseURL: '',
    apiKey: '',
    maxTokens: 0
  })

  let showLLMDetail = $state(false)
  let isNewLLM = $state(false)
  let editingLLMProvider = $state(null)

  // DB Connection Form State
  let isNewConnection = $state(false)

  // DB List → Detail navigation
  let selectedDataSource = $state(null)
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
    extra: '{}',
    filePath: ''
  })

  // DB Detail config state

  // Which fields are required per data source type (verified against driver BuildDSN).
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
    excel_file:     {},
    google_sheets:  { filePath: true }
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

  // Google Sheets OAuth state
  let googleAuthState = $state('idle') // idle | pending | success | error
  let googleError = $state('')
  let googleAuthDataSourceID = $state(null)
  let googleTempSessionID = $state('')  // UUID for unsaved connections

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
  async function handleSaveLLM() {
    llmStatus = ''
    if (!llmForm.name.trim() || !llmForm.model.trim()) {
      llmStatus = 'Error: Name and Model are required'
      return
    }
    try {
      if (isNewLLM) {
        await CreateLLMProvider(llmForm.name, llmForm.provider, llmForm.model, llmForm.baseURL, llmForm.apiKey, llmForm.maxTokens || 0)
        llmStatus = 'Provider created successfully'
      } else if (editingLLMProvider) {
        await UpdateLLMProvider(editingLLMProvider.id, llmForm.name, llmForm.model, llmForm.baseURL, llmForm.apiKey, llmForm.maxTokens || 0)
        llmStatus = 'Provider updated successfully'
      }
      closeLLMDetail()
      onUpdate()
    } catch (e) {
      llmStatus = 'Error: ' + e.toString()
    }
  }

  function startNewLLM() {
    llmForm = { name: '', provider: 'openai', model: '', baseURL: '', apiKey: '', maxTokens: 0 }
    isNewLLM = true
    editingLLMProvider = null
    showLLMDetail = true
    llmStatus = ''
  }

  function startEditLLM(provider) {
    llmForm = {
      name: provider.name,
      provider: provider.provider,
      model: provider.model || '',
      baseURL: provider.baseURL || '',
      apiKey: '',
      maxTokens: provider.maxTokens || 0
    }
    isNewLLM = false
    editingLLMProvider = provider
    showLLMDetail = true
    llmStatus = ''
  }

  function closeLLMDetail() {
    showLLMDetail = false
    isNewLLM = false
    editingLLMProvider = null
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

  async function handleSetDefaultLLM(id) {
    try {
      await SetDefaultLLMProvider(id)
      llmStatus = 'Default provider updated'
      onUpdate()
    } catch (e) {
      llmStatus = 'Error: ' + e.toString()
    }
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
      extra: '{}',
      filePath: ''
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
    selectedDataSource = null
    showDBDetail = true
    schemaData = null
    dbStatus = ''
  }

  // ==================== DB Detail View Handlers ====================
  function openSourceDetail(connection) {
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
      extra: connection.extra || '{}',
      filePath: connection.file_path || ''
    }
    // Reset Google Sheets auth state when opening a different connection
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
    // Require database for DB types, file path for file types
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
        exploration_safety: dbDetailConfig.exploration_safety,
        max_action_retries: dbDetailConfig.max_action_retries,
        max_final_query_retries: dbDetailConfig.max_final_query_retries,
        default_limit: dbDetailConfig.default_limit,
        exploration_default_limit: dbDetailConfig.exploration_default_limit,
        query_length_threshold: dbDetailConfig.query_length_threshold
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
        // Refresh connection list and find the new connection to switch to edit mode
        await onUpdate()
        // Find the newly created connection by name
        const newConn = dataSources.find(c => c.name === dbDetailForm.name)
        if (newConn) {
          selectedDataSource = newConn
          isNewConnection = false
          // Migrate temp auth config if we completed the OAuth flow before saving
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

  // ==================== Google Sheets OAuth Handlers ====================

  async function startGoogleAuth(dataSourceID) {
    if (dataSourceID === null || dataSourceID === undefined) return

    // New (unsaved) connection: use temp session flow
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

    // Existing (saved) connection: use the real data source ID
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
      // Unsaved connection: just clear temp state
      googleTempSessionID = ''
    } else if (selectedDataSource && selectedDataSource.id > 0) {
      await RevokeGoogleSheetsAuth(selectedDataSource.id)
    }
    googleAuthState = 'idle'
    googleError = ''
  }

  function copyText(text) {
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      navigator.clipboard.writeText(text)
    }
  }

  async function handleDeleteSource(id) {
    if (!confirm('Are you sure you want to delete this connection?')) return
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
      Data Sources
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'skills' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'skills'}
    >
      Skills
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'defaults' ? 'active' : ''}"
      onclick={() => { activeSettingsTab = 'defaults'; loadDefaults() }}
    >
      Defaults
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'general' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'general'}
    >
      General
    </button>
    {#if agentLoopEnabled}
      <button
        class="tab-btn {activeSettingsTab === 'agentloop' ? 'active' : ''}"
        onclick={() => { activeSettingsTab = 'agentloop'; loadAgentLoopConfig() }}
      >
        Agent Loop
      </button>
    {/if}

  </div>

  <div class="settings-content">
    {#if activeSettingsTab === 'models'}
      <div class="settings-section">
        {#if showLLMDetail}
          <!-- LLM Provider Detail View -->
          <div class="db-detail-view">
            <div class="db-detail-header">
              <button class="btn btn-secondary btn-back" onclick={closeLLMDetail}>
                ← Back to List
              </button>
              <h3>{isNewLLM ? 'New Provider' : (llmForm.name || 'Edit Provider')}</h3>
              {#if !isNewLLM}
                <span class="badge db-type">{llmForm.provider.toUpperCase()}</span>
              {/if}
            </div>

            <div class="db-detail-content">
              <div class="db-section">
                <h4>Provider Info</h4>
                <div class="form-grid">
                  <div class="form-group">
                    <label>Name <span class="required">*</span></label>
                    <input type="text" bind:value={llmForm.name} placeholder="My GPT-4" />
                  </div>
                  <div class="form-group">
                    <label>Provider</label>
                    <select bind:value={llmForm.provider} disabled={!isNewLLM}>
                      <option value="openai">OpenAI</option>
                      <option value="anthropic">Anthropic</option>
                      <option value="ollama">Ollama</option>
                      <option value="local">Local</option>
                    </select>
                  </div>
                  <div class="form-group">
                    <label>Model <span class="required">*</span></label>
                    <input type="text" bind:value={llmForm.model} placeholder="gpt-4-turbo" />
                  </div>
                  <div class="form-group">
                    <label>Max Tokens</label>
                    <div class="input-with-detect">
                      <input type="number" bind:value={llmForm.maxTokens} placeholder="4096" min="1" max={editingLLMProvider?.contextWindow > 0 ? editingLLMProvider.contextWindow : undefined} />
                      {#if !isNewLLM && editingLLMProvider}
                        <button class="btn btn-small" onclick={async () => { llmStatus = ''; try { const detected = await DetectModelMaxTokens(editingLLMProvider.id); if (detected > 0) { llmForm.maxTokens = detected; llmStatus = `Model max detected: ${detected} tokens.`; } else { llmStatus = 'Could not detect model max tokens. Using manual setting.'; } } catch(e) { llmStatus = 'Detection failed: ' + e.toString(); } }}>Detect</button>
                      {/if}
                    </div>
                    {#if !isNewLLM && editingLLMProvider?.modelMaxTokens > 0}
                      <span class="field-hint">Model max: {editingLLMProvider.modelMaxTokens} tokens</span>
                    {:else if !isNewLLM && editingLLMProvider?.contextWindow > 0}
                      <span class="field-hint">Context window: {editingLLMProvider.contextWindow} tokens (max output unknown)</span>
                    {:else if !isNewLLM}
                      <span class="field-hint warning">⚠ Could not determine model's token limit.</span>
                    {/if}
                  </div>
                  <div class="form-group">
                    <label>Base URL (optional)</label>
                    <input type="text" bind:value={llmForm.baseURL} placeholder="https://api.openai.com" />
                  </div>
                  <div class="form-group">
                    <label>API Key</label>
                    <input type="password" bind:value={llmForm.apiKey} placeholder={isNewLLM ? 'sk-...' : 'sk-... (leave blank to keep current)'} />
                  </div>
                </div>
              </div>

              {#if llmStatus}
                <div class="status-message {llmStatus.startsWith('Error') ? 'error' : 'success'}">
                  {llmStatus}
                </div>
              {/if}

              <div class="form-actions">
                <button class="btn btn-primary" onclick={handleSaveLLM}>{isNewLLM ? 'Create Provider' : 'Save Changes'}</button>
                {#if !isNewLLM}
                  <button class="btn btn-small" onclick={async () => { await handleTestLLM(editingLLMProvider.id); }}>Test Connection</button>
                {/if}
                <button class="btn btn-secondary" onclick={closeLLMDetail}>Cancel</button>
              </div>
            </div>
          </div>
        {:else}
          <!-- Provider List View -->
          <h3>Model Configurations</h3>
          <p class="section-desc">Configure your LLM providers (OpenAI, Anthropic, Ollama, etc.)</p>
          <div class="safety-hint">The models you configure will have access to the databases you configure.</div>

          <div class="providers-list-header">
            <button class="btn btn-primary" onclick={startNewLLM}>+ Add Provider</button>
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
                  <div class="provider-card-left">
                    <div class="provider-info">
                      <span class="provider-name">{provider.name}</span>
                      <span class="provider-type">{provider.provider}</span>
                    </div>
                    <div class="provider-details">
                      <span class="detail">Model: {provider.model || 'N/A'}</span>
                      <span class="detail">Max tokens: {provider.maxTokens || 2000}{#if provider.modelMaxTokens > 0} (model max: {provider.modelMaxTokens}){/if}{#if provider.contextWindow > 0} (context: {provider.contextWindow}){/if}</span>
                      {#if provider.is_default}
                        <span class="badge default">Default</span>
                      {/if}
                    </div>
                  </div>
                  <div class="provider-actions">
                    <button class="btn btn-small" onclick={() => handleSetDefaultLLM(provider.id)}>Set as Default</button>
                    <button class="btn btn-small" onclick={() => startEditLLM(provider)}>Edit</button>
                    <button class="btn btn-small" onclick={() => handleTestLLM(provider.id)}>Test</button>
                    <button class="btn btn-small btn-danger" onclick={() => handleDeleteLLM(provider.id)}>Delete</button>
                  </div>
                </div>
              {/each}
            {/if}
          </div>
        {/if}
      </div>
    {:else if activeSettingsTab === 'databases'}
      <div class="settings-section">
        <div class="db-tab-content">
          {#if showDBDetail && (selectedDataSource || isNewConnection)}
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
                      <input type="text" bind:value={dbDetailForm.name} placeholder="My Data Source" />
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
                        <option disabled>─────────────</option>
                        <option value="csv_file">CSV File</option>
                        <option value="excel_file">Excel File</option>
                        <option disabled>─────────────</option>
                        <option value="google_sheets">Google Sheets (Test Users Only)</option>
                      </select>
                    </div>
                    {#if dbDetailForm.type === 'csv_file' || dbDetailForm.type === 'excel_file'}
                    <div class="form-group">
                      <label>File <span class="required">*</span></label>
                      <input type="text" bind:value={dbDetailForm.filePath} placeholder="/path/to/file.csv" />
                    </div>
                    {:else if dbDetailForm.type === 'google_sheets'}
                    <div class="form-group">
                      <label>Spreadsheet ID / URL <span class="required">*</span></label>
                      <input type="text" bind:value={dbDetailForm.filePath} placeholder="ABC123 or https://docs.google.com/spreadsheets/d/ABC123/edit" />
                    </div>
                    {/if}
                    {#if dbDetailForm.type === 'google_sheets'}
                    <!-- Google Sheets Auth Section (moved here, right after Connection Info) -->
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
                          <p style="color: var(--success-color);">✅ Connected</p>
                          <button class="btn btn-small btn-danger" onclick={() => disconnectGoogle()}>Disconnect</button>
                        </div>
                      {:else if googleAuthState === 'error'}
                        <div class="form-group">
                          <p style="color: var(--error-color);">❌ {googleError}</p>
                          <button class="btn btn-secondary" onclick={() => { googleAuthState = 'idle'; googleError = '' }}>Dismiss</button>
                        </div>
                      {/if}
                    </div>
                    {:else if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite'}
                    <div class="form-group">
                      <label>Host {#if isDBFieldRequired(dbDetailForm.type, 'host')}<span class="required">*</span>{/if}</label>
                      <input type="text" bind:value={dbDetailForm.host} placeholder="localhost" />
                    </div>
                    {/if}
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite' && dbDetailForm.type !== 'google_sheets'}
                    <div class="form-group">
                      <label>Port {#if isDBFieldRequired(dbDetailForm.type, 'port')}<span class="required">*</span>{/if}</label>
                      <input type="number" bind:value={dbDetailForm.port} />
                    </div>
                    {/if}
                    <div class="form-group">
                      <label>Database {#if isDBFieldRequired(dbDetailForm.type, 'database')}<span class="required">*</span>{/if}</label>
                      {#if dbDetailForm.type === 'google_sheets'}
                        <!-- Google Sheets uses spreadsheet ID above, no database field -->
                      {:else if dbDetailForm.type === 'sqlite'}
                        <input type="text" bind:value={dbDetailForm.database} placeholder="/path/to/database.db" />
                      {:else if dbDetailForm.type === 'bigquery'}
                        <input type="text" bind:value={dbDetailForm.database} placeholder="Project ID" />
                      {:else}
                        <input type="text" bind:value={dbDetailForm.database} placeholder="e.g. classicmodels" />
                      {/if}
                    </div>
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite' && dbDetailForm.type !== 'google_sheets'}
                    <div class="form-group">
                      <label>Username {#if isDBFieldRequired(dbDetailForm.type, 'username')}<span class="required">*</span>{/if}</label>
                      <input type="text" bind:value={dbDetailForm.username} placeholder="e.g. root" />
                    </div>
                    {/if}
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite' && dbDetailForm.type !== 'google_sheets'}
                    <div class="form-group">
                      <label>Password {#if isDBFieldRequired(dbDetailForm.type, 'password')}<span class="required">*</span>{/if}</label>
                      <input type="password" bind:value={dbDetailForm.password} placeholder={isNewConnection ? '' : '(leave blank to keep current)'} />
                    </div>
                    {/if}
                    {#if dbDetailForm.type !== 'bigquery' && dbDetailForm.type !== 'sqlite' && dbDetailForm.type !== 'google_sheets'}
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

                <!-- Actions (sticky at bottom; status message pinned alongside buttons) -->
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
    {:else if activeSettingsTab === 'skills'}
      <div class="settings-section">
        <h3>Skills</h3>
        <p class="section-desc">Skills are markdown text blocks added to the system prompt. Use them to give the LLM domain context or behavioral guidance.</p>

        <div class="form-card" style="margin-bottom: var(--space-lg);">
          <button class="btn btn-primary" onclick={() => { skillEditor = { id: null, name: '', markdown_content: '' }; showSkillEditor = true }}>
            + New Skill
          </button>
        </div>

        {#if skills.length === 0}
          <div style="color: var(--text-tertiary); text-align: center; padding: var(--space-2xl);">
            No skills configured yet. Create one to add context to your conversations.
          </div>
        {:else}
          {#each skills as skill (skill.id)}
            <div class="skill-card">
              <div class="skill-card-main">
                <div class="skill-card-name">{skill.name}</div>
                <div class="skill-card-preview">{skill.markdown_content || '(empty content)'}</div>
              </div>
              <div class="skill-card-actions">
                <button onclick={() => { skillEditor = { ...skill }; showSkillEditor = true }}>Edit</button>
                <button class="delete" onclick={() => handleDeleteSkill(skill.id)}>Delete</button>
                <label class="toggle-switch">
                  <input type="checkbox" checked={skill.is_active} onchange={() => handleToggleSkillActive(skill.id, !skill.is_active)} />
                  <span class="slider"></span>
                </label>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    {:else if activeSettingsTab === 'defaults'}
      <div class="settings-section">
        <h3>Defaults for New Discussions</h3>
        <p class="section-desc">Configure the default settings applied when you create a new discussion.</p>

        <div class="form-card">
          <h4>Provider & Source</h4>
          <div class="form-group">
            <label>Default LLM Provider</label>
            <select bind:value={defaultsForm.llm_provider_id} onchange={saveDefaults}>
              <option value={null}>None (ask each time)</option>
              {#each llmProviders as p}
                <option value={p.id}>{p.name}</option>
              {/each}
            </select>
          </div>
          <div class="form-group">
            <label>Default Data Source</label>
            <select bind:value={defaultsForm.data_source_id} onchange={saveDefaults}>
              <option value={null}>None (ask each time)</option>
              {#each dataSources as ds}
                <option value={ds.id}>{ds.name}</option>
              {/each}
            </select>
          </div>
        </div>

        <div class="form-card">
          <h4>Conversation Behavior</h4>
          <div class="form-group">
            <label>Messages in Context</label>
            <input type="number" min="1" max="15" bind:value={defaultsForm.max_context_messages} onchange={saveDefaults} />
            <p class="hint">How many recent messages to send to the LLM (max 15). Default: 5. Higher values risk context-window exhaustion and empty responses.</p>
          </div>
          <div class="form-group">
            <label>Total Messages (0 = unlimited)</label>
            <input type="number" min="0" max="1000" bind:value={defaultsForm.max_messages} onchange={saveDefaults} />
            <p class="hint">Maximum messages stored in the conversation history.</p>
          </div>
        </div>

        <div class="form-card">
          <h4>Toggles</h4>
          <div class="checkbox-group">
            <label>
              <input type="checkbox" bind:checked={defaultsForm.tech_details} onchange={saveDefaults} />
              SHOW TECHNICAL DETAILS
            </label>
          </div>
          <div class="checkbox-group">
            <label>
              <input type="checkbox" bind:checked={defaultsForm.context_details} onchange={saveDefaults} />
              Show context &amp; token details
            </label>
          </div>
          <div class="checkbox-group">
            <label>
              <input type="checkbox" bind:checked={defaultsForm.summarize} onchange={saveDefaults} />
              Summarize results
            </label>
            <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
              LLM summarizes query results as a plain-English answer
            </div>
          </div>
          <div class="checkbox-group">
            <label>
              <input type="checkbox" bind:checked={defaultsForm.viz_enabled} onchange={saveDefaults} />
              Data visualization
            </label>
            <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
              LLM generates charts (bar, line, pie, scatter) when appropriate
            </div>
          </div>
          <div class="checkbox-group">
            <label>
              <input type="checkbox" bind:checked={defaultsForm.streaming_enabled} onchange={saveDefaults} />
              Stream LLM output
            </label>
            <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
              Show model output character-by-character in real time
            </div>
          </div>
        </div>

        <button class="btn btn-secondary" onclick={resetDefaults}>
          Reset to System Defaults
        </button>
      </div>
    {:else if activeSettingsTab === 'general'}
      <div class="settings-section">
        <h3>General Settings</h3>
        <p class="section-desc">Application-wide preferences</p>

        <!-- Theme Mode -->
        <div class="form-card">
          <h4>Theme</h4>
          <p class="card-hint">Choose your preferred appearance.</p>
          <div class="theme-options">
            <button
              class="theme-option-btn {currentTheme === 'light' ? 'active' : ''}"
              onclick={() => { currentTheme = 'light'; window.setThemeSelection('light') }}
            >
              <span class="theme-label">Light</span>
            </button>
            <button
              class="theme-option-btn {currentTheme === 'dark' ? 'active' : ''}"
              onclick={() => { currentTheme = 'dark'; window.setThemeSelection('dark') }}
            >
              <span class="theme-label">Dark</span>
            </button>
            <button
              class="theme-option-btn {currentTheme === 'system' ? 'active' : ''}"
              onclick={() => { currentTheme = 'system'; window.setThemeSelection('system') }}
            >
              <span class="theme-label">System</span>
            </button>
          </div>
          <p class="theme-hint">System follows your OS appearance setting.</p>
        </div>

        <!-- Accent Color -->
        <div class="form-card">
          <h4>Accent Color</h4>
          <p class="card-hint">Choose a color for buttons, links, and interactive elements.</p>
          <div class="accent-presets">
            {#each accentPresets as preset}
              <button
                class="accent-swatch {currentAccent === preset.hex ? 'active' : ''}"
                style="background: {preset.hex}"
                title="{preset.name}"
                onclick={() => { currentAccent = preset.hex; window.setAccent(preset.hex) }}
              >
                {#if currentAccent === preset.hex}<span class="check">✓</span>{/if}
              </button>
            {/each}
          </div>
          <div class="custom-accent">
            <label for="custom-accent-input">Custom Color</label>
            <div class="custom-accent-row">
              <input
                id="custom-accent-input"
                type="text"
                class="custom-hex-input"
                placeholder="#0288d1"
                value={currentAccent}
                oninput={(e) => {
                  const val = e.target.value
                  if (/^#?([a-f\d]{3}){1,2}$/i.test(val)) {
                    let hex = val.startsWith('#') ? val : '#' + val
                    if (hex.length === 4) hex = '#' + hex.slice(1).split('').map(c => c + c).join('')
                    currentAccent = hex
                    window.setAccent(hex)
                  }
                }}
              />
            </div>
            <p class="hint">Enter any hex color (e.g. #ff6b9d). Dark mode variant auto-computed.</p>
            {#if accentLuminanceWarning}
              <p class="accent-warning">{accentLuminanceWarning}</p>
            {/if}
          </div>
        </div>

        <!-- UI Scale -->
        <div class="form-card">
          <h4>UI Scale</h4>
          <p class="card-hint">Adjust the size of text and interface elements across the application.</p>
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

        <!-- Agent Loop Advanced Settings Toggle -->
        <div class="form-card">
          <h4>Advanced</h4>
          <div class="checkbox-group">
            <label>
              <input type="checkbox" checked={agentLoopEnabled} onchange={toggleAgentLoop} />
              Enable advanced agent loop settings
            </label>
            <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
              Customize the prompts, tool descriptions, and instructions your AI model receives. For advanced users who want fine-grained control over model behavior.
            </div>
          </div>
        </div>
      </div>
    {:else if activeSettingsTab === 'agentloop'}
      <div class="settings-section">
        <h3>Agent Loop</h3>
        <p class="section-desc">
          Customize what your AI model receives. These are advanced settings — changes here directly affect how the model behaves.
        </p>

        <div class="agent-loop-warning">
          <strong>⚠ Use with caution.</strong> Changing prompts or tool descriptions in ways that contradict YourQL's designed functionality — such as removing instructions about read-only queries, altering the one-shot finality rule, or rewriting parameter descriptions to claim capabilities the tools don't have — will cause models to use tools incorrectly and may break core application behavior. If things go wrong, use <strong>Restore All Defaults</strong> below.
        </div>

        {#if agentLoopStatus}
          <div class="status-message {agentLoopStatus.startsWith('Error') ? 'error' : 'success'}">
            {agentLoopStatus}
          </div>
        {/if}

        <button class="btn btn-secondary" onclick={resetAllAgentLoop} style="margin-bottom: var(--space-4xl);">
          Restore All Defaults
        </button>

        {#each sectionNames as section}
          {@const sectionFields = agentLoopFields.filter(f => f.section === section)}
          {#if sectionFields.length > 0}
            <div class="form-card agent-loop-section">
              <h4>{sectionLabels[section] || section}</h4>
              {#each sectionFields as field (field.key)}
                <div class="form-group" style="margin-bottom: var(--space-3xl);">
                  <label>{field.label}</label>
                  <div class="field-tooltip">{field.description}</div>
                  <textarea
                    class="agent-loop-textarea"
                    value={agentLoopConfigValues[field.key] || ''}
                    placeholder={field.description}
                    rows={field.key.startsWith('safety.') || field.key.startsWith('charts.') ? 3 : 2}
                    onblur={(e) => saveAgentLoopField(field.key, e.target.value)}
                  ></textarea>
                  <button
                    class="btn btn-small"
                    onclick={() => resetAgentLoopField(field.key)}
                    style="margin-top: var(--space-sm);"
                  >
                    Reset
                  </button>
                </div>
              {/each}
            </div>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</div>

{#if showSkillEditor && skillEditor}
  <div class="skill-editor-overlay" onclick={() => { showSkillEditor = false; skillEditor = null }}>
    <div class="skill-editor" onclick={(e) => e.stopPropagation()}>
      <h3>{skillEditor.id ? 'Edit Skill' : 'New Skill'}</h3>
      <label>Name</label>
      <input type="text" bind:value={skillEditor.name} placeholder="Sales Domain Context" />
      <label>Markdown Content</label>
      <textarea bind:value={skillEditor.markdown_content} placeholder="Revenue is in USD. Fiscal year starts July 1.&#10;Exclude test accounts from all queries."></textarea>
      <div class="skill-editor-actions">
        <button class="btn btn-primary" onclick={handleSaveSkill}>Save</button>
        <button class="btn btn-secondary" onclick={() => { showSkillEditor = false; skillEditor = null }}>Cancel</button>
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
    border-bottom: 1px solid var(--border-primary);
  }

  .tab-btn {
    padding: var(--space-xl) var(--space-4xl);
    background: transparent;
    border: none;
    border-bottom: 3px solid transparent;
    color: var(--text-muted);
    font-size: var(--font-md);
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .tab-btn:hover {
    color: var(--text-primary);
  }

  .tab-btn.active {
    color: var(--color-accent);
    border-bottom-color: var(--color-accent);
    font-weight: 600;
  }

  .settings-content {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-6xl) var(--space-7xl) 0;
    background: var(--bg-primary);
  }

  .settings-section {
    max-width: var(--content-max-width);
  }

  h3 {
    margin: 0 0 var(--space-lg);
    color: var(--text-primary);
    font-size: var(--font-4xl);
  }

  .section-desc {
    color: var(--text-muted);
    margin: 0 0 var(--space-6xl);
  }

  .form-card {
    background: var(--bg-tertiary);
    padding: var(--space-5xl);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-4xl);
    border: 1px solid var(--border-primary);
  }

  .form-card h4 {
    margin: 0 0 var(--space-4xl);
    color: var(--color-accent);
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
    color: var(--text-secondary);
    font-weight: 500;
  }

  .form-group label .required {
    color: var(--color-danger-hover);
    margin-left: 2px;
  }

  .form-group input,
  .form-group select {
    padding: var(--space-lg) var(--space-xl);
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-md);
  }

  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .form-actions {
    display: flex;
    gap: var(--space-lg);
  }

  .input-with-detect {
    display: flex;
    gap: var(--space-md);
    align-items: center;
  }
  .input-with-detect input {
    flex: 1;
  }

  .field-hint {
    font-size: var(--font-sm);
    color: var(--text-secondary);
    margin-top: var(--space-xs);
    display: block;
  }
  .field-hint.warning {
    color: var(--color-warning, #f57c00);
  }

  .btn {
    padding: var(--space-lg) var(--space-4xl);
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .providers-list-header {
    display: flex;
    justify-content: flex-end;
    margin-bottom: var(--space-4xl);
  }

  .btn-primary {
    background: var(--color-accent);
    color: #ffffff;
    font-weight: 600;
  }

  .btn-primary:hover {
    background: var(--color-accent-hover);
  }

  .btn-secondary {
    background: var(--border-primary);
    color: var(--text-secondary);
    border: 1px solid var(--text-secondary);
  }

  .btn-secondary:hover {
    background: var(--text-muted);
  }

  .btn-small {
    padding: var(--space-sm) var(--space-xl);
    font-size: var(--font-sm);
    background: var(--border-primary);
    color: var(--text-secondary);
    border: 1px solid var(--text-secondary);
  }

  .btn-small:hover {
    background: var(--text-muted);
  }

  .btn-danger {
    background: var(--color-accent-light);
    color: var(--color-danger);
    border: 1px solid var(--color-accent-border);
  }

  .btn-danger:hover {
    background: var(--color-accent-light);
  }

  .status-message {
    padding: var(--space-xl) var(--space-2xl);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-4xl);
    font-size: var(--font-base);
  }

  .status-message.success {
    background: var(--color-accent-light);
    color: var(--color-accent);
    border: 1px solid var(--color-accent-border);
  }

  .status-message.error {
    background: rgba(244, 67, 54, 0.2);
    color: var(--color-danger);
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
    color: var(--color-accent);
    font-size: var(--font-xl);
  }

  .provider-card,
  .connection-card,
  .data-source-item {
    background: var(--bg-tertiary);
    padding: var(--space-2xl);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-lg);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2xl);
    border: 1px solid var(--border-primary);
  }

  .data-source-left {
    display: flex;
    flex-direction: column;
    gap: var(--space-sm);
    min-width: 0;
    flex: 1 1 auto;
  }

  .provider-card-left {
    display: flex;
    flex-direction: column;
    gap: var(--space-sm);
    min-width: 0;
    flex: 1 1 auto;
  }

  .provider-info {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
  }

  .data-source-info {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    flex-wrap: wrap;
    min-width: 0;
  }

  .provider-name {
    font-weight: 600;
    color: var(--text-primary);
  }

  .data-source-name {
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 280px;
  }

  .provider-type,
  .data-source-type {
    font-size: var(--font-sm);
    color: var(--text-tertiary);
    background: var(--border-primary);
    padding: 3px var(--space-md);
    border-radius: var(--radius-md);
    white-space: nowrap;
    flex-shrink: 0;
  }

  .provider-details {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
  }

  .data-source-details {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    flex-wrap: wrap;
    min-width: 0;
  }

  .detail {
    font-size: var(--font-sm);
    color: var(--text-muted);
  }

  .badge {
    font-size: var(--font-xs);
    padding: 3px var(--space-md);
    border-radius: var(--radius-md);
    font-weight: 600;
  }

  .badge.wip {
    background: var(--color-accent-light);
    color: #e65100;
    border: 1px solid #ffcc80;
    margin-left: var(--space-md);
  }

  .badge.default {
    background: var(--color-accent-light);
    color: var(--color-accent);
    border: 1px solid var(--color-accent-border);
  }

  .badge.exploration {
    background: rgba(129, 236, 236, 0.15);
    color: var(--color-accent);
    border: 1px solid rgba(129, 236, 236, 0.3);
  }

  .provider-actions {
    display: flex;
    gap: var(--space-md);
    flex-shrink: 0;
    flex-wrap: nowrap;
  }

  .data-source-actions {
    display: flex;
    gap: var(--space-md);
    flex-shrink: 0;
    flex-wrap: nowrap;
  }

  .empty-hint {
    color: var(--text-tertiary);
    font-style: italic;
    margin-top: var(--space-lg);
  }

  .sqlite-info {
    background: var(--color-accent-light);
    border: 1px solid var(--color-accent-border);
    padding: var(--space-2xl) var(--space-4xl);
    border-radius: var(--radius-md);
    margin-top: var(--space-4xl);
  }

  .sqlite-info p {
    margin: 0 0 var(--space-md);
    color: var(--color-accent);
    font-size: var(--font-md);
  }

  .sqlite-info p:last-child {
    margin-bottom: 0;
  }

  .safety-hint {
    margin-top: var(--space-md);
    padding: var(--space-xl) var(--space-3xl);
    margin-bottom: var(--space-md);
    background: var(--bg-surface);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-base);
    color: var(--text-secondary);
    line-height: 1.5;
  }

  .safety-hint strong {
    color: var(--text-primary);
    font-weight: 600;
  }

  .sqlite-info .hint {
    color: var(--text-tertiary);
    font-size: var(--font-base);
  }

  .edit-modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--shadow);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    animation: fadeIn 0.2s ease;
  }

  .edit-modal {
    background: var(--bg-primary);
    color: var(--text-primary);
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
    border-bottom: 1px solid var(--border-primary);
  }

  .edit-modal-header h3 {
    margin: 0;
    color: var(--text-primary);
    font-size: var(--font-2xl);
  }

  .close-btn {
    background: none;
    border: none;
    font-size: var(--font-4xl);
    color: var(--text-muted);
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
    background: var(--border-secondary);
    color: var(--text-primary);
  }

  .edit-modal-body {
    padding: var(--space-5xl);
  }

  .edit-modal-body .form-group {
    margin-bottom: var(--space-2xl);
  }

  .edit-modal-body .form-group label {
    font-size: var(--font-base);
    color: var(--text-muted);
    font-weight: 500;
    margin-bottom: var(--space-xs);
    display: block;
  }

  .edit-modal-body .form-group input,
  .edit-modal-body .form-group select {
    width: 100%;
    padding: var(--space-lg) var(--space-xl);
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-md);
  }

  .edit-modal-body .form-group input:focus,
  .edit-modal-body .form-group select:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .edit-modal-body .form-group input:disabled,
  .edit-modal-body .form-group select:disabled {
    background: var(--bg-secondary);
    color: var(--text-muted);
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
    border-top: 2px solid var(--border-primary);
  }

  .exploration-config h5 {
    margin: 0 0 var(--space-md);
    color: var(--color-accent);
    font-size: var(--font-xl);
  }

  .exploration-config .form-group {
    margin-bottom: var(--space-xl);
  }

  .exploration-config label {
    font-size: var(--font-base);
    color: var(--text-secondary);
    font-weight: 500;
    display: flex;
    align-items: center;
    gap: var(--space-md);
  }

  .exploration-config input[type="checkbox"] {
    width: var(--space-3xl);
    height: var(--space-3xl);
    accent-color: var(--color-accent);
  }

  .exploration-config input[type="number"] {
    padding: var(--space-md) var(--space-lg);
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-md);
  }

  .exploration-config input[type="number"]:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .exploration-config select {
    padding: var(--space-md) var(--space-lg);
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-md);
  }

  .exploration-config select:focus {
    outline: none;
    border-color: var(--color-accent);
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
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    padding: var(--space-4xl);
    margin-top: var(--space-4xl);
  }

  .schema-preview h4 {
    margin: 0 0 var(--space-2xl);
    color: var(--text-primary);
    font-size: var(--font-xl);
  }

  .schema-sort-controls {
    display: flex;
    gap: var(--space-md);
    margin-bottom: var(--space-3xl);
    padding-bottom: var(--space-xl);
    border-bottom: 1px solid var(--border-primary);
  }

  .sort-btn {
    padding: var(--space-sm) var(--space-xl);
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.2s ease;
    display: flex;
    align-items: center;
    gap: var(--space-xs);
  }

  .sort-btn:hover {
    background: var(--border-primary);
    color: var(--text-secondary);
  }

  .sort-btn.active {
    background: var(--color-accent);
    color: #fff;
    border-color: var(--color-accent);
  }

  .sort-btn.active:hover {
    background: var(--color-accent-hover);
  }

  .schema-preview .loading {
    text-align: center;
    color: var(--text-muted);
  }

  .schema-table {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-primary);
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
    border-bottom: 1px solid var(--border-primary);
  }

  .schema-table-header strong {
    color: var(--text-primary);
    font-size: var(--font-md);
  }

  .row-count {
    font-size: var(--font-sm);
    color: var(--text-muted);
  }

  .schema-columns {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-sm);
  }

  .schema-col {
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    padding: 3px var(--space-md);
    font-size: var(--font-sm);
    color: var(--text-secondary);
  }

  .schema-col.pk {
    border-color: var(--color-accent);
    background: var(--color-accent-light);
  }

  .schema-col em {
    color: var(--text-muted);
    font-style: normal;
  }

  /* Editable Schema */
  .schema-table-editable {
    margin-bottom: var(--space-5xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .schema-table-desc {
    padding: var(--space-lg) var(--space-3xl);
    background: var(--bg-tertiary);
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    align-items: center;
    gap: var(--space-xl);
  }

  .schema-table-desc label {
    font-size: var(--font-sm);
    font-weight: 600;
    color: var(--text-secondary);
    white-space: nowrap;
  }

  .schema-table-desc input {
    flex: 1;
    border: 1px solid var(--border-primary);
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
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-primary);
    font-size: var(--font-sm);
    font-weight: 600;
    color: var(--text-secondary);
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
    border-bottom: 1px solid var(--border-secondary);
    font-size: var(--font-base);
  }

  .schema-col-row:last-child {
    border-bottom: none;
  }

  .schema-col-row.pk {
    background: var(--color-accent-light);
  }

  .col-name {
    flex: 0 0 10rem;
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    font-family: 'Courier New', monospace;
    font-size: var(--font-sm);
    color: var(--text-secondary);
  }

  .pk-badge {
    background: var(--color-accent);
    color: #fff;
    font-size: var(--font-2xs);
    padding: 1px var(--space-xs);
    border-radius: var(--radius-md);
    font-weight: 600;
  }

  .null-badge {
    color: var(--text-tertiary);
    font-size: var(--font-xs);
  }

  .col-type {
    flex: 0 0 6.25rem;
    font-size: var(--font-sm);
    color: var(--text-muted);
    font-style: italic;
  }

  .col-desc-input {
    flex: 1;
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    padding: var(--space-xs) var(--space-md);
    font-size: var(--font-sm);
    transition: border-color 0.2s ease;
  }

  .col-desc-input:focus {
    border-color: var(--color-accent);
    outline: none;
  }

  /* Exploration Queries */
  .exploration-section {
    margin-top: var(--space-6xl);
    padding-top: var(--space-6xl);
    border-top: 2px solid var(--border-primary);
  }

  .exploration-section h4 {
    margin: 0 0 var(--space-lg);
    color: var(--text-primary);
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
    border-color: var(--color-accent);
    background: transparent;
  }

  .query-results {
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
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
    border-bottom: 1px solid var(--border-primary);
  }

  .results-header span {
    font-size: var(--font-md);
    color: var(--color-accent);
    font-weight: 600;
  }

  .results-table {
    overflow-x: auto;
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
  }

  .table-header {
    display: flex;
    background: var(--bg-secondary);
    border-bottom: 2px solid var(--border-primary);
  }

  .table-row {
    display: flex;
    border-bottom: 1px solid var(--border-primary);
  }

  .table-row:last-child {
    border-bottom: none;
  }

  .table-cell {
    flex: 1;
    padding: var(--space-lg) var(--space-xl);
    font-size: var(--font-base);
    color: var(--text-secondary);
    border-right: 1px solid var(--border-primary);
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
    color: var(--text-primary);
  }

  .db-detail-content {
    display: flex;
    flex-direction: column;
    gap: var(--space-5xl);
  }

  .db-section {
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    padding: var(--space-4xl);
  }

  .db-section h4 {
    margin: 0 0 var(--space-3xl) 0;
    font-size: var(--font-xl);
    font-weight: 600;
    color: var(--text-primary);
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
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    padding: var(--space-3xl) 0 var(--space-6xl) 0;
    border-top: 1px solid var(--border-primary);
    position: sticky;
    bottom: 0;
    background: var(--bg-primary);
    z-index: 10;
  }

  .db-actions-left {
    display: flex;
    gap: var(--space-md);
  }

  .badge.db-type {
    background: var(--color-accent-light);
    color: var(--color-accent);
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
    background: var(--bg-tertiary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
  }

  .scale-option-btn:hover {
    background: var(--border-secondary);
    border-color: var(--color-accent);
  }

  .scale-option-btn.active {
    background: var(--color-accent-light);
    border-color: var(--color-accent);
    border-width: 2px;
  }

  .scale-option-label {
    font-size: var(--font-md);
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: var(--space-2xs);
  }

  .scale-option-desc {
    font-size: var(--font-sm);
    color: var(--text-tertiary);
  }

  /* Skill cards */
  .skill-card {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    padding: var(--space-lg) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: 8px;
    margin-bottom: var(--space-md);
    background: var(--bg-primary);
    gap: var(--space-lg);
  }
  .skill-card-main {
    flex: 1;
    min-width: 0;
  }
  .skill-card-name {
    font-weight: 600;
    font-size: var(--font-base);
    margin-bottom: var(--space-2xs);
    color: var(--text-primary);
  }
  .skill-card-preview {
    color: var(--text-secondary);
    font-size: var(--font-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .skill-card-actions {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    flex-shrink: 0;
  }
  .skill-card-actions button {
    padding: var(--space-xs) var(--space-sm);
    font-size: var(--font-sm);
    border: 1px solid var(--border-primary);
    border-radius: 4px;
    background: var(--bg-primary);
    color: var(--text-primary);
    cursor: pointer;
  }
  .skill-card-actions button:hover {
    background: var(--bg-secondary);
  }
  .skill-card-actions button.delete:hover {
    background: var(--color-accent-light);
    color: var(--color-danger);
    border-color: var(--color-accent-border);
  }

  /* Toggle switch */
  .toggle-switch {
    position: relative;
    display: inline-block;
    width: 44px;
    height: 24px;
  }
  .toggle-switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }
  .slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--text-tertiary);
    border-radius: 24px;
    transition: 0.2s;
  }
  .slider:before {
    position: absolute;
    content: "";
    height: 18px;
    width: 18px;
    left: 3px;
    bottom: 3px;
    background: var(--bg-primary);
    border-radius: 50%;
    transition: 0.2s;
  }
  input:checked + .slider {
    background: var(--color-accent);
  }
  input:checked + .slider:before {
    transform: translateX(20px);
  }

  /* Skill editor modal */
  .skill-editor-overlay {
    position: fixed;
    inset: 0;
    background: var(--shadow);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .skill-editor {
    background: var(--bg-primary);
    color: var(--text-primary);
    border-radius: 12px;
    padding: var(--space-2xl);
    width: 600px;
    max-width: 90vw;
    max-height: 80vh;
    overflow-y: auto;
  }
  .skill-editor h3 {
    margin: 0 0 var(--space-lg) 0;
    color: var(--text-primary);
  }
  .skill-editor label {
    display: block;
    font-weight: 600;
    margin-bottom: var(--space-xs);
    font-size: var(--font-sm);
    color: var(--text-secondary);
  }
  .skill-editor input[type="text"] {
    width: 100%;
    padding: var(--space-sm);
    border: 1px solid var(--border-primary);
    border-radius: 4px;
    font-size: var(--font-base);
    margin-bottom: var(--space-lg);
    box-sizing: border-box;
    background: var(--bg-primary);
    color: var(--text-primary);
  }
  .skill-editor input[type="text"]:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .skill-editor textarea {
    width: 100%;
    min-height: 200px;
    padding: var(--space-sm);
    border: 1px solid var(--border-primary);
    border-radius: 4px;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: var(--font-sm);
    line-height: 1.5;
    resize: vertical;
    margin-bottom: var(--space-lg);
    box-sizing: border-box;
    background: var(--bg-primary);
    color: var(--text-primary);
  }
  .skill-editor textarea:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .skill-editor-actions {
    display: flex;
    gap: var(--space-md);
    justify-content: flex-end;
  }

  /* ===== Theme & Accent Controls ===== */
  .card-hint {
    color: var(--text-muted);
    margin-bottom: var(--space-2xl);
    font-size: var(--font-base);
  }

  /* Theme options */
  .theme-options {
    display: flex;
    gap: var(--space-md);
    margin-bottom: var(--space-md);
  }

  .theme-option-btn {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-sm);
    padding: var(--space-xl) var(--space-4xl);
    background: var(--bg-tertiary);
    border: 2px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.2s ease;
    min-width: 100px;
  }

  .theme-option-btn:hover {
    background: var(--bg-surface);
    border-color: var(--color-accent);
  }

  .theme-option-btn.active {
    background: var(--color-accent-light);
    border-color: var(--color-accent);
  }

  .theme-label {
    font-size: var(--font-md);
    font-weight: 600;
    color: var(--text-primary);
  }

  .theme-hint {
    color: var(--text-muted);
    font-size: var(--font-sm);
    margin-top: 0;
  }

  /* Accent presets */
  .accent-presets {
    display: flex;
    gap: var(--space-md);
    margin-bottom: var(--space-2xl);
  }

  .accent-swatch {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    border: 3px solid transparent;
    cursor: pointer;
    transition: all 0.2s ease;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    outline: none;
  }

  .accent-swatch:hover {
    transform: scale(1.15);
  }

  .accent-swatch.active {
    border-color: var(--text-primary);
    box-shadow: 0 0 0 2px var(--text-primary);
  }

  .accent-swatch .check {
    color: #fff;
    font-size: var(--font-md);
    text-shadow: 0 1px 2px rgba(0,0,0,0.3);
    font-weight: bold;
  }

  /* Custom accent */
  .custom-accent {
    margin-top: var(--space-md);
  }

  .custom-accent label {
    display: block;
    font-size: var(--font-sm);
    font-weight: 600;
    color: var(--text-secondary);
    margin-bottom: var(--space-sm);
  }

  .custom-accent-row {
    display: flex;
    align-items: center;
    gap: var(--space-md);
  }

  .custom-hex-input {
    padding: var(--space-md) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-base);
    font-family: 'SF Mono', Monaco, monospace;
    width: 200px;
    background: var(--bg-primary);
    color: var(--text-primary);
  }

  .custom-hex-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .hint {
    color: var(--text-tertiary);
    font-size: var(--font-sm);
    margin-top: var(--space-md);
  }

  .accent-warning {
    color: var(--color-warning, #f57c00);
    font-size: var(--font-sm);
    margin-top: var(--space-md);
    padding: var(--space-sm) var(--space-lg);
    background: rgba(245, 124, 0, 0.1);
    border: 1px solid rgba(245, 124, 0, 0.25);
    border-radius: var(--radius-md);
  }

  /* Agent Loop Config */
  .agent-loop-textarea {
    width: 100%;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: var(--font-sm);
    line-height: 1.5;
    resize: vertical;
    padding: var(--space-md) var(--space-lg);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    background: var(--bg-primary);
    color: var(--text-primary);
  }

  .agent-loop-textarea:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .field-tooltip {
    color: var(--text-tertiary);
    font-size: var(--font-xs);
    margin-bottom: var(--space-sm);
    line-height: 1.4;
  }

  .agent-loop-warning {
    background: rgba(245, 124, 0, 0.08);
    border: 1px solid rgba(245, 124, 0, 0.25);
    border-radius: var(--radius-md);
    padding: var(--space-xl) var(--space-2xl);
    margin-bottom: var(--space-4xl);
    color: var(--text-primary);
    font-size: var(--font-base);
    line-height: 1.6;
  }
</style>
