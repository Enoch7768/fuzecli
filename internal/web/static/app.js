const $ = id => document.getElementById(id)
const conversation = $('conversation')
const prompt = $('prompt')
const send = $('send')
const fileInput = $('fileInput')
const attachmentsNode = $('attachments')
const composerWrap = $('composerWrap')
const dropOverlay = $('dropOverlay')
const providerSelect = $('providerSelect')
const modelInput = $('modelInput')
const modelOptions = $('modelOptions')
const providerSettings = $('providerSettings')
const modelSettings = $('modelSettings')
const modelSettingsOptions = $('modelSettingsOptions')
const sidebar = $('sidebar')
const historyList = $('historyList')
const toast = $('toast')
const composerStatus = $('composerStatus')

const STORAGE_KEY = 'fuzecli.web.chats.v2'
const MAX_ATTACHMENT_BYTES = 64 * 1024
const MAX_ATTACHMENTS = 6
const MAX_ATTACHMENT_TOTAL = 256 * 1024

let settings = null
let selectedProvider = ''
let selectedModel = ''
let busy = false
let activeAssistant = null
let activeAssistantRaw = ''
let thinkingNode = null
let queuedFiles = []
let currentChat = null
let externalContext = false
let source = null
let toastTimer = null

function escapeHTML(value) {
  return String(value ?? '').replace(/[&<>\"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',"'":'&#39;'}[c]))
}

function renderMarkdown(text) {
  const safe = escapeHTML(text || '')
  const blocks = safe.split(/(```[\s\S]*?```)/g)
  return blocks.map(block => {
    if (block.startsWith('```')) {
      const raw = block.slice(3, -3).replace(/^\s*[a-zA-Z0-9_+-]+\s*\n/, '')
      return `<pre><code>${raw}</code></pre>`
    }
    return block.split(/\n{2,}/g).map(part => {
      if (!part) return ''
      const value = part.replace(/\n/g, '<br>').replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>').replace(/`([^`]+)`/g, '<code>$1</code>')
      return `<p>${value}</p>`
    }).join('')
  }).join('')
}

function showToast(message) {
  toast.textContent = message
  toast.classList.add('show')
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => toast.classList.remove('show'), 1800)
}

function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

async function fetchJSON(url, options) {
  const response = await fetch(url, options)
  let data = {}
  try { data = await response.json() } catch {}
  if (!response.ok) {
    const error = new Error(data.error || data.message || `Request failed with HTTP ${response.status}`)
    error.payload = data
    throw error
  }
  return data
}

function chats() {
  try {
    const data = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')
    return Array.isArray(data) ? data : []
  } catch {
    return []
  }
}

function saveChats(items) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(items.slice(0, 100)))
}

function makeChat(title = 'New chat') {
  return { id: crypto.randomUUID(), title, createdAt: Date.now(), messages: [] }
}

function deriveTitle(text) {
  const clean = String(text || '').replace(/\s+/g, ' ').trim()
  if (!clean) return 'New chat'
  return clean.length > 42 ? `${clean.slice(0, 42).trim()}…` : clean
}

function archiveCurrentChat() {
  if (!currentChat || !currentChat.messages.length) return
  const items = chats().filter(item => item.id !== currentChat.id)
  items.unshift(currentChat)
  saveChats(items)
}

function renderHistory() {
  const items = chats()
  historyList.innerHTML = ''
  if (!items.length) {
    historyList.innerHTML = '<div class="history-empty">Your conversations will appear here.</div>'
    return
  }
  items.forEach(item => {
    const button = document.createElement('button')
    button.className = `history-item${currentChat?.id === item.id ? ' active' : ''}`
    button.textContent = item.title || 'New chat'
    button.title = item.title || 'New chat'
    button.addEventListener('click', () => openSavedChat(item.id))
    historyList.appendChild(button)
  })
}

function persistCurrentChat() {
  if (!currentChat || !currentChat.messages.length) return
  const items = chats().filter(item => item.id !== currentChat.id)
  items.unshift(currentChat)
  saveChats(items)
  renderHistory()
}

