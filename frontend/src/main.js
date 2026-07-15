import { mount } from 'svelte'
import App from './App.svelte'

mount(App, {
  target: document.getElementById('app')
})

// ==================== Table Sorting ====================
let sortState = new Map() // tableId -> { col: number, dir: 'asc'|'desc' }

function sortTable(tableEl, colIndex, direction) {
  const dataAttr = tableEl.getAttribute('data-sort-rows')
  if (!dataAttr) return

  try {
    const data = JSON.parse(decodeURIComponent(escape(dataAttr)))
    const rows = data.rows
    const columns = data.columns

    // Sort rows
    const sorted = [...rows].sort((a, b) => {
      let valA = a[colIndex]
      let valB = b[colIndex]

      // Handle null/undefined
      if (valA == null) return 1
      if (valB == null) return -1

      let comparison = 0
      // Try numeric comparison
      const numA = Number(valA)
      const numB = Number(valB)
      if (!isNaN(numA) && !isNaN(numB) && isFinite(numA) && isFinite(numB)) {
        comparison = numA - numB
      } else {
        // String comparison
        comparison = String(valA).localeCompare(String(valB))
      }

      return direction === 'asc' ? comparison : -comparison
    })

    // Update table body
    const tbody = tableEl.querySelector('tbody')
    if (!tbody) return

    tbody.innerHTML = ''
    for (const row of sorted) {
      const tr = document.createElement('tr')
      tr.className = 'result-row'
      for (let i = 0; i < row.length; i++) {
        const td = document.createElement('td')
        const cell = String(row[i])
        let cellClass = ''
        if (/^-?\d+(\.\d+)?$/.test(cell)) {
          cellClass = 'num-cell'
        } else if (/\d{4}-\d{2}-\d{2}/.test(cell)) {
          cellClass = 'date-cell'
        }
        td.className = cellClass
        td.style = 'border:1px solid #e8e8e8; padding:8px 12px; max-width:400px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;'
        td.title = cell
        td.textContent = cell
        tr.appendChild(td)
      }
      tbody.appendChild(tr)
    }

    // Update sort indicators
    const headers = tableEl.querySelectorAll('th.sort-header')
    headers.forEach((th, idx) => {
      const indicator = th.querySelector('.sort-indicator')
      if (idx === colIndex) {
        indicator.textContent = direction === 'asc' ? ' ↑' : ' ↓'
        indicator.style.color = '#0288d1'
      } else {
        indicator.textContent = ' ↕'
        indicator.style.color = '#ccc'
      }
    })
  } catch (e) {
    console.error('Failed to sort table:', e)
  }
}

// Event delegation for sort headers
document.addEventListener('click', (e) => {
  const th = e.target.closest('th.sort-header')
  if (!th) return

  const table = th.closest('table.result-table')
  if (!table) return

  const colIndex = parseInt(th.getAttribute('data-col'))
  if (isNaN(colIndex)) return

  // Get current sort state for this table
  let state = sortState.get(table)
  if (!state) {
    state = { col: -1, dir: 'asc' }
    sortState.set(table, state)
  }

  // Toggle direction if same column, otherwise switch to ascending
  if (state.col === colIndex) {
    state.dir = state.dir === 'asc' ? 'desc' : 'asc'
  } else {
    state.col = colIndex
    state.dir = 'asc'
  }

  sortTable(table, colIndex, state.dir)
})

// ==================== SQL Copy Button ====================
function copySQL(codeId) {
  const codeEl = document.getElementById(codeId)
  if (!codeEl) return

  const sql = codeEl.textContent
  navigator.clipboard.writeText(sql).then(() => {
    // Show temporary success feedback
    const btn = document.querySelector(`[onclick="copySQL('${codeId}')"]`)
    if (btn) {
      const originalText = btn.textContent
      btn.textContent = '✓ Copied!'
      btn.style.color = '#0288d1'
      btn.style.borderColor = '#0288d1'
      setTimeout(() => {
        btn.textContent = originalText
        btn.style.color = ''
        btn.style.borderColor = ''
      }, 2000)
    }
  }).catch(err => {
    console.error('Failed to copy SQL:', err)
  })
}

// Show/hide copy button when SQL details are opened/closed
function setupSQLBlockListeners() {
  const sqlBlocks = document.querySelectorAll('details.sql-block')
  sqlBlocks.forEach(block => {
    // Remove old listeners to prevent duplicates
    block.removeEventListener('toggle', handleSQLToggle)
    block.addEventListener('toggle', handleSQLToggle)
  })
}

function handleSQLToggle(e) {
  const block = e.target
  const copyBtn = block.querySelector('.copy-sql-btn')
  if (!copyBtn) return

  // Show copy button when details are open, hide when closed
  copyBtn.style.display = block.open ? 'block' : 'none'
}

// Set up listeners after DOM is ready
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', setupSQLBlockListeners)
} else {
  setupSQLBlockListeners()
}

// Also set up listeners when new content is added (e.g., after a message is sent)
const observer = new MutationObserver((mutations) => {
  let shouldSetup = false
  for (const mutation of mutations) {
    if (mutation.addedNodes.length > 0) {
      shouldSetup = true
      break
    }
  }
  if (shouldSetup) {
    setupSQLBlockListeners()
  }
})

observer.observe(document.body, { childList: true, subtree: true })
