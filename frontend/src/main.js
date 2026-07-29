import { mount } from 'svelte'
import App from './App.svelte'
import { EventsOn } from '../wailsjs/runtime/runtime.js'
import './variables.css'

// ==================== UI Scale ====================
const SCALE_KEY = 'yourql-ui-scale'
function applyScale(scale) {
  document.documentElement.setAttribute('data-ui-scale', scale)
  localStorage.setItem(SCALE_KEY, scale)
}
const savedScale = localStorage.getItem(SCALE_KEY) || 'medium'
applyScale(savedScale)
window.setUIScale = applyScale

// ==================== Accent Color ====================
const ACCENT_KEY = 'yourql-accent'
const ACCENT_DARK_MODE_MAP = {
  '#0288d1': '#4da8da',  // Blue
  '#388e3c': '#66bb6a',  // Green
  '#f57c00': '#ffa726',  // Orange
  '#7b1fa2': '#b39ddb',  // Purple
  '#c62828': '#ef5350',  // Red
  '#00695c': '#4db6ac',  // Teal
}

// HSL helpers for perceptual color manipulation
function hexToHSL(hex) {
  let r = 0, g = 0, b = 0
  hex = hex.replace('#', '')
  if (hex.length === 3) {
    r = parseInt(hex[0] + hex[0], 16) / 255
    g = parseInt(hex[1] + hex[1], 16) / 255
    b = parseInt(hex[2] + hex[2], 16) / 255
  } else {
    r = parseInt(hex.substring(0, 2), 16) / 255
    g = parseInt(hex.substring(2, 4), 16) / 255
    b = parseInt(hex.substring(4, 6), 16) / 255
  }
  const max = Math.max(r, g, b), min = Math.min(r, g, b)
  let h = 0, s = 0
  const l = (max + min) / 2
  if (max !== min) {
    const d = max - min
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
    switch (max) {
      case r: h = ((g - b) / d + (g < b ? 6 : 0)) / 6; break
      case g: h = ((b - r) / d + 2) / 6; break
      case b: h = ((r - g) / d + 4) / 6; break
    }
  }
  return { h, s, l }
}

function hslToHex(h, s, l) {
  const hue2rgb = (p, q, t) => {
    if (t < 0) t += 1
    if (t > 1) t -= 1
    if (t < 1/6) return p + (q - p) * 6 * t
    if (t < 1/2) return q
    if (t < 2/3) return p + (q - p) * (2/3 - t) * 6
    return p
  }
  if (s === 0) {
    const v = Math.round(l * 255)
    return '#' + [v, v, v].map(c => c.toString(16).padStart(2, '0')).join('')
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s
  const p = 2 * l - q
  const r = Math.round(hue2rgb(p, q, h + 1/3) * 255)
  const g = Math.round(hue2rgb(p, q, h) * 255)
  const b = Math.round(hue2rgb(p, q, h - 1/3) * 255)
  return '#' + [r, g, b].map(c => c.toString(16).padStart(2, '0')).join('')
}

function darken(hex, pct) {
  const { h, s, l } = hexToHSL(hex)
  return hslToHex(h, s, Math.max(0, l - pct / 100))
}

function lighten(hex, pct) {
  const { h, s, l } = hexToHSL(hex)
  return hslToHex(h, s, Math.min(1, l + pct / 100))
}

function hexToRgba(hex, alpha) {
  hex = hex.replace('#', '')
  if (hex.length === 3) hex = hex.split('').map(c => c + c).join('')
  const r = parseInt(hex.substring(0, 2), 16)
  const g = parseInt(hex.substring(2, 4), 16)
  const b = parseInt(hex.substring(4, 6), 16)
  return `rgba(${r},${g},${b},${alpha})`
}

function applyAccent(hex) {
  if (!/^#?([a-f\d]{3}){1,2}$/i.test(hex)) return
  if (hex.length === 4) hex = '#' + hex.slice(1).split('').map(c => c + c).join('')
  const root = document.documentElement
  root.style.setProperty('--color-accent', hex)
  root.style.setProperty('--color-accent-hover', darken(hex, 10))
  root.style.setProperty('--color-accent-light', hexToRgba(hex, 0.1))
  root.style.setProperty('--color-accent-border', hexToRgba(hex, 0.3))
  // Print accent: always the light-mode hex, used by the print media query
  root.style.setProperty('--color-accent-print', hex)
  localStorage.setItem(ACCENT_KEY, hex)
}

// ==================== Theme ====================
const THEME_KEY = 'yourql-theme'        // user's selection: 'light' | 'dark' | 'system'
const DEFAULT_THEME_SELECTION = 'system'

function resolveTheme(selection) {
  if (selection === 'system') {
    if (typeof window !== 'undefined' && window.matchMedia) {
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    }
    return 'light'
  }
  return selection
}

function applyResolvedTheme(resolved) {
  document.documentElement.setAttribute('data-theme', resolved)
  // Re-apply accent so dark-mode variant (preset map or 30% lighten) is correct
  const currentAccent = localStorage.getItem(ACCENT_KEY) || '#0288d1'
  const darkAccent = ACCENT_DARK_MODE_MAP[currentAccent]
  if (resolved === 'dark' && darkAccent) {
    document.documentElement.style.setProperty('--color-accent', darkAccent)
    document.documentElement.style.setProperty('--color-accent-hover', lighten(darkAccent, 10))
    document.documentElement.style.setProperty('--color-accent-light', hexToRgba(darkAccent, 0.15))
    document.documentElement.style.setProperty('--color-accent-border', hexToRgba(darkAccent, 0.4))
  } else if (resolved === 'dark' && !darkAccent) {
    const lightened = lighten(currentAccent, 30)
    document.documentElement.style.setProperty('--color-accent', lightened)
    document.documentElement.style.setProperty('--color-accent-hover', lighten(lightened, 10))
    document.documentElement.style.setProperty('--color-accent-light', hexToRgba(lightened, 0.15))
    document.documentElement.style.setProperty('--color-accent-border', hexToRgba(lightened, 0.4))
  } else {
    applyAccent(currentAccent)
  }
  // Notify chart components
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('theme-change', { detail: { theme: resolved } }))
  }
}

