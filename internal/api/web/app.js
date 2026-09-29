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
let studioEvents=null,studioEventsReady=null,studioRequestID="",studioStreamChunks=0,studioStreamBytes=0;
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
    ["session.started","provider.selected","generation.started","generation.completed","generation.streaming_unavailable","validation.failed","validation.completed","apply.started","apply.failed","generation.file_applied","session.completed","generation.failed"].forEach(type=>es.addEventListener(type,consume));
    es.addEventListener("generation.chunk",e=>{try{const d=JSON.parse(e.data);studioStreamChunks++;studioStreamBytes+=String(d.message||"").length;handleStudioEvent(d)}catch(_){}});
    es.onerror=()=>{if(!settled){clearTimeout(timer);try{es.close()}catch(_){};studioEvents=null;studioEventsReady=null;settle(reject,Error("Agent event stream connection failed"))}else if(studioRequestID)$("#chatState").textContent="Reconnecting to agent…"};
  });
  return studioEventsReady;
}
function handleStudioEvent(e){
  if(studioRequestID&&e.request_id!==studioRequestID)return;
  let pct=Number(e.percent||0);
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
function eventLabel(e){
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
  const applyChanges=!promptNeedsApproval(p);
  try{
    await startStudioEvents();
    setProgress(2,applyChanges?"Starting agent":"Analyzing workspace");
    const d=await api("/v1/chat",{method:"POST",signal:chatAbort.signal,body:JSON.stringify({request_id:studioRequestID,prompt:p,provider:$("#providerSelect").value,model:$("#modelSelect").value,files:attachedFiles,apply:applyChanges,billing_mode:$("#billingMode").value})});
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
async function applyInspector(){if(!selected)return;if(selected.source==="file"){try{await api("/v1/file",{method:"POST",body:JSON.stringify({path:selected.path,content:$("#editText").value})});toast("Saved "+selected.path);await workspace()}catch(e){toast(e.message)}return}const styles={color:$("#editColor").value.trim(),backgroundColor:$("#editBackground").value.trim(),fontSize:$("#editFontSize").value.trim(),padding:$("#editPadding").value.trim(),borderRadius:$("#editRadius").value.trim()};Object.keys(styles).forEach(k=>{if(!styles[k])delete styles[k]});postEditor("text",{value:$("#editText").value});postEditor("style",{styles});toast("Change applied live")}
async function savePreview(){if(!currentPreview)return;if(!previewHTML){postEditor("request-html");try{await new Promise((resolve,reject)=>{const timer=setTimeout(()=>{previewHTMLWaiter=null;reject(Error("Preview did not return its current HTML"))},5000);previewHTMLWaiter={resolve:()=>{clearTimeout(timer);previewHTMLWaiter=null;resolve()}}})}catch(e){toast(e.message);return}}try{await api("/v1/file",{method:"POST",body:JSON.stringify({path:currentPreview,content:previewHTML})});toast("Saved "+currentPreview);await loadBuilder(currentPreview)}catch(e){toast("Save failed: "+e.message)}}
function builderMessage(e){const d=e.data;if(!d||d.source!=="fuzecli-preview")return;if(d.type==="ready"){$("#previewStatus").textContent="Live · click an element to inspect"}if(d.type==="select"){selected=d;$("#applyInspector").textContent="Apply live";$("#requestSavePreview").style.display="block";$("#previewStatus").textContent="Selected "+d.tag;$("#selectedPath").textContent=d.path;$("#inspectorEmpty").hidden=true;$("#inspectorForm").hidden=false;$("#selectedTag").textContent=d.tag.toUpperCase();$("#editText").value=d.text||"";$("#editColor").value=d.color||"";$("#editBackground").value=d.background||"";$("#editFontSize").value=d.fontSize||"";$("#editPadding").value=d.padding||"";$("#editRadius").value=d.radius||""}if(d.type==="changed"){previewHTML=d.html;$("#previewStatus").textContent="Unsaved changes";if(previewHTMLWaiter)previewHTMLWaiter.resolve()}}
async function workspace(){const d=await api("/v1/files");$("#workspaceFileCount").textContent=d.files.length;$("#fileList").innerHTML=d.files.map(f=>'<button class="file-row" data-f="'+encodeURIComponent(f)+'">'+esc(f)+"</button>").join("");document.querySelectorAll(".file-panel .file-row").forEach(e=>e.onclick=async()=>{try{const d=await api("/v1/file?path="+e.dataset.f);$("#fileName").textContent=d.path;$("#fileContent").textContent=d.content}catch(x){toast(x.message)}})}
async function activity(){const d=await api("/v1/history");$("#activityList").innerHTML=d.messages.length?d.messages.map(m=>'<div class="activity-row"><div class="activity-role">'+esc(m.role)+"</div><div>"+esc(m.content)+"</div></div>").join(""):'<div class="activity-row">No activity yet.</div>'}
async function telemetry(){try{const d=await api("/v1/telemetry");const rows=Object.values(d.providers||{});if(!rows.length){$("#telemetryGrid").innerHTML='<div class="panel telemetry-empty"><b>No provider traffic yet</b><span>Run a request and return here to inspect reliability.</span></div>'}else{rows.sort((a,b)=>(b.requests||0)-(a.requests||0));$("#telemetryGrid").innerHTML=rows.map(p=>{const success=p.requests?Math.round((p.successes/p.requests)*100):0;const avg=p.requests?Math.round((p.total_latency||0)/p.requests/1000000):0;return '<div class="panel telemetry-card"><div class="telemetry-head"><div><b>'+esc(p.provider)+'</b><span>'+esc(p.last_error||'Healthy session')+'</span></div><strong>'+success+'%</strong></div><div class="telemetry-metrics"><div><small>Requests</small><b>'+p.requests+'</b></div><div><small>Success</small><b>'+p.successes+'</b></div><div><small>Failures</small><b>'+p.failures+'</b></div><div><small>Avg latency</small><b>'+avg+' ms</b></div><div><small>Prompt tokens</small><b>'+p.prompt_tokens+'</b></div><div><small>Output tokens</small><b>'+p.completion_tokens+'</b></div><div><small>Total tokens</small><b>'+p.total_tokens+'</b></div><div><small>Rate limits</small><b>'+p.rate_limited+'</b></div></div><div class="telemetry-bar"><i style="width:'+success+'%"></i></div></div>'}).join("")}const events=d.events||[];$("#telemetryEvents").innerHTML=events.slice().reverse().map(e=>'<div class="telemetry-event"><div><b>'+esc(e.provider)+'</b><span>'+esc(e.model||"automatic")+'</span></div><div><b>'+Number(e.total_tokens||0).toLocaleString()+' tokens</b><span>'+Number(e.latency_ms||0).toLocaleString()+' ms</span></div><div class="'+(e.success?"event-ok":"event-error")+'">'+esc(e.success?"OK":(e.error_kind||"error"))+'</div><div class="event-error-text">'+esc(e.error||"")+'</div></div>').join("")||'<div class="telemetry-empty">No request events yet.</div>'}catch(e){toast(e.message)}}
function view(v){document.querySelectorAll(".nav-item").forEach(b=>b.classList.toggle("active",b.dataset.view===v));document.querySelectorAll(".view").forEach(x=>x.classList.toggle("active",x.id==="view-"+v));$("#pageTitle").textContent=v==="builder"?"Live Builder":v[0].toUpperCase()+v.slice(1);if(v==="ide")loadIDE();if(v==="builder")loadBuilder(currentPreview);if(v==="workspace")workspace();if(v==="activity")activity();if(v==="telemetry")telemetry()}
async function save(){try{await api("/v1/config",{method:"POST",body:JSON.stringify({provider:$("#settingsProvider").value,model:$("#settingsModel").value.trim()})});toast("Settings saved");await loadConfig()}catch(e){toast(e.message)}}
document.querySelectorAll(".nav-item").forEach(b=>b.onclick=()=>view(b.dataset.view));initIDEEvents();
document.querySelectorAll(".quick-actions button").forEach(b=>b.onclick=()=>{$("#prompt").value=b.dataset.prompt;$("#prompt").focus();$("#prompt").dispatchEvent(new Event("input"))});
$("#attachBtn").onclick=()=>$("#fileUpload").click();$("#fileUpload").onchange=async e=>{try{await uploadFiles(e.target.files)}catch(x){toast(x.message)}e.target.value=""};$("#providerSelect").onchange=()=>models($("#providerSelect").value);$("#refreshMemoryBtn").onclick=async()=>{try{await api("/v1/memory/refresh",{method:"POST"});toast("Conversation memory refreshed")}catch(e){toast(e.message)}};$("#settingsProvider").onchange=()=>models($("#settingsProvider").value);$("#sendBtn").onclick=send;$("#saveProvider").onclick=save;$("#refreshBtn").onclick=()=>loadConfig().then(()=>toast("Runtime refreshed"));$("#workspaceRefresh").onclick=workspace;$("#activityRefresh").onclick=activity;$("#telemetryRefresh").onclick=telemetry;$("#previewFile").onchange=e=>openSite(e.target.value);$("#runRuntimePreview").onclick=runRuntimePreview;$("#openPreview").onclick=()=>window.open("/preview/"+encodeURI(currentPreview),"_blank","noopener");$("#savePreview").onclick=savePreview;$("#requestSavePreview").onclick=savePreview;$("#applyInspector").onclick=applyInspector;$("#deviceDesktop").onclick=()=>{$("#deviceFrame").className="device desktop"};$("#deviceMobile").onclick=()=>{$("#deviceFrame").className="device mobile"};window.addEventListener("message",builderMessage);
$("#prompt").onkeydown=e=>{if(e.key==="Enter"&&!e.shiftKey){e.preventDefault();send()}};$("#newSessionBtn").onclick=()=>{$("#messages").innerHTML='<div class="chat-empty" id="chatEmpty"><div class="chat-empty-mark">F</div><strong>What are we building?</strong><span>Ask FuzeCLI to inspect, create, fix, explain, or improve your project.</span></div>';$("#sessionTitle").textContent="New coding session";$("#prompt").focus()};$("#stopBtn").onclick=()=>chatAbort?.abort();$("#prompt").oninput=e=>{e.target.style.height="auto";e.target.style.height=Math.min(e.target.scrollHeight,180)+"px"};
(async()=>{try{await loadConfig();await activity();const h=await api("/v1/history");(h.messages||[]).slice(-80).forEach(m=>msg(m.role,m.content))}catch(e){$("#statusText").textContent="Offline";toast(e.message)}})();
let ideEditor=null,ideFiles=[],ideOpen=[],ideActive="",ideSaveTimer=null;
let ideLSPSocket=null,ideLSPSeq=100,ideLSPPending=new Map(),ideLSPVersion=0,ideLSPPath="",ideLSPProvidersRegistered=false;
let ideDebugSocket=null,ideDebugSeq=1,ideDebugPending=new Map(),ideDebugInitialized=false,ideDebugRunning=false,ideDebugConnectTimer=null;

function ideLang(path){const e=(path.split(".").pop()||"").toLowerCase();return ({js:"javascript",mjs:"javascript",cjs:"javascript",ts:"typescript",tsx:"typescript",jsx:"javascript",go:"go",py:"python",json:"json",css:"css",html:"html",htm:"html",md:"markdown",yaml:"yaml",yml:"yaml",sql:"sql",sh:"shell",xml:"xml",svg:"xml",php:"php"})[e]||"plaintext"}
function ideTree(files){const root={children:{},files:[]};files.forEach(path=>{const parts=path.split("/").filter(Boolean);let n=root;parts.forEach((part,i)=>{if(i===parts.length-1)n.files.push({name:part,path});else{n.children[part]??={children:{},files:[]};n=n.children[part]}})});const render=n=>{let out="";Object.keys(n.children).sort().forEach(name=>{out+='<div class="ide-folder" data-folder="'+esc(name)+'"><span>›</span><b>▱</b>'+esc(name)+'</div><div class="ide-folder-children">'+render(n.children[name])+"</div>"});n.files.sort((a,b)=>a.name.localeCompare(b.name)).forEach(f=>out+='<button class="ide-file '+(f.path===ideActive?"active":"")+'" data-ide-file="'+encodeURIComponent(f.path)+'"><span class="file-dot '+ideLang(f.path)+'"></span>'+esc(f.name)+"</button>");return out};$("#ideExplorerTree").innerHTML=render(root)}

function ideURI(path){let clean=String(path||"").replace(/\\/g,"/");if(!/^[A-Za-z]:\//.test(clean)&&cfg?.workspace){const root=String(cfg.workspace).replace(/\\/g,"/").replace(/\/$/,"");clean=root+"/"+clean.replace(/^\/+/,"")}return encodeURI("file:///"+clean.replace(/^\/+/, ""))}
function idePosition(pos){return {line:pos.lineNumber-1,character:pos.column-1}}
function ideLSPRequest(method,params){if(!ideLSPSocket||ideLSPSocket.readyState!==WebSocket.OPEN)return Promise.reject(Error("Language server is not connected"));const id=ideLSPSeq++;try{ideLSPSocket.send(JSON.stringify({jsonrpc:"2.0",id,method,params}))}catch(e){return Promise.reject(e)}return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{ideLSPPending.delete(id);reject(Error("Language server request timed out"))},10000);ideLSPPending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value)},reject:error=>{clearTimeout(timer);reject(error)}})})}
function ideLSPNotify(method,params){if(ideLSPSocket?.readyState===WebSocket.OPEN)ideLSPSocket.send(JSON.stringify({jsonrpc:"2.0",method,params}))}
function ideLSPRange(r){return new monaco.Range(r.start.line+1,r.start.character+1,r.end.line+1,r.end.character+1)}
function ideLSPSeverity(v){return v===1?monaco.MarkerSeverity.Error:v===2?monaco.MarkerSeverity.Warning:v===3?monaco.MarkerSeverity.Info:monaco.MarkerSeverity.Hint}
function ideLSPResult(value){return value?.result??value}
function ideLSPLocations(value){const v=ideLSPResult(value);if(!v)return[];return(Array.isArray(v)?v:[v]).map(x=>({uri:x.uri||x.targetUri,range:x.range||x.targetSelectionRange})).filter(x=>x.uri&&x.range)}
function ideLSPCompletionKind(kind){const map={1:18,2:17,3:5,4:8,5:9,6:14,7:2,8:10,9:3,10:1,11:4,12:11,13:15,14:7,15:6,16:19,17:16,18:13,19:12,20:21,21:20,22:22,23:23,24:24,25:25};return map[kind]||monaco.languages.CompletionItemKind.Text}
function registerLSPProviders(){if(ideLSPProvidersRegistered||!window.monaco)return;ideLSPProvidersRegistered=true;const langs=["go","typescript","javascript","python","rust","php","java","c","cpp","ruby","kotlin"];langs.forEach(lang=>{
monaco.languages.registerCompletionItemProvider(lang,{triggerCharacters:[".",":","/","<"],provideCompletionItems:async(model,pos)=>{
try{const d=await ideLSPRequest("textDocument/completion",{textDocument:{uri:model.uri.toString()},position:idePosition(pos),context:{triggerKind:1}});const result=ideLSPResult(d);const items=result?.items||result||[];return{suggestions:(Array.isArray(items)?items:[]).map(x=>{const edit=x.textEdit?.newText||x.insertText||x.label;const range=x.textEdit?.range?ideLSPRange(x.textEdit.range):undefined;return{label:x.label,kind:ideLSPCompletionKind(x.kind),detail:x.detail,documentation:typeof x.documentation==="string"?x.documentation:(x.documentation?.value||""),insertText:edit,range}})}}catch(e){return{suggestions:[]}}}});
monaco.languages.registerHoverProvider(lang,{provideHover:async(model,pos)=>{try{const d=ideLSPResult(await ideLSPRequest("textDocument/hover",{textDocument:{uri:model.uri.toString()},position:idePosition(pos)}));if(!d?.contents)return null;const values=Array.isArray(d.contents)?d.contents:[d.contents];return{contents:values.map(x=>({value:typeof x==="string"?x:(x.value||"")})),range:d.range?ideLSPRange(d.range):undefined}}catch(e){return null}}});
monaco.languages.registerDefinitionProvider(lang,{provideDefinition:async(model,pos)=>{try{return ideLSPLocations(await ideLSPRequest("textDocument/definition",{textDocument:{uri:model.uri.toString()},position:idePosition(pos)})).map(x=>({uri:monaco.Uri.parse(x.uri),range:ideLSPRange(x.range)}))}catch(e){return[]}}});
monaco.languages.registerReferenceProvider(lang,{provideReferences:async(model,pos)=>{try{return ideLSPLocations(await ideLSPRequest("textDocument/references",{textDocument:{uri:model.uri.toString()},position:idePosition(pos),context:{includeDeclaration:true}})).map(x=>({uri:monaco.Uri.parse(x.uri),range:ideLSPRange(x.range)}))}catch(e){return[]}}});
})}

