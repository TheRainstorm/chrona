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
		s, e := snapshotMetadata(f[0], f[5], f[1], f[2], f[3], f[4])
		if e != nil {
			continue
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

func destroyRealSnapshot(ctx context.Context, s Snapshot) (bool, error) {
	return (nativeEndpoint{}).destroyExpiredSnapshot(ctx, s, time.Now())
}
