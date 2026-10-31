#!/usr/bin/env python3
"""Run on nas-pve. Writes only new UUID namespaces and removes those after success."""
import datetime, hashlib, json, os, pathlib, subprocess, time, urllib.request, uuid

uid = 'chrona-verify-' + uuid.uuid4().hex[:10]
unit = uid
source_root = 'rpool/' + uid
local_root = 'Saturn/' + uid
remote_root = 'rpool/' + uid
work = pathlib.Path('/var/lib/chrona-verification') / uid
mount = pathlib.Path('/var/tmp') / uid
work.mkdir(parents=True, mode=0o700)
mount.mkdir(mode=0o700)
(work/'bin').mkdir(mode=0o700)
wrapper=work/'bin'/'zfs'
wrapper.write_text('''#!/usr/bin/python3
import os,sys,time,subprocess,pathlib
marker=pathlib.Path(__file__).parent.parent/'throttle'
if len(sys.argv)>1 and sys.argv[1]=='send' and marker.exists():
 p=subprocess.Popen(['/usr/sbin/zfs']+sys.argv[1:],stdout=subprocess.PIPE)
 while True:
  chunk=p.stdout.read(65536)
  if not chunk:break
  sys.stdout.buffer.write(chunk);sys.stdout.buffer.flush();time.sleep(.25)
 sys.exit(p.wait())
os.execv('/usr/sbin/zfs',['zfs']+sys.argv[1:])
''')
wrapper.chmod(0o700)
report = {'id':uid, 'checks':[], 'created_at':datetime.datetime.now(datetime.timezone.utc).isoformat()}

def cmd(args, remote=False, check=True):
    if remote:
        import shlex
        args=['ssh','-o','BatchMode=yes','-o','ConnectTimeout=8','n5105',shlex.join(args)]
    r=subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
    if check and r.returncode:
        raise RuntimeError(' '.join(args[:4])+': '+r.stderr)
    return r.stdout.strip()

def request(route, method='GET'):
    with urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:8097'+route,method=method),timeout=30) as r:
        return json.load(r)

def wait_ready():
    for _ in range(100):
        try:
            if request('/api/health')['mode']=='zfs':return
        except Exception:pass
        time.sleep(.2)
    raise RuntimeError('native verification service failed to start')

def run_job(job,expected):
    request('/api/backups/'+job+'/run','POST')
    for _ in range(200):
        s=request('/api/state')
        state=next(b['state'] for b in s['backups'] if b['task']['id']==job)
        if state['status']=='error':raise RuntimeError(state.get('error'))
        if state['status']=='success':
            source=next(iter(state['transfers']))
            t=state['transfers'][source]
            assert t['mode']==expected,(job,t)
            report['checks'].append(job+': '+expected)
            return state,t
        time.sleep(.2)
    raise RuntimeError('verification transfer timed out')

