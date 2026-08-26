<script>
  // One loading idiom (A7): bouncing dots + phase label. aria-live so screen
  // readers hear "Exploring schema…" style phases (charter §3.9).
  let { label = '' } = $props()
</script>

<div class="busy" aria-live="polite" aria-busy="true">
  <div class="dots" aria-hidden="true">
    <span></span><span></span><span></span>
  </div>
  {#if label}<span class="label">{label}</span>{/if}
</div>

<style>
  .busy {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
    color: var(--text-tertiary);
    font-size: var(--font-md);
  }
  .dots {
    display: flex;
    gap: var(--space-xs);
  }
  .dots span {
    width: var(--space-md);
    height: var(--space-md);
    background: var(--color-accent);
    border-radius: var(--radius-md);
    animation: busy-loading 1.4s infinite ease-in-out;
  }
  .dots span:nth-child(1) { animation-delay: -0.32s; }
  .dots span:nth-child(2) { animation-delay: -0.16s; }
  .label {
    font-size: var(--font-base);
  }
  @keyframes busy-loading {
    0%, 80%, 100% { transform: scale(0.6); opacity: 0.5; }
    40% { transform: scale(1); opacity: 1; }
  }
</style>
