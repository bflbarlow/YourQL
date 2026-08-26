<script>
  // General tab — extracted verbatim from SettingsView; theme/accent/scale now
  // via lib/theme.js (no window.* globals).
  import {
    GetAppSetting, SetAppSetting,
    GetLoggingEnabled, SetLoggingEnabled, ExportLog, ClearLog,
    ExportDatabase
  } from '../../../../wailsjs/go/main/App.js'
  import { setThemeSelection, setAccent, applyScale, getScale, getThemeSelection, getAccent } from '../../../lib/theme.js'

  let {
    minimalist = false,
    onMinimalistChange = () => {},
    onAgentLoopEnabledChange = () => {},
    onDbSwitcherEnabledChange = () => {}
  } = $props()

  let generalScale = $state(getScale())
  let currentTheme = $state(getThemeSelection())
  let currentAccent = $state(getAccent())

  // Agent Loop / DB switcher visibility toggles
  let agentLoopEnabled = $state(false)
  let dbSwitcherEnabled = $state(false)

  // Logging state
  let loggingEnabled = $state(false)
  let loggingRestartNotice = $state(false)
  let loggingExportStatus = $state('')

  // Database export state
  let exportFormat = $state('xlsx')
  let exportIncludeCredentials = $state(false)
  let exportTables = $state({})
  let exportStatus = $state('')
  let exportError = $state('')

  // Timeout state
  let pipelineTimeout = $state(180)
  let summarizationTimeout = $state(300)

  const allExportTables = [
    'conversations', 'conversation_messages', 'conversation_skills',
    'llm_providers', 'data_sources', 'skills',
    'queries', 'app_settings', 'discussion_defaults',
    'agent_loop_config', 'schema_migrations'
  ]

  const accentPresets = [
    { hex: '#0288d1', name: 'Blue' },
    { hex: '#388e3c', name: 'Green' },
    { hex: '#f57c00', name: 'Orange' },
    { hex: '#7b1fa2', name: 'Purple' },
    { hex: '#c62828', name: 'Red' },
    { hex: '#00695c', name: 'Teal' }
  ]

  function hexToLuminance(hex) {
    const r = parseInt(hex.slice(1, 3), 16) / 255
    const g = parseInt(hex.slice(3, 5), 16) / 255
    const b = parseInt(hex.slice(5, 7), 16) / 255
    const linearize = (c) => c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)
    return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b)
  }

  let accentLuminanceWarning = $derived(
    currentAccent && hexToLuminance(currentAccent) > 0.55
      ? '⚠ This color may have poor contrast on light backgrounds. Consider a darker shade.'
      : ''
  )

  loadAgentLoopEnabled()
  loadDbSwitcherPrefs()
  loadLoggingEnabled()
  loadTimeoutSettings()
  initExportTables()

  function initExportTables() {
    const map = {}
    for (const t of allExportTables) map[t] = true
    exportTables = map
  }

  function selectedTables() {
    return allExportTables.filter(t => exportTables[t])
  }

  let zeroTablesSelected = $derived(exportFormat === 'csv' && selectedTables().length === 0)

  function selectAllTables() {
    const map = {}
    for (const t of allExportTables) map[t] = true
    exportTables = map
  }

  function selectNoTables() {
    const map = {}
    for (const t of allExportTables) map[t] = false
    exportTables = map
  }

  async function handleExportDatabase() {
    const tables = exportFormat === 'csv' ? selectedTables() : []
    exportStatus = 'exporting'
    exportError = ''
    try {
      const result = await ExportDatabase(exportFormat, tables, exportIncludeCredentials)
      if (result === '') {
        exportStatus = 'success'
      } else {
        exportError = result
        exportStatus = 'error'
      }
    } catch (e) {
      exportError = String(e || 'Unknown error')
      exportStatus = 'error'
    }
  }

  async function loadAgentLoopEnabled() {
    try {
      const val = await GetAppSetting('agent_loop_advanced_enabled')
      agentLoopEnabled = val === 'true'
    } catch {
      agentLoopEnabled = false
    }
  }

  async function loadDbSwitcherPrefs() {
    try {
      const val = await GetAppSetting('db_switcher_enabled')
      dbSwitcherEnabled = val === 'true'
    } catch { /* not set yet, leave disabled */ }
  }

  async function toggleAgentLoop() {
    agentLoopEnabled = !agentLoopEnabled
    try {
      await SetAppSetting('agent_loop_advanced_enabled', agentLoopEnabled ? 'true' : 'false')
    } catch (e) {
      console.error('Failed to save agent loop toggle:', e)
    }
  }

  async function toggleDbSwitcher() {
    dbSwitcherEnabled = !dbSwitcherEnabled
    try {
      await SetAppSetting('db_switcher_enabled', dbSwitcherEnabled ? 'true' : 'false')
    } catch (e) {
      console.error('Failed to save DB switcher toggle:', e)
    }
  }

  async function loadLoggingEnabled() {
    try {
      loggingEnabled = await GetLoggingEnabled()
    } catch {
      loggingEnabled = false
    }
  }

  async function toggleLogging() {
    loggingEnabled = !loggingEnabled
    try {
      await SetLoggingEnabled(loggingEnabled)
      loggingRestartNotice = true
    } catch (e) {
      console.error('Failed to save logging toggle:', e)
      loggingEnabled = !loggingEnabled // revert
    }
  }

  async function exportLog() {
    loggingExportStatus = 'exporting'
    try {
      const result = await ExportLog()
      if (result === 'empty') {
        loggingExportStatus = 'empty'
        return
      }
      if (result === '') {
        loggingExportStatus = 'success'
        return
      }
      console.error('Failed to export log:', result)
      loggingExportStatus = 'error'
    } catch (e) {
      console.error('Failed to export log:', e)
      loggingExportStatus = 'error'
    }
  }

  async function clearLog() {
    loggingExportStatus = 'clearing'
    try {
      await ClearLog()
      loggingExportStatus = 'cleared'
    } catch (e) {
      console.error('Failed to clear log:', e)
      loggingExportStatus = 'error'
    }
  }

  async function loadTimeoutSettings() {
    try {
      const pt = await GetAppSetting('pipeline_timeout_seconds')
      if (pt) pipelineTimeout = parseInt(pt) || 180
    } catch { /* use default */ }
    try {
      const st = await GetAppSetting('summarization_timeout_seconds')
      if (st) summarizationTimeout = parseInt(st) || 300
    } catch { /* use default */ }
  }

  async function saveTimeoutSetting(key, value) {
    try {
      await SetAppSetting(key, String(value))
    } catch (e) {
      console.error('Failed to save timeout setting:', e)
    }
  }

  function chooseTheme(sel) {
    currentTheme = sel
    setThemeSelection(sel)
  }

  function chooseAccent(hex) {
    currentAccent = hex
    setAccent(hex)
  }

  function handleCustomAccentInput(e) {
    const val = e.target.value
    if (/^#?([a-f\d]{3}){1,2}$/i.test(val)) {
      let hex = val.startsWith('#') ? val : '#' + val
      if (hex.length === 4) hex = '#' + hex.slice(1).split('').map(c => c + c).join('')
      currentAccent = hex
      setAccent(hex)
    }
  }

  // Parent uses these to show/hide the Agent Loop / App DB tabs.
  $effect(() => { onAgentLoopEnabledChange(agentLoopEnabled) })
  $effect(() => { onDbSwitcherEnabledChange(dbSwitcherEnabled) })