function ideLSPOpen(path){if(!ideLSPSocket||ideLSPSocket.readyState!==WebSocket.OPEN||!ideEditor)return;ideLSPVersion++;ideLSPNotify("textDocument/didOpen",{textDocument:{uri:ideURI(path),languageId:ideLang(path),version:ideLSPVersion,text:ideEditor.getValue()}})}
function ideLSPChange(path,text){if(!ideLSPSocket||ideLSPSocket.readyState!==WebSocket.OPEN)return;ideLSPVersion++;ideLSPNotify("textDocument/didChange",{textDocument:{uri:ideURI(path),version:ideLSPVersion,contentChanges:[{text}]}})}
function ideLSPClose(path){ideLSPNotify("textDocument/didClose",{textDocument:{uri:ideURI(path)}})}
function ideLSPConnect(path){const lang=ideLang(path);const supported=["go","typescript","javascript","python","rust","php","java","c","cpp","ruby","kotlin"];if(!supported.includes(lang))return;if(ideLSPSocket){try{ideLSPClose(ideLSPPath)}catch(e){}try{ideLSPSocket.close()}catch(e){}}ideLSPPath=path;ideLSPVersion=0;$("#ideBreadcrumbs").textContent="Connecting language server · "+lang;const proto=location.protocol==="https:"?"wss":"ws";ideLSPSocket=new WebSocket(proto+"://"+location.host+"/v1/lsp?language="+encodeURIComponent(lang));ideLSPSocket.onopen=async()=>{try{const root=cfg?.workspace?ideURI(cfg.workspace):null;await ideLSPRequest("initialize",{processId:null,rootUri:root,workspaceFolders:root?[{uri:root,name:"FuzeCLI Workspace"}]:null,capabilities:{textDocument:{synchronization:{dynamicRegistration:false,willSave:false,willSaveWaitUntil:false,didSave:false},completion:{completionItem:{snippetSupport:false}},hover:{contentFormat:["markdown","plaintext"]},definition:{},references:{},publishDiagnostics:{}}}});ideLSPNotify("initialized",{});ideLSPOpen(path)}catch(e){toast("Language server initialization failed: "+e.message)}};
ideLSPSocket.onmessage=e=>{try{const d=JSON.parse(e.data);if(d.id!==undefined&&ideLSPPending.has(d.id)){const p=ideLSPPending.get(d.id);ideLSPPending.delete(d.id);d.error?p.reject(Error(d.error.message||"LSP request failed")):p.resolve(d.result);return}if(d.method==="textDocument/publishDiagnostics"){const uri=d.params?.uri||"";const model=monaco.editor.getModels().find(m=>m.uri.toString()===uri);if(model){const markers=(d.params.diagnostics||[]).map(x=>({severity:ideLSPSeverity(x.severity),message:x.message,startLineNumber:x.range.start.line+1,startColumn:x.range.start.character+1,endLineNumber:x.range.end.line+1,endColumn:x.range.end.character+1,code:x.code?String(x.code):undefined,source:x.source||"LSP"}));monaco.editor.setModelMarkers(model,"fuzecli-lsp",markers);$("#problemCount").textContent=String(markers.length)}}}catch(e){}};
ideLSPSocket.onerror=()=>{$("#ideBreadcrumbs").textContent="Language server unavailable · "+lang;toast("Language server connection failed. Make sure "+lang+" language-server tooling is installed.")};
ideLSPSocket.onclose=()=>{for(const p of ideLSPPending.values())p.reject(Error("Language server disconnected"));ideLSPPending.clear();ideLSPSocket=null;if(ideActive===path)$("#ideBreadcrumbs").textContent="Language server disconnected · "+lang};
}

