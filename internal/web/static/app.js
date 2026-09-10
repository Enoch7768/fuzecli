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
let lastPrompt='';

function escapeHTML(value){return String(value??'').replace(/[&<>\"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;',"'":'&#39;'}[c]));}

function renderMessageText(text){
  const safe=escapeHTML(text||'');
  const parts=safe.split(/(```[\\s\\S]*?```)/g);
  return parts.map(part=>{
    if(part.startsWith('```')){
      const raw=part.slice(3,-3).replace(/^\\s*[a-zA-Z0-9_-]+\\s*\\n/,'');
      return `<pre><code>${raw}</code></pre>`;
    }
    return part.replace(/\\*\\*(.+?)\\*\\*/g,'<strong>$1</strong>').replace(/`([^`]+)`/g,'<code>$1</code>').replace(/\\n/g,'<br>');
  }).join('');
}

function addMessage(role,text,options={}){
  const wrap=document.createElement('article');
  wrap.className=`message-group ${role}`;
  const avatar=document.createElement('div');
  avatar.className='message-avatar';
  avatar.textContent=role==='user'?'You':'F';
  const body=document.createElement('div');
  body.className='message-body';
  const head=document.createElement('div');
  head.className='message-head';
  const name=document.createElement('span');
  name.textContent=role==='user'?'You':'FuzeCLI';
  head.appendChild(name);
  if(options.meta){
    const meta=document.createElement('small');
    meta.textContent=options.meta;
    head.appendChild(meta);
  }
  const content=document.createElement('div');
  content.className='message-content';
  content.dataset.raw=text||'';
  content.innerHTML=role==='assistant'?renderMessageText(text):escapeHTML(text).replace(/\n/g,'<br>');
  body.appendChild(head);
  body.appendChild(content);
  if(role==='assistant'){
    const actions=document.createElement('div');
    actions.className='message-actions';
    const copy=document.createElement('button');
    copy.type='button';
    copy.textContent='Copy';
    copy.addEventListener('click',async()=>{
      try{await navigator.clipboard.writeText(content.dataset.raw||content.textContent||'');copy.textContent='Copied';setTimeout(()=>copy.textContent='Copy',1200);}catch{copy.textContent='Copy failed';setTimeout(()=>copy.textContent='Copy',1200);}
    });
    actions.appendChild(copy);
    body.appendChild(actions);
  }
  wrap.appendChild(avatar);
  wrap.appendChild(body);
  conversation.appendChild(wrap);
  conversation.scrollTop=conversation.scrollHeight;
  return content;
}

function ensureAssistant(){
  if(!activeAssistant){
    activeAssistant=addMessage('assistant','',{meta:`${selectedModel||'model'} · streaming`});
    activeAssistant.classList.add('streaming-content');
  }
  return activeAssistant;
}

function finishAssistant(){
  if(activeAssistant){
    activeAssistant.classList.remove('streaming-content');
    const group=activeAssistant.closest('.message-group');
    if(group){
      const meta=group.querySelector('.message-head small');
      if(meta)meta.textContent=selectedModel||'model';
    }
  }
  activeAssistant=null;
}

function appendAssistantDelta(delta){
  const node=ensureAssistant();
  node.dataset.raw=(node.dataset.raw||'')+delta;
  node.innerHTML=renderMessageText(node.dataset.raw);
  conversation.scrollTop=conversation.scrollHeight;
}

function showGeneratedSummary(files,message){
  if(!activeAssistant)return;
  const content=activeAssistant;
  content.dataset.raw=message||'';
  const list=(files||[]).slice(0,24).map(path=>`<div class="generation-file">${escapeHTML(path)}</div>`).join('');
  const more=(files||[]).length>24?`<div class="inline-error-meta">${files.length-24} more files are in the workspace.</div>`:'';
  content.innerHTML=`<div class="generation-card"><strong>Code generated successfully</strong><p>${escapeHTML(message||`Generated ${files.length} file${files.length===1?'':'s'} and saved them to the workspace.`)}</p>${list?`<div class="generation-files">${list}</div>`:''}${more}<a class="generation-download" href="/api/download">Download code</a></div>`;
}

function showError(payload){
  const data=typeof payload==='string'?parseErrorPayload(payload):payload||{};
  $('errorTitle').textContent=data.title||'Something went wrong';
  $('errorText').textContent=data.error||data.message||'The request could not be completed.';
  const bits=[];
  if(data.provider)bits.push(data.provider);
  if(data.model)bits.push(data.model);
  if(data.status_code)bits.push(`HTTP ${data.status_code}`);
  if(data.retry_after)bits.push(`retry in ~${data.retry_after}s`);
  $('errorMeta').textContent=bits.join(' · ');
  $('errorRecovery').textContent=data.recovery||'';
  $('errorTechnical').textContent=data.technical||'';
  $('errorTechnicalWrap').classList.toggle('hidden',!data.technical);
  $('retryError').classList.toggle('hidden',!data.retryable||!lastPrompt);
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
  $('errorMeta').textContent='';
  $('errorRecovery').textContent='';
  $('errorTechnical').textContent='';
  $('retryError').classList.add('hidden');
  $('errorTechnicalWrap').classList.add('hidden');
}

function setProgress(e){
  const total=Number(e.total_files||0);
  const completed=Number(e.completed_files||0);
  const pct=total?Math.min(100,Math.round(completed/total*100)):e.status==='completed'?100:0;
  $('percent').textContent=`${pct}%`;
  $('completed').textContent=completed;
  $('total').textContent=total;
  $('current').textContent=e.current_file||e.project||'Waiting for a request';
  $('message').textContent=e.message||'Ready';
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
    discovered.forEach(model=>{const option=document.createElement('option');option.value=model;modelOptions.appendChild(option);});
    const configured=settings?.providers?.[provider]?.default_model||'';
    selectedProvider=provider;
    selectedModel=configured||discovered[0]||'';
    modelInput.value=selectedModel;
    $('providerLabel').textContent=provider;
    $('modelLabel').textContent=selectedModel||'No model';
    $('agentProvider').textContent=provider;
    $('agentModel').textContent=selectedModel||'—';
    clearError();
  }catch(error){
    const configured=settings?.providers?.[provider]?.default_model||'';
    selectedProvider=provider;
    selectedModel=configured;
    modelInput.value=configured;
    $('providerLabel').textContent=provider;
    $('modelLabel').textContent=configured||'Unavailable';
    $('agentProvider').textContent=provider;
    $('agentModel').textContent=configured||'—';
    showError(error.payload||error.message);
  }finally{modelInput.disabled=false;}
}

providerSelect.addEventListener('change',loadModels);
modelInput.addEventListener('input',()=>{selectedModel=modelInput.value.trim();$('modelLabel').textContent=selectedModel||'No model';$('agentModel').textContent=selectedModel||'—';});

$('saveSettings').addEventListener('click',async()=>{
  const provider=providerSelect.value;
  const model=modelInput.value.trim();
  if(!provider||!model){showError({title:'Settings need a provider and model',error:'Select a provider and enter a model name.',recovery:'Choose a discovered model or enter the exact model identifier supported by the provider.'});return;}
  settingsState.textContent='Saving…';
  try{
    const d=await fetchJSON('/api/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({provider,model})});
    selectedProvider=d.provider;
    selectedModel=d.model;
    $('providerLabel').textContent=selectedProvider;
    $('modelLabel').textContent=selectedModel;
    $('agentProvider').textContent=selectedProvider;
    $('agentModel').textContent=selectedModel;
    settingsState.textContent='Saved';
    setTimeout(()=>settingsState.textContent='',1800);
    clearError();
  }catch(error){settingsState.textContent='Could not save';showError(error.payload||error.message);}
});

async function loadWorkspace(){
  const list=$('fileList');
  list.innerHTML='<div class="loading">Loading workspace…</div>';
  try{
    const d=await fetchJSON('/api/workspace');
    $('workspaceRoot').textContent=d.root||'Workspace';
    list.innerHTML='';
    if(!d.files?.length){list.innerHTML='<div class="empty-state">No generated files are being tracked yet.</div>';return;}
    d.files.forEach(file=>{const row=document.createElement('div');row.className='file-row';row.innerHTML=`<span class="file-icon">□</span><div><strong>${escapeHTML(file.path)}</strong><small>${formatBytes(file.size)} · ${new Date(file.modified).toLocaleString()}</small></div>`;list.appendChild(row);});
  }catch(error){
    list.innerHTML=`<div class="error-box"><div class="error-icon">!</div><div class="error-content"><strong>${escapeHTML(error.payload?.title||'Workspace unavailable')}</strong><p>${escapeHTML(error.payload?.error||error.message)}</p><small>${escapeHTML(error.payload?.recovery||'')}</small></div></div>`;
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
  }catch(error){box.innerHTML=`<div class="error-box"><div class="error-icon">!</div><div class="error-content"><strong>${escapeHTML(error.payload?.title||'Project plan unavailable')}</strong><p>${escapeHTML(error.payload?.error||error.message)}</p><small>${escapeHTML(error.payload?.recovery||'')}</small></div></div>`;}
}

function formatBytes(n){if(n<1024)return`${n} B`;if(n<1048576)return`${(n/1024).toFixed(1)} KB`;return`${(n/1048576).toFixed(1)} MB`;}
function resetConversationVisual(){conversation.innerHTML='';activeAssistant=null;}

function addFriendlyErrorMessage(data){
  const title=data.title||'Request failed';
  const text=data.error||data.message||'The request could not be completed.';
  const meta=[data.provider,data.model,data.status_code?`HTTP ${data.status_code}`:''].filter(Boolean).join(' · ');
  const content=addMessage('assistant','',{meta});
  content.innerHTML=`<div class="inline-error"><div class="inline-error-top"><span class="inline-error-icon">!</span><div><strong>${escapeHTML(title)}</strong><p>${escapeHTML(text)}</p></div></div>${data.recovery?`<div class="inline-error-recovery">${escapeHTML(data.recovery)}</div>`:''}${data.retry_after?`<div class="inline-error-meta">Retry delay: about ${escapeHTML(data.retry_after)} seconds</div>`:''}${data.technical?`<details><summary>Technical details</summary><pre>${escapeHTML(data.technical)}</pre></details>`:''}</div>`;
  return content;
}

const source=new EventSource('/api/events');
source.addEventListener('progress',handleProgress);
source.addEventListener('chat_token',handleChatToken);
source.addEventListener('generated',handleGenerated);
source.addEventListener('job_error',handleJobError);
source.onmessage=handleProgress;
source.onerror=()=>{
  $('runtime').classList.add('disconnected');
  $('connectionText').textContent='Connection interrupted';
  $('connectionDot').style.background='var(--warn)';
};
source.onopen=()=>{
  $('runtime').classList.remove('disconnected');
  $('connectionText').textContent='Connected';
  $('connectionDot').style.background='';
};

function handleProgress(event){
  try{
    if(!event.data)return;
    const data=JSON.parse(event.data);
    setProgress(data);
    if(data.provider)$('providerLabel').textContent=data.provider;
    if(data.model)$('modelLabel').textContent=data.model;
    if(data.status==='completed'){
      if(data.generated_files?.length)showGeneratedSummary(data.generated_files,data.message);
      jobRunning=false;
      send.disabled=false;
      document.body.classList.remove('busy');
      finishAssistant();
      loadWorkspace();
      loadPlan();
    }else if(data.status==='failed'){
      jobRunning=false;
      send.disabled=false;
      document.body.classList.remove('busy');
      finishAssistant();
    }
  }catch{}
}

function handleGenerated(event){
  try{
    if(!event.data)return;
    const data=JSON.parse(event.data);
    setProgress(data);
    if(data.generated_files?.length)showGeneratedSummary(data.generated_files,data.message);
  }catch{}
}

function handleChatToken(event){
  try{if(!event.data)return;const data=JSON.parse(event.data);setProgress(data);appendAssistantDelta(data.message||'');}catch{}
}

function handleJobError(event){
  try{
    if(!event.data)return;
    const data=JSON.parse(event.data);
    setProgress(data);
    const payload={title:data.error_title||'Request failed',error:data.error_message||data.message,recovery:data.error_recovery,technical:data.error_technical,provider:data.provider,model:data.model,retry_after:data.retry_after,status_code:data.http_status,retryable:true};
    finishAssistant();
    showError(payload);
    addFriendlyErrorMessage(payload);
    jobRunning=false;
    send.disabled=false;
    document.body.classList.remove('busy');
  }catch{}
}

async function submit(){
  if(jobRunning)return;
  const text=prompt.value.trim();
  if(!text)return;
  const isCode=code.checked||/^\/code(?:\s|$)/i.test(text);
  const clean=text.replace(/^\/code(?:\s|$)/i,'').trim();
  if(!clean){
    showError({title:'Tell FuzeCLI what to build',error:'The /code command needs a description of the code or project you want generated.',recovery:'Example: /code create a modern PHP landing page'});
    return;
  }
  if(conversation.querySelector('.welcome'))conversation.innerHTML='';
  lastPrompt=clean;
  addMessage('user',clean,{meta:isCode?'code generation':'message'});
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
    if(result?.accepted!==true)throw Object.assign(new Error('The server did not accept the chat request.'),{payload:{title:'Request was not accepted',error:'FuzeCLI did not start the requested chat job.',recovery:'Retry the message.'}});
  }catch(error){
    jobRunning=false;
    send.disabled=false;
    document.body.classList.remove('busy');
    const payload=error.payload||{title:'Could not send message',error:error.message,recovery:'Check that FuzeCLI web server is running and retry.'};
    showError(payload);
    addFriendlyErrorMessage(payload);
    setProgress({status:'failed',message:payload.error||error.message,provider:selectedProvider,model:selectedModel});
  }
}

$('retryError').addEventListener('click',()=>{clearError();prompt.value=lastPrompt;submit();});
$('attachInfo').addEventListener('click',()=>{showError({title:'Workspace context',error:'FuzeCLI automatically includes tracked workspace context where the current request needs it.',recovery:'Normal chat can answer questions. Code requests can be generated automatically when the model returns valid FuzeCLI generation JSON.'});});
$('downloadCode').addEventListener('click',()=>{window.location.href='/api/download';});
send.addEventListener('click',submit);
prompt.addEventListener('keydown',event=>{if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();submit();}});
prompt.addEventListener('input',()=>{prompt.style.height='auto';prompt.style.height=Math.min(prompt.scrollHeight,220)+'px';});
document.querySelectorAll('[data-prompt]').forEach(button=>button.addEventListener('click',()=>{prompt.value=button.dataset.prompt;code.checked=button.dataset.code==='true';prompt.focus();}));
$('newChat').addEventListener('click',()=>{if(jobRunning)return;resetConversationVisual();clearError();setProgress({status:'idle',message:'Ready',completed_files:0,total_files:0,provider:selectedProvider,model:selectedModel});openTab('chat');prompt.focus();});
$('refreshWorkspace').addEventListener('click',loadWorkspace);
$('refreshPlan').addEventListener('click',loadPlan);
conversation.addEventListener('scroll',()=>{const distance=conversation.scrollHeight-conversation.scrollTop-conversation.clientHeight;$('scrollBottom').classList.toggle('visible',distance>220);});
$('scrollBottom').addEventListener('click',()=>conversation.scrollTo({top:conversation.scrollHeight,behavior:'smooth'}));
document.addEventListener('keydown',event=>{if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='k'){event.preventDefault();if(!jobRunning)prompt.focus();}});

async function loadHistory(){
  try{
    const d=await fetchJSON('/api/history');
    const messages=d.messages||[];
    if(!messages.length)return;
    if(conversation.querySelector('.welcome'))conversation.innerHTML='';
    const rendered=[...conversation.querySelectorAll('.message-group')].length;
    if(rendered>0)return;
    messages.forEach(message=>addMessage(message.role,message.content));
  }catch{}
}

loadSettings();
loadHistory();
fetch('/api/state').then(response=>response.json()).then(setProgress).catch(()=>{});
