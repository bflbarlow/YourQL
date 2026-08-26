<script>
  import { Search, ChevronRight, ChevronDown, Pin, MessageSquare, Settings, FileCode, FileJson, FileInput, Wrench, AlertTriangle, Lightbulb } from 'lucide-svelte'
  import VizChart from './VizChart.svelte'
  import ExportMenu from './components/conversation/ExportMenu.svelte'
  import IconBtn from './components/ui/IconBtn.svelte'
  import Busy from './components/ui/Busy.svelte'
  import { CancelProcessing } from '../wailsjs/go/main/App.js'
  import { parsePayload, getChartConfig, fmtTokens, formatTime } from './lib/format.js'
  import { createStreamStore, attachAutoScroll } from './lib/stream.svelte.js'

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

  $effect(() => {
    localMessage = userMessage
  })

  // ── Streaming (shared store) ─────────────────────────────────
  const stream = createStreamStore()
  stream.bind(() => activeConversation?.id)
  const streamingEnabled = $derived(activeConversation?.streaming_enabled === true)

  // Restore saved height (or reset) when conversation changes
  $effect(() => {
    const el = textareaEl
    const cid = activeConversation?.id
    if (!el || !cid) return
    el.style.height = customHeights[cid] || ''
  })

  function handleInput(e) {
    localMessage = e.target.value
    onMessageChange(localMessage)
    const cid = activeConversation?.id
    if (cid && customHeights[cid]) return
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

  $effect(() => {
    if (!isDragging) return
    const handleMove = (e) => {
      const delta = dragStartY - e.clientY
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

  let enrichedMessages = $derived(
    conversationMessages.map(m => ({
      ...m,
      payload: parsePayload(m.metadata)
    }))
  )

  let filteredMessages = $derived.by(() => {
    let msgs = showTechDetails
      ? enrichedMessages
      : enrichedMessages.filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'system')

    // Merge consecutive assistant messages into a single bubble.
    // The tool-calling protocol naturally produces two assistant messages per
    // turn (results table + prose answer). Combining them keeps the chat clean.
    let merged = []
    for (let i = 0; i < msgs.length; i++) {
      if (msgs[i].role === 'assistant' && i + 1 < msgs.length && msgs[i + 1].role === 'assistant') {
        merged.push({
          ...msgs[i],
          content: msgs[i].content + msgs[i + 1].content,
          id: msgs[i].id,
          created_at: msgs[i + 1].created_at
        })
        i++
      } else {
        merged.push(msgs[i])
      }
    }
    msgs = merged

    if (!showHiddenMessages && maxMessages > 0 && msgs.length > maxMessages) {
      return msgs.slice(msgs.length - maxMessages)
    }
    return msgs
  })

  // Per-message payload toggles
  let payloadToggles = $state({})
  let showHiddenMessages = $state(false)

  function togglePayload(msgId, type) {
    const key = `${msgId}-${type}`
    payloadToggles = { ...payloadToggles, [key]: !payloadToggles[key] }
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
          if (usage.prompt_tokens) promptTotal += usage.prompt_tokens
          if (usage.completion_tokens) completionTotal += usage.completion_tokens
          if (usage.input_tokens) promptTotal += usage.input_tokens
          if (usage.output_tokens) completionTotal += usage.output_tokens
          msgCount++
        }
      } catch {}
    }
    return { promptTotal, completionTotal, msgCount }
  })

  // Auto-scroll on new messages / streaming deltas
  let messagesEl = $state(null)
  let autoScrollTimer

  $effect(() => {
    filteredMessages
    if (messagesEl) {
      clearTimeout(autoScrollTimer)
      autoScrollTimer = setTimeout(() => attachAutoScroll(messagesEl), 0)
    }
  })

  $effect(() => {
    if (stream.active && streamingEnabled && messagesEl) {
      attachAutoScroll(messagesEl)
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

    <ExportMenu conversation={activeConversation} />
    <IconBtn label="Conversation settings" onclick={onGearClick}>
      <Settings size={16} />
    </IconBtn>
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
              <!-- preserve line breaks -->
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

                    {#if message.payload.sql?.query}
                      <div class="payload-section">
                        <button class="payload-toggle" onclick={() => togglePayload(message.id, 'sql')} type="button">
                          <span class="payload-toggle-label"><FileCode size={12} /> SQL: {message.payload.sql.query.substring(0, 80)}{message.payload.sql.query.length > 80 ? '…' : ''}</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-sql']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </button>
                        {#if payloadToggles[message.id + '-sql']}
                          <pre class="payload-content">{message.payload.sql.query}</pre>
                          {#if message.payload.sql.error}
                            <div class="error-detail">{message.payload.sql.error}</div>
                          {/if}
                        {/if}
                      </div>
                    {/if}

                    <!-- Raw model output -->
                    {#if message.payload.response?.raw_output}
                      <div class="payload-section">
                        <button class="payload-toggle" onclick={() => togglePayload(message.id, 'raw')} type="button">
                          <span class="payload-toggle-label"><FileJson size={12} /> Raw model output ({message.payload.response.raw_output.length} chars)</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-raw']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </button>
                        {#if payloadToggles[message.id + '-raw']}
                          <pre class="payload-content">{message.payload.response.raw_output}</pre>
                        {/if}
                      </div>
                    {/if}

                    <!-- Request messages -->
                    {#if message.payload.request?.raw_messages}
                      <div class="payload-section">
                        <button class="payload-toggle" onclick={() => togglePayload(message.id, 'req-msgs')} type="button">
                          <span class="payload-toggle-label"><FileInput size={12} /> Request messages ({message.payload.request.message_count} total)</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-req-msgs']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </button>
                        {#if payloadToggles[message.id + '-req-msgs']}
                          <pre class="payload-content">{message.payload.request.raw_messages}</pre>
                        {/if}
                      </div>
                    {/if}

                    <!-- Tool call arguments -->
                    {#each message.payload.response?.tool_calls || [] as tc}
                      <div class="payload-section">
                        <button class="payload-toggle" onclick={() => togglePayload(message.id, 'tc-' + tc.name)} type="button">
                          <span class="payload-toggle-label"><Wrench size={12} /> {tc.name}: {tc.arguments?.substring(0, 60)}{tc.arguments?.length > 60 ? '…' : ''}</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-tc-' + tc.name]}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </button>
                        {#if payloadToggles[message.id + '-tc-' + tc.name]}
                          <pre class="payload-content">{tc.arguments}</pre>
                        {/if}
                      </div>
                    {/each}

                    <!-- Raw error (legacy error messages) -->
                    {#if showTechDetails && message.payload?.raw_error}
                      <div class="payload-section error-detail">
                        <button class="payload-toggle" onclick={() => togglePayload(message.id, 'error')} type="button">
                          <span class="payload-toggle-label"><AlertTriangle size={12} /> Raw Error Details</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-error']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </button>
                        {#if payloadToggles[message.id + '-error']}
                          <pre class="payload-content">{message.payload.raw_error}</pre>
                        {/if}
                      </div>
                    {/if}

                  {:else if message.payload.request_json}
                    <!-- Old payload format (legacy) -->
                    <div class="payload-section">
                      <button class="payload-toggle" onclick={() => togglePayload(message.id, 'request')} type="button">
                        <span class="payload-toggle-label">↑ Request Payload</span>
                        <span class="toggle-icon">{#if payloadToggles[message.id + '-request']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                      </button>
                      {#if payloadToggles[message.id + '-request']}
                        <pre class="payload-content">{JSON.stringify(message.payload.request_json, null, 2)}</pre>
                      {/if}
                    </div>

                    <div class="payload-section">
                      <button class="payload-toggle" onclick={() => togglePayload(message.id, 'response')} type="button">
                        <span class="payload-toggle-label">↓ Response Payload</span>
                        <span class="toggle-icon">{#if payloadToggles[message.id + '-response']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                      </button>
                      {#if payloadToggles[message.id + '-response']}
                        <pre class="payload-content">{JSON.stringify(message.payload.response_json, null, 2)}</pre>
                      {/if}
                    </div>

                    {#if message.payload.llm_messages}
                      <div class="payload-section">
                        <button class="payload-toggle" onclick={() => togglePayload(message.id, 'messages')} type="button">
                          <span class="payload-toggle-label"><MessageSquare size={12} /> LLM Messages ({message.payload.llm_messages.length})</span>
                          <span class="toggle-icon">{#if payloadToggles[message.id + '-messages']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                        </button>
                        {#if payloadToggles[message.id + '-messages']}
                          <pre class="payload-content">{JSON.stringify(message.payload.llm_messages, null, 2)}</pre>
                        {/if}
                      </div>
                    {/if}
                  {/if}
                {/if}
              </div>
            {:else}
              <!-- Assistant messages with HTML -->
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
                  <button class="payload-toggle" onclick={() => togglePayload(message.id, 'error')} type="button">
                    <span class="payload-toggle-label"><AlertTriangle size={12} /> Raw Error Details</span>
                    <span class="toggle-icon">{#if payloadToggles[message.id + '-error']}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}</span>
                  </button>
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

      {#if stream.active && streamingEnabled}
        <div class="message assistant">
          <div class="message-content">
            <div class="streaming-bubble">
              {#if stream.reasoningActive}
                <details class="streaming-reasoning" open>
                  <summary><Lightbulb size={11} /> Thinking…</summary>
                  <div class="streaming-reasoning-text">{stream.reasoningText || '…'}</div>
                </details>
              {:else}
                {#if stream.text}
                  <div class="streaming-text">{stream.text}</div>
                {/if}
              {/if}
              {#each stream.toolCards as card}
                <div class="streaming-tool-card">
                  <span class="tool-status-dot"></span>
                  <span class="tool-name">{card.name}</span>
                  <span class="tool-status">{card.status === 'executing' ? 'running…' : 'forming…'}</span>
                </div>
              {/each}
              {#if !stream.text && stream.toolCards.length === 0}
                <Busy />
              {/if}
            </div>
          </div>
        </div>
      {/if}

      {#if processingMessage && !(stream.active && streamingEnabled)}
        <div class="message assistant">
          <div class="message-content">
            <Busy label={processingMessage || 'Processing...'} />
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
          aria-label="Cancel processing"
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
          aria-label="Send message"
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
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    align-items: center;
    gap: var(--space-2xl);
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

  .conversation-meta { display: flex; gap: var(--space-md); flex-wrap: wrap; }

  .meta-tag {
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-md);
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
  }
  .context-tag {
    background: rgba(128, 128, 128, 0.08);
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
  .empty-conversation .hint { font-size: var(--font-md); color: var(--text-tertiary); }

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
  .conversation-header :global(.pin-icon), .collapsed-messages-banner :global(.pin-icon) { vertical-align: -2px; margin-right: 2px; }

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
  .show-all-btn:hover { background: var(--color-accent-hover); }

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
    font-size: var(--font-sm);
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

  .assistant-message :global(pre) {
    background: var(--code-bg);
    color: var(--code-fg);
    padding: var(--space-3xl);
    border-radius: var(--radius-md);
    overflow-x: auto;
    font-family: var(--font-mono);
    font-size: var(--font-base);
    margin: var(--space-lg) 0;
  }

  .assistant-message :global(code) {
    background: var(--border-secondary);
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-md);
    font-family: var(--font-mono);
    font-size: var(--font-base);
  }

  .assistant-message :global(table) { border-collapse: collapse; width: 100%; margin: var(--space-lg) 0; }
  .assistant-message :global(th), .assistant-message :global(td) { border: 1px solid var(--border-primary); padding: var(--space-md) var(--space-xl); text-align: left; }
  .assistant-message :global(th) { background: var(--bg-secondary); font-weight: 600; }

  .assistant-message :global(.markdown-content ul),
  .assistant-message :global(.markdown-content ol) { padding-left: 1.5rem; margin: 0.5rem 0; }
  .assistant-message :global(.markdown-content li) { margin-bottom: 0.25rem; }
  .assistant-message :global(.markdown-content a) { color: var(--color-accent); }

  .message-time { font-size: var(--font-sm); color: var(--text-tertiary); margin-top: 0.3125rem; }
  .message.user .message-time { text-align: right; }

  /* Streaming bubble */
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
  .streaming-reasoning summary {
    cursor: pointer;
    color: var(--text-secondary);
    font-size: var(--font-xs);
    user-select: none;
    padding: var(--space-xs) 0;
    display: flex;
    align-items: center;
    gap: var(--space-xs);
    list-style: none;
  }
  .streaming-reasoning-text {
    font-family: var(--font-mono);
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
    width: 8px; height: 8px; border-radius: 50%;
    background: var(--color-accent);
    animation: busy-loading 1.4s infinite ease-in-out;
  }
  .tool-name { font-weight: 600; color: var(--text-primary); }
  .tool-status { color: var(--text-secondary); margin-left: auto; }

  @keyframes busy-loading {
    0%, 80%, 100% { transform: scale(0.6); opacity: 0.5; }
    40% { transform: scale(1); opacity: 1; }
  }

  @keyframes messageSlideIn {
    from { opacity: 0; transform: translateY(var(--space-3xl)) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  .message-input-container {
    padding: 0 var(--space-6xl) var(--space-4xl);
    border-top: 1px solid var(--border-primary);
    background: var(--bg-primary);
  }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid var(--color-danger);
    color: var(--color-danger);
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    margin: var(--space-4xl) 0 var(--space-2xl);
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
    width: 36px; height: 4px;
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
    background: var(--bg-input);
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
    flex-shrink: 0;
  }
  .send-btn:hover:not(:disabled) { background: var(--color-accent-hover); }
  .send-btn:disabled { opacity: 0.6; cursor: not-allowed; }

  .cancel-btn {
    background: var(--color-danger);
    color: #ffffff;
    border: none;
    border-radius: var(--radius-md);
    width: 2.75rem; height: 2.75rem;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s ease;
    flex-shrink: 0;
  }
  .cancel-btn:hover { background: var(--color-danger-hover); }

  .exploration-result {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    padding: var(--space-3xl) var(--space-4xl);
  }

  .exploration-header {
    display: flex; align-items: center; gap: var(--space-md);
    margin-bottom: var(--space-xl); padding-bottom: var(--space-md);
    border-bottom: 1px solid var(--border-primary);
  }
  .exploration-icon { display: flex; align-items: center; }
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
    font-size: var(--font-2xs);
  }
  .tech-detail-stat {
    color: var(--text-secondary);
    padding: 1px 4px;
    border-right: 1px solid var(--border-primary);
  }
  .tech-detail-stat:last-child { border-right: none; }
  .error-detail {
    color: var(--color-danger);
    font-size: var(--font-xs);
    padding: var(--space-sm) var(--space-md);
  }

  .payload-section {
    margin-top: var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .payload-toggle {
    width: 100%;
    display: flex; justify-content: space-between; align-items: center;
    padding: var(--space-lg) var(--space-2xl);
    background: var(--bg-secondary);
    cursor: pointer;
    font-size: var(--font-base);
    font-weight: 500;
    color: var(--text-primary);
    transition: background 0.2s ease;
    border: none;
    text-align: left;
  }
  .payload-toggle:hover { background: var(--border-primary); }
  .payload-toggle-label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-xs);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .toggle-icon { display: inline-flex; color: var(--text-secondary); flex-shrink: 0; }

  .payload-content {
    background: var(--code-bg);
    color: var(--code-fg);
    padding: var(--space-xl);
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--font-sm);
    line-height: 1.5;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 25rem;
  }

  .viz-result-card {
    background: var(--bg-surface);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .viz-result-card .assistant-message {
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 0 1rem 1rem 1rem;
  }
  .viz-result-card :global(.viz-chart-container) {
    width: 100% !important;
    max-width: 100%;
    overflow: hidden;
  }
  .viz-result-card :global(canvas) {
    max-width: 100% !important;
    width: 100% !important;
  }
  .viz-result-card :global(.results-card) {
    background: transparent !important;
    border: none !important;
    margin: 0 !important;
  }
  .viz-result-card :global(.results-toolbar) {
    background: transparent !important;
    border: none !important;
    padding: 0 !important;
    margin-bottom: 0 !important;
  }
  .viz-result-card :global(.results-details) { margin-top: 0 !important; }
  .viz-result-card :global(.results-details summary) {
    background: none !important;
    border-radius: 0 !important;
    padding-left: 1rem !important;
  }
  .viz-result-card :global(.explore-block) {
    margin: 0 !important;
    padding: 0 1rem;
  }

  @media print {
    @page { size: portrait; margin: 0.5in; }
    * { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .conversation-header .back-btn,
    .conversation-header :global(.export-btn-wrapper),
    .conversation-header button,
    .conversation-header .conversation-meta,
    .message-input-container,
    .collapsed-messages-banner { display: none !important; }
    .conversation-view { height: auto; overflow: visible; }
    .messages-container { overflow: visible; padding: 0; }
    .assistant-message { background: var(--bg-primary); border: 1px solid var(--border-primary); }
    .user-message { background: var(--color-accent-print, #0288d1); color: #fff; }
    :global(.results-details[open] .table-container) { max-height: none; overflow: visible; }
    :global(.results-card .collapsed-row) { display: table-row !important; }
  }
</style>
