<script>
  import { EventsOn } from '../wailsjs/runtime/runtime.js'

  // ==================== Processing Phase Listener ====================
  EventsOn('processingPhase', (phase) => {
    processingMessage = phase
  })
  EventsOn('processingComplete', () => {
    processingMessage = ''
    processingConversationId = null
  })
  EventsOn('processingCancelled', async (data) => {
    processingMessage = ''
    processingConversationId = null
    // Refresh messages to show the "Cancelled" system message.
    if (data.conversation_id === activeConversation?.id) {
      conversationMessages = await GetConversationMessages(activeConversation.id)
    }
  })

  import { ListConversations, CreateConversation, GetConversationMessages, ProcessUserMessage, CancelProcessing, DeleteConversation, UpdateConversationTechDetails, ArchiveConversation, RestoreConversation, UpdateConversationSettings, ListLLMProviders, ListDataSources, UpdateConversationTitle, UpdateConversationMaxMessages, UpdateConversationMaxContextMessages, UpdateConversationPinned, DuplicateConversation, ClearConversationMessages, UpdateConversationContextDetails, UpdateConversationSummarize, UpdateConversationVizEnabled, UpdateConversationStreamingEnabled, ListSkills, GetConversationSkillIDs, SetConversationSkill, GetDiscussionDefaults, GetAppVersion, CheckForUpdate, DownloadUpdate, PerformUpgradeRestart } from '../wailsjs/go/main/App.js'
  import { MessageSquare, Settings, X, Copy, Trash2, Pin, ChevronRight, ChevronLeft, Plus } from 'lucide-svelte'
  import SettingsView from './SettingsView.svelte'
  import ConversationView from './ConversationView.svelte'

  let activeView = $state('discussions')
  let conversations = $state([])
  let llmProviders = $state([])
  let dataSources = $state([])
  let allSkills = $state([])
  let conversationSkillIDs = $state([])
  let sidebarCollapsed = $state(false)

  // --- Version & auto-update state ---
  let appVersion = $state('loading...')
  let updateInfo = $state(null)
  let updateChecking = $state(false)
  let updateDownloading = $state(false)
  let updateError = $state(null)
  let updateReady = $state(false)

  // Fetch the running version from the backend (injected at build time).
  async function loadAppVersion() {
    try {
      appVersion = await GetAppVersion()
    } catch {
      appVersion = 'unknown'
    }
  }
  loadAppVersion()

  // --- Auto-update helpers ---
  async function handleCheckForUpdate() {
    updateChecking = true
    updateError = null
    updateReady = false
    updateInfo = null
    try {
      updateInfo = await CheckForUpdate()
    } catch (e) {
      updateError = e || 'Unable to reach update server'
    } finally {
      updateChecking = false
    }
  }

  async function handleDownloadUpdate() {
    if (!updateInfo?.download_url || !updateInfo?.asset_checksum) {
      updateError = 'No verified download available'
      return
    }
    updateDownloading = true
    updateError = null
    try {
      await DownloadUpdate(updateInfo.download_url, updateInfo.asset_checksum)
      updateReady = true
    } catch (e) {
      updateError = e || 'Download failed'
    } finally {
      updateDownloading = false
    }
  }

  async function handlePerformUpgradeRestart() {
    try {
      await PerformUpgradeRestart()
    } catch {
      // The app exits before this resolves; ignore.
    }
  }

  let llmNameByID = $derived(
    Object.fromEntries(llmProviders.map(p => [p.id, p.name]))
  )
  let dataSourceNameByID = $derived(
    Object.fromEntries(dataSources.map(c => [c.id, c.name]))
  )

  let status = $state("Ready")
  let error = $state(null)

  let showNewDiscussion = $state(false)
  let newDiscussionTitle = $state('')
  let selectedLLMProvider = $state(null)
  let selectedDataSource = $state(null)

  async function openNewDiscussionModal() {
    // Pre-fill with user's defaults
    try {
      const defaults = await GetDiscussionDefaults()
      if (defaults && defaults.llm_provider_id) {
        selectedLLMProvider = llmProviders.find(p => p.id === defaults.llm_provider_id) || null
      }
      if (defaults && defaults.data_source_id) {
        selectedDataSource = dataSources.find(ds => ds.id === defaults.data_source_id) || null
      }
    } catch (e) {
      // Ignore — defaults are a convenience, not a requirement
    }
    showNewDiscussion = true
  }
  let creating = $state(false)
  let createError = $state(null)

  let activeConversation = $state(null)
  let conversationMessages = $state([])
  let userMessage = $state('')
  let processingMessage = $state('')
  let processingConversationId = $state(null)
  let messageError = $state(null)
  let showTechDetails = $state(false)
  let showContextDetails = $state(false)
  let selectedConversation = $state(null)
  let showGearPopover = $state(false)

  // Reset the message input when switching discussions.
  $effect(() => {
    activeConversation?.id
    userMessage = ''
  })

  $effect(() => {
    activeView
    showGearPopover = false
    selectedConversation = null
  })

  let deleteTargetId = $state(null)
  let deleteTargetTitle = $state('')
  let deleting = $state(false)

  let showArchived = $state(false)

  async function loadData() {
    status = "Loading..."
    error = null
    try {
      const convRes = await ListConversations()
      conversations = (convRes || []).filter(c => showArchived || c.status !== 'archived')

      const llmRes = await ListLLMProviders()
      llmProviders = llmRes || []

      const dbRes = await ListDataSources()
      dataSources = dbRes || []

      try {
        allSkills = await ListSkills() || []
      } catch (_) {}

      status = "Loaded successfully"
    } catch (e) {
      error = e.toString()
      status = "Error loading data"
    }
  }

  loadData()

  async function handleCreateDiscussion() {
    if (!newDiscussionTitle.trim()) {
      createError = 'Please enter a title'
      return
    }

    creating = true
    createError = null

    try {
      const llmProviderID = selectedLLMProvider ? selectedLLMProvider.id : null
      const dataSourceID = selectedDataSource ? selectedDataSource.id : null

      const conversation = await CreateConversation(newDiscussionTitle.trim(), llmProviderID, dataSourceID)

      showNewDiscussion = false
      newDiscussionTitle = ''
      selectedLLMProvider = null
      selectedDataSource = null

      await loadData()

      activeConversation = conversation
      activeView = 'conversation'
      conversationMessages = await GetConversationMessages(conversation.id)
    } catch (e) {
      createError = e.toString()
    } finally {
      creating = false
    }
  }

  async function openConversation(conversation) {
    activeConversation = conversation
    activeView = 'conversation'
    showTechDetails = conversation.tech_details ?? false
    showContextDetails = conversation.context_details ?? false

    try {
      conversationMessages = await GetConversationMessages(conversation.id)
    } catch (e) {
      messageError = e.toString()
    }
  }

  function requestDeleteConversation(id, title) {
    deleteTargetId = id
    deleteTargetTitle = title || 'Untitled'
  }

  function cancelDelete() {
    deleteTargetId = null
    deleteTargetTitle = ''
  }

  async function confirmDeleteConversation() {
    if (!deleteTargetId) return
    deleting = true
    try {
      await DeleteConversation(deleteTargetId)
      conversations = conversations.filter(c => c.id !== deleteTargetId)
      if (activeConversation && activeConversation.id === deleteTargetId) {
        activeConversation = null
        activeView = 'discussions'
        conversationMessages = []
      }
    } catch (e) {
      error = 'Failed to delete: ' + e.toString()
    } finally {
      deleting = false
      deleteTargetId = null
      deleteTargetTitle = ''
    }
  }

  async function handleSendMessage() {
    if (!userMessage.trim() || !activeConversation || processingConversationId) return

    processingConversationId = activeConversation.id
    processingMessage = 'Thinking...'
    messageError = null

    // Optimistically add user message to the thread for smooth animation
    const tempId = -(Date.now())
    const optimisticMsg = {
      id: tempId,
      role: 'user',
      content: userMessage.trim(),
      created_at: new Date().toISOString(),
      metadata: null
    }
    conversationMessages = [...conversationMessages, optimisticMsg]
    const msgToSend = userMessage.trim()
    userMessage = ''

    try {
      await ProcessUserMessage(activeConversation.id, msgToSend)
      conversationMessages = await GetConversationMessages(activeConversation.id)
    } catch (e) {
      messageError = e.toString()
      // Remove optimistic message on error
      conversationMessages = conversationMessages.filter(m => m.id !== tempId)
    } finally {
      processingMessage = ''
      processingConversationId = null
      // Refresh the conversation list so the active conversation floats to the top
      loadData()
    }
  }

  async function backToConversations() {
    activeConversation = null
    conversationMessages = []
    activeView = 'discussions'
    await loadData()
  }

  async function handleUpdateConversationSettings(llmProviderID, dataSourceID) {
    if (!activeConversation) return
    try {
      await UpdateConversationSettings(activeConversation.id, llmProviderID, dataSourceID)
      if (llmProviderID !== null) activeConversation.llm_provider_id = llmProviderID
      if (dataSourceID !== null) activeConversation.data_source_id = dataSourceID
      activeConversation.updated_at = new Date().toISOString()
      const convRes = await ListConversations()
      conversations = (convRes || []).filter(c => showArchived || c.status !== 'archived')
    } catch (e) {
      console.error('Failed to update conversation settings:', e)
    }
  }

  async function handleArchiveConversation() {
    if (!selectedConversation) return
    try {
      await ArchiveConversation(selectedConversation.id)
      backToConversations()
      const convRes = await ListConversations()
      conversations = (convRes || []).filter(c => showArchived || c.status !== 'archived')
    } catch (e) {
      console.error('Failed to archive conversation:', e)
    }
  }

  // ==================== New conversation settings handlers ====================
  async function handleRenameConversation() {
    if (!selectedConversation || !selectedConversation.title?.trim()) return
    try {
      const updated = await UpdateConversationTitle(selectedConversation.id, selectedConversation.title.trim())
      if (activeConversation && activeConversation.id === selectedConversation.id) {
        activeConversation.title = updated.title
      }
      await loadData()
    } catch (e) {
      console.error('Failed to rename conversation:', e)
    }
  }

  async function handleSetMaxMessages(maxMessages) {
    if (!selectedConversation) return
    try {
      await UpdateConversationMaxMessages(selectedConversation.id, maxMessages)
      if (activeConversation && activeConversation.id === selectedConversation.id) {
        activeConversation.max_messages = maxMessages
      }
    } catch (e) {
      console.error('Failed to set max messages:', e)
    }
  }

  async function handleSetMaxContextMessages(maxContextMessages) {
    if (!selectedConversation) return
    try {
      await UpdateConversationMaxContextMessages(selectedConversation.id, maxContextMessages)
      if (activeConversation && activeConversation.id === selectedConversation.id) {
        activeConversation.max_context_messages = maxContextMessages
      }
    } catch (e) {
      console.error('Failed to set max context messages:', e)
    }
  }

  async function handleSetPinned(pinned) {
    if (!selectedConversation) return
    try {
      await UpdateConversationPinned(selectedConversation.id, pinned)
    } catch (e) {
      console.error('Failed to set pinned:', e)
    }
  }

  async function handleToggleTechDetails(id) {
    try {
      await UpdateConversationTechDetails(id, selectedConversation.tech_details)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.tech_details = selectedConversation.tech_details
        showTechDetails = selectedConversation.tech_details
      }
    } catch (e) {
      console.error('Failed to toggle tech details:', e)
    }
  }

  async function handleToggleContextDetails(id) {
    try {
      await UpdateConversationContextDetails(id, selectedConversation.context_details)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.context_details = selectedConversation.context_details
        showContextDetails = selectedConversation.context_details
      }
    } catch (e) {
      console.error('Failed to toggle context details:', e)
    }
  }

  async function handleSetSummarize(id, summarize) {
    try {
      await UpdateConversationSummarize(id, summarize)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.summarize = summarize
      }
      if (selectedConversation && selectedConversation.id === id) {
        selectedConversation.summarize = summarize
      }
    } catch (e) {
      console.error('Failed to set summarize:', e)
    }
  }

  async function handleToggleVizEnabled(id, vizEnabled) {
    try {
      await UpdateConversationVizEnabled(id, vizEnabled)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.viz_enabled = vizEnabled
      }
      if (selectedConversation && selectedConversation.id === id) {
        selectedConversation.viz_enabled = vizEnabled
      }
    } catch (e) {
      console.error('Failed to toggle viz enabled:', e)
    }
  }

  async function handleToggleStreamingEnabled(id, enabled) {
    try {
      await UpdateConversationStreamingEnabled(id, enabled)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.streaming_enabled = enabled
      }
      if (selectedConversation && selectedConversation.id === id) {
        selectedConversation.streaming_enabled = enabled
      }
    } catch (e) {
      console.error('Failed to toggle streaming:', e)
    }
  }

  async function handleToggleConversationSkill(conversationId, skillId, enabled) {
    try {
      await SetConversationSkill(conversationId, skillId, enabled)
      if (enabled) {
        conversationSkillIDs = [...conversationSkillIDs, skillId]
      } else {
        conversationSkillIDs = conversationSkillIDs.filter(id => id !== skillId)
      }
    } catch (e) {
      console.error('Failed to toggle conversation skill:', e)
    }
  }

  async function loadConversationSkills(conversationId) {
    try {
      conversationSkillIDs = await GetConversationSkillIDs(conversationId) || []
    } catch (e) {
      conversationSkillIDs = []
    }
  }

  async function handleDuplicateConversation() {
    if (!selectedConversation) return
    try {
      const duplicated = await DuplicateConversation(selectedConversation.id)
      await loadData()
      // Open the duplicated conversation
      activeConversation = duplicated
      activeView = 'conversation'
      conversationMessages = await GetConversationMessages(duplicated.id)
      showGearPopover = false
      selectedConversation = null
    } catch (e) {
      console.error('Failed to duplicate conversation:', e)
    }
  }

  async function handleClearMessages() {
    if (!selectedConversation) return
    if (!confirm('Clear all messages in this conversation? The conversation itself will remain.')) return
    try {
      await ClearConversationMessages(selectedConversation.id)
      if (activeConversation && activeConversation.id === selectedConversation.id) {
        conversationMessages = []
      }
      showGearPopover = false
      selectedConversation = null
      await loadData()
    } catch (e) {
      console.error('Failed to clear messages:', e)
    }
  }
