package chrona

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func validateEffectiveBackupTargets(c Config, inventory []Dataset) error {
	used := map[string]string{}
	for _, task := range c.Backups {
		b := c.EffectiveBackup(task, inventory...)
		for _, binding := range b.Datasets {
			key := fmt.Sprintf("%s:%d:%s", b.Host, b.Port, binding.Destination)
			if previous, ok := used[key]; ok && previous != b.ID+":"+binding.Source {
				return errors.New("多个备份任务指向相同目的数据集：" + binding.Destination)
			}
			used[key] = b.ID + ":" + binding.Source
		}
	}
	return nil
}

func (a *App) pruneReplicasLocked(now time.Time) {
	kept := []ReplicaSnapshot{}
	for _, r := range a.state.Replicas {
		if r.Managed && !r.ExpiresAt.After(now) && len(r.HeldBy) == 0 {
			continue
		}
		kept = append(kept, r)
	}
	a.state.Replicas = kept
}

// Stage both receiver metadata and all new bases, then commit them together.
// A failure leaves the previous common snapshots protected on both sides.
func (a *App) finishDemoBackupLocked(b Backup, now time.Time) error {
	loc, _ := time.LoadLocation(a.config.Timezone)
	next, until := backupProtection(b, loc, now)
	previous := a.state.Backups[b.ID]
	bases, completed, transfers := map[string]string{}, map[string]time.Time{}, map[string]TransferResult{}
	for source, base := range previous.Bases {
		bases[source] = base
	}
	for source, at := range previous.Completed {
		completed[source] = at
	}
	for source, result := range previous.Transfers {
		transfers[source] = result
	}
	pending := []ReplicaSnapshot{}
	var totalBytes uint64
	for _, binding := range b.Datasets {
		if !a.config.IsLeafDataset(binding.Source, a.datasets...) {
			return errors.New("源已成为路径节点，已停止备份并保留上次基点：" + binding.Source)
		}
		base := previous.Bases[binding.Source]
		common := false
		if findReplica(a.state.Replicas, b, binding, base) != nil {
			for _, s := range a.state.Snapshots {
				if s.Name == base {
					common = true
					break
				}
			}
		}
		policy, _, ok := a.config.ResolveSnapshot(binding.Source)
		if !ok {
			policy = Policy{ID: b.ID, Dataset: binding.Source, Keep: Keep{Weekly: b.KeepWeekly, Monthly: b.KeepMonthly}}
		}
		e := ManualEvent(policy, loc, now)
		if e.ExpiresAt.Before(until) {
			e.ExpiresAt = until
		}
		if err := a.snapshotLocked(context.Background(), policy, e); err != nil {
			return err
		}
		name := binding.Source + "@" + SnapshotPrefix + e.PlannedAt.UTC().Format("20060102T150405Z")
		var source *Snapshot
		for i := range a.state.Snapshots {
			if a.state.Snapshots[i].Name == name {
				source = &a.state.Snapshots[i]
				// A scheduled snapshot in the same second may already exist.
				if source.ExpiresAt.Before(until) {
					source.ExpiresAt = until
				}
				break
			}
		}
		if source == nil {
			return errors.New("备份源快照创建后未找到")
		}
		remote := makeReplica(b, binding, *source, backupReplicaExpiry(b, loc, now))
		pending = append(pending, remote)
		mode := "full"
		if common {
			mode = "incremental"
		} else {
			base = ""
		}
		bases[binding.Source] = source.Name
		completed[binding.Source] = now
		transfers[binding.Source] = TransferResult{Mode: mode, Base: base, RemoteSnapshot: remote.Name, NextBackupAt: next, ProtectedUntil: until}
		totalBytes += mockBytes(b.ID + binding.Source)
	}
	// Receiver snapshots represent a completed recv with chrona:expires set.
	// Only after that has succeeded do we advance the common bases and release holds.
	for _, pendingReplica := range pending {
		replaced := false
		for i, r := range a.state.Replicas {
			if r.Host == pendingReplica.Host && r.Port == pendingReplica.Port && r.Name == pendingReplica.Name {
				if r.ExpiresAt.After(pendingReplica.ExpiresAt) {
					pendingReplica.ExpiresAt = r.ExpiresAt
				}
				a.state.Replicas[i] = pendingReplica
				replaced = true
				break
			}
		}
		if !replaced {
			a.state.Replicas = append(a.state.Replicas, pendingReplica)
		}
	}
	a.state.Backups[b.ID] = BackupState{Status: "success", Progress: 100, LastSuccess: now, LastSnapshot: bases[b.Datasets[0].Source], Bases: bases, Completed: completed, Transfers: transfers, Bytes: totalBytes}
	a.syncBackupHoldsLocked()
	a.pruneReplicasLocked(now)
	a.record("success", "backup", backupSources(b), fmt.Sprintf("模拟传输完成 · %d 个叶子数据集 · 基点至少保留至 %s", len(b.Datasets), until.Format("01-02 15:04")))
	a.persistLocked()
	return nil
}

func withoutHold(holds []string, id string) []string {
	result := []string{}
	for _, h := range holds {
		if h != id {
			result = append(result, h)
		}
	}
	return result
}

// Native ZFS holds are modeled separately from the single expiry property.
func (a *App) syncBackupHoldsLocked() {
	for _, b := range a.config.Backups {
		bases := a.state.Backups[b.ID].Bases
		effective := a.config.EffectiveBackup(b, a.datasets...).Datasets
		for i := range a.state.Snapshots {
			s := &a.state.Snapshots[i]
			s.HeldBy = withoutHold(s.HeldBy, b.ID)
			if bases[s.Dataset] == s.Name {
				s.HeldBy = append(s.HeldBy, b.ID)
			}
		}
		for i := range a.state.Replicas {
			r := &a.state.Replicas[i]
			r.HeldBy = withoutHold(r.HeldBy, b.ID)
			for _, binding := range effective {
				if binding.Destination == r.Dataset && b.Host == r.Host && b.Port == r.Port && bases[binding.Source] == r.SourceSnapshot {
					r.HeldBy = append(r.HeldBy, b.ID)
					break
				}
			}
		}
	}
}

// Changing the schedule must not leave the common base expiring before the new run.
func (a *App) extendBackupProtectionLocked(now time.Time) {
	if a.opts.Mode != "demo" {
		return
	}
	loc, _ := time.LoadLocation(a.config.Timezone)
	for _, task := range a.config.Backups {
		b := a.config.EffectiveBackup(task, a.datasets...)
		next, until := backupProtection(b, loc, now)
		state := a.state.Backups[b.ID]
		for _, binding := range b.Datasets {
			base := state.Bases[binding.Source]
			if base == "" {
				continue
			}
			for i := range a.state.Snapshots {
				s := &a.state.Snapshots[i]
				if s.Name == base && s.ExpiresAt.Before(until) {
					s.ExpiresAt = until
				}
			}
			for i := range a.state.Replicas {
				r := &a.state.Replicas[i]
				if r.Host == b.Host && r.Port == b.Port && r.Dataset == binding.Destination && r.SourceSnapshot == base && r.ExpiresAt.Before(until) {
					r.ExpiresAt = until
				}
			}
			if result, ok := state.Transfers[binding.Source]; ok {
				result.NextBackupAt = next
				result.ProtectedUntil = until
				state.Transfers[binding.Source] = result
			}
		}
		a.state.Backups[b.ID] = state
	}
}