function setThemeSelection(selection) {
  localStorage.setItem(THEME_KEY, selection)
  applyResolvedTheme(resolveTheme(selection))
}

function applyThemeAndAccent() {
  const savedSelection = localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION
  const savedAccent = localStorage.getItem(ACCENT_KEY) || '#0288d1'
  applyAccent(savedAccent)
  applyResolvedTheme(resolveTheme(savedSelection))
}

// Expose for SettingsView Svelte component
window.setThemeSelection = setThemeSelection
window.setAccent = (hex) => {
  applyAccent(hex)
  // Re-resolve theme so dark-mode variant updates
  const selection = localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION
  applyResolvedTheme(resolveTheme(selection))
}

// System preference listener — only fires when user chose "system"
if (typeof window !== 'undefined' && window.matchMedia) {
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    const selection = localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION
    if (selection === 'system') {
      applyResolvedTheme(resolveTheme('system'))
    }
  })
}

// Initialize theme & accent before mounting app
applyThemeAndAccent()

mount(App, {
  target: document.getElementById('app')
})

// ==================== Table Sorting ====================
let sortState = new Map()

function sortTable(tableEl, colIndex, direction) {
  const dataAttr = tableEl.getAttribute('data-sort-rows')
  if (!dataAttr) return

  try {
    const data = JSON.parse(decodeURIComponent(escape(dataAttr)))
    const rows = data.rows
    const sorted = [...rows].sort((a, b) => {
      let valA = a[colIndex]
      let valB = b[colIndex]
      if (valA == null) return 1
      if (valB == null) return -1
      let comparison = 0
      const numA = Number(valA)
      const numB = Number(valB)
      if (!isNaN(numA) && !isNaN(numB) && isFinite(numA) && isFinite(numB)) {
        comparison = numA - numB
      } else {
        comparison = String(valA).localeCompare(String(valB))
      }
      return direction === 'asc' ? comparison : -comparison
    })

    const tbody = tableEl.querySelector('tbody')
    if (!tbody) return

    const container = tableEl.closest('.results-card')
    const expandBtn = container ? container.querySelector('.table-expand-btn') : null
    const isExpanded = expandBtn && expandBtn.textContent.includes('Show first 10')
    let collapsedClass = null
    if (expandBtn) {
      for (const tr of tableEl.querySelectorAll('tr')) {
        for (const cls of tr.classList) {
          if (cls.startsWith('collapsed-row-')) {
            collapsedClass = cls
            break
          }
        }
        if (collapsedClass) break
      }
    }

    tbody.innerHTML = ''

    for (let ri = 0; ri < sorted.length; ri++) {
      const row = sorted[ri]
      const tr = document.createElement('tr')
      let rowClass = 'result-row'
      if (collapsedClass && ri >= 10 && !isExpanded) {
        rowClass += ' ' + collapsedClass
      }
      tr.className = rowClass
      if (collapsedClass && ri >= 10 && !isExpanded) tr.style.display = 'none'
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
        td.style.border = '1px solid var(--border-primary)'
        td.style.padding = '8px 12px'
        td.style.maxWidth = '400px'
        td.style.overflow = 'hidden'
        td.style.textOverflow = 'ellipsis'
        td.style.whiteSpace = 'nowrap'
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
        indicator.style.color = 'var(--color-accent)'
      } else {
        indicator.textContent = ' ↕'
        indicator.style.color = 'var(--text-tertiary)'
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
  let state = sortState.get(table)
  if (!state) {
    state = { col: -1, dir: 'asc' }
    sortState.set(table, state)
  }
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
  navigator.clipboard.writeText(codeEl.textContent).then(() => {
    const btn = document.querySelector(`[onclick="copySQL('${codeId}')"]`)
    if (btn) {
      const originalText = btn.textContent
      btn.textContent = '✓ Copied!'
      btn.style.color = 'var(--color-accent)'
      btn.style.borderColor = 'var(--color-accent)'
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

// Toggle SQL popover visibility
function toggleSQLPopover(btn, popoverId) {
  const popover = document.getElementById(popoverId)
  if (!popover) return
  if (popover.style.display === 'none') {
    popover.style.display = 'block'
    btn.style.background = 'var(--color-accent-light)'
    btn.style.borderColor = 'var(--color-accent)'
  } else {
    popover.style.display = 'none'
    btn.style.background = 'var(--bg-primary)'
    btn.style.borderColor = 'var(--border-primary)'
  }
}

// Make functions globally accessible for inline onclick handlers
window.toggleSQLPopover = toggleSQLPopover
window.copySQL = copySQL

// Show/hide copy button when SQL details are opened/closed
function setupSQLBlockListeners() {
  const sqlBlocks = document.querySelectorAll('details.sql-block')
  sqlBlocks.forEach(block => {
    block.removeEventListener('toggle', handleSQLToggle)
    block.addEventListener('toggle', handleSQLToggle)
  })
}

function handleSQLToggle(e) {
  const block = e.target
  const copyBtn = block.querySelector('.copy-sql-btn')
  if (!copyBtn) return
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
