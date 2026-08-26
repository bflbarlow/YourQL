<script>
  // App Database tab — extracted verbatim from SettingsView.
  import {
    GetActiveDatabaseInfo, PickNewDatabaseLocation, PickExistingDatabaseFile,
    CreateAndSwitchToNewDatabase, SwitchToExistingDatabase, ResetToDefaultDatabase
  } from '../../../../wailsjs/go/main/App.js'

  let activeDbPath = $state('')
  let activeDbIsDefault = $state(true)
  let newDbPath = $state('')
  let switchToPath = $state('')
  let dbConfirm = $state(null)
  let dbError = $state('')

  loadDbSwitcherInfo()

  async function loadDbSwitcherInfo() {
    try {
      const info = await GetActiveDatabaseInfo()
      activeDbPath = info.path
      activeDbIsDefault = info.is_default
    } catch (e) {
      console.error('Failed to load database info:', e)
    }
  }

  async function pickNewDbLocation() {
    try {
      const path = await PickNewDatabaseLocation()
      if (path) newDbPath = path
    } catch (e) {
      console.error('PickNewDatabaseLocation:', e)
    }
  }

  async function pickExistingDbFile() {
    try {
      const path = await PickExistingDatabaseFile()
      if (path) switchToPath = path
    } catch (e) {
      console.error('PickExistingDatabaseFile:', e)
    }
  }

  function createAndSwitch() {
    if (!newDbPath) return
    dbError = ''
    dbConfirm = {
      title: 'Create New Database',
      message: 'This creates a brand-new, empty YourQL database and restarts the app. Nothing in your current database is deleted.',
      action: 'create',
      path: newDbPath
    }
  }

  function switchToExisting() {
    if (!switchToPath) return
    dbError = ''
    dbConfirm = {
      title: 'Switch Database',
      message: 'Switch YourQL to "' + switchToPath + '" and restart?',
      action: 'switch',
      path: switchToPath
    }
  }

  function resetToDefault() {
    dbError = ''
    dbConfirm = {
      title: 'Reset to Default',
      message: 'Return YourQL to its default database (~/.yourql/yourql.db) and restart?',
      action: 'reset',
      path: ''
    }
  }

  function cancelDbConfirm() { dbConfirm = null }

  async function confirmDbAction() {
    const c = dbConfirm
    dbConfirm = null
    if (!c) return
    dbError = ''
    try {
      if (c.action === 'create') {
        await CreateAndSwitchToNewDatabase(c.path)
      } else if (c.action === 'switch') {
        await SwitchToExistingDatabase(c.path)
      } else if (c.action === 'reset') {
        await ResetToDefaultDatabase()
      }
    } catch (e) {
      dbError = e.message || String(e)
    }
  }
</script>

<div class="settings-section">
  <h3>Application Database</h3>
  <p class="section-desc">
    YourQL stores conversations, provider configs, data source configs,
    and skills in a local SQLite database. Switching creates a new blank
    database or points YourQL at a different file — the application
    restarts after switching.
  </p>

  {#if dbError}
    <div class="db-error">{dbError}</div>
  {/if}

  <div class="form-card">
    <p class="card-hint" style="margin-bottom: var(--space-sm);">
      <strong>Current database:</strong><br />
      {activeDbPath}
      {#if activeDbIsDefault}
        <span style="color: var(--text-tertiary);"> (default)</span>
      {/if}
    </p>
  </div>

  <div class="form-card">
    <h4>Create New Blank Database</h4>
    <p class="card-hint">
      Creates a brand-new, empty YourQL database at a location you
      choose — like launching the app for the very first time. Nothing
      in your current database is deleted.
    </p>
    <div class="db-actions-row">
      <input type="text" class="form-input" placeholder="Path to new database (e.g. ~/Documents/yourql-new.db)" bind:value={newDbPath} />
      <button class="btn btn-secondary" onclick={pickNewDbLocation}>Browse...</button>
      <button class="btn btn-primary" onclick={createAndSwitch} disabled={!newDbPath}>Create &amp; Restart</button>
    </div>
  </div>

  <div class="form-card">
    <h4>Switch to Existing Database</h4>
    <p class="card-hint">Point YourQL at an existing SQLite database file and restart.</p>
    <div class="db-actions-row">
      <input type="text" class="form-input" placeholder="Path to existing database" bind:value={switchToPath} />
      <button class="btn btn-secondary" onclick={pickExistingDbFile}>Browse...</button>
      <button class="btn btn-primary" onclick={switchToExisting} disabled={!switchToPath}>Switch &amp; Restart</button>
    </div>
  </div>

  {#if !activeDbIsDefault}
    <div class="form-card">
      <h4>Reset to Default</h4>
      <p class="card-hint">Return YourQL to its default database location (~/.yourql/yourql.db) and restart.</p>
      <button class="btn btn-secondary" onclick={resetToDefault}>Reset to Default &amp; Restart</button>
    </div>
  {/if}

  <p class="hint">
    ⚠ Switching databases always restarts YourQL. Any in-progress
    conversations will be interrupted. Your current database is left
    untouched on disk — you can always switch back.
  </p>
</div>

{#if dbConfirm}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="skill-editor-overlay" role="dialog" aria-modal="true" onclick={cancelDbConfirm} onkeydown={(e) => { if (e.key === 'Escape') cancelDbConfirm() }}>
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="skill-editor" role="document" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      <h3>{dbConfirm.title}</h3>
      <p style="margin-bottom: var(--space-md);">{dbConfirm.message}</p>
      <div class="confirm-actions">
        <button class="btn btn-primary" onclick={confirmDbAction}>Confirm &amp; Restart</button>
        <button class="btn btn-secondary" onclick={cancelDbConfirm}>Cancel</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .card-hint {
    color: var(--text-muted);
    margin-bottom: var(--space-2xl);
    font-size: var(--font-base);
  }
  .db-error {
    margin-bottom: var(--space-md);
    padding: var(--space-md) var(--space-xl);
    background: rgba(239, 83, 80, 0.1);
    border: 1px solid rgba(239, 83, 80, 0.3);
    border-radius: var(--radius-md);
    color: var(--color-danger);
    font-size: var(--font-sm);
  }
  .db-actions-row {
    display: flex;
    gap: var(--space-sm);
    flex-wrap: wrap;
  }
  .form-input {
    flex: 1;
    min-width: 250px;
    padding: var(--space-lg) var(--space-xl);
    border: 1px solid var(--border-primary);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-base);
    box-sizing: border-box;
  }
  .form-input:focus { outline: none; border-color: var(--color-accent); }
  .hint {
    color: var(--text-tertiary); font-size: var(--font-xs);
    margin-top: var(--space-md);
  }
  .confirm-actions {
    display: flex; gap: var(--space-md); justify-content: flex-end;
  }
</style>
