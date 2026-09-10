const $=id=>document.getElementById(id);
const conversation=$('conversation');
const prompt=$('prompt');
const send=$('send');
const code=$('code');
const providerSelect=$('providerSelect');
const modelInput=$('modelInput');
const modelOptions=$('modelOptions');
const settingsState=$('settingsState');
const tabs=[...document.querySelectorAll('.nav[data-tab]')];
let settings=null;
let selectedProvider='';
let selectedModel='';
let jobRunning=false;
let activeAssistant=null;

function addMessage(role,text){
  const el=document.createElement('div');
  el.className=`message ${role}`;
  el.textContent=text||'';
  conversation.appendChild(el);
  conversation.scrollTop=conversation.scrollHeight;
  return el;
}

function ensureAssistant(){
  if(!activeAssistant)activeAssistant=addMessage('assistant','');
  return activeAssistant;
}

function showError(payload){
  const data=typeof payload==='string'?parseErrorPayload(payload):payload||{};
  $('errorTitle').textContent=data.title||'Something went wrong';
  $('errorText').textContent=data.error||data.message||'The request could not be completed.';
  $('errorRecovery').textContent=data.recovery?`Next: ${data.recovery}`:'';
  $('errorBox').classList.remove('hidden');
}

function parseErrorPayload(value){
  try{const parsed=JSON.parse(value);if(parsed&&typeof parsed==='object')return parsed;}catch{}
  return{message:String(value)};
}

function clearError(){
  $('errorBox').classList.add('hidden');
  $('errorTitle').textContent='Something went wrong';
  $('errorText').textContent='';
  $('errorRecovery').textContent='';
}

function setProgress(e){
  const total=Number(e.total_files||0);
  const completed=Number(e.completed_files||0);
  const pct=total?Math.min(100,Math.round(completed/total*100)):e.status==='completed'?100:0;
  $('percent').textContent=`${pct}%`;
  $('completed').textContent=completed;
  $('total').textContent=total;
  $('current').textContent=e.current_file||e.project||'Working';
  $('message').textContent=e.message||'Working';
  $('status').textContent=prettyStatus(e.status||'idle');
  $('progressRing').style.setProperty('--progress',`${pct*3.6}deg`);
  $('agentProvider').textContent=e.provider||selectedProvider||'—';
  $('agentModel').textContent=e.model||selectedModel||'—';
  $('elapsed').textContent=formatElapsed(Number(e.elapsed_millis||0));
  $('sideRuntime').textContent=e.status==='failed'?'Needs attention':e.status==='completed'?'Ready':e.status&&e.status!=='idle'?'Working':'Ready';
  const order=['planning','generating','verifying','completed'];
  document.querySelectorAll('.step').forEach(step=>{
    step.classList.remove('active','done');
    const name=step.dataset.step;
    const i=order.indexOf(name);
    const current=order.indexOf(e.status);
    if(e.status==='completed'||(current>=0&&i<current))step.classList.add('done');
    if(name===e.status)step.classList.add('active');
  });
}

function prettyStatus(value){
  const map={idle:'Idle',planning:'Planning',planned:'Planned',generating:'Generating',writing:'Writing',verifying:'Verifying',correcting:'Correcting',streaming:'Streaming',completed:'Complete',failed:'Failed'};
  return map[value]||value;
}

function formatElapsed(ms){
  const seconds=Math.max(0,Math.floor(ms/1000));
  if(seconds<60)return`${seconds}s`;
  return`${Math.floor(seconds/60)}m ${String(seconds%60).padStart(2,'0')}s`;
}

function openTab(name){
  tabs.forEach(t=>t.classList.toggle('active',t.dataset.tab===name));
  document.querySelectorAll('.view').forEach(v=>v.classList.toggle('active',v.id===`view-${name}`));
  const titles={chat:'Chat',workspace:'Workspace',plan:'Project plan',settings:'Settings'};
  $('pageTitle').textContent=titles[name]||'Chat';
  if(name==='workspace')loadWorkspace();
  if(name==='plan')loadPlan();
  if(name==='settings')loadSettings();
}

tabs.forEach(t=>t.addEventListener('click',()=>openTab(t.dataset.tab)));

async function fetchJSON(url,options){
  const response=await fetch(url,options);
  let data={};
  try{data=await response.json();}catch{}
  if(!response.ok){
    const error=new Error(data.error||data.message||`Request failed with HTTP ${response.status}`);
    error.payload=data;
    throw error;
  }
  return data;
}

