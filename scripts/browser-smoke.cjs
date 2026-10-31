// Use an isolated demo instance. This script modifies its configuration.
const {chromium}=require(process.env.CHRONA_PLAYWRIGHT||'playwright');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base=process.env.CHRONA_TEST_URL||'http://127.0.0.1:8088';
const artifacts=path.resolve(__dirname,'../artifacts'),checks=[];
fs.mkdirSync(artifacts,{recursive:true});
async function api(route,options){const r=await fetch(base+route,options);if(!r.ok)throw new Error(await r.text());return r.headers.get('content-type')?.includes('json')?r.json():r.text();}
async function until(fn,timeout=8000){const start=Date.now();while(Date.now()-start<timeout){if(await fn())return;await new Promise(r=>setTimeout(r,100));}throw new Error('Condition timed out');}
async function check(name,fn){await fn();checks.push(name);console.log('PASS',name);}
(async()=>{
  assert.equal((await api('/api/health')).mode,'demo');
  const browser=await chromium.launch({executablePath:process.env.CHRONA_CHROME||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
  const context=await browser.newContext({viewport:{width:1400,height:1000},locale:'zh-CN',timezoneId:'Asia/Shanghai',reducedMotion:'reduce'});
  const page=await context.newPage(),errors=[],external=[];
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(!r.url().startsWith(base)&&!r.url().startsWith('data:'))external.push(r.url());});
  const row=name=>page.locator(`[data-dataset-row="${name}"]`);
  const backupRow=name=>page.locator(`[data-backup-dataset="${name}"]`);
  const save=async form=>{await page.locator('#save-editor').click();await page.locator(form).waitFor({state:'detached'});};
  const edit=async name=>{await row(name).locator('[data-action=edit-dataset]').last().click();await page.locator('#dataset-form').waitFor();};
  const state=()=>api('/api/state');
  let newPolicyID;
  try{
    await check('dataset-first list and exactly three navigation entries',async()=>{
      await page.goto(base,{waitUntil:'networkidle'});
      assert.deepEqual(await page.locator('nav a').allTextContents(),['快照','备份','配置']);
      assert.equal(await page.locator('[data-dataset-row]').count(),6);
      assert.equal(await page.locator('.public-section').getAttribute('open'),null);
      assert.equal(await page.locator('.summary-grid,.coverage-grid,.sidebar').count(),0);
      await page.screenshot({path:path.join(artifacts,'desktop.png'),fullPage:true});
    });
    await check('copy shared snapshot rule to independent dataset settings',async()=>{
      await edit('Saturn/Archive');assert(await page.locator('#keep-hourly').isDisabled());
      await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();
      await page.locator('#keep-hourly').fill('96');
      await page.screenshot({path:path.join(artifacts,'policy-editor.png'),fullPage:true});
      await save('#dataset-form');
      const s=await state(),d=s.config.datasets.find(d=>d.name==='Saturn/Archive');
      assert(!d.policy);assert.equal(d.snapshot.keep.hourly,96);
      assert.equal(s.config.datasets.find(d=>d.name==='Saturn/APP/Immich').policy,'fine');
      assert.equal(s.config.policies.find(p=>p.id==='fine').keep.hourly,72);
    });
    await check('public policy changes propagate to references and preserve independent rules',async()=>{
      await page.locator('.public-section summary').click();
      await page.getByRole('button',{name:'编辑公共策略 精细保留',exact:true}).click();
      await page.locator('#keep-hourly').fill('120');await save('#policy-form');
      const s=await state();
      assert.equal(s.dataset_rows.find(r=>r.config.name==='Saturn/APP/Immich').snapshot.keep.hourly,120);
      assert.equal(s.dataset_rows.find(r=>r.config.name==='Saturn/Archive').snapshot.keep.hourly,96);
      await edit('Saturn/Archive');await page.locator('#snapshot-mode').selectOption('fine');
      assert(await page.locator('#keep-hourly').isDisabled());await save('#dataset-form');
      assert.equal((await state()).dataset_rows.find(r=>r.config.name==='Saturn/Archive').snapshot.keep.hourly,120);
    });
    await check('pause one dataset without pausing its shared policy or other references',async()=>{
      await row('Saturn/Archive').getByRole('switch').click();
      await until(async()=>(await state()).config.datasets.find(d=>d.name==='Saturn/Archive').disabled===true);
      const s=await state();assert(s.dataset_rows.find(r=>r.config.name==='Saturn/APP/Immich').snapshot.enabled);
      await row('Saturn/Archive').getByRole('switch').click();
      await until(async()=>(await state()).dataset_rows.find(r=>r.config.name==='Saturn/Archive').snapshot.enabled);
    });
    await check('define a reusable public policy and reference it when adding a dataset',async()=>{
      await page.locator('.public-section').evaluate(e=>e.open=true);
      await page.locator('[data-action=new-policy]').click();await page.locator('[name=label]').fill('照片保护');
      await save('#policy-form');newPolicyID=(await state()).config.policies.find(p=>p.label==='照片保护').id;
      await page.getByRole('button',{name:'添加数据集',exact:true}).click();
      await page.locator('#dataset-form [name=name]').fill('Saturn/APP/Seafile');
      await page.locator('#snapshot-mode').selectOption(newPolicyID);await save('#dataset-form');
      assert.equal((await state()).config.datasets.find(d=>d.name==='Saturn/APP/Seafile').policy,newPolicyID);
      await edit('Saturn/APP/Seafile');await page.getByRole('button',{name:'移除管理',exact:true}).click();
      await page.getByRole('button',{name:'确认移除',exact:true}).click();await page.locator('.dialog').waitFor({state:'detached'});
      assert(!(await state()).config.datasets.some(d=>d.name==='Saturn/APP/Seafile'));
      await page.locator('.public-section').evaluate(e=>e.open=true);
      await page.locator(`[data-public-policy="${newPolicyID}"] [data-action=edit-policy]`).click();
      await page.getByRole('button',{name:'删除公共策略',exact:true}).click();await page.locator('#policy-form').waitFor({state:'detached'});
    });
    await check('retention validation prevents overwriting valid dataset configuration',async()=>{
      await edit('Saturn/Archive');await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();
      await page.locator('#keep-hourly').fill('1');
      await until(async()=>(await page.locator('#policy-validation').innerText()).includes('超过'));
      const before=await api('/api/config');await page.locator('#save-editor').click();
      await until(async()=>(await page.locator('.toast').innerText()).includes('超过'));
      assert.equal(await api('/api/config'),before);
      await page.locator('#keep-hourly').fill('120');await page.locator('[name=start_at]').fill('2027-01-01T00:00');
      await until(async()=>(await page.locator('#policy-preview').innerText()).includes('5 档合并'));
      await page.getByRole('button',{name:'取消',exact:true}).click();
    });
    await check('manual dataset snapshot and inline paginated history',async()=>{
      const before=(await api('/api/snapshots?dataset=Saturn%2FArchive')).total;
      await page.getByRole('button',{name:'立即快照 Saturn/Archive',exact:true}).click();
      await until(async()=>(await api('/api/snapshots?dataset=Saturn%2FArchive')).total>before);
      await row('Saturn/Archive').locator('[data-action=browse]').first().click();
      await page.locator('.dialog').waitFor();assert.equal(await page.locator('.dialog tbody tr').count(),20);
      assert.equal(await page.locator('nav a[aria-current=page]').innerText(),'快照');
      await page.screenshot({path:path.join(artifacts,'snapshots.png'),fullPage:true});
      await page.getByRole('button',{name:'下一页',exact:true}).click();
      await until(async()=>(await page.locator('.dialog .drawer-foot').innerText()).includes('2 /'));
      await page.getByRole('button',{name:'完成',exact:true}).click();
    });
    await check('dataset backup copies public rules without changing other sources',async()=>{
      await page.getByRole('link',{name:'备份',exact:true}).click();
      await page.getByRole('heading',{name:'数据集备份',exact:true}).waitFor();
      assert.equal(await page.locator('details.dataset-backup[open]').count(),0);
      await page.locator('details.dataset-backup').evaluateAll(items=>items.forEach(item=>item.open=true));
      await backupRow('rpool/data/vm-201-ares-root').locator('[data-action=dataset-backup][data-id]').click();
      assert(await page.locator('[name=host]').isDisabled());
      await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();
      await page.locator('[name=host]').fill('private-host');await save('#dataset-backup-form');
      const s=await state(),shared=s.config.backups.find(b=>b.id==='ares-saturn');
      assert.equal(shared.host,'nas-pve');assert.equal(shared.datasets.length,1);
      const local=s.config.backups.find(b=>b.private);assert.equal(local.host,'private-host');assert.equal(local.datasets[0].source,'rpool/data/vm-201-ares-root');
      await backupRow('Saturn/Disk1').getByRole('button',{name:'设置备份',exact:true}).click();
      await page.locator('#backup-mode').selectOption('archive-n5105');
      await page.locator('[name=destination]').fill('rpool/backup/Disk1');await save('#dataset-backup-form');
      assert.equal((await state()).config.backups.find(b=>b.id==='archive-n5105').datasets.length,2);
      await page.screenshot({path:path.join(artifacts,'backups.png'),fullPage:true});
    });
    await check('run one dataset backup while preserving another incremental base',async()=>{
      const s=await state(),old=s.backups.find(b=>b.task.id==='archive-n5105').state;
      await backupRow('Saturn/Disk1').locator('[data-action=run-backup]').click();
      await until(async()=>{const b=(await state()).backups.find(b=>b.task.id==='archive-n5105').state;return b.status==='success'&&b.completed['Saturn/Disk1'];});
      const next=(await state()).backups.find(b=>b.task.id==='archive-n5105').state;
      assert.equal(next.bases['Saturn/Archive'],old.bases['Saturn/Archive']);
      assert.equal(next.completed['Saturn/Archive'],old.completed['Saturn/Archive']);
      const snap=await api('/api/snapshots?dataset=Saturn%2FArchive');assert(snap.items.some(s=>s.name===old.bases['Saturn/Archive']&&s.held_by.includes('archive-n5105')));
    });
    await check('public backup editor shares rules and keeps per-dataset target paths',async()=>{
      await page.locator('.public-section').evaluate(e=>e.open=true);
      await page.locator('[data-public-backup="archive-n5105"] [data-action=edit-backup-policy]').click();
      assert.equal(await page.locator('[name=destination]').count(),0);
      await page.locator('[name=at]').fill('06:15');await save('#public-backup-form');
      const s=await state(),b=s.config.backups.find(b=>b.id==='archive-n5105');assert.equal(b.at,'06:15');
      assert.equal(b.datasets.find(d=>d.source==='Saturn/Disk1').destination,'rpool/backup/Disk1');
      assert.equal(s.config.backups.find(b=>b.private).host,'private-host');
    });
    await check('YAML reflects dataset ownership and rejects invalid references',async()=>{
      await page.getByRole('link',{name:'配置',exact:true}).click();await page.locator('#config-yaml').waitFor();
      const yaml=await page.locator('#config-yaml').inputValue();assert(yaml.startsWith('version: 3'));assert(yaml.includes('policy: fine'));
      await page.locator('#config-yaml').fill(yaml.replace('policy: fine','policy: missing-public-rule'));
      await page.getByRole('button',{name:'保存配置',exact:true}).click();
      await until(async()=>(await page.locator('#config-validation').innerText()).includes('不存在'));
      assert.equal(await api('/api/config'),yaml);
      await page.locator('#config-yaml').fill(yaml);await page.getByRole('button',{name:'保存配置',exact:true}).click();
      await until(async()=>(await page.locator('.toast').innerText()).includes('配置已保存'));
    });
    await check('mobile dataset list and editor fit the viewport without errors or external assets',async()=>{
      await page.setViewportSize({width:390,height:844});await page.getByRole('link',{name:'快照',exact:true}).click();
      await page.getByRole('heading',{name:'数据集快照',exact:true}).waitFor();
      assert.equal(await page.locator('[data-dataset-row]').count(),6);
      assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      await page.screenshot({path:path.join(artifacts,'mobile.png'),fullPage:true});
      await edit('Saturn/Archive');
      assert(await page.evaluate(()=>document.querySelector('.drawer').getBoundingClientRect().width<=innerWidth));
      await page.screenshot({path:path.join(artifacts,'mobile-editor.png'),fullPage:false});
      await page.getByRole('button',{name:'复制为独立规则',exact:true}).click();
      await page.locator('#keep-hourly').fill('96');await save('#dataset-form');
      assert.equal((await state()).config.datasets.find(d=>d.name==='Saturn/Archive').snapshot.keep.hourly,96);
      assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
    });
    fs.writeFileSync(path.join(artifacts,'browser-report.json'),JSON.stringify({checked_at:new Date().toISOString(),checks,errors,external,base},null,2)+'\n');
    console.log(`${checks.length} browser checks passed`);
  }catch(e){await page.screenshot({path:path.join(artifacts,'failure.png'),fullPage:true}).catch(()=>{});throw e;}
  finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
