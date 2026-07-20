<script>
  import { EventsOn } from '../wailsjs/runtime/runtime.js'

  // ==================== Processing Phase Listener ====================
  EventsOn('processingPhase', (phase) => {
    processingMessage = phase
  })
  EventsOn('processingComplete', () => {
    processingMessage = ''
  })

  import { ListConversations, CreateConversation, GetConversationMessages, ProcessUserMessage, DeleteConversation, UpdateConversationTechDetails, ArchiveConversation, RestoreConversation, UpdateConversationSettings, ListLLMProviders, ListDataSources, UpdateConversationTitle, UpdateConversationMaxMessages, UpdateConversationMaxContextMessages, UpdateConversationPinned, DuplicateConversation, ClearConversationMessages, UpdateConversationContextDetails, UpdateConversationSummarize } from '../wailsjs/go/main/App.js'
  import { MessageSquare, Settings, X, Copy, Trash2, Pin, ChevronRight, ChevronLeft, Plus } from 'lucide-svelte'
  import SettingsView from './SettingsView.svelte'
  import ConversationView from './ConversationView.svelte'

  let activeView = $state('discussions')
  let conversations = $state([])
  let llmProviders = $state([])
  let dataSources = $state([])
  let sidebarCollapsed = $state(false)

  const appVersion = '0.1.0'
  const appDescription = 'YourQL: Natural Language to SQL Desktop App'

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
  let creating = $state(false)
  let createError = $state(null)

  let activeConversation = $state(null)
  let conversationMessages = $state([])
  let userMessage = $state('')
  let processingMessage = $state('')
  let messageError = $state(null)
  let showTechDetails = $state(false)
  let showContextDetails = $state(false)
  let selectedConversation = $state(null)
  let showGearPopover = $state(false)

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
    if (!userMessage.trim() || !activeConversation) return

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
    }
  }

  function backToConversations() {
    activeConversation = null
    conversationMessages = []
    activeView = 'discussions'
  }

  async function handleTechDetailsToggle() {
    showTechDetails = !showTechDetails
    if (activeConversation) {
      try {
        await UpdateConversationTechDetails(activeConversation.id, showTechDetails)
      } catch (e) {
        console.error('Failed to save tech details toggle:', e)
      }
    }
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
    if (!activeConversation) return
    try {
      await ArchiveConversation(activeConversation.id)
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
      await UpdateConversationTechDetails(id, true)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.tech_details = true
      }
    } catch (e) {
      console.error('Failed to toggle tech details:', e)
    }
  }

  async function handleToggleContextDetails(id) {
    try {
      await UpdateConversationContextDetails(id, true)
      if (activeConversation && activeConversation.id === id) {
        activeConversation.context_details = true
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
          onclick={() => showNewDiscussion = true}
          type="button"
          title="New Discussion"
        >
          <Plus size={16} />
        </button>
      {:else}
        <button class="btn-new-discussion" onclick={() => showNewDiscussion = true} type="button">
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
            <button class="btn btn-primary" onclick={() => showNewDiscussion = true}>+ New Discussion</button>
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
                <div class="conversation-row">
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
        {processingMessage}
        {messageError}
        userMessage={userMessage}
        showTechDetails={showTechDetails}
        showContextDetails={showContextDetails}
        maxMessages={activeConversation?.max_messages || 0}
        onMaxMessagesChange={handleSetMaxMessages}
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
        {dataSources}
        onUpdate={loadData}
      />
    {:else if activeView === 'about'}
      <div class="about-view">
        <div class="about-content">
          <h2>YourQL</h2>
          <p class="version">Version {appVersion}</p>
          <p class="description">{appDescription}</p>

          <div class="about-disclaimer">
            <h3>Disclaimer</h3>
            <p>The models you configure will have access to the databases you configure. Databases with sensitive data should be used responsibly. If you are in doubt about the sensitivity of the data you have access to in your database, then do not use this application.</p>
            <p>This application can be quarantined in an environment such that the models and databases are local and no data leaves the network environment it is in, but this requires technical knowledge and execution.</p>
          </div>

          <div class="about-section">
            <h3>What is YourQL?</h3>
            <p>YourQL is a desktop application that lets you query databases using natural language. It uses Large Language Models (LLMs) to translate your questions into SQL queries and executes them against your configured databases.</p>
          </div>

          <div class="about-section">
            <h3>Key Features</h3>
            <ul>
              <li>Natural language to SQL conversion</li>
              <li>Support for multiple LLM providers (OpenAI, Anthropic, Ollama, Local)</li>
              <li>Data sources (MySQL, SQLite, CSV, Excel)</li>
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
              <li><strong>Data:</strong> SQLite (local app data) + MySQL/SQLite (external connections)</li>
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
            class="msg-limit-btn {selectedConversation.max_context_messages === 0 ? 'active' : ''}"
            onclick={() => handleSetMaxContextMessages(0)}
          >All</button>
          <input
            type="number"
            value={selectedConversation.max_context_messages || ''}
            placeholder="e.g. 20"
            min="1"
            max="500"
            oninput={(e) => {
              const val = parseInt(e.target.value)
              if (val >= 1 && val <= 500) {
                selectedConversation.max_context_messages = val
              }
            }}
            onblur={() => handleSetMaxContextMessages(selectedConversation.max_context_messages || 0)}
          />
        </div>
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
          Show technical details by default
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
        <div style="color: #999; font-size: var(--font-xs); margin-top: var(--space-2xs);">
          LLM summarizes query results as a plain-English answer
        </div>
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
            <label>LLM Provider (optional)</label>
            <select bind:value={selectedLLMProvider}>
              <option value={null}>Default</option>
              {#each llmProviders as provider}
                <option value={provider}>{provider.name}</option>
              {/each}
            </select>
          </div>

          <div class="form-group">
            <label>Data Source (optional)</label>
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
    background-color: #ffffff;
    color: #000000;
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
    background: #f5f5f5;
    display: flex;
    flex-direction: column;
    padding: var(--space-4xl) 0;    border-right: 1px solid #e0e0e0;
    border-right: 1px solid #e0e0e0;
    transition: width 0.2s ease, min-width 0.2s ease, padding 0.2s ease;
    overflow: hidden;
  }

  .sidebar.collapsed {
    width: var(--sidebar-collapsed);
    min-width: var(--sidebar-collapsed);
    padding: var(--space-4xl) 0;  }

  .sidebar-header {
    padding: 0 var(--space-4xl) var(--space-6xl);
    border-bottom: 1px solid #e0e0e0;
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
    color: #666666;
    transition: all 0.2s ease;
    flex-shrink: 0;
  }
  
  .sidebar-toggle:hover {
    background: rgba(2, 136, 209, 0.1);
    color: #0288d1;
  }

  .sidebar.collapsed .sidebar-header {
    padding: 0 var(--space-3xl) var(--space-4xl);
    justify-content: center;
  }

  .sidebar-header h1 {
    margin: 0;
    font-size: var(--font-4xl);
    font-weight: 700;
    color: #0277bd;
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
    color: #666666;
    font-size: var(--font-lg);
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
    gap: var(--space-xl);
  }

  .nav-item:hover {
    background: rgba(2, 136, 209, 0.1);
    color: #0288d1;
  }

  .nav-item.active {
    background: rgba(2, 136, 209, 0.15);
    color: #0288d1;
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
    border-top: 1px solid #e0e0e0;
  }

  .btn-new-discussion {
    width: 100%;
    padding: var(--space-lg) 0;    border: none;
    border-radius: var(--radius-md);
    background: #0288d1;
    color: #ffffff;
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: background 0.2s ease;
  }

  .btn-new-discussion:hover {
    background: #0288d1;
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
    background: #ffffff;
    overflow-y: auto;
    position: relative;
    transition: width 0.2s ease;
  }

  .error-banner {
    background: #ffebee;
    border: 1px solid #ffcdd2;
    color: #b71c1c;
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
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .view-header h2 {
    margin: 0;
    font-size: var(--font-5xl);
    font-weight: 600;
    color: #000000;
  }

  .view-header-actions {
    display: flex;
    align-items: center;
    gap: var(--space-xl);
  }

  .archived-toggle {
    font-size: var(--font-base);
    color: #808080;
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
    color: #cccccc;
  }

  .empty-state p {
    margin: 0 0 var(--space-lg);
    font-size: var(--font-2xl);
  }

  .empty-state .hint {
    font-size: var(--font-md);
    color: #bbbbbb;
  }

  .conversations-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-lg);
  }

  .conversation-row {
    display: flex;
    align-items: stretch;
    gap: var(--space-md);
  }

  .conversation-item {
    background: #f9f9f9;
    padding: var(--space-2xl) var(--space-4xl);
    border-radius: var(--radius-md);
    border: 1px solid #e0e0e0;
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
    color: #000000;
    margin-bottom: var(--space-md);
  }

  .conversation-meta {
    display: flex;
    gap: var(--space-lg);
    font-size: var(--font-sm);
    color: #999999;
  }

  .conversation-date {
    color: #999999;
  }

  .conversation-model, .conversation-db {
    background: rgba(2, 136, 209, 0.1);
    color: #0288d1;
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
    background: #ffffff;
    border-radius: var(--radius-md);
    width: var(--modal-width);
    max-width: 90vw;
    max-height: 90vh;
    overflow-y: auto;
    border: 2px solid #0288d1;
  }

  .modal-header {
    padding: var(--space-4xl) var(--space-6xl);
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-header h3 {
    margin: 0;
    font-size: var(--font-3xl);
    font-weight: 600;
    color: #000000;
  }

  .modal-close {
    background: none;
    border: none;
    font-size: var(--font-4xl);
    color: #999999;
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
    background: #f5f5f5;
    color: #000000;
  }

  .modal-body {
    padding: var(--space-6xl);
  }

  .modal-footer {
    padding: var(--space-4xl) var(--space-6xl);
    border-top: 1px solid #e0e0e0;
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
    color: #333333;
  }

  .form-group input,
  .form-group select {
    width: 100%;
    padding: var(--space-lg) var(--space-xl);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: #000000;
    background: #ffffff;
    transition: border-color 0.2s ease;
    box-sizing: border-box;
  }

  .form-group input:focus,
  .form-group select:focus {
    outline: none;
    border-color: #0288d1;
    border: 2px solid #0288d1;
  }

  .form-group input::placeholder {
    color: #cccccc;
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
    background: #0288d1;
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
    padding: var(--space-lg) var(--space-4xl);
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
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
    width: 1.75rem;
    height: 1.75rem;
    border: none;
    background: transparent;
    color: #ccc;
    font-size: var(--font-xl);
    cursor: pointer;
    border-radius: var(--radius-md);
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
    max-width: 25rem;
  }

  .delete-confirm .modal-body p {
    margin: 0 0 var(--space-md);
    color: #333;
  }

  .delete-confirm .hint {
    font-size: var(--font-base);
    color: #999;
  }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid #ef5350;
    color: #ef5350;
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
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-xl);
    transition: all 0.2s ease;
  }

  .gear-btn:hover {
    background: #e0e0e0;
    color: #000000;
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
    background: #ffffff;
    border: 2px solid #0288d1;
    border-radius: var(--radius-md);
    border: 2px solid #0288d1;
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
    border-bottom: 1px solid #f0f0f0;
    font-weight: 600;
    font-size: var(--font-md);
    color: #1a1a1a;
  }

  .gear-popover-close {
    background: none;
    border: none;
    font-size: var(--font-xl);
    color: #999999;
    cursor: pointer;
    padding: 0 var(--space-xs);
  }

  .gear-popover-close:hover {
    color: #000000;
  }

  .gear-popover-section {
    padding: var(--space-md) 18px;
  }

  .gear-popover-section label {
    display: block;
    font-size: var(--font-xs);
    font-weight: 600;
    color: #666666;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: var(--space-xs);
  }

  .gear-popover-section select {
    width: 100%;
    padding: var(--space-md) var(--space-xl);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: #000000;
    background: #f9f9f9;
    cursor: pointer;
    box-sizing: border-box;
  }

  .gear-popover-section select:focus {
    outline: none;
    border-color: #0288d1;
  }

  .gear-popover-section input[type="text"] {
    width: 100%;
    padding: var(--space-md) var(--space-xl);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: #000000;
    background: #f9f9f9;
    box-sizing: border-box;
    transition: border-color 0.2s ease;
  }

  .gear-popover-section input[type="text"]:focus {
    outline: none;
    border-color: #0288d1;
  }

  .message-limit-group {
    display: flex;
    gap: var(--space-md);
    align-items: center;
  }

  .msg-limit-btn {
    padding: var(--space-md) var(--space-2xl);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    background: #f9f9f9;
    color: #333333;
    font-size: var(--font-base);
    cursor: pointer;
    transition: all 0.2s ease;
    white-space: nowrap;
  }

  .msg-limit-btn:hover {
    background: #e0e0e0;
  }

  .msg-limit-btn.active {
    background: #0288d1;
    color: #ffffff;
    border-color: #0288d1;
  }

  .message-limit-group input[type="number"] {
    flex: 1;
    padding: var(--space-sm) var(--space-lg);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: #000000;
    background: #f9f9f9;
    box-sizing: border-box;
  }

  .message-limit-group input[type="number"]:focus {
    outline: none;
    border-color: #0288d1;
  }

  .gear-popover-section label {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    font-size: var(--font-base);
    color: #333333;
    cursor: pointer;
  }

  .gear-popover-section label input[type="checkbox"] {
    width: var(--space-3xl);
    height: var(--space-3xl);
    accent-color: #0288d1;
  }

  .gear-popover-actions {
    display: flex;
    gap: var(--space-md);
    padding: var(--space-2xl) 18px;
    border-top: 1px solid #f0f0f0;
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

  .gear-action-btn.duplicate {
    background: #ffffff;
    color: #1976d2;
    border-color: #1976d2;
  }

  .gear-action-btn.duplicate:hover {
    background: #1976d2;
    color: #ffffff;
  }

  .gear-action-btn.clear {
    background: #ffffff;
    color: #ff9800;
    border-color: #ff9800;
  }

  .gear-action-btn.clear:hover {
    background: #ff9800;
    color: #ffffff;
  }

  .gear-action-btn.delete {
    background: #ffffff;
    color: #d32f2f;
    border-color: #d32f2f;
    font-weight: 600;
  }

  .gear-action-btn.delete:hover {
    background: #d32f2f;
    color: #ffffff;
  }

  .gear-popover-divider {
    height: 1px;
    background: #f0f0f0;
    margin: 0 18px;
  }

  .gear-popover-actions {
    display: flex;
    gap: var(--space-md);
    padding: var(--space-lg) 18px;
    border-top: 1px solid #f0f0f0;
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

  .gear-action-btn.delete {
    background: #ffffff;
    color: #d32f2f;
    border-color: #d32f2f;
    font-weight: 600;
  }

  .gear-action-btn.delete:hover {
    background: #d32f2f;
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
    max-width: var(--content-max-width);
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
    color: #c62828;
    margin-bottom: 0.75rem;
  }

  .about-disclaimer p {
    font-size: 0.95rem;
    color: #5d4037;
    line-height: 1.6;
    margin-bottom: 0.5rem;
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