async function loadSettings(){
  try{
    const d=await fetchJSON('/api/config');
    settings=d;
    providerSelect.innerHTML='';
    Object.keys(d.providers||{}).forEach(name=>{
      const option=document.createElement('option');
      option.value=name;
      option.textContent=name==='llamacpp'?'llama.cpp':name[0].toUpperCase()+name.slice(1);
      providerSelect.appendChild(option);
    });
    selectedProvider=d.default_provider||providerSelect.value||'';
    providerSelect.value=selectedProvider;
    await loadModels();
    settingsState.textContent='';
    $('providerLabel').textContent=selectedProvider||'No provider';
  }catch(error){
    showError(error.payload||error.message);
    settingsState.textContent='Settings unavailable';
  }
}

async function loadModels(){
  const provider=providerSelect.value;
  if(!provider)return;
  modelOptions.innerHTML='';
  modelInput.value='Loading…';
  modelInput.disabled=true;
  try{
    const d=await fetchJSON(`/api/models?provider=${encodeURIComponent(provider)}`);
    const discovered=[...(d.models||[])];
    discovered.forEach(model=>{
      const option=document.createElement('option');
      option.value=model;
      modelOptions.appendChild(option);
    });
    const configured=settings?.providers?.[provider]?.default_model||'';
    selectedProvider=provider;
    selectedModel=configured||discovered[0]||'';
    modelInput.value=selectedModel;
    $('providerLabel').textContent=provider;
    $('modelLabel').textContent=selectedModel||'No model';
  }catch(error){
    const configured=settings?.providers?.[provider]?.default_model||'';
    selectedProvider=provider;
    selectedModel=configured;
    modelInput.value=configured;
    showError(error.payload||error.message);
    $('modelLabel').textContent=configured||'Unavailable';
  }finally{modelInput.disabled=false;}
}

providerSelect.addEventListener('change',loadModels);
modelInput.addEventListener('input',()=>{
  selectedModel=modelInput.value.trim();
  $('modelLabel').textContent=selectedModel||'No model';
});

$('saveSettings').addEventListener('click',async()=>{
  const provider=providerSelect.value;
  const model=modelInput.value.trim();
  if(!provider||!model){
    showError({title:'Settings need a provider and model',error:'Select a provider and enter a model name.',recovery:'Choose a discovered model or enter the exact model identifier supported by the provider.'});
    return;
  }
  settingsState.textContent='Saving…';
  clearError();
  try{
    const d=await fetchJSON('/api/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({provider,model})});
    selectedProvider=d.provider;
    selectedModel=d.model;
    $('providerLabel').textContent=selectedProvider;
    $('modelLabel').textContent=selectedModel;
    settingsState.textContent='Saved';
    setTimeout(()=>settingsState.textContent='',1800);
  }catch(error){
    settingsState.textContent='Could not save';
    showError(error.payload||error.message);
  }
});

async function loadWorkspace(){
  const list=$('fileList');
  list.innerHTML='<div class="loading">Loading workspace…</div>';
  try{
    const d=await fetchJSON('/api/workspace');
    $('workspaceRoot').textContent=d.root||'Workspace';
    list.innerHTML='';
    if(!d.files?.length){list.innerHTML='<div class="empty-state">No generated files are being tracked yet.</div>';return;}
    d.files.forEach(file=>{
      const row=document.createElement('div');
      row.className='file-row';
      row.innerHTML=`<span class="file-icon">□</span><div><strong>${escapeHTML(file.path)}</strong><small>${formatBytes(file.size)} · ${new Date(file.modified).toLocaleString()}</small></div>`;
      list.appendChild(row);
    });
  }catch(error){
    list.innerHTML='';
    const box=document.createElement('div');
    box.className='error-box';
    box.innerHTML=`<div class="error-icon">!</div><div><strong>${escapeHTML(error.payload?.title||'Workspace unavailable')}</strong><p>${escapeHTML(error.payload?.error||error.message)}</p><small>${escapeHTML(error.payload?.recovery||'')}</small></div>`;
    list.appendChild(box);
  }
}

async function loadPlan(){
  const box=$('planContent');
  box.innerHTML='<div class="loading">Loading project plan…</div>';
  try{
    const d=await fetchJSON('/api/plan');
    if(!d.exists){box.innerHTML='<div class="empty-state">No project plan is active. Start a larger /code request to create one.</div>';return;}
    const p=d.plan;
    const files=p.files||[];
    const done=files.filter(file=>file.status==='completed').length;
    const pct=files.length?Math.round(done/files.length*100):0;
    box.innerHTML=`<div class="plan-summary"><span class="kicker">${escapeHTML(p.project)}</span><h3>${escapeHTML(p.summary)}</h3><div class="plan-progress"><span style="width:${pct}%"></span></div><div class="plan-count">${done} of ${files.length} files complete</div></div><div class="file-list">${files.map(file=>`<div class="file-row ${file.status==='completed'?'complete':''}"><span class="file-status">${file.status==='completed'?'✓':'○'}</span><div><strong>${escapeHTML(file.path)}</strong><small>${escapeHTML(file.purpose||'Planned file')}</small></div></div>`).join('')}</div>`;
  }catch(error){
    box.innerHTML=`<div class="error-box"><div class="error-icon">!</div><div><strong>${escapeHTML(error.payload?.title||'Project plan unavailable')}</strong><p>${escapeHTML(error.payload?.error||error.message)}</p><small>${escapeHTML(error.payload?.recovery||'')}</small></div></div>`;
  }
}

function formatBytes(n){
  if(n<1024)return`${n} B`;
  if(n<1048576)return`${(n/1024).toFixed(1)} KB`;
  return`${(n/1048576).toFixed(1)} MB`;
}

function escapeHTML(s){return String(s??'').replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));}