function clearConversationView() {
  conversation.innerHTML = ''
  activeAssistant = null
  activeAssistantRaw = ''
  thinkingNode = null
}

function addMessage(role, text, meta = '') {
  const row = document.createElement('article')
  row.className = `message-row ${role}`
  const avatar = document.createElement('div')
  avatar.className = 'message-avatar'
  avatar.textContent = role === 'user' ? 'You' : 'F'
  const bubble = document.createElement('div')
  bubble.className = 'message-bubble'
  const content = document.createElement('div')
  content.className = 'message-content'
  content.dataset.raw = text || ''
  content.innerHTML = role === 'assistant' ? renderMarkdown(text) : `<p>${escapeHTML(text || '').replace(/\n/g, '<br>')}</p>`
  bubble.appendChild(content)
  if (meta) {
    const metaNode = document.createElement('div')
    metaNode.className = 'message-meta'
    metaNode.textContent = meta
    bubble.appendChild(metaNode)
  }
  if (role === 'assistant') {
    const tools = document.createElement('div')
    tools.className = 'message-tools'
    const copy = document.createElement('button')
    copy.type = 'button'
    copy.textContent = 'Copy'
    copy.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(content.dataset.raw || content.textContent || '')
        showToast('Copied')
      } catch {
        showToast('Copy unavailable')
      }
    })
    tools.appendChild(copy)
    bubble.appendChild(tools)
  }
  row.appendChild(avatar)
  row.appendChild(bubble)
  conversation.appendChild(row)
  conversation.scrollTop = conversation.scrollHeight
  return content
}

function ensureThinking(text = 'Thinking…') {
  if (!thinkingNode) {
    const row = document.createElement('article')
    row.className = 'message-row assistant thinking-row'
    const avatar = document.createElement('div')
    avatar.className = 'message-avatar'
    avatar.textContent = 'F'
    const bubble = document.createElement('div')
    bubble.className = 'message-bubble'
    const content = document.createElement('div')
    content.className = 'message-content'
    content.innerHTML = '<span class="thinking"><i></i><i></i><i></i></span>'
    bubble.appendChild(content)
    row.appendChild(avatar)
    row.appendChild(bubble)
    conversation.appendChild(row)
    thinkingNode = content
  }
  thinkingNode.dataset.message = text
  conversation.scrollTop = conversation.scrollHeight
}

function clearThinking() {
  if (!thinkingNode) return
  const row = thinkingNode.closest('.message-row')
  if (row) row.remove()
  thinkingNode = null
}

function ensureAssistant() {
  clearThinking()
  if (!activeAssistant) {
    activeAssistantRaw = ''
    activeAssistant = addMessage('assistant', '', selectedModel || 'streaming')
    activeAssistant.classList.add('streaming')
  }
  return activeAssistant
}

function appendAssistantDelta(delta) {
  if (!delta) return
  activeAssistantRaw += delta
  const node = ensureAssistant()
  node.dataset.raw = activeAssistantRaw
  node.innerHTML = renderMarkdown(activeAssistantRaw)
  conversation.scrollTop = conversation.scrollHeight
}

function persistAssistantReply() {
  if (!currentChat || !activeAssistantRaw.trim()) return
  currentChat.messages.push({ role: 'assistant', content: activeAssistantRaw })
  persistCurrentChat()
}

function finishAssistant() {
  clearThinking()
  if (activeAssistant) {
    activeAssistant.classList.remove('streaming')
    persistAssistantReply()
  }
  activeAssistant = null
  activeAssistantRaw = ''
}

