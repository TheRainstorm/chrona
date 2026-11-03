#!/usr/bin/env python3
"""Run on a ZFS host against an explicit candidate binary, using new UUID datasets only.

Required: CHRONA_VERIFY_BINARY=/absolute/path/to/candidate
Optional: CHRONA_RETENTION_SSH_HOST=n5105 CHRONA_RETENTION_SSH_PORT=2202
Optional: CHRONA_RETENTION_USE_SSH_CONFIG=1 omits the application's SSH port,
so the configured host alias must supply it; the harness still uses SSH_PORT.
No production config, service, snapshot or dataset is changed. The temporary app
has automatic schedules disabled; each test action targets a UUID dataset.
Creation times cannot be backdated: old default-expiry cases belong in unit tests.
"""
import datetime
import json
import os
import pathlib
import re
import shlex
import shutil
import stat
import socket
import subprocess
import time
import traceback
import urllib.parse
import urllib.request
import uuid


def main():
    binary = os.environ.get('CHRONA_VERIFY_BINARY', '')
    if not binary or not pathlib.Path(binary).is_file() or not os.access(binary, os.X_OK):
        raise SystemExit('Set CHRONA_VERIFY_BINARY to an executable candidate binary')
    ssh_host = os.environ.get('CHRONA_RETENTION_SSH_HOST', '')
    ssh_port = int(os.environ.get('CHRONA_RETENTION_SSH_PORT', '22'))
    use_ssh_config = os.environ.get('CHRONA_RETENTION_USE_SSH_CONFIG', '') == '1'
    if use_ssh_config and not ssh_host:
        raise SystemExit('SSH config verification requires CHRONA_RETENTION_SSH_HOST')
    if ssh_host and not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.@-]*', ssh_host):
        raise SystemExit('Invalid SSH host')
    if not 1 <= ssh_port <= 65535:
        raise SystemExit('Invalid SSH port')
    uid = 'chrona-retention-' + uuid.uuid4().hex[:12]
    root = 'rpool/' + uid
    remote_root = root
    work = pathlib.Path('/var/lib/chrona-verification') / uid
    work.mkdir(parents=True, mode=0o700)
    runtime = pathlib.Path('/run') / uid
    control_dir = runtime / 'ssh'
    control_dir.mkdir(parents=True, mode=0o700)
    report = {'id': uid, 'binary': binary, 'checks': [], 'ssh_port_from_config': use_ssh_config,
              'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat()}
    created_roots = []
    process = None
    external_hold = 'test:' + uid
    jobs = []
    log = None
    last_manual = {}

    def argv(args, remote=False):
        if remote:
            return ['ssh', '-T', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
                    '-o', 'ConnectTimeout=8', '-p', str(ssh_port), ssh_host, shlex.join(args)]
        return args

    def cmd(args, remote=False, check=True):
        result = subprocess.run(argv(args, remote), capture_output=True, text=True, timeout=60)
        if check and result.returncode:
            raise RuntimeError(shlex.join(args) + ': ' + result.stderr.strip())
        return result

    def exists(name, remote=False):
        result = cmd(['zfs', 'list', '-H', '-o', 'name', name], remote, check=False)
        if result.returncode == 0:
            return result.stdout.strip() == name
        if 'does not exist' not in result.stderr:
            raise RuntimeError('Cannot verify absence: ' + name + ': ' + result.stderr.strip())
        return False

    def prop(name, key, remote=False):
        return cmd(['zfs', 'get', '-Hp', '-o', 'value', key, name], remote).stdout.strip()

    def snapshot(dataset, suffix, expiry=None):
        name = dataset + '@' + suffix
        args = ['zfs', 'snapshot']
        if expiry is not None:
            args += ['-o', 'chrona:expires=' + str(expiry)]
        cmd(args + [name])
        return name

    def seed_receive(source, destination, remote, previous=None):
        send = ['zfs', 'send', '-p']
        if previous:
            send += ['-i', previous]
        receive = ['zfs', 'receive', '-u']
        if not previous:
            receive += ['-o', 'mountpoint=none', '-o', 'canmount=off', '-o', 'readonly=on']
        sender = subprocess.Popen(send + [source], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        receiver = subprocess.Popen(argv(receive + [destination], remote), stdin=sender.stdout,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        sender.stdout.close()
        try:
            _, receive_error = receiver.communicate(timeout=60)
            _, send_error = sender.communicate(timeout=60)
        except Exception:
            receiver.kill()
            sender.kill()
            receiver.wait()
            sender.wait()
            raise
        if sender.returncode or receiver.returncode:
            raise RuntimeError('Seed stream failed: ' + send_error.decode() + receive_error.decode())

    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    base = 'http://127.0.0.1:' + str(port)

    def api(route, method='GET', data=None):
        payload = json.dumps(data).encode() if data is not None else None
        request = urllib.request.Request(base + route, method=method, data=payload,
                                         headers={'Content-Type': 'application/json'} if payload else {})
        with urllib.request.urlopen(request, timeout=40) as response:
            return json.load(response)

    def manual(dataset):
        # Normal manual snapshots use whole-second names. Avoid a duplicate no-op.
        wait = last_manual.get(dataset, 0) + 1.05 - time.time()
        if wait > 0:
            time.sleep(wait)
        row = next(row for row in api('/api/state')['dataset_rows'] if row['config']['name'] == dataset)
        policy = row['snapshot']['id']
        api('/api/policies/' + urllib.parse.quote(policy, safe='') + '/run?' +
            urllib.parse.urlencode({'dataset': dataset}), 'POST')
        last_manual[dataset] = time.time()

    def run_job(job):
        api('/api/backups/' + job['id'] + '/run?' +
            urllib.parse.urlencode({'dataset': job['source']}), 'POST')
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            state = next(item['state'] for item in api('/api/state')['backups']
                         if item['task']['id'] == job['id'])
            if state['status'] == 'error':
                raise RuntimeError(state.get('error'))
            if state['status'] == 'success':
                assert not state.get('warning'), state.get('warning')
                return state
            time.sleep(.1)
        raise RuntimeError('Timed out waiting for ' + job['id'])

    def check(label):
        report['checks'].append(label)
        print('PASS ' + label, flush=True)

    try:
        for dataset, remote in [(root, False)] + ([(remote_root, True)] if ssh_host else []):
            assert not exists(dataset, remote), 'UUID namespace already exists'
            cmd(['zfs', 'create', '-o', 'mountpoint=none', '-o', 'canmount=off', dataset], remote)
            created_roots.append((dataset, remote))
        plain, child, keep_path = root + '/source', root + '/source/child', root + '/keep-in-path'
        for dataset in (plain, child, keep_path):
            cmd(['zfs', 'create', '-o', 'mountpoint=none', '-o', 'canmount=off', dataset])
        past = int(time.time()) - 3600
        expired = snapshot(plain, 'legacy-expired', past)
        kept = snapshot(plain, 'manual-keep-old', past)
        upper = snapshot(plain, 'manual-KEEP-old', past)
        held = snapshot(plain, 'manual-held-old', past)
        cmd(['zfs', 'hold', external_hold, held])
        fresh = snapshot(plain, 'manual-no-property')
        invalid = snapshot(plain, 'manual-invalid-property', 'invalid')
        child_old = snapshot(child, 'legacy-expired', past)
        path_old = snapshot(keep_path, 'legacy-expired', past)
        endpoints = [('local', False)] + ([('ssh', True)] if ssh_host else [])
        for kind, remote in endpoints:
            source, destination = root + '/backup-' + kind, root + '/receiver-' + kind
            cmd(['zfs', 'create', '-o', 'mountpoint=none', '-o', 'canmount=off', source])
            old = snapshot(source, 'legacy-expired-seed', past)
            seed_receive(old, destination, remote)
            permanent = snapshot(source, 'archive-KEEP', past)
            seed_receive(permanent, destination, remote, old)
            common = snapshot(source, 'legacy-common', past)
            seed_receive(common, destination, remote, permanent)
            expired_hourly = snapshot(source, 'chrona_20261102T120000Z', past)
            jobs.append({'id': 'retention-' + kind, 'source': source, 'destination': destination,
                         'remote': remote, 'old': old, 'keep': permanent, 'common': common, 'expired_hourly': expired_hourly})
        now = datetime.datetime.now(datetime.timezone.utc)
        def managed(dataset):
            return {'name': dataset, 'disabled': dataset not in [job['source'] for job in jobs], 'snapshot': {
                'start_at': now.replace(hour=0, minute=0, second=0, microsecond=0).isoformat(),
                'keep': {'quarter_hourly': 0, 'hourly': 0, 'daily': 0, 'weekly': 0, 'monthly': 12} if dataset in [job['source'] for job in jobs] else {'quarter_hourly': 0, 'hourly': 3, 'daily': 7, 'weekly': 0, 'monthly': 0}}}
        config = {'version': 3, 'timezone': 'Asia/Shanghai', 'jitter_seconds': 0, 'policies': [],
                  'datasets': [managed(name) for name in [plain, keep_path] + [j['source'] for j in jobs]] +
                              [{'name': child, 'snapshot_mode': 'none'}],
                  'backup_policies': [{'id': 'shared-time', 'name': 'Shared timing across receivers',
                                       'cadence': 'monthly', 'weekdays': [1], 'month_day': 1,
                                       'at': '23:00', 'grace_minutes': 60}],
                  'backups': [{'id': j['id'], 'name': j['id'], 'host': ssh_host if j['remote'] else 'local',
                               'port': ssh_port if j['remote'] else 22, 'policy': 'shared-time', 'enabled': False,
                               'datasets': [{'source': j['source'], 'destination': j['destination']}]}
                              for j in jobs]}
        if use_ssh_config:
            for task in config['backups']:
                if task['host'] == ssh_host:
                    task.pop('port')
        (work / 'config.json').write_text(json.dumps(config, indent=2) + '\n')
        log = (work / 'app.log').open('wb')
        env = dict(os.environ, PATH='/usr/sbin:/usr/bin:/sbin:/bin')
        process = subprocess.Popen([binary, '-mode', 'zfs', '-enable-zfs-writes', '-listen',
                                    '127.0.0.1:' + str(port), '-config', str(work / 'config.json'),
                                    '-state', str(work / 'state.json'), '-ssh-control-dir', str(control_dir)],
                                   env=env, stdout=log, stderr=subprocess.STDOUT)
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError('Candidate exited; inspect ' + str(work / 'app.log'))
            try:
                if api('/api/health')['mode'] == 'zfs':
                    break
            except Exception:
                pass
            time.sleep(.1)
        else:
            raise RuntimeError('Candidate did not start')
        loaded = api('/api/state')['config']
        assert len(loaded['backup_policies']) == 1
        assert not {'host', 'port', 'datasets'}.intersection(loaded['backup_policies'][0])
        assert all(job['policy'] == 'shared-time' and 'cadence' not in job for job in loaded['backups'])
        check('one timing-only policy is shared by independently configured local/SSH receivers')
        if use_ssh_config:
            assert all('port' not in job for job in loaded['backups'] if job['host'] == ssh_host)
            check('SSH alias keeps its application port omitted and delegates connection settings to OpenSSH')
        deadline = time.monotonic() + 40
        while time.monotonic() < deadline:
            state = api('/api/state')
            if all((next(row['state'] for row in state['backups'] if row['task']['id'] == job['id']).get('bases') or {}).get(job['source']) == job['common'] for job in jobs):
                break
            time.sleep(.1)
        else:
            raise RuntimeError('Imported common bases were not initialized before the first backup')
        for job in jobs:
            pending = next(row['state'] for row in state['backups'] if row['task']['id'] == job['id'])
            assert pending['last_success'].startswith('0001') and pending['status'] != 'success'
            assert not exists(job['expired_hourly']) and not exists(job['old'])
            assert exists(job['common']) and exists(job['keep'])
            assert 'chrona:' + job['id'] in cmd(['zfs', 'holds', '-H', job['common']]).stdout
            remote_common = job['destination'] + '@legacy-common'
            assert 'chrona:' + job['id'] in cmd(['zfs', 'holds', '-H', remote_common], job['remote']).stdout
            check(job['id'] + ': before any backup, startup prunes expired hourly snapshots and holds only the imported common base')
        buffer_minutes = 60
        items = {item['name']: item for item in api('/api/snapshots?' + urllib.parse.urlencode({'dataset': plain}))['items']}
        for name in (fresh, invalid):
            item = items[name]
            created = datetime.datetime.fromisoformat(item['created_at'].replace('Z', '+00:00'))
            expires = datetime.datetime.fromisoformat(item['expires_at'].replace('Z', '+00:00'))
            assert item['retention_source'] == 'default' and not item['permanent']
            assert expires - created == datetime.timedelta(days=30)
        assert prop(fresh, 'chrona:expires') == '-'
        assert prop(invalid, 'chrona:expires') == 'invalid'
        check('missing/invalid expiry derives exactly 30 days without rewriting properties')
        for name in (kept, upper):
            assert items[name]['permanent'] and items[name]['retention_source'] == 'keep'
            assert items[name]['expires_at'].startswith('0001')
        check('keep substring is case-insensitive and overrides explicit past expiry')
        manual(plain)
        assert not exists(expired)
        assert all(exists(name) for name in (kept, upper, held, fresh, invalid, child_old))
        assert prop(kept, 'chrona:expires') == str(past)
        check('exact local cleanup removes expired legacy name and preserves keep, hold, fresh and child')
        manual(keep_path)
        assert not exists(path_old)
        check('keep in dataset path does not protect an ordinary snapshot suffix')
        for job in jobs:
            manual(job['source'])
            assert not exists(job['old']) and exists(job['common'])
            check(job['id'] + ': imported common base survives normal source cleanup before first backup')
            first = run_job(job)
            transfer = first['transfers'][job['source']]
            assert transfer['mode'] == 'incremental'
            expected = int(datetime.datetime.fromisoformat(transfer['next_backup_at'].replace('Z', '+00:00')).timestamp()) + buffer_minutes * 60
            assert int(prop(first['bases'][job['source']], 'chrona:expires')) == expected
            assert int(prop(transfer['remote_snapshot'], 'chrona:expires', job['remote'])) == expected
            check(job['id'] + ': both new backup expiries equal next run plus buffer despite year-long snapshot policy')
            assert not exists(job['old'])
            assert not exists(job['destination'] + '@legacy-expired-seed', job['remote'])
            assert exists(job['keep']) and exists(job['destination'] + '@archive-KEEP', job['remote'])
            check(job['id'] + ': first incremental copy prunes expired legacy snapshots at both endpoints')
            if job is jobs[0]:
                before = api('/api/state')
                update = before['config']
                update['backup_policies'][0].update(month_day=2, at='22:15', grace_minutes=90)
                api('/api/config', 'PUT', update)
                after = api('/api/state')
                assert before['config']['backups'] == after['config']['backups']
                for old in before['backups']:
                    new = next(row for row in after['backups'] if row['task']['id'] == old['task']['id'])
                    assert new['task']['host'] == old['task']['host'] and new['task'].get('port', 0) == old['task'].get('port', 0)
                    assert new['task']['month_day'] == 2 and new['task']['at'] == '22:15' and new['task']['grace_minutes'] == 90
                    for key in ('bases', 'completed'):
                        assert (new['state'].get(key) or {}) == (old['state'].get(key) or {})
                # Native metadata maintenance is asynchronous; wait before forcing expiry
                # into the past so that the following protection check cannot race it.
                deadline = time.monotonic() + 30
                while time.monotonic() < deadline:
                    updated = next(row['state'] for row in api('/api/state')['backups'] if row['task']['id'] == job['id'])
                    changed = updated['transfers'][job['source']]
                    if changed['protected_until'] != transfer['protected_until']:
                        next_run = datetime.datetime.fromisoformat(changed['next_backup_at'].replace('Z', '+00:00'))
                        expires = datetime.datetime.fromisoformat(changed['protected_until'].replace('Z', '+00:00'))
                        assert next_run.day == 2 and next_run.hour == 22 and next_run.minute == 15
                        assert expires - next_run == datetime.timedelta(minutes=90)
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError('Shared policy maintenance did not update the existing base')
                buffer_minutes = 90
                check('editing shared timing updates both receivers without changing target mappings or common bases')
            base_name = first['bases'][job['source']]
            remote_base = transfer['remote_snapshot']
            for name, remote in [(base_name, False), (remote_base, job['remote'])]:
                cmd(['zfs', 'release', 'chrona:' + job['id'], name], remote)
                cmd(['zfs', 'set', 'chrona:expires=' + str(past), name], remote)
            manual(job['source'])
            assert exists(base_name) and exists(remote_base, job['remote'])
            check(job['id'] + ': persisted current base survives missing hold and past expiry')
            second = run_job(job)
            transfer = second['transfers'][job['source']]
            assert transfer['mode'] == 'incremental'
            expected = int(datetime.datetime.fromisoformat(transfer['next_backup_at'].replace('Z', '+00:00')).timestamp()) + buffer_minutes * 60
            assert int(prop(second['bases'][job['source']], 'chrona:expires')) == expected
            assert int(prop(transfer['remote_snapshot'], 'chrona:expires', job['remote'])) == expected
            check(job['id'] + ': subsequent backup uses identical next-run expiry at both ends')
            base_name = second['bases'][job['source']]
            assert prop(base_name, 'guid') == prop(transfer['remote_snapshot'], 'guid', job['remote'])
            assert 'chrona:' + job['id'] in cmd(['zfs', 'holds', '-H', base_name]).stdout
            assert 'chrona:' + job['id'] in cmd(['zfs', 'holds', '-H', transfer['remote_snapshot']], job['remote']).stdout
            check(job['id'] + ': subsequent incremental copy verifies GUID and restores protected bases')
        report['status'] = 'passed'
    except Exception as error:
        report['status'] = 'failed'
        report['error'] = traceback.format_exc()
        raise
    finally:
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        if log is not None:
            log.close()
        if ssh_host:
            for path in control_dir.iterdir():
                if stat.S_ISSOCK(path.stat().st_mode):
                    cmd(['ssh', '-S', str(path), '-O', 'exit', '-p', str(ssh_port), ssh_host], check=False)
        shutil.rmtree(runtime)
        # Cleanup is restricted to roots verified absent and created above.
        try:
            tags = {external_hold} | {'chrona:' + job['id'] for job in jobs}
            for dataset, remote in reversed(created_roots):
                assert dataset == root and dataset.startswith('rpool/chrona-retention-')
                names = cmd(['zfs', 'list', '-H', '-r', '-t', 'snapshot', '-o', 'name', dataset], remote).stdout.splitlines()
                for name in names:
                    assert name.startswith(dataset + '/') or name.startswith(dataset + '@')
                    for line in cmd(['zfs', 'holds', '-H', name], remote).stdout.splitlines():
                        fields = line.split()
                        if len(fields) > 1 and fields[1] in tags:
                            cmd(['zfs', 'release', fields[1], name], remote)
                cmd(['zfs', 'destroy', '-r', dataset], remote)
            report['cleanup'] = 'all created UUID namespaces removed; production service untouched'
        except Exception as cleanup_error:
            report['cleanup_error'] = str(cleanup_error)
            report['status'] = 'failed'
        report['report_path'] = str(work / 'report.json')
        (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report, indent=2), flush=True)
        if report['status'] != 'passed':
            raise SystemExit(1)


if __name__ == '__main__':
    main()
