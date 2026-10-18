const $ = (s) => document.querySelector(s);
let data = {jobs: [], repositories: [], workflows: [], schedules: [], runs: [], audit: []};
let runFilter = 'all';
let autoRefresh = true;
let runSnapshotReady = false;
const openLogs = new Set();
const triggerDrafts = new Map();
let savedTheme = 'dark';
try { savedTheme = localStorage.getItem('ausra-theme') === 'light' ? 'light' : 'dark'; } catch (_) {}
document.documentElement.dataset.theme = savedTheme;
let savedDensity = 'spacious';
try { savedDensity = localStorage.getItem('ausra-density') === 'compact' ? 'compact' : 'spacious'; } catch (_) {}
document.documentElement.dataset.density = savedDensity;
function syncDensityButton() {
  const compact = document.documentElement.dataset.density === 'compact';
  const button = $('#density-toggle');
  button.setAttribute('aria-label', compact ? 'Switch to spacious density' : 'Switch to compact density');
  button.setAttribute('aria-pressed', String(compact));
  button.innerHTML = compact ? '▤ <span>Spacious</span>' : '▦ <span>Compact</span>';
}
syncDensityButton();
$('#density-toggle').addEventListener('click', () => {
  const next = document.documentElement.dataset.density === 'compact' ? 'spacious' : 'compact';
  document.documentElement.dataset.density = next;
  try { localStorage.setItem('ausra-density', next); } catch (_) {}
  syncDensityButton();
});
function syncThemeButton() {
  const light = document.documentElement.dataset.theme === 'light';
  const button = $('#theme-toggle');
  button.setAttribute('aria-label', light ? 'Switch to dark theme' : 'Switch to light theme');
  button.innerHTML = light ? '☾ <span>Dark</span>' : '☼ <span>Light</span>';
}
syncThemeButton();
$('#theme-toggle').addEventListener('click', () => {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  try { localStorage.setItem('ausra-theme', next); } catch (_) {}
  syncThemeButton();
});
const escapeHTML = (s) => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const statusDisplay = {
  succeeded: {label:'Succeeded', icon:'✓'},
  failed: {label:'Failed', icon:'!'},
  canceled: {label:'Canceled', icon:'×'},
  interrupted: {label:'Interrupted', icon:'■'},
  running: {label:'Running', icon:'▶'},
  queued: {label:'Queued', icon:'◷'},
  waiting_approval: {label:'Waiting approval', icon:'◆'},
  enabled: {label:'Enabled', icon:'✓'},
  disabled: {label:'Disabled', icon:'○'},
  synced: {label:'Synced', icon:'✓'},
  sync_failed: {label:'Sync failed', icon:'!'},
  unsynced: {label:'Not synced', icon:'◷'}
};
function statusBadge(status) {
  const display = statusDisplay[status] || {label:String(status).replaceAll('_', ' '), icon:'•'};
  return `<span class="badge ${escapeHTML(status)}"><span class="badge-icon" aria-hidden="true">${display.icon}</span>${escapeHTML(display.label)}</span>`;
}
const stamp = (s) => s ? new Date(s).toLocaleString() : '—';
function toast(message, error=false) { const el=$('#toast'); el.textContent=message; el.className=error?'error':''; el.style.display='block'; clearTimeout(toast.timer); toast.timer=setTimeout(()=>el.style.display='none',4500); }
async function api(path, options={}) { const response=await fetch('/api'+path,{headers:{'Content-Type':'application/json'},...options}); const value=await response.json(); if(!response.ok) throw new Error(value.error||'Request failed'); return value; }
function announceRunChanges(before, after) {
  if (!runSnapshotReady) { runSnapshotReady = true; return; }
  const previous = new Map(before.map(run => [run.id, run]));
  const changes = after.flatMap(run => {
    const old = previous.get(run.id);
    if (!old) return [`${run.targetName} started; ${statusDisplay[run.status]?.label || run.status}`];
    if (old.status !== run.status) return [`${run.targetName}: ${statusDisplay[run.status]?.label || run.status}`];
    if (old.step !== run.step && run.step) return [`${run.targetName}: step ${run.step}`];
    return [];
  });
  if (changes.length) $('#run-announcements').textContent = changes.length <= 2 ? changes.join('. ') : `${changes.length} run updates. Open Runs to review.`;
}
async function load() { const keys=['jobs','repositories','workflows','schedules','runs','audit']; const previousRuns=data.runs; const results=await Promise.all(keys.map(k=>api('/'+k))); keys.forEach((k,i)=>data[k]=results[i]); announceRunChanges(previousRuns,data.runs); render(); }
function showApp(visible) { $('#login-panel').hidden=visible; $('#app').hidden=!visible; $('#logout').hidden=!visible; }
function webhookTriggerOptions(repoId) {
  const jobs=data.jobs.filter(j=>j.repositoryId===repoId).map(j=>({value:'job:'+j.id,label:'Job: '+j.name}));
  const workflows=data.workflows.filter(w=>{const members=w.jobIds.map(id=>data.jobs.find(j=>j.id===id));return members.every(Boolean)&&members.some(j=>j.repositoryId===repoId)&&members.every(j=>!j.repositoryId||j.repositoryId===repoId);}).map(w=>({value:'workflow:'+w.id,label:'Workflow: '+w.name}));
  return [...jobs,...workflows];
}
function render() {
  ['jobs','workflows','schedules','runs'].forEach(k=>$('#'+k.slice(0,-1)+'-count').textContent=data[k].length);
  $('#active-count').textContent = data.runs.filter(r => r.status === 'running' || r.status === 'queued').length;
  $('#attention-count').textContent = data.runs.filter(r => r.status === 'waiting_approval').length;
  $('#last-sync').textContent = 'Updated ' + new Date().toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'});
  $('#jobs-list').innerHTML=data.jobs.length ? data.jobs.map(j=>{const repo=data.repositories.find(r=>r.id===j.repositoryId);return `<div class="item"><div class="item-head"><div><h3>${escapeHTML(j.name)}</h3><div class="meta">${escapeHTML(j.project)} · ${escapeHTML(j.type)} · ${repo?'Git: '+escapeHTML(repo.name):'Workspace'} / ${escapeHTML(j.dir||'.')}</div></div><span class="badge">${escapeHTML(j.type)}</span></div><div class="item-actions"><button data-action="run-job" data-id="${j.id}">Run now</button><button class="ghost danger" data-action="delete-job" data-id="${j.id}">Delete</button><span class="code">${j.id}</span></div></div>`}).join('') : '<div class="empty">No jobs yet. Create your first job on the right.</div>';
  $('#repositories-list').innerHTML=data.repositories.length ? data.repositories.map(r=>{
    const githubRepo=new URL(r.url).hostname.toLowerCase()==='github.com';
    const triggerValue=triggerDrafts.has(r.id)?triggerDrafts.get(r.id):r.webhookTargetType+':'+r.webhookTargetId;
    const options=webhookTriggerOptions(r.id);
    return `<div class="item"><div class="item-head"><div><h3>${escapeHTML(r.name)}</h3><div class="meta">${escapeHTML(r.project)} · ${escapeHTML(r.url)} · ${escapeHTML(r.ref||'default branch')}</div></div>${statusBadge(r.lastError?'sync_failed':r.revision?'synced':'unsynced')}</div><div class="repo-detail">${r.revision?`Commit <span class="code" title="${escapeHTML(r.revision)}">${escapeHTML(r.revision.slice(0,12))}</span> · synced ${stamp(r.syncedAt)}`:'Not synced yet'}${r.lastError?`<div class="repo-error">${escapeHTML(r.lastError)}</div>`:''}<div>GitHub webhook: ${r.webhookEnabled?'enabled':'off'}${r.webhookLastDeliveryAt?' · last delivery '+stamp(r.webhookLastDeliveryAt):''}${r.webhookLastStatus?' · '+escapeHTML(r.webhookLastStatus):''}</div>${r.webhookLastRunId?`<div>Last triggered run: <span class="code">${escapeHTML(r.webhookLastRunId)}</span></div>`:''}${r.webhookLastError?`<div class="repo-error">${escapeHTML(r.webhookLastError)}</div>`:''}</div><div class="item-actions"><button data-action="sync-repository" data-id="${r.id}">Sync now</button>${githubRepo?`<button class="ghost" data-action="setup-webhook" data-id="${r.id}">${r.webhookEnabled?'Rotate webhook secret':'Set up webhook'}</button>${r.webhookEnabled?`<button class="ghost" data-action="disable-webhook" data-id="${r.id}">Disable webhook</button>`:''}`:''}<button class="ghost danger" data-action="delete-repository" data-id="${r.id}">Delete</button></div>${r.webhookEnabled?`<div class="trigger-controls"><label>On matching push<select data-trigger-repo="${r.id}"><option value="">Sync only</option>${options.map(o=>`<option value="${escapeHTML(o.value)}" ${o.value===triggerValue?'selected':''}>${escapeHTML(o.label)}</option>`).join('')}</select></label><button class="ghost" data-action="save-trigger" data-id="${r.id}">Save trigger</button>${r.webhookTargetId?`<button class="ghost" data-action="test-trigger" data-id="${r.id}">Create test run</button>`:''}<p class="form-note">Triggered jobs and workflows wait for admin approval. Runs use the pushed commit. A test run checks the mapping but does not test GitHub delivery.</p></div>`:''}</div>`;
  }).join('') : '<div class="empty">No repositories yet. Connect a public HTTPS Git repository on the right.</div>';
  for(const r of data.repositories.filter(r=>r.webhookEnabled)) { const controls=document.querySelector(`[data-trigger-repo="${r.id}"]`)?.closest('.trigger-controls'); if(controls) { const button=document.createElement('button'); button.className='ghost'; button.textContent='View deliveries'; button.dataset.action='view-deliveries'; button.dataset.id=r.id; controls.insertBefore(button,controls.querySelector('p')); } }
  const selectedRepo=$('#job-repository').value;
  $('#job-repository').innerHTML='<option value="">Mounted workspace</option>'+data.repositories.map(r=>`<option value="${r.id}" ${r.revision?'':'disabled'}>${escapeHTML(r.name)}${r.revision?'':' (sync required)'}</option>`).join('');
  if(data.repositories.some(r=>r.id===selectedRepo && r.revision)) $('#job-repository').value=selectedRepo;
  $('#workflows-list').innerHTML=data.workflows.length ? data.workflows.map(w=>`<div class="item"><div class="item-head"><div><h3>${escapeHTML(w.name)}</h3><div class="meta">${escapeHTML(w.project)} · ${w.jobIds.length} steps · ${w.requireApproval?'Approval required':'Runs immediately'}</div></div><span class="badge">Workflow</span></div><div class="item-actions"><button data-action="run-workflow" data-id="${w.id}">Run now</button><button class="ghost danger" data-action="delete-workflow" data-id="${w.id}">Delete</button><span class="code">${w.id}</span></div></div>`).join('') : '<div class="empty">No workflows yet. Chain some jobs together.</div>';
  $('#schedules-list').innerHTML=data.schedules.length ? data.schedules.map(s=>`<div class="item"><div class="item-head"><div><h3>${escapeHTML(s.name)}</h3><div class="meta">${escapeHTML(s.targetType)} · every ${s.everyMinutes} min · next ${stamp(s.nextAt)}</div></div>${statusBadge(s.enabled ? 'enabled' : 'disabled')}</div><div class="item-actions"><button class="ghost danger" data-action="delete-schedule" data-id="${s.id}">Delete</button></div></div>`).join('') : '<div class="empty">No schedules yet.</div>';
  $('#schedule-target').innerHTML=[...data.jobs.map(j=>({key:'job:'+j.id,label:'Job: '+j.name})),...data.workflows.map(w=>({key:'workflow:'+w.id,label:'Workflow: '+w.name}))].map(x=>`<option value="${x.key}">${escapeHTML(x.label)}</option>`).join('');
  const visibleRuns = data.runs.filter(r => runFilter === 'all' || (runFilter === 'active' && ['queued','running'].includes(r.status)) || (runFilter === 'approval' && r.status === 'waiting_approval'));
  $('#runs-filter').hidden = runFilter === 'all';
  $('#runs-filter-label').textContent = runFilter === 'active' ? 'Showing active runs' : 'Showing runs awaiting approval';
  $('#runs-list').innerHTML=visibleRuns.length ? visibleRuns.map(r=>`<div class="item"><div class="item-head"><div><h3>${escapeHTML(r.targetName)}</h3><div class="meta">${escapeHTML(r.targetType)} · ${escapeHTML(r.triggeredBy)} · ${stamp(r.startedAt)}${r.step?' · step '+r.step:''}</div>${(r.sources||[]).map(s=>`<div class="meta">${escapeHTML(data.repositories.find(repo=>repo.id===s.repositoryId)?.name||s.repositoryId)} @ <span class="code" title="${escapeHTML(s.commit)}">${escapeHTML(s.commit.slice(0,12)||'unsynced')}</span></div>`).join('')}</div>${statusBadge(r.status)}</div><div class="item-actions">${r.status==='waiting_approval'?`<button data-action="approve" data-id="${r.id}">Approve</button>`:''}${['queued','running','waiting_approval'].includes(r.status)?`<button class="ghost danger" data-action="cancel" data-id="${r.id}">Cancel</button>`:''}<button class="ghost" data-action="toggle-log" data-id="${r.id}" aria-controls="log-${r.id}" aria-expanded="${openLogs.has(r.id)}">${openLogs.has(r.id)?'Hide log':'View log'}</button></div><pre id="log-${r.id}" ${openLogs.has(r.id)?'':'hidden'}>${escapeHTML(r.log||'No output yet.')}</pre></div>`).join('') : `<div class="empty">${runFilter === 'all' ? 'No runs yet.' : 'No matching runs.'}</div>`;
  $('#audit-list').innerHTML=data.audit.length ? data.audit.slice(0,100).map(a=>`<div class="item"><strong>${escapeHTML(a.action)}</strong><div class="meta">${stamp(a.at)} · ${escapeHTML(a.detail)}</div></div>`).join('') : '<div class="empty">No activity yet.</div>';
}
function selectTab(name) {
  document.querySelectorAll('[data-tab]').forEach(x=>x.classList.toggle('active',x.dataset.tab===name));
  document.querySelectorAll('.tab-panel').forEach(x=>x.hidden=x.id!==name);
  document.querySelector('#'+name).scrollIntoView({block:'start',behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches?'auto':'smooth'});
}
document.addEventListener('click', async e => { const overview=e.target.closest('[data-overview-target]'); if(overview) { runFilter=overview.dataset.runFilter||'all'; selectTab(overview.dataset.overviewTarget); render(); return; }
  const tab=e.target.closest('[data-tab]'); if(tab) { if(tab.dataset.tab==='runs') {runFilter='all';render();} selectTab(tab.dataset.tab); return; }
  const copy=e.target.closest('[data-copy]'); if(copy) { try { await navigator.clipboard.writeText($('#'+copy.dataset.copy).value); toast('Copied'); } catch (_) { toast('Copy failed; select the field to copy',true); } return; }
  const button=e.target.closest('[data-action]'); if(!button) return; const {action,id}=button.dataset;
  if(action==='toggle-log') { const log=$('#log-'+id); log.hidden=!log.hidden; if(log.hidden) openLogs.delete(id); else openLogs.add(id); button.setAttribute('aria-expanded',String(!log.hidden)); button.textContent=log.hidden?'View log':'Hide log'; return; }
  if(action==='save-trigger') { const selected=document.querySelector(`[data-trigger-repo="${id}"]`).value; try { button.disabled=true; const [targetType,targetId]=selected.split(':'); await api(`/repositories/${id}/trigger`,selected?{method:'PUT',body:JSON.stringify({targetType,targetId})}:{method:'DELETE'}); triggerDrafts.delete(id); await load(); toast(selected?'Push trigger saved':'Push trigger removed'); } catch(err) { toast(err.message,true); } finally { button.disabled=false; } return; }
  if(action==='test-trigger') { try { button.disabled=true; await api(`/repositories/${id}/trigger/test`,{method:'POST'}); await load(); selectTab('runs'); toast('Test run waiting for approval'); } catch(err) { toast(err.message,true); } finally { button.disabled=false; } return; }
  if(action==='view-deliveries') { try { button.disabled=true; const deliveries=await api(`/repositories/${id}/deliveries`); const repo=data.repositories.find(r=>r.id===id); $('#webhook-deliveries-title').textContent=`Recent deliveries · ${repo?.name||'Repository'}`; $('#webhook-deliveries-list').innerHTML=deliveries.length?deliveries.map(d=>`<div class="item"><div class="item-head"><div><strong>${escapeHTML(d.event||'event')}</strong><div class="meta">${stamp(d.receivedAt)} · ${escapeHTML(d.ref||'—')}</div></div>${statusBadge(d.status)}</div><div class="repo-detail">Delivery <span class="code">${escapeHTML(d.id)}</span>${d.after?` · commit <span class="code">${escapeHTML(d.after.slice(0,12))}</span>`:''}${d.runId?` · run <span class="code">${escapeHTML(d.runId)}</span>`:''}</div></div>`).join(''):'<div class="empty">No deliveries yet. GitHub sends a ping when the webhook is created.</div>'; $('#webhook-deliveries').hidden=false; $('#webhook-deliveries').scrollIntoView({block:'nearest'}); } catch(err) { toast(err.message,true); } finally { button.disabled=false; } return; }
  if(action.startsWith('delete-') && !confirm('Delete this '+action.slice(7)+'?')) return;
  const paths={ 'run-job':['POST',`/jobs/${id}/run`], 'delete-job':['DELETE',`/jobs/${id}`], 'sync-repository':['POST',`/repositories/${id}/sync`], 'delete-repository':['DELETE',`/repositories/${id}`], 'setup-webhook':['POST',`/repositories/${id}/webhook`], 'disable-webhook':['DELETE',`/repositories/${id}/webhook`], 'run-workflow':['POST',`/workflows/${id}/run`], 'delete-workflow':['DELETE',`/workflows/${id}`], 'delete-schedule':['DELETE',`/schedules/${id}`], approve:['POST',`/runs/${id}/approve`], cancel:['POST',`/runs/${id}/cancel`]};
  try { button.disabled=true; const result=await api(paths[action][1],{method:paths[action][0]}); if(action==='setup-webhook') { $('#webhook-url').value=window.location.origin+result.path; $('#webhook-secret').value=result.secret; $('#webhook-setup').hidden=false; $('#webhook-setup').scrollIntoView({block:'nearest'}); } if(action==='disable-webhook'||action==='delete-repository') { hideWebhookSetup(); triggerDrafts.delete(id); } await load(); toast(action==='sync-repository'?'Repository synced':action==='setup-webhook'?'Webhook secret ready':action.startsWith('run-')?'Run started':'Updated'); } catch(err) { if(action==='sync-repository') await load().catch(()=>{}); toast(err.message,true); } finally { button.disabled=false; }
});
document.addEventListener('change',e=>{ if(e.target.matches('[data-trigger-repo]')) triggerDrafts.set(e.target.dataset.triggerRepo,e.target.value); });
function hideWebhookSetup() { $('#webhook-url').value=''; $('#webhook-secret').value=''; $('#webhook-setup').hidden=true; }
$('#webhook-setup-close').addEventListener('click',hideWebhookSetup);
$('#webhook-deliveries-close').addEventListener('click',()=>$('#webhook-deliveries').hidden=true);
$('#job-type').addEventListener('change',e=>{ const shell=e.target.value==='shell'; $('#args-label').hidden=shell; $('#command-label').hidden=!shell; });
$('#job-repository').addEventListener('change',e=>{ $('#job-dir-label').firstChild.textContent=e.target.value?'Repository directory':'Workspace directory'; });
$('#login-form').addEventListener('submit',async e=>{e.preventDefault(); try { await api('/login',{method:'POST',body:JSON.stringify({password:new FormData(e.target).get('password')})}); e.target.reset(); showApp(true); const me=await api('/me'); $('#workspace').textContent=me.workspace; await load(); } catch(err){ toast(err.message,true); }});
$('#logout').addEventListener('click',async()=>{await api('/logout',{method:'POST'}); hideWebhookSetup(); $('#webhook-deliveries').hidden=true; triggerDrafts.clear(); runSnapshotReady=false; data.runs=[]; openLogs.clear(); $('#run-announcements').textContent=''; showApp(false);});
$('#job-form').addEventListener('submit',async e=>{e.preventDefault(); const f=new FormData(e.target); const payload={name:f.get('name'),project:f.get('project'),repositoryId:f.get('repositoryId'),type:f.get('type'),dir:f.get('dir'),timeoutSec:Number(f.get('timeoutSec')),command:f.get('command'),args:String(f.get('args')||'').split('\n').map(s=>s.trim()).filter(Boolean)}; try { await api('/jobs',{method:'POST',body:JSON.stringify(payload)}); e.target.reset(); $('#job-type').dispatchEvent(new Event('change')); $('#job-repository').dispatchEvent(new Event('change')); await load(); toast('Job created'); } catch(err){toast(err.message,true);} });
$('#repository-form').addEventListener('submit',async e=>{e.preventDefault(); const f=new FormData(e.target); try { const repo=await api('/repositories',{method:'POST',body:JSON.stringify({name:f.get('name'),project:f.get('project'),url:f.get('url'),ref:f.get('ref')})}); e.target.reset(); await load(); toast(repo.lastError?`Repository saved; sync failed: ${repo.lastError}`:'Repository connected and synced',Boolean(repo.lastError)); } catch(err){toast(err.message,true);} });
$('#workflow-form').addEventListener('submit',async e=>{e.preventDefault(); const f=new FormData(e.target); const payload={name:f.get('name'),project:f.get('project'),jobIds:String(f.get('jobIds')).split('\n').map(s=>s.trim()).filter(Boolean),requireApproval:f.has('requireApproval')}; try {await api('/workflows',{method:'POST',body:JSON.stringify(payload)});e.target.reset();await load();toast('Workflow created');}catch(err){toast(err.message,true);} });
$('#schedule-form').addEventListener('submit',async e=>{e.preventDefault(); const f=new FormData(e.target); const [targetType,targetId]=String(f.get('target')).split(':'); try {await api('/schedules',{method:'POST',body:JSON.stringify({name:f.get('name'),targetType,targetId,everyMinutes:Number(f.get('everyMinutes'))})});e.target.reset();await load();toast('Schedule created');}catch(err){toast(err.message,true);} });
$('#refresh-runs').addEventListener('click',()=>load().catch(err=>toast(err.message,true)));
$('#toggle-auto-refresh').addEventListener('click',()=>{autoRefresh=!autoRefresh;$('#toggle-auto-refresh').textContent=autoRefresh?'Pause live updates':'Resume live updates';$('#toggle-auto-refresh').setAttribute('aria-pressed',String(!autoRefresh));$('#refresh-state').textContent=autoRefresh?'Live updates every 10 seconds.':'Live updates paused.';});
$('#clear-runs-filter').addEventListener('click',()=>{runFilter='all';render();});
api('/me').then(me=>{$('#workspace').textContent=me.workspace;showApp(true);return load();}).catch(()=>showApp(false));
setInterval(()=>{if(!$('#app').hidden && autoRefresh) load().catch(()=>{});},10000);
