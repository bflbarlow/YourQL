<script>
  // Defaults tab — extracted verbatim from SettingsView. Self-contained:
  // loads and saves Discussion Defaults on its own.
  import { GetDiscussionDefaults, UpdateDiscussionDefaults } from '../../../../wailsjs/go/main/App.js'

  let { llmProviders = [], dataSources = [] } = $props()

  let defaultsForm = $state({
    llm_provider_id: null,
    data_source_id: null,
    max_context_messages: 5,
    max_messages: 0,
    summarize: false,
    viz_enabled: true,
    tech_details: false,
    context_details: false,
    streaming_enabled: false
  })

  loadDefaults()

  async function loadDefaults() {
    try {
      const d = await GetDiscussionDefaults()
      if (d) {
        defaultsForm.llm_provider_id = d.llm_provider_id ?? null
        defaultsForm.data_source_id = d.data_source_id ?? null
        defaultsForm.max_context_messages = d.max_context_messages ?? 5
        defaultsForm.max_messages = d.max_messages ?? 0
        defaultsForm.summarize = d.summarize ?? false
        defaultsForm.viz_enabled = d.viz_enabled ?? true
        defaultsForm.tech_details = d.tech_details ?? false
        defaultsForm.context_details = d.context_details ?? false
        defaultsForm.streaming_enabled = d.streaming_enabled ?? false
      }
    } catch (e) {
      console.error('Failed to load defaults:', e)
    }
  }

  async function saveDefaults() {
    try {
      await UpdateDiscussionDefaults(defaultsForm)
    } catch (e) {
      console.error('Failed to save defaults:', e)
    }
  }

  async function resetDefaults() {
    defaultsForm.llm_provider_id = null
    defaultsForm.data_source_id = null
    defaultsForm.max_context_messages = 5
    defaultsForm.max_messages = 0
    defaultsForm.summarize = false
    defaultsForm.viz_enabled = true
    defaultsForm.tech_details = false
    defaultsForm.context_details = false
    defaultsForm.streaming_enabled = false
    await saveDefaults()
  }
</script>

<div class="settings-section">
  <h3>Defaults for New Discussions</h3>
  <p class="section-desc">Configure the default settings applied when you create a new discussion.</p>

  <div class="form-card">
    <h4>Provider & Source</h4>
    <div class="form-group">
      <label for="defaults-llm">Default LLM Provider</label>
      <select id="defaults-llm" bind:value={defaultsForm.llm_provider_id} onchange={saveDefaults}>
        <option value={null}>None (ask each time)</option>
        {#each llmProviders as p}
          <option value={p.id}>{p.name}</option>
        {/each}
      </select>
    </div>
    <div class="form-group">
      <label for="defaults-ds">Default Data Source</label>
      <select id="defaults-ds" bind:value={defaultsForm.data_source_id} onchange={saveDefaults}>
        <option value={null}>None (ask each time)</option>
        {#each dataSources as ds}
          <option value={ds.id}>{ds.name}</option>
        {/each}
      </select>
    </div>
  </div>

  <div class="form-card">
    <h4>Conversation Behavior</h4>
    <div class="form-group">
      <label for="defaults-max-ctx">Messages in Context</label>
      <input type="number" id="defaults-max-ctx" min="1" max="15" bind:value={defaultsForm.max_context_messages} onchange={saveDefaults} />
      <p class="hint">How many recent messages to send to the LLM (max 15). Default: 5. Higher values risk context-window exhaustion and empty responses.</p>
    </div>
    <div class="form-group">
      <label for="defaults-max-msgs">Total Messages (0 = unlimited)</label>
      <input type="number" id="defaults-max-msgs" min="0" max="1000" bind:value={defaultsForm.max_messages} onchange={saveDefaults} />
      <p class="hint">Maximum messages stored in the conversation history.</p>
    </div>
  </div>

  <div class="form-card">
    <h4>Toggles</h4>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" bind:checked={defaultsForm.tech_details} onchange={saveDefaults} />
        SHOW TECHNICAL DETAILS
      </label>
    </div>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" bind:checked={defaultsForm.context_details} onchange={saveDefaults} />
        Show context &amp; token details
      </label>
    </div>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" bind:checked={defaultsForm.summarize} onchange={saveDefaults} />
        Summarize results
      </label>
      <p class="hint">LLM summarizes query results as a plain-English answer</p>
    </div>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" bind:checked={defaultsForm.viz_enabled} onchange={saveDefaults} />
        Data visualization
      </label>
      <p class="hint">LLM generates charts (bar, line, pie, scatter) when appropriate</p>
    </div>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" bind:checked={defaultsForm.streaming_enabled} onchange={saveDefaults} />
        Stream LLM output
      </label>
      <p class="hint">Show model output character-by-character in real time</p>
    </div>
  </div>

  <button class="btn btn-secondary" onclick={resetDefaults}>
    Reset to System Defaults
  </button>
</div>
