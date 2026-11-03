package chrona

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const baseCheckWarning = "首次备份基点核对："

// A missing map entry means the receiver has not been checked. An empty value
// records a verified receiver with no usable base; a snapshot name pins its base.
// Neither case claims that a backup has completed.
func backupBaseChecked(state BackupState, dataset string) bool {
	base, checked := state.Bases[dataset]
	return checked && (base == "" || strings.HasPrefix(base, dataset+"@"))
}

type pendingBackupBase struct {
	task    Backup
	binding BackupDataset
}

type discoveredBackupBase struct {
	source  *Snapshot
	replica *Snapshot
	warning string
}

// Resolve imported receivers before their first scheduled transfer. This only
// lists metadata and protects an existing shared snapshot; it never sends data.
func discoverNativeBackupBase(ctx context.Context, receiver nativeEndpoint, b Backup, binding BackupDataset) (discoveredBackupBase, error) {
	var result discoveredBackupBase
	token, err := receiver.run(ctx, "get", "-Hp", "-o", "value", "receive_resume_token", binding.Destination)
	if missingDataset(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if token = strings.TrimSpace(token); token != "" && token != "-" {
		return result, fmt.Errorf("接收端有未完成的传输，请先执行备份以续传")
	}
	remote, err := receiver.snapshots(ctx, binding.Destination)
	if missingDataset(err) || (err == nil && len(remote) == 0) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	local, err := (nativeEndpoint{}).snapshots(ctx, binding.Source)
	if err != nil {
		return result, err
	}
	base, err := selectNativeBase(local, remote)
	if err != nil {
		// The peer was successfully read but cannot accept an incremental stream.
		// Preserving every source snapshot cannot repair that mismatch.
		result.warning = err.Error()
		return result, nil
	}
	if err = (nativeEndpoint{}).hold(ctx, base.Name, nativeTag(b.ID)); err != nil {
		return result, err
	}
	latest := remote[len(remote)-1]
	if err = receiver.hold(ctx, latest.Name, nativeTag(b.ID)); err != nil {
		return result, err
	}
	base.HeldBy = append(base.HeldBy, nativeTag(b.ID))
	latest.HeldBy = append(latest.HeldBy, nativeTag(b.ID))
	result.source, result.replica = base, &latest
	return result, nil
}

func (a *App) currentBackupBindingLocked(expected pendingBackupBase) (Backup, bool) {
	for _, task := range a.config.Backups {
		if task.ID != expected.task.ID {
			continue
		}
		b := a.config.EffectiveBackup(task, a.datasets...)
		for _, binding := range b.Datasets {
			if sameBackupTarget(expected.task, expected.binding, b, binding) {
				return b, true
			}
		}
	}
	return Backup{}, false
}

func (a *App) commitNativeBackupBaseLocked(expected pendingBackupBase, result discoveredBackupBase) bool {
	b, exists := a.currentBackupBindingLocked(expected)
	state := a.state.Backups[b.ID]
	if !exists || state.Status == "running" || backupBaseChecked(state, expected.binding.Source) {
		return false
	}
	if state.Bases == nil {
		state.Bases = map[string]string{}
	}
	state.Bases[expected.binding.Source] = ""
	if result.source != nil {
		state.Bases[expected.binding.Source] = result.source.Name
		replica := ReplicaSnapshot{Snapshot: *result.replica, TaskID: b.ID, Host: b.Host, Port: b.Port, SourceSnapshot: result.source.Name}
		a.state.Replicas = append(a.state.Replicas, replica)
		for i := range a.state.Snapshots {
			if a.state.Snapshots[i].Name == result.source.Name {
				a.state.Snapshots[i].HeldBy = result.source.HeldBy
			}
		}
	}
	if result.warning != "" {
		state.Warning = baseCheckWarning + result.warning
	} else if strings.HasPrefix(state.Warning, baseCheckWarning) {
		state.Warning = ""
	}
	a.state.Backups[b.ID] = state
	message := "已核对接收端，未发现可复用基点，快照按到期时间清理"
	if result.source != nil {
		message = "已保护已有公共基点 " + result.source.Name + "，其余快照恢复到期清理"
	}
	level := "info"
	if result.warning != "" {
		level, message = "warning", state.Warning
	}
	a.record(level, "backup-base", expected.binding.Source, message)
	// Commit protection before allowing any source cleanup.
	a.persistLocked()
	return true
}

// No metadata probes are made by page refreshes. Pending receivers are checked
// on startup/config changes, with a 15-minute retry after connectivity failures.
func (a *App) scheduleNativeBaseInitializationLocked(now time.Time) {
	if a.opts.Mode != "zfs" || !a.opts.AllowWrites || a.closing || a.baseCheckRunning || now.Before(a.nextBaseCheck) {
		return
	}
	pending := []pendingBackupBase{}
	for _, task := range a.config.Backups {
		b := a.config.EffectiveBackup(task, a.datasets...)
		state := a.state.Backups[b.ID]
		if state.Status == "running" {
			continue
		}
		for _, binding := range b.Datasets {
			if !backupBaseChecked(state, binding.Source) {
				pending = append(pending, pendingBackupBase{b, binding})
			}
		}
	}
	if len(pending) == 0 {
		return
	}
	a.nextBaseCheck, a.baseCheckRunning = now.Add(15*time.Minute), true
	config := NormalizeConfig(a.config)
	a.workers.Add(1)
	go func() {
		defer func() {
			a.mu.Lock()
			a.baseCheckRunning = false
			a.mu.Unlock()
			a.workers.Done()
		}()
		ctx, cancel := context.WithTimeout(a.workerCtx, 2*time.Minute)
		defer cancel()
		for _, item := range pending {
			if ctx.Err() != nil {
				return
			}
			probe, cancelProbe := context.WithTimeout(ctx, 20*time.Second)
			result, err := discoverNativeBackupBase(probe, a.endpoint(item.task), item.task, item.binding)
			cancelProbe()
			a.mu.Lock()
			if a.closing {
				a.mu.Unlock()
				return
			}
			b, exists := a.currentBackupBindingLocked(item)
			if !exists {
				// Retire any holds acquired while that binding was being edited.
				a.scheduleNativeMaintenanceLocked(config, a.config)
				a.mu.Unlock()
				continue
			}
			state := a.state.Backups[b.ID]
			if state.Status == "running" || backupBaseChecked(state, item.binding.Source) {
				a.mu.Unlock()
				continue
			}
			if err != nil {
				state.Warning = baseCheckWarning + fmt.Sprintf("%s；稍后重试，暂缓此源的清理", err)
				a.state.Backups[b.ID] = state
				a.record("warning", "backup-base", item.binding.Source, state.Warning)
				a.persistLocked()
			} else if a.commitNativeBackupBaseLocked(item, result) {
				if p, _, ok := a.config.ResolveSnapshot(item.binding.Source); ok && p.Enabled {
					a.pruneSnapshotDatasetLocked(ctx, item.binding.Source)
					a.persistLocked()
				}
			}
			a.mu.Unlock()
		}
	}()
}
