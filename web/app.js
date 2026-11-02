'use strict';

const ICONS = {
 branch:'<path d="M6 3v12a4 4 0 0 0 4 4h8M6 9h12m-4-4 4 4-4 4m0 2 4 4-4 4"/>',
 layers:'<path d="m3 8 9-5 9 5-9 5-9-5Z"/><path d="m3 12 9 5 9-5M3 16l9 5 9-5"/>',
 grid:'<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
 clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
 backup:'<path d="M4 7h13l-3-3M20 17H7l3 3M17 7l-3 3M7 17l3-3"/><path d="M20 7v4M4 17v-4"/>',
 camera:'<rect x="3" y="5" width="18" height="15" rx="2"/><path d="m8 5 1-2h6l1 2"/><circle cx="12" cy="12" r="4"/>',
 list:'<path d="M9 6h12M9 12h12M9 18h12M3 6h.01M3 12h.01M3 18h.01"/>',
 code:'<path d="m8 6-6 6 6 6M16 6l6 6-6 6M14 3l-4 18"/>',
 server:'<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M11 6.5h6M11 17.5h6"/>',
 disk:'<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="12" cy="11" r="5"/><path d="m14 13 3 4M6 18h.01"/>',
 chip:'<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9" y="9" width="6" height="6" rx="1"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/>',
 chevron:'<path d="m9 5 7 7-7 7"/>',
 down:'<path d="m6 9 6 6 6-6"/>',
 plus:'<path d="M12 5v14M5 12h14"/>',
 search:'<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
 shield:'<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>',
 check:'<path d="m5 12 4 4L19 6"/>',
 checkcircle:'<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>',
 circle:'<circle cx="12" cy="12" r="9"/>',
 info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
 x:'<path d="m6 6 12 12M6 18 18 6"/>',
 edit:'<path d="m16 3 5 5-13 13H3v-5L16 3ZM13 6l5 5"/>',
 play:'<path d="m8 4 12 8-12 8V4Z"/>',
 trash:'<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>',
 arrow:'<path d="M4 12h16m-5-5 5 5-5 5"/>',
 download:'<path d="M12 3v12m-5-5 5 5 5-5M4 15v6h16v-6"/>',
 refresh:'<path d="M21 7v5h-5M3 17v-5h5"/><path d="M5.5 6a8 8 0 0 1 13.2-.6L21 12M3 12l2.3 6.6A8 8 0 0 0 18.5 18"/>',
 folder:'<path d="M3 7a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/>',
 lock:'<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3"/>',
 book:'<path d="M4 3h6a3 3 0 0 1 3 3v15a4 4 0 0 0-4-2H4V3ZM13 6a3 3 0 0 1 3-3h5v16h-4a4 4 0 0 0-4 2"/>',
 menu:'<path d="M4 6h16M4 12h16M4 18h16"/>',
 leaf:'<path d="M20 3C7 2 2 7 5 15c8 8 16 1 15-12ZM4 21 15 10"/>',
 external:'<path d="M14 3h7v7m0-7L10 14M10 3H3v18h18v-7"/>',
 copy:'<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V3H3v13h5"/>',
 link:'<path d="m10 13 4-4M8 15l-2 2a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m4 2 2-2a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" transform="translate(1 -1)"/>',
 calendar:'<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M7 3v4M17 3v4M3 11h18"/>',
 warning:'<path d="m12 3 10 18H2L12 3Z"/><path d="M12 9v5M12 17h.01"/>'
};
const icon = (name, cls='') => `<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name]||ICONS.circle}</svg>`;

const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const $=(s,r=document)=>r.querySelector(s), all=(s,r=document)=>[...r.querySelectorAll(s)];
const TIERS=[['quarter_hourly','15 分钟'],['hourly','小时'],['daily','天'],['weekly','周'],['monthly','月']];
const PAGES=[['snapshots','clock','快照'],['backups','backup','备份'],['config','code','配置']];
const collapsedSnapshotDatasets=new Set();
let datasetScope=localStorage.getItem('chrona-dataset-scope')||'configured';
let backupScope=localStorage.getItem('chrona-backup-scope')||'planned';
let data,modal=null,search='',previewTimer,previewSequence=0,toastTimer,configDirty=false;
function normalizePage(s){return s==='backups'?s:s==='config'||s==='settings'?'config':'snapshots';}
let page=normalizePage(location.hash.slice(1));