const source=new EventSource('/api/events');
source.addEventListener('progress',handleProgress);
source.addEventListener('chat_token',handleChatToken);
source.addEventListener('error',handleProgress);
source.onmessage=handleProgress;
source.onerror=()=>{
  $('runtime').classList.add('disconnected');
  $('connectionDot').style.background='var(--warn)';
};

function handleProgress(event){
  try{
    if(!event.data)return;
    const data=JSON.parse(event.data);
    setProgress(data);
    if(data.provider)$('providerLabel').textContent=data.provider;
    if(data.model)$('modelLabel').textContent=data.model;
    if(data.status==='failed'){
      showError(parseErrorPayload(data.message||'The request failed.'));
      jobRunning=false;
      send.disabled=false;
      document.body.classList.remove('busy');
      activeAssistant=null;
    }else if(data.status==='completed'){
      jobRunning=false;
      send.disabled=false;
      document.body.classList.remove('busy');
      activeAssistant=null;
      loadWorkspace();
      loadPlan();
    }
  }catch{}
}

function handleChatToken(event){
  try{
    if(!event.data)return;
    const data=JSON.parse(event.data);
    setProgress(data);
    const node=ensureAssistant();
    node.textContent+=data.message||'';
    conversation.scrollTop=conversation.scrollHeight;
  }catch{}
}

async function submit(){
  if(jobRunning)return;
  const text=prompt.value.trim();
  if(!text)return;
  const isCode=code.checked||text.startsWith('/code ');
  const clean=text.startsWith('/code ')?text.slice(6).trim():text;
  if(!clean)return;
  if(conversation.querySelector('.welcome'))conversation.innerHTML='';
  addMessage('user',clean);
  activeAssistant=null;
  prompt.value='';
  prompt.style.height='auto';
  jobRunning=true;
  document.body.classList.add('busy');
  send.disabled=true;
  clearError();
  setProgress({status:isCode?'planning':'generating',message:isCode?'Starting project planning…':'Connecting to provider…',provider:selectedProvider,model:selectedModel});
  try{
    const result=await fetchJSON('/api/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({prompt:clean,provider:selectedProvider,model:selectedModel,code:isCode,yes:true})});
    if(result?.accepted!==true)throw new Error('The server did not accept the chat request.');
  }catch(error){
    jobRunning=false;
    send.disabled=false;
    document.body.classList.remove('busy');
    showError(error.payload||error.message);
    addMessage('assistant',error.payload?.error||error.message);
    setProgress({status:'failed',message:JSON.stringify(error.payload||{message:error.message}),provider:selectedProvider,model:selectedModel});
  }
}

send.addEventListener('click',submit);
prompt.addEventListener('keydown',event=>{
  if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();submit();}
});
prompt.addEventListener('input',()=>{
  prompt.style.height='auto';
  prompt.style.height=Math.min(prompt.scrollHeight,220)+'px';
});

document.querySelectorAll('[data-prompt]').forEach(button=>button.addEventListener('click',()=>{
  prompt.value=button.dataset.prompt;
  code.checked=button.dataset.code==='true';
  prompt.focus();
}));

$('newChat').addEventListener('click',()=>{
  if(jobRunning)return;
  conversation.innerHTML='';
  clearError();
  activeAssistant=null;
  setProgress({status:'idle',message:'Ready',completed_files:0,total_files:0,provider:selectedProvider,model:selectedModel});
  openTab('chat');
  prompt.focus();
});

$('refreshWorkspace').addEventListener('click',loadWorkspace);
$('refreshPlan').addEventListener('click',loadPlan);

document.addEventListener('keydown',event=>{
  if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='k'){
    event.preventDefault();
    if(!jobRunning)prompt.focus();
  }
});

loadSettings();
fetch('/api/state').then(response=>response.json()).then(setProgress).catch(()=>{});
