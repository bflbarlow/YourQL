<script>
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import { ProcessUserMessage, CancelProcessing } from '../wailsjs/go/main/App.js'
  import { createDefaultConversation } from './lib/api.js'
  import SettingsView from './components/settings/SettingsView.svelte'
  import VizChart from './VizChart.svelte'
  import { getChartConfig, getSqlFromMessage } from './lib/format.js'
  import { createStreamStore, attachAutoScroll } from './lib/stream.svelte.js'

  let {
    initialView = 'discussion',
    conversations = [],
    activeConversation = null,
    conversationMessages = [],
    llmProviders = [],
    dataSources = [],
    processingActive = false,
    onExit = () => {},
    onConversationChange = () => {},
    onMessageSent = () => {}
  } = $props()

  // ── Navigation state ─────────────────────────────────────────
  let currentView = $state(initialView)
  let showNav = $state(false)

  // ── Discussion state ─────────────────────────────────────────
  let inputText = $state('')
  let showDiscussionsOverlay = $state(false)
  let showCommands = $state(false)
  let commandFilter = $state('')
  let showSql = $state({})
  let processing = $state(false)
  let processingError = $state(null)
  let textareaEl = $state(null)

  // Local message state to avoid mutating props
  let localMessages = $state([])
  $effect(() => { localMessages = conversationMessages || [] })

  // ── Auto-scroll ─────────────────────────────────────────────
  let messagesEl = $state(null)

  // Scroll to bottom when messages change (new message, switch discussion)
  $effect(() => {
    localMessages
    if (messagesEl) {
      attachAutoScroll(messagesEl)
    }
  })

  // Scroll to bottom as streaming content arrives
  $effect(() => {
    if (streamingActive && messagesEl) {
      attachAutoScroll(messagesEl)
    }
  })

  // ── Streaming (shared store) ────────────────────────────────
  const stream = createStreamStore()
  stream.bind(() => activeConversation?.id)
  let streamingActive = $derived(stream.active)
  let streamingText = $derived(stream.text)
  let streamingReasoning = $derived(stream.reasoningActive)
  let streamingReasoningText = $derived(stream.reasoningText)
  let streamingToolCards = $derived(stream.toolCards)

  // ── Derived ──────────────────────────────────────────────────
  let llmNameByID = $derived(Object.fromEntries(llmProviders.map(p => [p.id, p.name])))
  let dataSourceNameByID = $derived(Object.fromEntries(dataSources.map(d => [d.id, d.name])))
  let hasMessages = $derived(localMessages && localMessages.length > 0)
  let hasDataSources = $derived(dataSources && dataSources.length > 0)
  let hasProviders = $derived(llmProviders && llmProviders.length > 0)

  // Streaming is only available when the conversation has it enabled.
  // The backend only emits `llm:stream` events in that case; this gate
  // keeps the UI from ever showing a streaming bubble when it shouldn't.
  let streamingEnabled = $derived(activeConversation?.streaming_enabled === true)

  // ── Slash commands ──────────────────────────────────────────
  const commands = [
    { name: '/new',    desc: 'Start a fresh conversation' },
    { name: '/source', desc: 'Switch data source' },
    { name: '/model',  desc: 'Switch LLM provider' },
    { name: '/history', desc: 'Show recent conversations' },
    { name: '/exit',   desc: 'Exit minimalist mode' },
    { name: '/help',   desc: 'Show available commands' }
  ]
  let filteredCommands = $derived(
    commandFilter ? commands.filter(c => c.name.startsWith(commandFilter)) : commands
  )

  // ── Navigation ──────────────────────────────────────────────
  function navigateTo(view) {
    showNav = false
    currentView = view
  }

  function toggleNav() { showNav = !showNav }

  // ── Input handling ──────────────────────────────────────────
  function handleInput(e) {
    const val = e.target.value
    inputText = val
    if (val.startsWith('/') && val.length > 0) {
      commandFilter = val
      showCommands = true
    } else {
      showCommands = false
      commandFilter = ''
    }
  }

  function handleKeyDown(e) {
    if (e.key === 'Enter' && !e.shiftKey && !showCommands) {
      e.preventDefault()
      sendMessage()
    } else if (e.key === 'Escape') {
      showCommands = false
      showDiscussionsOverlay = false
      showNav = false
    }
  }

  async function executeCommand(cmd) {
    showCommands = false
    inputText = ''
    commandFilter = ''
    switch (cmd) {
      case '/new':
        try {
          const conv = await createDefaultConversation()
          currentView = 'discussion'
          await onConversationChange(conv)
        } catch (e) { console.error('Failed to create conversation:', e) }
        break
      case '/history':
        showDiscussionsOverlay = true
        break
      case '/exit':
        onExit()
        break
      case '/help':
        inputText = '/'
        showCommands = true
        break
      default: break
    }
  }

  async function sendMessage() {
    const msg = inputText.trim()
    if (!msg || !activeConversation || processing || processingActive) return
    inputText = ''
    processing = true
    processingError = null
    const tempId = -(Date.now())
    localMessages = [...localMessages, { id: tempId, role: 'user', content: msg, created_at: new Date().toISOString(), metadata: null }]
    try {
      await ProcessUserMessage(activeConversation.id, msg)
      await onMessageSent(activeConversation.id)
    } catch (e) {
      processingError = e.toString()
      localMessages = localMessages.filter(m => m.id !== tempId)
    } finally { processing = false }
  }

  function cancelSend() { CancelProcessing(activeConversation?.id).catch(e => console.error('Failed to cancel:', e)) }

  // ── Discussions ─────────────────────────────────────────────
  async function selectDiscussion(conv) {
    showDiscussionsOverlay = false
    currentView = 'discussion'
    await onConversationChange(conv)
  }

  async function createNewDiscussion() {
    showDiscussionsOverlay = false
    try {
      // Honors Discussion Defaults → app-wide defaults (Two Modes Principle).
      const conv = await createDefaultConversation()
      currentView = 'discussion'
      await onConversationChange(conv)
    } catch (e) { console.error('Failed to create discussion:', e) }
  }

  // ── SQL toggle ──────────────────────────────────────────────
  function toggleSql(msgId) { showSql = { ...showSql, [msgId]: !showSql[msgId] } }

  // ── Auto-resize ─────────────────────────────────────────────
  $effect(() => {
    const el = textareaEl
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, Math.floor(window.innerHeight * 0.4)) + 'px'
    inputText
  })
