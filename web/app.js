const $ = (s) => document.querySelector(s);
let data = {jobs: [], repositories: [], workflows: [], schedules: [], runs: [], audit: []};
let runFilter = 'all';
let repositoryFilter = 'all';
let mapSelection = null;
let selectedProject = '';
const overviewSections = ['signals', 'map', 'activity', 'setup'];
let visibleSections = new Set(overviewSections);
try { const saved = JSON.parse(localStorage.getItem('ausra-overview-sections')); if (Array.isArray(saved)) visibleSections = new Set(saved.filter(x => overviewSections.includes(x))); } catch (_) {}
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
  processing: {label:'Processing', icon:'◷'},
  ignored: {label:'Ignored', icon:'○'},
  superseded: {label:'Superseded', icon:'◆'},
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
const auditActor = action => /^(login|(?:job|workflow|schedule)\.(?:created|deleted)|repository\.(?:created|deleted|webhook_enabled|webhook_disabled|webhook_trigger_changed)|run\.(?:approved|canceled))$/.test(action) ? 'Admin (inferred)' : 'Not recorded';
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
function renderOverview() {
  overviewSections.forEach(name => {
    document.querySelector(`[data-overview-panel="${name}"]`).hidden = !visibleSections.has(name);
    document.querySelector(`[data-overview-section="${name}"]`).checked = visibleSections.has(name);
  });
  const active = data.runs.filter(r => ['queued', 'running'].includes(r.status));
  const approvals = data.runs.filter(r => r.status === 'waiting_approval');
  const unhealthy = data.repositories.filter(r => r.lastError || r.webhookLastError);
  const next = data.schedules.filter(s => s.enabled && s.nextAt).sort((a,b) => Date.parse(a.nextAt) - Date.parse(b.nextAt))[0];
  const signal = (label, value, note, target, extra = '') => `<button class="signal card" type="button" data-overview-target="${target}" ${extra}><span class="signal-label">${label} <span aria-hidden="true">↗</span></span><strong>${value}</strong><small>${note}</small></button>`;
  $('#signal-grid').innerHTML = signal('Active runs', active.length, 'Queued or running', 'runs', 'data-run-filter="active"') + signal('Pending approvals', approvals.length, 'Runs awaiting a decision', 'runs', 'data-run-filter="approval"') + signal('Unhealthy Git sources', unhealthy.length, data.repositories.length ? 'Sync or webhook errors' : 'No Git sources connected', 'repositories', 'data-repository-filter="unhealthy"') + (next ? signal('Next scheduled action', escapeHTML(next.name), escapeHTML(stamp(next.nextAt)), 'schedules', `data-focus-id="${escapeHTML(next.id)}"`) : signal('Next scheduled action', 'None', 'Create a schedule', 'schedules')) + '<div class="signal card unavailable"><span class="signal-label">Drift findings</span><strong>Unavailable</strong><small>Drift detection is not configured in Aušra yet.</small></div><div class="signal card unavailable"><span class="signal-label">Policy failures</span><strong>Unavailable</strong><small>Policy evaluation is not configured in Aušra yet.</small></div>';
  const projectOf = x => x.project || 'Workspace';
  const projects = [...new Set([...data.repositories, ...data.jobs, ...data.workflows].map(projectOf))].sort((a,b) => a.localeCompare(b));
  if (!projects.includes(selectedProject)) selectedProject = projects[0] || '';
  $('#project-picker').innerHTML = projects.length ? projects.map(p => `<option value="${escapeHTML(p)}">${escapeHTML(p)}</option>`).join('') : '<option value="">No projects yet</option>';
  $('#project-picker').value = selectedProject;
  const repos = data.repositories.filter(x => projectOf(x) === selectedProject);
  const jobs = data.jobs.filter(x => projectOf(x) === selectedProject);
  const workflows = data.workflows.filter(x => projectOf(x) === selectedProject);
  const nodes = (title, kind, items, detail) => `<div class="map-column"><h4>${title} <span>${items.length}</span></h4>${items.length ? items.map(x => `<button class="map-node ${mapSelection?.kind === kind && mapSelection.id === x.id ? 'selected' : ''}" type="button" data-map-kind="${kind}" data-map-id="${escapeHTML(x.id)}"><strong>${escapeHTML(x.name)}</strong><small>${escapeHTML(detail(x))}</small></button>`).join('') : '<p class="map-empty">None yet</p>'}</div>`;
  $('#project-map').innerHTML = projects.length ? `<div class="map-columns">${nodes('Repositories', 'repository', repos, x => x.lastError ? 'Sync needs attention' : x.revision ? 'Synced' : 'Awaiting sync')}${nodes('Jobs', 'job', jobs, x => data.repositories.find(r => r.id === x.repositoryId)?.name || 'Mounted workspace')}${nodes('Workflows', 'workflow', workflows, x => x.jobIds.map(id => data.jobs.find(j => j.id === id)?.name || 'Missing job').join(' → '))}</div><p class="map-coverage">Credentials, cloud accounts, and EKS clusters are not modeled yet. They will appear here when connection support is added.</p>` : '<div class="empty">No project map yet. Connect a source or create a job to start.</div>';
  renderMapDetail();
  const events = [
    ...data.runs.map(r => ({at:r.startedAt, actor:r.triggeredBy || 'Not recorded', action:`${r.targetType} run · ${r.targetName}`, environment:'Unassigned', outcome:statusDisplay[r.status]?.label || r.status, target:'runs', id:r.id})),
    ...data.audit.map((a, index) => ({at:a.at, actor:auditActor(a.action), action:a.action.replaceAll('.', ' '), environment:'Unassigned', outcome:a.action.includes('failed') ? 'Failed' : 'Recorded', target:'audit', index, detail:a.detail}))
  ].sort((a,b) => Date.parse(b.at) - Date.parse(a.at)).slice(0, 8);
  $('#activity-list').innerHTML = events.length ? events.map(e => `<div class="activity-row"><div><strong>${escapeHTML(e.action)}</strong><div class="meta">${escapeHTML(stamp(e.at))} · Actor: ${escapeHTML(e.actor)} · Environment: ${escapeHTML(e.environment)}</div>${e.detail ? `<div class="meta">${escapeHTML(e.detail)}</div>` : ''}</div><div class="activity-end"><span class="badge">${escapeHTML(e.outcome)}</span><button class="text-button" type="button" data-overview-target="${e.target}" ${e.id ? `data-run-id="${escapeHTML(e.id)}"` : `data-audit-index="${e.index}"`}>View ${e.target === 'runs' ? 'run' : 'audit'} ↗</button></div></div>`).join('') : '<div class="empty">No activity yet. Create and run a job to see events here.</div>';
  const steps = [
    ['Connect source', data.repositories.length > 0 || data.jobs.some(j => !j.repositoryId), 'repositories', 'Connect a public HTTPS Git repository, or use the mounted workspace.', 'repositories'],
    ['Add credentials', false, '', 'Managed credentials are not available yet. Public Git and mounted workspace jobs work without them.', ''],
    ['Create job', data.jobs.length > 0, 'jobs', 'Choose a source and define an executable job.', 'jobs'],
    ['Test run', data.runs.length > 0, 'jobs', 'Use Run now on a job and inspect its output.', 'jobs'],
    ['Schedule', data.schedules.length > 0, 'schedules', 'Choose a job or workflow and set an interval.', 'schedules']
  ];
  $('#setup-path').innerHTML = steps.map(([title, done, target, note, destination], i) => `<div class="setup-step card"><span class="step-number">${done ? '✓' : i + 1}</span><div><strong>${title}</strong><p>${note}</p>${target ? `<button class="text-button" type="button" data-overview-target="${target}">${done ? 'View' : 'Open'} ${destination} ↗</button>` : '<span class="meta">Coming later</span>'}</div></div>`).join('');
}
function renderMapDetail() {
  const panel = $('#map-detail');
  const kind = mapSelection?.kind;
  const item = ({repository:data.repositories, job:data.jobs, workflow:data.workflows})[kind]?.find(x => x.id === mapSelection.id);
  panel.hidden = !item;
  if (!item) return;
  const related = kind === 'repository' ? data.jobs.filter(j => j.repositoryId === item.id).map(j => ({...j, kind:'job'})) : kind === 'job' ? data.workflows.filter(w => w.jobIds.includes(item.id)).map(w => ({...w, kind:'workflow'})) : item.jobIds.map(id => data.jobs.find(j => j.id === id)).filter(Boolean).map(j => ({...j, kind:'job'}));
  const target = kind === 'repository' ? 'repositories' : kind === 'job' ? 'jobs' : 'workflows';
  panel.innerHTML = `<div class="item-head"><div><span class="card-kicker">${escapeHTML(kind.toUpperCase())}</span><h3>${escapeHTML(item.name)}</h3></div><button class="text-button" type="button" data-overview-target="${target}" data-focus-id="${escapeHTML(item.id)}">Open detail ↗</button></div><div class="meta">Project: ${escapeHTML(item.project || 'Workspace')} · ID: ${escapeHTML(item.id)}</div><div class="meta">${kind === 'repository' ? 'Jobs using this source' : kind === 'job' ? 'Workflows containing this job' : 'Jobs in execution order'}:</div><div class="map-relations">${related.length ? related.map(x => `<button class="ghost" type="button" data-map-kind="${x.kind}" data-map-id="${escapeHTML(x.id)}">${escapeHTML(x.name)} ↗</button>`).join('') : '<span class="meta">None</span>'}</div>`;
}
function render() {
  ['jobs','workflows','schedules','runs'].forEach(k=>$('#'+k.slice(0,-1)+'-count').textContent=data[k].length);
  $('#active-count').textContent = data.runs.filter(r => r.status === 'running' || r.status === 'queued').length;
  $('#attention-count').textContent = data.runs.filter(r => r.status === 'waiting_approval').length;
  $('#last-sync').textContent = 'Updated ' + new Date().toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'});
  $('#overview-refresh-state').textContent = autoRefresh ? 'Refreshes every 10 seconds.' : 'Live updates paused.';
  renderOverview();
  $('#jobs-list').innerHTML=data.jobs.length ? data.jobs.map(j=>{const repo=data.repositories.find(r=>r.id===j.repositoryId);return `<div class="item" data-item-id="${escapeHTML(j.id)}"><div class="item-head"><div><h3>${escapeHTML(j.name)}</h3><div class="meta">${escapeHTML(j.project)} · ${escapeHTML(j.type)} · ${repo?'Git: '+escapeHTML(repo.name):'Workspace'} / ${escapeHTML(j.dir||'.')}</div></div><span class="badge">${escapeHTML(j.type)}</span></div><div class="item-actions"><button data-action="run-job" data-id="${j.id}">Run now</button><button class="ghost danger" data-action="delete-job" data-id="${j.id}">Delete</button><span class="code">${j.id}</span></div></div>`}).join('') : '<div class="empty">No jobs yet. Create your first job on the right.</div>';
  const visibleRepositories = data.repositories.filter(r => repositoryFilter !== 'unhealthy' || r.lastError || r.webhookLastError);
  $('#repositories-filter').hidden = repositoryFilter !== 'unhealthy';
  $('#repositories-list').innerHTML=visibleRepositories.length ? visibleRepositories.map(r=>{
    const githubRepo=new URL(r.url).hostname.toLowerCase()==='github.com';
    const triggerValue=triggerDrafts.has(r.id)?triggerDrafts.get(r.id):r.webhookTargetType+':'+r.webhookTargetId;
    const options=webhookTriggerOptions(r.id);
    return `<div class="item" data-item-id="${escapeHTML(r.id)}"><div class="item-head"><div><h3>${escapeHTML(r.name)}</h3><div class="meta">${escapeHTML(r.project)} · ${escapeHTML(r.url)} · ${escapeHTML(r.ref||'default branch')}</div></div>${statusBadge(r.lastError?'sync_failed':r.revision?'synced':'unsynced')}</div><div class="repo-detail">${r.revision?`Commit <span class="code" title="${escapeHTML(r.revision)}">${escapeHTML(r.revision.slice(0,12))}</span> · synced ${stamp(r.syncedAt)}`:'Not synced yet'}${r.lastError?`<div class="repo-error">${escapeHTML(r.lastError)}</div>`:''}<div>GitHub webhook: ${r.webhookEnabled?'enabled':'off'}${r.webhookLastDeliveryAt?' · last delivery '+stamp(r.webhookLastDeliveryAt):''}${r.webhookLastStatus?' · '+escapeHTML(r.webhookLastStatus):''}</div>${r.webhookLastRunId?`<div>Last triggered run: <span class="code">${escapeHTML(r.webhookLastRunId)}</span></div>`:''}${r.webhookLastError?`<div class="repo-error">${escapeHTML(r.webhookLastError)}</div>`:''}</div><div class="item-actions"><button data-action="sync-repository" data-id="${r.id}">Sync now</button>${githubRepo?`<button class="ghost" data-action="setup-webhook" data-id="${r.id}">${r.webhookEnabled?'Rotate webhook secret':'Set up webhook'}</button>${r.webhookEnabled?`<button class="ghost" data-action="disable-webhook" data-id="${r.id}">Disable webhook</button>`:''}`:''}<button class="ghost danger" data-action="delete-repository" data-id="${r.id}">Delete</button></div>${r.webhookEnabled?`<div class="trigger-controls"><label>On matching push<select data-trigger-repo="${r.id}"><option value="">Sync only</option>${options.map(o=>`<option value="${escapeHTML(o.value)}" ${o.value===triggerValue?'selected':''}>${escapeHTML(o.label)}</option>`).join('')}</select></label><button class="ghost" data-action="save-trigger" data-id="${r.id}">Save trigger</button>${r.webhookTargetId?`<button class="ghost" data-action="test-trigger" data-id="${r.id}">Create test run</button>`:''}<p class="form-note">Triggered jobs and workflows wait for admin approval. Runs use the pushed commit. A test run checks the mapping but does not test GitHub delivery.</p></div>`:''}</div>`;
  }).join('') : `<div class="empty">${repositoryFilter === 'unhealthy' ? 'No sources with errors.' : 'No repositories yet. Connect a public HTTPS Git repository on the right.'}</div>`;
  for(const r of data.repositories.filter(r=>r.webhookEnabled)) { const controls=document.querySelector(`[data-trigger-repo="${r.id}"]`)?.closest('.trigger-controls'); if(controls) { const button=document.createElement('button'); button.className='ghost'; button.textContent='View deliveries'; button.dataset.action='view-deliveries'; button.dataset.id=r.id; controls.insertBefore(button,controls.querySelector('p')); } }
  const selectedRepo=$('#job-repository').value;
  $('#job-repository').innerHTML='<option value="">Mounted workspace</option>'+data.repositories.map(r=>`<option value="${r.id}" ${r.revision?'':'disabled'}>${escapeHTML(r.name)}${r.revision?'':' (sync required)'}</option>`).join('');
  if(data.repositories.some(r=>r.id===selectedRepo && r.revision)) $('#job-repository').value=selectedRepo;
  $('#workflows-list').innerHTML=data.workflows.length ? data.workflows.map(w=>`<div class="item" data-item-id="${escapeHTML(w.id)}"><div class="item-head"><div><h3>${escapeHTML(w.name)}</h3><div class="meta">${escapeHTML(w.project)} · ${w.jobIds.length} steps · ${w.requireApproval?'Approval required':'Runs immediately'}</div></div><span class="badge">Workflow</span></div><div class="item-actions"><button data-action="run-workflow" data-id="${w.id}">Run now</button><button class="ghost danger" data-action="delete-workflow" data-id="${w.id}">Delete</button><span class="code">${w.id}</span></div></div>`).join('') : '<div class="empty">No workflows yet. Create a job, then add its ID to a workflow. <button class="text-button" type="button" data-overview-target="jobs">Open jobs ↗</button></div>';
  $('#schedules-list').innerHTML=data.schedules.length ? data.schedules.map(s=>`<div class="item" data-item-id="${escapeHTML(s.id)}"><div class="item-head"><div><h3>${escapeHTML(s.name)}</h3><div class="meta">${escapeHTML(s.targetType)} · every ${s.everyMinutes} min · next ${stamp(s.nextAt)}</div></div>${statusBadge(s.enabled ? 'enabled' : 'disabled')}</div><div class="item-actions"><button class="ghost danger" data-action="delete-schedule" data-id="${s.id}">Delete</button></div></div>`).join('') : '<div class="empty">No schedules yet. Choose a job or workflow, then set its interval.</div>';
  $('#schedule-target').innerHTML=[...data.jobs.map(j=>({key:'job:'+j.id,label:'Job: '+j.name})),...data.workflows.map(w=>({key:'workflow:'+w.id,label:'Workflow: '+w.name}))].map(x=>`<option value="${x.key}">${escapeHTML(x.label)}</option>`).join('');
  $('#schedule-form button[type="submit"]').disabled = !data.jobs.length && !data.workflows.length;
  const visibleRuns = data.runs.filter(r => runFilter === 'all' || (runFilter === 'active' && ['queued','running'].includes(r.status)) || (runFilter === 'approval' && r.status === 'waiting_approval') || runFilter === 'id:' + r.id);
  $('#runs-filter').hidden = runFilter === 'all';
  $('#runs-filter-label').textContent = runFilter === 'active' ? 'Showing active runs' : runFilter === 'approval' ? 'Showing runs awaiting approval' : 'Showing selected run';
  $('#runs-list').innerHTML=visibleRuns.length ? visibleRuns.map(r=>`<div class="item" data-item-id="${escapeHTML(r.id)}"><div class="item-head"><div><h3>${escapeHTML(r.targetName)}</h3><div class="meta">${escapeHTML(r.targetType)} · ${escapeHTML(r.triggeredBy)} · ${stamp(r.startedAt)}${r.step?' · step '+r.step:''}</div>${(r.sources||[]).map(s=>`<div class="meta">${escapeHTML(data.repositories.find(repo=>repo.id===s.repositoryId)?.name||s.repositoryId)} @ <span class="code" title="${escapeHTML(s.commit)}">${escapeHTML(s.commit.slice(0,12)||'unsynced')}</span></div>`).join('')}</div>${statusBadge(r.status)}</div><div class="item-actions">${r.status==='waiting_approval'?`<button data-action="approve" data-id="${r.id}">Approve</button>`:''}${['queued','running','waiting_approval'].includes(r.status)?`<button class="ghost danger" data-action="cancel" data-id="${r.id}">Cancel</button>`:''}<button class="ghost" data-action="toggle-log" data-id="${r.id}" aria-controls="log-${r.id}" aria-expanded="${openLogs.has(r.id)}">${openLogs.has(r.id)?'Hide log':'View log'}</button></div><pre id="log-${r.id}" ${openLogs.has(r.id)?'':'hidden'}>${escapeHTML(r.log||'No output yet.')}</pre></div>`).join('') : `<div class="empty">${runFilter === 'all' ? 'No runs yet. Create a job and use Run now to start one.' : 'No matching runs.'}</div>`;
  $('#audit-list').innerHTML=data.audit.length ? data.audit.slice(0,100).map((a,index)=>`<div class="item" data-audit-item="${index}"><strong>${escapeHTML(a.action)}</strong><div class="meta">${stamp(a.at)} · ${escapeHTML(a.detail)}</div></div>`).join('') : '<div class="empty">No activity yet.</div>';
}
function selectTab(name) {
  document.querySelectorAll('[data-tab]').forEach(x=>x.classList.toggle('active',x.dataset.tab===name));
  document.querySelectorAll('.tab-panel').forEach(x=>x.hidden=x.id!==name);
  document.querySelector('#'+name).scrollIntoView({block:'start',behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches?'auto':'smooth'});
}
document.addEventListener('click', async e => { const overview=e.target.closest('[data-overview-target]'); if(overview) { runFilter=overview.dataset.runId ? 'id:'+overview.dataset.runId : overview.dataset.runFilter||'all'; repositoryFilter=overview.dataset.repositoryFilter||'all'; render(); selectTab(overview.dataset.overviewTarget); const focus=overview.dataset.focusId && [...document.querySelectorAll(`#${overview.dataset.overviewTarget} [data-item-id]`)].find(x=>x.dataset.itemId===overview.dataset.focusId); const audit=overview.dataset.auditIndex && document.querySelector(`#audit [data-audit-item="${overview.dataset.auditIndex}"]`); (focus||audit)?.scrollIntoView({block:'center'}); return; }
  const mapNode=e.target.closest('[data-map-kind]'); if(mapNode) { mapSelection={kind:mapNode.dataset.mapKind,id:mapNode.dataset.mapId}; const collection={repository:data.repositories,job:data.jobs,workflow:data.workflows}[mapSelection.kind]; const item=collection?.find(x=>x.id===mapSelection.id); if(item) selectedProject=item.project||'Workspace'; renderOverview(); $('#map-detail').scrollIntoView({block:'nearest'}); return; }
  const tab=e.target.closest('[data-tab]'); if(tab) { if(tab.dataset.tab==='runs') {runFilter='all';render();} selectTab(tab.dataset.tab); return; }
  const copy=e.target.closest('[data-copy]'); if(copy) { try { await navigator.clipboard.writeText($('#'+copy.dataset.copy).value); toast('Copied'); } catch (_) { toast('Copy failed; select the field to copy',true); } return; }
  const button=e.target.closest('[data-action]'); if(!button) return; const {action,id}=button.dataset;
  if(action==='toggle-log') { const log=$('#log-'+id); log.hidden=!log.hidden; if(log.hidden) openLogs.delete(id); else openLogs.add(id); button.setAttribute('aria-expanded',String(!log.hidden)); button.textContent=log.hidden?'View log':'Hide log'; return; }
  if(action==='save-trigger') { const selected=document.querySelector(`[data-trigger-repo="${id}"]`).value; try { button.disabled=true; const [targetType,targetId]=selected.split(':'); await api(`/repositories/${id}/trigger`,selected?{method:'PUT',body:JSON.stringify({targetType,targetId})}:{method:'DELETE'}); triggerDrafts.delete(id); await load(); toast(selected?'Push trigger saved':'Push trigger removed'); } catch(err) { toast(err.message,true); } finally { button.disabled=false; } return; }
  if(action==='test-trigger') { try { button.disabled=true; await api(`/repositories/${id}/trigger/test`,{method:'POST'}); await load(); selectTab('runs'); toast('Test run waiting for approval'); } catch(err) { toast(err.message,true); } finally { button.disabled=false; } return; }
  if(action==='view-deliveries') { try { button.disabled=true; const deliveries=await api(`/repositories/${id}/deliveries`); const repo=data.repositories.find(r=>r.id===id); $('#webhook-deliveries-title').textContent=`Recent deliveries · ${repo?.name||'Repository'}`; $('#webhook-deliveries-list').innerHTML=deliveries.length?deliveries.map(d=>`<div class="item"><div class="item-head"><div><strong>${escapeHTML(d.event||'event')}</strong><div class="meta">${stamp(d.receivedAt)} · ${escapeHTML(d.ref||'—')}</div></div>${statusBadge(d.status)}</div><div class="repo-detail">Delivery <span class="code">${escapeHTML(d.id)}</span>${d.after?` · commit <span class="code">${escapeHTML(d.after.slice(0,12))}</span>`:''}${d.runId?` · run <span class="code">${escapeHTML(d.runId)}</span>`:''}</div>${d.error?`<div class="repo-error">${escapeHTML(d.error)}</div>`:''}</div>`).join(''):'<div class="empty">No deliveries yet. GitHub sends a ping when the webhook is created.</div>'; $('#webhook-deliveries').hidden=false; $('#webhook-deliveries').scrollIntoView({block:'nearest'}); } catch(err) { toast(err.message,true); } finally { button.disabled=false; } return; }
  if(action.startsWith('delete-') && !confirm('Delete this '+action.slice(7)+'?')) return;
  const paths={ 'run-job':['POST',`/jobs/${id}/run`], 'delete-job':['DELETE',`/jobs/${id}`], 'sync-repository':['POST',`/repositories/${id}/sync`], 'delete-repository':['DELETE',`/repositories/${id}`], 'setup-webhook':['POST',`/repositories/${id}/webhook`], 'disable-webhook':['DELETE',`/repositories/${id}/webhook`], 'run-workflow':['POST',`/workflows/${id}/run`], 'delete-workflow':['DELETE',`/workflows/${id}`], 'delete-schedule':['DELETE',`/schedules/${id}`], approve:['POST',`/runs/${id}/approve`], cancel:['POST',`/runs/${id}/cancel`]};
  try { button.disabled=true; const result=await api(paths[action][1],{method:paths[action][0]}); if(action==='setup-webhook') { $('#webhook-url').value=window.location.origin+result.path; $('#webhook-secret').value=result.secret; $('#webhook-setup').hidden=false; $('#webhook-setup').scrollIntoView({block:'nearest'}); } if(action==='disable-webhook'||action==='delete-repository') { hideWebhookSetup(); triggerDrafts.delete(id); } await load(); toast(action==='sync-repository'?'Repository synced':action==='setup-webhook'?'Webhook secret ready':action.startsWith('run-')?'Run started':'Updated'); } catch(err) { if(action==='sync-repository') await load().catch(()=>{}); toast(err.message,true); } finally { button.disabled=false; }
});
document.addEventListener('change',e=>{ if(e.target.matches('[data-trigger-repo]')) triggerDrafts.set(e.target.dataset.triggerRepo,e.target.value); });
$('#customize-overview').addEventListener('click',e=>{ const panel=$('#overview-options'); panel.hidden=!panel.hidden; e.currentTarget.setAttribute('aria-expanded',String(!panel.hidden)); });
document.addEventListener('change',e=>{ if(e.target.matches('[data-overview-section]')) { if(e.target.checked) visibleSections.add(e.target.dataset.overviewSection); else visibleSections.delete(e.target.dataset.overviewSection); try { localStorage.setItem('ausra-overview-sections',JSON.stringify([...visibleSections])); } catch (_) {} renderOverview(); } });
$('#project-picker').addEventListener('change',e=>{ selectedProject=e.target.value; mapSelection=null; renderOverview(); });
$('#clear-repositories-filter').addEventListener('click',()=>{repositoryFilter='all';render();});
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
$('#toggle-auto-refresh').addEventListener('click',()=>{autoRefresh=!autoRefresh;$('#toggle-auto-refresh').textContent=autoRefresh?'Pause live updates':'Resume live updates';$('#toggle-auto-refresh').setAttribute('aria-pressed',String(!autoRefresh));$('#refresh-state').textContent=autoRefresh?'Live updates every 10 seconds.':'Live updates paused.';$('#overview-refresh-state').textContent=autoRefresh?'Refreshes every 10 seconds.':'Live updates paused.';});
$('#clear-runs-filter').addEventListener('click',()=>{runFilter='all';render();});
api('/me').then(me=>{$('#workspace').textContent=me.workspace;showApp(true);return load();}).catch(()=>showApp(false));
setInterval(()=>{if(!$('#app').hidden && autoRefresh) load().catch(()=>{$('#last-sync').textContent='Update failed';$('#overview-refresh-state').textContent='Update failed. Retrying in 10 seconds.';});},10000);