function addGenerationMessage(files, message) {
  finishAssistant()
  const content = addMessage('assistant', message || 'Your workspace was updated.')
  const card = document.createElement('div')
  card.className = 'generation-card'
  card.innerHTML = `<strong>Workspace updated</strong><p>${escapeHTML(message || `Updated ${files.length} file${files.length === 1 ? '' : 's'}.`)}</p><div class="generation-files">${files.slice(0, 24).map(path => `<div class="generation-file">${escapeHTML(path)}</div>`).join('')}</div><a class="generation-download" href="/api/download">Download workspace</a>`
  content.innerHTML = ''
  content.appendChild(card)
  if (currentChat) {
    currentChat.messages.push({ role: 'assistant', content: message || `Updated ${files.length} file${files.length === 1 ? '' : 's'}.` })
    persistCurrentChat()
  }
}

function addErrorMessage(payload) {
  finishAssistant()
  const content = addMessage('assistant', '')
  content.innerHTML = `<div class="error-card"><strong>${escapeHTML(payload.title || 'Request failed')}</strong><p>${escapeHTML(payload.error || payload.message || 'The request could not be completed.')}</p>${payload.recovery ? `<small>${escapeHTML(payload.recovery)}</small>` : ''}</div>`
}

function syncComposerState() {
  send.disabled = busy || !prompt.value.trim()
  composerStatus.textContent = busy ? 'Working…' : queuedFiles.length ? `${queuedFiles.length} file${queuedFiles.length === 1 ? '' : 's'} attached` : 'Ready'
}

function renderAttachments() {
  attachmentsNode.innerHTML = ''
  queuedFiles.forEach(file => {
    const chip = document.createElement('div')
    chip.className = 'attachment-chip'
    chip.innerHTML = `<span>${escapeHTML(file.name)}</span><small>${formatBytes(file.size)}</small>`
    const remove = document.createElement('button')
    remove.className = 'attachment-remove'
    remove.type = 'button'
    remove.textContent = '×'
    remove.setAttribute('aria-label', `Remove ${file.name}`)
    remove.addEventListener('click', () => {
      queuedFiles = queuedFiles.filter(item => item.id !== file.id)
      renderAttachments()
      syncComposerState()
    })
    chip.appendChild(remove)
    attachmentsNode.appendChild(chip)
  })
}

async function readUpload(file) {
  if (file.size > MAX_ATTACHMENT_BYTES) throw new Error(`${file.name} is larger than 64 KiB.`)
  const data = new Uint8Array(await file.arrayBuffer())
  let content
  try { content = new TextDecoder('utf-8', { fatal: true }).decode(data) } catch { throw new Error(`${file.name} is not valid UTF-8 text.`) }
  return { id: crypto.randomUUID(), name: file.name, size: file.size, content }
}

async function queueUploads(files) {
  const list = Array.from(files || [])
  if (!list.length) return
  if (queuedFiles.length + list.length > MAX_ATTACHMENTS) {
    showToast(`Maximum ${MAX_ATTACHMENTS} files`)
    return
  }
  let total = queuedFiles.reduce((sum, file) => sum + file.size, 0)
  for (const file of list) {
    if (total + file.size > MAX_ATTACHMENT_TOTAL) {
      showToast('Attachment limit reached')
      break
    }
    try {
      const item = await readUpload(file)
      queuedFiles.push(item)
      total += item.size
    } catch (error) {
      showToast(error.message)
    }
  }
  renderAttachments()
  syncComposerState()
}

async function loadSettings() {
  try {
    settings = await fetchJSON('/api/config')
    providerSelect.innerHTML = ''
    providerSettings.innerHTML = ''
    for (const name of Object.keys(settings.providers || {})) {
      const label = name === 'llamacpp' ? 'llama.cpp' : name[0].toUpperCase() + name.slice(1)
      for (const select of [providerSelect, providerSettings]) {
        const option = document.createElement('option')
        option.value = name
        option.textContent = label
        select.appendChild(option)
      }
    }
    selectedProvider = settings.default_provider || providerSelect.value || ''
    providerSelect.value = selectedProvider
    providerSettings.value = selectedProvider
    await loadModels(selectedProvider)
  } catch (error) {
    showToast(error.message)
  }
}

