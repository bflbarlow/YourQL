<script>
  import { ListConversations, CreateConversation, GetConversationMessages, ProcessUserMessage, DeleteConversation, UpdateConversationTechDetails, ArchiveConversation, RestoreConversation, UpdateConversationSettings, ListLLMProviders, ListDBConnections, GetGeneralSettings } from '../wailsjs/go/main/App.js'
  import SettingsView from './SettingsView.svelte'
  import ConversationView from './ConversationView.svelte'

  let activeView = $state('discussions')
  let conversations = $state([])
  let llmProviders = $state([])
  let dbConnections = $state([])
  
  // App version and description
  const appVersion = '0.1.0'
  const appDescription = 'YourQL: Natural Language to SQL Desktop App'
  
  // Lookup maps for names by ID
  let llmNameByID = $derived(
    Object.fromEntries(llmProviders.map(p => [p.id, p.name]))
  )
  let dbNameByID = $derived(
    Object.fromEntries(dbConnections.map(c => [c.id, c.name]))
  )
  let generalSettings = $state({})
  let status = $state("Ready")
  let error = $state(null)

  // New discussion form state
  let showNewDiscussion = $state(false)
  let newDiscussionTitle = $state('')
  let selectedLLMProvider = $state(null)
  let selectedDBConnection = $state(null)
  let creating = $state(false)
  let createError = $state(null)

  // Active conversation state
  let activeConversation = $state(null)
  let conversationMessages = $state([])
  let userMessage = $state('')
  let processingMessage = $state(false)
  let messageError = $state(null)
  let showTechDetails = $state(false)
  let selectedConversation = $state(null)
  let showGearPopover = $state(false)
  
  // Close gear popover whenever activeView changes (page navigation)
  $effect(() => {
    // Trigger on activeView change
    activeView
    showGearPopover = false
    selectedConversation = null
  })

  // Delete confirmation state
  let deleteTargetId = $state(null)
  let deleteTargetTitle = $state('')
  let deleting = $state(false)

  // Load initial data
  async function loadData() {
    status = "Loading..."
    error = null
    try {
      // Load conversations
      const convRes = await ListConversations(1, 1)
      conversations = convRes || []

      // Load LLM providers
      const llmRes = await ListLLMProviders()
      llmProviders = llmRes || []

      // Load DB connections
      const dbRes = await ListDBConnections()
      dbConnections = dbRes || []

      // Load general settings
      const settingsRes = await GetGeneralSettings()
      generalSettings = settingsRes || {}

      status = "Loaded successfully"
    } catch (e) {
      error = e.toString()
      status = "Error loading data"
    }
  }

  // Call loadData on component mount
  loadData()

  // Create a new discussion
  async function handleCreateDiscussion() {
    if (!newDiscussionTitle.trim()) {
      createError = 'Please enter a title'
      return
    }

    creating = true
    createError = null

    try {
      const llmProviderID = selectedLLMProvider ? selectedLLMProvider.id : null
      const dbConnectionID = selectedDBConnection ? selectedDBConnection.id : null

      const conversation = await CreateConversation(1, 1, newDiscussionTitle.trim(), llmProviderID, dbConnectionID)

      // Reset form
      showNewDiscussion = false
      newDiscussionTitle = ''
      selectedLLMProvider = null
      selectedDBConnection = null

      // Reload conversations
      await loadData()

      // Open the new conversation
      activeConversation = conversation
      activeView = 'conversation'

      // Load messages
      conversationMessages = await GetConversationMessages(conversation.id)
    } catch (e) {
      createError = e.toString()
    } finally {
      creating = false
    }
  }

  // Open a conversation
  async function openConversation(conversation) {
    activeConversation = conversation
    activeView = 'conversation'
    showTechDetails = conversation.tech_details ?? false

    try {
      conversationMessages = await GetConversationMessages(conversation.id)
    } catch (e) {
      messageError = e.toString()
    }
  }

  // Delete conversation confirmation
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
    if (!userMessage.trim() || !activeConversation) return

    processingMessage = true
    messageError = null

    try {
      // Process through discussion engine (backend now persists user message)
      await ProcessUserMessage(activeConversation.id, userMessage.trim())
      userMessage = ''

      // Reload messages from DB
      conversationMessages = await GetConversationMessages(activeConversation.id)
    } catch (e) {
      messageError = e.toString()
    } finally {
      processingMessage = false
    }
  }

  // Back to conversations list
  function backToConversations() {
    activeConversation = null
    conversationMessages = []
    activeView = 'discussions'
  }
  
  // Toggle tech details
  async function handleTechDetailsToggle() {
    showTechDetails = !showTechDetails
    if (activeConversation) {
      try {
        // Persist toggle state to backend
        await UpdateConversationTechDetails(activeConversation.id, showTechDetails)
      } catch (e) {
        console.error('Failed to save tech details toggle:', e)
      }
    }
  }
  
  async function handleUpdateConversationSettings(llmProviderID, dbConnectionID) {
    if (!activeConversation) return
    try {
      await UpdateConversationSettings(activeConversation.id, llmProviderID, dbConnectionID)
      // Update local state
      if (llmProviderID !== null) activeConversation.llm_provider_id = llmProviderID
      if (dbConnectionID !== null) activeConversation.db_connection_id = dbConnectionID
      activeConversation.updated_at = new Date().toISOString()
      // Reload conversations to update sidebar order
      const convRes = await ListConversations(1, 1)
      conversations = convRes || []
    } catch (e) {
      console.error('Failed to update conversation settings:', e)
    }
  }
  
  async function handleArchiveConversation() {
    if (!activeConversation) return
    try {
      await ArchiveConversation(activeConversation.id)
      backToConversations()
      // Reload conversations to update sidebar
      const convRes = await ListConversations(1, 1)
      conversations = convRes || []
    } catch (e) {
      console.error('Failed to archive conversation:', e)
    }
  }
