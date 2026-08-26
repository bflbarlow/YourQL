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

  import {
    ListConversations, CreateConversation, GetConversationMessages,
    ProcessUserMessage, UpdateConversationPinned,
    UpdateConversationSettings, ArchiveConversation, DeleteConversation,
    RemoveTagFromConversation, GetTagsForConversation, ListSkills,
    ListLLMProviders, ListDataSources, GetDiscussionDefaults,
    GetAppVersion, CheckForUpdate, DownloadUpdate, PerformUpgradeRestart,
    GetAppSetting, SetAppSetting
  } from '../wailsjs/go/main/App.js'
  import { createDefaultConversation } from './lib/api.js'
  import { MessageSquare, Settings, X, Search, ChevronRight, ChevronLeft, Plus, Info, Pin, PinOff, Tag } from 'lucide-svelte'
  import SettingsView from './components/settings/SettingsView.svelte'
  import ConversationView from './ConversationView.svelte'
  import MinimalistView from './MinimalistView.svelte'
  import ConversationSettingsDialog from './components/conversation/ConversationSettingsDialog.svelte'
  import TagPopover from './components/conversation/TagPopover.svelte'
  import Modal from './components/ui/Modal.svelte'
  import ConfirmDialog from './components/ui/ConfirmDialog.svelte'
  import ConfirmHost from './components/ui/ConfirmHost.svelte'
  import IconBtn from './components/ui/IconBtn.svelte'
  import EmptyState from './components/ui/EmptyState.svelte'

  let activeView = $state('discussions')
  let conversations = $state([])
  let llmProviders = $state([])
  let dataSources = $state([])
  let sidebarCollapsed = $state(false)

  // --- Minimalist mode state ---
  let minimalist = $state(false)

  function setMinimalist(enabled) {
    minimalist = enabled
    document.documentElement.setAttribute('data-minimalist', enabled ? 'true' : 'false')
    SetAppSetting('minimalist_mode', enabled ? 'true' : 'false').catch(() => {})
  }

  async function loadMinimalistMode() {
    try {
      const val = await GetAppSetting('minimalist_mode')
      minimalist = val === 'true'
    } catch {
      minimalist = false
    }
    document.documentElement.setAttribute('data-minimalist', minimalist ? 'true' : 'false')
  }
  loadMinimalistMode()

  function handleKeydown(e) {
    if ((e.metaKey || e.ctrlKey) && e.shiftKey && (e.key === 'M' || e.key === 'm')) {
      e.preventDefault()
      setMinimalist(!minimalist)
    }
  }

  // --- Version & auto-update state ---
  let appVersion = $state('loading...')
  let updateInfo = $state(null)
  let updateChecking = $state(false)
  let updateDownloading = $state(false)
  let updateError = $state(null)
  let updateReady = $state(false)

  async function loadAppVersion() {
    try {
      appVersion = await GetAppVersion()
    } catch {
      appVersion = 'unknown'
    }
  }
  loadAppVersion()

  let updateCheckedTimer = null

  async function handleCheckForUpdate() {
    updateChecking = true
    updateError = null
    updateReady = false
    updateInfo = null
    if (updateCheckedTimer) clearTimeout(updateCheckedTimer)
    try {
      updateInfo = await CheckForUpdate()
      if (updateInfo && !updateInfo.update_available) {
        updateCheckedTimer = setTimeout(() => { updateInfo = null }, 5000)
      }
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

  let error = $state(null)

  let showNewDiscussion = $state(false)
  let newDiscussionTitle = $state('')
  let selectedLLMProvider = $state(null)
  let selectedDataSource = $state(null)

  async function openNewDiscussionModal() {
    try {
      const defaults = await GetDiscussionDefaults()
      if (defaults?.llm_provider_id) {
        selectedLLMProvider = llmProviders.find(p => p.id === defaults.llm_provider_id) || null
      }
      if (defaults?.data_source_id) {
        selectedDataSource = dataSources.find(ds => ds.id === defaults.data_source_id) || null
      }
    } catch {
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
  let errorConversationId = $state(null)
  let showTechDetails = $state(false)
  let showContextDetails = $state(false)

  let showSettingsDialog = $state(false)
  let settingsTarget = $state(null)   // conversation whose settings dialog is open
  let settingsSkills = $state([])     // skills list for the dialog

  // Reset the message input when switching discussions.
  $effect(() => {
    activeConversation?.id
    userMessage = ''
  })

  $effect(() => {
    activeView
    showSettingsDialog = false
    settingsTarget = null
  })

  let deleteTargetId = $state(null)
  let deleteTargetTitle = $state('')
  let deleting = $state(false)

  let showArchived = $state(false)

  // Search and filter
  let searchQuery = $state('')
  let filterDataSource = $state('')
  let filterLLM = $state('')

  // Inline row-level tag popover
  let rowTagPopoverForId = $state(null)

  function clearFilters() {
    searchQuery = ''
    filterDataSource = ''
    filterLLM = ''
  }

  let filteredConversations = $derived(
    conversations.filter(conv => {
      const query = searchQuery.toLowerCase().trim()
      if (query) {
        const title = (conv.title || '').toLowerCase()
        const tags = (conv.tags || []).join(' ').toLowerCase()
        const dbName = (dataSourceNameByID[conv.data_source_id] || '').toLowerCase()
        const llmName = (llmNameByID[conv.llm_provider_id] || '').toLowerCase()
        if (!title.includes(query) && !tags.includes(query) && !dbName.includes(query) && !llmName.includes(query)) {
          return false
        }
      }
      if (filterDataSource && conv.data_source_id !== +filterDataSource) {
        if (!conv.pinned) return false
      }
      if (filterLLM && conv.llm_provider_id !== +filterLLM) {
        if (!conv.pinned) return false
      }
      return true
    })
  )

  async function loadData() {
    error = null
    try {
      const convRes = await ListConversations()
      conversations = (convRes || []).filter(c => showArchived || c.status !== 'archived')

      const llmRes = await ListLLMProviders()
      llmProviders = llmRes || []

      const dbRes = await ListDataSources()
      dataSources = dbRes || []
    } catch (e) {
      error = e.toString()
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
      const conversation = await CreateConversation(
        newDiscussionTitle.trim(),
        selectedLLMProvider ? selectedLLMProvider.id : null,
        selectedDataSource ? selectedDataSource.id : null
      )

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
      errorConversationId = conversation.id
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
      cancelDelete()
    } catch (e) {
      error = 'Failed to delete: ' + e.toString()
    } finally {
      deleting = false
    }
  }

  async function handleSendMessage() {
    if (!userMessage.trim() || !activeConversation || processingConversationId) return

    processingConversationId = activeConversation.id
    processingMessage = 'Thinking...'
    messageError = null
    errorConversationId = null

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
      errorConversationId = activeConversation.id
      // Remove optimistic message on error, then re-fetch from DB
      conversationMessages = conversationMessages.filter(m => m.id !== tempId)
      try {
        conversationMessages = await GetConversationMessages(activeConversation.id)
      } catch {
        // DB fetch failed too — keep the filtered array
      }
    } finally {
      processingMessage = ''
      processingConversationId = null
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
      await loadData()
    } catch (e) {
      console.error('Failed to update conversation settings:', e)
    }
  }

  async function handleArchiveConversation() {
    if (!settingsTarget && !activeConversation) return
    try {
      const targetId = settingsTarget?.id ?? activeConversation.id
      await ArchiveConversation(targetId)
      backToConversations()
    } catch (e) {
      console.error('Failed to archive conversation:', e)
    }
  }

  // ── Row actions ──
  async function handleQuickTogglePin(conv, e) {
    e.stopPropagation()
    try {
      const newPinned = !conv.pinned
      await UpdateConversationPinned(conv.id, newPinned)
      conv.pinned = newPinned
      conversations = [...conversations]
    } catch (err) {
      console.error('Failed to toggle pinned:', err)
    }
  }

  function toggleRowTagPopover(conv, e) {
    e.stopPropagation()
    rowTagPopoverForId = rowTagPopoverForId === conv.id ? null : conv.id
  }

  async function handleRowRemoveTag(conv, name, e) {
    e?.stopPropagation()
    try {
      await RemoveTagFromConversation(conv.id, name)
      conv.tags = await GetTagsForConversation(conv.id) || []
      conversations = [...conversations]
    } catch (err) {
      console.error('Failed to remove tag:', err)
    }
  }

  function openConversationSettings(conv) {
    settingsTarget = conv
    showSettingsDialog = true
    loadSettingsSkills()
  }

  // Live-sync settings changed in the dialog onto everything that renders them:
  // the list-row object (mutated in place by the dialog), the open thread's
  // activeConversation copy, and the tech/context detail flags.
  function handleDialogSetting(key, value) {
    const targetId = settingsTarget?.id
    if (activeConversation && targetId === activeConversation.id) {
      if (key in activeConversation) {
        activeConversation[key] = value
      }
      if (key === 'tech_details') showTechDetails = value
      if (key === 'context_details') showContextDetails = value
    }
    // Keep list rows consistent even when target came from the thread header
    const row = conversations.find(c => c.id === targetId)
    if (row && row !== settingsTarget && key in row) row[key] = value
  }

  function openActiveConversationSettings() {
    settingsTarget = activeConversation
    showSettingsDialog = true
    loadSettingsSkills()
  }

  async function loadSettingsSkills() {
    try {
      settingsSkills = await ListSkills() || []
    } catch {
      settingsSkills = []
    }
  }

  function closeSettingsDialog(changed, duplicated) {
    showSettingsDialog = false
    settingsTarget = null
    loadData()
    if (duplicated) {
      openConversation(duplicated)
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if minimalist}
  <MinimalistView
    initialView={activeView === 'settings' ? 'settings' : activeView === 'about' ? 'about' : activeView === 'discussions' ? 'discussions' : 'discussion'}
    conversations={filteredConversations}
    activeConversation={activeConversation}
    conversationMessages={conversationMessages}
    {llmProviders}
    {dataSources}
    processingActive={!!processingConversationId}
    onExit={() => setMinimalist(false)}
    onConversationChange={async (conv) => { await openConversation(conv); loadData() }}
    onMessageSent={async (convId) => {
      if (convId === activeConversation?.id) {
        conversationMessages = await GetConversationMessages(convId)
        loadData()
      }
    }}
  />
  <ConfirmHost />
{:else}
<div class="app-layout">
  <aside class="sidebar" class:collapsed={sidebarCollapsed}>
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
        <span class="nav-icon"><Info size={18} /></span>
        {#if !sidebarCollapsed}
          <span>About</span>
        {/if}
      </button>
    </nav>

    <div class="sidebar-footer">
      {#if sidebarCollapsed}
        <button
          class="btn-minimal-mode btn-minimal-mode-icon"
          onclick={() => setMinimalist(true)}
          type="button"
          title="Minimal Mode"
        >
          ◐
        </button>
      {:else}
        <button class="btn-minimal-mode" onclick={() => setMinimalist(true)} type="button">
          ◐ Minimal Mode
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
            <button class="btn btn-primary" onclick={openNewDiscussionModal}><Plus size={14} style="vertical-align: -2px;" /> New Discussion</button>
          </div>
        </div>
        <div class="discussion-filters">
          <div class="search-row">
            <div class="search-input-wrapper">
              <Search size={15} class="search-icon" />
              <input
                type="text"
                class="search-input"
                placeholder="Search by title, tag, data source, or model…"
                bind:value={searchQuery}
              />
              {#if searchQuery}
                <button class="search-clear-btn" onclick={() => searchQuery = ''} title="Clear search" type="button">
                  <X size={13} />
                </button>
              {/if}
            </div>
            <select class="filter-select" bind:value={filterDataSource}>
              <option value="">All data sources</option>
              {#each dataSources as ds}
                <option value={ds.id}>{ds.name}</option>
              {/each}
            </select>
            <select class="filter-select" bind:value={filterLLM}>
              <option value="">All models</option>
              {#each llmProviders as p}
                <option value={p.id}>{p.name}</option>
              {/each}
            </select>
          </div>
          {#if searchQuery || filterDataSource || filterLLM}
            <div class="active-filters-row">
              <span class="active-filters-label">{filteredConversations.length} of {conversations.length} discussions</span>
              {#if filterDataSource}
                <span class="filter-pill">{dataSourceNameByID[+filterDataSource]} <button onclick={() => filterDataSource = ''} title="Remove filter"><X size={11} /></button></span>
              {/if}
              {#if filterLLM}
                <span class="filter-pill">{llmNameByID[+filterLLM]} <button onclick={() => filterLLM = ''} title="Remove filter"><X size={11} /></button></span>
              {/if}
              <button class="btn-clear-filters" onclick={clearFilters}>Clear all</button>
            </div>
          {/if}
        </div>
        <div class="view-content">
          {#if filteredConversations.length === 0}
            {#if searchQuery || filterDataSource || filterLLM}
              <EmptyState
                title="No discussions match your filters."
                hint="Try broadening your search or removing filters."
                actionLabel="Clear filters"
                onaction={clearFilters}
              />
            {:else}
              <EmptyState
                title="No discussions found"
                hint="Create a new discussion to start querying your data."
                actionLabel="+ New Discussion"
                onaction={openNewDiscussionModal}
              />
            {/if}
          {:else}
            <div class="conversations-list">
              {#each filteredConversations as conv (conv.id)}
                <div class="conversation-row" class:archived={conv.status === 'archived'} class:pinned={conv.pinned}>
                  <div
                    class="conversation-item"
                    role="button"
                    tabindex="0"
                    onclick={() => openConversation(conv)}
                    onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openConversation(conv) } }}
                  >
                    <div class="conversation-title">
                      {#if conv.pinned}<Pin size={13} class="pin-indicator" />{/if}
                      {conv.title || 'Untitled'}
                    </div>
                    <div class="conversation-meta">
                      <span class="conversation-date">{new Date(conv.updated_at).toLocaleDateString()}</span>
                      {#if conv.tags && conv.tags.length > 0}
                        {#each conv.tags as tag}
                          <span class="tag-chip tag-chip-row">
                            {tag}
                            <button class="tag-chip-remove" onclick={(e) => handleRowRemoveTag(conv, tag, e)} title="Remove tag">
                              <X size={9} />
                            </button>
                          </span>
                        {/each}
                      {/if}
                      {#if conv.llm_provider_id}
                        <span class="conversation-model">{llmNameByID[conv.llm_provider_id] || 'LLM'}</span>
                      {/if}
                      {#if conv.data_source_id}
                        <span class="conversation-db">{dataSourceNameByID[conv.data_source_id] || 'DB'}</span>
                      {/if}
                    </div>
                  </div>
                  <div class="conversation-row-actions">
                    <IconBtn
                      size="sm"
                      label={conv.pinned ? 'Unpin' : 'Pin to top'}
                      active={conv.pinned}
                      onclick={(e) => handleQuickTogglePin(conv, e)}
                    >
                      {#if conv.pinned}<PinOff size={14} />{:else}<Pin size={14} />{/if}
                    </IconBtn>
                    <div class="row-tag-popover-wrapper">
                      <IconBtn size="sm" label="Add or remove tags" onclick={(e) => toggleRowTagPopover(conv, e)}>
                        <Tag size={14} />
                      </IconBtn>
                      <TagPopover
                        conversation={conv}
                        open={rowTagPopoverForId === conv.id}
                        onclose={() => rowTagPopoverForId = null}
                      />
                    </div>
                    <IconBtn size="sm" label="Conversation settings" onclick={() => openConversationSettings(conv)}>
                      <Settings size={14} />
                    </IconBtn>
                  </div>
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
        messageError={errorConversationId === activeConversation?.id ? messageError : null}
        userMessage={userMessage}
        showTechDetails={showTechDetails}
        showContextDetails={showContextDetails}
        maxMessages={activeConversation?.max_messages || 0}
        onSendMessage={handleSendMessage}
        onBack={backToConversations}
        onMessageChange={(val) => userMessage = val}
        onGearClick={openActiveConversationSettings}
      />
    {:else if activeView === 'settings'}
      <SettingsView
        {llmProviders}
        {dataSources}
        onUpdate={loadData}
        {minimalist}
        onMinimalistChange={setMinimalist}
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
              <button class="btn btn-primary" onclick={handleCheckForUpdate}>Check for Updates</button>
            {:else if updateChecking}
              <span class="update-status">Checking for updates…</span>
            {:else if updateError}
              <span class="update-error">{updateError}</span>
              <button class="btn btn-primary" onclick={handleCheckForUpdate}>Retry</button>
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
                  <button class="btn btn-primary" onclick={handleDownloadUpdate}>Download &amp; Install</button>
                {/if}
              </div>
            {:else if updateReady}
              <div class="update-ready">
                <span class="update-status update-uptodate">Update downloaded and verified</span>
                <button class="btn btn-primary" onclick={handlePerformUpgradeRestart}>Restart Now</button>
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
              <li>Plain-English result summaries with live-streamed reasoning so you see the model thinking through your data</li>
              <li><strong>Tags &amp; search</strong> — label discussions with tags, search across titles, tags, data sources, and models all at once, filter by source or model with quick dropdowns</li>
              <li><strong>Inline controls</strong> — pin, add tags, or remove tags directly from the discussion list without opening settings</li>
              <li>Custom system prompts, business rules, and table/column descriptions per database connection</li>
              <li>Reusable Skills — Markdown prompt fragments you can activate per conversation for domain knowledge</li>
              <li>Configurable pipeline and summarization timeouts — tune for your model's speed</li>
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

  <!-- Conversation Settings dialog -->
  <ConversationSettingsDialog
    conversation={settingsTarget}
    allSkills={settingsSkills}
    {llmProviders}
    {dataSources}
    open={showSettingsDialog && !!settingsTarget}
    onchange={handleDialogSetting}
    onclose={closeSettingsDialog}
    ondeleted={async (id) => {
      if (activeConversation?.id === id) {
        activeConversation = null
        activeView = 'discussions'
        conversationMessages = []
      }
      await loadData()
    }}
  />

  <!-- New Discussion -->
  <Modal open={showNewDiscussion} title="New Discussion" onclose={() => showNewDiscussion = false}>
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

    {#snippet footer()}
      <button class="btn btn-secondary" onclick={() => showNewDiscussion = false}>Cancel</button>
      <button class="btn btn-primary" onclick={handleCreateDiscussion} disabled={creating}>
        {creating ? 'Creating...' : 'Create Discussion'}
      </button>
    {/snippet}
  </Modal>

  <!-- Delete confirmation -->
  <ConfirmDialog
    open={!!deleteTargetId}
    title="Delete Discussion"
    body={'Are you sure you want to delete "' + deleteTargetTitle + '"?<br/><span class="delete-hint">This will soft-delete the discussion. It can be recovered later.</span>'}
    confirmLabel="Delete"
    danger={true}
    busy={deleting}
    onconfirm={confirmDeleteConversation}
    onclose={cancelDelete}
  />
</div>
<ConfirmHost />
{/if}

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
    padding: var(--space-4xl) 0;
  }

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
    color: var(--text-secondary);
    transition: all 0.2s ease;
    flex-shrink: 0;
    padding: 0;
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
    background: var(--color-accent-light);
    color: var(--color-accent);
    font-weight: 600;
  }

  .nav-icon {
    display: flex;
    align-items: center;
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

  .btn-minimal-mode {
    width: 100%;
    padding: var(--space-lg) 0;
    border: none;
    border-radius: var(--radius-md);
    background: var(--color-accent-light);
    color: var(--color-accent);
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: background 0.2s ease;
  }
  .btn-minimal-mode:hover {
    background: var(--color-accent-hover);
    color: #ffffff;
  }
  .btn-minimal-mode-icon {
    padding: var(--space-xl) 0;
    font-size: var(--font-3xl);
    font-weight: 400;
  }

  .main-content {
    flex: 1;
    background: var(--bg-primary);
    overflow-y: auto;
    position: relative;
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

  .archived-toggle input { cursor: pointer; }

  .view-content {
    flex: 1;
    padding: var(--space-6xl) var(--space-7xl);
    overflow-y: auto;
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
    transition: background 0.15s ease, border-color 0.15s ease;
  }

  .conversation-item:hover {
    background: var(--color-accent-light);
    border-color: var(--color-accent-border);
  }

  .conversation-item:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }

  .conversation-title {
    font-size: var(--font-xl);
    font-weight: 500;
    color: var(--text-primary);
    margin-bottom: var(--space-md);
  }

  .conversation-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-lg);
    font-size: var(--font-sm);
    color: var(--text-tertiary);
  }

  .conversation-model, .conversation-db {
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-md);
    border-radius: var(--radius-md);
    font-size: var(--font-xs);
  }

  /* Filter bar */
  .discussion-filters {
    padding: var(--space-lg) var(--space-7xl) var(--space-md);
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    flex-direction: column;
    gap: var(--space-sm);
  }
  .search-row {
    display: flex;
    gap: var(--space-sm);
    align-items: center;
  }
  .search-input-wrapper {
    position: relative;
    flex: 1;
    display: flex;
    align-items: center;
  }
  .search-input-wrapper :global(.search-icon) {
    position: absolute;
    left: var(--space-md);
    color: var(--text-tertiary);
    pointer-events: none;
  }
  .search-input {
    width: 100%;
    box-sizing: border-box;
    padding: var(--space-sm) var(--space-2xl) var(--space-sm) calc(var(--space-md) * 2 + 15px);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-lg);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-sm);
    transition: border-color 0.15s ease, box-shadow 0.15s ease;
  }
  .search-input:focus {
    outline: none;
    border-color: var(--color-accent);
    box-shadow: 0 0 0 3px var(--color-accent-light);
  }
  .search-clear-btn {
    position: absolute;
    right: var(--space-sm);
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-tertiary);
    display: flex;
    align-items: center;
    padding: var(--space-2xs);
    border-radius: 50%;
  }
  .search-clear-btn:hover {
    color: var(--text-primary);
    background: var(--bg-surface);
  }
  .filter-select {
    padding: var(--space-sm) var(--space-md);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-lg);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-xs);
    max-width: 11rem;
    cursor: pointer;
  }
  .filter-select:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .active-filters-row {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    flex-wrap: wrap;
  }
  .active-filters-label {
    font-size: var(--font-xs);
    color: var(--text-tertiary);
  }
  .filter-pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2xs);
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-md);
    font-size: var(--font-xs);
    font-weight: 500;
  }
  .filter-pill button {
    background: none;
    border: none;
    color: var(--color-accent);
    cursor: pointer;
    display: flex;
    align-items: center;
    padding: 0;
    opacity: 0.7;
  }
  .filter-pill button:hover { opacity: 1; }
  .btn-clear-filters {
    font-size: var(--font-xs);
    padding: var(--space-2xs) var(--space-sm);
    background: none;
    border: none;
    color: var(--text-tertiary);
    text-decoration: underline;
    cursor: pointer;
    margin-left: auto;
  }
  .btn-clear-filters:hover { color: var(--text-primary); }

  /* Pin indicator */
  .conversation-title :global(.pin-indicator) {
    margin-right: var(--space-2xs);
    vertical-align: -2px;
    color: var(--color-accent);
  }
  .conversation-row.pinned .conversation-item {
    border-left: 3px solid var(--color-accent);
    background: var(--color-accent-light);
  }

  /* Row action buttons */
  .conversation-row-actions {
    display: flex;
    align-items: stretch;
    gap: var(--space-xs);
    flex-shrink: 0;
  }
  .row-tag-popover-wrapper { position: relative; }

  /* Tag chips */
  .tag-chip-row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2xs);
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-sm);
    font-size: var(--font-xs);
  }
  .tag-chip-remove {
    background: none;
    border: none;
    color: var(--color-accent);
    cursor: pointer;
    display: flex;
    align-items: center;
    padding: 0;
    opacity: 0;
    transition: opacity 0.15s ease;
  }
  .tag-chip-row:hover .tag-chip-remove { opacity: 0.7; }
  .tag-chip-remove:hover { opacity: 1 !important; }

  /* Buttons (shared with modal content) */
  .btn {
    padding: var(--space-lg) var(--space-4xl);
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
    display: inline-flex;
    align-items: center;
    gap: var(--space-xs);
    justify-content: center;
  }
  .btn:disabled { opacity: 0.6; cursor: not-allowed; }
  .btn-primary {
    background: var(--color-accent);
    color: #ffffff;
  }
  .btn-primary:hover:not(:disabled) { background: var(--color-accent-hover); }
  .btn-secondary {
    background: var(--bg-secondary);
    color: var(--text-secondary);
  }
  .btn-secondary:hover { background: var(--border-primary); }

  .form-group { margin-bottom: var(--space-4xl); }
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
    background: var(--bg-input);
    transition: border-color 0.2s ease;
    box-sizing: border-box;
  }
  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .form-group input::placeholder { color: var(--text-tertiary); }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid var(--color-danger);
    color: var(--color-danger);
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    margin-top: var(--space-3xl);
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
  .update-section {
    margin-bottom: 2rem;
    padding: var(--space-4xl) var(--space-5xl);
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
  }
  .update-status { font-size: 0.9rem; color: var(--text-secondary); }
  .update-uptodate { color: var(--color-success); }
  .update-dev-note { font-size: 0.875rem; color: var(--text-secondary); font-style: italic; }
  .update-error { font-size: 0.875rem; color: var(--color-danger); margin-right: 0.75rem; }
  .update-checked {
    font-size: 0.8rem; color: var(--text-secondary);
    margin-left: 0.5rem; font-style: italic;
  }
  .update-available { display: flex; flex-direction: column; gap: 0.75rem; }
  .update-available p { margin: 0; font-size: 0.95rem; color: var(--text-primary); }
  .update-available p strong { color: var(--color-warning); }
  .update-ready { display: flex; align-items: center; gap: 1rem; flex-wrap: wrap; }
  .update-notes { font-size: 0.85rem; color: var(--text-secondary); }
  .update-notes summary { cursor: pointer; color: var(--color-primary); font-weight: 500; }
  .update-notes pre {
    margin-top: 0.5rem; padding: 0.75rem;
    background: var(--code-bg); color: var(--code-fg);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-sm);
    font-size: 0.8rem; line-height: 1.5;
    white-space: pre-wrap;
    max-height: 12rem; overflow-y: auto;
  }
  .about-section { margin-bottom: 2.5rem; }
  .about-disclaimer {
    margin-bottom: 2.5rem;
    padding: var(--space-4xl) var(--space-5xl);
    background: rgba(239, 83, 80, 0.08);
    border: 1px solid rgba(239, 83, 80, 0.25);
    border-radius: var(--radius-md);
  }
  .about-disclaimer h3 {
    font-size: 1.1rem; font-weight: 600;
    color: var(--color-danger); margin-bottom: 0.75rem;
  }
  .about-disclaimer p {
    font-size: 0.95rem; color: var(--text-secondary);
    line-height: 1.6; margin-bottom: 0.5rem;
  }
  .about-section h3 {
    font-size: 1.25rem; font-weight: 600;
    color: var(--text-primary); margin-bottom: 1rem;
  }
  .about-section p {
    font-size: 1rem; color: var(--text-secondary);
    line-height: 1.6; margin-bottom: 0.75rem;
  }
  .about-section ul { margin-left: 1.5rem; margin-bottom: 1rem; }
  .about-section li {
    font-size: 1rem; color: var(--text-secondary);
    line-height: 1.6; margin-bottom: 0.5rem;
  }
  .about-section li strong { color: var(--text-primary); font-weight: 600; }

  @media print {
    .sidebar, .sidebar-footer, .sidebar-nav, .sidebar-header,
    .view-header, .view-header-actions,
    :global(.overlay), .error-banner { display: none !important; }
    .main-content { width: 100% !important; }
    .app-layout { display: block !important; }
  }
</style>
