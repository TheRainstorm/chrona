#!/usr/bin/env python3
"""Run on nas-pve after the read-only service and native integration checks pass."""
import pathlib,subprocess,json,urllib.request,base64,time,shutil,hashlib
info=json.loads(pathlib.Path('/etc/chrona/deployment.json').read_text())
backup=pathlib.Path(info['backup'])
current=subprocess.run(['crontab','-l'],capture_output=True,text=True,check=True).stdout
(backup/'crontab.at-activation').write_text(current)
lines=[];removed=[]
for line in current.splitlines():
    words=line.split()
    if words and not line.lstrip().startswith('#') and '/root/scripts/zfs_backup/zfs_snapshot.sh' in words:
        dataset=words[-1]
        if dataset=='rpool' or dataset.startswith('rpool/') or dataset=='Saturn' or dataset.startswith('Saturn/'):
            removed.append(line);line='# Replaced by chrona.service: '+line
    lines.append(line)
updated='\n'.join(lines)+'\n'
(backup/'crontab.after').write_text(updated)
password=pathlib.Path('/etc/chrona/web-password').read_text().strip()
headers={'Authorization':'Basic '+base64.b64encode(('chrona:'+password).encode()).decode()}

def state():
    req=urllib.request.Request('http://192.168.35.253:8086/api/state',headers=headers)
    return json.load(urllib.request.urlopen(req,timeout=20))

before=state()
assert before['mode']=='zfs' and not before['writes_enabled']
assert len(before['datasets'])>=40
try:
    subprocess.run(['crontab','-'],input=updated,text=True,check=True)
    shutil.copy2('/etc/chrona/chrona.service.prepared','/etc/systemd/system/chrona.service')
    subprocess.run(['systemctl','daemon-reload'],check=True)
    subprocess.run(['systemctl','enable','chrona.service'],check=True)
    subprocess.run(['systemctl','restart','chrona.service'],check=True)
    for _ in range(50):
        try:
            s=state()
            if s['mode']=='zfs' and s['writes_enabled']:break
        except Exception:pass
        time.sleep(.2)
    else:raise RuntimeError('writable service did not become ready')
    assert subprocess.run(['systemctl','is-active','--quiet','chrona.service']).returncode==0
    assert subprocess.run(['systemctl','is-enabled','--quiet','chrona.service']).returncode==0
    info.update(phase='active',replaced_snapshot_cron_lines=len(removed),config_sha256=hashlib.sha256(pathlib.Path('/etc/chrona/config.yaml').read_bytes()).hexdigest())
    pathlib.Path('/etc/chrona/deployment.json').write_text(json.dumps(info,indent=2)+'\n')
    print(json.dumps(dict(info,mode=s['mode'],writes_enabled=s['writes_enabled'],datasets=len(s['datasets']),snapshot_policy_datasets=sum(bool(r['snapshot']) for r in s['dataset_rows'])),indent=2))
except Exception:
    subprocess.run(['crontab','-'],input=current,text=True,check=True)
    shutil.copy2('/etc/chrona/chrona-readonly.service','/etc/systemd/system/chrona.service')
    subprocess.run(['systemctl','daemon-reload'],check=True)
    subprocess.run(['systemctl','restart','chrona.service'],check=True)
    raise
