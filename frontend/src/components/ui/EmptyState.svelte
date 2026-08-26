<script>
  // One empty-state idiom (A7): always answers "what happened / what next?"
  let {
    icon = null,        // Lucide component rendered by parent via children snippet
    title,
    hint = '',
    actionLabel = '',
    onaction = () => {},
    children
  } = $props()
</script>

<div class="empty-state">
  {#if children}{@render children()}{/if}
  <p class="empty-title">{title}</p>
  {#if hint}
    <p class="empty-hint">{hint}</p>
  {/if}
  {#if actionLabel}
    <button class="empty-action" onclick={onaction} type="button">{actionLabel}</button>
  {/if}
</div>

<style>
  .empty-state {
    text-align: center;
    padding: 5rem var(--space-4xl);
    color: var(--text-tertiary);
  }
  .empty-title {
    margin: 0 0 var(--space-lg);
    font-size: var(--font-2xl);
    color: var(--text-secondary);
  }
  .empty-hint {
    margin: 0 0 var(--space-xl);
    font-size: var(--font-md);
    color: var(--text-tertiary);
  }
  .empty-action {
    padding: var(--space-md) var(--space-4xl);
    background: var(--color-accent-light);
    border: none;
    border-radius: var(--radius-md);
    color: var(--color-accent);
    font-size: var(--font-base);
    font-weight: 500;
    cursor: pointer;
    transition: background 0.2s ease;
  }
  .empty-action:hover {
    background: var(--color-accent-border);
  }
</style>
