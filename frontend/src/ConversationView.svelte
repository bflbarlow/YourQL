<script>
  import { Settings, Pin, Search, ChevronRight, ChevronDown, MessageSquare } from 'lucide-svelte'
  let { 
    activeConversation, 
    conversationMessages = [], 
    llmProviders = [], 
    dbConnections = [],
    processingMessage = '', 
    messageError = null, 
    userMessage = '',
    showTechDetails = false,
    showContextDetails = false,
    maxMessages = 0,
    onSendMessage = () => {},
    onBack = () => {},
    onMessageChange = () => {},
    onTechDetailsToggle = () => {},
    onGearClick = () => {},
    onMaxMessagesChange = () => {}
  } = $props()
  
  let localMessage = $state(userMessage)
  
  $effect(() => {
    localMessage = userMessage
  })
  
  function handleInput(e) {
    localMessage = e.target.value
    onMessageChange(localMessage)
    // Auto-resize textarea
    const el = e.target
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }
  
  function handleKeyDown(e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (localMessage.trim()) {
        onSendMessage()
      }
    }
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
        : enrichedMessages.filter(m => m.role === 'user' || m.role === 'assistant')
      
      // Apply max_messages limit — show last N messages
      if (maxMessages > 0 && msgs.length > maxMessages) {
        return msgs.slice(msgs.length - maxMessages)
      }
      return msgs
    })()
  )

  // Per-message payload toggles (§4.7)
  let payloadToggles = $state({})

  // Token counting from message payloads
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
        {#if activeConversation?.db_connection_id}
          <span class="meta-tag">{dbConnections.find(c => c.id === activeConversation.db_connection_id)?.name || 'DB'}</span>
        {/if}
        {#if showContextDetails && tokenSummary.msgCount > 0}
          <span class="meta-tag context-tag">
            {fmtTokens(tokenSummary.promptTotal)}&uarr; {fmtTokens(tokenSummary.completionTotal)}&darr; tokens
          </span>
          <span class="meta-tag context-tag">
            {conversationMessages.length} msg{conversationMessages.length !== 1 ? 's' : ''}
          </span>
        {/if}
      </div>
    </div>
    <button 
      class="tech-toggle {showTechDetails ? 'active' : ''}" 
      onclick={onTechDetailsToggle}
      title="Show technical details"
    >
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <polyline points="16 18 22 12 16 6"></polyline>
        <polyline points="8 6 2 12 8 18"></polyline>
      </svg>
      <span class="toggle-label">Tech</span>
    </button>
    
    <button class="gear-btn-header" onclick={onGearClick} title="Conversation settings" type="button"><Settings size={16} /></button>
  </div>
  
  <div class="messages-container" bind:this={messagesEl}>
    {#if maxMessages > 0 && conversationMessages.length > maxMessages}
      <div class="collapsed-messages-banner">
        <span><Pin size={14} class="pin-icon" /> {conversationMessages.length - maxMessages} older message(s) hidden</span>
        <button class="show-all-btn" onclick={() => onMaxMessagesChange(0)}>Show all</button>
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
            {:else if message.role === 'exploration'}
              <div class="exploration-result">
                <div class="exploration-header">
                  <span class="exploration-icon"><Search size={14} /></span>
                  <span class="exploration-title">{message.content}</span>
                </div>
                
                {#if message.payload}
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
              </div>
            {:else}
              <!-- §4.8: assistant messages with HTML -->
              <div class="assistant-message">{@html message.content}</div>
            {/if}
          </div>
          <div class="message-time">
            {formatTime(message.created_at)}
          </div>
          {#if showTechDetails && message.llm_content}
            <div class="tech-details">
              <div class="tech-details-header">
                <span>Raw LLM Response</span>
                <button class="copy-btn" onclick={() => navigator.clipboard.writeText(message.llm_content)}>Copy</button>
              </div>
              <pre>{message.llm_content}</pre>
            </div>
          {/if}
        </div>
      {/each}
      
      {#if processingMessage}
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
    {#if messageError}
      <div class="error-message">{messageError}</div>
    {/if}
    
    <div class="message-input-wrapper">
      <textarea
        value={localMessage}
        placeholder="Type your message..."
        oninput={handleInput}
        onkeydown={handleKeyDown}
        rows="1"
      ></textarea>
      <button 
        class="send-btn" 
        onclick={onSendMessage}
        disabled={processingMessage || !localMessage.trim()}
      >
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="22" y1="2" x2="11" y2="13"></line>
          <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
        </svg>
      </button>
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
  
  .tech-toggle {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    padding: var(--space-md) var(--space-xl);
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-base);
    color: #666666;
    transition: all 0.2s ease;
  }
  
  .tech-toggle:hover {
    background: #e0e0e0;
    color: #000000;
  }
  
  .tech-toggle.active {
    background: #0288d1;
    color: #ffffff;
    border-color: #0288d1;
  }
  
  .gear-btn-header {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.25rem;
    height: 2.25rem;
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-2xl);
    transition: all 0.2s ease;
    flex-shrink: 0;
  }
  
  .gear-btn-header:hover { background: #e0e0e0; color: #000000; }
  
  .back-btn {
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    color: #666666;
    padding: var(--space-md) var(--space-xl);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: var(--font-md);
    transition: all 0.2s ease;
  }

  .back-btn:hover { background: #e0e0e0; color: #000000; }

  .conversation-info { flex: 1; }
  .conversation-info h3 { margin: 0 0 0.3125rem; font-size: var(--font-2xl); font-weight: 600; color: #000000; }

  .conversation-meta { display: flex; gap: var(--space-md); }

  .meta-tag {
    background: rgba(2, 136, 209, 0.1);
    color: #0288d1;
    padding: var(--space-2xs) var(--space-md);
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
  }
  
  .context-tag {
    background: rgba(102, 102, 102, 0.08);
    color: #666666;
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
    color: #cccccc;
  }

  .empty-conversation p { margin: 0 0 var(--space-lg); font-size: var(--font-xl); }
  .empty-conversation .hint { font-size: var(--font-md); color: #bbbbbb; }

  .collapsed-messages-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-lg) var(--space-3xl);
    background: #f0f7ff;
    border: 1px solid #d0e8ff;
    border-radius: var(--radius-md);
    margin-bottom: var(--space-3xl);
    font-size: var(--font-base);
    color: #0288d1;
  }

  .show-all-btn {
    padding: var(--space-xs) var(--space-xl);
    background: #0288d1;
    color: #ffffff;
    border: none;
    border-radius: var(--radius-md);
    font-size: var(--font-sm);
    cursor: pointer;
    transition: background 0.2s ease;
  }

  .show-all-btn:hover {
    background: #0288d1;
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
    background: #0288d1;
    color: #ffffff;
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    line-height: 1.5;
  }

  .assistant-message {
    background: #f9f9f9;
    color: #000000;
    padding: var(--space-3xl) var(--space-4xl);
    border-radius: var(--radius-md);
    border: 1px solid #e0e0e0;
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
    background: #f0f0f0;
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-md);
    font-family: 'Courier New', monospace;
    font-size: var(--font-base);
  }

  .assistant-message table { border-collapse: collapse; width: 100%; margin: var(--space-lg) 0; }
  .assistant-message th, .assistant-message td { border: 1px solid #e0e0e0; padding: var(--space-md) var(--space-xl); text-align: left; }
  .assistant-message th { background: #f5f5f5; font-weight: 600; }

  .message-time { font-size: var(--font-sm); color: #999999; margin-top: 0.3125rem; }
  .message.user .message-time { text-align: right; }

  .loading-indicator {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
    color: #999999;
    font-size: var(--font-md);
  }

  .loading-dots { display: flex; gap: var(--space-xs); }
  .loading-dots span {
    width: var(--space-md); height: var(--space-md);
    background: #0288d1;
    border-radius: var(--radius-md);
    animation: loading 1.4s infinite ease-in-out;
  }
  .loading-dots span:nth-child(1) { animation-delay: -0.32s; }
  .loading-dots span:nth-child(2) { animation-delay: -0.16s; }

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

  .message-input-container { padding: var(--space-4xl) var(--space-6xl); border-top: 1px solid #e0e0e0; background: #ffffff; }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid #ef5350;
    color: #ef5350;
    padding: var(--space-xl) var(--space-3xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    margin-bottom: var(--space-2xl);
  }

  .message-input-wrapper { display: flex; gap: var(--space-lg); align-items: flex-end; }

  .message-input-wrapper textarea {
    flex: 1;
    padding: var(--space-xl) var(--space-3xl);
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: #000000;
    background: #f9f9f9;
    resize: none;
    font-family: inherit;
    line-height: 1.5;
    min-height: 2.75rem;
    max-height: 12.5rem;
    transition: border-color 0.2s ease;
  }

  .message-input-wrapper textarea:focus {
    outline: none;
    border-color: #0288d1;
    border: 2px solid #0288d1;
  }

  .message-input-wrapper textarea::placeholder { color: #cccccc; }

  .send-btn {
    background: #0288d1;
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

  .send-btn:hover:not(:disabled) { background: #0288d1; }
  .send-btn:disabled { opacity: 0.6; cursor: not-allowed; }
  
  .exploration-result {
    background: #f9f9f9;
    border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
    padding: var(--space-3xl) var(--space-4xl);
  }

  .sort-header {
    cursor: pointer;
    transition: background 0.2s ease;
  }

  .sort-header:hover {
    background: #e8f0fe;
  }

  .sort-indicator {
    font-size: var(--font-2xs);
    margin-left: var(--space-xs);
    color: #ccc;
  }
  
  .exploration-header {
    display: flex; align-items: center; gap: var(--space-md);
    margin-bottom: var(--space-xl); padding-bottom: var(--space-md);
    border-bottom: 1px solid #e0e0e0;
  }
  
  .exploration-icon { font-size: var(--font-xl); }
  .exploration-title { font-weight: 600; color: #0288d1; font-size: var(--font-md); }
  
  .tech-details {
    margin-top: var(--space-xl); padding: var(--space-xl);
    background: #f5f5f5; border: 1px solid #e0e0e0;
    border-radius: var(--radius-md);
  }
  
  .tech-details-header {
    display: flex; justify-content: space-between; align-items: center;
    margin-bottom: var(--space-md); font-size: var(--font-sm); color: #666666; font-weight: 500;
  }
  
  .copy-btn {
    padding: var(--space-xs) var(--space-md); background: #ffffff; border: 1px solid #e0e0e0;
    border-radius: var(--radius-md); font-size: var(--font-xs); cursor: pointer;
    color: #666666; transition: all 0.2s ease;
  }
  .copy-btn:hover { background: #e0e0e0; color: #000000; }
  
  .tech-details pre {
    background: #1e1e1e; color: #d4d4d4; padding: var(--space-xl);
    border-radius: var(--radius-md); overflow-x: auto;
    font-family: 'Courier New', monospace; font-size: var(--font-xs);
    line-height: 1.4; margin: 0;
  }
  
  .payload-section {
    margin-top: var(--space-xl); border: 1px solid #e0e0e0;
    border-radius: var(--radius-md); overflow: hidden;
  }
  
  .payload-toggle {
    display: flex; justify-content: space-between; align-items: center;
    padding: var(--space-lg) var(--space-2xl); background: #f5f5f5;
    cursor: pointer; font-size: var(--font-base); font-weight: 500;
    color: #333333; transition: background 0.2s ease;
  }
  .payload-toggle:hover { background: #e0e0e0; }
  .toggle-icon { font-size: var(--font-sm); color: #666666; }
  
  .payload-content {
    background: #1e1e1e; color: #d4d4d4;
    padding: var(--space-xl) margin: 0;
    font-family: 'Courier New', monospace; font-size: var(--font-sm);
    line-height: 1.5; overflow-x: auto;
    white-space: pre-wrap; word-break: break-all;
    max-height: 25rem;
  }
</style>
