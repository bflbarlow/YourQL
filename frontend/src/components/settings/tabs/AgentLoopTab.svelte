<script>
  // Agent Loop tab — extracted verbatim from SettingsView.
  import { GetAgentLoopConfig, SetAgentLoopConfigKey, ResetAgentLoopConfigKey, ResetAllAgentLoopConfig } from '../../../../wailsjs/go/main/App.js'
  import { confirm } from '../../../lib/confirm.svelte.js'

  let agentLoopConfigValues = $state({})
  let agentLoopFields = $state([])
  let agentLoopStatus = $state('')

  const sectionNames = ['tool_descriptions', 'instructions', 'safety', 'charts', 'persona', 'responses']
  const sectionLabels = {
    tool_descriptions: 'Tool Descriptions',
    instructions: 'System Instructions',
    safety: 'Exploration Safety Rules',
    charts: 'Chart Guidance',
    persona: 'Fallback Persona',
    responses: 'Tool Response Messages'
  }

  loadAgentLoopConfig()

  function configKeyToFieldName(key) {
    const map = {
      'tool.query_database.description': 'tool_query_database_desc',
      'tool.query_database.params.sql.description': 'tool_query_database_sql_desc',
      'tool.query_database.params.is_exploration.description': 'tool_query_database_is_exploration_desc',
      'tool.query_database.params.reasoning.description': 'tool_query_database_reasoning_desc',
      'tool.respond_to_user.description': 'tool_respond_to_user_desc',
      'tool.respond_to_user.params.text.description': 'tool_respond_to_user_text_desc',
      'tool.render_chart.description': 'tool_render_chart_desc',
      'tool.render_chart.params.chart_config.description': 'tool_render_chart_config_desc',
      'instructions.1': 'instruction_1',
      'instructions.2': 'instruction_2',
      'instructions.2a': 'instruction_2a',
      'instructions.2b': 'instruction_2b',
      'instructions.2c': 'instruction_2c',
      'instructions.3': 'instruction_3',
      'instructions.4': 'instruction_4',
      'instructions.5': 'instruction_5',
      'instructions.6': 'instruction_6',
      'safety.preamble': 'safety_preamble',
      'safety.strict.rules': 'safety_strict',
      'safety.moderate.rules': 'safety_moderate',
      'safety.relaxed.rules': 'safety_relaxed',
      'safety.footer': 'safety_footer',
      'safety.oneshot': 'safety_oneshot',
      'charts.intro': 'charts_intro',
      'charts.format': 'charts_format',
      'charts.timing': 'charts_timing',
      'persona.fallback': 'persona_fallback',
      'response.parse_error': 'response_parse_error',
      'response.exploration_exhausted': 'response_exploration_exhausted',
      'response.safety_rejected': 'response_safety_rejected',
      'response.oneshot_violation': 'response_oneshot_violation',
      'response.unknown_tool': 'response_unknown_tool',
      'response.render_chart_no_pending': 'response_render_chart_no_pending',
      'response.render_chart_parse_error': 'response_render_chart_parse_error',
      'response.respond_parse_error': 'response_respond_parse_error',
      'response.loop_exhausted': 'response_loop_exhausted',
      'response.empty_truncated': 'response_empty_truncated'
    }
    return map[key] || key
  }

  async function loadAgentLoopConfig() {
    try {
      const result = await GetAgentLoopConfig()
      agentLoopFields = result.fields || []
      const cfg = result.config || {}
      const vals = {}
      for (const f of agentLoopFields) {
        const fieldName = configKeyToFieldName(f.key)
        vals[f.key] = cfg[fieldName] || ''
      }
      agentLoopConfigValues = vals
    } catch (e) {
      console.error('Failed to load agent loop config:', e)
    }
  }

  async function saveAgentLoopField(key, value) {
    agentLoopStatus = ''
    try {
      await SetAgentLoopConfigKey(key, value)
      agentLoopStatus = 'Saved'
      setTimeout(() => { agentLoopStatus = '' }, 1500)
    } catch (e) {
      agentLoopStatus = 'Error: ' + e.toString()
    }
  }

  async function resetAgentLoopField(key) {
    try {
      await ResetAgentLoopConfigKey(key)
      await loadAgentLoopConfig()
    } catch (e) {
      console.error('Failed to reset field:', e)
    }
  }

  async function resetAllAgentLoop() {
    const ok = await confirm({
      title: 'Reset Agent Loop Settings',
      body: 'This will reset all Agent Loop settings to their defaults. This cannot be undone. Continue?',
      confirmLabel: 'Reset All',
      danger: true
    })
    if (!ok) return
    try {
      await ResetAllAgentLoopConfig()
      await loadAgentLoopConfig()
      agentLoopStatus = 'All settings restored to defaults'
      setTimeout(() => { agentLoopStatus = '' }, 3000)
    } catch (e) {
      agentLoopStatus = 'Error: ' + e.toString()
    }
  }
</script>

<div class="settings-section">
  <h3>Agent Loop</h3>
  <p class="section-desc">
    Customize what your AI model receives. These are advanced settings — changes here directly affect how the model behaves.
  </p>

  <div class="agent-loop-warning">
    <strong>⚠ Use with caution.</strong> Changing prompts or tool descriptions in ways that contradict YourQL's designed functionality — such as removing instructions about read-only queries, altering the one-shot finality rule, or rewriting parameter descriptions to claim capabilities the tools don't have — will cause models to use tools incorrectly and may break core application behavior. If things go wrong, use <strong>Restore All Defaults</strong> below.
  </div>

  {#if agentLoopStatus}
    <div class="status-message {agentLoopStatus.startsWith('Error') ? 'error' : 'success'}">
      {agentLoopStatus}
    </div>
  {/if}

  <button class="btn btn-secondary" onclick={resetAllAgentLoop} style="margin-bottom: var(--space-4xl);">
    Restore All Defaults
  </button>

  {#each sectionNames as section}
    {@const sectionFields = agentLoopFields.filter(f => f.section === section)}
    {#if sectionFields.length > 0}
      <div class="form-card agent-loop-section">
        <h4>{sectionLabels[section] || section}</h4>
        {#each sectionFields as field (field.key)}
          <div class="form-group" style="margin-bottom: var(--space-3xl);">
            <label for="agent-loop-{field.key}">{field.label}</label>
            <div class="field-tooltip">{field.description}</div>
            <textarea
              id="agent-loop-{field.key}"
              class="agent-loop-textarea"
              value={agentLoopConfigValues[field.key] || ''}
              placeholder={field.description}
              rows={field.key.startsWith('safety.') || field.key.startsWith('charts.') ? 3 : 2}
              onblur={(e) => saveAgentLoopField(field.key, e.target.value)}
            ></textarea>
            <button
              class="btn btn-small"
              onclick={() => resetAgentLoopField(field.key)}
              style="margin-top: var(--space-sm);"
            >
              Reset
            </button>
          </div>
        {/each}
      </div>
    {/if}
  {/each}
</div>