async function ideOpenFile(path){try{const d=await api("/v1/file?path="+encodeURIComponent(path));if(ideActive&&ideActive!==path)ideLSPClose(ideActive);if(!ideOpen.includes(path))ideOpen.push(path);ideActive=path;renderIdeTabs();renderIdeEditor(d.content,path);$("#ideAIContext").textContent=path;$("#ideBreadcrumbs").textContent="Workspace / "+path;ideTree(ideFiles)}catch(e){toast(e.message)}}
function renderIdeTabs(){const el=$("#ideTabs");el.innerHTML=ideOpen.map(p=>'<button class="ide-tab '+(p===ideActive?"active":"")+'" data-ide-tab="'+encodeURIComponent(p)+'"><span class="file-dot '+ideLang(p)+'"></span>'+esc(p.split("/").pop())+'<i data-ide-close="'+encodeURIComponent(p)+'">×</i></button>').join("")}
function renderIdeEditor(content,path){const host=$("#ideEditor");if(!host)return;if(ideEditor){ideEditor.dispose();ideEditor=null}host.innerHTML="";if(!window.monaco){const ta=document.createElement("textarea");ta.id="ideFallbackEditor";ta.className="ide-fallback-editor";ta.value=content;ta.spellcheck=false;ta.addEventListener("input",()=>{$(".ide-tab.active")?.classList.add("dirty")});ta.addEventListener("keydown",e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==="s"){e.preventDefault();ideSaveFallback(path)}});host.appendChild(ta);$("#ideBreadcrumbs").textContent="Workspace / "+path+" · text editor";return}registerLSPProviders();const model=monaco.editor.createModel(content,ideLang(path),monaco.Uri.parse(ideURI(path)));ideEditor=monaco.editor.create(host,{model,theme:"vs-dark",automaticLayout:true,minimap:{enabled:true},fontFamily:"Cascadia Code, Consolas, monospace",fontSize:13,lineHeight:21,wordWrap:"off",smoothScrolling:true,scrollBeyondLastLine:false,bracketPairColorization:{enabled:true},padding:{top:14,bottom:18},stickyScroll:{enabled:true},quickSuggestions:true,glyphMargin:true});ideEditor.addCommand(monaco.KeyMod.CtrlCmd|monaco.KeyCode.KeyS,ideSaveFile);ideEditor.onDidChangeModelContent(()=>{ideLSPChange(path,ideEditor.getValue());clearTimeout(ideSaveTimer);ideSaveTimer=setTimeout(()=>$(".ide-tab.active")?.classList.add("dirty"),500);ideRenderOutline()});ideEditor.onMouseDown(e=>{if(e.target.type===monaco.editor.MouseTargetType.GUTTER_GLYPH_MARGIN)ideToggleBreakpoint(e.target.position.lineNumber)});if(ideLSPSocket?.readyState===WebSocket.OPEN&&ideLSPPath===path)ideLSPOpen(path);else ideLSPConnect(path)}
async function ideSaveFallback(path){const ta=$("#ideFallbackEditor");if(!ta)return;try{await api("/v1/file",{method:"POST",body:JSON.stringify({path,content:ta.value})});$(".ide-tab.active")?.classList.remove("dirty");toast("Saved "+path);await loadIDE()}catch(e){toast("Save failed: "+e.message)}}
async function ideSaveFile(){if(!ideActive)return;const content=ideEditor?ideEditor.getValue():$("#ideFallbackEditor")?.value;if(content===undefined)return;try{await api("/v1/file",{method:"POST",body:JSON.stringify({path:ideActive,content})});$(".ide-tab.active")?.classList.remove("dirty");toast("Saved "+ideActive);await workspace()}catch(e){toast("Save failed: "+e.message)}}
async function loadIDE(){try{ideFiles=await files();ideTree(ideFiles);$("#ideWorkspaceName").textContent=(cfg?.workspace||"WORKSPACE").split(/[\\/]/).pop().toUpperCase();if(window.monaco){registerLSPProviders();if(ideActive)ideOpenFile(ideActive)}else if(window.require){window.require.config({paths:{vs:"https://cdn.jsdelivr.net/npm/monaco-editor@0.52.2/min/vs"}});window.require(["vs/editor/editor.main"],()=>{registerLSPProviders();if(ideActive)ideOpenFile(ideActive)})}}catch(e){toast(e.message)}}
async function askIDE(extra){const p=(extra||$("#idePrompt").value).trim();if(!p)return;const context=ideActive&&ideEditor?"\n\nCURRENT FILE: "+ideActive+"\n\n"+ideEditor.getValue():"";const applyChanges=!promptNeedsApproval(p);$("#ideAIMessages").innerHTML+='<div class="ide-ai-msg user">'+esc(p)+'</div>';$("#idePrompt").value="";try{const d=await api("/v1/chat",{method:"POST",body:JSON.stringify({prompt:p+context,provider:$("#providerSelect").value,model:$("#modelSelect").value,files:ideActive?[ideActive]:[],apply:applyChanges,billing_mode:$("#billingMode").value})});$("#ideAIMessages").innerHTML+='<div class="ide-ai-msg assistant">'+esc(d.content||"No response.")+'</div>';if(applyChanges&&d.written_files?.length){for(const path of d.written_files){if(path===ideActive)await ideOpenFile(path)}await loadIDE()}}catch(e){$("#ideAIMessages").innerHTML+='<div class="ide-ai-msg error">'+esc(e.message)+'</div>'}$("#ideAIMessages").scrollTop=$("#ideAIMessages").scrollHeight}

