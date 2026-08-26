<script>
  // Skills tab — extracted verbatim from SettingsView.
  import { ListSkills, CreateSkill, UpdateSkill, DeleteSkill, SetSkillActive } from '../../../../wailsjs/go/main/App.js'

  let { onUpdate = () => {} } = $props()

  let skills = $state([])
  let skillEditor = $state(null)
  let showSkillEditor = $state(false)

  loadSkills()

  async function loadSkills() {
    try {
      skills = await ListSkills()
    } catch (e) {
      console.error('Failed to load skills:', e)
    }
  }

  async function handleSaveSkill() {
    if (!skillEditor.name.trim()) return
    try {
      if (skillEditor.id) {
        await UpdateSkill(skillEditor.id, skillEditor.name, skillEditor.markdown_content)
      } else {
        await CreateSkill(skillEditor.name, skillEditor.markdown_content)
      }
      showSkillEditor = false
      skillEditor = null
      await loadSkills()
      onUpdate()
    } catch (e) {
      console.error('Failed to save skill:', e)
    }
  }

  async function handleDeleteSkill(id) {
    try {
      await DeleteSkill(id)
      await loadSkills()
      onUpdate()
    } catch (e) {
      console.error('Failed to delete skill:', e)
    }
  }

  async function handleToggleSkillActive(id, active) {
    try {
      await SetSkillActive(id, active)
      const skill = skills.find(s => s.id === id)
      if (skill) skill.is_active = active
      skills = [...skills]
      onUpdate()
    } catch (e) {
      console.error('Failed to toggle skill:', e)
    }
  }

  function openEditor(skill = null) {
    skillEditor = skill ? { ...skill } : { id: null, name: '', markdown_content: '' }
    showSkillEditor = true
  }
</script>

<div class="settings-section">
  <h3>Skills</h3>
  <p class="section-desc">Skills are markdown text blocks added to the system prompt. Use them to give the LLM domain context or behavioral guidance.</p>

  <div class="form-card" style="margin-bottom: var(--space-lg);">
    <button class="btn btn-primary" onclick={() => openEditor()}>
      + New Skill
    </button>
  </div>

  {#if skills.length === 0}
    <div style="color: var(--text-tertiary); text-align: center; padding: var(--space-2xl);">
      No skills configured yet. Create one to add context to your conversations.
    </div>
  {:else}
    {#each skills as skill (skill.id)}
      <div class="skill-card">
        <div class="skill-card-main">
          <div class="skill-card-name">{skill.name}</div>
          <div class="skill-card-preview">{skill.markdown_content || '(empty content)'}</div>
        </div>
        <div class="skill-card-actions">
          <button onclick={() => openEditor(skill)}>Edit</button>
          <button class="delete" onclick={() => handleDeleteSkill(skill.id)}>Delete</button>
          <label class="toggle-switch">
            <input type="checkbox" checked={skill.is_active} onchange={() => handleToggleSkillActive(skill.id, !skill.is_active)} />
            <span class="slider"></span>
          </label>
        </div>
      </div>
    {/each}
  {/if}
</div>

{#if showSkillEditor && skillEditor}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="skill-editor-overlay" role="dialog" aria-modal="true" onclick={() => { showSkillEditor = false; skillEditor = null }} onkeydown={(e) => { if (e.key === 'Escape') { showSkillEditor = false; skillEditor = null } }}>
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="skill-editor" role="document" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      <h3>{skillEditor.id ? 'Edit Skill' : 'New Skill'}</h3>
      <label for="skill-editor-name">Name</label>
      <input type="text" id="skill-editor-name" bind:value={skillEditor.name} placeholder="Sales Domain Context" />
      <label for="skill-editor-content">Markdown Content</label>
      <textarea id="skill-editor-content" bind:value={skillEditor.markdown_content} placeholder="Revenue is in USD. Fiscal year starts July 1.&#10;Exclude test accounts from all queries."></textarea>
      <div class="skill-editor-actions">
        <button class="btn btn-primary" onclick={handleSaveSkill}>Save</button>
        <button class="btn btn-secondary" onclick={() => { showSkillEditor = false; skillEditor = null }}>Cancel</button>
      </div>
    </div>
  </div>
{/if}