try:
    for dataset,remote in [(source_root,False),(local_root,False),(remote_root,True)]:
        assert cmd(['zfs','list','-H','-o','name',dataset],remote,False)=='', 'verification UUID already exists'
    cmd(['zfs','create','-o','mountpoint=none','-o','canmount=off',source_root])
    fs=source_root+'/source-fs';vol=source_root+'/source-vol'
    cmd(['zfs','create','-o','mountpoint='+str(mount/'source'),fs])
    cmd(['zfs','create','-s','-V','16M',vol])
    for _ in range(50):
        if pathlib.Path('/dev/zvol/'+vol).exists():break
        time.sleep(.1)
    assert pathlib.Path('/dev/zvol/'+vol).exists()
    (mount/'source'/'proof.txt').write_text('chrona native recovery v1\n')
    with open('/dev/zvol/'+vol,'r+b',buffering=0) as f:f.write(b'chrona-volume-v1'.ljust(4096,b'.'))
    now=datetime.datetime.now(datetime.timezone(datetime.timedelta(hours=8)))
    def backup(identifier,source,destination,host):
        return dict(id=identifier,name=identifier,datasets=[dict(source=source,destination=destination)],host=host,port=2202,cadence='monthly',weekdays=[1],month_day=1,at='23:00',enabled=False,keep_weekly=0,keep_monthly=1,grace_minutes=90)
    config=dict(version=3,timezone='Asia/Shanghai',jitter_seconds=10,policies=[],datasets=[dict(name=source,disabled=True,snapshot=dict(start_at=now.replace(hour=0,minute=0,second=0,microsecond=0).isoformat(),keep=dict(quarter_hourly=0,hourly=3,daily=7,weekly=0,monthly=0))) for source in (fs,vol)],backups=[backup('verify-fs-local',fs,local_root+'/received-fs','local'),backup('verify-fs-ssh',fs,remote_root+'/received-fs','n5105'),backup('verify-vol-local',vol,local_root+'/received-vol','local'),backup('verify-fs-resume',fs,local_root+'/received-resume','local')])
    (work/'config.json').write_text(json.dumps(config,indent=2))
    cmd(['systemd-run','--unit='+unit,'--collect','--property=UMask=0077','--setenv=PATH='+str(work/'bin')+':/usr/sbin:/usr/bin:/sbin:/bin','/opt/chrona/chrona-0.6.0.next','-mode','zfs','-enable-zfs-writes','-listen','127.0.0.1:8097','-config',str(work/'config.json'),'-state',str(work/'state.json'),'-ssh-control-dir','/run/'+uid+'/ssh'])
    wait_ready()
    for job in ['verify-fs-local','verify-fs-ssh','verify-vol-local']:run_job(job,'full')
    (mount/'source'/'proof.txt').write_text('chrona native recovery v2\n'+uid+'\n')
    expected=hashlib.sha256((mount/'source'/'proof.txt').read_bytes()).hexdigest()
    with open('/dev/zvol/'+vol,'r+b',buffering=0) as f:f.write(b'chrona-volume-v2'.ljust(4096,b'!'))
    cmd(['zpool','sync','rpool'])
    latest={}
    for job in ['verify-fs-local','verify-fs-ssh','verify-vol-local']:
        state,t=run_job(job,'incremental')
        remote=job.endswith('ssh')
        source=next(iter(state['bases']));snapshot=state['bases'][source]
        guid=cmd(['zfs','get','-Hp','-o','value','guid',snapshot])
        assert guid==cmd(['zfs','get','-Hp','-o','value','guid',t['remote_snapshot']],remote)
        floor=int(datetime.datetime.fromisoformat(t['protected_until'].replace('Z','+00:00')).timestamp())
        assert int(cmd(['zfs','get','-Hp','-o','value','chrona:expires',snapshot]))>=floor
        assert int(cmd(['zfs','get','-Hp','-o','value','chrona:expires',t['remote_snapshot']],remote))>=floor
        assert 'chrona:'+job in cmd(['zfs','holds','-H',snapshot])
        assert 'chrona:'+job in cmd(['zfs','holds','-H',t['remote_snapshot']],remote)
        latest[job]=t
        report['checks'].append(job+': GUID, source/receiver expiry and holds')
    clone=local_root+'/restore-proof'
    cmd(['zfs','clone','-o','canmount=on','-o','readonly=on','-o','mountpoint='+str(mount/'restore'),latest['verify-fs-local']['remote_snapshot'],clone])
    actual=hashlib.sha256((mount/'restore'/'proof.txt').read_bytes()).hexdigest()
    assert actual==expected
    report['restore_sha256']=actual
    report['checks'].append('restore clone file checksum equals current source file')
    cmd(['zfs','destroy',clone])
    # Interrupt a throttled real stream, then verify receive_resume_token recovery.
    (mount/'source'/'proof-blob.bin').write_bytes(os.urandom(6*1024*1024))
    cmd(['zpool','sync','rpool'])
    (work/'throttle').touch()
    request('/api/backups/verify-fs-resume/run','POST')
    for _ in range(150):
        state=next(b['state'] for b in request('/api/state')['backups'] if b['task']['id']=='verify-fs-resume')
        if state['status']=='error':raise RuntimeError(state.get('error'))
        if state.get('bytes',0)>256*1024:break
        time.sleep(.1)
    else:raise RuntimeError('throttled stream did not start')
    cmd(['systemctl','stop',unit])
    token=cmd(['zfs','get','-Hp','-o','value','receive_resume_token',local_root+'/received-resume'])
    assert token and token!='-', 'interruption did not save a resume token'
    (work/'throttle').unlink()
    # Start the collected transient unit again using its saved complete command.
    cmd(['systemd-run','--unit='+unit,'--collect','--property=UMask=0077','--setenv=PATH='+str(work/'bin')+':/usr/sbin:/usr/bin:/sbin:/bin','/opt/chrona/chrona-0.6.0.next','-mode','zfs','-enable-zfs-writes','-listen','127.0.0.1:8097','-config',str(work/'config.json'),'-state',str(work/'state.json'),'-ssh-control-dir','/run/'+uid+'/ssh'])
    wait_ready()
    state,t=run_job('verify-fs-resume','incremental')
    assert cmd(['zfs','get','-Hp','-o','value','receive_resume_token',local_root+'/received-resume'])=='-'
    assert cmd(['zfs','get','-Hp','-o','value','guid',state['bases'][fs]])==cmd(['zfs','get','-Hp','-o','value','guid',t['remote_snapshot']])
    report['checks'].append('interrupted real receive resumed and advanced to a fresh incremental snapshot')
    # Verify persistent state across an actual service restart.
    before=request('/api/state')['backups']
    cmd(['systemctl','restart',unit])
    wait_ready()
    after=request('/api/state')['backups']
    assert {b['task']['id']:b['state']['bases'] for b in before}=={b['task']['id']:b['state']['bases'] for b in after}
    report['checks'].append('systemd restart preserves real common-base state')
    cmd(['systemctl','stop',unit])
    for dataset,remote in [(source_root,False),(local_root,False),(remote_root,True)]:
        names=cmd(['zfs','list','-H','-r','-t','snapshot','-o','name',dataset],remote).splitlines()
        for name in names:
            holds=cmd(['zfs','holds','-H',name],remote)
            for line in holds.splitlines():
                fields=line.split()
                if len(fields)>1 and fields[1].startswith('chrona:verify-'):
                    cmd(['zfs','release',fields[1],name],remote)
        # All three roots were absent at the start and were created by this test.
        assert dataset in [source_root,local_root,remote_root]
        cmd(['zfs','destroy','-r',dataset],remote)
    report['cleanup']='all UUID dataset namespaces removed'
    report['status']='passed'
    (work/'report.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(dict(report,report_path=str(work/'report.json')),indent=2),flush=True)
except Exception as e:
    report['status']='failed';report['error']=str(e)
    (work/'report.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(dict(report,report_path=str(work/'report.json')),indent=2),flush=True)
    raise
