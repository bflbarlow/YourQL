<script>
  // Models tab — extracted verbatim from SettingsView (state + handlers + markup).
  import {
    CreateLLMProvider, UpdateLLMProvider, DeleteLLMProvider,
    SetDefaultLLMProvider, TestLLMProviderConnection, DetectModelMaxTokens
  } from '../../../../wailsjs/go/main/App.js'

  import { confirm } from '../../../lib/confirm.svelte.js'

  let { llmProviders = [], onUpdate = () => {} } = $props()

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
  let llmStatus = $state('')

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
    const ok = await confirm({
      title: 'Delete Provider',
      body: 'Are you sure you want to delete this provider?',
      confirmLabel: 'Delete',
      danger: true
    })
    if (!ok) return
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

  async function handleTestLLM(id) {
    llmStatus = 'Testing connection...'
    try {
      const result = await TestLLMProviderConnection(id)
      llmStatus = result
    } catch (e) {
      llmStatus = 'Error: ' + e.toString()
    }
  }
</script>

<div class="settings-section">
  {#if showLLMDetail}
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
              <label for="llm-name">Name <span class="required">*</span></label>
              <input type="text" id="llm-name" bind:value={llmForm.name} placeholder="My GPT-4" />
            </div>
            <div class="form-group">
              <label for="llm-provider">Provider</label>
              <select id="llm-provider" bind:value={llmForm.provider} disabled={!isNewLLM}>
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
                <option value="ollama">Ollama</option>
                <option value="local">Local</option>
              </select>
            </div>
            <div class="form-group">
              <label for="llm-model">Model <span class="required">*</span></label>
              <input type="text" id="llm-model" bind:value={llmForm.model} placeholder="gpt-4-turbo" />
            </div>
            <div class="form-group">
              <label for="llm-max-tokens">Max Tokens</label>
              <div class="input-with-detect">
                <input type="number" id="llm-max-tokens" bind:value={llmForm.maxTokens} placeholder="4096" min="1" max={editingLLMProvider?.contextWindow > 0 ? editingLLMProvider.contextWindow : undefined} />
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
              <label for="llm-base-url">Base URL (optional)</label>
              <input type="text" id="llm-base-url" bind:value={llmForm.baseURL} placeholder="https://api.openai.com" />
            </div>
            <div class="form-group">
              <label for="llm-api-key">API Key</label>
              <input type="password" id="llm-api-key" bind:value={llmForm.apiKey} placeholder={isNewLLM ? 'sk-...' : 'sk-... (leave blank to keep current)'} />
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
            <button class="btn btn-small" onclick={() => handleTestLLM(editingLLMProvider.id)}>Test Connection</button>
          {/if}
          <button class="btn btn-secondary" onclick={closeLLMDetail}>Cancel</button>
        </div>
      </div>
    </div>
  {:else}
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
