<script>
  // Inline row-level tag popover for the discussion list (extracted from App.svelte).
  import { ListAllTags, GetTagsForConversation, AddTagToConversation, RemoveTagFromConversation } from '../../../wailsjs/go/main/App.js'

  let { conversation = null, open = false, onclose = () => {} } = $props()

  let tags = $state([])
  let allTags = $state([])
  let input = $state('')

  $effect(() => {
    if (open && conversation) {
      input = ''
      load()
    }
  })

  async function load() {
    try {
      tags = await GetTagsForConversation(conversation.id) || []
      allTags = await ListAllTags() || []
    } catch (e) { console.error('Failed to load tags:', e) }
  }

  let suggestions = $derived(
    (allTags || []).filter(t =>
      t.toLowerCase().includes((input || '').toLowerCase()) && !tags.includes(t)
    ).slice(0, 8)
  )

  async function addTag(name) {
    const val = (name || '').trim()
    if (!val) return
    try {
      await AddTagToConversation(conversation.id, val)
      tags = await GetTagsForConversation(conversation.id) || []
      allTags = await ListAllTags() || []
      input = ''
    } catch (e) { console.error('Failed to add tag:', e) }
  }

  export async function removeTag(name) {
    try {
      await RemoveTagFromConversation(conversation.id, name)
      tags = await GetTagsForConversation(conversation.id) || []
    } catch (e) { console.error('Failed to remove tag:', e) }
  }

  function handleKeydown(e) {
    if (e.key === 'Enter') {
      e.preventDefault()
      const val = input.trim()
      if (!val) return
      const match = suggestions.find(t => t.toLowerCase() === val.toLowerCase())
      addTag(match || val)
    } else if (e.key === 'Escape') {
      onclose()
    }
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="tp-overlay" onclick={onclose}></div>
  <div class="tp-popover" onclick={(e) => e.stopPropagation()} role="dialog" aria-label="Tags">
    <div class="tp-title">Tags</div>
    <div class="tp-chips">
      {#each tags as tag}
        <span class="tp-chip">
          {tag}
          <button onclick={() => removeTag(tag)} title="Remove tag" aria-label="Remove tag {tag}" type="button">&times;</button>
        </span>
      {/each}
      {#if tags.length === 0}
        <span class="tp-empty">No tags yet</span>
      {/if}
    </div>
    <div class="tp-input-wrapper">
      <input type="text" placeholder="Add a tag…" bind:value={input} onkeydown={handleKeydown} />
      {#if suggestions.length > 0}
        <div class="tp-suggestions">
          {#each suggestions as s}
            <button type="button" onclick={() => addTag(s)}>{s}</button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .tp-overlay {
    position: fixed;
    inset: 0;
    z-index: var(--z-popover);
  }
  .tp-popover {
    position: absolute;
    top: calc(100% + var(--space-sm));
    right: 0;
    width: 15rem;
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    padding: var(--space-md);
    z-index: calc(var(--z-popover) + 1);
  }
  .tp-title {
    font-size: var(--font-xs);
    font-weight: 600;
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    margin-bottom: var(--space-sm);
  }
  .tp-chips { display: flex; flex-wrap: wrap; gap: var(--space-xs); }
  .tp-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2xs);
    background: var(--color-accent-light);
    color: var(--color-accent);
    padding: var(--space-2xs) var(--space-sm);
    border-radius: var(--radius-sm);
    font-size: var(--font-xs);
  }
  .tp-chip button {
    background: none; border: none; color: var(--color-accent);
    cursor: pointer; padding: 0; line-height: 1; opacity: 0.6; font-size: var(--font-md);
  }
  .tp-chip button:hover { opacity: 1; }
  .tp-empty {
    color: var(--text-tertiary); font-size: var(--font-xs); font-style: italic;
  }
  .tp-input-wrapper { position: relative; margin-top: var(--space-sm); }
  .tp-input-wrapper input {
    width: 100%;
    padding: var(--space-sm) var(--space-md);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-sm);
    box-sizing: border-box;
  }
  .tp-input-wrapper input:focus { outline: none; border-color: var(--color-accent); }
  .tp-suggestions {
    position: absolute;
    top: 100%; left: 0; right: 0;
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-top: none;
    border-radius: 0 0 var(--radius-md) var(--radius-md);
    z-index: 1;
    max-height: 200px;
    overflow-y: auto;
  }
  .tp-suggestions button {
    display: block; width: 100%; text-align: left;
    padding: var(--space-sm) var(--space-md);
    background: none; border: none;
    color: var(--text-primary); font-size: var(--font-sm); cursor: pointer;
  }
  .tp-suggestions button:hover { background: var(--bg-surface); }
</style>
