<script>
  // SettingsView — now a slim shell: tab bar + tab components.
  // All tab logic lives in components/settings/tabs/*.
  import ModelsTab from './tabs/ModelsTab.svelte'
  import DatabasesTab from './tabs/DatabasesTab.svelte'
  import SkillsTab from './tabs/SkillsTab.svelte'
  import DefaultsTab from './tabs/DefaultsTab.svelte'
  import GeneralTab from './tabs/GeneralTab.svelte'
  import AgentLoopTab from './tabs/AgentLoopTab.svelte'
  import AppDbTab from './tabs/AppDbTab.svelte'
  import './settings.css'

  let {
    llmProviders = [],
    dataSources = [],
    onUpdate = () => {},
    minimalist = false,
    onMinimalistChange = () => {}
  } = $props()

  let activeSettingsTab = $state('models')
  let agentLoopEnabled = $state(false)
  let dbSwitcherEnabled = $state(false)

  function handleAgentLoopEnabledChange(enabled) {
    agentLoopEnabled = enabled
    if (!enabled && activeSettingsTab === 'agentloop') activeSettingsTab = 'general'
  }
  function handleDbSwitcherEnabledChange(enabled) {
    dbSwitcherEnabled = enabled
    if (!enabled && activeSettingsTab === 'appdb') activeSettingsTab = 'general'
  }
</script>

<div class="settings-container">
  <div class="settings-tabs">
    <button
      class="tab-btn {activeSettingsTab === 'models' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'models'}
    >
      Model Configurations
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'databases' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'databases'}
    >
      Data Sources
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'skills' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'skills'}
    >
      Skills
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'defaults' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'defaults'}
    >
      Defaults
    </button>
    <button
      class="tab-btn {activeSettingsTab === 'general' ? 'active' : ''}"
      onclick={() => activeSettingsTab = 'general'}
    >
      General
    </button>
    {#if agentLoopEnabled}
      <button
        class="tab-btn {activeSettingsTab === 'agentloop' ? 'active' : ''}"
        onclick={() => activeSettingsTab = 'agentloop'}
      >
        Agent Loop
      </button>
    {/if}
    {#if dbSwitcherEnabled}
      <button
        class="tab-btn {activeSettingsTab === 'appdb' ? 'active' : ''}"
        onclick={() => activeSettingsTab = 'appdb'}
      >
        App Database
      </button>
    {/if}
  </div>

  <div class="settings-content">
    {#if activeSettingsTab === 'models'}
      <ModelsTab {llmProviders} {onUpdate} />
    {:else if activeSettingsTab === 'databases'}
      <DatabasesTab {dataSources} {onUpdate} />
    {:else if activeSettingsTab === 'skills'}
      <SkillsTab {onUpdate} />
    {:else if activeSettingsTab === 'defaults'}
      <DefaultsTab {llmProviders} {dataSources} />
    {:else if activeSettingsTab === 'general'}
      <GeneralTab
        {minimalist}
        {onMinimalistChange}
        onAgentLoopEnabledChange={handleAgentLoopEnabledChange}
        onDbSwitcherEnabledChange={handleDbSwitcherEnabledChange}
      />
    {:else if activeSettingsTab === 'agentloop'}
      <AgentLoopTab />
    {:else if activeSettingsTab === 'appdb'}
      <AppDbTab />
    {/if}
  </div>
</div>
