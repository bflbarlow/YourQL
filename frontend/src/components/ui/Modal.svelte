<script>
  // Single overlay primitive (A4/D2): scrim + Escape + focus management.
  import { X } from 'lucide-svelte'
  let {
    open = false,
    title = '',
    width = 'md',        // sm: 25rem | md: 31.25rem | lg: 42rem
    onclose = () => {},
    children,
    footer
  } = $props()

  let dialogEl = $state(null)
  let previouslyFocused = null

  $effect(() => {
    if (open) {
      previouslyFocused = document.activeElement
      // Focus first focusable element after mount
      queueMicrotask(() => {
        const el = dialogEl?.querySelector('input, select, textarea, button')
        el?.focus()
      })
      return () => {
        previouslyFocused?.focus?.()
      }
    }
  })

  function handleKeydown(e) {
    if (e.key === 'Escape' && open) {
      e.stopPropagation()
      onclose()
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="overlay" data-width={width} onclick={(e) => { if (e.target === e.currentTarget) onclose() }}>
    <div class="dialog" role="dialog" aria-modal="true" aria-label={title} bind:this={dialogEl}>
      {#if title}
        <div class="dialog-header">
          <h3>{title}</h3>
          <button class="icon-close" onclick={onclose} aria-label="Close" type="button">
            <X size={16} />
          </button>
        </div>
      {/if}
      <div class="dialog-body">
        {@render children?.()}
      </div>
      {#if footer}
        <div class="dialog-footer">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: var(--z-modal);
  }
  .dialog {
    background: var(--bg-primary);
    border-radius: var(--radius-md);
    width: var(--modal-width);
    max-width: 90vw;
    max-height: 90vh;
    overflow-y: auto;
    border: 1px solid var(--border-primary);
    box-shadow: var(--shadow-lg);
    color: var(--text-primary);
  }
  .overlay[data-width="sm"] .dialog { width: 25rem; }
  .overlay[data-width="lg"] .dialog { width: 42rem; }

  .dialog-header {
    padding: var(--space-4xl) var(--space-6xl);
    border-bottom: 1px solid var(--border-primary);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .dialog-header h3 {
    margin: 0;
    font-size: var(--font-3xl);
    font-weight: 600;
    color: var(--text-primary);
  }
  .icon-close {
    background: none;
    border: none;
    color: var(--text-tertiary);
    cursor: pointer;
    padding: var(--space-xs);
    width: var(--space-6xl);
    height: var(--space-6xl);
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-md);
  }
  .icon-close:hover {
    background: var(--bg-secondary);
    color: var(--text-primary);
  }
  .dialog-body { padding: var(--space-6xl); }
  .dialog-footer {
    padding: var(--space-4xl) var(--space-6xl);
    border-top: 1px solid var(--border-primary);
    display: flex;
    justify-content: flex-end;
    gap: var(--space-lg);
  }
</style>