</script>

<div class="app-layout">
  <aside class="sidebar {sidebarCollapsed ? 'collapsed' : ''}">
    <div class="sidebar-header">
      <button class="sidebar-toggle" onclick={() => sidebarCollapsed = !sidebarCollapsed} title="{sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}">
        {#if sidebarCollapsed}
          <ChevronRight size={16} />
        {:else}
          <ChevronLeft size={16} />
        {/if}
      </button>
      {#if !sidebarCollapsed}
        <h1>YourQL</h1>
      {/if}
    </div>

    <nav class="sidebar-nav">
      <button
        class="nav-item {activeView === 'discussions' ? 'active' : ''}"
        onclick={() => activeView = 'discussions'}
        title="Discussions"
      >
        <span class="nav-icon"><MessageSquare size={18} /></span>
        {#if !sidebarCollapsed}
          <span>Discussions</span>
        {/if}
      </button>

      <button
        class="nav-item {activeView === 'settings' ? 'active' : ''}"
        onclick={() => activeView = 'settings'}
        title="Settings"
      >
        <span class="nav-icon"><Settings size={18} /></span>
        {#if !sidebarCollapsed}
          <span>Settings</span>
        {/if}
      </button>

      <button
        class="nav-item {activeView === 'about' ? 'active' : ''}"
        onclick={() => activeView = 'about'}
        title="About"
      >
        <span class="nav-icon">i️</span>
        {#if !sidebarCollapsed}
          <span>About</span>
        {/if}
      </button>
    </nav>

    <div class="sidebar-footer">
      {#if sidebarCollapsed}
        <button
          class="btn-new-discussion btn-new-discussion-icon"
          onclick={openNewDiscussionModal}
          type="button"
          title="New Discussion"
        >
          <Plus size={16} />
        </button>
      {:else}
        <button class="btn-new-discussion" onclick={openNewDiscussionModal} type="button">
          <Plus size={14} /> New Discussion
        </button>
      {/if}
    </div>
  </aside>

  <main class="main-content">
    {#if error}
      <div class="error-banner">
        {error}
      </div>
    {/if}

    {#if activeView === 'discussions'}
      <div class="view-container">
        <div class="view-header">
          <h2>Discussions</h2>
          <div class="view-header-actions">
            <label class="archived-toggle">
              <input type="checkbox" bind:checked={showArchived} onchange={() => loadData()} />
              Show archived
            </label>
            <button class="btn btn-primary" onclick={openNewDiscussionModal}>+ New Discussion</button>
          </div>
        </div>
        <div class="view-content">
          {#if conversations.length === 0}
            <div class="empty-state">
              <p>No discussions found</p>
              <p class="hint">Create a new discussion to start querying your data</p>
            </div>
          {:else}
            <div class="conversations-list">
              {#each conversations as conv}
                <div class="conversation-row" class:archived={conv.status === 'archived'}>
                  <button class="conversation-item" onclick={() => openConversation(conv)} type="button">
                    <div class="conversation-title">{conv.title || 'Untitled'}</div>
                    <div class="conversation-meta">
                      <span class="conversation-date">{new Date(conv.updated_at).toLocaleDateString()}</span>
                      {#if conv.llm_provider_id}
                        <span class="conversation-model">{llmNameByID[conv.llm_provider_id] || 'LLM'}</span>
                      {/if}
                      {#if conv.data_source_id}
                        <span class="conversation-db">{dataSourceNameByID[conv.data_source_id] || 'DB'}</span>
                      {/if}
                    </div>
                  </button>
                  <button
                    class="gear-btn"
                    onclick={() => { selectedConversation = conv; showGearPopover = !showGearPopover }}
                    title="Conversation settings"
                    type="button"
                  >
                    <Settings size={14} />
                  </button>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {:else if activeView === 'conversation'}
      <ConversationView
        {activeConversation}
        {conversationMessages}
        {llmProviders}
        {dataSources}
        processingMessage={processingConversationId === activeConversation?.id ? processingMessage : ''}
        {messageError}
        userMessage={userMessage}
        showTechDetails={showTechDetails}
        showContextDetails={showContextDetails}
        maxMessages={activeConversation?.max_messages || 0}
        onMaxMessagesChange={handleSetMaxMessages}
        onSendMessage={handleSendMessage}
        onBack={backToConversations}
        onMessageChange={(val) => userMessage = val}
        onArchiveConversation={handleArchiveConversation}
        onUpdateConversationSettings={handleUpdateConversationSettings}
        onGearClick={async () => { selectedConversation = activeConversation; showGearPopover = true; allSkills = await ListSkills() || []; loadConversationSkills(activeConversation.id) }}
      />
    {:else if activeView === 'settings'}
      <SettingsView
        {llmProviders}
        {dataSources}
        onUpdate={loadData}
      />
    {:else if activeView === 'about'}
      <div class="about-view">
        <div class="about-content">
          <h2>YourQL</h2>
          <p class="version">Version {appVersion}</p>
          <p class="description">Talk to your database in plain English.</p>

          <!-- Auto-update controls -->
          <div class="update-section">
            {#if appVersion === 'dev' || appVersion === 'loading...'}
              {#if appVersion === 'dev'}
                <span class="update-dev-note">Development build — updates not available</span>
              {:else}
                <span class="update-dev-note">Checking version…</span>
              {/if}
            {:else if !updateInfo && !updateChecking && !updateError}
              <button class="update-btn" onclick={handleCheckForUpdate}>Check for Updates</button>
            {:else if updateChecking}
              <span class="update-status">Checking for updates…</span>
            {:else if updateError}
              <span class="update-error">{updateError}</span>
              <button class="update-btn update-btn-retry" onclick={handleCheckForUpdate}>Retry</button>
            {:else if updateInfo && !updateInfo.update_available}
              <span class="update-status update-uptodate">You're on the latest version</span>
              <span class="update-checked">(checked just now)</span>
            {:else if updateInfo && updateInfo.update_available && !updateReady}
              <div class="update-available">
                <p><strong>Update available:</strong> {updateInfo.latest_version}</p>
                {#if updateInfo.release_notes}
                  <details class="update-notes">
                    <summary>Release notes</summary>
                    <pre>{updateInfo.release_notes}</pre>
                  </details>
                {/if}
                {#if updateDownloading}
                  <span class="update-status">Downloading…</span>
                {:else}
                  <button class="update-btn" onclick={handleDownloadUpdate}>Download &amp; Install</button>
                {/if}
              </div>
            {:else if updateReady}
              <div class="update-ready">
                <span class="update-status update-uptodate">Update downloaded and verified</span>
                <button class="update-btn" onclick={handlePerformUpgradeRestart}>Restart Now</button>
              </div>
            {/if}
          </div>

          <div class="about-disclaimer">
            <h3>Disclaimer</h3>
            <p>The models you configure will have access to the databases you configure. Databases with sensitive data should be used responsibly. If you are in doubt about the sensitivity of the data you have access to in your database, then do not use this application.</p>
            <p>This application can be quarantined in an environment such that the models and databases are local and no data leaves the network environment it is in, but this requires technical knowledge and execution.</p>
          </div>

          <div class="about-section">
            <h3>What is YourQL?</h3>
            <p>YourQL is a desktop application that lets you query your databases using natural language. Instead of writing SQL by hand, ask questions the way you'd ask a colleague — and YourQL translates them into accurate, safe queries using your preferred AI model. Results come back as interactive tables, charts, and plain-English summaries. Everything runs locally on your machine.</p>
          </div>

          <div class="about-section">
            <h3>Key Features</h3>
            <ul>
              <li>Ask questions about your data in plain English and get answers backed by real SQL queries</li>
              <li>Connect to MySQL, PostgreSQL, SQLite, SQL Server, Snowflake, BigQuery, Redshift, MariaDB, CSV files, Excel files, and Google Sheets</li>
              <li>Use OpenAI, Anthropic Claude, Ollama, or any OpenAI-compatible endpoint — bring your own model</li>
              <li>Safe data exploration with configurable safety modes (strict, moderate, relaxed) — read-only by default</li>
              <li>Automatic chart generation — bar, line, pie, scatter, radar, and more</li>
              <li>Plain-English result summaries so you don't have to decipher tables of numbers</li>
              <li>Custom system prompts, business rules, and table/column descriptions per database connection</li>
              <li>Reusable Skills — Markdown prompt fragments you can activate per conversation for domain knowledge</li>
              <li>Advanced agent loop settings — customize the exact prompts, tool descriptions, and instructions your AI model receives</li>
              <li>Pin, archive, duplicate, rename, and clear discussions — full conversation management</li>
              <li>No telemetry, no analytics, no data collection — your credentials and history stay on your machine</li>
            </ul>
          </div>

          <div class="about-section">
            <h3>Technology</h3>
            <ul>
              <li><strong>Desktop framework:</strong> Wails v2</li>
              <li><strong>Backend:</strong> Go, with native database drivers for every supported type</li>
              <li><strong>Frontend:</strong> Svelte 5 + Vite + Chart.js</li>
              <li><strong>Local storage:</strong> SQLite — all conversations, settings, and configurations stored in <code>~/.yourql/yourql.db</code></li>
              <li><strong>LLM integration:</strong> Provider-agnostic interface supporting OpenAI, Anthropic, Ollama, and custom HTTP endpoints</li>
            </ul>
          </div>

          <div class="about-section">
            <h3>License</h3>
            <p>YourQL is open-source software.</p>
          </div>
        </div>
      </div>
    {/if}
  </main>

  {#if showGearPopover && selectedConversation}
    <div class="gear-popover-overlay" onclick={() => { showGearPopover = false; selectedConversation = null }}></div>
    <div class="gear-popover gear-popover-wide" onclick={(e) => e.stopPropagation()}>
      <div class="gear-popover-header">
        <span>Settings for "{selectedConversation.title}"</span>
        <button class="gear-popover-close" onclick={() => { showGearPopover = false; selectedConversation = null }}><X size={16} /></button>
      </div>

      <!-- LLM Provider -->
      <div class="gear-popover-section">
        <label>LLM Provider</label>
        <select
          value={selectedConversation.llm_provider_id || ''}
          onchange={(e) => {
            const val = e.target.value ? parseInt(e.target.value) : null
            handleUpdateConversationSettings(val, selectedConversation.data_source_id || null)
          }}
        >
          <option value="">(none)</option>
          {#each llmProviders as provider}
            <option value="{provider.id}">{provider.name}</option>
          {/each}
        </select>
      </div>

      <!-- DB Connection -->
      <div class="gear-popover-section">
        <label>DB Connection</label>
        <select
          value={selectedConversation.data_source_id || ''}
          onchange={(e) => {
            const val = e.target.value ? parseInt(e.target.value) : null
            handleUpdateConversationSettings(selectedConversation.llm_provider_id || null, val)
          }}
        >
          <option value="">(none)</option>
          {#each dataSources as conn}
            <option value="{conn.id}">{conn.name}</option>
          {/each}
        </select>
      </div>

      <!-- Rename -->
      <div class="gear-popover-section">
        <label>Rename</label>
        <input
          type="text"
          value={selectedConversation.title || ''}
          placeholder="Enter new name..."
          oninput={(e) => {
            selectedConversation.title = e.target.value
          }}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              handleRenameConversation()
              e.target.blur()
            }
          }}
          onblur={() => {
            handleRenameConversation()
          }}
        />
      </div>

      <!-- Message Limit -->
      <div class="gear-popover-section">
        <label>Visible Messages</label>
        <div class="message-limit-group">
          <button
            class="msg-limit-btn {selectedConversation.max_messages === 0 ? 'active' : ''}"
            onclick={() => handleSetMaxMessages(0)}
          >Show All</button>
          <input
            type="number"
            value={selectedConversation.max_messages || ''}
            placeholder="e.g. 50"
            min="1"
            max="500"
            oninput={(e) => {
              const val = parseInt(e.target.value)
              if (val >= 1 && val <= 500) {
                selectedConversation.max_messages = val
              }
            }}
            onblur={() => handleSetMaxMessages(selectedConversation.max_messages || 0)}
          />
        </div>
      </div>

      <!-- Context Messages -->
      <div class="gear-popover-section">
        <label>Messages in LLM Context</label>
        <div class="message-limit-group">
          <button
            class="msg-limit-btn {selectedConversation.max_context_messages === 15 ? 'active' : ''}"
            onclick={() => handleSetMaxContextMessages(15)}
          >Max 15</button>
          <input
            type="number"
            value={selectedConversation.max_context_messages || ''}
            placeholder="e.g. 5"
            min="1"
            max="15"
            oninput={(e) => {
              const val = parseInt(e.target.value)
              if (val >= 1 && val <= 15) {
                selectedConversation.max_context_messages = val
              }
            }}
            onblur={() => handleSetMaxContextMessages(selectedConversation.max_context_messages || 0)}
          />
        </div>
        <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">Default: 5 messages. Hard cap: 15. Large context windows can cause empty responses.</div>
      </div>

      <!-- Pin -->
      <div class="gear-popover-section">
        <label>
          <input type="checkbox" bind:checked={selectedConversation.pinned} onchange={() => handleSetPinned(selectedConversation.pinned)} />
          Pin to top of list
        </label>
      </div>

      <!-- Tech Details -->
      <div class="gear-popover-section">
        <label>
          <input type="checkbox" bind:checked={selectedConversation.tech_details} onchange={() => handleToggleTechDetails(selectedConversation.id)} />
          SHOW TECHNICAL DETAILS
        </label>
      </div>

      <!-- Context Details -->
      <div class="gear-popover-section">
        <label>
          <input type="checkbox" bind:checked={selectedConversation.context_details} onchange={() => handleToggleContextDetails(selectedConversation.id)} />
          Show context &amp; token details
        </label>
      </div>

      <!-- Summarize -->
      <div class="gear-popover-section">
        <label>
          <input type="checkbox" checked={selectedConversation.summarize} onchange={(e) => handleSetSummarize(selectedConversation.id, e.target.checked)} />
          Summarize results
        </label>
        <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
          LLM summarizes query results as a plain-English answer
        </div>
      </div>

      <!-- Data Visualization -->
      <div class="gear-popover-section">
        <label>
          <input type="checkbox" checked={selectedConversation.viz_enabled !== false} onchange={(e) => handleToggleVizEnabled(selectedConversation.id, e.target.checked)} />
          Data visualization
        </label>
        <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
          LLM generates charts (bar, line, pie, scatter) when appropriate
        </div>
      </div>

      <!-- Streaming -->
      <div class="gear-popover-section">
        <label>
          <input type="checkbox" checked={selectedConversation.streaming_enabled === true} onchange={(e) => handleToggleStreamingEnabled(selectedConversation.id, e.target.checked)} />
          Stream LLM output
        </label>
        <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
          Show model output character-by-character in real time
        </div>
      </div>

      <!-- Skills -->
      <div class="gear-popover-section">
        <div class="gear-popover-section-title">Skills</div>
        {#each allSkills as skill (skill.id)}
          {@const enabled = conversationSkillIDs.includes(skill.id)}
          <label class="skill-toggle">
            <input type="checkbox"
                   checked={enabled}
                   disabled={!skill.is_active}
                   onchange={(e) => handleToggleConversationSkill(selectedConversation.id, skill.id, e.target.checked)} />
            {skill.name}
          </label>
        {/each}
        {#if allSkills.length === 0}
          <div style="color: var(--text-tertiary); font-size: var(--font-xs);">No skills configured. Add skills in Settings.</div>
        {/if}
      </div>

      <div class="gear-popover-divider"></div>

      <!-- Action buttons -->
      <div class="gear-popover-actions">
        <button class="gear-action-btn duplicate" onclick={() => handleDuplicateConversation()} title="Duplicate">
          <Copy size={14} /> Duplicate
        </button>
        <button class="gear-action-btn clear" onclick={() => handleClearMessages()} title="Clear messages">
          <Trash2 size={14} /> Clear
        </button>
        {#if selectedConversation.status === 'archived'}
          <button class="gear-action-btn restore" onclick={() => {
            RestoreConversation(selectedConversation.id).then(() => {
              showGearPopover = false
              selectedConversation = null
              loadData()
            }).catch(e => console.error('Failed to restore:', e))
          }}>Restore</button>
        {/if}
        <button class="gear-action-btn archive" onclick={() => {
          handleArchiveConversation()
          showGearPopover = false
          selectedConversation = null
        }}>Archive</button>
        <button class="gear-action-btn delete" onclick={() => {
          requestDeleteConversation(selectedConversation.id, selectedConversation.title || 'Untitled')
          showGearPopover = false
        }}>Delete</button>
      </div>
    </div>
  {/if}

  {#if showNewDiscussion}
    <div class="modal-overlay" onclick={() => showNewDiscussion = false}>
      <div class="modal" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h3>New Discussion</h3>
          <button class="modal-close" onclick={() => showNewDiscussion = false}><X size={16} /></button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label>Title</label>
            <input
              type="text"
              bind:value={newDiscussionTitle}
              placeholder="Enter discussion title"
              onkeydown={(e) => e.key === 'Enter' && handleCreateDiscussion()}
            />
          </div>

          <div class="form-group">
            <label>LLM Provider</label>
            <select bind:value={selectedLLMProvider}>
              <option value={null}>Default</option>
              {#each llmProviders as provider}
                <option value={provider}>{provider.name}</option>
              {/each}
            </select>
          </div>

          <div class="form-group">
            <label>Data Source</label>
            <select bind:value={selectedDataSource}>
              <option value={null}>Default</option>
              {#each dataSources as conn}
                <option value={conn}>{conn.name} ({conn.type})</option>
              {/each}
            </select>
          </div>

          {#if createError}
            <div class="error-message">{createError}</div>
          {/if}
        </div>
        <div class="modal-footer">
          <button class="btn btn-secondary" onclick={() => showNewDiscussion = false}>Cancel</button>
          <button class="btn btn-primary" onclick={handleCreateDiscussion} disabled={creating}>
            {#if creating}Creating...{:else}Create Discussion{/if}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if deleteTargetId}
    <div class="modal-overlay" onclick={cancelDelete}>
      <div class="modal delete-confirm" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h3>Delete Discussion</h3>
        </div>
        <div class="modal-body">
          <p>Are you sure you want to delete "<strong>{deleteTargetTitle}</strong>"?</p>
          <p class="hint">This will soft-delete the discussion. It can be recovered later.</p>
        </div>
        <div class="modal-footer">
          <button class="btn btn-secondary" onclick={cancelDelete} disabled={deleting}>Cancel</button>
          <button class="btn btn-danger" onclick={confirmDeleteConversation} disabled={deleting}>
            {#if deleting}Deleting...{:else}Delete{/if}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background-color: var(--bg-primary);
    color: var(--text-primary);
    overflow: hidden;
  }

  .app-layout {
    display: flex;
    height: 100vh;
    width: 100vw;
  }

  .sidebar {
    width: var(--sidebar-width);
    min-width: var(--sidebar-width);
    background: var(--bg-secondary);
    display: flex;
    flex-direction: column;
    padding: var(--space-4xl) 0;
    border-right: 1px solid var(--border-primary);
    transition: width 0.2s ease, min-width 0.2s ease, padding 0.2s ease;
    overflow: hidden;
  }

  .sidebar.collapsed {
    width: var(--sidebar-collapsed);
    min-width: var(--sidebar-collapsed);
    padding: var(--space-4xl) 0;  }

  .sidebar-header {
    padding: 0 var(--space-4xl) var(--space-6xl);
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    align-items: center;
    gap: var(--space-lg);
  }

  .sidebar-toggle {
    width: 1.75rem;
    height: 1.75rem;
    border: none;
    background: transparent;
    border-radius: var(--radius-md);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--font-xl);
    color: var(--text-secondary);
    transition: all 0.2s ease;
    flex-shrink: 0;
  }
  
  .sidebar-toggle:hover {
    background: var(--color-accent-light);
    color: var(--color-accent);
  }

  .sidebar.collapsed .sidebar-header {
    padding: 0 var(--space-3xl) var(--space-4xl);
    justify-content: center;
  }

  .sidebar-header h1 {
    margin: 0;
    font-size: var(--font-4xl);
    font-weight: 700;
    color: var(--color-accent);
    letter-spacing: 1px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sidebar.collapsed .sidebar-header h1 {
    display: none;
  }

  .sidebar-nav {
    flex: 1;
    padding: var(--space-4xl) var(--space-lg);
  }

  .nav-item {
    display: flex;
    align-items: center;
    width: 100%;
    padding: var(--space-xl) var(--space-2xl);
    margin-bottom: var(--space-md);
    background: transparent;
    border: none;
    border-radius: var(--radius-md);
    color: var(--text-secondary);
    font-size: var(--font-lg);
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
    gap: var(--space-xl);
  }

  .nav-item:hover {
    background: var(--color-accent-light);
    color: var(--color-accent);
  }

  .nav-item.active {
    background: rgba(2, 136, 209, 0.15);
    color: var(--color-accent);
    font-weight: 600;
  }

  .nav-icon {
    font-size: var(--font-2xl);
    flex-shrink: 0;
  }

  .sidebar.collapsed .nav-item {
    justify-content: center;
    padding: var(--space-xl);
    margin-bottom: var(--space-xl);
  }

  .sidebar.collapsed .nav-item span:not(.nav-icon) {
    display: none;
  }

  .sidebar-footer {
    padding: var(--space-2xl) var(--space-4xl);
    border-top: 1px solid var(--border-primary);
  }

  .btn-new-discussion {
    width: 100%;
    padding: var(--space-lg) 0;    border: none;
    border-radius: var(--radius-md);
    background: var(--color-accent);
    color: #ffffff;
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: background 0.2s ease;
  }

  .btn-new-discussion:hover {
    background: var(--color-accent);
  }

  .btn-new-discussion-icon {
    width: 100%;
    padding: var(--space-xl) 0;    font-size: var(--font-3xl);
    font-weight: 400;
  }

  .sidebar.collapsed .sidebar-footer {
    padding: var(--space-2xl) var(--space-md);
  }

  .main-content {
    flex: 1;
    background: var(--bg-primary);
    overflow-y: auto;
    position: relative;
    transition: width 0.2s ease;
  }

  .error-banner {
    background: var(--color-accent-light);
    border: 1px solid var(--color-accent-border);
    color: var(--color-danger);
    padding: var(--space-xl) var(--space-4xl);
    font-size: var(--font-base);
    border-radius: var(--radius-md);
  }

  .view-container {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .view-header {
    padding: var(--space-6xl) var(--space-7xl) var(--space-4xl);
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .view-header h2 {
    margin: 0;
    font-size: var(--font-5xl);
    font-weight: 600;
    color: var(--text-primary);
  }

  .view-header-actions {
    display: flex;
    align-items: center;
    gap: var(--space-xl);
  }

  .archived-toggle {
    font-size: var(--font-base);
    color: var(--text-muted);
    display: flex;
    align-items: center;
    gap: var(--space-xs);
    cursor: pointer;
  }

  .archived-toggle input {
    cursor: pointer;
  }

  .view-content {
    flex: 1;
    padding: var(--space-6xl) var(--space-7xl);
    overflow-y: auto;
  }

  .empty-state {
    text-align: center;
    padding: 5rem var(--space-4xl);
    color: var(--text-tertiary);
  }

  .empty-state p {
    margin: 0 0 var(--space-lg);
    font-size: var(--font-2xl);
  }

  .empty-state .hint {
    font-size: var(--font-md);
    color: var(--text-tertiary);
  }

  .conversations-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-lg);
  }

  .conversation-row.archived {
    border-left: 3px solid var(--color-accent);
  }

  .conversation-row.archived .conversation-item {
    border-left: none;
    border-radius: 0 var(--radius-md) var(--radius-md) 0;
  }

  .conversation-row.archived .conversation-title {
    opacity: 0.65;
  }

  .conversation-row {
    display: flex;
    align-items: stretch;
    gap: var(--space-md);
  }

  .conversation-item {
    background: var(--bg-tertiary);
    padding: var(--space-2xl) var(--space-4xl);
    border-radius: var(--radius-md);
    border: 1px solid var(--border-primary);
    flex: 1;
    text-align: left;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .conversation-item:hover {
    background: rgba(2, 136, 209, 0.05);
    border-color: rgba(2, 136, 209, 0.3);
    transform: translateX(0.3125rem);
  }

  .conversation-title {
    font-size: var(--font-xl);
    font-weight: 500;
    color: var(--text-primary);
    margin-bottom: var(--space-md);
  }

  .conversation-meta {
    display: flex;
    gap: var(--space-lg);
    font-size: var(--font-sm);
    color: var(--text-tertiary);
  }

  .conversation-date {
    color: var(--text-tertiary);
  }

  .conversation-model, .conversation-db {
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-md);
    border-radius: var(--radius-md);
    font-size: var(--font-xs);
  }

  .modal-overlay {
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
  }

  .modal {
    background: var(--bg-primary);
    border-radius: var(--radius-md);
    width: var(--modal-width);
    max-width: 90vw;
    max-height: 90vh;
    overflow-y: auto;
    border: 2px solid var(--color-accent);
  }

  .modal-header {
    padding: var(--space-4xl) var(--space-6xl);
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-header h3 {
    margin: 0;
    font-size: var(--font-3xl);
    font-weight: 600;
    color: var(--text-primary);
  }

  .modal-close {
    background: none;
    border: none;
    font-size: var(--font-4xl);
    color: var(--text-tertiary);
    cursor: pointer;
    padding: 0;
    width: var(--space-6xl);
    height: var(--space-6xl);
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
  }

  .modal-close:hover {
    background: var(--bg-secondary);
    color: var(--text-primary);
  }

  .modal-body {
    padding: var(--space-6xl);
  }

  .modal-footer {
    padding: var(--space-4xl) var(--space-6xl);
    border-top: 1px solid var(--border-primary);
    display: flex;
    justify-content: flex-end;
    gap: var(--space-lg);
  }

  .form-group {
    margin-bottom: var(--space-4xl);
  }

  .form-group label {
    display: block;
    margin-bottom: var(--space-md);
    font-size: var(--font-md);
    font-weight: 500;
    color: var(--text-secondary);
  }

  .form-group input,
  .form-group select {
    width: 100%;
    padding: var(--space-lg) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: var(--text-primary);
    background: var(--bg-primary);
    transition: border-color 0.2s ease;
    box-sizing: border-box;
  }

  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: var(--color-accent);
    border: 2px solid var(--color-accent);
  }

  .form-group input::placeholder {
    color: var(--text-tertiary);
  }

  .btn {
    padding: var(--space-lg) var(--space-4xl);
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .btn-primary {
    background: var(--color-accent);
    color: #ffffff;
  }

  .btn-primary:hover:not(:disabled) {
    background: var(--color-accent);
  }

  .btn-secondary {
    background: var(--bg-secondary);
    color: var(--text-secondary);
  }

  .btn-secondary:hover {
    background: var(--border-primary);
  }

  .btn-danger {
    padding: var(--space-lg) var(--space-4xl);
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    background: var(--color-danger);
    color: white;
  }

  .btn-danger:hover:not(:disabled) {
    background: var(--color-danger-hover);
  }

  .btn-danger:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .delete-discussion-btn {
    flex-shrink: 0;
    width: 1.75rem;
    height: 1.75rem;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--font-xl);
    cursor: pointer;
    border-radius: var(--radius-md);
    transition: all 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .delete-discussion-btn:hover {
    background: var(--color-accent-light);
    color: var(--color-danger);
  }

  .delete-confirm {
    max-width: 25rem;
  }

  .delete-confirm .modal-body p {
    margin: 0 0 var(--space-md);
    color: var(--text-secondary);
  }

  .delete-confirm .hint {
    font-size: var(--font-base);
    color: var(--text-tertiary);
  }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid var(--color-danger);
    color: var(--color-danger);
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    margin-top: var(--space-3xl);
  }

  .gear-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    height: 2rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-xl);
    color: var(--text-secondary);
    transition: all 0.2s ease;
  }

  .gear-btn:hover {
    background: var(--border-primary);
    color: var(--text-primary);
  }

  .gear-popover-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 19999;
  }

  .gear-popover {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: var(--gear-popover-width);
    background: var(--bg-primary);
    border: 2px solid var(--color-accent);
    border-radius: var(--radius-md);
    z-index: 20000;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .gear-popover-wide {
    width: var(--gear-popover-wide);
  }

  .gear-popover-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--space-lg) 18px;
    border-bottom: 1px solid var(--border-secondary);
    font-weight: 600;
    font-size: var(--font-md);
    color: var(--text-primary);
  }

  .gear-popover-close {
    background: none;
    border: none;
    font-size: var(--font-xl);
    color: var(--text-tertiary);
    cursor: pointer;
    padding: 0 var(--space-xs);
  }

  .gear-popover-close:hover {
    color: var(--text-primary);
  }

  .gear-popover-section {
    padding: var(--space-md) 18px;
  }

  .gear-popover-section label {
    display: block;
    font-size: var(--font-xs);
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: var(--space-xs);
  }

  .gear-popover-section select {
    width: 100%;
    padding: var(--space-md) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: var(--text-primary);
    background: var(--bg-tertiary);
    cursor: pointer;
    box-sizing: border-box;
  }

  .gear-popover-section select:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .gear-popover-section input[type="text"] {
    width: 100%;
    padding: var(--space-md) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: var(--text-primary);
    background: var(--bg-tertiary);
    box-sizing: border-box;
    transition: border-color 0.2s ease;
  }

  .gear-popover-section input[type="text"]:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .message-limit-group {
    display: flex;
    gap: var(--space-md);
    align-items: center;
  }

  .msg-limit-btn {
    padding: var(--space-md) var(--space-2xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    font-size: var(--font-base);
    cursor: pointer;
    transition: all 0.2s ease;
    white-space: nowrap;
  }

  .msg-limit-btn:hover {
    background: var(--border-primary);
  }

  .msg-limit-btn.active {
    background: var(--color-accent);
    color: #ffffff;
    border-color: var(--color-accent);
  }

  .message-limit-group input[type="number"] {
    flex: 1;
    padding: var(--space-sm) var(--space-lg);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: var(--text-primary);
    background: var(--bg-tertiary);
    box-sizing: border-box;
  }

  .message-limit-group input[type="number"]:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .gear-popover-section label {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    font-size: var(--font-base);
    color: var(--text-secondary);
    cursor: pointer;
  }

  .gear-popover-section label input[type="checkbox"] {
    width: var(--space-3xl);
    height: var(--space-3xl);
    accent-color: var(--color-accent);
  }

  .gear-popover-divider {
    height: 1px;
    background: var(--border-secondary);
    margin: 0 18px;
  }

  /* Skill toggles in gear popover */
  .gear-popover-section-title {
    font-weight: 600;
    font-size: var(--font-sm);
    margin-bottom: var(--space-sm);
    color: var(--text-secondary);
  }
  .skill-toggle {
    display: flex !important;
    align-items: center;
    gap: var(--space-sm);
    padding: var(--space-xs) 0;
    font-size: var(--font-sm) !important;
    font-weight: 400 !important;
    text-transform: none !important;
    letter-spacing: normal !important;
    color: var(--text-secondary) !important;
    cursor: pointer;
    margin-bottom: 0 !important;
  }
  .skill-toggle input[type="checkbox"] {
    accent-color: var(--color-accent);
  }

  .gear-popover-actions {
    display: flex;
    gap: var(--space-md);
    padding: var(--space-lg) 18px;
    border-top: 1px solid var(--border-secondary);
    flex-wrap: wrap;
  }

  .gear-action-btn {
    flex: 1;
    min-width: 5rem;
    padding: var(--space-md) var(--space-xl);
    border-radius: var(--radius-md);
    font-size: var(--font-base);
    font-weight: 500;
    cursor: pointer;
    border: 1px solid var(--border-primary);
    transition: all 0.2s ease;
  }

  .gear-action-btn.archive {
    background: var(--bg-primary);
    color: var(--color-danger);
    border-color: var(--color-danger);
  }

  .gear-action-btn.archive:hover {
    background: var(--color-danger);
    color: #ffffff;
  }

  .gear-action-btn.restore {
    background: var(--bg-primary);
    color: var(--color-success);
    border-color: var(--color-success);
  }

  .gear-action-btn.restore:hover {
    background: var(--color-success);
    color: #ffffff;
  }

  .gear-action-btn.delete {
    background: var(--bg-primary);
    color: var(--color-danger);
    border-color: var(--color-danger);
    font-weight: 600;
  }

  .gear-action-btn.delete:hover {
    background: var(--color-danger-hover);
    color: #ffffff;
  }

  /* About View */
  .about-view {
    flex: 1;
    padding: 2rem 3rem;
    overflow-y: auto;
    background: var(--bg-primary);
  }

  .about-content {
    max-width: var(--content-max-width);
    margin: 0 auto;
  }

  .about-content h2 {
    font-size: 2.5rem;
    font-weight: 700;
    color: var(--text-primary);
    margin-bottom: 0.5rem;
  }

  .about-content .version {
    font-size: 1rem;
    color: var(--text-secondary);
    margin-bottom: 1.5rem;
  }

  .about-content .description {
    font-size: 1.125rem;
    color: var(--text-secondary);
    margin-bottom: 2.5rem;
    line-height: 1.6;
  }

  /* Update section */
  .update-section {
    margin-bottom: 2rem;
    padding: var(--space-4xl) var(--space-5xl);
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-md);
  }

  .update-btn {
    padding: 0.5rem 1rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: #ffffff;
    background: var(--color-primary);
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background 0.15s;
  }

  .update-btn:hover {
    background: var(--color-primary-hover);
  }

  .update-btn-retry {
    margin-left: 0.75rem;
    padding: 0.35rem 0.75rem;
    font-size: 0.8rem;
  }

  .update-status {
    font-size: 0.9rem;
    color: var(--text-secondary);
  }

  .update-uptodate {
    color: var(--color-success);
  }

  .update-dev-note {
    font-size: 0.875rem;
    color: var(--text-secondary);
    font-style: italic;
  }

  .update-error {
    font-size: 0.875rem;
    color: var(--color-danger);
  }

  .update-checked {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-left: 0.5rem;
    font-style: italic;
  }

  .update-available {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .update-available p {
    margin: 0;
    font-size: 0.95rem;
    color: var(--text-primary);
  }

  .update-available p strong {
    color: var(--color-warning, #f0a020);
  }

  .update-ready {
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .update-notes {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }

  .update-notes summary {
    cursor: pointer;
    color: var(--color-primary);
    font-weight: 500;
  }

  .update-notes pre {
    margin-top: 0.5rem;
    padding: 0.75rem;
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    font-size: 0.8rem;
    line-height: 1.5;
    white-space: pre-wrap;
    max-height: 12rem;
    overflow-y: auto;
    color: var(--text-primary);
  }

  .about-section {
    margin-bottom: 2.5rem;
  }

  .about-disclaimer {
    margin-bottom: 2.5rem;
    padding: var(--space-4xl) var(--space-5xl);
    background: rgba(239, 83, 80, 0.08);
    border: 1px solid rgba(239, 83, 80, 0.25);
    border-radius: var(--radius-md);
  }

  .about-disclaimer h3 {
    font-size: 1.1rem;
    font-weight: 600;
    color: var(--color-danger);
    margin-bottom: 0.75rem;
  }

  .about-disclaimer p {
    font-size: 0.95rem;
    color: var(--text-secondary);
    line-height: 1.6;
    margin-bottom: 0.5rem;
  }

  .about-section h3 {
    font-size: 1.25rem;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 1rem;
  }

  .about-section p {
    font-size: 1rem;
    color: var(--text-secondary);
    line-height: 1.6;
    margin-bottom: 0.75rem;
  }

  .about-section ul {
    margin-left: 1.5rem;
    margin-bottom: 1rem;
  }

  .about-section li {
    font-size: 1rem;
    color: var(--text-secondary);
    line-height: 1.6;
    margin-bottom: 0.5rem;
  }

  .about-section li strong {
    color: var(--text-primary);
    font-weight: 600;
  }

  @media print {
    .sidebar, .sidebar-footer, .sidebar-nav, .sidebar-header,
    .view-header, .view-header-actions,
    .modal-overlay, .gear-popover-overlay, .gear-popover,
    .error-banner { display: none !important; }
    .main-content { width: 100% !important; }
    .app-layout { display: block !important; }
  }
</style>
