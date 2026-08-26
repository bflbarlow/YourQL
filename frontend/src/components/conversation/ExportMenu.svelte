<script>
  // Export menu (extracted from ConversationView header).
  import { FileDown, Printer, FileCode, FileText } from 'lucide-svelte'
  import IconBtn from '../ui/IconBtn.svelte'
  import { ExportConversationPDF, ExportConversationHTML, ExportConversationMarkdown } from '../../../wailsjs/go/main/App.js'

  let { conversation = null } = $props()

  let open = $state(false)
  let exportMenuEl = $state(null)

  function handleDocClick(e) {
    if (open && !e.target.closest('.export-btn-wrapper')) open = false
  }

  $effect(() => {
    document.addEventListener('click', handleDocClick)
    return () => document.removeEventListener('click', handleDocClick)
  })

  function handlePrintPDF() {
    const scale = localStorage.getItem('yourql-ui-scale') || 'medium'
    const fontSize = { small: '12px', medium: '13px', large: '14px' }[scale]
    const style = document.createElement('style')
    style.id = 'print-scale'
    style.textContent = '@media print { html { font-size: ' + fontSize + ' !important; } }'
    document.head.appendChild(style)
    document.title = 'YourQL - ' + (conversation?.title || 'Untitled') + ' - ' + new Date().toLocaleDateString()
    ExportConversationPDF()
    setTimeout(() => {
      document.title = 'YourQL'
      const el = document.getElementById('print-scale')
      if (el) el.remove()
    }, 1000)
  }

  async function handleExportHTML() {
    if (!conversation?.id) return
    const err = await ExportConversationHTML(conversation.id)
    if (err) console.error('HTML export failed:', err)
  }

  async function handleExportMarkdown() {
    if (!conversation?.id) return
    const err = await ExportConversationMarkdown(conversation.id)
    if (err) console.error('Markdown export failed:', err)
  }
</script>

<div class="export-btn-wrapper" bind:this={exportMenuEl}>
  <IconBtn label="Export conversation" onclick={() => open = !open}>
    <FileDown size={16} />
  </IconBtn>
  {#if open}
    <div class="export-popover" role="menu">
      <button class="item" onclick={() => { open = false; handlePrintPDF() }} type="button" role="menuitem">
        <Printer size={14} /><span>Print to PDF</span>
      </button>
      <button class="item" onclick={() => { open = false; handleExportHTML() }} type="button" role="menuitem">
        <FileCode size={14} /><span>Export as HTML</span>
      </button>
      <button class="item" onclick={() => { open = false; handleExportMarkdown() }} type="button" role="menuitem">
        <FileText size={14} /><span>Export as Markdown</span>
      </button>
    </div>
  {/if}
</div>

<style>
  .export-btn-wrapper { position: relative; flex-shrink: 0; }
  .export-popover {
    position: absolute;
    top: calc(100% + var(--space-xs));
    right: 0;
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    z-index: var(--z-popover);
    min-width: 180px;
    padding: var(--space-xs);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .item {
    display: flex;
    align-items: center;
    gap: var(--space-lg);
    padding: var(--space-md) var(--space-xl);
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    font-size: var(--font-sm);
    cursor: pointer;
    text-align: left;
    width: 100%;
    transition: background 0.15s ease;
  }
  .item:hover { background: var(--bg-secondary); }
</style>