async function loadModels(provider) {
  if (!provider) return
  try {
    const data = await fetchJSON(`/api/models?provider=${encodeURIComponent(provider)}`)
    modelOptions.innerHTML = ''
    modelSettingsOptions.innerHTML = ''
    for (const name of data.models || []) {
      for (const list of [modelOptions, modelSettingsOptions]) {
        const option = document.createElement('option')
        option.value = name
        list.appendChild(option)
      }
    }
    const configured = settings?.providers?.[provider]?.default_model || ''
    selectedModel = configured || data.models?.[0] || ''
    modelInput.value = selectedModel
    modelSettings.value = selectedModel
  } catch {
    selectedModel = settings?.providers?.[provider]?.default_model || ''
    modelInput.value = selectedModel
    modelSettings.value = selectedModel
  }
}

function syncRuntimeLabels() {
  document.title = `FuzeCLI · ${selectedModel || selectedProvider || 'Chat'}`
}

providerSelect.addEventListener('change', async () => {
  selectedProvider = providerSelect.value
  providerSettings.value = selectedProvider
  await loadModels(selectedProvider)
})

modelInput.addEventListener('input', () => {
  selectedModel = modelInput.value.trim()
  modelSettings.value = selectedModel
  syncRuntimeLabels()
})

providerSettings.addEventListener('change', async () => {
  providerSelect.value = providerSettings.value
  selectedProvider = providerSettings.value
  await loadModels(selectedProvider)
})

modelSettings.addEventListener('input', () => {
  modelInput.value = modelSettings.value
  selectedModel = modelSettings.value.trim()
  syncRuntimeLabels()
})

$('saveSettings').addEventListener('click', async () => {
  const provider = providerSettings.value
  const model = modelSettings.value.trim()
  if (!provider || !model) return showToast('Provider and model are required')
  $('settingsStatus').textContent = 'Saving…'
  try {
    const data = await fetchJSON('/api/config', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ provider, model }) })
    selectedProvider = data.provider
    selectedModel = data.model
    providerSelect.value = selectedProvider
    modelInput.value = selectedModel
    $('settingsStatus').textContent = 'Saved'
    syncRuntimeLabels()
    setTimeout(() => $('settingsStatus').textContent = '', 1500)
  } catch (error) {
    $('settingsStatus').textContent = 'Could not save'
    showToast(error.message)
  }
})

async function loadHistoryFromServer() {
  const data = await fetchJSON('/api/history')
  const messages = data.messages || []
  currentChat = makeChat(messages.find(m => m.role === 'user')?.content ? deriveTitle(messages.find(m => m.role === 'user').content) : 'Current chat')
  currentChat.messages = messages.map(message => ({ role: message.role, content: message.content }))
  externalContext = false
  clearConversationView()
  if (!messages.length) showWelcome()
  else renderConversation(messages)
}

function renderConversation(messages) {
  clearConversationView()
  messages.forEach(message => addMessage(message.role, message.content))
}

function showWelcome() {
  clearConversationView()
  const welcome = document.createElement('div')
  welcome.className = 'welcome'
  welcome.id = 'welcome'
  welcome.innerHTML = `<div class="welcome-mark"><svg viewBox="0 0 48 48"><path d="M13 8h24v7H21v6h13v7H21v12h-8V8Z"/><path d="m29 31 7-7 5 5-7 7-5-5Z"/></svg></div><div class="eyebrow">PRIVATE · LOCAL · FOCUSED</div><h1>What are we <span>building today?</span></h1><p>Chat with your workspace-aware coding assistant. Ask questions, inspect files, upload context, or describe the implementation you want.</p><div class="suggestions"><button data-prompt="Explain this project">Explain this project</button><button data-prompt="Review my workspace for problems">Review my workspace</button><button data-prompt="Create a clean landing page">Build a landing page</button></div>`
  conversation.appendChild(welcome)
  bindSuggestions()
}

function bindSuggestions() {
  document.querySelectorAll('[data-prompt]').forEach(button => {
    button.onclick = () => {
      prompt.value = button.dataset.prompt || ''
      prompt.focus()
      syncComposerState()
    }
  })
}