</script>

<div class="app-layout">
  <!-- Sidebar -->
  <aside class="sidebar">
    <div class="sidebar-header">
      <h1>YourQL</h1>
    </div>

    <nav class="sidebar-nav">
      <button
        class="nav-item {activeView === 'discussions' ? 'active' : ''}"
        onclick={() => activeView = 'discussions'}
      >
        <span class="nav-icon">💬</span>
        <span>Discussions</span>
      </button>

      <button
        class="nav-item {activeView === 'settings' ? 'active' : ''}"
        onclick={() => activeView = 'settings'}
      >
        <span class="nav-icon">⚙️</span>
        <span>Settings</span>
      </button>
      
      <button
        class="nav-item {activeView === 'about' ? 'active' : ''}"
        onclick={() => activeView = 'about'}
      >
        <span class="nav-icon">ℹ️</span>
        <span>About</span>
      </button>
    </nav>

    <div class="sidebar-footer">
      <div class="status-indicator">
        <span class="status-dot {error ? 'error' : 'success'}"></span>
        <span class="status-text">{status}</span>
      </div>
    </div>
  </aside>

  <!-- Main Content Area -->
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
          <button class="btn btn-primary" onclick={() => showNewDiscussion = true}>+ New Discussion</button>
        </div>
        <div class="view-content">
          {#if conversations.length === 0}
            <div class="empty-state">
              <p>No discussions found</p>
              <p class="hint">Create a new discussion to start querying your database</p>
            </div>
          {:else}
            <div class="conversations-list">
              {#each conversations as conv}
                <div class="conversation-row">
                  <button class="conversation-item" onclick={() => openConversation(conv)} type="button">
                    <div class="conversation-title">{conv.title || 'Untitled'}</div>
                    <div class="conversation-meta">
                      <span class="conversation-date">{new Date(conv.updated_at).toLocaleDateString()}</span>
                      {#if conv.llm_provider_id}
                        <span class="conversation-model">{llmNameByID[conv.llm_provider_id] || 'LLM'}</span>
                      {/if}
                      {#if conv.db_connection_id}
                        <span class="conversation-db">{dbNameByID[conv.db_connection_id] || 'DB'}</span>
                      {/if}
                    </div>
                  </button>
                  <button
                    class="delete-discussion-btn"
                    onclick={(e) => { e.stopPropagation(); requestDeleteConversation(conv.id, conv.title) }}
                    title="Delete discussion"
                    type="button"
                  >
                    ✕
                  </button>
                  <button
                    class="gear-btn"
                    onclick={() => { selectedConversation = conv; showGearPopover = !showGearPopover }}
                    title="Conversation settings"
                    type="button"
                  >
                    ⚙️
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
        {dbConnections}
        {processingMessage}
        {messageError}
        userMessage={userMessage}
        showTechDetails={showTechDetails}
        onSendMessage={handleSendMessage}
        onBack={backToConversations}
        onMessageChange={(val) => userMessage = val}
        onTechDetailsToggle={handleTechDetailsToggle}
        onArchiveConversation={handleArchiveConversation}
        onUpdateConversationSettings={handleUpdateConversationSettings}
        onGearClick={() => { selectedConversation = activeConversation; showGearPopover = true }}
      />
    {:else if activeView === 'settings'}
      <SettingsView
        {llmProviders}
        {dbConnections}
        {generalSettings}
        onUpdate={loadData}
      />
    {:else if activeView === 'about'}
      <div class="about-view">
        <div class="about-content">
          <h2>YourQL</h2>
          <p class="version">Version {appVersion}</p>
          <p class="description">{appDescription}</p>
          
          <div class="about-section">
            <h3>What is YourQL?</h3>
            <p>YourQL is a desktop application that lets you query databases using natural language. It uses Large Language Models (LLMs) to translate your questions into SQL queries and executes them against your configured databases.</p>
          </div>
          
          <div class="about-section">
            <h3>Key Features</h3>
            <ul>
              <li>Natural language to SQL conversion</li>
              <li>Support for multiple LLM providers (OpenAI, Anthropic, Ollama, Local)</li>
              <li>Database connections (MySQL, SQLite)</li>
              <li>Conversation history and management</li>
              <li>Exploration queries for data discovery</li>
              <li>Technical details toggle for debugging</li>
            </ul>
          </div>
          
          <div class="about-section">
            <h3>Technology Stack</h3>
            <ul>
              <li><strong>Backend:</strong> Go with Wails v2 framework</li>
              <li><strong>Frontend:</strong> Svelte 5 with Vite</li>
              <li><strong>Database:</strong> SQLite (local app data) + MySQL/SQLite (external connections)</li>
              <li><strong>LLM Integration:</strong> OpenAI API, Anthropic Claude, Ollama, Local models</li>
            </ul>
          </div>
          
          <div class="about-section">
            <h3>License</h3>
            <p>YourQL is an open-source project. Source code available on GitHub.</p>
          </div>
        </div>
      </div>
    {/if}
  </main>
  
  {#if showGearPopover && selectedConversation}
    <div class="gear-popover" onclick={(e) => e.stopPropagation()}>
      <div class="gear-popover-header">
        <span>Settings for "{selectedConversation.title}"</span>
        <button class="gear-popover-close" onclick={() => { showGearPopover = false; selectedConversation = null }}>✕</button>
      </div>
      <div class="gear-popover-section">
        <label>LLM Provider</label>
        <select 
          value={selectedConversation.llm_provider_id || ''}
          onchange={(e) => {
            const val = e.target.value ? parseInt(e.target.value) : null
            handleUpdateConversationSettings(val, selectedConversation.db_connection_id || null)
          }}
        >
          <option value="">(none)</option>
          {#each llmProviders as provider}
            <option value="{provider.id}">{provider.name}</option>
          {/each}
        </select>
      </div>
      <div class="gear-popover-section">
        <label>DB Connection</label>
        <select 
          value={selectedConversation.db_connection_id || ''}
          onchange={(e) => {
            const val = e.target.value ? parseInt(e.target.value) : null
            handleUpdateConversationSettings(selectedConversation.llm_provider_id || null, val)
          }}
        >
          <option value="">(none)</option>
          {#each dbConnections as conn}
            <option value="{conn.id}">{conn.name}</option>
          {/each}
        </select>
      </div>
      <div class="gear-popover-divider"></div>
      <div class="gear-popover-actions">
        {#if selectedConversation.status === 'archived'}
          <button class="gear-action-btn restore" onclick={() => {
            RestoreConversation(selectedConversation.id).then(() => {
              showGearPopover = false
              selectedConversation = null
              const convRes = ListConversations(1, 1)
              convRes.then(res => { conversations = res || [] })
            }).catch(e => console.error('Failed to restore:', e))
          }}>Restore</button>
        {/if}
        <button class="gear-action-btn archive" onclick={() => {
          handleArchiveConversation()
          showGearPopover = false
          selectedConversation = null
        }}>Archive</button>
      </div>
    </div>
  {/if}

  <!-- New Discussion Modal -->
  {#if showNewDiscussion}
    <div class="modal-overlay" onclick={() => showNewDiscussion = false}>
      <div class="modal" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h3>New Discussion</h3>
          <button class="modal-close" onclick={() => showNewDiscussion = false}>×</button>
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
            <label>LLM Provider (optional)</label>
            <select bind:value={selectedLLMProvider}>
              <option value={null}>Default</option>
              {#each llmProviders as provider}
                <option value={provider}>{provider.name}</option>
              {/each}
            </select>
          </div>

          <div class="form-group">
            <label>Database Connection (optional)</label>
            <select bind:value={selectedDBConnection}>
              <option value={null}>Default</option>
              {#each dbConnections as conn}
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

  <!-- Delete Confirmation Modal -->
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
    background-color: #ffffff;
    color: #000000;
    overflow: hidden;
  }

  .app-layout {
    display: flex;
    height: 100vh;
    width: 100vw;
  }

  /* Sidebar Styles */
  .sidebar {
    width: 250px;
    background: #f5f5f5;
    display: flex;
    flex-direction: column;
    padding: 20px 0;
    box-shadow: 2px 0 10px rgba(0, 0, 0, 0.05);
    border-right: 1px solid #e0e0e0;
  }

  .sidebar-header {
    padding: 0 20px 30px;
    border-bottom: 1px solid #e0e0e0;
  }

  .sidebar-header h1 {
    margin: 0;
    font-size: 24px;
    font-weight: 700;
    color: #4fc3f7;
    letter-spacing: 1px;
  }

  .sidebar-nav {
    flex: 1;
    padding: 20px 10px;
  }

  .nav-item {
    display: flex;
    align-items: center;
    width: 100%;
    padding: 12px 15px;
    margin-bottom: 8px;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: #666666;
    font-size: 15px;
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
  }

  .nav-item:hover {
    background: rgba(79, 195, 247, 0.1);
    color: #0288d1;
  }

  .nav-item.active {
    background: rgba(79, 195, 247, 0.15);
    color: #0288d1;
    font-weight: 600;
  }

  .nav-icon {
    margin-right: 12px;
    font-size: 18px;
  }

  .sidebar-footer {
    padding: 15px 20px;
    border-top: 1px solid #e0e0e0;
  }

  .status-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #4fc3f7;
  }

  .status-dot.error {
    background: #ef5350;
  }

  .status-dot.success {
    background: #4fc3f7;
  }

  .status-text {
    font-size: 12px;
    color: #999999;
  }

  /* Main Content Styles */
  .main-content {
    flex: 1;
    background: #ffffff;
    overflow-y: auto;
    position: relative;
  }

  .error-banner {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid #ef5350;
    color: #ffcdd2;
    padding: 12px 20px;
    font-size: 13px;
    border-radius: 0 0 8px 8px;
  }

  .view-container {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .view-header {
    padding: 30px 40px 20px;
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .view-header h2 {
    margin: 0;
    font-size: 28px;
    font-weight: 600;
    color: #000000;
  }

  .view-content {
    flex: 1;
    padding: 30px 40px;
    overflow-y: auto;
  }

  .empty-state {
    text-align: center;
    padding: 80px 20px;
    color: #cccccc;
  }

  .empty-state p {
    margin: 0 0 10px;
    font-size: 18px;
  }

  .empty-state .hint {
    font-size: 14px;
    color: #bbbbbb;
  }

  .conversations-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .conversation-row {
    display: flex;
    align-items: stretch;
    gap: 8px;
  }

  .conversation-item {
    background: #f9f9f9;
    padding: 15px 20px;
    border-radius: 8px;
    border: 1px solid #e0e0e0;
    flex: 1;
    text-align: left;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .conversation-item:hover {
    background: rgba(79, 195, 247, 0.05);
    border-color: rgba(79, 195, 247, 0.3);
    transform: translateX(5px);
  }

  .conversation-item:active {
    transform: translateX(3px);
  }

  .conversation-title {
    font-size: 16px;
    font-weight: 500;
    color: #000000;
    margin-bottom: 8px;
  }

  .conversation-meta {
    display: flex;
    gap: 10px;
    font-size: 12px;
    color: #999999;
  }

  .conversation-date {
    color: #999999;
  }

  .conversation-model, .conversation-db {
    background: rgba(79, 195, 247, 0.1);
    color: #0288d1;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
  }

  /* Modal Styles */
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
    background: #ffffff;
    border-radius: 12px;
    width: 500px;
    max-width: 90vw;
    max-height: 90vh;
    overflow-y: auto;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
  }

  .modal-header {
    padding: 20px 30px;
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-header h3 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    color: #000000;
  }

  .modal-close {
    background: none;
    border: none;
    font-size: 24px;
    color: #999999;
    cursor: pointer;
    padding: 0;
    width: 30px;
    height: 30px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
  }

  .modal-close:hover {
    background: #f5f5f5;
    color: #000000;
  }

  .modal-body {
    padding: 30px;
  }

  .modal-footer {
    padding: 20px 30px;
    border-top: 1px solid #e0e0e0;
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  /* Form Styles */
  .form-group {
    margin-bottom: 20px;
  }

  .form-group label {
    display: block;
    margin-bottom: 8px;
    font-size: 14px;
    font-weight: 500;
    color: #333333;
  }

  .form-group input,
  .form-group select {
    width: 100%;
    padding: 10px 12px;
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    font-size: 14px;
    color: #000000;
    background: #ffffff;
    transition: border-color 0.2s ease;
  }

  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: #4fc3f7;
    box-shadow: 0 0 0 3px rgba(79, 195, 247, 0.1);
  }

  .form-group input::placeholder {
    color: #cccccc;
  }

  /* Button Styles */
  .btn {
    padding: 10px 20px;
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .btn-primary {
    background: #4fc3f7;
    color: #ffffff;
  }

  .btn-primary:hover:not(:disabled) {
    background: #0288d1;
  }

  .btn-secondary {
    background: #f5f5f5;
    color: #666666;
  }

  .btn-secondary:hover {
    background: #e0e0e0;
  }

  .btn-danger {
    padding: 10px 20px;
    border: none;
    border-radius: 6px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    background: #ef5350;
    color: white;
  }

  .btn-danger:hover:not(:disabled) {
    background: #d32f2f;
  }

  .btn-danger:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .delete-discussion-btn {
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    border: none;
    background: transparent;
    color: #ccc;
    font-size: 16px;
    cursor: pointer;
    border-radius: 4px;
    transition: all 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .delete-discussion-btn:hover {
    background: #ffebee;
    color: #ef5350;
  }

  .delete-confirm {
    max-width: 400px;
  }

  .delete-confirm .modal-body p {
    margin: 0 0 8px;
    color: #333;
  }

  .delete-confirm .hint {
    font-size: 13px;
    color: #999;
  }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid #ef5350;
    color: #ef5350;
    padding: 12px 16px;
    border-radius: 8px;
    font-size: 14px;
    margin-top: 16px;
  }
  
  /* Gear Button */
  .gear-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    cursor: pointer;
    font-size: 16px;
    transition: all 0.2s ease;
  }
  
  .gear-btn:hover {
    background: #e0e0e0;
    color: #000000;
  }
  
  /* Gear Popover */
  .gear-popover {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: 320px;
    background: #ffffff;
    border: 2px solid #4fc3f7;
    border-radius: 12px;
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.2);
    z-index: 20000;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  
  .gear-popover-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 18px;
    border-bottom: 1px solid #f0f0f0;
    font-weight: 600;
    font-size: 14px;
    color: #1a1a1a;
  }
  
  .gear-popover-close {
    background: none;
    border: none;
    font-size: 16px;
    color: #999999;
    cursor: pointer;
    padding: 0 4px;
  }
  
  .gear-popover-close:hover {
    color: #000000;
  }
  
  .gear-popover-section {
    padding: 14px 18px;
  }
  
  .gear-popover-section label {
    display: block;
    font-size: 11px;
    font-weight: 600;
    color: #666666;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 8px;
  }
  
  .gear-popover-section select {
    width: 100%;
    padding: 10px 12px;
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    font-size: 14px;
    color: #000000;
    background: #f9f9f9;
    cursor: pointer;
  }
  
  .gear-popover-section select:focus {
    outline: none;
    border-color: #4fc3f7;
  }
  
  .gear-popover-divider {
    height: 1px;
    background: #f0f0f0;
    margin: 0 18px;
  }
  
  .gear-popover-actions {
    display: flex;
    gap: 10px;
    padding: 14px 18px;
    border-top: 1px solid #f0f0f0;
  }
  
  .gear-action-btn {
    flex: 1;
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    border: 1px solid #e0e0e0;
    transition: all 0.2s ease;
  }
  
  .gear-action-btn.archive {
    background: #ffffff;
    color: #ef5350;
    border-color: #ef5350;
  }
  
  .gear-action-btn.archive:hover {
    background: #ef5350;
    color: #ffffff;
  }
  
  .gear-action-btn.restore {
    background: #ffffff;
    color: #4caf50;
    border-color: #4caf50;
  }
  
  .gear-action-btn.restore:hover {
    background: #4caf50;
    color: #ffffff;
  }
  
  /* About View */
  .about-view {
    flex: 1;
    padding: 2rem 3rem;
    overflow-y: auto;
    background: #ffffff;
  }
  
  .about-content {
    max-width: 800px;
    margin: 0 auto;
  }
  
  .about-content h2 {
    font-size: 2.5rem;
    font-weight: 700;
    color: #1a1a1a;
    margin-bottom: 0.5rem;
  }
  
  .about-content .version {
    font-size: 1rem;
    color: #666666;
    margin-bottom: 1.5rem;
  }
  
  .about-content .description {
    font-size: 1.125rem;
    color: #333333;
    margin-bottom: 2.5rem;
    line-height: 1.6;
  }
  
  .about-section {
    margin-bottom: 2.5rem;
  }
  
  .about-section h3 {
    font-size: 1.25rem;
    font-weight: 600;
    color: #1a1a1a;
    margin-bottom: 1rem;
  }
  
  .about-section p {
    font-size: 1rem;
    color: #333333;
    line-height: 1.6;
    margin-bottom: 0.75rem;
  }
  
  .about-section ul {
    margin-left: 1.5rem;
    margin-bottom: 1rem;
  }
  
  .about-section li {
    font-size: 1rem;
    color: #333333;
    line-height: 1.6;
    margin-bottom: 0.5rem;
  }
  
  .about-section li strong {
    color: #000000;
    font-weight: 600;
  }
</style>
