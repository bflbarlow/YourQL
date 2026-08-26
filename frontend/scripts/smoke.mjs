// Headless smoke test: load the built bundle in happy-dom with stubbed
// Wails runtime/bindings, mount, click through views, and surface any error.
import { Window } from 'happy-dom'
import { readdirSync } from 'fs'
import { pathToFileURL } from 'url'
import { resolve } from 'path'

const win = new Window()
const document = win.document

globalThis.window = win
globalThis.document = document
Object.defineProperty(globalThis, 'navigator', { value: win.navigator, configurable: true })
globalThis.localStorage = win.localStorage
globalThis.CustomEvent = win.CustomEvent
globalThis.Event = win.Event
globalThis.HTMLElement = win.HTMLElement
globalThis.Text = win.Text
globalThis.Comment = win.Comment
globalThis.SVGElement = win.SVGElement
globalThis.HTMLMediaElement = win.HTMLMediaElement
globalThis.HTMLDetailsElement = win.HTMLDetailsElement
globalThis.DocumentFragment = win.DocumentFragment
globalThis.MouseEvent = win.MouseEvent
globalThis.HTMLCanvasElement = win.HTMLCanvasElement
globalThis.Element = win.Element
globalThis.Node = win.Node
globalThis.requestAnimationFrame = (cb) => setTimeout(cb, 0)
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} })
globalThis.ResizeObserver = class { observe(){} unobserve(){} disconnect(){} }
globalThis.MutationObserver = class { observe(){} disconnect(){} }
globalThis.getComputedStyle = win.getComputedStyle.bind(win)

// Stub Wails runtime
const listeners = {}
win.runtime = {
  EventsOn: (name, cb) => win.runtime.EventsOnMultiple(name, cb, -1),
  EventsOnMultiple: (name, cb) => { (listeners[name] ||= []).push(cb); return () => {} },
  EventsOff: (...names) => names.forEach(n => delete listeners[n]),
  EventsOff: () => {},
  EventsEmit: (name, ...args) => (listeners[name] || []).forEach(cb => cb(...args)),
  BrowserOpenURL: () => {}
}
globalThis.runtime = win.runtime

// Stub Wails Go bindings — every method returns a resolved promise with
// plausible data so the UI can render real flows.
let convSeq = 1
const makeConv = (title) => ({
  id: convSeq++, title, status: 'active', pinned: false, tags: [],
  llm_provider_id: null, data_source_id: null,
  max_messages: 0, max_context_messages: 5,
  tech_details: false, context_details: false, summarize: true,
  viz_enabled: true, streaming_enabled: false,
  created_at: new Date().toISOString(), updated_at: new Date().toISOString()
})
let provSeq=1, dsSeq=1
const conversations = [makeConv('Alpha'), makeConv('Beta')]
conversations[0].max_messages = 50
let msgSeq = 1
const messagesByConv = {}
messagesByConv[conversations[0].id] = Array.from({ length: 81 }, (_, i) => ({
  id: msgSeq++,
  role: i % 5 === 0 ? 'user' : (i % 5 === 1 ? 'assistant' : 'exploration'),
  content: i % 5 === 0 ? `Question ${i}` : (i % 5 === 1 ? `<p>Answer ${i}</p>` : 'Exploring schema…'),
  created_at: new Date(Date.now() - (100 - i) * 60000).toISOString(),
  metadata: null
}))
conversations[0].llm_provider_id = 7
conversations[0].data_source_id = 9
conversations[0].tags = ['ops']
conversations[0].pinned = true
win.go = {
  main: {
    App: {
      ListConversations: async () => conversations,
      GetConversationMessages: async (id) => messagesByConv[id] || [],
      ListLLMProviders: async () => [{ id: 7, name: 'GPT', provider: 'openai', model: 'gpt-4', maxTokens: 4096, is_default: true }],
      ListDataSources: async () => [{ id: 9, name: 'MainDB', type: 'mysql', host: 'x', port: 3306, database: 'db', is_default: true, exploration_allowed: true }],
      UpdateConversationPinned: async (id, v) => { (win.__calls ||= []).push(['pinned', id, v]); const c = conversations.find(c=>c.id===id); if(c) c.pinned = v },
      UpdateConversationTechDetails: async (id, v) => { (win.__calls ||= []).push(['tech', id, v]); const c = conversations.find(c=>c.id===id); if(c) c.tech_details = v },
      UpdateConversationSummarize: async (id, v) => { (win.__calls ||= []).push(['summarize', id, v]); const c = conversations.find(c=>c.id===id); if(c) c.summarize = v },
      UpdateConversationTitle: async (id, v) => { (win.__calls ||= []).push(['title', id, v]); const c = conversations.find(c=>c.id===id); if(c) c.title = v; return { title: v } },
      UpdateConversationSettings: async () => {},
      ListConversations: async () => conversations,
      ListSkills: async () => [],
      GetDiscussionDefaults: async () => ({}),
      CreateConversation: async (title) => { const c = makeConv(title); conversations.push(c); return c },
      ProcessUserMessage: async () => {},
      GetAppVersion: async () => 'test',
      CheckForUpdate: async () => ({ update_available: false }),
      DownloadUpdate: async () => {},
      PerformUpgradeRestart: async () => {},
      GetAppSetting: async () => '',
      SetAppSetting: async () => {},
      GetGeneralSettings: async () => ({}),
      UpdateGeneralSettings: async () => {},
      GetTagsForConversation: async () => [],
      ListAllTags: async () => [],
      GetConversationSkillIDs: async () => []
    }
  }
}
globalThis.go = win.go