async function newChat() {
  if (busy) return
  archiveCurrentChat()
  currentChat = makeChat()
  externalContext = false
  queuedFiles = []
  renderAttachments()
  clearConversationView()
  showWelcome()
  renderHistory()
  try {
    await fetchJSON('/api/session', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ memory: 'clear', provider: selectedProvider, model: selectedModel }) })
  } catch {}
  prompt.focus()
  closeSidebarMobile()
  syncComposerState()
}

function openSavedChat(id) {
  if (busy) return
  const item = chats().find(chat => chat.id === id)
  if (!item) return
  archiveCurrentChat()
  currentChat = JSON.parse(JSON.stringify(item))
  externalContext = true
  clearConversationView()
  if (currentChat.messages.length) renderConversation(currentChat.messages)
  else showWelcome()
  renderHistory()
  closeSidebarMobile()
}

function buildPromptWithFiles(text) {
  const chunks = []
  if (externalContext && currentChat?.messages?.length) {
    chunks.push('Earlier conversation context from this chat:\n' + currentChat.messages.map(message => `${message.role}: ${message.content}`).join('\n\n'))
  }
  if (queuedFiles.length) {
    chunks.push('User-attached files:\n' + queuedFiles.map(file => `--- ${file.name} ---\n${file.content}\n--- end ${file.name} ---`).join('\n\n'))
  }
  chunks.push(text)
  return chunks.join('\n\n')
}

function handleProgressData(data) {
  if (data.provider) selectedProvider = data.provider
  if (data.model) selectedModel = data.model
  if (data.status === 'planning' || data.status === 'generating' || data.status === 'verifying') ensureThinking(data.message || 'Thinking…')
  if (data.status === 'completed') {
    busy = false
    finishAssistant()
    if (data.generated_files?.length) addGenerationMessage(data.generated_files, data.message)
    $('connectionText').textContent = 'Connected'
    $('connectionDot').classList.remove('warn')
    loadWorkspace()
    syncComposerState()
  }
  if (data.status === 'failed') {
    busy = false
    finishAssistant()
    syncComposerState()
  }
}

function handleChatTokenData(data) {
  if (data.provider) selectedProvider = data.provider
  if (data.model) selectedModel = data.model
  if (data.status === 'streaming') appendAssistantDelta(data.message || '')
  else if (data.status !== 'completed') ensureThinking(data.message || 'Thinking…')
}

function handleGeneratedData(data) {
  if (data.generated_files?.length) addGenerationMessage(data.generated_files, data.message)
}

function handleErrorData(data) {
  busy = false
  addErrorMessage(data)
  showToast(data.error_message || data.message || 'Request failed')
  syncComposerState()
}

function connectEvents() {
  if (source) source.close()
  source = new EventSource('/api/events')
  source.addEventListener('progress', event => { try { handleProgressData(JSON.parse(event.data)) } catch {} })
  source.addEventListener('chat_token', event => { try { handleChatTokenData(JSON.parse(event.data)) } catch {} })
  source.addEventListener('generated', event => { try { handleGeneratedData(JSON.parse(event.data)) } catch {} })
  source.addEventListener('job_error', event => { try { handleErrorData(JSON.parse(event.data)) } catch {} })
  source.onopen = () => {
    $('connectionText').textContent = 'Connected'
    $('connectionDot').classList.remove('warn')
  }
  source.onerror = () => {
    $('connectionText').textContent = 'Reconnecting…'
    $('connectionDot').classList.add('warn')
  }
}

