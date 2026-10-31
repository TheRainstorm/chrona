package chrona

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func zfsCommand(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", name, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func realInventory(ctx context.Context) ([]Pool, []Dataset, []Snapshot, error) {
	poolText, err := zfsCommand(ctx, "zpool", "list", "-Hp", "-o", "name,size,alloc,free,health")
	if err != nil {
		return nil, nil, nil, err
	}
	var pools []Pool
	for _, line := range strings.Split(strings.TrimSpace(poolText), "\n") {
		f := strings.Fields(line)
		if len(f) != 5 {
			continue
		}
		size, _ := strconv.ParseUint(f[1], 10, 64)
		used, _ := strconv.ParseUint(f[2], 10, 64)
		free, _ := strconv.ParseUint(f[3], 10, 64)
		pools = append(pools, Pool{f[0], "ZFS pool", f[4], size, used, free})
	}
	text, err := zfsCommand(ctx, "zfs", "list", "-Hp", "-t", "filesystem,volume", "-o", "name,type,used,available")
	if err != nil {
		return nil, nil, nil, err
	}
	var datasets []Dataset
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		used, _ := strconv.ParseUint(f[2], 10, 64)
		available, _ := strconv.ParseUint(f[3], 10, 64)
		datasets = append(datasets, Dataset{f[0], f[1], used, available})
	}
	text, err = zfsCommand(ctx, "zfs", "list", "-Hp", "-t", "snapshot", "-o", "name,creation,used,"+ExpiryProperty+",userrefs,guid")
	if err != nil {
		return nil, nil, nil, err
	}
	var snapshots []Snapshot
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 {
			continue
		}
		parts := strings.SplitN(f[0], "@", 2)
		if len(parts) != 2 {
			continue
		}
		creation, e := strconv.ParseInt(f[1], 10, 64)
		if e != nil {
			continue
		}
		used, _ := strconv.ParseUint(f[2], 10, 64)
		expires, propErr := strconv.ParseInt(f[3], 10, 64)
		managed := strings.HasPrefix(parts[1], SnapshotPrefix) && propErr == nil && expires > 0
		s := Snapshot{Name: f[0], GUID: f[5], Dataset: parts[0], CreatedAt: time.Unix(creation, 0), Used: used, HeldBy: []string{}, Managed: managed}
		if managed {
			s.ExpiresAt = time.Unix(expires, 0)
		}
		if f[4] != "0" && f[4] != "-" {
			s.HeldBy = []string{"ZFS hold"}
		}
		snapshots = append(snapshots, s)
	}
	sortSnapshots(snapshots)
	return pools, datasets, snapshots, nil
}

// The one and only custom snapshot property is the absolute expiration timestamp.
func createRealSnapshot(ctx context.Context, dataset string, event Event) (string, error) {
	if !datasetPattern.MatchString(dataset) {
		return "", errors.New("dataset 名称无效")
	}
	name := dataset + "@" + SnapshotPrefix + event.PlannedAt.UTC().Format("20060102T150405Z")
	_, err := zfsCommand(ctx, "zfs", "snapshot", "-o", ExpiryProperty+"="+strconv.FormatInt(event.ExpiresAt.Unix(), 10), name)
	return name, err
}

func destroyRealSnapshot(ctx context.Context, s Snapshot) error {
	parts := strings.SplitN(s.Name, "@", 2)
	if len(parts) != 2 || !datasetPattern.MatchString(parts[0]) || !strings.HasPrefix(parts[1], SnapshotPrefix) || !s.Managed || len(s.HeldBy) > 0 {
		return errors.New("快照不满足清理条件")
	}
	// No -r/-R: child policies and manual snapshots are outside this operation.
	_, err := zfsCommand(ctx, "zfs", "destroy", s.Name)
	return err
}