process.on('unhandledRejection', (e) => {
  console.error('UNHANDLED REJECTION:', e)
})

document.body.innerHTML = '<div id="app"></div>'

const jsFile = readdirSync(resolve(process.cwd(), 'dist/assets')).find(f => f.endsWith('.js'))
const mod = await import(pathToFileURL(resolve(process.cwd(), 'dist/assets', jsFile)))

// Let microtasks + effects settle
await new Promise(r => setTimeout(r, 300))

const click = (el) => el.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
const q = (sel) => document.querySelector(sel)
const qa = (sel) => [...document.querySelectorAll(sel)]

function log(label) {
  console.log('---', label, '| activeView content:', q('#app')?.textContent?.slice(0, 80).replace(/\s+/g, ' '))
}

log('mounted')

// 1. Should show discussion list with two rows
const rows = qa('.conversation-item')
console.log('discussion rows:', rows.length)
if (rows.length === 0) { console.error('FAIL: no discussion rows rendered'); process.exit(1) }

// 2. Click first row -> should navigate to conversation view
click(rows[0])
await new Promise(r => setTimeout(r, 300))
log('after row click')
console.log('banner:', !!q('.collapsed-messages-banner'), '| empty:', !!q('.empty-conversation'), '| bubbles:', qa('.message').length)
// Type into composer and send
const ta = q('.message-input-wrapper textarea')
if (ta) {
  ta.value = 'How many rows in users?'
  ta.dispatchEvent(new win.Event('input', { bubbles: true }))
  await new Promise(r => setTimeout(r, 100))
  const send = q('.send-btn')
  click(send)
  await new Promise(r => setTimeout(r, 400))
  console.log('after send | banner:', !!q('.collapsed-messages-banner'), '| empty:', !!q('.empty-conversation'), '| user bubbles:', qa('.message.user').length)
  console.log(qa('.message.user').slice(-1)[0]?.textContent?.trim().slice(0, 60))
}
if (!q('.conversation-view')) {
  console.error('FAIL: conversation view did not open after clicking a row')
  console.error('app innerHTML head:', q('#app').innerHTML.slice(0, 500))
  process.exit(1)
}

// 3. Back button
click(q('.back-btn'))
await new Promise(r => setTimeout(r, 300))
log('after back')
if (!q('.conversations-list')) { console.error('FAIL: list did not return'); process.exit(1) }

// 4. Click into the second discussion
const rows2 = qa('.conversation-item')
click(rows2[1])
await new Promise(r => setTimeout(r, 300))
log('after second click')
console.log(q('.conversation-view') ? 'PASS: second discussion opened' : 'FAIL: second discussion did not open')

// 5. Navigate around: back -> settings -> tabs -> discussions -> click row
click(q('.back-btn'))
await new Promise(r => setTimeout(r, 200))
const navBtns = () => qa('.nav-item')
click(navBtns().find(b => b.textContent.includes('Settings')))
await new Promise(r => setTimeout(r, 300))
log('settings open')
for (const t of qa('.tab-btn')) {
  click(t)
  await new Promise(r => setTimeout(r, 150))
}
console.log('tabs cycled OK, last:', q('.tab-btn.active')?.textContent?.trim())
click(navBtns().find(b => b.textContent.includes('Discussions')))
await new Promise(r => setTimeout(r, 300))
log('back on discussions')

