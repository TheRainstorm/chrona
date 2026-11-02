// Mutates an isolated Demo only. Never run against the production NAS.
const {chromium}=require(process.env.CHRONA_PLAYWRIGHT||'playwright');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base=process.env.CHRONA_TEST_URL||'http://127.0.0.1:8093';
const artifacts=path.resolve(__dirname,'../artifacts'),checks=[];
async function api(route,options){
 const response=await fetch(base+route,options);
 if(!response.ok)throw new Error(await response.text());
 return response.headers.get('content-type')?.includes('json')?response.json():response.text();
}
async function until(fn){
 const end=Date.now()+12000;
 while(Date.now()<end){if(await fn())return;await new Promise(resolve=>setTimeout(resolve,100));}
 throw new Error('Condition timed out');
}
function formattedTime(value){
 const parts=Object.fromEntries(new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hourCycle:'h23'}).formatToParts(new Date(value)).filter(part=>part.type!=='literal').map(part=>[part.type,part.value]));
 return `${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}
(async()=>{
 assert.equal((await api('/api/health')).mode,'demo');
 fs.mkdirSync(artifacts,{recursive:true});
 const initial=await api('/api/state'),config=structuredClone(initial.config),fine=config.policies.find(policy=>policy.id==='fine');
 config.datasets=[
  {name:'Saturn',policy:'fine'},
  {name:'Saturn/APP/Immich',snapshot:{start_at:fine.start_at,keep:{quarter_hourly:12,hourly:24,daily:7,weekly:4,monthly:12}}},
  {name:'Saturn/Disk1',snapshot_mode:'none'},
  {name:'rpool/data',policy:'ssd'},
  {name:'rpool/ROOT/pve-1',policy:'ssd'}
 ];
 const original=config.backups.find(task=>task.id==='archive-n5105');
 original.datasets.push({source:'Saturn/APP/Immich',destination:'rpool/backup/Immich-paused',disabled:true});
 config.backups.push({...structuredClone(original),id:'archive-secondary-test',name:'Archive 第二目的地',host:'local',port:22,datasets:[{source:'Saturn/Archive',destination:'rpool/backup/Archive-secondary'}]});
 await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(config)});
 const browser=await chromium.launch({executablePath:process.env.CHRONA_CHROME||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
 const context=await browser.newContext({viewport:{width:1500,height:1050},locale:'zh-CN',timezoneId:'Asia/Shanghai',reducedMotion:'reduce'});
 const page=await context.newPage(),errors=[],external=[],runs=[],configWrites=[];
 page.on('pageerror',error=>errors.push(error.message));
 page.on('request',request=>{
  if(!request.url().startsWith(base)&&!request.url().startsWith('data:'))external.push(request.url());
  if(request.method()==='POST'&&new URL(request.url()).pathname.endsWith('/run'))runs.push(new URL(request.url()));
  if(request.method()==='PUT'&&new URL(request.url()).pathname==='/api/config')configWrites.push(request.postDataJSON());
 });
 const snapshot=name=>page.locator(`[data-dataset-row="${name}"]`);
 const backup=(name,id)=>page.locator(`[data-backup-dataset="${name}"]${id===undefined?'':`[data-backup-id="${id}"]`}`);
 const state=()=>api('/api/state');
 const resolved=async name=>(await state()).dataset_rows.find(row=>row.config.name===name);
 const binding=async(name,id)=>(await resolved(name)).backups.find(entry=>entry.task.id===id);
 const timingKeys=['cadence','weekdays','month_day','at','grace_minutes'];
 const cleanPolicy=policy=>{for(const key of ['host','port','datasets','destination','enabled','private'])assert.equal(Object.hasOwn(policy,key),false,'Timing policies must not own '+key);};
 const noBackupKeep=task=>{assert.equal(Object.hasOwn(task,'keep_weekly'),false);assert.equal(Object.hasOwn(task,'keep_monthly'),false);};
 const backupFields=async()=>{assert.equal(await page.locator('[name=keep_weekly],[name=keep_monthly]').count(),0);assert(await page.locator('[name=grace_minutes]').isEditable());};
 const backupExpiry=async(name,id)=>{
  const entry=await binding(name,id),transfer=entry.state.transfers[name],expected=Date.parse(transfer.next_backup_at)+(entry.task.grace_minutes||60)*60000;
  assert(entry.replica);assert.equal(Date.parse(entry.replica.expires_at),expected,'Receiver expiry must be exactly the next backup plus its buffer');assert.equal(Date.parse(transfer.protected_until),expected);
  const history=await api('/api/snapshots?'+new URLSearchParams({dataset:name,q:entry.replica.source_snapshot})),source=history.items.find(snapshot=>snapshot.name===entry.replica.source_snapshot);
  assert(source,'The source common snapshot must exist');assert.equal(Date.parse(source.expires_at),expected,'Ordinary snapshot retention must not extend the backup source expiry');assert.equal(source.name.split('@')[1],entry.replica.name.split('@')[1]);
  return entry;
 };
 const overview=async kind=>{
  const current=await state(),value=async key=>page.locator(`.overview-card[data-stat="${key}"]`).getAttribute('data-value');assert.equal(await page.locator('.overview-card').count(),4);
  if(kind==='snapshots'){
   assert.equal(Number(await value('snapshot-datasets')),current.dataset_rows.filter(row=>row.snapshot?.enabled).length);assert.equal(Number(await value('snapshot-count')),current.stats.snapshots);assert.equal(Number(await value('snapshot-space')),current.stats.snapshot_bytes);
   const next=current.dataset_rows.map(row=>row.next?.run_at).filter(Boolean).sort((a,b)=>new Date(a)-new Date(b))[0]||'';assert.equal(await value('snapshot-next'),next);
  }else{
   const entries=current.dataset_rows.filter(row=>row.is_leaf).flatMap(row=>row.backups),enabled=entries.filter(entry=>entry.task.enabled&&!entry.disabled),jobs=[...new Map(entries.map(entry=>[entry.task.id,entry])).values()];
   assert.equal(Number(await value('backup-targets')),entries.length);assert.equal(Number(await value('backup-enabled')),enabled.length);assert.equal(Number(await value('backup-health')),jobs.filter(entry=>entry.state.error||entry.state.warning).length);
   const next=enabled.map(entry=>entry.next).filter(Boolean).sort((a,b)=>new Date(a)-new Date(b))[0]||'';assert.equal(await value('backup-next'),next);
  }
 };

 const edit=async name=>{await snapshot(name).locator('.row-actions [data-action=edit-dataset]').click();await page.locator('#dataset-form').waitFor();};
 const save=async form=>{await page.locator('#save-editor').click();await page.locator(form).waitFor({state:'detached'});};
 const zeroCounts=async()=>{
  assert.deepEqual(await page.locator('.keep-grid input').evaluateAll(inputs=>inputs.map(input=>Number(input.value))),[0,0,0,0,0]);
  await until(async()=>(await page.locator('.keep-grid small').allTextContents()).every(text=>text==='未启用'));
 };
 const search=async value=>{await page.locator('#search').fill(value);};
 const check=async(name,fn)=>{await fn();checks.push(name);console.log('PASS',name);};
 try{
  await check('snapshot rows show actual last snapshot time, full paths, named rules, retention and next time',async()=>{
   await page.goto(base,{waitUntil:'networkidle'});
   assert.deepEqual(await page.locator('nav a').allTextContents(),['快照','备份','配置']);
   assert.equal(await page.locator('[data-dataset-row]:visible').count(),10);
   for(const row of await page.locator('[data-dataset-row]').all())assert.equal(await row.locator('.dataset-path').innerText(),await row.getAttribute('data-dataset-row'));
   const row=snapshot('Saturn/Archive'),entry=await resolved('Saturn/Archive');
   assert((await row.locator('.snapshot-policy').innerText()).includes('精细保留'));
   for(const label of ['15分','小时','天','周','月'])assert((await row.locator('.snapshot-keep').innerText()).includes(label));
   for(const count of Object.values(entry.snapshot.keep))assert((await row.locator('.snapshot-keep').innerText()).includes(String(count)));
   assert((await row.locator('.snapshot-next').innerText()).includes(formattedTime(entry.next.run_at)));
   assert((await row.locator('.snapshot-last').innerText()).includes(formattedTime(entry.latest_snapshot.created_at)));
   const empty=(await state()).dataset_rows.find(row=>row.snapshot&&!row.snapshot_count);if(empty){assert.equal(await snapshot(empty.config.name).locator('.snapshot-last time').count(),0);assert((await snapshot(empty.config.name).locator('.snapshot-last').innerText()).includes('尚无快照'));}
   assert.equal(await page.locator('[data-action=backup-details],[data-action=snapshot-settings]').count(),0);
   await overview('snapshots');await page.screenshot({path:path.join(artifacts,'list-snapshots-desktop.png'),fullPage:true});
  });
  await check('optional snapshot folds hide children, then filters and reload show full paths expanded',async()=>{
   const fold=snapshot('Saturn').locator('[data-action=toggle-tree]');
   assert.equal(await fold.getAttribute('aria-expanded'),'true');await fold.click();
   assert.equal(await snapshot('Saturn/APP/Immich').isVisible(),false);
   await snapshot('Saturn').locator('[data-action=toggle-tree]').click();assert(await snapshot('Saturn/APP/Immich').isVisible());
   await page.getByRole('button',{name:'全部收起',exact:true}).click();assert.equal(await snapshot('Saturn/APP/Immich').isVisible(),false);
   await page.getByRole('button',{name:'全部展开',exact:true}).click();assert.equal(await page.locator('[data-dataset-row]:visible').count(),10);
   await page.getByRole('button',{name:'仅本级设置',exact:true}).click();assert.equal(await page.locator('[data-dataset-row]:visible').count(),5);assert.equal(await snapshot('Saturn/Archive').count(),0);
   await page.getByRole('button',{name:'全部数据集',exact:true}).click();assert.equal(await page.locator('[data-dataset-row]:visible').count(),13);assert.equal((await resolved('rpool')).snapshot_count,0);assert.equal(await snapshot('rpool').locator('.snapshot-last time').count(),0);
   for(const row of (await state()).dataset_rows){const local=!!(row.config.policy||row.config.snapshot||row.config.snapshot_mode==='none');assert.equal(await snapshot(row.config.name).evaluate(element=>element.classList.contains('local-rule')),local);assert.equal(await snapshot(row.config.name).locator('.local-rule-badge').count(),local?1:0);}await overview('snapshots');
   await edit('rpool/ROOT');await page.locator('#snapshot-mode').selectOption('ssd');await save('#dataset-form');assert.equal((await resolved('rpool/ROOT')).config.policy,'ssd');
   await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('[data-dataset-row]:visible').count(),13);
  });
  await check('snapshot history and plan open directly; enable and manual snapshot operate from the row',async()=>{
   await snapshot('Saturn/Archive').locator('[data-action=browse]').click();await page.locator('.dialog').waitFor();assert.equal(await page.locator('.dialog tbody tr').count(),20);
   await page.locator('.inspection-tabs [data-action=plan]').click();await page.locator('.plan-table').waitFor();assert.equal(await page.locator('.plan-tier-grid>div').count(),5);assert.equal(await page.locator('.plan-table tbody tr').count(),16);await page.keyboard.press('Escape');
   await snapshot('Saturn/Archive').locator('[data-action=plan]').click();await page.locator('.plan-table').waitFor();await page.keyboard.press('Escape');
   await snapshot('Saturn/Archive').locator('[data-action=toggle-dataset]').click();await until(async()=>(await snapshot('Saturn/Archive').locator('[data-action=toggle-dataset]').getAttribute('aria-checked'))==='false');assert.equal((await resolved('Saturn/Archive')).snapshot.enabled,false);assert((await snapshot('Saturn/Archive').locator('.snapshot-next').innerText()).includes('暂停'));
   await snapshot('Saturn/Archive').locator('[data-action=toggle-dataset]').click();await until(async()=>(await snapshot('Saturn/Archive').locator('[data-action=toggle-dataset]').getAttribute('aria-checked'))==='true');
   const before=(await resolved('Saturn/Archive')).snapshot_count;await snapshot('Saturn/Archive').locator('[data-action=run-dataset]').click();await until(async()=>(await resolved('Saturn/Archive')).snapshot_count>before);await until(async()=>await snapshot('Saturn/Archive').locator('[data-action=run-dataset]').isEnabled());const latest=(await resolved('Saturn/Archive')).latest_snapshot;assert((await snapshot('Saturn/Archive').locator('.snapshot-last').innerText()).includes(formattedTime(latest.created_at)));assert.equal(await snapshot('Saturn/Archive').evaluate(element=>element.classList.contains('local-rule')),false,'Pause-only config must not be highlighted as a local rule');
  });
  if(process.env.CHRONA_RETENTION_FIXTURE_DATASET)await check('legacy snapshot history shows creation plus 30 days, while mixed-case keep is permanent',async()=>{
   const name=process.env.CHRONA_RETENTION_FIXTURE_DATASET,response=await api('/api/snapshots?'+new URLSearchParams({dataset:name,page:1}));
   const legacy=response.items.find(item=>item.name.endsWith('@qa_legacy_30days')),permanent=response.items.find(item=>item.name.endsWith('@qa_KeEp_forever'));
   assert(legacy&&permanent,'Seed both retention fixtures in isolated Demo state before launch');assert.equal(legacy.retention_source,'default');assert.equal(legacy.permanent,false);assert.equal(new Date(legacy.expires_at)-new Date(legacy.created_at),30*24*60*60*1000);assert.equal(permanent.retention_source,'keep');assert.equal(permanent.permanent,true);
   await snapshot(name).locator('[data-action=browse]').click();await page.locator('.dialog table').waitFor();
   const legacyRow=page.locator('.dialog tbody tr').filter({hasText:'qa_legacy_30days'}),keepRow=page.locator('.dialog tbody tr').filter({hasText:'qa_KeEp_forever'});
   assert((await legacyRow.locator('td').nth(2).innerText()).includes(formattedTime(legacy.expires_at)));assert((await legacyRow.locator('td').nth(2).innerText()).replace(/\s/g,'').includes('默认保留30天'));assert((await keepRow.locator('td').nth(2).innerText()).includes('永久保留'));assert.equal(await keepRow.locator('td').nth(2).locator('time').count(),0);
   await page.screenshot({path:path.join(artifacts,'retention-history.png')});await page.keyboard.press('Escape');
  });
  await check('new snapshot policies default to zero while existing and copied counts remain intact',async()=>{
   await page.locator('.public-section').evaluate(element=>element.open=true);const previousPublic=await page.locator('.public-section').elementHandle();await page.waitForFunction(element=>!element.isConnected,previousPublic,{timeout:12000});assert(await page.locator('.public-section').evaluate(element=>element.open));await page.locator('[data-action=new-policy]').click();await zeroCounts();await page.keyboard.press('Escape');
   await edit('rpool');await page.locator('#snapshot-mode').selectOption('custom');await zeroCounts();await page.keyboard.press('Escape');
   await edit('Saturn/Archive');await until(async()=>(await page.locator('#binding-explanation').innerText()).includes('继承自 Saturn'));await page.locator('#snapshot-mode').selectOption('custom');await zeroCounts();await page.keyboard.press('Escape');
   await edit('Saturn');await page.locator('#snapshot-mode').selectOption('custom');await zeroCounts();await page.keyboard.press('Escape');
   await edit('Saturn/APP/Immich');assert.deepEqual(await page.locator('.keep-grid input').evaluateAll(inputs=>inputs.map(input=>Number(input.value))),[12,24,7,4,12]);await page.keyboard.press('Escape');
   await edit('Saturn/APP/Seafile');await until(async()=>(await page.locator('#binding-explanation').innerText()).includes('继承自 Saturn'));await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();await page.locator('#keep-hourly').fill('36');await save('#dataset-form');
   await page.locator('.public-section').evaluate(element=>element.open=true);await page.getByRole('button',{name:'编辑公共策略 精细保留',exact:true}).click();await page.locator('#keep-hourly').fill('120');await save('#policy-form');assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,36);assert.equal((await resolved('Saturn/Archive')).snapshot.keep.hourly,120);
   await edit('Saturn/APP/Seafile');await page.getByRole('button',{name:'恢复继承',exact:true}).click();await save('#dataset-form');assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,120);
  });
  await check('backups are sorted flat rows, one per destination, with complete paths, rule and per-source times',async()=>{
   await page.getByRole('link',{name:'备份',exact:true}).click();await page.getByRole('heading',{name:'数据集备份',exact:true}).waitFor();
   assert.equal(await page.locator('[data-backup-dataset]').count(),5);assert.equal(await backup('Saturn/Archive').count(),2);
   assert.equal(await page.locator('.dataset-panel details,[data-action=backup-details],[data-action=tree-all]').count(),0);
   assert(await page.locator('.backup-table tbody tr').evaluateAll(rows=>rows.every(row=>[...row.cells].every(cell=>Math.abs(cell.getBoundingClientRect().top-row.getBoundingClientRect().top)<=1&&Math.abs(cell.getBoundingClientRect().bottom-row.getBoundingClientRect().bottom)<=1))),'Backup columns must share each row’s boundaries');
   const sources=await page.locator('[data-backup-dataset]').evaluateAll(rows=>rows.map(row=>row.dataset.backupDataset));assert.deepEqual(sources,[...sources].sort((a,b)=>a.localeCompare(b,'en',{numeric:true})));
   const destinations=await backup('Saturn/Archive').locator('.backup-destination .dataset-path').allTextContents();assert.deepEqual(destinations,[...destinations].sort((a,b)=>a.localeCompare(b,'en',{numeric:true})));
   for(const row of await page.locator('[data-backup-dataset]').all()){
    const name=await row.getAttribute('data-backup-dataset'),id=await row.getAttribute('data-backup-id'),entry=await binding(name,id);
    assert.equal(await row.locator('.backup-source .dataset-path').innerText(),name);
    assert((await row.locator('.backup-destination').innerText()).includes(entry.destination));assert((await row.locator('.backup-destination').innerText()).includes(entry.task.host));assert((await row.locator('.backup-rule').innerText()).includes(entry.task.policy_name||entry.task.name));
    const completed=entry.state.completed?.[name];if(completed)assert((await row.locator('.backup-last').innerText()).includes(formattedTime(completed)));else assert(!/\d{2}-\d{2}/.test(await row.locator('.backup-last').innerText()));
    if(entry.next)assert((await row.locator('.backup-next').innerText()).includes(formattedTime(entry.next)));
   }
   const paused=backup('Saturn/APP/Immich','archive-n5105');assert((await paused.locator('.backup-next').innerText()).includes('暂停'));assert(await paused.locator('[data-action=run-backup]').isDisabled());
   assert(!/\d{2}-\d{2}/.test(await paused.locator('.backup-last').innerText()));
   await overview('backups');await page.screenshot({path:path.join(artifacts,'list-backups-desktop.png'),fullPage:true});
  });
  await check('per-destination backup switches persist without enabling or pausing siblings',async()=>{
   const row=backup('Saturn/Archive','archive-n5105'),toggle=row.locator('.row-actions [data-action=toggle-backup]'),other=backup('Saturn/Archive','archive-secondary-test'),paused=backup('Saturn/APP/Immich','archive-n5105');
   assert.equal(await toggle.getAttribute('aria-checked'),'true');await toggle.click();await until(async()=>(await toggle.getAttribute('aria-checked'))==='false');
   assert((await row.locator('.backup-next').innerText()).includes('暂停'));assert.equal(await row.locator('.backup-next time').count(),0);assert(await row.locator('[data-action=run-backup]').isDisabled());
   let current=await binding('Saturn/Archive','archive-n5105');assert.equal(current.disabled,true);assert.equal(current.task.enabled,true);assert.equal((await binding('Saturn/APP/Immich','archive-n5105')).disabled,true);assert.equal(await other.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'true');
   await page.reload({waitUntil:'networkidle'});assert.equal(await toggle.getAttribute('aria-checked'),'false');assert.equal((await binding('Saturn/Archive','archive-n5105')).disabled,true);
   await toggle.click();await until(async()=>(await toggle.getAttribute('aria-checked'))==='true');assert(await row.locator('.backup-next time').isVisible());assert(await row.locator('[data-action=run-backup]').isEnabled());assert.equal((await binding('Saturn/APP/Immich','archive-n5105')).disabled,true);
   await paused.locator('[data-action=toggle-backup]').click();await until(async()=>(await paused.locator('[data-action=toggle-backup]').getAttribute('aria-checked'))==='true');assert.equal(await toggle.getAttribute('aria-checked'),'true');assert.equal(await other.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'true');
   await paused.locator('[data-action=toggle-backup]').click();await until(async()=>(await paused.locator('[data-action=toggle-backup]').getAttribute('aria-checked'))==='false');
  });
  await check('enabling one destination of a globally disabled shared rule keeps its sibling off',async()=>{
   const current=(await state()).config,template=current.backups.find(task=>task.id==='archive-n5105');current.backups.push({...structuredClone(template),id:'disabled-rule-test',name:'暂停的公共规则',enabled:false,datasets:[{source:'Saturn/Archive',destination:'rpool/backup/DisabledArchive'},{source:'Saturn/APP/Seafile',destination:'rpool/backup/DisabledSeafile'}]});await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(current)});await page.reload({waitUntil:'networkidle'});
   const row=backup('Saturn/Archive','disabled-rule-test'),sibling=backup('Saturn/APP/Seafile','disabled-rule-test');assert.equal(await row.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'false');assert.equal(await sibling.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'false');
   await row.locator('[data-action=toggle-backup]').click();await until(async()=>(await row.locator('[data-action=toggle-backup]').getAttribute('aria-checked'))==='true');
   assert.equal((await binding('Saturn/Archive','disabled-rule-test')).task.enabled,true);assert.equal((await binding('Saturn/APP/Seafile','disabled-rule-test')).disabled,true);assert.equal(await sibling.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'false');assert.equal(await sibling.locator('.backup-next time').count(),0);assert(await sibling.locator('[data-action=run-backup]').isDisabled());
   await page.reload({waitUntil:'networkidle'});assert.equal(await row.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'true');assert.equal(await sibling.locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'false');
   const cleanup=(await state()).config;cleanup.backups=cleanup.backups.filter(task=>task.id!=='disabled-rule-test');await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(cleanup)});await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('[data-backup-dataset]').count(),5);
  });
  await check('backup search matches exact destination and rule without aggregating another destination',async()=>{
   await search('Archive-secondary');assert.equal(await page.locator('[data-backup-dataset]').count(),1);assert.equal(await backup('Saturn/Archive','archive-secondary-test').count(),1);
   await search('local');assert.equal(await page.locator('[data-backup-dataset]').count(),1);
   await search('Saturn/Archive');assert.equal(await backup('Saturn/Archive').count(),2);await search('');
  });
  await check('public backup rules save weekly or monthly frequency and editable buffer without historical keep counts',async()=>{
   await page.locator('.public-section').evaluate(element=>element.open=true);await page.locator('[data-action=new-backup-policy]').click();await page.locator('#public-backup-form').waitFor();await backupFields();assert.equal(await page.locator('[name=grace_minutes]').inputValue(),'60');
   assert.equal(await page.locator('[name=host],[name=port],[name=destination]').count(),0);await page.screenshot({path:path.join(artifacts,'backup-time-policy-editor.png')});await page.locator('[name=name]').fill('Browser cadence test');await page.locator('#backup-cadence').selectOption('monthly');assert(await page.locator('#monthday-field').isVisible());assert(await page.locator('#weekday-field').isHidden());await page.locator('[name=month_day]').fill('31');await page.locator('[name=grace_minutes]').fill('90');await save('#public-backup-form');
   let created=(await state()).config.backup_policies.find(task=>task.name==='Browser cadence test');assert(created);assert.equal(created.cadence,'monthly');assert.equal(created.month_day,31);assert.equal(created.grace_minutes,90);noBackupKeep(created);cleanPolicy(created);
   await page.locator(`[data-public-backup="${created.id}"] [data-action=edit-backup-policy]`).click();await page.locator('#public-backup-form').waitFor();await backupFields();assert.equal(await page.locator('[name=grace_minutes]').inputValue(),'90');await page.locator('#backup-cadence').selectOption('weekly');assert(await page.locator('#weekday-field').isVisible());assert(await page.locator('#monthday-field').isHidden());
   for(const day of await page.locator('.weekday').all())if((await day.getAttribute('aria-pressed')==='true')!==(await day.getAttribute('data-day')==='3'))await day.click();await page.locator('[name=grace_minutes]').fill('37');await save('#public-backup-form');
   created=(await state()).config.backup_policies.find(task=>task.id===created.id);assert.equal(created.cadence,'weekly');assert.deepEqual(created.weekdays,[3]);assert.equal(created.grace_minutes,37);noBackupKeep(created);cleanPolicy(created);
   await page.locator(`[data-public-backup="${created.id}"] [data-action=edit-backup-policy]`).click();await page.locator('[data-action=delete-public-backup]').click();await page.locator('#public-backup-form').waitFor({state:'detached'});
  });
  await check('one public timing policy serves independent local and SSH targets, with safe shared edits and deletion',async()=>{
   const saved=(await state()).config,remote=await binding('Saturn/Archive','archive-n5105'),local=await binding('Saturn/Archive','archive-secondary-test'),policy=saved.backup_policies.find(policy=>policy.id===remote.task.policy);
   assert(policy);assert.equal(local.task.policy,policy.id);assert.equal(remote.task.host,'n5105');assert.equal(local.task.host,'local');assert.equal(remote.next,local.next);cleanPolicy(policy);
   await backup('Saturn/Archive','archive-secondary-test').locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#dataset-backup-form').waitFor();assert.equal(await page.locator('#backup-mode').inputValue(),policy.id);assert(await page.locator('[name=host]').isEditable());assert(await page.locator('[name=destination]').isEditable());assert(await page.locator('[name=port]').isDisabled());assert(await page.locator('[name=cadence]').isDisabled());await page.screenshot({path:path.join(artifacts,'backup-target-editor.png')});
   await page.locator('[name=host]').fill('qa-receiver');await page.locator('[name=host]').blur();assert(await page.locator('[name=port]').isEditable());await page.locator('[name=port]').fill('2222');await save('#dataset-backup-form');
   const edited=await binding('Saturn/Archive','archive-secondary-test');assert.equal(edited.task.host,'qa-receiver');assert.equal(edited.task.port,2222);assert.equal(edited.task.policy,policy.id);assert.equal((await binding('Saturn/Archive','archive-n5105')).task.host,remote.task.host);assert.deepEqual((await state()).config.backup_policies,saved.backup_policies);
   await page.locator('.public-section').evaluate(element=>element.open=true);await page.locator(`[data-public-backup="${policy.id}"] [data-action=edit-backup-policy]`).click();await page.locator('#public-backup-form').waitFor();assert.equal(await page.locator('[name=host],[name=port],[name=destination]').count(),0);assert(await page.locator('[data-action=delete-public-backup]').isDisabled());await page.locator('[name=at]').fill('06:23');await page.locator('[name=grace_minutes]').fill('67');await save('#public-backup-form');
   for(const id of ['archive-n5105','archive-secondary-test']){const entry=await binding('Saturn/Archive',id);assert.equal(entry.task.at,'06:23');assert.equal(entry.task.grace_minutes,67);assert.equal(entry.task.policy,policy.id);}assert.equal((await binding('Saturn/Archive','archive-secondary-test')).task.host,'qa-receiver');assert.equal((await binding('Saturn/Archive','archive-n5105')).task.host,'n5105');assert.equal((await binding('Saturn/Archive','archive-n5105')).next,(await binding('Saturn/Archive','archive-secondary-test')).next);
   const invalid=(await state()).config;invalid.backup_policies=invalid.backup_policies.filter(entry=>entry.id!==policy.id);const rejected=await fetch(base+'/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(invalid)});assert.equal(rejected.status,422);assert((await state()).config.backup_policies.some(entry=>entry.id===policy.id));
   await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(saved)});await page.reload({waitUntil:'networkidle'});
  });
  await check('editing one legacy grouped destination preserves its sibling identity, pause and backup bases',async()=>{
   const saved=(await state()).config,unchanged=await binding('Saturn/Archive','archive-n5105'),selected=await binding('Saturn/APP/Immich','archive-n5105');assert.equal(selected.disabled,true);
   await backup('Saturn/APP/Immich','archive-n5105').locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#dataset-backup-form').waitFor();assert.equal(await page.locator('[name=enabled]').isChecked(),false);await page.locator('[name=host]').fill('qa-split-receiver');await save('#dataset-backup-form');
   const split=(await resolved('Saturn/APP/Immich')).backups.find(entry=>entry.destination===selected.destination),sibling=await binding('Saturn/Archive','archive-n5105');assert(split);assert.notEqual(split.task.id,selected.task.id);assert.equal(split.task.host,'qa-split-receiver');assert.equal(split.task.policy,selected.task.policy);assert(!split.task.enabled||split.disabled);assert.equal(sibling.task.id,unchanged.task.id);assert.equal(sibling.task.host,unchanged.task.host);assert.equal(sibling.destination,unchanged.destination);assert.deepEqual(sibling.state.bases,unchanged.state.bases);assert.deepEqual(sibling.state.completed,unchanged.state.completed);assert.deepEqual((await state()).config.backup_policies,saved.backup_policies);
   await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(saved)});await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('[data-backup-dataset]').count(),5);
  });
  await check('adding another destination copies its row rule into an independent plan',async()=>{
   const before=await binding('Saturn/Archive','archive-secondary-test');
   await backup('Saturn/Archive','archive-secondary-test').locator('.row-actions [data-action=dataset-backup]:not([data-id])').click();await page.locator('#dataset-backup-form').waitFor();assert.equal(await page.locator('#backup-mode').inputValue(),before.task.policy||'custom');assert.equal(await page.locator('[name=destination]').inputValue(),'');await page.locator('#backup-mode').selectOption('custom');
   await backupFields();for(const field of ['host','port','cadence','at','month_day'])assert.equal(await page.locator(`[name=${field}]`).inputValue(),String(before.task[field]));assert.equal(await page.locator('[name=grace_minutes]').inputValue(),String(before.task.grace_minutes||60));
   await page.locator('#backup-cadence').selectOption('monthly');assert(await page.locator('#monthday-field').isVisible());assert(await page.locator('#weekday-field').isHidden());await page.locator('[name=month_day]').fill('28');await page.locator('[name=grace_minutes]').fill('23');
   await page.locator('[name=destination]').fill('rpool/backup/Archive-third-test');await save('#dataset-backup-form');
   const created=(await resolved('Saturn/Archive')).backups.find(entry=>entry.destination==='rpool/backup/Archive-third-test');assert(!created.task.policy);assert.notEqual(created.task.id,before.task.id);assert.equal(await backup('Saturn/Archive').count(),3);assert.equal((await binding('Saturn/Archive','archive-secondary-test')).destination,before.destination);assert.equal(created.task.cadence,'monthly');assert.equal(created.task.month_day,28);assert.equal(created.task.grace_minutes,23);noBackupKeep(created.task);
   await backup('Saturn/Archive',created.task.id).locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#dataset-backup-form').waitFor();await backupFields();assert.equal(await page.locator('[name=grace_minutes]').inputValue(),'23');await page.locator('#backup-cadence').selectOption('weekly');assert(await page.locator('#weekday-field').isVisible());await save('#dataset-backup-form');assert.equal((await binding('Saturn/Archive',created.task.id)).task.cadence,'weekly');
   const privateRow=backup('Saturn/Archive',created.task.id);await privateRow.locator('[data-action=toggle-backup]').click();await until(async()=>(await privateRow.locator('[data-action=toggle-backup]').getAttribute('aria-checked'))==='false');assert.equal(await backup('Saturn/Archive','archive-secondary-test').locator('[data-action=toggle-backup]').getAttribute('aria-checked'),'true');await privateRow.locator('[data-action=toggle-backup]').click();await until(async()=>(await privateRow.locator('[data-action=toggle-backup]').getAttribute('aria-checked'))==='true');
   await backup('Saturn/Archive',created.task.id).locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#backup-mode').selectOption('none');await save('#dataset-backup-form');assert.equal(await backup('Saturn/Archive').count(),2);assert.equal((await resolved('Saturn/Archive')).backups.length,2);
  });
  await check('inline edit and run affect only their destination, and completed rows update automatically',async()=>{
   const originalBefore=await binding('Saturn/Archive','archive-n5105');
   await backup('Saturn/Archive','archive-secondary-test').locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#dataset-backup-form').waitFor();assert.equal(await page.locator('#backup-mode').inputValue(),'archive-n5105');assert.equal(await page.locator('#backup-mode option[value=inherit]').count(),0);
   await page.locator('[name=destination]').fill('rpool/backup/Archive-secondary-edited');await save('#dataset-backup-form');assert.equal((await binding('Saturn/Archive','archive-secondary-test')).destination,'rpool/backup/Archive-secondary-edited');assert.equal((await binding('Saturn/Archive','archive-n5105')).destination,originalBefore.destination);
   await backup('Saturn/Archive','archive-secondary-test').locator('[data-action=run-backup]').click();await until(async()=>await backup('Saturn/Archive','archive-secondary-test').locator('[data-action=toggle-backup]').isDisabled());assert(await backup('Saturn/Archive','archive-n5105').locator('[data-action=toggle-backup]').isDisabled());await until(async()=>!!(await binding('Saturn/Archive','archive-secondary-test')).state.completed?.['Saturn/Archive']);
   const entry=await backupExpiry('Saturn/Archive','archive-secondary-test');assert.equal((await binding('Saturn/Archive','archive-n5105')).state.completed['Saturn/Archive'],originalBefore.state.completed['Saturn/Archive']);
   const request=runs.at(-1);assert.equal(request.pathname,'/api/backups/archive-secondary-test/run');assert.equal(request.searchParams.get('dataset'),'Saturn/Archive');
   await until(async()=>(await backup('Saturn/Archive','archive-secondary-test').locator('.backup-last').innerText()).includes(formattedTime(entry.state.completed['Saturn/Archive'])));assert(await backup('Saturn/Archive','archive-secondary-test').locator('[data-action=run-backup]').isEnabled());assert(await backup('Saturn/Archive','archive-secondary-test').locator('[data-action=toggle-backup]').isEnabled());assert(await backup('Saturn/Archive','archive-n5105').locator('[data-action=toggle-backup]').isEnabled());
  });
  await check('monthly backups keep both common snapshots only until next run plus buffer even under a long source policy',async()=>{
   const saved=(await state()).config,current=structuredClone(saved),name='Saturn/Archive',id='archive-secondary-test';
   current.datasets=current.datasets.filter(dataset=>dataset.name!==name);current.datasets.push({name,snapshot:{start_at:fine.start_at,keep:{quarter_hourly:0,hourly:0,daily:0,weekly:0,monthly:12}}});const task=current.backups.find(task=>task.id===id);delete task.policy;Object.assign(task,{private:true,cadence:'monthly',weekdays:[1],month_day:31,at:'05:17',grace_minutes:17});
   await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(current)});const before=await binding(name,id),policy=(await resolved(name)).snapshot;
   await api(`/api/policies/${encodeURIComponent(policy.id)}/run?dataset=${encodeURIComponent(name)}`,{method:'POST'});const ordinary=(await resolved(name)).latest_snapshot;assert(Date.parse(ordinary.expires_at)>Date.parse(before.next)+17*60000,'Fixture must have ordinary retention beyond the next monthly backup');
   await page.reload({waitUntil:'networkidle'});await backup(name,id).locator('[data-action=run-backup]').click();await until(async()=>(await binding(name,id)).state.completed[name]!==before.state.completed[name]);const entry=await backupExpiry(name,id);assert.equal(entry.state.transfers[name].mode,'incremental');assert.equal(entry.state.transfers[name].base,before.replica.source_snapshot);assert.notEqual(entry.replica.source_snapshot,ordinary.name);
   const history=await api('/api/snapshots?'+new URLSearchParams({dataset:name,q:ordinary.name}));assert.equal(history.items.find(snapshot=>snapshot.name===ordinary.name).expires_at,ordinary.expires_at,'An existing ordinary snapshot keeps its original expiry');
   await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(saved)});await page.reload({waitUntil:'networkidle'});
  });
  await check('all backup datasets allow direct leaf plans and removal preserves the other destination',async()=>{
   await page.getByRole('button',{name:'全部数据集',exact:true}).click();assert.equal(await page.locator('[data-backup-dataset]').count(),14);
   assert.equal(await backup('Saturn','').locator('[data-action=dataset-backup]').count(),0);
   await page.locator('[data-action=new-dataset-backup]').click();await page.locator('#backup-source').waitFor();const leafNames=(await state()).dataset_rows.filter(row=>row.is_leaf).map(row=>row.config.name).sort((a,b)=>a.localeCompare(b,'en',{numeric:true}));assert.deepEqual(await page.locator('#backup-source option').evaluateAll(options=>options.map(option=>option.value)),leafNames);await page.locator('#backup-source').selectOption('Saturn/APP/Seafile');assert.equal(await page.locator('[name=destination]').inputValue(),'backup/Saturn/APP/Seafile');await page.locator('[name=destination]').fill('vault/user-edited-path');await page.locator('#backup-source').selectOption('Saturn/Archive');assert.equal(await page.locator('[name=destination]').inputValue(),'vault/user-edited-path');await page.keyboard.press('Escape');
   await backup('Saturn/APP/Seafile','').locator('[data-action=dataset-backup]').click();await page.locator('#backup-mode').selectOption('archive-n5105');await page.locator('[name=destination]').fill('rpool/backup/OnlySeafile');await save('#dataset-backup-form');assert.equal((await resolved('Saturn/APP/Seafile')).backups[0].destination,'rpool/backup/OnlySeafile');assert.equal((await resolved('Saturn/APP')).backups.length,0);
   await page.getByRole('button',{name:'有备份计划',exact:true}).click();assert.equal(await page.locator('[data-backup-dataset]').count(),6);
   await backup('Saturn/Archive','archive-secondary-test').locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#backup-mode').selectOption('none');await save('#dataset-backup-form');assert.equal(await backup('Saturn/Archive').count(),1);assert.equal((await resolved('Saturn/Archive')).backups[0].task.id,'archive-n5105');
  });
  await check('parent backup mappings never become inherited child plans',async()=>{
   const current=(await state()).config,task={...structuredClone(current.backups[0]),id:'parent-must-not-inherit',name:'旧路径计划',datasets:[{source:'Saturn/APP',destination:'vault/apps'}]};current.backups.push(task);await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(current)});
   assert.equal((await resolved('Saturn/APP/Seafile')).backups.length,1);assert.equal((await resolved('Saturn/APP/Immich')).backups.length,1);
   const rejected=await fetch(base+'/api/backups/parent-must-not-inherit/run?dataset=Saturn%2FAPP',{method:'POST'});assert.equal(rejected.status,400);
   await page.reload({waitUntil:'networkidle'});assert.equal(await backup('Saturn/APP').count(),0);assert.equal(await page.locator('[data-backup-dataset]').count(),5);
  });
  await check('mobile preserves every column with contained horizontal scrolling and saved filters',async()=>{
   await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));assert.equal(await page.locator('.overview-grid').evaluate(element=>getComputedStyle(element).gridTemplateColumns.split(' ').length),2);
   assert.equal(await page.locator('[data-backup-dataset]').count(),5);for(const cell of ['.backup-source','.backup-destination','.backup-rule','.backup-last','.backup-next'])assert(await backup('Saturn/Archive','archive-n5105').locator(cell).isVisible());
   await page.screenshot({path:path.join(artifacts,'list-backups-mobile.png'),fullPage:true});
   assert(await page.locator('.backup-table').evaluate(table=>[...table.querySelectorAll('.row-actions button')].every(button=>button.getBoundingClientRect().right<=table.getBoundingClientRect().right+1)));const scroll=page.locator('.dataset-backup-list .list-scroll');await scroll.evaluate(element=>element.scrollLeft=element.scrollWidth);const scrollLeft=await scroll.evaluate(element=>element.scrollLeft),oldTable=await page.locator('.backup-table').elementHandle();await page.waitForFunction(element=>!element.isConnected,oldTable,{timeout:12000});assert.equal(await scroll.evaluate(element=>element.scrollLeft),scrollLeft);
   await backup('Saturn/Archive','archive-n5105').locator('.row-actions [data-action=dataset-backup][data-id]').click();await page.locator('#dataset-backup-form').waitFor();assert.equal(await page.locator('[name=destination]').inputValue(),'rpool/backup/Archive');await page.keyboard.press('Escape');assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   await page.getByRole('link',{name:'快照',exact:true}).click();await page.getByRole('button',{name:'仅本级设置',exact:true}).click();await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('[data-action=dataset-scope].active').innerText(),'仅本级设置');
   assert.equal(await page.locator('.overview-grid').evaluate(element=>getComputedStyle(element).gridTemplateColumns.split(' ').length),2);assert(await snapshot('Saturn/APP/Immich').isVisible());for(const cell of ['.dataset-path','.snapshot-policy','.snapshot-keep','.snapshot-last','.snapshot-next'])assert(await snapshot('Saturn/APP/Immich').locator(cell).isVisible());assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   assert(await page.locator('.snapshot-table').evaluate(table=>[...table.querySelectorAll('.row-actions button')].every(button=>button.getBoundingClientRect().right<=table.getBoundingClientRect().right+1)));await page.screenshot({path:path.join(artifacts,'list-snapshots-mobile.png'),fullPage:true});assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
  });
  await check('saved browser configuration and normalized YAML contain no obsolete backup keep fields',async()=>{
   assert(configWrites.length>0);for(const config of [...configWrites,(await state()).config]){for(const policy of config.backup_policies)cleanPolicy(policy);for(const task of config.backups){noBackupKeep(task);if(task.policy)for(const key of timingKeys)assert.equal(Object.hasOwn(task,key),false,'Referenced jobs must not duplicate '+key);}}assert(!/\bkeep_(weekly|monthly)\s*:/.test(await api('/api/config')));
  });
  fs.writeFileSync(path.join(artifacts,'browser-report.json'),JSON.stringify({checked_at:new Date().toISOString(),checks,errors,external,base},null,2)+'\n');console.log(`${checks.length} current-UI checks passed`);
 }catch(error){await page.screenshot({path:path.join(artifacts,'failure.png'),fullPage:true}).catch(()=>{});throw error;}
 finally{await browser.close();}
})().catch(error=>{console.error(error);process.exit(1)});
