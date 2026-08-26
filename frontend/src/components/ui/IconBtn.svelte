<script>
  // Single square icon-button (D2): replaces row-action-btn, gear-btn-header,
  // export-btn-header, sidebar-toggle, modal-close, search-clear-btn.
  let {
    icon = null,        // Lucide component (rendered via <i>{@html}> not needed — pass as child instead)
    label,              // required: aria-label + title
    active = false,
    size = 'md',        // md = 2.25rem square, sm = 1.75rem
    type = 'button',
    onclick = () => {},
    children
  } = $props()
</script>

<button
  {type}
  class="icon-btn"
  class:active
  data-size={size}
  aria-label={label}
  title={label}
  {onclick}
>
  {@render children?.()}
</button>

<style>
  .icon-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.25rem;
    height: 2.25rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    color: var(--text-secondary);
    transition: all 0.15s ease;
    flex-shrink: 0;
    padding: 0;
  }
  .icon-btn[data-size="sm"] {
    width: 1.75rem;
    height: 1.75rem;
  }
  .icon-btn:hover {
    background: var(--color-accent-light);
    color: var(--color-accent);
    border-color: var(--color-accent-border);
  }
  .icon-btn.active {
    background: var(--color-accent-light);
    color: var(--color-accent);
    border-color: var(--color-accent);
  }
</style>
