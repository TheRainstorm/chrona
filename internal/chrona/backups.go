package chrona

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type ResolvedBackup struct {
	Task    Backup
	Binding BackupDataset
	Origin  string
}

// Path nodes may own defaults, but only actual leaves are transfer sources.
func (c Config) IsLeafDataset(name string, inventory ...Dataset) bool {
	prefix := name + "/"
	for _, d := range inventory {
		if strings.HasPrefix(d.Name, prefix) {
			return false
		}
	}
	for _, d := range c.Datasets {
		if strings.HasPrefix(d.Name, prefix) {
			return false
		}
	}
	return true
}

// The nearest level with explicit destinations overrides all inherited targets.
func (c Config) ResolveBackups(name string) []ResolvedBackup {
	byName := c.datasetIndex()
	for candidate := name; candidate != ""; candidate = parentDataset(candidate) {
		if byName[candidate].BackupMode == "none" {
			return nil
		}
		result := []ResolvedBackup{}
		for _, task := range c.Backups {
			for _, binding := range task.Datasets {
				if binding.Source != candidate {
					continue
				}
				relative := strings.TrimPrefix(name, candidate)
				result = append(result, ResolvedBackup{Task: task, Binding: BackupDataset{Source: name, Destination: binding.Destination + relative, Disabled: binding.Disabled}, Origin: candidate})
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return nil
}

func (c Config) EffectiveBackup(task Backup, inventory ...Dataset) Backup {
	result := task
	result.Datasets = []BackupDataset{}
	for _, d := range c.ManagedDatasets(inventory...) {
		if !c.IsLeafDataset(d.Name, inventory...) {
			continue
		}
		for _, resolved := range c.ResolveBackups(d.Name) {
			if resolved.Task.ID == task.ID {
				result.Datasets = append(result.Datasets, resolved.Binding)
			}
		}
	}
	return result
}

func backupGrace(b Backup) time.Duration {
	minutes := b.GraceMinutes
	if minutes == 0 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

func backupProtection(b Backup, loc *time.Location, now time.Time) (next, until time.Time) {
	next = NextBackup(b, loc, now)
	until = next.Add(backupGrace(b))
	return
}

func backupReplicaExpiry(b Backup, loc *time.Location, now time.Time) time.Time {
	_, expiry := backupProtection(b, loc, now)
	var retention time.Time
	if b.Cadence == "monthly" {
		retention = addMonthsClamped(now.In(loc), b.KeepMonthly)
	} else {
		retention = now.AddDate(0, 0, b.KeepWeekly*7)
	}
	if retention.After(expiry) {
		expiry = retention
	}
	return expiry
}

func makeReplica(b Backup, binding BackupDataset, source Snapshot, expiry time.Time) ReplicaSnapshot {
	suffix := strings.SplitN(source.Name, "@", 2)[1]
	return ReplicaSnapshot{Snapshot: Snapshot{Name: binding.Destination + "@" + suffix, Dataset: binding.Destination, CreatedAt: source.CreatedAt, ExpiresAt: expiry, Managed: true, HeldBy: []string{b.ID}, Used: source.Used}, TaskID: b.ID, Host: b.Host, Port: b.Port, SourceSnapshot: source.Name}
}

func findReplica(replicas []ReplicaSnapshot, b Backup, binding BackupDataset, base string) *ReplicaSnapshot {
	for _, r := range replicas {
		if r.Host == b.Host && r.Port == b.Port && r.Dataset == binding.Destination && r.SourceSnapshot == base {
			copy := r
			return &copy
		}
	}
	return nil
}

// Old demo fixtures had source bases without simulated receiver snapshots.
// Initialize once; an explicitly empty receiver is never reconstructed on restart.
func initializeDemoReplicas(c Config, s *State, inventory []Dataset, now time.Time) {
	if s.Replicas != nil {
		return
	}
	s.Replicas = []ReplicaSnapshot{}
	loc, _ := time.LoadLocation(c.Timezone)
	for _, task := range c.Backups {
		b := c.EffectiveBackup(task, inventory...)
		for _, binding := range b.Datasets {
			base := s.Backups[b.ID].Bases[binding.Source]
			if base == "" {
				continue
			}
			for i := range s.Snapshots {
				source := &s.Snapshots[i]
				if source.Name != base {
					continue
				}
				_, until := backupProtection(b, loc, now)
				if source.ExpiresAt.Before(until) {
					source.ExpiresAt = until
				}
				s.Replicas = append(s.Replicas, makeReplica(b, binding, *source, backupReplicaExpiry(b, loc, now)))
				break
			}
		}
	}
}

// Show the inherited targets that would apply after clearing this level.
func (a *App) handleBackupInheritance(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("dataset")
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !datasetPattern.MatchString(name) || !a.config.IsManagedDataset(name) {
		fail(w, 404, errors.New("数据集尚未纳入管理"))
		return
	}
	c := NormalizeConfig(a.config)
	for i := range c.Datasets {
		if c.Datasets[i].Name == name {
			c.Datasets[i].BackupMode = ""
		}
	}
	for i := range c.Backups {
		bindings := []BackupDataset{}
		for _, binding := range c.Backups[i].Datasets {
			if binding.Source != name {
				bindings = append(bindings, binding)
			}
		}
		c.Backups[i].Datasets = bindings
	}
	items := []map[string]any{}
	for _, resolved := range c.ResolveBackups(name) {
		items = append(items, map[string]any{"task": resolved.Task, "origin": resolved.Origin, "destination": resolved.Binding.Destination, "disabled": resolved.Binding.Disabled})
	}
	writeJSON(w, 200, map[string]any{"dataset": name, "items": items})
}