</script>

<div class="settings-section">
  <h3>General Settings</h3>
  <p class="section-desc">Application-wide preferences</p>

  <!-- Theme Mode -->
  <div class="form-card">
    <h4>Theme</h4>
    <p class="card-hint">Choose your preferred appearance.</p>
    <div class="theme-options">
      {#each ['light', 'dark', 'system'] as sel}
        <button class="theme-option-btn {currentTheme === sel ? 'active' : ''}" onclick={() => chooseTheme(sel)}>
          <span class="theme-label">{sel[0].toUpperCase() + sel.slice(1)}</span>
        </button>
      {/each}
    </div>
    <p class="theme-hint">System follows your OS appearance setting.</p>
  </div>

  <!-- Accent Color -->
  <div class="form-card">
    <h4>Accent Color</h4>
    <p class="card-hint">Choose a color for buttons, links, and interactive elements.</p>
    <div class="accent-presets">
      {#each accentPresets as preset}
        <button
          class="accent-swatch {currentAccent === preset.hex ? 'active' : ''}"
          style="background: {preset.hex}"
          title="{preset.name}"
          onclick={() => chooseAccent(preset.hex)}
        >
          {#if currentAccent === preset.hex}<span class="check">✓</span>{/if}
        </button>
      {/each}
    </div>
    <div class="custom-accent">
      <label for="custom-accent-input">Custom Color</label>
      <div class="custom-accent-row">
        <input
          id="custom-accent-input"
          type="text"
          class="custom-hex-input"
          placeholder="#0288d1"
          value={currentAccent}
          oninput={handleCustomAccentInput}
        />
      </div>
      <p class="hint">Enter any hex color (e.g. #ff6b9d). Dark mode variant auto-computed.</p>
      {#if accentLuminanceWarning}
        <p class="accent-warning">{accentLuminanceWarning}</p>
      {/if}
    </div>
  </div>

  <!-- UI Scale -->
  <div class="form-card">
    <h4>UI Scale</h4>
    <p class="card-hint">Adjust the size of text and interface elements across the application.</p>
    <div class="scale-options">
      {#each [{ v: 'small', l: 'Small', d: 'Compact layout, more content visible' }, { v: 'medium', l: 'Medium', d: 'Default size, balanced for desktop' }, { v: 'large', l: 'Large', d: 'Larger text, easier to read' }] as opt}
        <button
          class="scale-option-btn {generalScale === opt.v ? 'active' : ''}"
          onclick={() => { generalScale = opt.v; applyScale(opt.v) }}
        >
          <span class="scale-option-label">{opt.l}</span>
          <span class="scale-option-desc">{opt.d}</span>
        </button>
      {/each}
    </div>
  </div>

  <!-- Minimalist Mode -->
  <div class="form-card">
    <h4>Minimalist Mode</h4>
    <p class="card-hint">Focus on the question and the answer. Hides sidebar, toolbar, metadata, and extra buttons across the entire application.</p>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" checked={minimalist} onchange={(e) => onMinimalistChange(e.target.checked)} />
        Enable minimalist mode
      </label>
      <p class="hint">
        You can also toggle with <kbd>⌘⇧M</kbd> (macOS) / <kbd>Ctrl⇧M</kbd> (Windows/Linux).
      </p>
    </div>
  </div>

  <!-- Advanced Toggles -->
  <div class="form-card">
    <h4>Advanced</h4>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" checked={agentLoopEnabled} onchange={toggleAgentLoop} />
        Enable advanced agent loop settings
      </label>
      <p class="hint">
        Customize the prompts, tool descriptions, and instructions your AI model receives. For advanced users who want fine-grained control over model behavior.
      </p>
    </div>
    <div class="checkbox-group" style="margin-top: var(--space-sm);">
      <label>
        <input type="checkbox" checked={dbSwitcherEnabled} onchange={toggleDbSwitcher} />
        Enable Application Database Switching
      </label>
      <p class="hint">
        Create a new blank database or switch YourQL to a different SQLite
        file. Switching restarts the application. Your current database is
        left untouched on disk — you can always switch back.
      </p>
    </div>
  </div>

  <!-- Diagnostic Logging -->
  <div class="form-card">
    <h4>Diagnostic Logging</h4>
    <p class="card-hint">Record application activity to a log file for troubleshooting. Changes take effect after restart.</p>
    <div class="checkbox-group">
      <label>
        <input type="checkbox" checked={loggingEnabled} onchange={toggleLogging} />
        Enable diagnostic logging
      </label>
      <p class="hint">
        Logs are written to ~/.yourql/yourql.log. No sensitive information (API keys, passwords) is included.
      </p>
    </div>
    {#if loggingRestartNotice}
      <div class="restart-notice">
        ⚠ Restart YourQL for this change to take effect.
      </div>
    {/if}
    <div class="inline-actions">
      <button class="btn btn-secondary" onclick={exportLog} disabled={loggingExportStatus === 'exporting'}>
        {loggingExportStatus === 'exporting' ? 'Exporting…' : 'Export Log'}
      </button>
      <button class="btn btn-secondary" onclick={clearLog} disabled={loggingExportStatus === 'clearing'}>
        {loggingExportStatus === 'clearing' ? 'Clearing…' : 'Clear Log'}
      </button>
      {#if loggingExportStatus === 'success'}
        <span class="ok">✓ Exported</span>
      {:else if loggingExportStatus === 'cleared'}
        <span class="ok">✓ Cleared</span>
      {:else if loggingExportStatus === 'empty'}
        <span class="muted">No log data</span>
      {:else if loggingExportStatus === 'error'}
        <span class="bad">Failed</span>
      {/if}
    </div>
  </div>

  <!-- Database Export -->
  <div class="form-card">
    <h4>Export Your Data</h4>
    <p class="card-hint">Download a complete copy of your YourQL database — all conversations, settings, skills, and configurations in a portable format you can open in any spreadsheet app.</p>

    <div class="form-group">
      <label for="export-format">Format</label>
      <select id="export-format" bind:value={exportFormat}>
        <option value="xlsx">Excel (.xlsx) — one workbook, all tables</option>
        <option value="csv">CSV (.zip) — select tables, one file each</option>
      </select>
    </div>

    {#if exportFormat === 'csv'}
      <div class="form-group" style="margin-top: var(--space-sm);">
        <label>Tables to export</label>
        <div class="inline-actions" style="margin-bottom: var(--space-sm);">
          <button class="btn btn-tiny" onclick={selectAllTables}>Select All</button>
          <button class="btn btn-tiny" onclick={selectNoTables}>Select None</button>
        </div>
        <div class="export-table-checklist">
          {#each allExportTables as table}
            <label class="checkbox-group" style="margin-bottom: var(--space-2xs);">
              <input type="checkbox" bind:checked={exportTables[table]} />
              {table}
            </label>
          {/each}
        </div>
      </div>
    {/if}

    <div class="checkbox-group" style="margin-top: var(--space-sm);">
      <label>
        <input type="checkbox" bind:checked={exportIncludeCredentials} />
        Include credentials (API keys and passwords)
      </label>
      {#if exportIncludeCredentials}
        <p class="hint bad">⚠️ This will include your API keys and database passwords in plaintext. Do not share this file.</p>
      {/if}
    </div>

    <div class="inline-actions">
      <button
        class="btn btn-primary"
        onclick={handleExportDatabase}
        disabled={exportStatus === 'exporting' || zeroTablesSelected}
      >
        {exportStatus === 'exporting' ? 'Exporting…' : 'Export Database'}
      </button>
      {#if exportStatus === 'success'}
        <span class="ok">✓ Exported — check your chosen folder</span>
      {:else if exportStatus === 'error'}
        <span class="bad">⚠ Failed: {exportError}</span>
      {/if}
    </div>
  </div>

  <!-- Timeouts -->
  <div class="form-card">
    <h4>Timeouts</h4>
    <p class="card-hint">Maximum time (in seconds) before a long-running operation is cancelled. Increase these if you have a slow LLM provider or complex questions.</p>
    <div class="timeouts-row">
      <div class="timeout-field">
        <label for="timeout-pipeline">Pipeline timeout</label>
        <input id="timeout-pipeline" type="number" min="30" max="3600" bind:value={pipelineTimeout} onchange={() => saveTimeoutSetting('pipeline_timeout_seconds', pipelineTimeout)} />
        <p class="hint">Covers the entire LLM conversation turn. Default 180.</p>
      </div>
      <div class="timeout-field">
        <label for="timeout-summarization">Summarization timeout</label>
        <input id="timeout-summarization" type="number" min="10" max="600" bind:value={summarizationTimeout} onchange={() => saveTimeoutSetting('summarization_timeout_seconds', summarizationTimeout)} />
        <p class="hint">Independent of the pipeline timeout. Default 300.</p>
      </div>
    </div>
  </div>
</div>

<style>
  .card-hint {
    color: var(--text-muted);
    margin-bottom: var(--space-2xl);
    font-size: var(--font-base);
  }
  .theme-options {
    display: flex;
    gap: var(--space-md);
    margin-bottom: var(--space-md);
  }
  .theme-option-btn {
    display: flex; flex-direction: column; align-items: center;
    gap: var(--space-sm);
    padding: var(--space-xl) var(--space-4xl);
    background: var(--bg-tertiary);
    border: 2px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.2s ease;
    min-width: 100px;
  }
  .theme-option-btn:hover { background: var(--bg-surface); border-color: var(--color-accent); }
  .theme-option-btn.active { background: var(--color-accent-light); border-color: var(--color-accent); }
  .theme-label { font-size: var(--font-md); font-weight: 600; color: var(--text-primary); }
  .theme-hint { color: var(--text-muted); font-size: var(--font-sm); margin-top: 0; }

  .accent-presets { display: flex; gap: var(--space-md); margin-bottom: var(--space-2xl); }
  .accent-swatch {
    width: 36px; height: 36px; border-radius: 50%;
    border: 3px solid transparent; cursor: pointer;
    transition: all 0.2s ease; display: flex; align-items: center; justify-content: center;
    padding: 0; outline: none;
  }
  .accent-swatch:hover { transform: scale(1.15); }
  .accent-swatch.active { border-color: var(--text-primary); box-shadow: 0 0 0 2px var(--text-primary); }
  .accent-swatch .check {
    color: #fff; font-size: var(--font-md);
    text-shadow: 0 1px 2px rgba(0,0,0,0.3); font-weight: bold;
  }

  .custom-accent { margin-top: var(--space-md); }
  .custom-accent label {
    display: block; font-size: var(--font-sm); font-weight: 600;
    color: var(--text-secondary); margin-bottom: var(--space-sm);
  }
  .custom-accent-row { display: flex; align-items: center; gap: var(--space-md); }
  .custom-hex-input {
    padding: var(--space-md) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    font-size: var(--font-base);
    font-family: var(--font-mono);
    width: 200px;
    background: var(--bg-input);
    color: var(--text-primary);
  }
  .custom-hex-input:focus { outline: none; border-color: var(--color-accent); }

  .hint {
    color: var(--text-tertiary); font-size: var(--font-xs);
    margin: var(--space-2xs) 0 0;
  }
  kbd {
    background: var(--bg-secondary); padding: 1px 5px; border-radius: 3px;
    border: 1px solid var(--border-primary); font-size: var(--font-2xs);
  }
  .accent-warning {
    color: #f57c00; font-size: var(--font-sm); margin-top: var(--space-md);
    padding: var(--space-sm) var(--space-lg);
    background: rgba(245, 124, 0, 0.1);
    border: 1px solid rgba(245, 124, 0, 0.25);
    border-radius: var(--radius-md);
  }

  .scale-options { display: flex; flex-direction: column; gap: var(--space-md); }
  .scale-option-btn {
    display: flex; flex-direction: column; align-items: flex-start;
    width: 100%;
    padding: var(--space-xl) var(--space-3xl);
    background: var(--bg-tertiary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
  }
  .scale-option-btn:hover { background: var(--border-secondary); border-color: var(--color-accent); }
  .scale-option-btn.active { background: var(--color-accent-light); border-color: var(--color-accent); border-width: 2px; }
  .scale-option-label { font-size: var(--font-md); font-weight: 600; color: var(--text-primary); margin-bottom: var(--space-2xs); }
  .scale-option-desc { font-size: var(--font-sm); color: var(--text-tertiary); }

  .restart-notice {
    margin-top: var(--space-xs);
    padding: var(--space-md) var(--space-xl);
    background: var(--color-accent-light);
    border: 1px solid var(--color-accent-border);
    border-radius: var(--radius-md);
    color: var(--color-accent);
    font-size: var(--font-sm);
  }

  .inline-actions {
    margin-top: var(--space-sm);
    display: flex; align-items: center; gap: var(--space-sm);
    flex-wrap: wrap;
  }
  .ok { color: var(--color-success); font-size: var(--font-sm); }
  .muted { color: var(--text-tertiary); font-size: var(--font-sm); }
  .bad { color: var(--color-danger); font-size: var(--font-sm); }

  .export-table-checklist {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-2xs) var(--space-lg);
    font-size: var(--font-sm);
    padding: var(--space-sm) var(--space-md);
    background: var(--bg-primary);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-sm);
    max-height: 12rem;
    overflow-y: auto;
  }

  .timeouts-row {
    display: flex;
    gap: var(--space-md);
    flex-wrap: wrap;
  }
  .timeout-field {
    flex: 1;
    min-width: 200px;
    margin-bottom: var(--space-lg);
  }
  .timeout-field input[type="number"] {
    width: 100%;
    padding: var(--space-md) var(--space-lg);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-base);
    box-sizing: border-box;
  }
</style>
