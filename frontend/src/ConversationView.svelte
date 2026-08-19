<script>
  import { Settings, Pin, Search, ChevronRight, ChevronDown, MessageSquare, FileDown, Printer, FileCode, FileText } from 'lucide-svelte'
  import VizChart from './VizChart.svelte'
  import { ExportConversationPDF, ExportConversationHTML, ExportConversationMarkdown, CancelProcessing } from '../wailsjs/go/main/App.js'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  let {
    activeConversation,
    conversationMessages = [],
    llmProviders = [],
    dataSources = [],
    processingMessage = '',
    messageError = null,
    userMessage = '',
    showTechDetails = false,
    showContextDetails = false,
    maxMessages = 0,
    onSendMessage = () => {},
    onBack = () => {},
    onMessageChange = () => {},
    onGearClick = () => {}
  } = $props()

  let localMessage = $state(userMessage)
  let textareaEl = $state(null)
  let customHeights = $state({})
  let showExportMenu = $state(false)

  $effect(() => {
    localMessage = userMessage
  })

  // Close export dropdown on outside click
  function handleExportClick(e) {
    if (!e.target.closest('.export-btn-wrapper')) {
      showExportMenu = false
    }
  }
  $effect(() => {
    if (showExportMenu) {
      document.addEventListener('click', handleExportClick)
      return () => document.removeEventListener('click', handleExportClick)
    }
  })

  // Restore saved height (or reset) when conversation changes
  $effect(() => {
    const el = textareaEl
    const cid = activeConversation?.id
    if (!el || !cid) return
    if (customHeights[cid]) {
      el.style.height = customHeights[cid]
    } else {
      el.style.height = ''
    }
  })

  // ── Streaming state ──────────────────────────────────────────
  let streamingActive = $state(false)
  let streamingText = $state('')
  let streamingReasoning = $state(false)
  let streamingToolCards = $state([]) // [{name, args, status}]

  EventsOn('llm:stream', (data) => {
    if (!activeConversation || data.conversation_id !== activeConversation.id) return
    const ev = data.event
    switch (ev.type) {
      case 'reasoning_start':
        streamingActive = true
        streamingReasoning = true
        break
      case 'reasoning_end':
        streamingReasoning = false
        break
      case 'content_delta':
        streamingActive = true
        streamingText += ev.content
        break
      case 'tool_call_start':
        streamingActive = true
        streamingToolCards = [...streamingToolCards, { name: ev.tool_name || 'tool', args: '', status: 'forming' }]
        break
      case 'tool_call_delta':
        streamingToolCards = streamingToolCards.map(c =>
          c.name === ev.tool_name && ev.arguments ? { ...c, args: c.args + ev.arguments } : c)
        break
      case 'tool_call_end':
        streamingToolCards = streamingToolCards.map(c =>
          c.name === ev.tool_name ? { ...c, status: 'executing' } : c)
        break
      case 'done':
        streamingActive = false
        streamingText = ''
        streamingToolCards = []
        streamingReasoning = false
        break
    }
  })

  function handleInput(e) {
    localMessage = e.target.value
    onMessageChange(localMessage)
    // Skip auto-resize if user manually set a height for this conversation
    const cid = activeConversation?.id
    if (cid && customHeights[cid]) return
    // Auto-resize textarea
    const el = e.target
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, Math.floor(window.innerHeight * 0.5)) + 'px'
  }

  let isDragging = $state(false)
  let dragStartY = $state(0)
  let dragStartHeight = $state(0)

  function startResize(e) {
    e.preventDefault()
    isDragging = true
    dragStartY = e.clientY
    dragStartHeight = textareaEl ? textareaEl.getBoundingClientRect().height : 44
  }

  // Drag-to-resize: move the top edge up/down
  $effect(() => {
    if (!isDragging) return
    const handleMove = (e) => {
      const delta = dragStartY - e.clientY // up = positive
      const minH = 44
      const maxH = Math.floor(window.innerHeight * 0.5)
      const newH = Math.max(minH, Math.min(dragStartHeight + delta, maxH))
      if (textareaEl) textareaEl.style.height = newH + 'px'
    }
    const handleUp = () => {
      isDragging = false
      const cid = activeConversation?.id
      if (cid && textareaEl) {
        customHeights = { ...customHeights, [cid]: textareaEl.style.height }
      }
    }
    document.addEventListener('mousemove', handleMove)
    document.addEventListener('mouseup', handleUp)
    return () => {
      document.removeEventListener('mousemove', handleMove)
      document.removeEventListener('mouseup', handleUp)
    }
  })

  function handleKeyDown(e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (localMessage.trim() && !processingMessage) {
        sendAndReset()
      }
    }
  }

  function sendAndReset() {
    // Clear custom height and reset textarea to default after submit
    const cid = activeConversation?.id
    if (cid && customHeights[cid]) {
      const newHeights = { ...customHeights }
      delete newHeights[cid]
      customHeights = newHeights
    }
    if (textareaEl) textareaEl.style.height = ''
    onSendMessage()
  }

  function handleCancel() {
    CancelProcessing(activeConversation?.id).catch(e =>
      console.error('Failed to cancel processing:', e))
  }

  function parsePayload(metadata) {
    if (!metadata) return null
    try {
      return JSON.parse(metadata)
    } catch {
      return null
    }
  }

  let enrichedMessages = $derived(
    conversationMessages.map(m => ({
      ...m,
      payload: parsePayload(m.metadata)
    }))
  )

  let filteredMessages = $derived(
    (() => {
      let msgs = showTechDetails
        ? enrichedMessages
        : enrichedMessages.filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'system')

      // Merge consecutive assistant messages into a single bubble.
      // The tool-calling protocol naturally produces two assistant
      // messages per turn (one for the results table, one for the
      // model's prose answer). Combining them here keeps the chat
      // clean without requiring the backend to produce a single
      // combined message.
      let merged = []
      for (let i = 0; i < msgs.length; i++) {
        if (msgs[i].role === 'assistant' && i + 1 < msgs.length && msgs[i + 1].role === 'assistant') {
          merged.push({
            ...msgs[i],
            content: msgs[i].content + msgs[i + 1].content,
            id: msgs[i].id,
            created_at: msgs[i + 1].created_at
          })
          i++ // skip the next one since we merged it
        } else {
          merged.push(msgs[i])
        }
      }
      msgs = merged

      // Apply max_messages limit - show last N messages (skip if showing all)
      if (!showHiddenMessages && maxMessages > 0 && msgs.length > maxMessages) {
        return msgs.slice(msgs.length - maxMessages)
      }
      return msgs
    })()
  )

  // Per-message payload toggles (§4.7)
  let payloadToggles = $state({})

  // Toggle for showing hidden messages when maxMessages is active
  let showHiddenMessages = $state(false)

  function getChartConfig(message) {
    try {
      if (!message.metadata) return null
      const meta = typeof message.metadata === 'string' ? JSON.parse(message.metadata) : message.metadata
      return meta?.chart_config || null
    } catch { return null }
  }

  let visibleMessageCount = $derived(
    conversationMessages.filter(m => m.role === 'user' || m.role === 'assistant').length
  )
  let tokenSummary = $derived.by(() => {
    let promptTotal = 0
    let completionTotal = 0
    let msgCount = 0
    for (const m of conversationMessages) {
      const payload = parsePayload(m.metadata)
      if (!payload || !payload.response_json) continue
      try {
        const resp = typeof payload.response_json === 'string'
          ? JSON.parse(payload.response_json)
          : payload.response_json
        const usage = resp.usage || resp.usage_info
        if (usage) {
          // OpenAI format
          if (usage.prompt_tokens) promptTotal += usage.prompt_tokens
          if (usage.completion_tokens) completionTotal += usage.completion_tokens
          // Anthropic format
          if (usage.input_tokens) promptTotal += usage.input_tokens
          if (usage.output_tokens) completionTotal += usage.output_tokens
          msgCount++
        }
      } catch {}
    }
    return { promptTotal, completionTotal, msgCount }
  })

  function togglePayload(msgId, type) {
    const key = `${msgId}-${type}`
    payloadToggles[key] = !payloadToggles[key]
  }

  function fmtTokens(n) {
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
    return String(n)
  }

  // Global functions for inline HTML event handlers in assistant messages
  window.toggleSQLSection = function(btn, sectionId) {
    const section = document.getElementById(sectionId)
    if (!section) return
    const isVisible = section.style.display === 'block'
    section.style.display = isVisible ? 'none' : 'block'
  }

  window.copySQL = function(codeId) {
    const code = document.getElementById(codeId)
    if (!code) return
    navigator.clipboard.writeText(code.textContent).then(() => {
      const btn = code.parentElement?.parentElement?.querySelector('.copy-sql-btn')
      if (btn) {
        const orig = btn.textContent
        btn.textContent = 'Copied!'
        setTimeout(() => { btn.textContent = orig }, 1500)
      }
    })
  }

  window.exportCSV = function(btn, rowCount) {
    const table = btn.closest('.results-card')?.querySelector('.result-table')
    if (!table) return
    const headers = [...table.querySelectorAll('thead th')].map(th => th.textContent.trim())
    const rows = [...table.querySelectorAll('tbody tr')].map(tr =>
      [...tr.querySelectorAll('td')].map(td => '"' + td.textContent.replace(/"/g, '""') + '"')
    )
    const csv = [headers.join(','), ...rows.map(r => r.join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'query-results.csv'
    a.click()
    URL.revokeObjectURL(url)
  }

  // Format relative timestamps (§4.8)
  function formatTime(dateStr) {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now - date
    const diffMins = Math.floor(diffMs / 60000)
    const diffHours = Math.floor(diffMs / 3600000)

    const timeStr = date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })

    if (diffMins < 1) return 'Just now'
    if (diffMins < 60) return `${diffMins}m ago`
    if (diffHours < 24) return timeStr

    const yesterday = new Date(now)
    yesterday.setDate(yesterday.getDate() - 1)
    if (date.toDateString() === yesterday.toDateString()) {
      return `Yesterday ${timeStr}`
    }
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + timeStr
  }

  // Auto-scroll on new messages (§4.8)
  let messagesEl

  $effect(() => {
    filteredMessages  // trigger on change
    if (messagesEl) {
      clearTimeout(autoScrollTimer)
      autoScrollTimer = setTimeout(() => {
        messagesEl.scrollTo({
          top: messagesEl.scrollHeight,
          behavior: 'instant'
        })
      }, 0)
    }
  })

  let autoScrollTimer

  function handlePrintPDF() {
    const scale = localStorage.getItem('yourql-ui-scale') || 'medium'
    const fontSize = { small: '12px', medium: '13px', large: '14px' }[scale]
    const style = document.createElement('style')
    style.id = 'print-scale'
    style.textContent = '@media print { html { font-size: ' + fontSize + ' !important; } }'
    document.head.appendChild(style)
    document.title = 'YourQL - ' + (activeConversation?.title || 'Untitled') + ' - ' + new Date().toLocaleDateString()
    ExportConversationPDF()
    setTimeout(() => {
      document.title = 'YourQL'
      const el = document.getElementById('print-scale')
      if (el) el.remove()
    }, 1000)
  }

  async function handleExportHTML() {
    if (!activeConversation?.id) return
    const err = await ExportConversationHTML(activeConversation.id)
    if (err) console.error('HTML export failed:', err)
  }

  async function handleExportMarkdown() {
    if (!activeConversation?.id) return
    const err = await ExportConversationMarkdown(activeConversation.id)
    if (err) console.error('Markdown export failed:', err)
  }

  // Auto-scroll as streaming content arrives — fires on every character
  // delta so the user always sees the latest output without manual scrolling.
  $effect(() => {
    // touch the streaming state so Svelte re-runs on every delta
    const _t = streamingText
    const _c = streamingToolCards.length
    if (streamingActive && messagesEl) {
      messagesEl.scrollTo({
        top: messagesEl.scrollHeight,
        behavior: 'instant'
      })
    }
  })
</script>

<div class="conversation-view">
  <div class="conversation-header">
    <button class="back-btn" onclick={onBack}>← Back</button>
    <div class="conversation-info">
      <h3>{activeConversation?.title || 'Untitled'}</h3>
      <div class="conversation-meta">
        {#if activeConversation?.llm_provider_id}
          <span class="meta-tag">{llmProviders.find(p => p.id === activeConversation.llm_provider_id)?.name || 'LLM'}</span>
        {/if}
        {#if activeConversation?.data_source_id}
          <span class="meta-tag">{dataSources.find(c => c.id === activeConversation.data_source_id)?.name || 'Data'}</span>
        {/if}
        {#if showContextDetails && tokenSummary.msgCount > 0}
          <span class="meta-tag context-tag">
            {fmtTokens(tokenSummary.promptTotal)}&uarr; {fmtTokens(tokenSummary.completionTotal)}&darr; tokens
          </span>
          <span class="meta-tag context-tag">
            {visibleMessageCount} msg{visibleMessageCount !== 1 ? 's' : ''}
          </span>
        {/if}
      </div>
    </div>

    <div class="export-btn-wrapper">
      <button
        class="export-btn-header"
        onclick={() => showExportMenu = !showExportMenu}
        title="Export conversation"
        type="button"
      ><FileDown size={16} /></button>
      {#if showExportMenu}
        <div class="export-popover" role="menu">
          <button
            class="export-popover-item"
            onclick={() => { showExportMenu = false; handlePrintPDF() }}
            type="button"
            role="menuitem"
          ><Printer size={14} /><span>Print to PDF</span></button>
          <button
            class="export-popover-item"
            onclick={() => { showExportMenu = false; handleExportHTML() }}
            type="button"
            role="menuitem"
          ><FileCode size={14} /><span>Export as HTML</span></button>
          <button
            class="export-popover-item"
            onclick={() => { showExportMenu = false; handleExportMarkdown() }}
            type="button"
            role="menuitem"
          ><FileText size={14} /><span>Export as Markdown</span></button>
        </div>
      {/if}
    </div>
    <button class="gear-btn-header" onclick={onGearClick} title="Conversation settings" type="button"><Settings size={16} /></button>
  </div>

  <div class="messages-container" bind:this={messagesEl}>
    {#if maxMessages > 0 && visibleMessageCount > maxMessages}
      <div class="collapsed-messages-banner">
        <span><Pin size={14} class="pin-icon" /> {visibleMessageCount - maxMessages} older message(s) hidden</span>
        {#if showHiddenMessages}
          <button class="show-all-btn" onclick={() => showHiddenMessages = false}>Show last {maxMessages}</button>
        {:else}
          <button class="show-all-btn" onclick={() => showHiddenMessages = true}>Show all</button>
        {/if}
      </div>
    {/if}
    {#if filteredMessages.length === 0}
      <div class="empty-conversation">
        <p>{conversationMessages.length === 0 ? 'No messages yet' : 'No messages match the current filter'}</p>
        {#if conversationMessages.length === 0}
          <p class="hint">Ask a question about your data, or type a question to get started.</p>
        {/if}
      </div>
    {:else}
      {#each filteredMessages as message (message.id)}
        <div class="message {message.role}">
          <div class="message-content">
            {#if message.role === 'user'}
              <!-- §4.8: preserve line breaks -->
              <div class="user-message" style="white-space: pre-wrap">{message.content}</div>
            {:else if message.role === 'system'}
              <div class="system-message">{message.content}</div>
            {:else if message.role === 'exploration'}
              <div class="exploration-result">
                <div class="exploration-header">
                  <span class="exploration-icon"><Search size={14} /></span>
                  <span class="exploration-title">{message.content}</span>
                </div>

                {#if message.payload}
                  {#if message.payload.version}
                    <!-- New TechDetail format -->
                    <div class="tech-detail-summary">
                      {#if message.payload.kind}
                        <span class="tech-detail-chip">{message.payload.kind}</span>
                      {/if}
                      {#if message.payload.duration_ms}
                        <span class="tech-detail-stat">{message.payload.duration_ms}ms</span>
                      {/if}
                      {#if message.payload.request?.message_count}
                        <span class="tech-detail-stat">{message.payload.request.message_count} msgs</span>
                      {/if}
                      {#if message.payload.response?.tool_calls?.length}
                        <span class="tech-detail-stat">{message.payload.response.tool_calls.length} tool call(s)</span>
                      {/if}
                      {#if message.payload.response?.finish_reason}
                        <span class="tech-detail-stat">→ {message.payload.response.finish_reason}</span>
                      {/if}
                      {#if message.payload.sql?.query}
                        <span class="tech-detail-stat">SQL: {message.payload.sql.row_count || 0} rows</span>
                      {/if}
                      {#if message.payload.stream?.chunk_count}
                        <span class="tech-detail-stat">{message.payload.stream.chunk_count} chunks</span>
                      {/if}
                    </div>

                    <!-- Expand for SQL query -->
                    {#if message.payload.sql?.query}
                      <div class="payload-section">
                        <div class="payload-toggle" onclick={() => togglePayload(message.id, 'sql')}>
                          <span>📋 SQL: {message.payload.sql.query.substring(0, 80)}{message.payload.sql.query.length > 80 ? '…' : ''}</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-sql']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </div>
                        {#if payloadToggles[message.id + '-sql']}
                          <pre class="payload-content">{message.payload.sql.query}</pre>
                          {#if message.payload.sql.error}
                            <div class="error-detail">{message.payload.sql.error}</div>
                          {/if}
                        {/if}
                      </div>
                    {/if}

<!-- Expand for raw model output — the full text the model emitted before parsing. -->
                    {#if message.payload.response?.raw_output}
                      <div class="payload-section">
                        <div class="payload-toggle" onclick={() => togglePayload(message.id, 'raw')}>
                          <span>📄 Raw model output ({message.payload.response.raw_output.length} chars)</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-raw']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </div>
                        {#if payloadToggles[message.id + '-raw']}
                          <pre class="payload-content">{message.payload.response.raw_output}</pre>
                        {/if}
                      </div>
                    {/if}

                    <!-- Expand for request messages -->
                    {#if message.payload.request?.raw_messages}
                      <div class="payload-section">
                        <div class="payload-toggle" onclick={() => togglePayload(message.id, 'req-msgs')}>
                          <span>📥 Request messages ({message.payload.request.message_count} total)</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-req-msgs']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </div>
                        {#if payloadToggles[message.id + '-req-msgs']}
                          <pre class="payload-content">{message.payload.request.raw_messages}</pre>
                        {/if}
                      </div>
                    {/if}

                    <!-- Expand for tool call arguments -->
                    {#each message.payload.response?.tool_calls || [] as tc}
                      <div class="payload-section">
                        <div class="payload-toggle" onclick={() => togglePayload(message.id, 'tc-' + tc.name)}>
                          <span>🔧 {tc.name}: {tc.arguments?.substring(0, 60)}{tc.arguments?.length > 60 ? '…' : ''}</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-tc-' + tc.name]}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </div>
                        {#if payloadToggles[message.id + '-tc-' + tc.name]}
                          <pre class="payload-content">{tc.arguments}</pre>
                        {/if}
                      </div>
                    {/each}

                    <!-- Expand for raw error (legacy error messages) -->
                    {#if showTechDetails && message.payload?.raw_error}
                      <div class="payload-section error-detail">
                        <div class="payload-toggle" onclick={() => togglePayload(message.id, 'error')}>
                          <span>⚠ Raw Error Details</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-error']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </div>
                        {#if payloadToggles[message.id + '-error']}
                          <pre class="payload-content">{message.payload.raw_error}</pre>
                        {/if}
                      </div>
                    {/if}

                  {:else if message.payload.request_json}
                    <!-- Old payload format (legacy) -->
                    <div class="payload-section">
                      <div class="payload-toggle" onclick={() => togglePayload(message.id, 'request')}>
                        <span>up Request Payload</span>
                        <span class="toggle-icon">{#if payloadToggles[message.id + '-request']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                      </div>
                      {#if payloadToggles[message.id + '-request']}
                        <pre class="payload-content">{JSON.stringify(message.payload.request_json, null, 2)}</pre>
                      {/if}
                    </div>

                    <div class="payload-section">
                      <div class="payload-toggle" onclick={() => togglePayload(message.id, 'response')}>
                        <span>down Response Payload</span>
                        <span class="toggle-icon">{#if payloadToggles[message.id + '-response']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                      </div>
                      {#if payloadToggles[message.id + '-response']}
                        <pre class="payload-content">{JSON.stringify(message.payload.response_json, null, 2)}</pre>
                      {/if}
                    </div>

                    {#if message.payload.llm_messages}
                      <div class="payload-section">
                        <div class="payload-toggle" onclick={() => togglePayload(message.id, 'messages')}>
                          <span><MessageSquare size={12} /> LLM Messages ({message.payload.llm_messages.length})</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-messages']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </div>
                        {#if payloadToggles[message.id + '-messages']}
                          <pre class="payload-content">{JSON.stringify(message.payload.llm_messages, null, 2)}</pre>
                        {/if}
                      </div>
                    {/if}
                  {/if}
                {/if}
              </div>
            {:else}
              <!-- §4.8: assistant messages with HTML -->
              <!-- Chart visualization — rendered first so the chart is the primary content -->
              {#if getChartConfig(message)}
                <div class="viz-result-card">
                  <VizChart config={getChartConfig(message)} standalone={false} />
                  <div class="assistant-message">{@html message.content}</div>
                </div>
              {:else}
                <div class="assistant-message">{@html message.content}</div>
              {/if}
              <!-- Raw error detail (visible when tech details is enabled) -->
              {#if showTechDetails && message.payload?.raw_error}
                <div class="payload-section error-detail">
                  <div class="payload-toggle" onclick={() => togglePayload(message.id, 'error')}>
                    <span>⚠ Raw Error Details</span>
                    <span class="toggle-icon">{#if payloadToggles[message.id + '-error']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                  </div>
                  {#if payloadToggles[message.id + '-error']}
                    <pre class="payload-content">{message.payload.raw_error}</pre>
                  {/if}
                </div>
              {/if}
            {/if}
          </div>
          <div class="message-time">
            {formatTime(message.created_at)}
          </div>
        </div>
      {/each}

      {#if streamingActive}
        <div class="message assistant">
          <div class="message-content">
            <div class="streaming-bubble">
              {#if streamingReasoning}
                <details class="streaming-reasoning">
                  <summary>💭 Thinking…</summary>
                  <div class="streaming-reasoning-text">{streamingText || '…'}</div>
                </details>
              {:else}
                {#if streamingText}
                  <div class="streaming-text">{streamingText}</div>
                {/if}
              {/if}
              {#each streamingToolCards as card}
                <div class="streaming-tool-card">
                  <span class="tool-status-dot"></span>
                  <span class="tool-name">{card.name}</span>
                  <span class="tool-status">{card.status === 'executing' ? '⏳ running…' : '⚙ forming…'}</span>
                </div>
              {/each}
              {#if !streamingText && streamingToolCards.length === 0}
                <div class="loading-indicator">
                  <div class="loading-dots">
                    <span></span><span></span><span></span>
                  </div>
                </div>
              {/if}
            </div>
          </div>
        </div>
      {/if}

      {#if processingMessage && !streamingActive}
        <div class="message assistant">
          <div class="message-content">
            <div class="loading-indicator">
              <div class="loading-dots">
                <span></span>
                <span></span>
                <span></span>
              </div>
              <span>{processingMessage || 'Processing...'}</span>
            </div>
          </div>
        </div>
      {/if}
    {/if}
  </div>

  <div class="message-input-container">
    <div
      class="resize-handle"
      class:resize-handle--dragging={isDragging}
      onmousedown={startResize}
      role="separator"
      aria-label="Drag to resize message input"
    >
      <div class="resize-handle__grip"></div>
    </div>
    {#if messageError}
      <div class="error-message">{messageError}</div>
    {/if}

    <div class="message-input-wrapper">
      <textarea
        bind:this={textareaEl}
        value={localMessage}
        placeholder="Type your message..."
        oninput={handleInput}
        onkeydown={handleKeyDown}
        rows="1"
      ></textarea>
      {#if processingMessage}
        <button
          class="cancel-btn"
          onclick={handleCancel}
          title="Cancel processing"
        >
          <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor">
            <rect x="6" y="6" width="12" height="12" rx="2"></rect>
          </svg>
        </button>
      {:else}
        <button
          class="send-btn"
          onclick={sendAndReset}
          disabled={!localMessage.trim()}
        >
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="22" y1="2" x2="11" y2="13"></line>
            <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
          </svg>
        </button>
      {/if}
    </div>
  </div>
</div>

<style>
  .conversation-view {
    height: 100%;
    display: flex;
    flex-direction: column;
    position: relative;
  }

  .conversation-header {
    padding: var(--space-4xl) var(--space-6xl);
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    align-items: center;
    gap: var(--space-2xl);
  }

  .gear-btn-header {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.25rem;
    height: 2.25rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-2xl);
    color: var(--text-secondary);
    transition: all 0.2s ease;
    flex-shrink: 0;
  }

  .gear-btn-header:hover { background: var(--border-primary); color: var(--text-primary); }

  .export-btn-header {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.25rem;
    height: 2.25rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-2xl);
    color: var(--text-secondary);
    transition: all 0.2s ease;
    flex-shrink: 0;
  }

  .export-btn-header:hover { background: var(--border-primary); color: var(--text-primary); }

  .export-btn-wrapper { position: relative; flex-shrink: 0; }

  .export-popover {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    box-shadow: 0 4px 16px rgba(0,0,0,0.12);
    z-index: 100;
    min-width: 180px;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .export-popover-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    font-size: var(--font-sm);
    cursor: pointer;
    text-align: left;
    width: 100%;
    transition: background 0.15s ease;
  }

  .export-popover-item:hover { background: var(--bg-secondary); }

  @media print {
    @page { size: portrait; margin: 0.5in; }
    * { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .conversation-header .back-btn,
    .conversation-header .export-btn-wrapper,
    .conversation-header .gear-btn-header,
    .conversation-header .conversation-meta,
    .message-input-container,
    .collapsed-messages-banner { display: none !important; }
    .conversation-view { height: auto; overflow: visible; }
    .messages-container { overflow: visible; padding: 0; }
    .assistant-message { background: var(--bg-primary); border: 1px solid var(--border-primary); }
    .user-message { background: var(--color-accent-print, #0288d1); color: #fff; }
    .results-details[open] .table-container { max-height: none; overflow: visible; }
    .results-card .collapsed-row { display: table-row !important; }
  }

  .back-btn {
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    color: var(--text-secondary);
    padding: var(--space-md) var(--space-xl);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-md);
    transition: all 0.2s ease;
  }

  .back-btn:hover { background: var(--border-primary); color: var(--text-primary); }

  .conversation-info { flex: 1; }
  .conversation-info h3 { margin: 0 0 0.3125rem; font-size: var(--font-2xl); font-weight: 600; color: var(--text-primary); }

  .conversation-meta { display: flex; gap: var(--space-md); }

  .meta-tag {
    background: rgba(2, 136, 209, 0.1);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-md);
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
  }

  .context-tag {
    background: rgba(102, 102, 102, 0.08);
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .messages-container {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-6xl);
  }

  .empty-conversation {
    text-align: center;
    padding: var(--sidebar-collapsed) var(--space-4xl);
    color: var(--text-tertiary);
  }

  .empty-conversation p { margin: 0 0 var(--space-lg); font-size: var(--font-xl); }
  .empty-conversation .hint { font-size: var(--font-md); color: #bbbbbb; }

  .collapsed-messages-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-lg) var(--space-3xl);
    background: var(--color-accent-light);
    border: 1px solid var(--color-accent-border);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-3xl);
    font-size: var(--font-base);
    color: var(--color-accent);
  }

  .show-all-btn {
    padding: var(--space-xs) var(--space-xl);
    background: var(--color-accent);
    color: #ffffff;
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
    cursor: pointer;
    transition: background 0.2s ease;
  }

  .show-all-btn:hover {
    background: var(--color-accent);
  }

  .message {
    margin-bottom: var(--space-4xl);
    display: flex;
    flex-direction: column;
    animation: messageSlideIn 0.35s ease-out;
  }
  .message.user { align-items: flex-end; }
  .message.assistant { align-items: flex-start; }

  .message-content { max-width: var(--content-max-width); width: 100%; }

  .user-message {
    background: var(--color-accent);
    color: #ffffff;
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    line-height: 1.5;
  }

  .system-message {
    color: var(--text-tertiary);
    font-size: var(--font-sm, 0.8rem);
    text-align: center;
    padding: var(--space-sm) 0;
    opacity: 0.7;
  }

  .assistant-message {
    background: var(--bg-tertiary);
    color: var(--text-primary);
    padding: var(--space-3xl) var(--space-4xl);
    border-radius: var(--radius-md);
    border: 1px solid var(--border-primary);
    font-size: var(--font-md);
    line-height: 1.6;
    position: relative;
  }

  .assistant-message pre {
    background: #1e1e1e;
    color: #d4d4d4;
    padding: var(--space-3xl);
    border-radius: var(--radius-md);
    overflow-x: auto;
    font-family: 'Courier New', monospace;
    font-size: var(--font-base);
    margin: var(--space-lg) 0;  }

  .assistant-message code {
    background: var(--border-secondary);
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-md);
    font-family: 'Courier New', monospace;
    font-size: var(--font-base);
  }

  .assistant-message table { border-collapse: collapse; width: 100%; margin: var(--space-lg) 0; }
  .assistant-message th, .assistant-message td { border: 1px solid var(--border-primary); padding: var(--space-md) var(--space-xl); text-align: left; }
  .assistant-message th { background: var(--bg-secondary); font-weight: 600; }

  /* Markdown content (summaries, explanations) */
  .assistant-message .markdown-content ul,
  .assistant-message .markdown-content ol { padding-left: 1.5rem; margin: 0.5rem 0; }
  .assistant-message .markdown-content li { margin-bottom: 0.25rem; }
  .assistant-message .markdown-content a { color: var(--color-accent); }

  .message-time { font-size: var(--font-sm); color: var(--text-tertiary); margin-top: 0.3125rem; }
  .message.user .message-time { text-align: right; }

  .loading-indicator {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
    color: var(--text-tertiary);
    font-size: var(--font-md);
  }

  .loading-dots { display: flex; gap: var(--space-xs); }
  .loading-dots span {
    width: var(--space-md); height: var(--space-md);
    background: var(--color-accent);
    border-radius: var(--radius-md);
    animation: loading 1.4s infinite ease-in-out;
  }
  .loading-dots span:nth-child(1) { animation-delay: -0.32s; }
  .loading-dots span:nth-child(2) { animation-delay: -0.16s; }

  /* ── Streaming bubble ─────────────────────────────────────── */
  .streaming-bubble {
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-lg);
    padding: var(--space-lg) var(--space-xl);
    font-size: var(--font-sm);
    line-height: 1.6;
  }
  .streaming-text {
    white-space: pre-wrap;
    word-break: break-word;
  }
  .streaming-reasoning {
    margin: 0;
  }
  .streaming-reasoning summary {
    cursor: pointer;
    color: var(--text-secondary);
    font-size: var(--font-xs);
    user-select: none;
    padding: var(--space-xs) 0;
  }
  .streaming-reasoning-text {
    font-family: var(--font-mono, monospace);
    font-size: var(--font-2xs);
    color: var(--text-tertiary);
    background: var(--bg-secondary);
    border-radius: var(--radius-md);
    padding: var(--space-md);
    margin-top: var(--space-xs);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .streaming-tool-card {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    padding: var(--space-sm) var(--space-md);
    margin-top: var(--space-sm);
    background: var(--bg-surface);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-2xs);
  }
  .tool-status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-accent);
    animation: loading 1.4s infinite ease-in-out;
  }
  .tool-name {
    font-weight: 600;
    color: var(--text-primary);
  }
  .tool-status {
    color: var(--text-secondary);
    margin-left: auto;
  }

  @keyframes loading {
    0%, 80%, 100% { transform: scale(0.6); opacity: 0.5; }
    40% { transform: scale(1); opacity: 1; }
  }

  @keyframes messageSlideIn {
    from {
      opacity: 0;
      transform: translateY(var(--space-3xl)) scale(0.97);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .message-input-container { padding: 0 var(--space-6xl) var(--space-4xl); border-top: 1px solid #e0e0e0; background: var(--bg-primary); }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid var(--color-danger);
    color: var(--color-danger);
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    margin: var(--space-4xl) var(--space-6xl) var(--space-2xl);
  }

  .message-input-wrapper { display: flex; gap: var(--space-lg); align-items: flex-end; }

  .resize-handle {
    height: 10px;
    cursor: ns-resize;
    display: flex;
    align-items: center;
    justify-content: center;
    margin: var(--space-md) 0;
    transition: background 0.15s ease;
    border-radius: 3px;
  }
  .resize-handle__grip {
    width: 36px;
    height: 4px;
    background: var(--border-primary);
    border-radius: 2px;
    transition: background 0.15s ease, width 0.15s ease;
  }
  .resize-handle:hover .resize-handle__grip,
  .resize-handle--dragging .resize-handle__grip {
    background: var(--color-accent);
    width: 48px;
  }

  .message-input-wrapper textarea {
    flex: 1;
    padding: var(--space-xl) var(--space-3xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: var(--text-primary);
    background: var(--bg-tertiary);
    resize: none;
    font-family: inherit;
    line-height: 1.5;
    min-height: 2.75rem;
    max-height: 50vh;
    transition: border-color 0.2s ease;
  }

  .message-input-wrapper textarea:focus {
    outline: none;
    border-color: var(--color-accent);
    border: 2px solid var(--color-accent);
  }

  .message-input-wrapper textarea::placeholder { color: var(--text-tertiary); }

  .send-btn {
    background: var(--color-accent);
    color: #ffffff;
    border: none;
    border-radius: var(--radius-md);
    width: 2.75rem; height: 2.75rem;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .send-btn:hover:not(:disabled) { background: var(--color-accent); }
  .send-btn:disabled { opacity: 0.6; cursor: not-allowed; }

  .cancel-btn {
    background: var(--color-error, #ef4444);
    color: #ffffff;
    border: none;
    border-radius: var(--radius-md);
    width: 2.75rem; height: 2.75rem;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .cancel-btn:hover { background: var(--color-error-hover, #dc2626); }

  .exploration-result {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    padding: var(--space-3xl) var(--space-4xl);
  }

  .sort-header {
    cursor: pointer;
    transition: background 0.2s ease;
  }

  .sort-header:hover {
    background: var(--color-accent-light);
  }

  .sort-indicator {
    font-size: var(--font-2xs);
    margin-left: var(--space-xs);
    color: var(--text-tertiary);
  }

  .exploration-header {
    display: flex; align-items: center; gap: var(--space-md);
    margin-bottom: var(--space-xl); padding-bottom: var(--space-md);
    border-bottom: 1px solid #e0e0e0;
  }

  .exploration-icon { font-size: var(--font-xl); }
  .exploration-title { font-weight: 600; color: var(--color-accent); font-size: var(--font-md); }

  .tech-detail-summary {
    display: flex; flex-wrap: wrap; gap: var(--space-sm);
    padding: var(--space-md) 0 var(--space-xs);
    font-size: var(--font-2xs);
  }
  .tech-detail-chip {
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: 1px 6px;
    border-radius: var(--radius-sm);
    font-weight: 600;
    text-transform: uppercase;
    font-size: var(--font-3xs);
  }
  .tech-detail-stat {
    color: var(--text-secondary);
    padding: 1px 4px;
    border-right: 1px solid var(--border-primary);
  }
  .tech-detail-stat:last-child { border-right: none; }
  .error-detail {
    color: var(--color-danger, #e74c3c);
    font-size: var(--font-xs);
    padding: var(--space-sm) var(--space-md);
  }

  .payload-section {
    margin-top: var(--space-xl); border: 1px solid var(--border-primary);
    border-radius: var(--radius-md); overflow: hidden;
  }

  .payload-toggle {
    display: flex; justify-content: space-between; align-items: center;
    padding: var(--space-lg) var(--space-2xl); background: var(--bg-secondary);
    cursor: pointer; font-size: var(--font-base); font-weight: 500;
    color: var(--text-primary); transition: background 0.2s ease;
  }
  .payload-toggle:hover { background: var(--border-primary); }
  .toggle-icon { font-size: var(--font-sm); color: var(--text-secondary); }

  .payload-content {
    background: #1e1e1e; color: #d4d4d4;
    padding: var(--space-xl) margin: 0;
    font-family: 'Courier New', monospace; font-size: var(--font-sm);
    line-height: 1.5; overflow-x: auto;
    white-space: pre-wrap; word-break: break-all;
    max-height: 25rem;
  }

  .viz-result-card {
    background: var(--bg-surface);
    border-radius: 8px;
    overflow: hidden;
  }
  .viz-result-card .assistant-message {
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 0 1rem 1rem 1rem;
  }
  .viz-result-card .viz-chart-container {
    width: 100% !important;
    max-width: 100%;
    overflow: hidden;
  }
  .viz-result-card canvas {
    max-width: 100% !important;
    width: 100% !important;
  }
  .viz-result-card .results-card {
    background: transparent !important;
    border: none !important;
    margin: 0 !important;
  }
  .viz-result-card .results-toolbar {
    background: transparent !important;
    border: none !important;
    padding: 0 !important;
    margin-bottom: 0 !important;
  }
  .viz-result-card .results-details {
    margin-top: 0 !important;
  }
  .viz-result-card .results-details summary {
    background: none !important;
    border-radius: 0 !important;
    padding-left: 1rem !important;
  }
  .viz-result-card .explore-block {
    margin: 0 !important;
    padding: 0 1rem;
  }
</style>