function ideBottom(kind){
  const b=$("#ideBottomBody");
  if(kind==="problems"){
    const markers=window.monaco?monaco.editor.getModelMarkers({}):[];
    $("#problemCount").textContent=String(markers.length);
    b.innerHTML=markers.length?markers.slice(0,200).map((m,i)=>'<button class="ide-problem-row" data-problem-uri="'+encodeURIComponent(m.resource?.toString()||"")+'" data-problem-line="'+m.startLineNumber+'"><span class="problem-severity">'+(m.severity===8?"ERROR":m.severity===4?"WARN":"INFO")+'</span><span class="problem-message">'+esc(m.message)+'</span><span class="problem-location">'+m.startLineNumber+":"+m.startColumn+'</span></button>').join(""):'<div class="ide-panel-empty">No diagnostics in the workspace.</div>';
    b.onclick=e=>{const row=e.target.closest("[data-problem-line]");if(!row)return;const uri=decodeURIComponent(row.dataset.problemUri);const model=monaco.editor.getModels().find(x=>x.uri.toString()===uri);if(model){const path=uri.startsWith("file:///")?decodeURIComponent(uri.slice(8)):uri;const clean=path.replace(/^[A-Za-z]:[\\/]/,"").replace(/^\/+/,"");if(ideActive&&ideEditor){ideEditor.revealLineInCenter(Number(row.dataset.problemLine));ideEditor.focus()}}};
    return;
  }
  if(kind==="diff"){
    b.innerHTML='<div class="ide-panel-empty">Loading changed files…</div>';
    ideRenderSourcePanel().then(()=>{const rows=[...document.querySelectorAll("#ideSourceResults [data-source-file]")];b.innerHTML=rows.length?rows.map(row=>'<button class="ide-change-row" data-change-file="'+row.dataset.sourceFile+'"><span class="file-dot '+(row.querySelector(".file-dot")?.className.split(" ").pop()||"plaintext")+'"></span><span>'+row.textContent.trim()+'</span><span class="change-mark">M</span></button>').join(""):'<div class="ide-panel-empty">No workspace changes recorded.</div>';b.onclick=e=>{const row=e.target.closest("[data-change-file]");if(row)ideOpenFile(decodeURIComponent(row.dataset.changeFile))}});
    return;
  }
  const lines={terminal:"Integrated workspace terminal.",output:"Fuze output will appear here as workspace operations complete."};
  b.innerHTML='<div class="ide-terminal-line"><span class="terminal-prompt">F</span><span>'+esc(lines[kind]||"Ready.")+"</span></div>";
  if(kind==="terminal"){
    const row=document.createElement("div");row.className="ide-terminal-input-row";
    row.innerHTML='<span class="terminal-prompt">›</span><input id="ideTerminalInput" autocomplete="off" spellcheck="false" placeholder="Type a command and press Enter…">';
    b.appendChild(row);
    const input=$("#ideTerminalInput");
    input.onkeydown=e=>{if(e.key==="Enter"){e.preventDefault();const value=input.value+"\n";input.value="";ideTerminalInput(value)}};
    input.focus();
  }
}
function ideUpdateDebugStatus(text){$("#ideDebugStatus").textContent=text}
function ideDebugRequest(command,args={}){if(!ideDebugSocket||ideDebugSocket.readyState!==WebSocket.OPEN)return Promise.reject(Error("Debugger is not connected"));const seq=ideDebugSeq++;ideDebugSocket.send(JSON.stringify({seq,type:"request",command,arguments:args}));return new Promise((resolve,reject)=>ideDebugPending.set(seq,{resolve,reject}))}
function ideDebugConnect(){
  if(ideDebugSocket?.readyState===WebSocket.OPEN&&ideDebugInitialized)return Promise.resolve();
  if(ideDebugConnectTimer)return ideDebugConnectTimer;
  const proto=location.protocol==="https:"?"wss":"ws";
  const ws=new WebSocket(proto+"://"+location.host+"/v1/debug");
  ideDebugSocket=ws;ideDebugInitialized=false;
  ideUpdateDebugStatus("Connecting to Delve…");
  ideDebugConnectTimer=new Promise((resolve,reject)=>{
    let settled=false,initializedEvent=false;
    const finish=(ok,err)=>{
      if(settled)return;settled=true;clearTimeout(timer);ideDebugConnectTimer=null;
      if(ok)resolve();else reject(err||Error("Debugger connection failed"));
    };
    const timer=setTimeout(()=>{try{ws.close()}catch(_){};finish(false,Error("Debugger connection timed out"))},10000);
    ws.onopen=async()=>{
      try{
        await ideDebugRequest("initialize",{clientID:"fuzecli-studio",clientName:"FuzeCLI Studio",adapterID:"delve",linesStartAt1:true,columnsStartAt1:true,supportsVariableType:true,supportsRunInTerminalRequest:true});
        ideDebugInitialized=true;
        ideUpdateDebugStatus("DAP initialized · waiting for launch");
        finish(true);
      }catch(err){ideUpdateDebugStatus("Debugger initialization failed");finish(false,err)}
    };
    ws.onmessage=e=>{
      try{
        const d=JSON.parse(e.data);
        if(d.type==="response"&&d.request_seq!==undefined&&ideDebugPending.has(d.request_seq)){
          const p=ideDebugPending.get(d.request_seq);ideDebugPending.delete(d.request_seq);
          if(d.success===false)p.reject(Error(d.message||"Debugger request failed"));else p.resolve(d);
          return;
        }
        if(d.type==="event"){
          if(d.event==="initialized"){
            initializedEvent=true;
            if(ideDebugInitialized){ideUpdateDebugStatus("Debugger ready");finish(true)}
          }else if(d.event==="stopped"){
            ideDebugRunning=false;ideUpdateDebugStatus("Paused · "+(d.body?.reason||"breakpoint"));ideDebugStack(d.body?.threadId);
          }else if(d.event==="continued"){
            ideDebugRunning=true;ideUpdateDebugStatus("Running");
          }else if(d.event==="output"){
            ideBottom("output");$("#ideBottomBody").insertAdjacentText("beforeend",d.body?.output||"");$("#ideBottomBody").scrollTop=$("#ideBottomBody").scrollHeight;
          }else if(d.event==="terminated"||d.event==="exited"){
            ideDebugRunning=false;ideUpdateDebugStatus("Program exited");
          }else if(d.event==="thread"){
            ideUpdateDebugStatus("Debugger thread event");
          }
        }
      }catch(err){toast("Debugger message error: "+err.message)}
    };
    ws.onclose=()=>{
      if(ideDebugSocket===ws)ideDebugSocket=null;
      ideDebugInitialized=false;ideDebugConnectTimer=null;
      ideUpdateDebugStatus("Debugger disconnected");
      for(const p of ideDebugPending.values())p.reject(Error("Debugger disconnected"));
      ideDebugPending.clear();
      if(!settled)finish(false,Error("Debugger disconnected"));
    };
    ws.onerror=()=>{ideUpdateDebugStatus("Debugger connection error");if(!settled)finish(false,Error("Debugger connection failed"))};
  });
  return ideDebugConnectTimer;
}
async function ideSendBreakpoints(){if(!ideDebugSocket||ideDebugSocket.readyState!==WebSocket.OPEN||!ideActive)return;const bp=window.__fuzeBreakpoints||new Map();const lines=[];for(const [key,value] of bp){if(key.startsWith(ideActive+":"))lines.push({line:Number(value.line),column:1})}const workspace=String(cfg?.workspace||"").replace(/[\\/]+$/,"");const absoluteSource=workspace?workspace+"/"+ideActive.replace(/^[/\\]+/,""):ideActive;await ideDebugRequest("setBreakpoints",{source:{path:absoluteSource,name:ideActive,sourceReference:0},breakpoints:lines});}
async function ideWaitForInitialized(timeoutMs=10000){if(ideDebugInitialized)return;return new Promise((resolve,reject)=>{const started=Date.now();const poll=()=>{if(ideDebugInitialized){resolve();return}if(Date.now()-started>timeoutMs){reject(Error("Debugger did not send initialized event"))}else setTimeout(poll,50)};poll()})}
async function ideDebugStart(){
  try{
    if(!ideActive)throw Error("Open a Go file before starting the debugger");
    if(!/\.go$/i.test(ideActive))throw Error("Studio debugger targets Go files with Delve");
    const dirty=$(".ide-tab.active")?.classList.contains("dirty");
    if(dirty)await ideSaveFile();
    await ideDebugConnect();
    const workspace=String(cfg?.workspace||"").replace(/[\\/]+$/,"");
    if(!workspace)throw Error("Workspace root is unavailable");
    const program=workspace+"/"+ideActive.replace(/^[/\\]+/,"");
    ideUpdateDebugStatus("Launching · "+ideActive);
    await ideDebugRequest("launch",{mode:"debug",program,cwd:workspace,stopOnEntry:false});
    await ideWaitForInitialized();
    await ideSendBreakpoints();
    try{await ideDebugRequest("setExceptionBreakpoints",{filters:[]})}catch(_){}
    await ideDebugRequest("configurationDone",{});
    ideDebugRunning=true;
    ideUpdateDebugStatus("Running · "+ideActive);
    ideBottom("output");
    $("#ideBottomBody").insertAdjacentText("beforeend","\nStarted "+ideActive+"\n");
  }catch(e){
    ideDebugRunning=false;ideUpdateDebugStatus("Debugger error");ideBottom("output");$("#ideBottomBody").insertAdjacentText("beforeend","\nDebugger error: "+e.message+"\n");toast("Debugger: "+e.message);
  }
}
async function ideDebugCommand(command,args={}){try{if(!ideDebugSocket||ideDebugSocket.readyState!==WebSocket.OPEN)await ideDebugConnect();await ideDebugRequest(command,args)}catch(e){ideUpdateDebugStatus("Debugger unavailable");toast("Debugger: "+e.message)}}
async function ideDebugStack(threadId){try{const d=await ideDebugRequest("stackTrace",{threadId:threadId||1,startFrame:0,levels:50});const frames=d.body?.stackFrames||[];$("#ideDebugFrames").innerHTML=frames.map(f=>'<button class="ide-debug-frame" data-line="'+f.line+'">'+esc(f.name||"frame")+' <span>'+esc(f.source?.path||"")+"</span></button>").join("")||"<span>No stack frames.</span>";$("#ideDebugFrames").onclick=e=>{const b=e.target.closest("[data-line]");if(b&&ideEditor)ideEditor.revealLineInCenter(Number(b.dataset.line))}}catch(e){}}
function ideToggleBreakpoint(line){if(!ideEditor||!ideActive)return;const key=ideActive+":"+line;window.__fuzeBreakpoints??=new Map();const bp=window.__fuzeBreakpoints;if(bp.has(key)){const item=bp.get(key);bp.delete(key);if(item.dec)ideEditor.deltaDecorations(item.dec,[])}else{const dec=ideEditor.deltaDecorations([],[{range:new monaco.Range(line,1,line,1),options:{isWholeLine:false,glyphMarginClassName:"ide-breakpoint"}}]);bp.set(key,{line,dec})}if(ideDebugSocket?.readyState===WebSocket.OPEN)ideSendBreakpoints().catch(()=>{})}
let ideTerminalSocket=null,ideTerminalConnectPromise=null;
function ideTerminalConnect(){if(ideTerminalSocket?.readyState===WebSocket.OPEN)return Promise.resolve();if(ideTerminalConnectPromise)return ideTerminalConnectPromise;ideBottom("terminal");const proto=location.protocol==="https:"?"wss":"ws";const ws=new WebSocket(proto+"://"+location.host+"/v1/terminal");ideTerminalSocket=ws;const box=$("#ideBottomBody");ideTerminalConnectPromise=new Promise((resolve,reject)=>{let settled=false;const timer=setTimeout(()=>{if(!settled){settled=true;try{ws.close()}catch(e){}ideTerminalConnectPromise=null;reject(Error("Terminal connection timed out"))}},8000);ws.onopen=()=>{if(settled)return;settled=true;clearTimeout(timer);ideTerminalConnectPromise=null;box.insertAdjacentHTML("beforeend",'<div class="ide-terminal-line"><span class="terminal-prompt">F</span><span>Terminal connected · '+esc(cfg?.workspace||"workspace")+'</span></div>');$("#ideTerminalInput")?.focus();resolve()};ws.onmessage=e=>{try{const d=JSON.parse(e.data);if(d.type==="output"){box.insertAdjacentText("beforeend",d.data||"");box.scrollTop=box.scrollHeight}else if(d.type==="ready"){box.insertAdjacentHTML("beforeend",'<div class="ide-terminal-line"><span class="terminal-prompt">F</span><span>Shell ready</span></div>')}else if(d.type==="error")toast(d.message||"Terminal error")}catch(err){toast("Terminal message error: "+err.message)}};ws.onerror=()=>{if(!settled){settled=true;clearTimeout(timer);ideTerminalConnectPromise=null;reject(Error("Terminal connection failed"))}toast("Terminal connection failed")};ws.onclose=()=>{if(ideTerminalSocket===ws)ideTerminalSocket=null;ideTerminalConnectPromise=null;const input=$("#ideTerminalInput");if(input)input.disabled=true}});return ideTerminalConnectPromise}
async function ideTerminalInput(value){if(!value)return;try{await ideTerminalConnect();if(ideTerminalSocket?.readyState===WebSocket.OPEN)ideTerminalSocket.send(JSON.stringify({type:"input",data:value}))}catch(e){toast(e.message)}}

