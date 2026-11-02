# Chrona

[English](README.md) · [简体中文](README.zh-CN.md)

A small, self-hosted Web UI for ZFS snapshots and backups. Run it on your NAS, set retention counts instead of writing cron expressions, and keep configuration in a plain YAML file.

Chrona is one Go binary with an embedded HTML/CSS/JavaScript interface. It needs no database, Node.js runtime, container, or external CDN. YAML parsing is its only third-party Go dependency. The current Web interface is in Simplified Chinese.

## What you can do

- **Snapshots:** see full dataset paths, policy names, retention counts, last snapshot and next run in one row. Browse snapshots, preview the schedule, edit rules, pause, or take a snapshot directly.
- **Policies:** set counts for 15-minute, hourly, daily, weekly and monthly tiers. New rules start at zero. Children inherit the nearest parent’s snapshot policy unless explicitly overridden.
- **Backups:** use a sorted, flat list with one row per source/destination pair. See both paths, rule, last success, next run and status; enable or pause each destination independently. Only leaf datasets are backed up, with no backup inheritance.
- **Overview:** read snapshot space usage and task summaries from the service’s existing cached inventory, without additional SSH calls.
- **Configuration:** edit or download the same YAML file from the UI. File changes reload automatically; invalid changes leave the working configuration intact.

## Try the demo

Requires Go 1.24 or newer:

```sh
git clone https://github.com/TheRainstorm/chrona.git
cd chrona
go run ./cmd/chrona -listen 127.0.0.1:8086
```

Open [localhost:8086](http://127.0.0.1:8086). Demo mode uses simulated data and does not touch ZFS. The first run creates configuration and demo state under `data/`.

Build a standalone binary with `make build`, or:

```sh
CGO_ENABLED=0 go build -trimpath -o bin/chrona ./cmd/chrona
```

## Connect real ZFS

Use Linux with OpenZFS tools (`zfs`, `zpool`) and permissions to manage the intended datasets. Start with your own configuration, using [config.example.yaml](config.example.yaml) as a reference:

```sh
sudo install -d -m 700 /etc/chrona /var/lib/chrona
sudo install -m 600 config.example.yaml /etc/chrona/config.yaml
sudoedit /etc/chrona/config.yaml
sudo ./bin/chrona -mode zfs -listen 127.0.0.1:8086 \
  -config /etc/chrona/config.yaml -state /var/lib/chrona/state.json
```

ZFS mode is **read-only by default**. Add `-enable-zfs-writes` to enable scheduled snapshots, expiry cleanup and replication. Use a separate state file from Demo mode.

Useful options:

| Option | Purpose |
| --- | --- |
| `-auth-file /etc/chrona/web-password` | Enable HTTP Basic authentication; username `chrona`, password read from a file containing at least 20 characters. |
| `-ssh-control-dir /run/chrona/ssh` | Reuse SSH connections for remote metadata commands and transfers. |
| `-listen 127.0.0.1:8086` | Choose the listening address; use HTTPS through a reverse proxy for remote access. |

For boot startup, adapt [chrona.service.example](chrona.service.example) to your binary, paths and listening address. Run one service instance per configuration file.

## Retention, without cron

Set an anchor time and the number of snapshots to keep at each tier. For example:

```yaml
version: 3
timezone: UTC
jitter_seconds: 30
policies: []
datasets:
  - name: tank/data
    snapshot:
      start_at: "2026-01-01T00:00:00Z"
      keep: {quarter_hourly: 0, hourly: 3, daily: 7, weekly: 4, monthly: 12}
backups: []
```

A zero count disables that tier. A longer tier must cover at least as much history as a shorter tier. When tiers coincide, Chrona creates one snapshot using the longest tier’s expiry. A stable jitter spreads work out, and monthly schedules follow the calendar. See the example configuration for shared policy references and backup plans.

Expiry rules also cover existing snapshots:

- A snapshot suffix containing `keep`, in any letter case, is retained indefinitely. Only the part after `@` is checked; this overrides a finite expiry property.
- Otherwise, a valid `chrona:expires` Unix timestamp is respected, including on snapshots created by other tools.
- Without a valid explicit expiry, the default is **creation time + 30 days**. This is calculated without rewriting legacy snapshot properties.

Cleanup follows successful snapshot creation or replication in the exact dataset. Holds and recorded incremental bases are protected; an uninitialized backup source defers cleanup until its common base is known. Deletion rechecks snapshot identity, expiry and holds, and never uses recursive or forced deletion.

## Native replication

Backups use ZFS send/receive, locally across pools or through an SSH host alias such as `backup-nas`. The service uses its account’s SSH keys/configuration and verified host keys, with connection reuse. Use `host: local` for a local receiver.

The weekly/monthly frequency controls **when a backup runs**. Backup snapshots have no weekly/monthly retention counts: each run creates a dedicated snapshot with the same expiry at both ends, **next scheduled backup + `grace_minutes`** (default 60 minutes). This is separate from dataset snapshot retention. Removing retention counts does not shorten existing snapshot expiries; obsolete `keep_weekly` / `keep_monthly` config keys are accepted but ignored and omitted on the next save.

The core commands are `zfs send -w -p -i BASE NEW | zfs receive -u -s DEST`, with `-i BASE` omitted for the first full transfer. Each leaf dataset is transferred independently, without `-R`, intermediate-snapshot `-I`, or forced-receive `-F`.

Incremental transfers require the receiver’s latest snapshot GUID to exist at the source. Chrona protects both ends through the next backup plus a buffer, advances the base only after successful verification, and supports interrupted receive resumption. Existing destinations are never forcibly rolled back to make a transfer fit. New receiving datasets are left unmounted and read-only.

## Development

Run `make test` for Go tests and `node --check web/app.js` for JavaScript syntax. [scripts/browser-smoke.cjs](scripts/browser-smoke.cjs) exercises an isolated Demo using Playwright; it changes demo configuration and must not target production. Rebuild after changing Go or embedded Web assets; YAML edits need no rebuild.

## License

[MIT](LICENSE) © 2026 Fuyan Yuan.
