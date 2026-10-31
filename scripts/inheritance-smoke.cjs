// Mutates an isolated Demo instance; never use against a real ZFS configuration.
const {chromium}=require(process.env.CHRONA_PLAYWRIGHT||'playwright');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base=process.env.CHRONA_INHERITANCE_URL||'http://127.0.0.1:8089';
const artifacts=path.resolve(__dirname,'../artifacts'),checks=[];
async function state(){return (await fetch(base+'/api/state')).json();}
async function until(fn){const end=Date.now()+8000;while(Date.now()<end){if(await fn())return;await new Promise(r=>setTimeout(r,100));}throw new Error('Condition timed out');}
(async()=>{
 assert.equal((await (await fetch(base+'/api/health')).json()).mode,'demo');
 const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:1400,height:1100},locale:'zh-CN',timezoneId:'Asia/Shanghai',reducedMotion:'reduce'}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 const row=name=>page.locator(`[data-dataset-row="${name}"]`);
 const edit=async name=>{await row(name).locator('[data-action=edit-dataset]').last().click();await page.locator('#dataset-form').waitFor();};
 const save=async()=>{await page.locator('#save-editor').click();await page.locator('#dataset-form').waitFor({state:'detached'});};
 const resolved=async name=>(await state()).dataset_rows.find(r=>r.config.name===name);
 const check=async(name,fn)=>{await fn();checks.push(name);console.log('PASS',name);};
 try{
  await page.goto(base,{waitUntil:'networkidle'});
  await check('adding a parent discovers existing children with default inheritance',async()=>{
   await page.getByRole('button',{name:'添加数据集',exact:true}).click();
   assert.equal(await page.locator('#snapshot-mode').inputValue(),'inherit');
   assert((await page.locator('#dataset-options').innerHTML()).includes('value="Saturn"'));
   await page.locator('[name=name]').fill('Saturn');await page.locator('#snapshot-mode').selectOption('fine');await save();
   const s=await state();assert.equal(s.dataset_rows.length,10);
   const child=s.dataset_rows.find(r=>r.config.name==='Saturn/APP/Seafile');
   assert.equal(child.snapshot_source,'Saturn');assert(child.inherited);assert(!child.explicit);assert.equal(child.snapshot.keep.hourly,72);
   assert(!s.config.datasets.some(d=>d.name==='Saturn/APP/Seafile'));
   assert.equal(await row('Saturn/APP/Seafile').getAttribute('data-inherited-from'),'Saturn');
  });
  await check('inherited child editor previews the actual parent rules without copying config',async()=>{
   await edit('Saturn/APP/Seafile');assert.equal(await page.locator('#snapshot-mode').inputValue(),'inherit');
   await until(async()=>(await page.locator('#binding-explanation').innerText()).includes('继承自 Saturn'));
   assert(await page.locator('#keep-hourly').isDisabled());assert.equal(await page.locator('#keep-hourly').inputValue(),'72');
   await page.screenshot({path:path.join(artifacts,'inheritance-editor.png')});await page.keyboard.press('Escape');
   const before=(await (await fetch(base+'/api/snapshots?dataset=Saturn%2FAPP%2FSeafile')).json()).total;
   await row('Saturn/APP/Seafile').locator('[data-action=run-dataset]').click();
   await until(async()=>(await (await fetch(base+'/api/snapshots?dataset=Saturn%2FAPP%2FSeafile')).json()).total>before);
  });
  await check('nearest intermediate override wins while explicit siblings retain their rules',async()=>{
   await edit('Saturn/APP');await until(async()=>(await page.locator('#binding-explanation').innerText()).includes('继承自 Saturn'));
   await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();await page.locator('#keep-hourly').fill('96');await save();
   assert.equal((await resolved('Saturn/APP/Seafile')).snapshot_source,'Saturn/APP');
   assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,96);
   assert.equal((await resolved('Saturn/APP/Immich')).snapshot_source,'Saturn/APP/Immich');
   await edit('Saturn/APP/Immich');await page.locator('#snapshot-mode').selectOption('inherit');await save();
   assert.equal((await resolved('Saturn/APP/Immich')).snapshot.keep.hourly,96);
   await edit('Saturn');await page.locator('#snapshot-mode').selectOption('files');await save();
   assert.equal((await resolved('Saturn/VM')).snapshot.keep.hourly,24);
   assert.equal((await resolved('Saturn/APP/Immich')).snapshot.keep.hourly,96);
   assert.equal((await resolved('Saturn/Archive')).snapshot.keep.hourly,72);
  });
  await check('custom child survives a parent edit and can restore live inheritance',async()=>{
   await edit('Saturn/APP/Seafile');await until(async()=>(await page.locator('#keep-hourly').inputValue())==='96');
   await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();await page.locator('#keep-hourly').fill('48');await save();
   await edit('Saturn/APP');await page.locator('#keep-hourly').fill('120');await save();
   assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,48);
   assert.equal((await resolved('Saturn/APP/Immich')).snapshot.keep.hourly,120);
   await edit('Saturn/APP/Seafile');await page.getByRole('button',{name:'恢复继承',exact:true}).click();
   await until(async()=>(await page.locator('#keep-hourly').inputValue())==='120');
   assert(await page.locator('#keep-hourly').isDisabled());await save();
   const c=(await state()).config.datasets.find(d=>d.name==='Saturn/APP/Seafile');assert(!c.snapshot&&!c.policy);
   assert.equal((await resolved('Saturn/APP/Seafile')).snapshot_source,'Saturn/APP');
  });
  await check('pause an implicit child without pausing its parent or siblings',async()=>{
   await row('Saturn/VM').getByRole('switch').click();await until(async()=>(await resolved('Saturn/VM')).snapshot.enabled===false);
   assert((await resolved('Saturn')).snapshot.enabled);assert((await resolved('Saturn/APP/Seafile')).snapshot.enabled);
   await row('Saturn/VM').getByRole('switch').click();await until(async()=>(await resolved('Saturn/VM')).snapshot.enabled);
  });
  await check('explicit exclusion blocks a branch and a child override can opt back in',async()=>{
   await edit('Saturn/APP');await page.locator('#snapshot-mode').selectOption('none');await save();
   assert.equal((await resolved('Saturn/APP')).snapshot,null);assert.equal((await resolved('Saturn/APP/Seafile')).snapshot,null);
   assert.equal((await resolved('Saturn/Archive')).snapshot.keep.hourly,72);
   await edit('Saturn/APP/Immich');await page.locator('#snapshot-mode').selectOption('fine');await save();
   assert.equal((await resolved('Saturn/APP/Immich')).snapshot.keep.hourly,72);
   await edit('Saturn/APP');await page.locator('#snapshot-mode').selectOption('inherit');await save();
   assert.equal((await resolved('Saturn/APP/Seafile')).snapshot_source,'Saturn');assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,24);
  });
  await check('public policy updates reach descendants and hierarchy fits mobile',async()=>{
   await page.locator('.public-section').evaluate(e=>e.open=true);await page.getByRole('button',{name:'编辑公共策略 日常文件',exact:true}).click();
   await page.locator('#keep-hourly').fill('36');await page.locator('#save-editor').click();await page.locator('#policy-form').waitFor({state:'detached'});
   assert.equal((await resolved('Saturn/APP/Seafile')).snapshot.keep.hourly,36);
   assert.equal((await resolved('Saturn/APP/Immich')).snapshot.keep.hourly,72);
   await page.locator('.public-section').evaluate(e=>e.open=false);await page.locator('#toast-root').evaluate(e=>e.replaceChildren());
   await page.screenshot({path:path.join(artifacts,'inheritance-desktop.png'),fullPage:true});
   await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   await page.screenshot({path:path.join(artifacts,'inheritance-mobile.png'),fullPage:true});
   await edit('Saturn/APP/Seafile');await until(async()=>(await page.locator('#binding-explanation').innerText()).includes('继承自 Saturn'));
   assert(await page.evaluate(()=>document.querySelector('.drawer').getBoundingClientRect().width<=innerWidth));
   await page.screenshot({path:path.join(artifacts,'inheritance-mobile-editor.png')});
   assert.deepEqual(errors,[]);assert.deepEqual(await page.locator('nav a').allTextContents(),['快照','备份','配置']);
  });
  fs.writeFileSync(path.join(artifacts,'inheritance-report.json'),JSON.stringify({checked_at:new Date().toISOString(),checks,errors,base},null,2)+'\n');console.log(`${checks.length} inheritance checks passed`);
 }catch(e){await page.screenshot({path:path.join(artifacts,'inheritance-failure.png'),fullPage:true}).catch(()=>{});throw e;}
 finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