async function submit() {
  if (busy) return
  const text = prompt.value.trim()
  if (!text) return
  if (conversation.querySelector('.welcome')) conversation.innerHTML = ''
  if (!currentChat) currentChat = makeChat(deriveTitle(text))
  if (!currentChat.messages.length) currentChat.title = deriveTitle(text)
  const outgoing = buildPromptWithFiles(text)
  addMessage('user', text, queuedFiles.length ? `${queuedFiles.length} attachment${queuedFiles.length === 1 ? '' : 's'}` : '')
  currentChat.messages.push({ role: 'user', content: text })
  persistCurrentChat()
  externalContext = false
  prompt.value = ''
  queuedFiles = []
  renderAttachments()
  busy = true
  ensureThinking('Thinking…')
  syncComposerState()
  try {
    await fetchJSON('/api/chat', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ prompt: outgoing, provider: selectedProvider, model: selectedModel, code: false, yes: true }) })
  } catch (error) {
    busy = false
    addErrorMessage(error.payload || { title: 'Could not send message', error: error.message })
    syncComposerState()
  }
}

async function loadWorkspace() {
  const list = $('fileList')
  if (!list) return
  try {
    const data = await fetchJSON('/api/workspace')
    $('workspaceRoot').textContent = data.root || 'Workspace'
    list.innerHTML = ''
    for (const file of data.files || []) {
      const row = document.createElement('div')
      row.className = 'file-row'
      row.innerHTML = `<span class="file-icon">□</span><div><strong>${escapeHTML(file.path)}</strong><small>${formatBytes(file.size)} · ${new Date(file.modified).toLocaleString()}</small></div>`
      list.appendChild(row)
    }
    if (!data.files?.length) list.innerHTML = '<div class="history-empty">No generated files are being tracked yet.</div>'
  } catch {}
}

function openPanel(name) {
  $('chatView').classList.toggle('hidden', name !== 'chat')
  $('workspaceView').classList.toggle('hidden', name !== 'workspace')
  $('settingsView').classList.toggle('hidden', name !== 'settings')
  if (name === 'workspace') loadWorkspace()
  if (name === 'settings') {
    providerSettings.value = selectedProvider
    modelSettings.value = selectedModel
  }
  closeSidebarMobile()
}

function closeSidebarMobile() { sidebar.classList.remove('open') }

$('openSettings').addEventListener('click', () => openPanel('settings'))
$('openSettingsTop').addEventListener('click', () => openPanel('settings'))
$('workspaceButton').addEventListener('click', () => openPanel('workspace'))
$('menuButton').addEventListener('click', () => sidebar.classList.toggle('open'))
$('newChat').addEventListener('click', newChat)
$('refreshWorkspace').addEventListener('click', loadWorkspace)
$('clearHistory').addEventListener('click', () => {
  if (!chats().length) return
  localStorage.removeItem(STORAGE_KEY)
  renderHistory()
  showToast('Saved chat history cleared')
})
$('attachButton').addEventListener('click', () => fileInput.click())
fileInput.addEventListener('change', async () => {
  await queueUploads(fileInput.files)
  fileInput.value = ''
})
;['dragenter','dragover'].forEach(type => composerWrap.addEventListener(type, event => {
  event.preventDefault()
  dropOverlay.classList.add('show')
}))
;['dragleave','drop'].forEach(type => composerWrap.addEventListener(type, event => {
  event.preventDefault()
  if (type === 'dragleave' && event.relatedTarget && composerWrap.contains(event.relatedTarget)) return
  dropOverlay.classList.remove('show')
}))
composerWrap.addEventListener('drop', async event => { await queueUploads(event.dataTransfer.files) })
send.addEventListener('click', submit)
prompt.addEventListener('input', () => {
  prompt.style.height = 'auto'
  prompt.style.height = `${Math.min(prompt.scrollHeight, 220)}px`
  syncComposerState()
})
prompt.addEventListener('keydown', event => {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    submit()
  }
})
document.addEventListener('keydown', event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    if (!busy) prompt.focus()
  }
  if (event.key === 'Escape') closeSidebarMobile()
})

async function boot() {
  bindSuggestions()
  await loadSettings()
  connectEvents()
  try {
    await loadHistoryFromServer()
  } catch {
    currentChat = makeChat()
    showWelcome()
  }
  renderHistory()
  loadWorkspace()
  syncComposerState()
}

boot()
