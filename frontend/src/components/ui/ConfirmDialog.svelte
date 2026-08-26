<script>
  // Confirmation dialog for destructive/irreversible actions (charter §3.9).
  // Replaces native window.confirm everywhere.
  import Modal from './Modal.svelte'
  let {
    open = false,
    title = 'Are you sure?',
    body = '',
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    danger = false,
    busy = false,
    onconfirm = () => {},
    onclose = () => {}
  } = $props()
</script>

<Modal {open} {title} width="sm" {onclose}>
  <p class="confirm-body">{@html body}</p>
  {#if danger}
    <p class="confirm-hint">This action cannot be undone.</p>
  {/if}
  {#snippet footer()}
    <button class="btn-cancel" onclick={onclose} disabled={busy}>{cancelLabel}</button>
    <button
      class="btn-confirm"
      class:danger
      onclick={onconfirm}
      disabled={busy}
    >{busy ? 'Working…' : confirmLabel}</button>
  {/snippet}
</Modal>

<style>
  .confirm-body {
    margin: 0 0 var(--space-md);
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .confirm-hint {
    margin: 0;
    font-size: var(--font-base);
    color: var(--text-tertiary);
  }
  .btn-cancel,
  .btn-confirm {
    padding: var(--space-lg) var(--space-4xl);
    border-radius: var(--radius-md);
    font-size: var(--font-md);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
  }
  .btn-cancel {
    background: var(--bg-secondary);
    color: var(--text-secondary);
    border: none;
  }
  .btn-cancel:hover { background: var(--border-primary); }
  .btn-confirm {
    background: var(--color-accent);
    color: #ffffff;
    border: none;
  }
  .btn-confirm:hover:not(:disabled) { background: var(--color-accent-hover); }
  .btn-confirm.danger {
    background: var(--color-danger);
    color: white;
  }
  .btn-confirm.danger:hover:not(:disabled) { background: var(--color-danger-hover); }
  .btn-confirm:disabled, .btn-cancel:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
