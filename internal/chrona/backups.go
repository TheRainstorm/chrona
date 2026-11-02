package chrona

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

type ResolvedBackup struct {
	Task    Backup
	Binding BackupDataset
	Origin  string
}

// Hierarchy does not imply a plan; only explicitly assigned leaves are transfer sources.
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

// A backup plan is bound only to the dataset named in its source mapping.
// Hierarchy is presentation only; neither rules nor destination paths inherit.
func (c Config) ResolveBackups(name string) []ResolvedBackup {
	result := []ResolvedBackup{}
	for _, task := range c.Backups {
		for _, binding := range task.Datasets {
			if binding.Source == name {
				result = append(result, ResolvedBackup{Task: c.ResolveBackup(task), Binding: binding, Origin: name})
			}
		}
	}
	return result
}

func (c Config) EffectiveBackup(task Backup, inventory ...Dataset) Backup {
	result := c.ResolveBackup(task)
	result.Datasets = []BackupDataset{}
	for _, binding := range task.Datasets {
		if c.IsLeafDataset(binding.Source, inventory...) {
			result.Datasets = append(result.Datasets, binding)
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
	return expiry
}

// Backup bases have their own names and lifetime, independent of snapshot policies.
func backupSnapshotName(dataset, task string, now time.Time) string {
	hash := sha256.Sum256([]byte(task))
	return fmt.Sprintf("%s@%s%s_b%x_%x", dataset, SnapshotPrefix, now.UTC().Format("20060102T150405Z"), hash[:3], now.Nanosecond())
}

func makeReplica(b Backup, binding BackupDataset, source Snapshot, expiry time.Time) ReplicaSnapshot {
	suffix := strings.SplitN(source.Name, "@", 2)[1]
	return ReplicaSnapshot{Snapshot: Snapshot{Name: binding.Destination + "@" + suffix, Dataset: binding.Destination, CreatedAt: source.CreatedAt, ExpiresAt: expiry, Managed: true, HeldBy: []string{b.ID}, Used: source.Used}, TaskID: b.ID, Host: b.Host, Port: b.Port, SourceSnapshot: source.Name}
}

func findReplica(replicas []ReplicaSnapshot, b Backup, binding BackupDataset, base string) *ReplicaSnapshot {
	for _, r := range replicas {
		if sameBackupReceiver(r.Host, r.Port, b.Host, b.Port) && r.Dataset == binding.Destination && r.SourceSnapshot == base {
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
