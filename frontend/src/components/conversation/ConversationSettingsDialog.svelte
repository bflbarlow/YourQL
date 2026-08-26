<script>
  // Conversation Settings dialog — extracted from App.svelte's gear popover.
  // Owns all per-conversation setting handlers; emits onclose(changed).
  import { X, Copy, Trash2 } from 'lucide-svelte'
  import Modal from '../ui/Modal.svelte'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import {
    UpdateConversationSettings, UpdateConversationTitle,
    UpdateConversationMaxMessages, UpdateConversationMaxContextMessages,
    UpdateConversationPinned, UpdateConversationTechDetails,
    UpdateConversationContextDetails, UpdateConversationSummarize,
    UpdateConversationVizEnabled, UpdateConversationStreamingEnabled,
    SetConversationSkill, GetConversationSkillIDs, ListSkills,
    AddTagToConversation, RemoveTagFromConversation,
    GetTagsForConversation, ListAllTags,
    DuplicateConversation, ClearConversationMessages,
    ArchiveConversation, DeleteConversation, RestoreConversation
  } from '../../../wailsjs/go/main/App.js'

  let {
    conversation = null,     // reactive copy — parent passes selectedConversation
    allSkills = [],
    llmProviders = [],
    dataSources = [],
    open = false,
    onclose = () => {},      // onclose(changed: boolean)
    ondeleted = () => {}     // fired after successful delete
  } = $props()

  let conv = $state(null)
  let conversationSkillIDs = $state([])
  let convTags = $state([])
  let tagInput = $state('')
  let allTags = $state([])
  let clearConfirmOpen = $state(false)

  // Mirror the passed conversation into local state so edits are reactive.
  $effect(() => {
    if (conversation && open) {
      conv = { ...conversation }
    }
  })

  $effect(() => {
    if (open && conversation?.id) {
      loadSkills()
      loadTags()
    }
  })

  async function loadSkills() {
    try {
      conversationSkillIDs = await GetConversationSkillIDs(conversation.id) || []
    } catch { conversationSkillIDs = [] }
  }

  async function loadTags() {
    try {
      convTags = await GetTagsForConversation(conversation.id) || []
      allTags = await ListAllTags() || []
    } catch (e) { console.error('Failed to load tags:', e) }
  }

  let tagSuggestions = $derived(
    (allTags || []).filter(t =>
      t.toLowerCase().includes((tagInput || '').toLowerCase()) &&
      !(convTags || []).includes(t)
    ).slice(0, 8)
  )

  function changed() { /* parent refreshes via loadData() on close */ }

  async function updateSetting(fn, ...args) {
    try {
      await fn(...args)
    } catch (e) {
      console.error('Failed to update conversation setting:', e)
    }
  }

  function handleProviderChange(e) {
    const val = e.target.value ? parseInt(e.target.value) : null
    updateSetting(UpdateConversationSettings, conv.id, val, conv.data_source_id || null)
    conv.llm_provider_id = val
  }
  function handleSourceChange(e) {
    const val = e.target.value ? parseInt(e.target.value) : null
    updateSetting(UpdateConversationSettings, conv.id, conv.llm_provider_id || null, val)
    conv.data_source_id = val
  }

  function handleRenameKeydown(e) {
    if (e.key === 'Enter') {
      saveRename()
      e.target.blur()
    }
  }
  function saveRename() {
    if (!conv?.title?.trim()) return
    updateSetting(UpdateConversationTitle, conv.id, conv.title.trim())
  }

  function setMaxMessages(val) {
    updateSetting(UpdateConversationMaxMessages, conv.id, val)
    conv.max_messages = val
  }
  function handleMaxMessagesInput(e) {
    const v = parseInt(e.target.value)
    if (v >= 1 && v <= 500) conv.max_messages = v
  }
  function handleMaxMessagesBlur() {
    setMaxMessages(conv.max_messages || 0)
  }

  function setMaxContext(val) {
    updateSetting(UpdateConversationMaxContextMessages, conv.id, val)
    conv.max_context_messages = val
  }
  function handleMaxContextInput(e) {
    const v = parseInt(e.target.value)
    if (v >= 1 && v <= 15) conv.max_context_messages = v
  }
  function handleMaxContextBlur() {
    setMaxContext(conv.max_context_messages || 0)
  }

  function togglePinned(e) {
    updateSetting(UpdateConversationPinned, conv.id, e.target.checked)
    conv.pinned = e.target.checked
  }
  function toggleTechDetails(e) {
    updateSetting(UpdateConversationTechDetails, conv.id, e.target.checked)
    conv.tech_details = e.target.checked
  }
  function toggleContextDetails(e) {
    updateSetting(UpdateConversationContextDetails, conv.id, e.target.checked)
    conv.context_details = e.target.checked
  }
  function toggleSummarize(e) {
    updateSetting(UpdateConversationSummarize, conv.id, e.target.checked)
    conv.summarize = e.target.checked
  }
  function toggleViz(e) {
    updateSetting(UpdateConversationVizEnabled, conv.id, e.target.checked)
    conv.viz_enabled = e.target.checked
  }
  function toggleStreaming(e) {
    updateSetting(UpdateConversationStreamingEnabled, conv.id, e.target.checked)
    conv.streaming_enabled = e.target.checked
  }
  function toggleSkill(skillId, e) {
    updateSetting(SetConversationSkill, conv.id, skillId, e.target.checked)
  }

  // ── Tags ──
  async function addTag(name) {
    const val = (name || '').trim()
    if (!val) return
    try {
      await AddTagToConversation(conv.id, val)
      convTags = await GetTagsForConversation(conv.id) || []
      allTags = await ListAllTags() || []
      tagInput = ''
    } catch (e) { console.error('Failed to add tag:', e) }
  }
  async function removeTag(name) {
    try {
      await RemoveTagFromConversation(conv.id, name)
      convTags = await GetTagsForConversation(conv.id) || []
      allTags = await ListAllTags() || []
    } catch (e) { console.error('Failed to remove tag:', e) }
  }
  function handleTagKeydown(e) {
    if (e.key === 'Enter') {
      e.preventDefault()
      const val = tagInput.trim()
      if (!val) return
      const match = tagSuggestions.find(t => t.toLowerCase() === val.toLowerCase())
      addTag(match || val)
    }
  }

  // ── Actions ──
  async function duplicate() {
    try {
      const dup = await DuplicateConversation(conv.id)
      onclose(false, dup)
    } catch (e) { console.error('Failed to duplicate:', e) }
  }
  function requestClear() { clearConfirmOpen = true }
  async function confirmClear() {
    try {
      await ClearConversationMessages(conv.id)
      clearConfirmOpen = false
      onclose(true)
    } catch (e) { console.error('Failed to clear messages:', e) }
  }
  async function archive() {
    try {
      await ArchiveConversation(conv.id)
      onclose(true)
    } catch (e) { console.error('Failed to archive:', e) }
  }
  async function restore() {
    try {
      await RestoreConversation(conv.id)
      onclose(true)
    } catch (e) { console.error('Failed to restore:', e) }
  }
  async function doDelete() {
    try {
      await DeleteConversation(conv.id)
      ondeleted(conv.id)
      onclose(true)
    } catch (e) { console.error('Failed to delete:', e) }
  }