async function ideRenderSourcePanel(){
  try{
    const d=await api("/v1/touched");
    const items=d.files||[];
    $("#ideSourceResults").innerHTML=items.length?items.map(path=>'<button class="ide-source-row" data-source-file="'+encodeURIComponent(path)+'"><span class="file-dot '+ideLang(path)+'"></span><span>'+esc(path)+'</span></button>').join(""):'<div class="ide-panel-empty">No changed files yet.</div>';
  }catch(e){$("#ideSourceResults").innerHTML='<div class="ide-panel-empty">Source control unavailable.</div>';toast(e.message)}
}
function ideRenderOutline(){
  const el=$("#ideOutlineResults");
  if(!ideEditor){el.innerHTML='<div class="ide-panel-empty">Open a file to inspect its structure.</div>';return}
  const lang=ideLang(ideActive), lines=ideEditor.getValue().split("\n"), items=[];
  const patterns={
    go:[/^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][\w]*)/,/^\s*type\s+([A-Za-z_][\w]*)\s+struct\b/],
    javascript:[/^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)/,/^\s*(?:export\s+)?class\s+([A-Za-z_$][\w$]*)/,/^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\(/],
    typescript:[/^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)/,/^\s*(?:export\s+)?class\s+([A-Za-z_$][\w$]*)/,/^\s*(?:export\s+)?(?:interface|type)\s+([A-Za-z_$][\w$]*)/],
    python:[/^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)/,/^\s*class\s+([A-Za-z_]\w*)/],
    php:[/^\s*(?:public|private|protected|static|final|abstract|\s)*function\s+([A-Za-z_]\w*)/,/^\s*class\s+([A-Za-z_]\w*)/],
    rust:[/^\s*(?:pub\s+)?fn\s+([A-Za-z_]\w*)/,/^\s*(?:pub\s+)?struct\s+([A-Za-z_]\w*)/,/^\s*(?:pub\s+)?enum\s+([A-Za-z_]\w*)/]
  }[lang]||[];
  const heading=/^\s*#{1,6}\s+(.+)$/;
  lines.forEach((line,i)=>{
    let name="";
    for(const re of patterns){const m=line.match(re);if(m){name=m[1]||"";break}}
    if(!name){const h=line.match(heading);if(h)name=h[1].trim()}
    if(name)items.push({name,line:i+1});
  });
  el.innerHTML=items.slice(0,150).map(x=>'<button class="ide-outline-row" data-outline-line="'+x.line+'"><span>'+String(x.line).padStart(4," ")+'</span><b>'+esc(x.name)+'</b></button>').join("")||'<div class="ide-panel-empty">No symbols detected in this file.</div>';
}
const ideCommands=[
  {label:"Open file",key:"Ctrl+P",run:()=>ideCommandOpen("quick")},
  {label:"Command palette",key:"Ctrl+Shift+P",run:()=>ideCommandOpen("command")},
  {label:"Focus terminal",key:"Ctrl+`",run:()=>{ideBottom("terminal");ideTerminalConnect().catch(()=>{})}},
  {label:"Start debugger",key:"F5",run:()=>ideDebugStart()},
  {label:"Save current file",key:"Ctrl+S",run:()=>ideSaveFile()}
];
function ideCommandOpen(mode="quick"){const overlay=$("#ideCommandOverlay"),input=$("#ideCommandInput"),label=$("#ideCommandLabel");if(!overlay||!input)return;overlay.hidden=false;overlay.dataset.mode=mode;label.textContent=mode==="command"?"COMMAND PALETTE":"QUICK OPEN";input.placeholder=mode==="command"?"Type a command…":"Search workspace files…";input.value="";ideRenderCommandResults();requestAnimationFrame(()=>input.focus())}
function ideCommandClose(){const overlay=$("#ideCommandOverlay");if(overlay)overlay.hidden=true}
function ideRenderCommandResults(){const overlay=$("#ideCommandOverlay"),input=$("#ideCommandInput"),out=$("#ideCommandResults");if(!overlay||!input||!out)return;const q=input.value.trim().toLowerCase(),mode=overlay.dataset.mode||"quick";const items=mode==="command"?ideCommands.filter(x=>!q||x.label.toLowerCase().includes(q)):ideFiles.filter(x=>!q||x.toLowerCase().includes(q)).slice(0,80).map(path=>({label:path,key:"",run:()=>{ideCommandClose();ideOpenFile(path)}}));out.innerHTML=items.map((x,i)=>'<button class="ide-command-row '+(i===0?"active":"")+'" data-command-index="'+i+'"><span>'+esc(x.label)+'</span><kbd>'+esc(x.key||"")+"</kbd></button>").join("")||'<div class="ide-panel-empty">No matches.</div>';out.__items=items}
function ideRunCommandIndex(index){const out=$("#ideCommandResults"),item=out?.__items?.[index];if(item){ideCommandClose();item.run()}}
function initIDEEvents(){
document.querySelectorAll("[data-ide-panel]").forEach(b=>b.onclick=async()=>{
  document.querySelectorAll(".ide-rail-btn").forEach(x=>x.classList.toggle("active",x===b));
  const panel=b.dataset.idePanel;
  const search=panel==="search", source=panel==="source", outline=panel==="outline";
  $("#ideSearchPanel").hidden=!search; $("#ideSourcePanel").hidden=!source; $("#ideOutlinePanel").hidden=!outline;
  $("#ideExplorerTree").hidden=search||source||outline;
  $("#idePanelTitle").textContent=search?"SEARCH":source?"SOURCE CONTROL":outline?"OUTLINE":"EXPLORER";
  if(source)await ideRenderSourcePanel();
  if(outline)ideRenderOutline();
});
document.addEventListener("keydown",e=>{
  const mod=e.ctrlKey||e.metaKey;
  if(e.key==="Escape"&&$("#ideCommandOverlay")&&!$("#ideCommandOverlay").hidden){e.preventDefault();ideCommandClose();return}
  if(mod&&e.shiftKey&&e.key.toLowerCase()==="p"){e.preventDefault();ideCommandOpen("command");return}
  if(mod&&!e.shiftKey&&e.key.toLowerCase()==="p"){e.preventDefault();ideCommandOpen("quick");return}
  if(e.key==="F5"&&document.querySelector("#view-ide.active")){e.preventDefault();ideDebugStart();return}
  if(mod&&e.key.toLowerCase()==="s"&&document.querySelector("#view-ide.active")){e.preventDefault();ideSaveFile();return}
  if(mod&&e.key==="`"&&document.querySelector("#view-ide.active")){e.preventDefault();ideBottom("terminal");ideTerminalConnect().catch(()=>{});return}
});
$("#ideCommandInput")?.addEventListener("input",ideRenderCommandResults);
$("#ideCommandInput")?.addEventListener("keydown",e=>{const rows=[...document.querySelectorAll(".ide-command-row")],active=Math.max(0,rows.findIndex(x=>x.classList.contains("active")));if(e.key==="ArrowDown"){e.preventDefault();rows.forEach(x=>x.classList.remove("active"));rows[Math.min(rows.length-1,active+1)]?.classList.add("active")}else if(e.key==="ArrowUp"){e.preventDefault();rows.forEach(x=>x.classList.remove("active"));rows[Math.max(0,active-1)]?.classList.add("active")}else if(e.key==="Enter"){e.preventDefault();ideRunCommandIndex(Math.max(0,rows.findIndex(x=>x.classList.contains("active"))))}});
$("#ideCommandResults")?.addEventListener("click",e=>{const row=e.target.closest("[data-command-index]");if(row)ideRunCommandIndex(Number(row.dataset.commandIndex))});
$("#ideRefresh").onclick=loadIDE;
$("#ideCollapse").onclick=()=>$(".ide-explorer").classList.toggle("collapsed");
$("#ideExplorerTree").onclick=e=>{const f=e.target.closest("[data-ide-file]");if(f)ideOpenFile(decodeURIComponent(f.dataset.ideFile));const folder=e.target.closest("[data-folder]");if(folder){folder.classList.toggle("open");folder.nextElementSibling?.classList.toggle("open")}};
$("#ideTabs").onclick=e=>{const c=e.target.closest("[data-ide-close]");if(c){const p=decodeURIComponent(c.dataset.ideClose);if(ideActive===p)ideLSPClose(ideActive);ideOpen=ideOpen.filter(x=>x!==p);ideActive=ideOpen.at(-1)||"";renderIdeTabs();if(ideActive)ideOpenFile(ideActive);else $("#ideEditor").innerHTML='<div class="ide-editor-empty"><div class="ide-logo">F</div><h3>FuzeCLI Studio</h3><p>Open a file to start editing.</p></div>';ideRenderOutline();return}const t=e.target.closest("[data-ide-tab]");if(t)ideOpenFile(decodeURIComponent(t.dataset.ideTab))};
$("#ideSearch").oninput=()=>{const q=$("#ideSearch").value.toLowerCase();$("#ideSearchResults").innerHTML=ideFiles.filter(f=>f.toLowerCase().includes(q)).slice(0,80).map(f=>'<button class="ide-search-row" data-ide-file="'+encodeURIComponent(f)+'">'+esc(f)+"</button>").join("")};
$("#ideSearchPanel").onclick=e=>{const f=e.target.closest("[data-ide-file]");if(f)ideOpenFile(decodeURIComponent(f.dataset.ideFile))};
$("#ideSourceResults").onclick=e=>{const f=e.target.closest("[data-source-file]");if(f)ideOpenFile(decodeURIComponent(f.dataset.sourceFile))};
$("#ideOutlineResults").onclick=e=>{const b=e.target.closest("[data-outline-line]");if(b&&ideEditor){ideEditor.revealLineInCenter(Number(b.dataset.outlineLine));ideEditor.setPosition({lineNumber:Number(b.dataset.outlineLine),column:1});ideEditor.focus()}};
document.querySelectorAll("[data-bottom]").forEach(b=>b.onclick=()=>{document.querySelectorAll("[data-bottom]").forEach(x=>x.classList.toggle("active",x===b));ideBottom(b.dataset.bottom);if(b.dataset.bottom==="terminal")ideTerminalConnect()});
document.querySelectorAll("[data-ide-action]").forEach(b=>b.onclick=()=>askIDE({explain:"Explain this file and point out risky code.",fix:"Find correctness bugs and propose a minimal safe fix.",refactor:"Refactor this file for clarity without changing behavior.",tests:"Design focused tests for this file."}[b.dataset.ideAction]));
$("#ideAsk").onclick=()=>askIDE();$("#idePrompt").onkeydown=e=>{if(e.key==="Enter"&&!e.shiftKey){e.preventDefault();askIDE()}};
$("#ideDebugStart").onclick=ideDebugStart;
$("#ideDebugStop").onclick=async()=>{try{if(ideDebugSocket?.readyState===WebSocket.OPEN)await ideDebugRequest("disconnect",{restart:false,terminateDebuggee:true})}catch(e){}ideDebugRunning=false;ideUpdateDebugStatus("Debugger stopped")};
$("#ideDebugContinue").onclick=()=>ideDebugCommand("continue");$("#ideDebugPause").onclick=()=>ideDebugCommand("pause");$("#ideDebugStepOver").onclick=()=>ideDebugCommand("next");$("#ideDebugStepInto").onclick=()=>ideDebugCommand("stepIn");$("#ideDebugStepOut").onclick=()=>ideDebugCommand("stepOut");
}
window.addEventListener("error",e=>{const message=e.error?.message||e.message||"Unexpected browser error";toast("Studio error: "+message);});
window.addEventListener("unhandledrejection",e=>{const message=e.reason?.message||String(e.reason||"Unhandled promise rejection");toast("Studio error: "+message);});
