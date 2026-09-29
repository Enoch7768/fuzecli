const $=s=>document.querySelector(s),$$=s=>[...document.querySelectorAll(s)];
let cfg=null,currentPreview="index.html",selected=null,previewHTML="",attachedFiles=[],chatAbort=null,previewHTMLWaiter=null;
async function api(path,opt={}){const externalSignal=opt.signal||null;const controller=externalSignal?null:new AbortController();const signal=externalSignal||controller.signal;const timeoutMs=Number.isFinite(opt.timeoutMs)?Math.max(0,opt.timeoutMs):30000;const timeout=controller&&timeoutMs>0?setTimeout(()=>controller.abort(),timeoutMs):null;const requestOptions={...opt};delete requestOptions.timeoutMs;try{const r=await fetch(path,{credentials:"same-origin",...requestOptions,signal,headers:{"Content-Type":"application/json",...(opt.headers||{})}});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||`Request failed (${r.status})`);return d}catch(e){if(e.name==="AbortError"){if(externalSignal?.aborted)throw e;const timeoutError=Error(`Request timed out after ${Math.round(timeoutMs/1000)} seconds`);timeoutError.name="TimeoutError";throw timeoutError}throw e}finally{if(timeout)clearTimeout(timeout)}}
function esc(s){return String(s??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
function toast(t){const e=$("#toast");e.textContent=t;e.classList.add("show");clearTimeout(window.__toast);window.__toast=setTimeout(()=>e.classList.remove("show"),2400)}
function renderAttachments(){const e=$("#attachmentList");e.innerHTML=attachedFiles.map(f=>'<span class="attachment-chip">'+esc(f)+'<button type="button" data-remove="'+encodeURIComponent(f)+'">×</button></span>').join("");document.querySelectorAll('[data-remove]').forEach(b=>b.onclick=()=>{attachedFiles=attachedFiles.filter(x=>x!==decodeURIComponent(b.dataset.remove));renderAttachments()})}
async function uploadFiles(fileList){const fd=new FormData();[...fileList].forEach(f=>fd.append("files",f,f.name));const r=await fetch("/v1/upload",{method:"POST",credentials:"same-origin",body:fd});const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||"Upload failed");attachedFiles=[...new Set([...attachedFiles,...(d.files||[])])];renderAttachments();toast((d.files||[]).length+" file(s) attached to workspace")}
function msg(role,text){$("#chatEmpty")?.remove();const e=document.createElement("div");e.className="msg "+role;e.innerHTML='<div class="avatar">'+(role==="user"?"You":"F")+'</div><div class="bubble">'+esc(text).replace(/\\n/g,"<br>")+'</div>';$("#messages").append(e);$("#messages").scrollTop=$("#messages").scrollHeight}
function renderProviders(){const p=cfg.providers;const options=p.map(x=>'<option value="'+esc(x.name)+'">'+esc(x.name)+'</option>').join("");$("#providerSelect").innerHTML=options;$("#settingsProvider").innerHTML=options;$("#providerSelect").value=cfg.default_provider;$("#settingsProvider").value=cfg.default_provider;models($("#providerSelect").value)}
async function models(provider){const name=provider||$("#providerSelect").value;const p=cfg.providers.find(x=>x.name===name)||cfg.providers[0];try{const d=await api("/v1/models?provider="+encodeURIComponent(name),{timeoutMs:120000});const list=d.models||[];const fallback=d.default_model||p.default_model||"";const cap=d.capabilities||{};$("#modelSelect").title="Streaming: "+!!cap.streaming+" · Structured JSON: "+!!cap.structured_json+" · Vision: "+!!cap.vision+" · Tools: "+!!cap.tool_calling;$("#modelSelect").innerHTML=(list.length?list:[fallback]).filter(Boolean).map(x=>'<option value="'+esc(x)+'">'+esc(x)+'</option>').join("")||'<option value="">Auto select available model</option>';if(fallback&&list.includes(fallback))$("#modelSelect").value=fallback;const sp=cfg.providers.find(x=>x.name===$("#settingsProvider").value)||p;$("#settingsModel").innerHTML=(list.length?list:[fallback]).filter(Boolean).map(x=>'<option value="'+esc(x)+'">'+esc(x)+'</option>').join("")||'<option value="">Auto select available model</option>';if(fallback)$("#settingsModel").value=fallback}catch(e){const fallback=p.default_model&&p.default_model!=="auto"&&p.default_model!=="default"?p.default_model:"";$("#modelSelect").innerHTML=fallback?'<option value="'+esc(fallback)+'">'+esc(fallback)+' (configured)</option>':'<option value="">Automatic model discovery</option>';$("#settingsModel").innerHTML=$("#modelSelect").innerHTML}}
function keys(){$("#keyRows").innerHTML=cfg.providers.map(p=>'<div class="key-row"><span>'+esc(p.name)+'</span><span class="configured">'+(p.configured?"Configured":"Not configured")+"</span></div>").join("")}
async function loadConfig(){cfg=await api("/v1/config");renderProviders();keys();$("#workspacePath").textContent=cfg.workspace;$("#statusText").textContent="Ready"}
let studioEvents=null,studioEventsReady=null,studioRequestID="",studioStreamChunks=0,studioStreamBytes=0,liveTelemetryTimer=null;
function startStudioEvents(){
  if(studioEvents?.readyState===EventSource.OPEN)return Promise.resolve();
  if(studioEventsReady)return studioEventsReady;
  studioEventsReady=new Promise((resolve,reject)=>{
    const es=new EventSource("/v1/events");
    let settled=false;
    const settle=(fn,v)=>{if(settled)return;settled=true;fn(v)};
    const timer=setTimeout(()=>{try{es.close()}catch(_){};studioEvents=null;studioEventsReady=null;settle(reject,Error("Agent event stream connection timed out"))},8000);
    const consume=e=>{try{handleStudioEvent(JSON.parse(e.data))}catch(_){}};
    es.onopen=()=>{clearTimeout(timer);studioEvents=es;settle(resolve)};
    es.onmessage=consume;
    ["session.started","provider.selected","generation.started","generation.usage","generation.completed","generation.streaming_unavailable","validation.failed","validation.completed","apply.started","apply.failed","generation.file_applied","session.completed","generation.failed"].forEach(type=>es.addEventListener(type,consume));
    es.addEventListener("generation.chunk",e=>{try{const d=JSON.parse(e.data);studioStreamChunks++;studioStreamBytes+=String(d.message||"").length;handleStudioEvent(d)}catch(_){}});
    es.onerror=()=>{if(!settled){clearTimeout(timer);try{es.close()}catch(_){};studioEvents=null;studioEventsReady=null;settle(reject,Error("Agent event stream connection failed"))}else if(studioRequestID)$("#chatState").textContent="Reconnecting to agent…"};
  });
  return studioEventsReady;
}
function handleStudioEvent(e){
  if(studioRequestID&&e.request_id!==studioRequestID)return;
  let pct=Number(e.percent||0);
  if(e.type==="generation.usage"){updateLiveUsage(e);$("#chatState").textContent="Tracking usage…";return}
  if(e.type==="generation.chunk"){
    const estimated=Math.min(58,16+Math.max(1,studioStreamChunks)*0.35+Math.min(18,studioStreamBytes/2400));
    pct=Math.max(pct,estimated);
  }
  if(pct>0)setProgress(pct,eventLabel(e));
  if(e.type==="provider.selected")$("#chatState").textContent="Using "+(e.provider||"provider")+(e.model?" · "+e.model:"");
  else if(e.type==="generation.started")$("#chatState").textContent="Generating…";
  else if(e.type==="generation.chunk")$("#chatState").textContent="Generating…";
  else if(e.type==="generation.completed")$("#chatState").textContent="Validating structured response…";
  else if(e.type==="validation.completed")$("#chatState").textContent="Validated response";
  else if(e.type==="apply.started")$("#chatState").textContent="Applying files…";
  else if(e.type==="generation.file_applied")$("#chatState").textContent="Applied "+(e.path||"file");
  else if(e.type==="session.completed")$("#chatState").textContent="Complete";
  else if(e.type==="generation.failed"||e.type==="validation.failed"||e.type==="apply.failed")$("#chatState").textContent="Agent stopped";
}
let liveTelemetrySnapshot={requests:0,tokens:0,cost:0};
function updateLiveUsage(e){
  const t=Number(e.total_tokens||0), c=Number(e.cost_usd||0);
  $("#liveTokens").textContent=t.toLocaleString();
  $("#liveCost").textContent=c>0?"$"+c.toFixed(4):"Cost pending";
}
async function refreshLiveTelemetry(){
  try{
    const d=await api("/v1/telemetry",{timeoutMs:10000});
    const providers=Object.values(d.providers||{});
    const requests=providers.reduce((n,p)=>n+Number(p.requests||0),0);
    const tokens=providers.reduce((n,p)=>n+Number(p.total_tokens||0),0);
    const cost=providers.reduce((n,p)=>n+Number(p.total_cost_usd||0),0);
    liveTelemetrySnapshot={requests,tokens,cost};
    $("#liveRequests").textContent=requests.toLocaleString();
    $("#liveTokens").textContent=tokens.toLocaleString();
    $("#liveCost").textContent=cost>0?"$"+cost.toFixed(4):"Cost pending";
    if(document.querySelector("#view-telemetry.active"))renderTelemetrySnapshot(d);
  }catch(e){}
}
function renderTelemetrySnapshot(d){
  const rows=Object.values(d.providers||{});
  if(!rows.length){$("#telemetryGrid").innerHTML='<div class="panel telemetry-empty"><b>No provider traffic yet</b><span>Run a request and the dashboard will update automatically.</span></div>'}
  else{
    rows.sort((a,b)=>(b.requests||0)-(a.requests||0));
    $("#telemetryGrid").innerHTML=rows.map(p=>{
      const success=p.requests?Math.round((p.successes/p.requests)*100):0;
      const avg=p.requests?Math.round((p.total_latency||0)/p.requests/1000000):0;
      return '<div class="panel telemetry-card"><div class="telemetry-head"><div><b>'+esc(p.provider)+'</b><span>'+esc(p.last_error||'Healthy session')+'</span></div><strong>'+success+'%</strong></div><div class="telemetry-metrics"><div><small>Requests</small><b>'+p.requests+'</b></div><div><small>Success</small><b>'+p.successes+'</b></div><div><small>Failures</small><b>'+p.failures+'</b></div><div><small>Avg latency</small><b>'+avg+' ms</b></div><div><small>Prompt tokens</small><b>'+Number(p.prompt_tokens||0).toLocaleString()+'</b></div><div><small>Output tokens</small><b>'+Number(p.completion_tokens||0).toLocaleString()+'</b></div><div><small>Total tokens</small><b>'+Number(p.total_tokens||0).toLocaleString()+'</b></div><div><small>Cost</small><b>'+((Number(p.total_cost_usd||0)>0)?'
  switch(e.type){
    case "session.started":return e.message||"Preparing workspace";
    case "provider.selected":return "Provider selected";
    case "generation.started":return "Generating response";
    case "generation.chunk":return studioStreamChunks>1?"Generating response":"Receiving provider response";
    case "generation.completed":return "Validating structured response";
    case "generation.streaming_unavailable":return "Using standard provider response";
    case "validation.completed":return "Validated response";
    case "apply.started":return "Applying workspace changes";
    case "generation.file_applied":return "Applied "+(e.path||"file");
    case "session.completed":return "Complete";
    default:return e.type==="generation.failed"||e.type==="validation.failed"||e.type==="apply.failed"?"Agent stopped":"Working…";
  }
}
function setProgress(percent,label){$("#generationProgress").hidden=false;$("#progressFill").style.width=Math.max(0,Math.min(100,percent))+"%";$("#progressPercent").textContent=Math.round(percent)+"%";$("#progressLabel").textContent=label}
function finishProgress(){setProgress(100,"Complete");setTimeout(()=>$("#generationProgress").hidden=true,800)}
function promptNeedsApproval(p){const s=String(p||"").toLowerCase();const analysisOnly=/(analy[sz]e|audit|review|inspect|assess|identify|suggest|recommend|improvement|improvements|before changing|before making changes)/.test(s);const explicitChange=/(fix|change|edit|update|implement|apply|build|create|add|remove|refactor|rewrite|replace|do so|make those changes|go ahead)/.test(s);return analysisOnly&&!explicitChange}
async function send(){
  const b=$("#prompt"),p=b.value.trim();if(!p||chatAbort)return;
  b.value="";b.style.height="auto";msg("user",p);$("#sendBtn").disabled=true;$("#stopBtn").disabled=false;$("#chatState").textContent="Starting agent…";chatAbort=new AbortController();
  studioRequestID="studio-"+Date.now()+"-"+Math.random().toString(36).slice(2);studioStreamChunks=0;studioStreamBytes=0;
  const applyChanges=!promptNeedsApproval(p);const agentPrompt=promptNeedsApproval(p)?"Analyze the actual workspace and current files. Give concrete, evidence-based findings and exactly three high-value improvements before changing anything. Do not modify files. "+p:p;
  try{
    await startStudioEvents();
    setProgress(2,applyChanges?"Starting agent":"Analyzing workspace");
    const d=await api("/v1/chat",{method:"POST",signal:chatAbort.signal,body:JSON.stringify({request_id:studioRequestID,prompt:agentPrompt,provider:$("#providerSelect").value,model:$("#modelSelect").value,files:attachedFiles,apply:applyChanges,billing_mode:$("#billingMode").value})});
    setProgress(96,"Finalizing workspace");
    msg("assistant",d.content+(d.written_files?.length?"\n\nChanged:\n"+d.written_files.join("\n"):""));
    attachedFiles=[];renderAttachments();finishProgress();
    if(d.written_files?.some(x=>/\\.(html?|css|js)$/i.test(x)))loadBuilder(currentPreview);
  }catch(e){
    if(e.name==="AbortError"){setProgress(100,"Stopped");msg("assistant","Request stopped.");setTimeout(()=>$("#generationProgress").hidden=true,900)}
    else{setProgress(100,"Failed");msg("assistant","Request failed: "+e.message);setTimeout(()=>$("#generationProgress").hidden=true,1200)}
  }finally{$("#sendBtn").disabled=false;$("#stopBtn").disabled=true;$("#chatState").textContent="Ready";chatAbort=null;studioRequestID="";b.focus()}
}
async function files(){const d=await api("/v1/files");return d.files}
function isSiteFile(f){return /\.(html?|css|js|mjs|json|svg|png|jpe?g|webp|gif|ico|woff2?|ttf|otf)$/i.test(f)}
async function loadBuilder(preferred){const all=await files();const site=all.filter(isSiteFile);$("#fileCount").textContent=site.length;$("#builderFileList").innerHTML=site.map(f=>'<button class="file-row '+(f===currentPreview?"selected":"")+'" data-file="'+encodeURIComponent(f)+'">'+esc(f)+"</button>").join("");const htmls=site.filter(f=>/\.html?$/i.test(f));const target=htmls.includes(preferred)?preferred:(htmls[0]||"");$("#previewFile").innerHTML=htmls.length?htmls.map(f=>'<option value="'+esc(f)+'">'+esc(f)+"</option>").join(""):'<option value="">No HTML page</option>';if(target){$("#previewFile").value=target;currentPreview=target;openSite(target)}document.querySelectorAll(".builder-files .file-row").forEach(e=>e.onclick=()=>{const f=decodeURIComponent(e.dataset.file);if(/\.html?$/i.test(f))openSite(f);else editRawFile(f)})}
async function runRuntimePreview(){try{$("#runRuntimePreview").disabled=true;$("#previewStatus").textContent="Starting local runtime…";const runtime=$("#runtimeSelect").value;const d=await api("/v1/preview/runtime?runtime="+encodeURIComponent(runtime),{method:"POST"});$("#sitePreview").sandbox="allow-scripts allow-forms";$("#sitePreview").src=d.url+"?fuzecli="+Date.now();$("#previewStatus").textContent="Live "+d.kind+" runtime · "+d.url;toast(d.kind.toUpperCase()+" preview running")}catch(e){$("#previewStatus").textContent="Runtime preview unavailable";toast(e.message)}finally{$("#runRuntimePreview").disabled=false}}
function openSite(file=currentPreview){if(!file)return;currentPreview=file;selected=null;previewHTML="";$("#applyInspector").textContent="Apply live";$("#requestSavePreview").style.display="block";$("#sitePreview").sandbox="allow-scripts allow-forms";$("#inspectorForm").hidden=true;$("#inspectorEmpty").hidden=false;$("#selectedPath").textContent="Nothing selected";$("#sitePreview").src="/preview/"+encodeURI(file)+"?t="+Date.now();document.querySelectorAll(".builder-files .file-row").forEach(e=>e.classList.toggle("selected",decodeURIComponent(e.dataset.file)===file))}
async function editRawFile(file){try{const d=await api("/v1/file?path="+encodeURIComponent(file));$("#selectedPath").textContent=file;$("#inspectorEmpty").hidden=true;$("#inspectorForm").hidden=false;$("#selectedTag").textContent="SOURCE FILE";$("#editText").value=d.content;selected={source:"file",path:file};$("#applyInspector").textContent="Save file";$("#requestSavePreview").style.display="none";toast(file+" opened for editing")}catch(e){toast(e.message)}}
function postEditor(type,payload={}){$("#sitePreview").contentWindow?.postMessage({source:"fuzecli-editor",type,...payload},"*")}
async function applyInspector(){if(!selected)return;if(selected.source==="file"){try{await api("/v1/file",{method:"POST",body:JSON.stringify({path:selected.path,content:$("#editText").value})});toast("Saved "+selected.path);await workspace()}catch(e){toast(e.message)}return}const styles={color:$("#editColor").value.trim(),backgroundColor:$("#editBackground").value.trim(),fontSize:$("#editFontSize").value.trim(),padding:$("#editPadding").value.trim(),borderRadius:$("#editRadius").value.trim()};Object.keys(styles).forEach(k=>{if(!styles[k])delete styles[k]});postEditor("text",{value:$("#editText").value});postEditor("style",{styles});if($("#imageInspector").hidden===false)postEditor("attr",{src:$("#editSrc").value.trim(),alt:$("#editAlt").value.trim()});if($("#linkInspector").hidden===false)postEditor("attr",{href:$("#editHref").value.trim()});$("#builderSaveState").textContent="Unsaved";toast("Change applied live")}
async function savePreview(){if(!currentPreview)return;if(!previewHTML){postEditor("request-html");try{await new Promise((resolve,reject)=>{const timer=setTimeout(()=>{previewHTMLWaiter=null;reject(Error("Preview did not return its current HTML"))},5000);previewHTMLWaiter={resolve:()=>{clearTimeout(timer);previewHTMLWaiter=null;resolve()}}})}catch(e){toast(e.message);return}}try{const cleanHTML=String(previewHTML||"").replace(/<script[^>]*data-fuzecli-preview-editor=["']true["'][\\s\\S]*?<\\/script>/gi,"");await api("/v1/file",{method:"POST",body:JSON.stringify({path:currentPreview,content:cleanHTML})});$("#builderSaveState").textContent="Saved";toast("Saved "+currentPreview);await loadBuilder(currentPreview)}catch(e){toast("Save failed: "+e.message)}}
function builderMessage(e){const d=e.data;if(!d||d.source!=="fuzecli-preview")return;if(d.type==="ready"){$("#previewStatus").textContent="Live · click an element to inspect";$("#builderSaveState").textContent="Saved"}if(d.type==="select"){selected=d;$("#applyInspector").textContent="Apply live";$("#requestSavePreview").style.display="block";$("#previewStatus").textContent=d.tag?"Selected "+d.tag:"Nothing selected";$("#selectedPath").textContent=d.path||"Nothing selected";$("#inspectorEmpty").hidden=!d.tag;$("#inspectorForm").hidden=!d.tag;$("#selectedTag").textContent=(d.tag||"ELEMENT").toUpperCase();$("#editText").value=d.text||"";$("#editColor").value=d.color||"";$("#editBackground").value=d.background||"";$("#editFontSize").value=d.fontSize||"";$("#editPadding").value=d.padding||"";$("#editRadius").value=d.radius||"";$("#imageInspector").hidden=d.tag!=="img";$("#linkInspector").hidden=d.tag!=="a";$("#editSrc").value=d.src||"";$("#editAlt").value=d.alt||"";$("#editHref").value=d.href||""}if(d.type==="changed"){previewHTML=d.html;$("#previewStatus").textContent="Unsaved changes";$("#builderSaveState").textContent="Unsaved";if(previewHTMLWaiter)previewHTMLWaiter.resolve()}}
async function workspace(){const d=await api("/v1/files");$("#workspaceFileCount").textContent=d.files.length;$("#fileList").innerHTML=d.files.map(f=>'<button class="file-row" data-f="'+encodeURIComponent(f)+'">'+esc(f)+"</button>").join("");document.querySelectorAll(".file-panel .file-row").forEach(e=>e.onclick=async()=>{try{const d=await api("/v1/file?path="+e.dataset.f);$("#fileName").textContent=d.path;$("#fileContent").textContent=d.content}catch(x){toast(x.message)}})}
async function activity(){const d=await api("/v1/history");$("#activityList").innerHTML=d.messages.length?d.messages.map(m=>'<div class="activity-row"><div class="activity-role">'+esc(m.role)+"</div><div>"+esc(m.content)+"</div></div>").join(""):'<div class="activity-row">No activity yet.</div>'}
async function telemetry(){try{const d=await api("/v1/telemetry");renderTelemetrySnapshot(d);refreshLiveTelemetry();}catch(e){toast(e.message)}}

initIDEEvents();