</script>

<!-- ================================================================ -->
<div class="mm-shell">
  <!-- Persistent toggle (top-right, hover-revealed) -->
  <button class="mm-toggle" onclick={onExit} title="Exit minimalist mode" type="button">✕ Exit</button>
  <!-- Hamburger menu (top-left) -->
  <button class="mm-hamburger" onclick={toggleNav} title="Menu" type="button">☰</button>
  {#if showNav}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="mm-nav-popover" role="menu">
      <button class="mm-nav-row" onclick={() => navigateTo('discussions')}>Discussions</button>
      <button class="mm-nav-row" onclick={() => navigateTo('settings')}>Settings</button>
      <button class="mm-nav-row" onclick={() => navigateTo('about')}>About</button>
      <div class="mm-nav-divider"></div>
      <button class="mm-nav-row mm-nav-row--danger" onclick={onExit}>Exit minimal mode</button>
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div style="position:fixed;inset:0;z-index:997" onclick={() => showNav = false} role="none"></div>
  {/if}

  {#if currentView === 'discussion'}
    <!-- ===== Discussion View ===================================== -->
    {#if processing}
      <div class="mm-progress"></div>
    {/if}
    <!-- Minimal title: discussion title + model · data source -->
    <div class="mm-titlebar">
      {#if activeConversation?.title}
        <h1 class="mm-discussion-title">{activeConversation.title}</h1>
      {/if}
      <span class="mm-title">
        {llmNameByID[activeConversation?.llm_provider_id] || 'No model'}
        {#if dataSourceNameByID[activeConversation?.data_source_id]}
          <span class="mm-title-dot">·</span>
          {dataSourceNameByID[activeConversation?.data_source_id]}
        {/if}
      </span>
    </div>
    <div class="mm-scroll" bind:this={messagesEl}>
      <div class="mm-column">
        {#if !hasMessages && !streamingActive && !processing}
          {#if !hasDataSources || !hasProviders}
            <div class="mm-setup-prompt">
              {#if !hasDataSources}
                <p>Connect a data source to get started.</p>
              {/if}
              {#if !hasProviders}
                <p>Configure an LLM provider to get started.</p>
              {/if}
              <a href="#" onclick={(e) => { e.preventDefault(); navigateTo('settings') }}>Open Settings</a>
            </div>
          {:else}
            <div class="mm-empty">
              <h1>Ask a question about your data.</h1>
            </div>
          {/if}
        {:else}
          <div class="mm-thread">
            {#each localMessages as msg (msg.id)}
              {#if msg.role === 'user'}
                <div class="mm-msg mm-msg--user">
                  <div class="mm-msg__label">You</div>
                  <div class="mm-msg__body">{msg.content}</div>
                </div>
              {:else if msg.role === 'assistant'}
                <div class="mm-msg mm-msg--assistant">
                  <div class="mm-msg__label">YourQL</div>
                  {#if getChartConfig(msg)}
                    <div class="mm-chart-container">
                      <VizChart config={getChartConfig(msg)} standalone={false} />
                    </div>
                  {/if}
                  <div class="mm-msg__body">{@html msg.content}</div>
                  {#if getSqlFromMessage(msg)}
                    <button class="mm-sql-toggle" onclick={() => toggleSql(msg.id)}>
                      {showSql[msg.id] ? 'Hide SQL' : 'Show SQL'}
                    </button>
                    {#if showSql[msg.id]}
                      <div class="mm-sql-block">{getSqlFromMessage(msg)}</div>
                    {/if}
                  {/if}
                </div>
              {:else if msg.role === 'system'}
                <div class="mm-msg mm-msg--assistant">
                  <div class="mm-msg__body" style="color: var(--text-tertiary); font-size: 0.75rem; text-align: center;">{msg.content}</div>
                </div>
              {/if}
            {/each}
            {#if processing}
              {#if streamingEnabled && streamingActive}
                <div class="mm-msg mm-msg--assistant">
                  <div class="mm-msg__label">YourQL</div>
                  {#if streamingReasoning}
                    <details class="mm-reasoning" open>
                      <summary>Thinking…</summary>
                      <div class="mm-reasoning-text">{streamingReasoningText || '…'}</div>
                    </details>
                  {:else}
                    <div class="mm-msg__body">
                      {#if streamingText}{streamingText}<span class="mm-cursor"></span>
                      {:else if streamingToolCards.length > 0}<span class="mm-cursor"></span>
                      {/if}
                    </div>
                  {/if}
                  {#each streamingToolCards as card}
                    <div class="mm-tool-card">
                      <span class="mm-tool-card__dot"></span>
                      <span class="mm-tool-card__name">{card.name}</span>
                      <span class="mm-tool-card__status">{card.status === 'executing' ? 'running…' : 'forming…'}</span>
                    </div>
                  {/each}
                  {#if !streamingReasoning && !streamingText && streamingToolCards.length === 0}
                    <div class="mm-msg__body"><span class="mm-cursor"></span></div>
                  {/if}
                </div>
              {:else}
                <!-- Streaming disabled (or no events yet): show a quiet
                     thinking indicator so the user knows work is happening. -->
                <div class="mm-msg mm-msg--assistant">
                  <div class="mm-msg__label">YourQL</div>
                  <div class="mm-msg__body"><span class="mm-cursor"></span></div>
                </div>
              {/if}
            {/if}
          </div>
        {/if}
        {#if processingError}
          <div class="mm-error">
            Something went wrong.
            <button class="mm-error__retry" onclick={() => { processingError = null }}>Dismiss</button>
          </div>
        {/if}
      </div>
    </div>
    <!-- Input bar -->
    <div class="mm-inputbar" style="position: relative;">
      <div class="mm-input-row">
        <textarea
          bind:this={textareaEl}
          class="mm-input"
          rows="1"
          placeholder="Ask a question…"
          value={inputText}
          oninput={handleInput}
          onkeydown={handleKeyDown}
          disabled={processing}
        ></textarea>
        {#if processing}
          <button class="mm-send mm-send--visible" onclick={cancelSend} title="Cancel">■</button>
        {:else if inputText.trim()}
          <button class="mm-send mm-send--visible" onclick={sendMessage} title="Send">↑</button>
        {/if}
      </div>
      {#if showCommands && filteredCommands.length > 0}
        <div class="mm-command-popover">
          {#each filteredCommands as cmd (cmd.name)}
            <div class="mm-command-item" onclick={() => executeCommand(cmd.name)}>
              <span class="mm-command-item__name">{cmd.name}</span>
              <span class="mm-command-item__desc">{cmd.desc}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
    <!-- Discussions overlay -->
    {#if showDiscussionsOverlay}
      <div class="mm-discussions-overlay" onclick={() => showDiscussionsOverlay = false}>
        <div class="mm-discussions-list" onclick={(e) => e.stopPropagation()}>
          <button class="mm-discussion-row mm-discussion-new" onclick={createNewDiscussion}>+ New discussion</button>
          {#each conversations as conv}
            <button class="mm-discussion-row" onclick={() => selectDiscussion(conv)}>
              <span class="mm-discussion-row__title">{conv.title || 'Untitled'}</span>
              <span class="mm-discussion-row__meta">
                {llmNameByID[conv.llm_provider_id] || ''}
                {#if llmNameByID[conv.llm_provider_id] && dataSourceNameByID[conv.data_source_id]} · {/if}
                {dataSourceNameByID[conv.data_source_id] || ''}
              </span>
            </button>
          {/each}
        </div>
      </div>
    {/if}

  {:else if currentView === 'discussions'}
    <!-- ===== Discussions List View =============================== -->
    <div class="mm-discussions-page">
      <div class="mm-scroll">
        <div class="mm-discussions-list">
          <button class="mm-discussion-row mm-discussion-new" onclick={() => { navigateTo('discussion'); createNewDiscussion() }}>+ New discussion</button>
          {#each conversations as conv}
            <button class="mm-discussion-row" onclick={() => selectDiscussion(conv)}>
              <span class="mm-discussion-row__title">{conv.title || 'Untitled'}</span>
              <span class="mm-discussion-row__meta">
                {llmNameByID[conv.llm_provider_id] || ''}
                {#if llmNameByID[conv.llm_provider_id] && dataSourceNameByID[conv.data_source_id]} · {/if}
                {dataSourceNameByID[conv.data_source_id] || ''}
              </span>
            </button>
          {/each}
          {#if conversations.length === 0}
            <p style="color: var(--text-tertiary); font-size: 0.75rem; text-align: center; padding: 2rem 0;">No discussions yet</p>
          {/if}
        </div>
      </div>
    </div>

  {:else if currentView === 'settings'}
    <!-- ===== Settings View ======================================= -->
    <SettingsView
      {llmProviders}
      {dataSources}
      onUpdate={() => {}}
      minimalist={true}
      onMinimalistChange={(enabled) => { if (!enabled) onExit() }}
    />

  {:else if currentView === 'about'}
    <!-- ===== About View ========================================== -->
    <div class="mm-about">
      <h2 style="font-size: 2.5rem; font-weight: 700; color: var(--text-primary); margin-bottom: 0.5rem;">YourQL</h2>
      <p>Talk to your database in plain English.</p>
      <a href="#" onclick={(e) => { e.preventDefault(); onExit() }}>View full details</a>
    </div>
  {/if}
</div>