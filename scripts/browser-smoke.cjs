// Mutates an isolated Demo only. Never run against the production NAS.
const {chromium}=require(process.env.CHRONA_PLAYWRIGHT||'playwright');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base=process.env.CHRONA_TEST_URL||'http://127.0.0.1:8093',artifacts=path.resolve(__dirname,'../artifacts'),checks=[];
async function api(route,options){const r=await fetch(base+route,options);if(!r.ok)throw new Error(await r.text());return r.headers.get('content-type')?.includes('json')?r.json():r.text();}
async function until(fn){const end=Date.now()+9000;while(Date.now()<end){if(await fn())return;await new Promise(r=>setTimeout(r,100));}throw new Error('Condition timed out');}
(async()=>{
 assert.equal((await api('/api/health')).mode,'demo');
 const initial=await api('/api/state'),config=structuredClone(initial.config),fine=config.policies.find(p=>p.id==='fine');
 config.datasets=[{name:'Saturn',policy:'fine'},{name:'Saturn/APP/Immich',snapshot:{start_at:fine.start_at,keep:{quarter_hourly:12,hourly:24,daily:7,weekly:4,monthly:12}}},{name:'Saturn/Disk1',snapshot_mode:'none'},{name:'rpool/data',policy:'ssd'},{name:'rpool/ROOT/pve-1',policy:'ssd'}];
 await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(config)});
 const browser=await chromium.launch({executablePath:process.env.CHRONA_CHROME||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
 const context=await browser.newContext({viewport:{width:1400,height:1000},locale:'zh-CN',timezoneId:'Asia/Shanghai',reducedMotion:'reduce'}),page=await context.newPage(),errors=[],external=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(!r.url().startsWith(base)&&!r.url().startsWith('data:'))external.push(r.url());});
 const node=name=>page.locator(`[data-dataset-node="${name}"]`),summary=name=>node(name).locator(':scope > summary');
 const open=async name=>{const parts=name.split('/');for(let i=1;i<=parts.length;i++){const current=parts.slice(0,i).join('/'),n=node(current);if(await n.count()&&!await n.evaluate(e=>e.open))await summary(current).click();}};
 const edit=async name=>{const parts=name.split('/');for(let i=1;i<parts.length;i++){const parent=parts.slice(0,i).join('/');if(await node(parent).count())await open(parent);}await summary(name).getByRole('button',{name:`编辑数据集 ${name}`,exact:true}).click();await page.locator('#dataset-form').waitFor();};
 const save=async form=>{await page.locator('#save-editor').click();await page.locator(form).waitFor({state:'detached'});};
 const state=()=>api('/api/state'),resolved=async name=>(await state()).dataset_rows.find(r=>r.config.name===name);
 const check=async(name,fn)=>{await fn();checks.push(name);console.log('PASS',name);};
 try{
  await check('hierarchical snapshot datasets are independently collapsed by default',async()=>{
   await page.goto(base,{waitUntil:'networkidle'});assert.deepEqual(await page.locator('nav a').allTextContents(),['快照','备份','配置']);assert.equal(await page.locator('[data-dataset-row]').count(),10);assert.equal(await page.locator('.tree-node[open]').count(),0);
   assert(!(await summary('Saturn/APP/Immich').isVisible()));await summary('Saturn').click();assert(await summary('Saturn/APP').isVisible());assert(!(await summary('Saturn/APP/Immich').isVisible()));await summary('Saturn/APP').click();assert(await summary('Saturn/APP/Immich').isVisible());assert(!await node('Saturn/APP/Immich').evaluate(e=>e.open));
   await page.screenshot({path:path.join(artifacts,'tree-snapshots-desktop.png'),fullPage:true});
  });
  await check('details shows snapshot history and actual projected schedule without expanding the dataset',async()=>{
   await summary('Saturn/Archive').getByRole('button',{name:'查看 Saturn/Archive 详情',exact:true}).click();await page.locator('.dialog').waitFor();assert.equal(await page.locator('.dialog tbody tr').count(),20);assert(!await node('Saturn/Archive').evaluate(e=>e.open));
   await page.locator('.inspection-tabs [data-action=plan]').click();await page.locator('.plan-table').waitFor();assert.equal(await page.locator('.plan-tier-grid>div').count(),5);assert.equal(await page.locator('.plan-table tbody tr').count(),16);await page.screenshot({path:path.join(artifacts,'tree-snapshot-plan.png')});await page.keyboard.press('Escape');
  });
  await check('only local snapshot settings are shown in the direct view',async()=>{
   await page.getByRole('button',{name:'仅本级设置',exact:true}).click();assert.equal(await page.locator('[data-dataset-row]').count(),5);assert.equal(await page.locator('.tree-rule-badge.inherited').count(),0);assert.equal(await node('Saturn/Archive').count(),0);assert.equal(await node('Saturn/APP/Seafile').count(),0);assert.equal(await node('Saturn/APP/Immich').count(),1);assert.equal(await page.locator('.tree-node[open]').count(),0);
  });
  await check('all datasets can be expanded, folded and assigned a local rule',async()=>{
   await page.getByRole('button',{name:'全部数据集',exact:true}).click();assert.equal(await page.locator('[data-dataset-row]').count(),13);assert.equal(await page.locator('.tree-node[open]').count(),0);await page.getByRole('button',{name:'全部展开',exact:true}).click();assert.equal(await page.locator('.tree-node[open]').count(),13);await page.getByRole('button',{name:'全部收起',exact:true}).click();assert.equal(await page.locator('.tree-node[open]').count(),0);
   await edit('rpool/ROOT');await page.locator('#snapshot-mode').selectOption('ssd');await save('#dataset-form');assert.equal((await resolved('rpool/ROOT')).config.policy,'ssd');await page.getByRole('button',{name:'仅本级设置',exact:true}).click();assert.equal(await node('rpool/ROOT').count(),1);
  });
  await check('copied snapshot rules stay independent from parent edits and can resume inheritance',async()=>{
   await page.getByRole('button',{name:'有快照策略',exact:true}).click();await edit('Saturn/APP/Seafile');await until(async()=>(await page.locator('#binding-explanation').innerText()).includes('继承自 Saturn'));await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();await page.locator('#keep-hourly').fill('36');await save('#dataset-form');
   await page.locator('.public-section').evaluate(e=>e.open=true);await page.getByRole('button',{name:'编辑公共策略 精细保留',exact:true}).click();await page.locator('#keep-hourly').fill('120');await save('#policy-form');assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,36);assert.equal((await resolved('Saturn/Archive')).snapshot.keep.hourly,120);assert.equal((await resolved('Saturn/APP/Immich')).snapshot.keep.hourly,24);
   await edit('Saturn/APP/Seafile');await page.getByRole('button',{name:'恢复继承',exact:true}).click();await save('#dataset-form');assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,120);
  });
  await check('backup default view contains only datasets with explicit plans and starts folded',async()=>{
   await page.getByRole('link',{name:'备份',exact:true}).click();await page.getByRole('heading',{name:'数据集备份',exact:true}).waitFor();assert.equal(await page.locator('[data-backup-dataset]').count(),3);assert.equal(await page.locator('.tree-node[open]').count(),0);assert.equal(await node('Saturn').count(),0);assert.equal(await node('Saturn/APP/Immich').count(),0);await page.screenshot({path:path.join(artifacts,'tree-backups-planned.png'),fullPage:true});
   await open('Saturn/Archive');assert((await node('Saturn/Archive').innerText()).includes('n5105:rpool/backup/Archive'));await node('Saturn/Archive').locator('[data-action=dataset-backup][data-id]').click();assert.equal(await page.locator('#backup-mode option[value=inherit]').count(),0);await page.keyboard.press('Escape');
  });
  await check('all-backup view permits a direct plan without giving siblings any plan',async()=>{
   await page.getByRole('button',{name:'全部数据集',exact:true}).click();assert.equal(await page.locator('[data-backup-dataset]').count(),13);assert.equal(await page.locator('.tree-node[open]').count(),0);await open('Saturn/APP/Immich');await node('Saturn/APP/Immich').getByRole('button',{name:'设置备份计划',exact:true}).click();await page.locator('#backup-mode').selectOption('archive-n5105');await page.locator('[name=destination]').fill('rpool/backup/OnlyImmich');await save('#dataset-backup-form');
   assert.equal((await resolved('Saturn/APP/Immich')).backups[0].destination,'rpool/backup/OnlyImmich');assert.equal((await resolved('Saturn/APP/Seafile')).backups.length,0);assert.equal((await resolved('Saturn/APP')).backups.length,0);await page.getByRole('button',{name:'有备份计划',exact:true}).click();assert.equal(await page.locator('[data-backup-dataset]').count(),4);
  });
  await check('an explicit parent mapping never expands targets or becomes a child task',async()=>{
   const c=(await state()).config,b=structuredClone(c.backups[0]);b.id='parent-must-not-inherit';b.name='旧路径计划';b.datasets=[{source:'Saturn/APP',destination:'vault/apps'}];c.backups.push(b);await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(c)});
   assert.equal((await resolved('Saturn/APP/Seafile')).backups.length,0);assert.equal((await resolved('Saturn/APP/Immich')).backups.length,1);const rejected=await fetch(base+'/api/backups/parent-must-not-inherit/run?dataset=Saturn%2FAPP',{method:'POST'});assert.equal(rejected.status,400);
   await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('[data-backup-dataset]').count(),4);assert.equal(await node('Saturn/APP').count(),0);
  });
  await check('per-dataset backup execution and expiry protection still work',async()=>{
   await open('Saturn/APP/Immich');await node('Saturn/APP/Immich').getByRole('button',{name:'立即备份',exact:true}).click();await until(async()=>!!(await resolved('Saturn/APP/Immich')).backups[0].state.completed?.['Saturn/APP/Immich']);const entry=(await resolved('Saturn/APP/Immich')).backups[0],transfer=entry.state.transfers['Saturn/APP/Immich'];assert(entry.replica);assert(new Date(entry.replica.expires_at)>new Date(transfer.next_backup_at));assert.equal((await resolved('Saturn/APP/Seafile')).backups.length,0);
   await node('Saturn/APP/Immich').locator('[data-action=dataset-backup][data-id]').click();await page.locator('#backup-mode').selectOption('none');await save('#dataset-backup-form');assert.equal((await resolved('Saturn/APP/Immich')).backups.length,0);assert.equal(await node('Saturn/APP/Immich').count(),0);
  });
  await check('mobile trees fit and each view resets folds without losing filter preferences',async()=>{
   await page.setViewportSize({width:390,height:844});await page.getByRole('button',{name:'全部数据集',exact:true}).click();assert.equal(await page.locator('.tree-node[open]').count(),0);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await open('Saturn/Archive');assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:path.join(artifacts,'tree-backups-mobile.png'),fullPage:true});
   await page.getByRole('link',{name:'快照',exact:true}).click();await page.getByRole('button',{name:'仅本级设置',exact:true}).click();await page.reload({waitUntil:'networkidle'});assert.equal(await page.locator('[data-action=dataset-scope].active').innerText(),'仅本级设置');assert.equal(await page.locator('.tree-node[open]').count(),0);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:path.join(artifacts,'tree-snapshots-mobile.png'),fullPage:true});assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
  });
  fs.writeFileSync(path.join(artifacts,'browser-report.json'),JSON.stringify({checked_at:new Date().toISOString(),checks,errors,external,base},null,2)+'\n');console.log(`${checks.length} current-UI checks passed`);
 }catch(e){await page.screenshot({path:path.join(artifacts,'failure.png'),fullPage:true}).catch(()=>{});throw e;}
 finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1)});
