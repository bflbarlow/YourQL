<script>
  let { 
    activeConversation, 
    conversationMessages = [], 
    llmProviders = [], 
    dbConnections = [],
    processingMessage = false, 
    messageError = null, 
    userMessage = '',
    showTechDetails = false,
    onSendMessage = () => {},
    onBack = () => {},
    onMessageChange = () => {},
    onTechDetailsToggle = () => {},
    onGearClick = () => {}
  } = $props()
  
  let localMessage = $state(userMessage)
  
  // Sync localMessage when parent clears userMessage (e.g., after send)
  $effect(() => {
    localMessage = userMessage
  })
  
  function handleInput(e) {
    localMessage = e.target.value
    onMessageChange(localMessage)
  }
  
  function handleKeyDown(e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (localMessage.trim()) {
        onSendMessage()
      }
    }
  }
  
  // Parse payload from metadata
  function parsePayload(metadata) {
    if (!metadata) return null
    try {
      return JSON.parse(metadata)
    } catch {
      return null
    }
  }

  // Enrich messages with parsed payloads
  let enrichedMessages = $derived(
    conversationMessages.map(m => ({
      ...m,
      payload: parsePayload(m.metadata)
    }))
  )

  // Filter messages based on tech details toggle
  let filteredMessages = $derived(
    showTechDetails
      ? enrichedMessages
      : enrichedMessages.filter(m => m.role === 'user' || m.role === 'assistant')
  )

  // Get exploration results for display
  let explorationResults = $derived(
    showTechDetails
      ? enrichedMessages.filter(m => m.role === 'exploration')
      : []
  )

  // State for payload toggles
  let showRequest = $state(false)
  let showResponse = $state(false)
  let showMessages = $state(false)
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
        <span class="meta-tag">{conversationMessages.length} messages</span>
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
    
    <button 
      class="gear-btn-header" 
      onclick={onGearClick}
      title="Conversation settings"
      type="button"
    >
      ⚙️
    </button>
  </div>
  
  <div class="messages-container">
    {#if filteredMessages.length === 0}
      <div class="empty-conversation">
        <p>No messages yet</p>
        <p class="hint">Start the conversation by typing a message below</p>
      </div>
    {:else}
      {#each filteredMessages as message (message.id)}
        <div class="message {message.role}">
          <div class="message-content">
            {#if message.role === 'user'}
              <div class="user-message">{message.content}</div>
            {:else if message.role === 'exploration'}
              <div class="exploration-result">
                <div class="exploration-header">
                  <span class="exploration-icon">🔍</span>
                  <span class="exploration-title">{message.content}</span>
                </div>
                
                {#if message.payload}
                  <div class="payload-section">
                    <div class="payload-toggle" onclick={() => showRequest = !showRequest}>
                      <span>📤 Request Payload</span>
                      <span class="toggle-icon">{showRequest ? '▼' : '▶'}</span>
                    </div>
                    {#if showRequest}
                      <pre class="payload-content">{JSON.stringify(message.payload.request_json, null, 2)}</pre>
                    {/if}
                  </div>
                  
                  <div class="payload-section">
                    <div class="payload-toggle" onclick={() => showResponse = !showResponse}>
                      <span>📥 Response Payload</span>
                      <span class="toggle-icon">{showResponse ? '▼' : '▶'}</span>
                    </div>
                    {#if showResponse}
                      <pre class="payload-content">{JSON.stringify(message.payload.response_json, null, 2)}</pre>
                    {/if}
                  </div>
                  
                  {#if message.payload.llm_messages}
                    <div class="payload-section">
                      <div class="payload-toggle" onclick={() => showMessages = !showMessages}>
                        <span>💬 LLM Messages ({message.payload.llm_messages.length})</span>
                        <span class="toggle-icon">{showMessages ? '▼' : '▶'}</span>
                      </div>
                      {#if showMessages}
                        <pre class="payload-content">{JSON.stringify(message.payload.llm_messages, null, 2)}</pre>
                      {/if}
                    </div>
                  {/if}
                {/if}
              </div>
            {:else}
              <div class="assistant-message">{@html message.content}</div>
            {/if}
          </div>
          <div class="message-time">
            {new Date(message.created_at).toLocaleTimeString()}
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
      
      {#if explorationResults.length > 0}
        <div class="exploration-summary">
          <strong>Exploration Summary:</strong> {explorationResults.length} intermediate query(ies) were run to explore the data before producing the final answer.
        </div>
      {/if}
    {/if}
    
    {#if processingMessage}
      <div class="message assistant">
        <div class="message-content">
          <div class="loading-indicator">
            <div class="loading-dots">
              <span></span>
              <span></span>
              <span></span>
            </div>
            <span>Processing...</span>
          </div>
        </div>
      </div>
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
    padding: 20px 30px;
    border-bottom: 1px solid #e0e0e0;
    display: flex;
    align-items: center;
    gap: 15px;
  }
  
  .tech-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    color: #666666;
    transition: all 0.2s ease;
  }
  
  .tech-toggle:hover {
    background: #e0e0e0;
    color: #000000;
  }
  
  .tech-toggle.active {
    background: #4fc3f7;
    color: #ffffff;
    border-color: #4fc3f7;
  }
  
  .tech-toggle.active:hover {
    background: #0288d1;
    border-color: #0288d1;
  }
  
  .gear-btn-header {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    cursor: pointer;
    font-size: 18px;
    transition: all 0.2s ease;
    flex-shrink: 0;
  }
  
  .gear-btn-header:hover {
    background: #e0e0e0;
    color: #000000;
  }
  
  .toggle-label {
    font-weight: 500;
  }
  
  .back-btn {
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    color: #666666;
    padding: 8px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.2s ease;
  }

  .back-btn:hover {
    background: #e0e0e0;
    color: #000000;
  }

  .conversation-info {
    flex: 1;
  }

  .conversation-info h3 {
    margin: 0 0 5px;
    font-size: 18px;
    font-weight: 600;
    color: #000000;
  }

  .conversation-meta {
    display: flex;
    gap: 8px;
  }

  .meta-tag {
    background: rgba(79, 195, 247, 0.1);
    color: #0288d1;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 12px;
  }

  .messages-container {
    flex: 1;
    overflow-y: auto;
    padding: 30px;
  }

  .empty-conversation {
    text-align: center;
    padding: 60px 20px;
    color: #cccccc;
  }

  .empty-conversation p {
    margin: 0 0 10px;
    font-size: 16px;
  }

  .empty-conversation .hint {
    font-size: 14px;
    color: #bbbbbb;
  }

  .message {
    margin-bottom: 20px;
    display: flex;
    flex-direction: column;
  }

  .message.user {
    align-items: flex-end;
  }

  .message.assistant {
    align-items: flex-start;
  }

  .message-content {
    max-width: 800px;
    width: 100%;
  }

  .user-message {
    background: #4fc3f7;
    color: #ffffff;
    padding: 12px 16px;
    border-radius: 12px 12px 0 12px;
    font-size: 14px;
    line-height: 1.5;
  }

  .assistant-message {
    background: #f9f9f9;
    color: #000000;
    padding: 16px 20px;
    border-radius: 12px;
    border: 1px solid #e0e0e0;
    font-size: 14px;
    line-height: 1.6;
  }

  .assistant-message pre {
    background: #1e1e1e;
    color: #d4d4d4;
    padding: 16px;
    border-radius: 8px;
    overflow-x: auto;
    font-family: 'Courier New', monospace;
    font-size: 13px;
    margin: 10px 0;
  }

  .assistant-message code {
    background: #f0f0f0;
    padding: 2px 6px;
    border-radius: 4px;
    font-family: 'Courier New', monospace;
    font-size: 13px;
  }

  .assistant-message table {
    border-collapse: collapse;
    width: 100%;
    margin: 10px 0;
  }

  .assistant-message th,
  .assistant-message td {
    border: 1px solid #e0e0e0;
    padding: 8px 12px;
    text-align: left;
  }

  .assistant-message th {
    background: #f5f5f5;
    font-weight: 600;
  }

  .message-time {
    font-size: 12px;
    color: #999999;
    margin-top: 5px;
  }

  .message.user .message-time {
    text-align: right;
  }

  .loading-indicator {
    display: flex;
    align-items: center;
    gap: 10px;
    color: #999999;
    font-size: 14px;
  }

  .loading-dots {
    display: flex;
    gap: 4px;
  }

  .loading-dots span {
    width: 8px;
    height: 8px;
    background: #4fc3f7;
    border-radius: 50%;
    animation: loading 1.4s infinite ease-in-out;
  }

  .loading-dots span:nth-child(1) {
    animation-delay: -0.32s;
  }

  .loading-dots span:nth-child(2) {
    animation-delay: -0.16s;
  }

  @keyframes loading {
    0%, 80%, 100% {
      transform: scale(0.6);
      opacity: 0.5;
    }
    40% {
      transform: scale(1);
      opacity: 1;
    }
  }

  .message-input-container {
    padding: 20px 30px;
    border-top: 1px solid #e0e0e0;
    background: #ffffff;
  }

  .error-message {
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid #ef5350;
    color: #ef5350;
    padding: 12px 16px;
    border-radius: 8px;
    font-size: 14px;
    margin-bottom: 15px;
  }

  .message-input-wrapper {
    display: flex;
    gap: 10px;
    align-items: flex-end;
  }

  .message-input-wrapper textarea {
    flex: 1;
    padding: 12px 16px;
    border: 1px solid #e0e0e0;
    border-radius: 12px;
    font-size: 14px;
    color: #000000;
    background: #f9f9f9;
    resize: none;
    font-family: inherit;
    line-height: 1.5;
    min-height: 44px;
    max-height: 200px;
    transition: border-color 0.2s ease;
  }

  .message-input-wrapper textarea:focus {
    outline: none;
    border-color: #4fc3f7;
    box-shadow: 0 0 0 3px rgba(79, 195, 247, 0.1);
  }

  .message-input-wrapper textarea::placeholder {
    color: #cccccc;
  }

  .send-btn {
    background: #4fc3f7;
    color: #ffffff;
    border: none;
    border-radius: 12px;
    width: 44px;
    height: 44px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .send-btn:hover:not(:disabled) {
    background: #0288d1;
  }

  .send-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  
  /* Exploration Results */
  .exploration-result {
    background: #f9f9f9;
    border: 1px solid #e0e0e0;
    border-radius: 12px;
    padding: 16px 20px;
  }
  
  .exploration-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    padding-bottom: 8px;
    border-bottom: 1px solid #e0e0e0;
  }
  
  .exploration-icon {
    font-size: 16px;
  }
  
  .exploration-title {
    font-weight: 600;
    color: #0288d1;
    font-size: 14px;
  }
  
  .exploration-sql {
    margin-bottom: 12px;
  }
  
  .exploration-sql strong {
    display: block;
    margin-bottom: 4px;
    color: #333333;
    font-size: 13px;
  }
  
  .exploration-sql pre {
    background: #1e1e1e;
    color: #d4d4d4;
    padding: 12px;
    border-radius: 8px;
    overflow-x: auto;
    font-family: 'Courier New', monospace;
    font-size: 12px;
    margin: 4px 0;
  }
  
  .exploration-results {
    margin-top: 12px;
  }
  
  .exploration-results strong {
    display: block;
    margin-bottom: 4px;
    color: #333333;
    font-size: 13px;
  }
  
  .exploration-results table {
    border-collapse: collapse;
    width: 100%;
    margin: 8px 0;
    font-size: 12px;
  }
  
  .exploration-results th,
  .exploration-results td {
    border: 1px solid #e0e0e0;
    padding: 6px 10px;
    text-align: left;
  }
  
  .exploration-results th {
    background: #f5f5f5;
    font-weight: 600;
  }
  
  .exploration-results td {
    background: #ffffff;
  }
  
  /* Technical Details */
  .tech-details {
    margin-top: 12px;
    padding: 12px;
    background: #f5f5f5;
    border: 1px solid #e0e0e0;
    border-radius: 8px;
  }
  
  .tech-details-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
    font-size: 12px;
    color: #666666;
    font-weight: 500;
  }
  
  .copy-btn {
    padding: 4px 8px;
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: 4px;
    font-size: 11px;
    cursor: pointer;
    color: #666666;
    transition: all 0.2s ease;
  }
  
  .copy-btn:hover {
    background: #e0e0e0;
    color: #000000;
  }
  
  .tech-details pre {
    background: #1e1e1e;
    color: #d4d4d4;
    padding: 12px;
    border-radius: 6px;
    overflow-x: auto;
    font-family: 'Courier New', monospace;
    font-size: 11px;
    line-height: 1.4;
    margin: 0;
  }
  
  /* Exploration Summary */
  .exploration-summary {
    margin: 20px 0;
    padding: 12px 16px;
    background: rgba(79, 195, 247, 0.05);
    border: 1px solid rgba(79, 195, 247, 0.2);
    border-radius: 8px;
    font-size: 13px;
    color: #0288d1;
  }
  
  .exploration-summary strong {
    color: #0288d1;
  }
  
  /* Payload Sections */
  .payload-section {
    margin-top: 12px;
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    overflow: hidden;
  }
  
  .payload-toggle {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 14px;
    background: #f5f5f5;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    color: #333333;
    transition: background 0.2s ease;
  }
  
  .payload-toggle:hover {
    background: #e0e0e0;
  }
  
  .toggle-icon {
    font-size: 12px;
    color: #666666;
  }
  
  .payload-content {
    background: #1e1e1e;
    color: #d4d4d4;
    padding: 12px;
    margin: 0;
    font-family: 'Courier New', monospace;
    font-size: 12px;
    line-height: 1.5;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
