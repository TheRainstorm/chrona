// Use only with an isolated Demo configuration: this script changes its rules.
const {chromium}=require(process.env.CHRONA_PLAYWRIGHT||'playwright');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base=process.env.CHRONA_BACKUP_PLAN_URL||'http://127.0.0.1:8089',artifacts=path.resolve(__dirname,'../artifacts'),checks=[];
async function api(route,options){const r=await fetch(base+route,options);if(!r.ok)throw new Error(await r.text());return r.json();}
async function until(fn){const end=Date.now()+9000;while(Date.now()<end){if(await fn())return;await new Promise(r=>setTimeout(r,100));}throw new Error('Condition timed out');}
(async()=>{
 assert.equal((await api('/api/health')).mode,'demo');
 const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:1400,height:1000},locale:'zh-CN',timezoneId:'Asia/Shanghai',reducedMotion:'reduce'}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 const state=()=>api('/api/state'),row=name=>page.locator(`[data-backup-dataset="${name}"]`),snapshotRow=name=>page.locator(`[data-dataset-row="${name}"]`);
 const resolved=async name=>(await state()).dataset_rows.find(r=>r.config.name===name);
 const open=async name=>{if(!await row(name).evaluate(e=>e.open))await row(name).locator('summary').click();};
 const save=async form=>{await page.locator('#save-editor').click();await page.locator(form).waitFor({state:'detached'});};
 const check=async(name,fn)=>{await fn();checks.push(name);console.log('PASS',name);};
 let parentID;
 try{
  await page.goto(base,{waitUntil:'networkidle'});
  await check('each snapshot row opens current history and the five-horizon future plan',async()=>{
   const r=snapshotRow('Saturn/Archive');assert((await r.locator('.snapshot-access').innerText()).includes('张快照'));
   await r.locator('[data-action=plan]').click();await page.getByRole('heading',{name:'快照计划',exact:true}).waitFor();
   assert.equal(await page.locator('.plan-tier-grid>div').count(),5);assert.equal(await page.locator('.plan-table tbody tr').count(),16);
   await page.screenshot({path:path.join(artifacts,'snapshot-plan.png')});
   await page.locator('.inspection-tabs [data-action=browse]').click();await page.getByRole('heading',{name:'数据集快照',exact:true}).last().waitFor();
   assert.equal(await page.locator('.dialog tbody tr').count(),20);await page.keyboard.press('Escape');
  });
  await check('backup rows are collapsed by default and reveal their destinations on demand',async()=>{
   await page.getByRole('link',{name:'备份',exact:true}).click();await page.getByRole('heading',{name:'数据集备份',exact:true}).waitFor();
   assert.equal(await page.locator('details.dataset-backup').count(),6);assert.equal(await page.locator('details.dataset-backup[open]').count(),0);
   await page.screenshot({path:path.join(artifacts,'backups-collapsed.png'),fullPage:true});
   await open('Saturn/Archive');assert.equal(await row('Saturn/Archive').locator('.dataset-backup-binding').count(),1);
   assert((await row('Saturn/Archive').innerText()).includes('n5105:rpool/backup/Archive'));
  });
  await check('parent backup defaults expand relative paths to leaf targets without parent transfers',async()=>{
   await page.getByRole('button',{name:'添加数据集',exact:true}).click();await page.locator('[name=name]').fill('Saturn');await page.locator('#snapshot-mode').selectOption('fine');await save('#dataset-form');
   await open('Saturn');await row('Saturn').getByRole('button',{name:'设置继承规则',exact:true}).click();
   await page.locator('#backup-mode').selectOption('custom');await page.locator('[name=host]').fill('backup-node');await page.locator('[name=destination]').fill('vault/Saturn');
   await page.locator('[name=keep_weekly]').fill('0');await page.locator('[name=keep_monthly]').fill('1');await page.locator('[name=grace_minutes]').fill('45');await save('#dataset-backup-form');
   const s=await state();parentID=s.config.backups.find(b=>b.datasets.some(d=>d.source==='Saturn')).id;
   assert.equal(await row('Saturn').locator('[data-action=run-backup]').count(),0);
   assert.equal(await row('Saturn/APP').locator('[data-action=run-backup]').count(),0);
   const leaf=await resolved('Saturn/APP/Seafile');assert(leaf.is_leaf);assert.equal(leaf.backups[0].origin,'Saturn');assert.equal(leaf.backups[0].destination,'vault/Saturn/APP/Seafile');
   const rejected=await fetch(base+`/api/backups/${parentID}/run?dataset=Saturn`,{method:'POST'});assert.equal(rejected.status,400);
  });
  await check('first leaf transfer creates a receiver snapshot beyond next backup plus buffer',async()=>{
   const name='Saturn/APP/Seafile';await open(name);await row(name).locator('[data-action=run-backup]').click();
   await until(async()=>!!(await resolved(name)).backups[0].state.transfers?.[name]);
   const entry=(await resolved(name)).backups[0],transfer=entry.state.transfers[name];assert.equal(transfer.mode,'full');
   assert(new Date(entry.replica.expires_at)>=new Date(new Date(transfer.next_backup_at).getTime()+45*60000));
   assert.equal(entry.replica.source_snapshot,entry.state.bases[name]);
   const snaps=await api('/api/snapshots?'+new URLSearchParams({dataset:name}));assert(snaps.items.some(s=>s.name===entry.replica.source_snapshot&&s.held_by.includes(parentID)));
   assert(await row(name).evaluate(e=>e.open));
  });
  await check('second transfer uses the shared base and advances receiver protection after success',async()=>{
   const name='Saturn/APP/Seafile',old=(await resolved(name)).backups[0].state.bases[name];
   await row(name).locator('[data-action=run-backup]').click();await until(async()=>(await resolved(name)).backups[0].state.bases[name]!==old);
   const entry=(await resolved(name)).backups[0];assert.equal(entry.state.transfers[name].mode,'incremental');assert.equal(entry.state.transfers[name].base,old);
   const replicas=(await state()).replicas.filter(r=>r.task_id===parentID&&r.dataset===entry.destination);
   assert(replicas.some(r=>r.source_snapshot===old&&!r.held_by.includes(parentID)));assert(replicas.some(r=>r.source_snapshot===entry.state.bases[name]&&r.held_by.includes(parentID)));
   await page.locator('#toast-root').evaluate(e=>e.replaceChildren());await page.screenshot({path:path.join(artifacts,'backups-expanded.png'),fullPage:true});
  });
  await check('leaf backup can override and restore inherited destinations without changing the parent',async()=>{
   const name='Saturn/APP/Seafile';await row(name).locator('[data-action=dataset-backup][data-id]').click();
   assert.equal(await page.locator('#backup-mode').inputValue(),'inherit');await until(async()=>await page.getByRole('button',{name:'复制为独立规则',exact:true}).isVisible());
   await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();await page.locator('[name=host]').fill('leaf-host');await page.locator('[name=destination]').fill('leaf/Seafile');await save('#dataset-backup-form');
   assert.equal((await resolved(name)).backups[0].task.host,'leaf-host');assert((await state()).config.backups.some(b=>b.id===parentID&&b.datasets[0].source==='Saturn'));
   await row(name).locator('[data-action=dataset-backup][data-id]').click();await page.locator('#backup-mode').selectOption('none');await save('#dataset-backup-form');assert.equal((await resolved(name)).backups.length,0);
   // Editing snapshots must preserve the independent backup opt-out.
   await page.getByRole('link',{name:'快照',exact:true}).click();await snapshotRow(name).locator('[data-action=edit-dataset]').last().click();await page.locator('#snapshot-mode').selectOption('inherit');await save('#dataset-form');
   assert.equal((await resolved(name)).config.backup_mode,'none');
   await page.getByRole('link',{name:'备份',exact:true}).click();await open(name);await row(name).getByRole('button',{name:'设置备份',exact:true}).click();await page.locator('#backup-mode').selectOption('inherit');await save('#dataset-backup-form');
   assert.equal((await resolved(name)).backups[0].origin,'Saturn');assert.equal((await resolved(name)).backups[0].destination,'vault/Saturn/APP/Seafile');
  });
  await check('intermediate backup exclusion and restore affect only their own branch',async()=>{
   await open('Saturn/APP');await row('Saturn/APP').locator('[data-action=dataset-backup][data-id]').click();await page.locator('#backup-mode').selectOption('none');await save('#dataset-backup-form');
   assert.equal((await resolved('Saturn/APP/Seafile')).backups.length,0);assert.equal((await resolved('Saturn/APP/Immich')).backups.length,0);assert.equal((await resolved('Saturn/Archive')).backups.length,1);
   await row('Saturn/APP').getByRole('button',{name:'设置继承规则',exact:true}).click();await page.locator('#backup-mode').selectOption('inherit');await save('#dataset-backup-form');assert.equal((await resolved('Saturn/APP/Seafile')).backups[0].origin,'Saturn');
  });
  await check('collapsed backup layout, receiver metadata and snapshot plans fit mobile',async()=>{
   await page.getByRole('link',{name:'快照',exact:true}).click();await page.getByRole('link',{name:'备份',exact:true}).click();await page.setViewportSize({width:390,height:844});
   assert.equal(await page.locator('details.dataset-backup[open]').count(),0);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   await page.screenshot({path:path.join(artifacts,'backups-mobile.png'),fullPage:true});await open('Saturn/Archive');assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   await page.getByRole('link',{name:'快照',exact:true}).click();await snapshotRow('Saturn/Archive').locator('[data-action=plan]').click();await page.locator('.plan-table').waitFor();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:path.join(artifacts,'snapshot-plan-mobile.png')});assert.deepEqual(errors,[]);
  });
  fs.writeFileSync(path.join(artifacts,'backup-plan-report.json'),JSON.stringify({checked_at:new Date().toISOString(),checks,errors,base},null,2)+'\n');console.log(`${checks.length} backup and plan checks passed`);
 }catch(e){await page.screenshot({path:path.join(artifacts,'backup-plan-failure.png'),fullPage:true}).catch(()=>{});throw e;}
 finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1)});