</script>

<Modal open={open && !!conv} title={conv ? `Settings for “${conv.title}”` : ''} width="lg" onclose={() => onclose(false)}>
  {#if conv}
    <div class="cs-body">
      <!-- LLM Provider -->
      <section>
        <label class="field-label" for="cs-provider">LLM Provider</label>
        <select id="cs-provider" value={conv.llm_provider_id || ''} onchange={handleProviderChange}>
          <option value="">(none)</option>
          {#each llmProviders as provider}
            <option value={provider.id}>{provider.name}</option>
          {/each}
        </select>
      </section>

      <!-- DB Connection -->
      <section>
        <label class="field-label" for="cs-source">DB Connection</label>
        <select id="cs-source" value={conv.data_source_id || ''} onchange={handleSourceChange}>
          <option value="">(none)</option>
          {#each dataSources as conn}
            <option value={conn.id}>{conn.name}</option>
          {/each}
        </select>
      </section>

      <!-- Rename -->
      <section>
        <label class="field-label" for="cs-rename">Rename</label>
        <input id="cs-rename" type="text" value={conv.title || ''} placeholder="Enter new name..."
          oninput={(e) => conv.title = e.target.value}
          onkeydown={handleRenameKeydown}
          onblur={saveRename} />
      </section>

      <!-- Message limits -->
      <section>
        <span class="field-label">Visible Messages</span>
        <div class="limit-group">
          <button class="msg-limit-btn" class:active={conv.max_messages === 0} onclick={() => setMaxMessages(0)} type="button">Show All</button>
          <input type="number" value={conv.max_messages || ''} placeholder="e.g. 50" min="1" max="500"
            oninput={handleMaxMessagesInput} onblur={handleMaxMessagesBlur} />
        </div>
      </section>

      <section>
        <span class="field-label">Messages in LLM Context</span>
        <div class="limit-group">
          <button class="msg-limit-btn" class:active={conv.max_context_messages === 15} onclick={() => setMaxContext(15)} type="button">Max 15</button>
          <input type="number" value={conv.max_context_messages || ''} placeholder="e.g. 5" min="1" max="15"
            oninput={handleMaxContextInput} onblur={handleMaxContextBlur} />
        </div>
        <p class="hint">Default: 5 messages. Hard cap: 15. Large context windows can cause empty responses.</p>
      </section>

      <!-- Checkboxes -->
      <section class="checks">
        <label><input type="checkbox" checked={conv.pinned === true} onchange={togglePinned} /> Pin to top of list</label>
        <label><input type="checkbox" checked={conv.tech_details === true} onchange={toggleTechDetails} /> Show technical details</label>
        <label><input type="checkbox" checked={conv.context_details === true} onchange={toggleContextDetails} /> Show context &amp; token details</label>
        <label>
          <input type="checkbox" checked={conv.summarize === true} onchange={toggleSummarize} /> Summarize results
        </label>
        <p class="hint indent">LLM summarizes query results as a plain-English answer</p>
        <label>
          <input type="checkbox" checked={conv.viz_enabled !== false} onchange={toggleViz} /> Data visualization
        </label>
        <p class="hint indent">LLM generates charts (bar, line, pie, scatter) when appropriate</p>
        <label>
          <input type="checkbox" checked={conv.streaming_enabled === true} onchange={toggleStreaming} /> Stream LLM output
        </label>
        <p class="hint indent">Show model output character-by-character in real time</p>
      </section>

      <!-- Skills -->
      <section>
        <span class="section-title">Skills</span>
        {#each allSkills as skill (skill.id)}
          <label class="skill-row">
            <input type="checkbox"
                   checked={conversationSkillIDs.includes(skill.id)}
                   disabled={!skill.is_active}
                   onchange={(e) => toggleSkill(skill.id, e)} />
            {skill.name}
          </label>
        {/each}
        {#if allSkills.length === 0}
          <p class="hint">No skills configured. Add skills in Settings.</p>
        {/if}
      </section>

      <hr />

      <!-- Tags -->
      <section>
        <span class="section-title">Tags</span>
        <div class="tag-chips">
          {#each convTags as tag}
            <span class="chip">
              {tag}
              <button onclick={() => removeTag(tag)} title="Remove tag" aria-label="Remove tag {tag}" type="button">&times;</button>
            </span>
          {/each}
        </div>
        <div class="tag-input-wrapper">
          <input type="text" placeholder="Add a tag…" bind:value={tagInput} onkeydown={handleTagKeydown} />
          {#if tagSuggestions.length > 0}
            <div class="tag-suggestions">
              {#each tagSuggestions as suggestion}
                <button type="button" onclick={() => addTag(suggestion)}>{suggestion}</button>
              {/each}
            </div>
          {/if}
        </div>
      </section>

      <hr />

      <!-- Actions -->
      <section class="actions">
        <button class="action-btn" onclick={duplicate} title="Duplicate" type="button"><Copy size={14} /> Duplicate</button>
        <button class="action-btn danger-ghost" onclick={requestClear} title="Clear messages" type="button"><Trash2 size={14} /> Clear</button>
        {#if conv.status === 'archived'}
          <button class="action-btn success-ghost" onclick={restore} type="button">Restore</button>
        {/if}
        <button class="action-btn danger-ghost" onclick={archive} type="button">Archive</button>
        <button class="action-btn danger-solid" onclick={doDelete} type="button">Delete</button>
      </section>
    </div>
  {/if}
</Modal>

<ConfirmDialog
  open={clearConfirmOpen}
  title="Clear Messages"
  body="Clear all messages in this conversation? The conversation itself will remain."
  confirmLabel="Clear Messages"
  onconfirm={confirmClear}
  onclose={() => clearConfirmOpen = false}
/>

<style>
  .cs-body { display: flex; flex-direction: column; gap: var(--space-xl); }
  section { padding: var(--space-md); }
  hr { border: none; border-top: 1px solid var(--border-secondary); margin: 0; }

  .field-label {
    display: block;
    font-size: var(--font-xs);
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: var(--space-xs);
  }
  .section-title {
    display: block;
    font-weight: 600;
    font-size: var(--font-sm);
    margin-bottom: var(--space-sm);
    color: var(--text-secondary);
  }

  section select,
  section input[type="text"],
  .limit-group input[type="number"] {
    width: 100%;
    padding: var(--space-md) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    color: var(--text-primary);
    background: var(--bg-input);
    box-sizing: border-box;
    transition: border-color 0.15s ease;
  }
  section select:focus,
  section input:focus { outline: none; border-color: var(--color-accent); }

  .limit-group {
    display: flex;
    gap: var(--space-md);
    align-items: center;
  }
  .msg-limit-btn {
    padding: var(--space-md) var(--space-2xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-secondary);
    font-size: var(--font-base);
    cursor: pointer;
    transition: all 0.2s ease;
    white-space: nowrap;
  }
  .msg-limit-btn:hover { background: var(--bg-secondary); }
  .msg-limit-btn.active {
    background: var(--color-accent);
    color: #ffffff;
    border-color: var(--color-accent);
  }
  .limit-group input[type="number"] { flex: 1; width: auto; }

  .checks label,
  .skill-row {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    font-size: var(--font-base);
    color: var(--text-secondary);
    cursor: pointer;
    padding: var(--space-xs) 0;
  }
  .checks input[type="checkbox"], .skill-row input[type="checkbox"] {
    width: var(--space-3xl);
    height: var(--space-3xl);
    accent-color: var(--color-accent);
  }
  .indent { margin-left: var(--space-3xl); }

  .hint {
    color: var(--text-tertiary);
    font-size: var(--font-xs);
    margin: var(--space-2xs) 0 0;
  }

  .tag-chips { display: flex; flex-wrap: wrap; gap: var(--space-xs); }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2xs);
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-sm);
    font-size: var(--font-xs);
  }
  .chip button {
    background: none; border: none; color: var(--color-accent);
    cursor: pointer; padding: 0; line-height: 1; opacity: 0.6;
  }
  .chip button:hover { opacity: 1; }

  .tag-input-wrapper { position: relative; margin-top: var(--space-sm); }
  .tag-suggestions {
    position: absolute;
    top: 100%; left: 0; right: 0;
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-top: none;
    border-radius: 0 0 var(--radius-md) var(--radius-md);
    z-index: calc(var(--z-modal) + 1);
    max-height: 200px;
    overflow-y: auto;
  }
  .tag-suggestions button {
    display: block; width: 100%; text-align: left;
    padding: var(--space-sm) var(--space-md);
    background: none; border: none;
    color: var(--text-primary); font-size: var(--font-sm); cursor: pointer;
  }
  .tag-suggestions button:hover { background: var(--bg-surface); }

  .actions { display: flex; gap: var(--space-md); flex-wrap: wrap; }
  .action-btn {
    flex: 1;
    min-width: 5rem;
    display: inline-flex; align-items: center; justify-content: center; gap: var(--space-xs);
    padding: var(--space-md) var(--space-xl);
    border-radius: var(--radius-md);
    font-size: var(--font-base);
    font-weight: 500;
    cursor: pointer;
    border: 1px solid var(--border-primary);
    background: var(--bg-primary);
    color: var(--text-primary);
    transition: all 0.2s ease;
  }
  .danger-ghost { color: var(--color-danger); border-color: var(--color-danger); }
  .danger-ghost:hover { background: var(--color-danger-hover); color: #fff; }
  .success-ghost { color: var(--color-success); border-color: var(--color-success); }
  .success-ghost:hover { background: var(--color-success); color: #fff; }
  .danger-solid {
    background: var(--color-danger); color: #fff; border-color: var(--color-danger); font-weight: 600;
  }
  .danger-solid:hover:not(:disabled) { background: var(--color-danger-hover); }
</style>
