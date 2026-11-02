package chrona

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const DefaultSnapshotRetention = 30 * 24 * time.Hour

// Retention is derived from the snapshot suffix, never its dataset path.
// No additional user properties are required for legacy or permanent snapshots.
func keepSnapshot(name string) bool {
	_, suffix, ok := strings.Cut(name, "@")
	return ok && strings.Contains(strings.ToLower(suffix), "keep")
}

func withSnapshotRetention(s Snapshot) Snapshot {
	s.Permanent = keepSnapshot(s.Name)
	if s.Permanent {
		s.RetentionSource = "keep"
		s.ExpiresAt = time.Time{}
	} else if s.Managed && !s.ExpiresAt.IsZero() && s.ExpiresAt.Unix() > 0 {
		s.RetentionSource = "property"
	} else if !s.CreatedAt.IsZero() && s.CreatedAt.Unix() > 0 {
		s.Managed = false
		s.RetentionSource = "default"
		s.ExpiresAt = s.CreatedAt.Add(DefaultSnapshotRetention)
	} else {
		s.Managed = false
		s.RetentionSource = "unknown"
		s.ExpiresAt = time.Time{}
	}
	return s
}

func snapshotExpired(s Snapshot, now time.Time) bool {
	s = withSnapshotRetention(s)
	return !s.Permanent && !s.ExpiresAt.IsZero() && !s.ExpiresAt.After(now) && len(s.HeldBy) == 0
}

func snapshotMetadata(name, guid, creation, used, expiry, refs string) (Snapshot, error) {
	dataset, suffix, ok := strings.Cut(name, "@")
	if !ok || suffix == "" || !datasetPattern.MatchString(dataset) || strings.ContainsAny(suffix, "@/") {
		return Snapshot{}, fmt.Errorf("快照名称无效：%s", name)
	}
	s := Snapshot{Name: name, Dataset: dataset, GUID: guid, HeldBy: []string{}}
	if created, err := strconv.ParseInt(creation, 10, 64); err == nil && created > 0 {
		s.CreatedAt = time.Unix(created, 0)
	}
	s.Used, _ = strconv.ParseUint(used, 10, 64)
	if unix, err := strconv.ParseInt(expiry, 10, 64); err == nil && unix > 0 {
		s.Managed = true
		s.ExpiresAt = time.Unix(unix, 0)
	}
	// Unknown userref data must not be interpreted as absence of holds.
	if refs != "0" {
		s.HeldBy = []string{"ZFS hold"}
	}
	return withSnapshotRetention(s), nil
}

// Re-read identity, retention and holds immediately before a single exact delete.
// ZFS itself rejects concurrent holds or dependent clones; never use -r/-R/-f/-d.
func (e nativeEndpoint) destroyExpiredSnapshot(ctx context.Context, expected Snapshot, now time.Time) (bool, error) {
	dataset, suffix, ok := strings.Cut(expected.Name, "@")
	if !ok || dataset != expected.Dataset || !datasetPattern.MatchString(dataset) || suffix == "" || strings.ContainsAny(suffix, "@/") || expected.GUID == "" || !snapshotExpired(expected, now) {
		return false, fmt.Errorf("快照不满足清理条件：%s", expected.Name)
	}
	out, err := e.run(ctx, "list", "-Hp", "-t", "snapshot", "-o", "name,guid,creation,used,"+ExpiryProperty+",userrefs", expected.Name)
	if missingDataset(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	fields := strings.Fields(out)
	if len(fields) != 6 || fields[0] != expected.Name {
		return false, fmt.Errorf("无法验证待清理快照：%s", expected.Name)
	}
	current, err := snapshotMetadata(fields[0], fields[1], fields[2], fields[3], fields[4], fields[5])
	if err != nil {
		return false, err
	}
	if current.GUID != expected.GUID || current.CreatedAt.IsZero() || !snapshotExpired(current, now) {
		return false, nil
	}
	if _, err = e.run(ctx, "destroy", expected.Name); err != nil {
		return false, err
	}
	return true, nil
}

// A persisted incremental base remains protected even if an external operation
// removed its ZFS hold. Include receiver names so local receiving datasets are safe.
func (a *App) protectedSnapshotNamesLocked() map[string]bool {
	protected := map[string]bool{}
	for _, task := range a.config.Backups {
		for _, binding := range task.Datasets {
			base := a.state.Backups[task.ID].Bases[binding.Source]
			if !strings.HasPrefix(base, binding.Source+"@") {
				continue
			}
			protected[base] = true
			protected[binding.Destination+"@"+strings.SplitN(base, "@", 2)[1]] = true
			for _, replica := range a.state.Replicas {
				if replica.TaskID == task.ID && sameBackupReceiver(replica.Host, replica.Port, task.Host, task.Port) && replica.Dataset == binding.Destination && replica.SourceSnapshot == base {
					protected[replica.Name] = true
				}
			}
		}
	}
	return protected
}

// Before the first successful backup, a peer may depend on an old unregistered
// snapshot. Defer source cleanup until each configured destination has a base.
func (a *App) hasUninitializedBackupLocked(dataset string) bool {
	if !a.config.IsLeafDataset(dataset, a.datasets...) {
		return false
	}
	for _, task := range a.config.Backups {
		for _, binding := range task.Datasets {
			if binding.Source == dataset && !strings.HasPrefix(a.state.Backups[task.ID].Bases[dataset], dataset+"@") {
				return true
			}
		}
	}
	return false
}

func replicaWithRetention(replica *ReplicaSnapshot) *ReplicaSnapshot {
	if replica == nil {
		return nil
	}
	copy := *replica
	copy.Snapshot = withSnapshotRetention(copy.Snapshot)
	return &copy
}