async function api(path,options={}){
  const r=await fetch(path,{...options,headers:{...(options.body&&!options.yaml?{'Content-Type':'application/json'}:{}),...options.headers}});
  if(!r.ok){let message;try{message=(await r.json()).error;}catch{message=`请求失败 (${r.status})`;}throw new Error(message);}
  return r.headers.get('content-type')?.includes('application/json')?r.json():r.text();
}
function parts(date){return Object.fromEntries(new Intl.DateTimeFormat('en-CA',{timeZone:data?.timezone||'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hourCycle:'h23'}).formatToParts(new Date(date)).filter(x=>x.type!=='literal').map(x=>[x.type,x.value]));}
function dateTime(date,full=false){if(!date||String(date).startsWith('0001'))return '—';const p=parts(date);return `${full?p.year+'-':''}${p.month}-${p.day} ${p.hour}:${p.minute}`;}
function timeOnly(date){if(!date)return '—';const p=parts(date);return `${p.hour}:${p.minute}`;}
function localInput(date){const p=parts(date);return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;}
function fromLocalInput(value){
  const n=value.split(/[-T:]/).map(Number);if(n.length<5||n.some(x=>!Number.isFinite(x)))throw new Error('请填写完整的起始时间');
  let result=Date.UTC(n[0],n[1]-1,n[2],n[3],n[4]);const desired=result;
  for(let i=0;i<3;i++){const p=parts(new Date(result));result+=desired-Date.UTC(+p.year,+p.month-1,+p.day,+p.hour,+p.minute);}
  if(localInput(new Date(result))!==value)throw new Error('这个时间在当前时区不存在');return new Date(result).toISOString();
}
function relative(date){if(!date||String(date).startsWith('0001'))return '尚未运行';let s=(new Date(date)-new Date())/1000;const future=s>0;s=Math.abs(s);return (s<60?`${Math.max(1,Math.round(s))} 秒`:s<3600?`${Math.ceil(s/60)} 分钟`:s<86400?`${Math.round(s/3600)} 小时`:`${Math.round(s/86400)} 天`)+(future?'后':'前');}
function bytes(n){if(!n)return '0 B';const u=['B','KiB','MiB','GiB','TiB'],i=Math.min(4,Math.floor(Math.log(n)/Math.log(1024)));return `${(n/1024**i).toFixed(i?1:0)} ${u[i]}`;}
function span(k,n){if(!n)return '未启用';return k==='quarter_hourly'?`${n/4} 小时`:k==='hourly'?(n>=24?`${+(n/24).toFixed(1)} 天`:`${n} 小时`):`${n} ${k==='daily'?'天':k==='weekly'?'周':'个月'}`;}
function toast(message,error=false){clearTimeout(toastTimer);$('#toast-root').innerHTML=`<div class="toast ${error?'error':''}" role="status">${icon(error?'warning':'checkcircle')}<span>${esc(message)}</span></div>`;toastTimer=setTimeout(()=>$('#toast-root').innerHTML='',3500);}
function download(name,text){const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([text],{type:'application/yaml'}));a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(a.href),1000);}
function newID(p){return p+'-'+Array.from(crypto.getRandomValues(new Uint8Array(6)),n=>n.toString(16).padStart(2,'0')).join('');}

function rowFor(name){return data.dataset_rows.find(r=>r.config.name===name);}
function hasLocalSnapshot(r){return !!(r.config.policy||r.config.snapshot||r.config.snapshot_mode==='none');}
function datasetRows(){return data.dataset_rows.filter(r=>page==='snapshots'?(datasetScope==='all'||datasetScope==='direct'&&hasLocalSnapshot(r)||datasetScope==='configured'&&!!r.snapshot):(backupScope==='all'||r.is_leaf&&r.backups.length)).sort((a,b)=>a.config.name.localeCompare(b.config.name,'en',{numeric:true}));}
function policyUses(id){return data.dataset_rows.filter(r=>r.snapshot?.id===id).length;}
async function commit(config){return api('/api/config',{method:'PUT',body:JSON.stringify(config),yaml:true,headers:{'Content-Type':'application/json'}});}
function shell(){
  $('#app').innerHTML=`<header class="appbar"><div class="appbar-inner"><a class="brand" href="#snapshots"><span class="brand-mark">${icon('layers')}</span>Chrona</a><nav class="tabs" aria-label="主导航">${PAGES.map(([id,i,title])=>`<a href="#${id}" class="tab ${page===id?'active':''}" ${page===id?'aria-current="page"':''}>${icon(i)}${title}</a>`).join('')}</nav><div class="host"><span class="dot"></span><span>${esc(data.hostname)}</span><span class="mode">${data.mode==='demo'?'DEMO':data.writes_enabled?'ZFS':'只读'}</span></div></div></header><main id="main" class="main"></main>`;renderPage();
}
function heading(title,description,button=''){return `<div class="page-heading"><div><h1>${title}</h1><p>${description}</p></div>${button}</div>`;}
function empty(title,text){return `<div class="empty">${icon('folder')}<h3>${title}</h3><p>${text}</p></div>`;}
function miniKeep(keep){return keep?`<div class="mini-keep">${TIERS.map(([key,label],i)=>`<span class="tier-${i}" title="${label} ${keep[key]} 张"><small>${label}</small><b>${keep[key]||'—'}</b></span>`).join('')}</div>`:'<span class="muted-label">—</span>';}
function snapshotBranches(rows){return rows.filter(r=>rows.some(child=>child.config.name.startsWith(r.config.name+'/'))).map(r=>r.config.name);}
function datasetToolbar(rows,mappingCount){
 const isSnapshot=page==='snapshots',branches=isSnapshot?snapshotBranches(rows):[];
 const scopes=isSnapshot?[['configured','有快照策略'],['direct','仅本级设置'],['all','全部数据集']]:[['planned','有备份计划'],['all','全部数据集']],scope=isSnapshot?datasetScope:backupScope;
 return `<div class="toolbar list-toolbar"><div class="dataset-toolbar-left"><span>${rows.length} / ${data.dataset_rows.length} 个数据集${mappingCount===undefined?'':` · ${mappingCount} 个目的地`}</span><div class="scope-control" role="group" aria-label="数据集显示范围">${scopes.map(([key,label])=>`<button class="${scope===key?'active':''}" data-action="dataset-scope" data-scope="${key}">${label}</button>`).join('')}</div></div><div class="list-toolbar-right">${branches.length?`<button class="text-button" data-action="tree-all">${branches.some(name=>!collapsedSnapshotDatasets.has(name))?'全部收起':'全部展开'}</button>`:''}<label class="search">${icon('search')}<input id="search" value="${esc(search)}" placeholder="${isSnapshot?'搜索路径或策略':'搜索源、目标或规则'}" aria-label="搜索数据集"></label></div></div>`;
}
function scheduleTime(value,fallback='—'){return value&&!String(value).startsWith('0001')?`<time datetime="${esc(value)}" title="${dateTime(value,true)} · ${esc(data.timezone)}">${dateTime(value)}</time>`:`<span class="muted-label">${fallback}</span>`;}
function retentionSummary(keep){return keep?`<span class="retention-counts">${TIERS.map(([key,label],i)=>`<span class="tier-${i} ${keep[key]?'':'off'}" title="每${label}一张，保留 ${keep[key]} 张"><span>${label==='15 分钟'?'15分':label}</span><b>${keep[key]}</b></span>`).join('')}</span>`:'<span class="muted-label">—</span>';}
function renderSnapshotRows(rows){
 const branches=snapshotBranches(rows),visible=rows.filter(r=>search||!branches.some(parent=>collapsedSnapshotDatasets.has(parent)&&r.config.name.startsWith(parent+'/')));
 return visible.map(r=>{const name=r.config.name,p=r.snapshot,branch=branches.includes(name),expanded=!collapsedSnapshotDatasets.has(name),depth=rows.filter(parent=>name.startsWith(parent.config.name+'/')).length;
  const label=p?(data.config.policies.find(rule=>rule.id===p.id)?.label||'自定义规则'):r.config.snapshot_mode==='none'||r.snapshot_source?'不创建快照':'未设置策略';
  return `<tr class="snapshot-row ${hasLocalSnapshot(r)?'local-rule':''}" data-dataset-row="${esc(name)}" data-inherited-from="${r.inherited?esc(r.snapshot_source):''}"><td><div class="snapshot-identity" style="--depth:${depth}">${branch?`<button class="fold-button" data-action="toggle-tree" data-dataset="${esc(name)}" aria-expanded="${expanded}" aria-label="${expanded?'收起':'展开'} ${esc(name)}">${icon('chevron')}</button>`:'<span class="fold-space"></span>'}<span class="dataset-path">${esc(name)}</span></div></td><td class="snapshot-policy"><span class="policy-name">${esc(label)}</span><small class="cell-note ${r.inherited?'inherited':hasLocalSnapshot(r)?'local-rule-badge':''}" title="${r.inherited?'继承自 '+esc(r.snapshot_source):''}">${r.inherited?'继承父级':p?(r.config.policy?'本级引用':'本级自定义'):r.config.snapshot_mode==='none'?'停止继承':'未设置'}</small></td><td class="snapshot-keep">${retentionSummary(p?.keep)}</td><td class="snapshot-last">${scheduleTime(r.latest_snapshot?.created_at,'尚无快照')}</td><td class="snapshot-next">${scheduleTime(r.next?.run_at,p?'已暂停':'未启用')}</td><td class="row-actions"><div class="inline-actions"><button class="text-button" data-action="browse" data-dataset="${esc(name)}" aria-label="查看 ${esc(name)} 现有快照">快照 <span class="count-label">${r.snapshot_count}</span></button><button class="text-button" data-action="plan" data-dataset="${esc(name)}" aria-label="查看 ${esc(name)} 快照计划">计划</button><button class="icon-button" data-action="edit-dataset" data-dataset="${esc(name)}" aria-label="编辑数据集 ${esc(name)}" title="编辑策略">${icon('edit')}</button><button class="icon-button" data-action="run-dataset" data-dataset="${esc(name)}" aria-label="立即快照 ${esc(name)}" title="立即快照" ${!p||!data.writes_enabled?'disabled':''}>${icon('camera')}</button>${p?`<button class="switch-button" data-action="toggle-dataset" data-dataset="${esc(name)}" role="switch" aria-checked="${p.enabled}" aria-label="${esc(name)} 快照启用状态" title="${p.enabled?'暂停':'启用'}自动快照"><span class="switch ${p.enabled?'on':''}"></span></button>`:''}</div></td></tr>`;
 }).join('');
}
function datasetTable(){const rows=datasetRows().filter(r=>!search||`${r.config.name} ${data.config.policies.find(p=>p.id===r.snapshot?.id)?.label||''} ${r.snapshot_source||''}`.toLowerCase().includes(search.toLowerCase()));return `<section class="dataset-panel dataset-snapshot-list">${datasetToolbar(rows)}${rows.length?`<div class="list-scroll" tabindex="0" role="region" aria-label="数据集快照列表"><table class="operation-table snapshot-table"><colgroup><col class="col-snapshot-source"><col class="col-snapshot-policy"><col class="col-snapshot-keep"><col class="col-snapshot-last"><col class="col-snapshot-next"><col class="col-snapshot-actions"></colgroup><thead><tr><th>数据集 · 完整路径</th><th>快照策略</th><th>保留数量 · 每档张数</th><th>上次快照</th><th>下一次快照</th><th>操作</th></tr></thead><tbody>${renderSnapshotRows(rows)}</tbody></table></div><p class="scroll-hint">横向滚动查看全部列</p>`:empty('没有匹配的数据集','切换视图或搜索条件，查看其他数据集。')}</section>`;}
function publicSnapshotPolicies(){return `<details class="public-section"><summary><span>${icon('book')}公共快照策略 <b>${data.config.policies.length}</b></span><span>维护可复用的规则 ${icon('down')}</span></summary><div class="public-body"><div class="public-toolbar"><p>修改公共策略会同步影响引用它的数据集。</p><button class="button small" data-action="new-policy">${icon('plus')}添加公共策略</button></div>${data.config.policies.map(p=>{const uses=policyUses(p.id);return `<div class="public-rule" data-public-policy="${esc(p.id)}"><div><strong>${esc(p.label)}</strong><small>${uses} 个数据集引用</small></div>${miniKeep(p.keep)}<button class="button small" data-action="edit-policy" data-id="${esc(p.id)}" aria-label="编辑公共策略 ${esc(p.label)}">编辑</button></div>`;}).join('')||'<p class="field-note">尚未定义公共策略。</p>'}</div></details>`;}
function overviewCard(key,label,value,note,i,metric=value,tone=''){
 return `<article class="overview-card ${tone}" data-stat="${key}" data-value="${esc(metric)}"><div class="overview-card-head"><span>${label}</span><span class="overview-icon">${icon(i)}</span></div><strong class="overview-value">${esc(value)}</strong><p title="${esc(note)}">${esc(note)}</p></article>`;
}
function nearestScheduled(items){return items.filter(x=>x.at&&!String(x.at).startsWith('0001')).sort((a,b)=>new Date(a.at)-new Date(b.at))[0];}
function nextOverview(key,label,item,i){const p=item?parts(item.at):null;return overviewCard(key,label,item?timeOnly(item.at):'—',item?`${p.month}-${p.day} · ${item.name}`:'暂无启用的计划',i,item?.at||'');}
function snapshotOverview(){
 const assigned=data.dataset_rows.filter(r=>r.snapshot),enabled=assigned.filter(r=>r.snapshot.enabled).length,next=nearestScheduled(assigned.map(r=>({at:r.next?.run_at,name:r.config.name})));
 return `<section class="overview-grid" aria-label="全机快照概览">${overviewCard('snapshot-datasets','自动快照数据集',enabled,`${assigned.length} 个有策略 · ${assigned.length-enabled} 个已暂停`,'layers',enabled)}${overviewCard('snapshot-count','现有快照',data.stats.snapshots,`24 小时新增 ${data.stats.recent} · 永久保留 ${data.stats.permanent||0}`,'camera')}${overviewCard('snapshot-space','快照独占空间',bytes(data.stats.snapshot_bytes),'本机快照 USED 合计','disk',data.stats.snapshot_bytes)}${nextOverview('snapshot-next','下一次快照',next,'clock')}</section>`;
}
function backupOverview(){
 const entries=data.dataset_rows.filter(r=>r.is_leaf).flatMap(r=>r.backups.map(entry=>({r,entry}))),enabled=entries.filter(({entry})=>entry.task.enabled&&!entry.disabled),jobs=[...new Map(entries.map(({entry})=>[entry.task.id,entry])).values()],issues=jobs.filter(x=>x.state.error||x.state.warning).length,running=jobs.filter(x=>x.state.status==='running').length;
 const next=nearestScheduled(enabled.map(({r,entry})=>({at:entry.next,name:r.config.name})));
 return `<section class="overview-grid" aria-label="全部备份概览">${overviewCard('backup-targets','备份目的地',entries.length,`${new Set(entries.map(x=>x.r.config.name)).size} 个源数据集 · ${jobs.length} 个任务`,'backup')}${overviewCard('backup-enabled','已启用目的地',enabled.length,`${entries.length-enabled.length} 个已暂停`,'checkcircle')}${overviewCard('backup-health','需关注的任务',issues,`${running} 条运行中 · 按备份任务统计`,issues?'warning':'shield',issues,issues?'attention':'')}${nextOverview('backup-next','下一次备份',next,'calendar')}</section>`;
}
function renderSnapshots(){return heading('数据集快照','父级统一设置，子数据集默认继承，也可单独覆盖。',`<button class="button primary" data-action="new-dataset">${icon('plus')}添加数据集</button>`)+snapshotOverview()+datasetTable()+publicSnapshotPolicies()+`<p class="page-note">${data.mode==='demo'?'当前为演示数据。':''}子数据集自动继承最近父级的规则；本级设置优先，暂停仅影响当前数据集。</p>`;}
function backupHost(b){const at=b.host.lastIndexOf('@'),user=at<0?'':b.host.slice(0,at+1),address=b.host.slice(at+1),host=user+(address.includes(':')&&!address.startsWith('[')?'['+address+']':address);return host+(b.port&&b.host!=='local'?':'+b.port:'');}
function backupPolicyName(b){return b.policy?(data.config.backup_policies.find(p=>p.id===b.policy)?.name||b.policy_name||b.name):'自定义时间';}
function backupPolicyUses(id){return data.config.backups.filter(b=>b.policy===id).reduce((count,b)=>count+b.datasets.length,0);}
function backupSchedule(b){return b.cadence==='monthly'?`每月 ${b.month_day} 日 ${b.at}`:`${(b.weekdays||[]).map(x=>['周日','周一','周二','周三','周四','周五','周六'][x]).join('、')} ${b.at}`;}
function backupStatus(r,entry){
 const s=entry.state,b=entry.task,at=s.completed?.[r.config.name];
 if(!r.is_leaf)return '<span class="status-badge neutral">非叶子节点</span>';
 if(s.status==='running')return `<span class="status-badge running">任务运行中</span><small class="cell-note">${!b.enabled||entry.disabled?'后续已暂停':data.mode==='demo'?s.progress+'%':s.bytes?bytes(s.bytes):'准备传输'}</small>`;
 if(!b.enabled||entry.disabled)return '<span class="status-badge neutral">已暂停</span>';
 if(s.error)return `<span class="status-badge error">任务异常</span><small class="cell-note row-error">${esc(s.error)}</small>`;
 if(s.warning)return `<span class="status-badge warning">需要关注</span><small class="cell-note row-error">${esc(s.warning)}</small>`;
 return `<span class="status-badge ${at&&!String(at).startsWith('0001')?'success':'neutral'}">${at&&!String(at).startsWith('0001')?'成功':'尚未运行'}</span>`;
}
function backupListRows(){const entries=datasetRows().flatMap(r=>r.backups.length?r.backups.map(entry=>({r,entry})):[{r,entry:null}]);const query=search.toLowerCase();return entries.filter(({r,entry})=>!search||`${r.config.name} ${entry?.destination||''} ${entry?backupHost(entry.task):''} ${entry?.task.name||''} ${entry?backupPolicyName(entry.task):''}`.toLowerCase().includes(query)).sort((a,b)=>a.r.config.name.localeCompare(b.r.config.name,'en',{numeric:true})||`${a.entry?backupHost(a.entry.task):''}:${a.entry?.destination||''}`.localeCompare(`${b.entry?backupHost(b.entry.task):''}:${b.entry?.destination||''}`,'en',{numeric:true})||(a.entry?.task.id||'').localeCompare(b.entry?.task.id||''));}
function renderBackupRow({r,entry}){
 const name=r.config.name,add=r.is_leaf?`<button class="icon-button" data-action="dataset-backup" data-dataset="${esc(name)}" data-template="${esc(entry?.task.id||'')}" aria-label="为 ${esc(name)} 添加目的地" title="添加目的地">${icon('plus')}</button>`:'';
 if(!entry)return `<tr data-backup-dataset="${esc(name)}" data-backup-id=""><td class="backup-source"><span class="dataset-path">${esc(name)}</span></td><td class="backup-destination muted-label">—</td><td class="backup-rule muted-label">未设置</td><td class="backup-last muted-label">—</td><td class="backup-next muted-label">—</td><td class="backup-status"><span class="status-badge neutral">${r.is_leaf?'无备份计划':'非叶子节点'}</span></td><td class="row-actions">${r.is_leaf?`<button class="button small" data-action="dataset-backup" data-dataset="${esc(name)}">添加计划</button>`:''}</td></tr>`;
 const b=entry.task,s=entry.state,active=r.is_leaf&&b.enabled&&!entry.disabled,at=s.completed?.[name],transfer=s.transfers?.[name];
 return `<tr data-backup-dataset="${esc(name)}" data-backup-id="${esc(b.id)}"><td class="backup-source"><span class="dataset-path">${esc(name)}</span></td><td class="backup-destination"><span class="dataset-path">${esc(backupHost(b))}:${esc(entry.destination)}</span>${entry.replica?`<small class="cell-note" title="${esc(entry.replica.name)} · 保留至 ${dateTime(entry.replica.expires_at,true)}">${transfer?.mode==='incremental'?'增量备份':transfer?.mode==='full'?'全量备份':'共同基点已保护'}</small>`:''}</td><td class="backup-rule"><button class="cell-link" data-action="${b.policy?'edit-backup-policy':'dataset-backup'}" data-dataset="${esc(name)}" data-id="${esc(b.policy||b.id)}" title="${b.policy?'编辑公共时间策略':'编辑此计划'}">${esc(backupPolicyName(b))}</button><small class="cell-note">${backupSchedule(b)}</small></td><td class="backup-last">${scheduleTime(at,'尚未运行')}</td><td class="backup-next">${scheduleTime(active?entry.next:null,active?'—':r.is_leaf?'已暂停':'不执行')}</td><td class="backup-status">${backupStatus(r,entry)}</td><td class="row-actions"><div class="inline-actions"><button class="icon-button" data-action="run-backup" data-id="${esc(b.id)}" data-dataset="${esc(name)}" aria-label="立即备份 ${esc(name)} 到 ${esc(backupHost(b))}:${esc(entry.destination)}" title="立即备份到此目的地" ${!active||s.status==='running'||!data.writes_enabled?'disabled':''}>${icon('play')}</button><button class="icon-button" data-action="dataset-backup" data-dataset="${esc(name)}" data-id="${esc(b.id)}" aria-label="编辑 ${esc(name)} 到 ${esc(backupHost(b))}:${esc(entry.destination)}" title="编辑此目的地">${icon('edit')}</button>${add}${r.is_leaf?`<button class="switch-button" data-action="toggle-backup" data-id="${esc(b.id)}" data-dataset="${esc(name)}" role="switch" aria-checked="${active}" aria-label="${esc(name)} 到 ${esc(backupHost(b))}:${esc(entry.destination)} 备份启用状态" title="${data.backups.some(x=>x.state.status==='running')?'有备份正在运行，完成后可修改启用状态':(active?'暂停':'启用')+'此目的地的自动备份'}" ${data.backups.some(x=>x.state.status==='running')?'disabled':''}><span class="switch ${active?'on':''}"></span></button>`:''}</div></td></tr>`;
}
function renderBackups(){const entries=backupListRows(),rows=[...new Map(entries.map(({r})=>[r.config.name,r])).values()];return heading('数据集备份','每个源与目的地单独一行，按完整路径排序。',`<button class="button primary" data-action="new-dataset-backup">${icon('plus')}添加备份</button>`)+backupOverview()+`<section class="dataset-panel dataset-backup-list">${datasetToolbar(rows,entries.filter(x=>x.entry).length)}${entries.length?`<div class="list-scroll" tabindex="0" role="region" aria-label="数据集备份列表"><table class="operation-table backup-table"><colgroup><col class="col-backup-source"><col class="col-backup-destination"><col class="col-backup-rule"><col class="col-backup-time"><col class="col-backup-time"><col class="col-backup-status"><col class="col-backup-actions"></colgroup><thead><tr><th>源数据集 · ${esc(data.hostname)}</th><th>目标主机 : 数据集</th><th>时间策略 / 频率</th><th>上次成功</th><th>下一次备份</th><th>状态</th><th>操作</th></tr></thead><tbody>${entries.map(renderBackupRow).join('')}</tbody></table></div><p class="scroll-hint">横向滚动查看全部列</p>`:empty('暂无匹配的备份计划','切换到全部数据集，或添加一个备份目的地。')}</section>`+publicBackupPolicies()+`<p class="page-note">${data.mode==='demo'?'当前为传输演示。':''}源与目标的共同基点保留至下次备份加缓冲；新的备份成功后才切换基点。</p>`;}
function publicBackupPolicies(){const items=data.config.backup_policies;return `<details class="public-section"><summary><span>${icon('book')}公共备份时间策略 <b>${items.length}</b></span><span>维护可复用的时间安排 ${icon('down')}</span></summary><div class="public-body"><div class="public-toolbar"><p>只共享备份时间和缓冲；每个目的地独立设置目标主机与数据集。</p><button class="button small" data-action="new-backup-policy">${icon('plus')}添加公共策略</button></div>${items.map(b=>`<div class="public-rule" data-public-backup="${esc(b.id)}"><div><strong>${esc(b.name)}</strong><small>${backupPolicyUses(b.id)} 个备份目的地引用</small></div><span class="backup-summary">${backupSchedule(b)} · 缓冲 ${b.grace_minutes||60} 分钟</span><button class="button small" data-action="edit-backup-policy" data-id="${esc(b.id)}" aria-label="编辑公共时间策略 ${esc(b.name)}">编辑</button></div>`).join('')||'<p class="field-note">尚未定义公共时间策略。</p>'}</div></details>`;}
async function renderConfig(){const yaml=await api('/api/config');if(page!=='config')return;$('#main').innerHTML=heading('配置','快照可继承父级规则，备份计划按数据集单独设置。',`<div class="actions"><button class="button" data-action="download">${icon('download')}下载</button><button class="button primary" data-action="save-config">保存配置</button></div>`)+`<section class="config-panel"><header>${icon('code')}config.yaml</header><textarea id="config-yaml" class="code-editor" spellcheck="false" aria-label="YAML 配置">${esc(yaml)}</textarea><div class="config-validation" id="config-validation">保存时自动检查配置和引用关系。</div></section>`;configDirty=false;}
function renderPage(){
 if(page==='config'){renderConfig().catch(e=>toast(e.message,true));return;}
 const main=$('#main'),samePage=main.dataset.page===page,publicOpen=samePage&&!!$('.public-section',main)?.open,scroll=samePage?$('.list-scroll',main)?.scrollLeft||0:0;
 const focused=main.contains(document.activeElement)&&document.activeElement.matches('[data-action]')?{...document.activeElement.dataset}:null;
 main.innerHTML=page==='backups'?renderBackups():renderSnapshots();main.dataset.page=page;
 if(publicOpen&&$('.public-section',main))$('.public-section',main).open=true;
 const table=$('.list-scroll',main);if(table)table.scrollLeft=scroll;
 if(focused&&!modal)all('[data-action]',main).find(x=>x.dataset.action===focused.action&&x.dataset.dataset===focused.dataset&&x.dataset.id===focused.id&&x.dataset.scope===focused.scope)?.focus({preventScroll:true});
}
async function refresh(render=true){data=await api('/api/state');document.title=`${data.hostname} · Chrona`;if(render){if(!$('#main'))shell();else renderPage();}}
function showModal(html,state){const f=modal?.returnFocus?.isConnected?modal.returnFocus:document.activeElement;modal=state;modal.returnFocus=f;$('#modal-root').innerHTML=html;document.body.style.overflow='hidden';setTimeout(()=>$('#modal-root input:not(:disabled),#modal-root select,#modal-root button')?.focus(),20);}
function closeModal(){const f=modal?.returnFocus;modal=null;previewSequence++;clearTimeout(previewTimer);$('#modal-root').innerHTML='';document.body.style.overflow='';if(f?.isConnected)f.focus();}
function drawer(title,body,footer,id){return `<div class="shade" data-action="close"></div><section class="drawer" role="dialog" aria-modal="true" aria-labelledby="dialog-title"><header class="drawer-head"><h2 id="dialog-title">${title}</h2><button class="icon-button" data-action="close" aria-label="关闭">${icon('x')}</button></header><form id="${id}" class="drawer-form"><div class="drawer-body">${body}</div><footer class="drawer-foot">${footer}</footer></form></section>`;}
function keepFields(keep){return `<div class="section-label">每档保留数量 <small>0 表示关闭</small></div><div class="keep-grid">${TIERS.map(([key,label],i)=>`<div class="keep-card tier-${i}"><span>${label}</span><div class="stepper"><button type="button" data-action="step" data-key="${key}" data-step="-1" aria-label="减少${label}数量">−</button><input type="number" id="keep-${key}" name="${key}" min="0" max="10000" step="1" value="${keep[key]}" required aria-label="${label}保留数量"><button type="button" data-action="step" data-key="${key}" data-step="1" aria-label="增加${label}数量">+</button></div><small id="span-${key}">${span(key,keep[key])}</small></div>`).join('')}</div>`;}
const defaultKeep=()=>({quarter_hourly:0,hourly:0,daily:0,weekly:0,monthly:0});
function editorButtons(remove=''){return `<span>${remove}</span><div class="actions"><button type="button" class="button" data-action="close">取消</button><button type="submit" class="button primary" id="save-editor">保存</button></div>`;}
function openDataset(name){
  const row=rowFor(name),d=row?structuredClone(row.config):{name:''},settings=row?.snapshot;
  const mode=d.snapshot?'custom':d.policy||(d.snapshot_mode==='none'?'none':'inherit');
  const body=`<label class="field"><span>数据集</span><input name="name" list="dataset-options" value="${esc(d.name)}" placeholder="例如 Saturn 或 Saturn/APP" required ${row?'readonly':''}><datalist id="dataset-options">${data.datasets.filter(x=>!data.config.datasets.some(d=>d.name===x.name)).map(x=>`<option value="${esc(x.name)}">`).join('')}</datalist><small class="field-note">选择父数据集后，其已有和新增子数据集默认纳入管理。</small></label><label class="field"><span>快照策略</span><select name="mode" id="snapshot-mode"><option value="inherit" ${mode==='inherit'?'selected':''}>继承父级（默认）</option><option value="custom" ${mode==='custom'?'selected':''}>自定义 · 本级覆盖</option>${data.config.policies.map(p=>`<option value="${esc(p.id)}" ${mode===p.id?'selected':''}>公共 · ${esc(p.label)}</option>`).join('')}<option value="none" ${mode==='none'?'selected':''}>不创建快照（停止继承）</option></select></label><div class="binding-explanation" id="binding-explanation"></div><section id="snapshot-fields"><label class="field"><span>起始时间</span><input type="datetime-local" name="start_at" value="${localInput(settings?.start_at||new Date())}" required></label>${keepFields(settings?.keep||defaultKeep())}<div id="policy-validation" class="validation"></div><label class="enabled-field"><span>启用此数据集的自动快照</span><input type="checkbox" name="enabled" ${!d.disabled?'checked':''}></label><div id="policy-preview" class="preview-line"></div></section>`;
  const remove=row?.managed_ancestor?`<button type="button" class="text-button" data-action="inherit-dataset">恢复继承</button>`:row?.explicit?`<button type="button" class="text-button danger" data-action="remove-dataset" data-dataset="${esc(name)}">移除管理</button>`:'';
  showModal(drawer(row?'编辑数据集快照':'添加数据集',body,editorButtons(remove),'dataset-form'),{type:'dataset',dataset:d,originalName:name});setSnapshotMode(false);requestPreview();
}
function setSnapshotMode(changed){
  const form=$('#dataset-form'),mode=form.elements.mode.value,shared=!['none','inherit','custom'].includes(mode);
  $('#snapshot-fields').hidden=mode==='none';
  if(changed&&mode==='custom'){const original=modal.dataset.snapshot;form.elements.start_at.value=localInput(original?.start_at||new Date());const keep=original?.keep||defaultKeep();for(const [key]of TIERS)form.elements[key].value=keep[key];}
  if(changed&&shared){const p=data.config.policies.find(p=>p.id===mode);form.elements.start_at.value=localInput(p.start_at);for(const [key]of TIERS)form.elements[key].value=p.keep[key];}
  all('input[name=start_at],.stepper input,.stepper button',form).forEach(x=>x.disabled=mode!=='custom');
  $('#binding-explanation').innerHTML=mode==='inherit'?'沿用最近父级的策略，自动跟随其更新。':shared?`本级引用公共规则，未单独设置的子数据集会继承。<button type="button" class="text-button" data-action="make-custom">复制为独立规则</button>`:mode==='custom'?'在本级设置自定义规则，未单独设置的子数据集会继承。':'本级与继承它的子数据集不创建快照；子级仍可单独指定规则。';
}
function readKeep(form){const keep={};for(const [key]of TIERS){const value=form.elements[key].value;keep[key]=Number(value);if(value===''||!Number.isInteger(keep[key])||keep[key]<0||keep[key]>10000)throw new Error('保留数量必须是 0～10000 的整数');}return keep;}
function readDataset(){const f=$('#dataset-form'),d={name:f.elements.name.value.trim(),disabled:!f.elements.enabled.checked},mode=f.elements.mode.value;if(mode==='custom')d.snapshot={start_at:fromLocalInput(f.elements.start_at.value),keep:readKeep(f)};else if(mode==='none')d.snapshot_mode='none';else if(mode!=='inherit')d.policy=mode;return d;}
async function saveDataset(e){e.preventDefault();try{const d=readDataset(),c=structuredClone(data.config),index=c.datasets.findIndex(x=>x.name===modal.originalName);if(index<0){if(c.datasets.some(x=>x.name===d.name))throw new Error('这个数据集已经添加');c.datasets.push(d);}else c.datasets[index]=d;await commit(c);closeModal();await refresh();toast('数据集策略已保存');}catch(e){toast(e.message,true);}}
function openPublicPolicy(id){const p=structuredClone(data.config.policies.find(p=>p.id===id)||{id:newID('policy'),label:'',start_at:new Date().toISOString(),keep:defaultKeep()}),uses=policyUses(id);const body=`<label class="field"><span>公共策略名称</span><input name="label" value="${esc(p.label)}" required maxlength="160"></label><label class="field"><span>起始时间</span><input type="datetime-local" name="start_at" value="${localInput(p.start_at)}" required></label>${keepFields(p.keep)}<div id="policy-validation" class="validation"></div><p class="field-note">${uses?'保存会同步更新 '+uses+' 个引用它的数据集。':'数据集可以直接引用这组规则。'}</p><div id="policy-preview" class="preview-line"></div>`;showModal(drawer(id?'编辑公共快照策略':'添加公共快照策略',body,editorButtons(id?`<button type="button" class="text-button danger" data-action="delete-public-policy" data-id="${esc(id)}" ${uses?'disabled title="请先切换引用者的策略"':''}>删除公共策略</button>`:''),'policy-form'),{type:'public-policy',policy:p});requestPreview();}
function readPublicPolicy(){const f=$('#policy-form');return {...modal.policy,label:f.elements.label.value.trim(),start_at:fromLocalInput(f.elements.start_at.value),keep:readKeep(f)};}
async function savePublicPolicy(e){e.preventDefault();try{const p=readPublicPolicy(),c=structuredClone(data.config),i=c.policies.findIndex(x=>x.id===p.id);if(i<0)c.policies.push(p);else c.policies[i]=p;await commit(c);closeModal();await refresh();toast('公共策略已保存，引用者已同步');}catch(e){toast(e.message,true);}}
async function requestPreview(){const seq=++previewSequence;for(const [key]of TIERS){const input=$(`#keep-${key}`),label=$(`#span-${key}`);if(input&&label)label.textContent=span(key,Number(input.value)||0);}try{
  let p,r;if(modal?.type==='dataset'){
    const f=$('#dataset-form'),d=readDataset(),mode=f.elements.mode.value;
    if(!d.name){$('#snapshot-fields').hidden=mode==='inherit'||mode==='none';return;}
    r=await api('/api/dataset-preview',{method:'POST',body:JSON.stringify(d)});
    if(seq!==previewSequence||modal?.type!=='dataset')return;
    p=r.snapshot;$('#snapshot-fields').hidden=!p;
    if(mode==='inherit')$('#binding-explanation').innerHTML=p?`${icon('branch')}继承自 <strong>${esc(r.snapshot_source)}</strong> · ${esc(data.config.policies.find(x=>x.id===p.id)?.label||'自定义规则')}<button type="button" class="text-button" data-action="make-custom">复制为独立规则</button>`:r.snapshot_source?`继承在 ${esc(r.snapshot_source)} 停止。可以在本级设置规则重新启用。`:'父级尚未设置快照策略。可以先设置父级，或在本级指定规则。';
    if(!p)return;
    if(mode!=='custom'){f.elements.start_at.value=localInput(p.start_at);for(const [key]of TIERS)f.elements[key].value=p.keep[key];}
  }else if(modal?.type==='public-policy'){
    p={...readPublicPolicy(),label:readPublicPolicy().label||'公共策略'};r=await api('/api/preview',{method:'POST',body:JSON.stringify(p)});
    if(seq!==previewSequence||!modal)return;
  }else return;
  for(const [key]of TIERS)$(`#span-${key}`).textContent=span(key,p.keep[key]);
  $('#policy-validation').className='validation valid';$('#policy-validation').innerHTML=icon('checkcircle')+'保留跨度符合规则';const n=r.events[0];$('#policy-preview').innerHTML=n?`${icon('clock')}下一次 ${dateTime(n.run_at)} · ${n.label}档${n.merged.length>1?' · '+n.merged.length+' 档合并':''}`:p.enabled===false?'此数据集的自动快照已暂停':'';
}catch(e){if(seq!==previewSequence||!$('#policy-validation'))return;$('#policy-validation').className='validation invalid';$('#policy-validation').innerHTML=icon('warning')+esc(e.message);if($('#policy-preview'))$('#policy-preview').textContent='';}}

function backupFields(b){return `<div class="field-row"><label class="field"><span>频率</span><select name="cadence" id="backup-cadence"><option value="weekly" ${b.cadence==='weekly'?'selected':''}>每周</option><option value="monthly" ${b.cadence==='monthly'?'selected':''}>每月</option></select></label><label class="field"><span>时间</span><input name="at" type="time" value="${b.at}" required></label></div><div id="weekday-field" class="field" ${b.cadence==='monthly'?'hidden':''}><span>每周运行日期</span><div class="weekdays">${[1,2,3,4,5,6,0].map(d=>`<button type="button" class="weekday ${(b.weekdays||[]).includes(d)?'active':''}" data-action="weekday" data-day="${d}" aria-pressed="${(b.weekdays||[]).includes(d)}">${['日','一','二','三','四','五','六'][d]}</button>`).join('')}</div></div><label class="field" id="monthday-field" ${b.cadence!=='monthly'?'hidden':''}><span>每月运行日期</span><input name="month_day" type="number" min="1" max="31" value="${b.month_day||1}"></label><label class="field"><span>下次备份后的缓冲（分钟）</span><input name="grace_minutes" type="number" min="1" max="10080" value="${b.grace_minutes||60}" required><small class="field-note">备份专用快照的到期时间 = 下一次备份时间 + 此缓冲。两端使用相同期限；失败时继续保护旧基点，直到新备份成功。</small></label>`;}
function backupTargetFields(b,destination){const hosts=[...new Set(['local',...data.config.backups.map(job=>job.host)])];return `<div class="field-row"><label class="field"><span>目标主机</span><input name="host" id="backup-host" value="${esc(b.host||'local')}" list="backup-host-options" required placeholder="local、nas-pve 或 example.com:2222"><datalist id="backup-host-options">${hosts.map(host=>`<option value="${esc(host)}">${host==='local'?'本机':''}</option>`).join('')}</datalist><small class="field-note">本机填 local；SSH 别名使用运行账号的 ~/.ssh/config，也可填 domain:port。</small></label><label class="field short"><span>SSH 端口（可选）</span><input name="port" type="number" min="1" max="65535" value="${b.port||''}" placeholder="自动"><small class="field-note">留空使用 SSH 配置，未配置则为 22。</small></label></div><label class="field"><span>目标数据集</span><input name="destination" value="${esc(destination)}" placeholder="填写完整的目标数据集路径" required></label>`;}
function defaultBackupRules(){return {cadence:'weekly',weekdays:[1,5],month_day:1,at:'05:00',grace_minutes:60};}
function defaultBackup(){return {id:newID('backup'),name:'',host:'local',...defaultBackupRules(),enabled:true,datasets:[]};}
function readBackupRules(form){return {cadence:form.elements.cadence.value,at:form.elements.at.value,month_day:Number(form.elements.month_day.value),grace_minutes:Number(form.elements.grace_minutes.value),weekdays:all('.weekday.active',form).map(x=>+x.dataset.day)};}
function setBackupCadence(){const f=$('#dataset-backup-form')||$('#public-backup-form');$('#weekday-field').hidden=f.elements.cadence.value!=='weekly';$('#monthday-field').hidden=f.elements.cadence.value!=='monthly';}
function fillBackupRules(form,b){for(const key of ['cadence','at','month_day'])form.elements[key].value=b[key]|| (key==='month_day'?1:'');form.elements.grace_minutes.value=b.grace_minutes||60;all('.weekday',form).forEach(x=>{x.classList.toggle('active',(b.weekdays||[]).includes(+x.dataset.day));x.setAttribute('aria-pressed',x.classList.contains('active'));});setBackupCadence();}
function setBackupHost(){const f=$('#dataset-backup-form');if(!f)return;const local=f.elements.host.value.trim()==='local',removing=f.elements.mode.value==='none';f.elements.port.disabled=local||removing;f.elements.port.title=local?'本机复制不使用 SSH 端口':'';}
function openDatasetBackup(name,id,template){
 const choosing=!name,leaves=data.dataset_rows.filter(r=>r.is_leaf).map(r=>r.config.name).sort((a,b)=>a.localeCompare(b,'en',{numeric:true}));if(!name)name=leaves[0];if(!name){toast('没有可备份的叶子数据集',true);return;}
 const row=rowFor(name),entry=row?.backups.find(x=>x.task.id===id),original=entry?.task,copied=row?.backups.find(x=>x.task.id===template)?.task,b=structuredClone(original||copied||defaultBackup());
 if(!row.is_leaf&&!entry){toast('请为叶子数据集设置备份计划',true);return;}
 const mode=b.policy||'custom',destination=entry?.destination||(template?'':'backup/'+name);
 const body=`${choosing?`<label class="field"><span>源数据集</span><select name="source" id="backup-source">${leaves.map(path=>`<option value="${esc(path)}" ${path===name?'selected':''}>${esc(path)}</option>`).join('')}</select></label>`:`<p class="snapshot-dataset">${esc(name)}</p>`}${!row.is_leaf?'<p class="branch-note">此路径节点不执行备份，请移除旧计划并给叶子节点分别设置。</p>':''}<section id="backup-target-fields">${backupTargetFields(b,destination)}</section><label class="field"><span>时间策略</span><select name="mode" id="backup-mode"><option value="custom" ${mode==='custom'?'selected':''}>自定义时间</option>${data.config.backup_policies.map(x=>`<option value="${esc(x.id)}" ${mode===x.id?'selected':''}>公共 · ${esc(x.name)}</option>`).join('')}${entry?'<option value="none">移除此目的地</option>':''}</select></label><div id="backup-explanation" class="binding-explanation"></div><section id="backup-editor-fields"><fieldset id="backup-rule-fields">${backupFields(b)}</fieldset><label class="enabled-field"><span>启用此备份计划</span><input type="checkbox" name="enabled" ${!entry||original.enabled&&!entry.disabled?'checked':''}></label></section>`;
 showModal(drawer(id?'编辑数据集备份':'添加数据集备份',body,editorButtons(),'dataset-backup-form'),{type:'dataset-backup',name,originalId:id,backup:b,template});setBackupMode(false);
}
function setBackupMode(changed){const f=$('#dataset-backup-form'),mode=f.elements.mode.value,shared=!['none','custom'].includes(mode),removing=mode==='none';$('#backup-editor-fields').hidden=removing;$('#backup-rule-fields').disabled=mode!=='custom';for(const key of ['host','port','destination'])f.elements[key].disabled=removing;if(changed&&shared)fillBackupRules(f,data.config.backup_policies.find(b=>b.id===mode));setBackupCadence();setBackupHost();$('#backup-explanation').innerHTML=shared?`仅共用备份时间与缓冲；目标主机和数据集由当前目的地单独设置。<button type="button" class="text-button" data-action="backup-custom">复制为自定义时间</button>`:mode==='custom'?(modal.template?'已复制此目的地的主机和时间；填写新目标数据集后，另存一条备份计划。':'这组时间只用于当前目的地。'):'仅移除当前目的地，不影响其他数据集或目的地。';}
function setBackupBindingEnabled(task,binding,enabled){
 // A legacy grouped task can be globally paused. Enable only the selected destination.
 if(enabled&&!task.enabled){for(const sibling of task.datasets)sibling.disabled=true;task.enabled=true;}
 binding.disabled=!enabled;
}
function backupJobSettings(b){
 let host=b.host,port=b.port||0;const inline=host.match(/^(.*@)?(?:\[([^\]]+)\]|([^:]+))(?::(\d+))?$/);
 if(inline&&(!inline[4]||!port||port===Number(inline[4]))){host=(inline[1]||'')+(inline[2]||inline[3]);port=Number(inline[4])||port;}
 if(host==='local')port=0;
 return JSON.stringify({host,port,policy:b.policy||'',...(b.policy?{}:{cadence:b.cadence,weekdays:[...(b.weekdays||[])].sort(),month_day:b.month_day,at:b.at,grace_minutes:b.grace_minutes||60})});
}
async function saveDatasetBackup(e){e.preventDefault();try{
 const f=$('#dataset-backup-form'),mode=f.elements.mode.value,c=structuredClone(data.config),name=modal.name,previous=c.backups.find(b=>b.id===modal.originalId),enabled=f.elements.enabled.checked;if(!c.datasets.some(d=>d.name===name))c.datasets.push({name});
 if(mode==='none'){
  if(previous)previous.datasets=previous.datasets.filter(x=>x.source!==name);
  c.backups=c.backups.filter(b=>b.datasets.length);
 }else{
  const settings={host:f.elements.host.value.trim()};if(f.elements.port.value.trim())settings.port=Number(f.elements.port.value);
  if(mode==='custom')Object.assign(settings,readBackupRules(f));else settings.policy=mode;
  const binding={source:name,destination:f.elements.destination.value.trim()};
  if(previous&&(previous.datasets.length===1||backupJobSettings(previous)===backupJobSettings(settings))){
   for(const key of ['policy','cadence','weekdays','month_day','at','grace_minutes','private','port'])delete previous[key];
   Object.assign(previous,settings);
   if(mode==='custom'&&previous.datasets.length===1)previous.private=true;
   const old=previous.datasets.find(x=>x.source===name);Object.assign(old,binding);setBackupBindingEnabled(previous,old,enabled);
  }else{
   if(previous)previous.datasets=previous.datasets.filter(x=>x.source!==name);
   c.backups.push({...settings,id:newID('backup'),name:name.split('/').at(-1)+' 备份',...(mode==='custom'?{private:true}:{}),enabled,datasets:[binding]});
  }
 }
 await commit(c);closeModal();await refresh();toast('此目的地的备份已保存');
}catch(e){toast(e.message,true);}}
function openPublicBackup(id){const b=structuredClone(data.config.backup_policies.find(x=>x.id===id)||{id:newID('backup-policy'),name:'',...defaultBackupRules()}),uses=backupPolicyUses(b.id);showModal(drawer(id?'编辑公共备份时间策略':'添加公共备份时间策略',`<label class="field"><span>公共策略名称</span><input name="name" value="${esc(b.name)}" required></label>${backupFields(b)}<p class="field-note">${uses?'保存会更新 '+uses+' 个目的地的时间安排，目标主机与路径保持各自设置。':'任何主机上的备份目的地都可以引用这组时间安排。'}</p>`,editorButtons(id?`<button type="button" class="text-button danger" data-action="delete-public-backup" data-id="${esc(id)}" ${uses?'disabled title="请先切换引用者的时间策略"':''}>删除公共策略</button>`:''),'public-backup-form'),{type:'public-backup',backup:b});}
async function savePublicBackup(e){e.preventDefault();try{const f=$('#public-backup-form'),b={id:modal.backup.id,name:f.elements.name.value.trim(),...readBackupRules(f)},c=structuredClone(data.config),i=c.backup_policies.findIndex(x=>x.id===b.id);if(i<0)c.backup_policies.push(b);else c.backup_policies[i]=b;await commit(c);closeModal();await refresh();toast('公共备份时间策略已保存');}catch(e){toast(e.message,true);}}
function dialog(title,body,footer){return `<div class="dialog-shade" data-action="close"><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title"><header class="drawer-head"><h2 id="dialog-title">${title}</h2><button class="icon-button" data-action="close" aria-label="关闭">${icon('x')}</button></header><div class="dialog-body">${body}</div><footer class="drawer-foot">${footer}</footer></section></div>`;}
function snapshotExpiry(s){
 if(s.permanent)return `<span class="permanent-label">${icon('lock')}永久保留</span><small class="cell-note">名称包含 keep</small>`;
 if(s.retention_source==='unknown'||!s.expires_at||String(s.expires_at).startsWith('0001'))return '<span class="muted-label">无法确定</span>';
 const source=s.retention_source==='default'?'默认保留 30 天':'chrona:expires';
 return `<time datetime="${esc(s.expires_at)}" title="${esc(source)} · ${dateTime(s.expires_at,true)}">${dateTime(s.expires_at,true)}</time>${s.retention_source==='default'?'<small class="cell-note">默认保留 30 天</small>':''}`;
}
async function browse(name,index=1){const r=await api('/api/snapshots?'+new URLSearchParams({dataset:name,page:index}));showModal(dialog('数据集快照',`<p class="snapshot-dataset">${esc(name)}</p>${inspectionTabs(name,'snapshots')}${r.items.length?`<div class="table-scroll"><table><thead><tr><th>创建时间</th><th>快照</th><th>过期时间</th></tr></thead><tbody>${r.items.map(s=>`<tr><td>${dateTime(s.created_at,true)}</td><td class="mono">${esc(s.name.split('@')[1])}${s.held_by?.length?'<span class="hold" title="ZFS hold 保护中">受保护</span>':''}</td><td class="snapshot-expiry">${snapshotExpiry(s)}</td></tr>`).join('')}</tbody></table></div>`:empty('暂无快照','创建快照后，版本会显示在这里。')}`,`<small>${r.total} 张 · ${r.page} / ${Math.max(1,Math.ceil(r.total/20))} 页</small><div class="actions"><button class="button small" data-action="snapshot-page" data-page="${r.page-1}" ${r.page<=1?'disabled':''}>上一页</button><button class="button small" data-action="snapshot-page" data-page="${r.page+1}" ${r.page*20>=r.total?'disabled':''}>下一页</button><button class="button small primary" data-action="close">完成</button></div>`),{type:'browse',name});}
function inspectionTabs(name,active){return `<div class="inspection-tabs"><button class="${active==='snapshots'?'active':''}" data-action="browse" data-dataset="${esc(name)}">现有快照</button><button class="${active==='plan'?'active':''}" data-action="plan" data-dataset="${esc(name)}">快照计划</button></div>`;}
async function showPlan(name){const r=await api('/api/dataset-plan?'+new URLSearchParams({dataset:name}));const body=`<p class="snapshot-dataset">${esc(name)}</p>${inspectionTabs(name,'plan')}${r.snapshot?`<p class="plan-origin">${r.snapshot_source===name?'本级规则':'继承自 '+esc(r.snapshot_source)} · ${r.snapshot.enabled?'按当前配置预计':'自动快照已暂停'}</p>`:''}${r.tiers.length?`<div class="plan-tier-grid">${r.tiers.map((t,i)=>`<div class="tier-${TIERS.findIndex(([key])=>key===t.tier)}"><strong>${esc(t.label)} <small>保留 ${t.keep}</small></strong><span>${dateTime(t.next.run_at)}</span></div>`).join('')}</div><p class="field-note">各档下次命中时间。同一时刻触发时只创建一张快照，由最长档决定到期时间。</p><div class="table-scroll"><table class="plan-table"><thead><tr><th>预计执行时间</th><th>档位</th><th>到期时间</th></tr></thead><tbody>${r.events.map(e=>`<tr><td>${dateTime(e.run_at,true)}</td><td>${esc(e.label)}档${e.merged.length>1?`<small>合并 ${e.merged.length} 档</small>`:''}</td><td>${dateTime(e.expires_at,true)}</td></tr>`).join('')}</tbody></table></div>`:empty(r.snapshot?'自动快照已暂停':'尚未设置快照策略','配置并启用策略后，这里会显示后续计划。')}`;showModal(dialog('快照计划',body,`<small>预计未来 ${r.events.length} 次 · 依当前配置计算</small><button class="button small primary" data-action="close">完成</button>`),{type:'plan',name});}
async function saveConfig(){try{await api('/api/config',{method:'PUT',body:$('#config-yaml').value,yaml:true,headers:{'Content-Type':'application/yaml'}});configDirty=false;await refresh();toast('配置已保存');}catch(e){$('#config-validation').className='config-validation error-text';$('#config-validation').textContent=e.message;toast(e.message,true);}}

document.addEventListener('click',async e=>{const b=e.target.closest('[data-action]');if(!b||b.disabled)return;if(b.classList.contains('dialog-shade')&&e.target!==b)return;const a=b.dataset.action,name=b.dataset.dataset,id=b.dataset.id;try{
  if(a==='close'){closeModal();return;}
  if(a==='dataset-scope'){if(page==='snapshots'){datasetScope=b.dataset.scope;localStorage.setItem('chrona-dataset-scope',datasetScope);collapsedSnapshotDatasets.clear();}else{backupScope=b.dataset.scope;localStorage.setItem('chrona-backup-scope',backupScope);}renderPage();return;}
  if(a==='tree-all'){const names=snapshotBranches(datasetRows().filter(r=>!search||`${r.config.name} ${data.config.policies.find(p=>p.id===r.snapshot?.id)?.label||''} ${r.snapshot_source||''}`.toLowerCase().includes(search.toLowerCase())));if(names.some(name=>!collapsedSnapshotDatasets.has(name)))for(const name of names)collapsedSnapshotDatasets.add(name);else collapsedSnapshotDatasets.clear();renderPage();return;}
  if(a==='toggle-tree'){if(collapsedSnapshotDatasets.has(name))collapsedSnapshotDatasets.delete(name);else collapsedSnapshotDatasets.add(name);renderPage();return;}
  if(a==='new-dataset'||a==='edit-dataset'){openDataset(name);return;}
  if(a==='inherit-dataset'){$('#snapshot-mode').value='inherit';setSnapshotMode(false);requestPreview();return;}
  if(a==='make-custom'){$('#snapshot-mode').value='custom';setSnapshotMode(false);requestPreview();return;}
  if(a==='backup-custom'){$('#backup-mode').value='custom';setBackupMode(false);return;}
  if(a==='toggle-dataset'){const c=structuredClone(data.config),d=c.datasets.find(d=>d.name===name)||{name};if(!c.datasets.includes(d))c.datasets.push(d);d.disabled=!d.disabled;await commit(c);await refresh();return;}
  if(a==='run-dataset'){const r=rowFor(name);b.disabled=true;await api(`/api/policies/${encodeURIComponent(r.snapshot.id)}/run?dataset=${encodeURIComponent(name)}`,{method:'POST'});await refresh();toast('快照已创建');return;}
  if(a==='step'){const input=$(`#keep-${b.dataset.key}`);input.value=Math.max(0,Math.min(10000,Number(input.value||0)+Number(b.dataset.step)));requestPreview();return;}
  if(a==='new-policy'||a==='edit-policy'){openPublicPolicy(id);return;}
  if(a==='delete-public-policy'||a==='delete-public-backup'){const c=structuredClone(data.config);if(a==='delete-public-policy')c.policies=c.policies.filter(p=>p.id!==id);else c.backup_policies=c.backup_policies.filter(p=>p.id!==id);await commit(c);closeModal();await refresh();return;}
  if(a==='new-dataset-backup'){openDatasetBackup();return;}
  if(a==='dataset-backup'){openDatasetBackup(name,id,b.dataset.template);return;}
  if(a==='new-backup-policy'||a==='edit-backup-policy'){openPublicBackup(id);return;}
  if(a==='weekday'){b.classList.toggle('active');b.setAttribute('aria-pressed',b.classList.contains('active'));return;}
  if(a==='toggle-backup'){b.disabled=true;const c=structuredClone(data.config),task=c.backups.find(x=>x.id===id),binding=task?.datasets.find(x=>x.source===name);if(!binding)throw new Error('备份目的地已变更，请刷新后重试');const enabled=task.enabled&&!binding.disabled;setBackupBindingEnabled(task,binding,!enabled);await commit(c);await refresh();toast(enabled?'此目的地的自动备份已暂停':'此目的地的自动备份已启用');return;}
  if(a==='run-backup'){b.disabled=true;await api(`/api/backups/${encodeURIComponent(id)}/run?dataset=${encodeURIComponent(name)}`,{method:'POST'});await refresh();nextRefreshAt=0;toast('这个数据集的备份已开始');return;}
  if(a==='browse'){await browse(name);return;}
  if(a==='plan'){await showPlan(name);return;}
  if(a==='snapshot-page'){await browse(modal.name,+b.dataset.page);return;}
  if(a==='remove-dataset'){showModal(dialog('移除数据集管理？',`<p>${esc(name)}</p><p class="field-note">仅移除任务配置，已有数据和快照继续保留。</p>`,`<span></span><div class="actions"><button class="button" data-action="close">取消</button><button class="button danger-button" data-action="confirm-remove" data-dataset="${esc(name)}">确认移除</button></div>`),{type:'confirm'});return;}
  if(a==='confirm-remove'){const c=structuredClone(data.config);c.datasets=c.datasets.filter(d=>d.name!==name);for(const job of c.backups)job.datasets=job.datasets.filter(d=>d.source!==name);c.backups=c.backups.filter(job=>job.datasets.length);for(const job of c.backups)for(const binding of job.datasets)if(!c.datasets.some(d=>binding.source===d.name||binding.source.startsWith(d.name+'/')))c.datasets.push({name:binding.source});await commit(c);closeModal();await refresh();return;}
  if(a==='save-config'){await saveConfig();return;}if(a==='download'){download('chrona-config.yaml',$('#config-yaml').value);return;}
}catch(e){toast(e.message,true);if(b.isConnected)b.disabled=false;}});
document.addEventListener('submit',e=>{if(e.target.id==='dataset-form')saveDataset(e);else if(e.target.id==='policy-form')savePublicPolicy(e);else if(e.target.id==='dataset-backup-form')saveDatasetBackup(e);else if(e.target.id==='public-backup-form')savePublicBackup(e);});
document.addEventListener('input',e=>{const t=e.target;if(t.id==='backup-host')setBackupHost();if(t.closest('#dataset-form,#policy-form')){clearTimeout(previewTimer);previewTimer=setTimeout(requestPreview,180);}if(t.id==='search'){search=t.value;const pos=t.selectionStart;renderPage();$('#search').focus();$('#search').setSelectionRange(pos,pos);}if(t.id==='config-yaml')configDirty=true;});
document.addEventListener('change',e=>{if(e.target.id==='snapshot-mode'){setSnapshotMode(true);requestPreview();}if(e.target.id==='backup-source'){const f=$('#dataset-backup-form');if(f.elements.destination.value==='backup/'+modal.name)f.elements.destination.value='backup/'+e.target.value;modal.name=e.target.value;}if(e.target.id==='backup-mode')setBackupMode(true);if(e.target.id==='backup-cadence')setBackupCadence();});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&modal)closeModal();if(e.key==='Tab'&&e.target.classList.contains('code-editor')&&!e.shiftKey){e.preventDefault();e.target.setRangeText('  ',e.target.selectionStart,e.target.selectionEnd,'end');return;}if(e.key==='Tab'&&modal){const f=all('#modal-root button:not(:disabled),#modal-root input:not(:disabled),#modal-root select:not(:disabled),#modal-root textarea').filter(x=>x.getClientRects().length),first=f[0],last=f.at(-1);if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}}});
window.addEventListener('hashchange',()=>{collapsedSnapshotDatasets.clear();page=normalizePage(location.hash.slice(1));search='';if(modal)closeModal();shell();});window.addEventListener('beforeunload',e=>{if(configDirty){e.preventDefault();e.returnValue='';}});
let nextRefreshAt=0;async function poll(){try{if(Date.now()>=nextRefreshAt){
 const busy=data?.backups.some(x=>x.state.status==='running');
 if(!modal&&page!=='config'&&!['INPUT','TEXTAREA','SELECT'].includes(document.activeElement?.tagName)){await refresh();nextRefreshAt=Date.now()+(busy?1500:8000);}
}}catch{}setTimeout(poll,500);}
refresh().then(()=>setTimeout(poll,1200)).catch(e=>{$('#app').innerHTML=`<div class="boot"><p>${esc(e.message)}</p></div>`;});