const rows3 = qa('.conversation-item')
console.log('rows now:', rows3.length)
if (rows3.length === 0) { console.error('FAIL: rows disappeared'); process.exit(1) }
click(rows3[0])
await new Promise(r => setTimeout(r, 400))
log('after post-settings click')
console.log(q('.conversation-view') ? 'PASS: opened after settings roundtrip' : 'FAIL: cannot open discussion after settings roundtrip')

// 6. New Discussion modal open/cancel
click(q('.back-btn')); await new Promise(r => setTimeout(r, 200))
click(q('.view-header-actions .btn-primary'))
await new Promise(r => setTimeout(r, 200))
console.log('modal open:', !!q('.dialog'))
const cancel = [...qa('.dialog button')].find(b => b.textContent.trim() === 'Cancel')
click(cancel); await new Promise(r => setTimeout(r, 200))

// 7. Conversation settings dialog via gear IconBtn then close
const gear = document.querySelector('.conversation-row-actions button:last-child')
click(gear); await new Promise(r => setTimeout(r, 300))
console.log('settings dialog open:', !!q('[role="dialog"]'))
const x = q('.icon-close')
if (x) { click(x); await new Promise(r => setTimeout(r, 200)) }

// final sanity: still able to click into a discussion?
const rows4 = qa('.conversation-item')
if (rows4.length) {
  click(rows4[0]); await new Promise(r => setTimeout(r, 300))
  console.log(q('.conversation-view') ? 'PASS: final open works' : 'FAIL: final open broken')
}
// ===== Settings-dialog multi-change + live-propagation scenario =====
// Open a discussion that HAS messages, then open its settings from the
// thread header so we can verify changes propagate to the live view.
click(q('.back-btn')); await new Promise(r => setTimeout(r, 200))
click(qa('.conversation-item')[0]); await new Promise(r => setTimeout(r, 300))
const bubblesBefore = qa('.exploration-result').length
console.log('thread exploration blocks visible:', bubblesBefore)

win.__calls = []
const threadGear = [...qa('button')].find(b => b.getAttribute('aria-label') === 'Conversation settings')
click(threadGear); await new Promise(r => setTimeout(r, 300))
console.log('dialog open:', !!q('[role="dialog"]'))

// Change 1+2: tech details on, then off, then ON again (multi-change stickiness)
const boxes = () => [...qa('[role="dialog"] input[type="checkbox"]')]
const techBox = boxes()[1]
click(techBox); await new Promise(r => setTimeout(r, 150))
click(techBox); await new Promise(r => setTimeout(r, 150))
click(techBox); await new Promise(r => setTimeout(r, 250))
console.log('after 3 toggles | tech box:', techBox.checked,
  '| backend calls:', JSON.stringify(win.__calls.filter(c => c[0]==='tech').map(c=>c[2])),
  '| exploration visible in thread behind dialog:', qa('.exploration-result').length > bubblesBefore)

// Change 4: rename
const ren = q('#cs-rename')
ren.value = 'Renamed!'
ren.dispatchEvent(new win.Event('input', { bubbles: true }))
await new Promise(r => setTimeout(r, 100))
ren.dispatchEvent(new win.FocusEvent('blur'))
await new Promise(r => setTimeout(r, 200))
console.log('rename persisted call:', JSON.stringify(win.__calls.filter(c=>c[0]==='title')))

// Change 5: summarize toggle (different field, same session)
const sumBox = boxes()[3]
click(sumBox); await new Promise(r => setTimeout(r, 200))
console.log('summarize now:', sumBox.checked, '| calls:', JSON.stringify(win.__calls.filter(c=>c[0]==='summarize')))

// Close, reopen: values must reflect what we set (no stale revert)
const closeX = q('.icon-close')
click(closeX); await new Promise(r => setTimeout(r, 400))
console.log('thread title after rename:', q('.conversation-info h3')?.textContent?.trim())
click([...qa('button')].find(b => b.getAttribute('aria-label') === 'Conversation settings'))
await new Promise(r => setTimeout(r, 300))
console.log('reopened | rename field:', q('#cs-rename')?.value)
boxes().forEach((b,i)=>console.log('  box',i,'checked=',b.checked))
click(q('.icon-close')); await new Promise(r => setTimeout(r, 200))

console.log('SMOKE DONE')
