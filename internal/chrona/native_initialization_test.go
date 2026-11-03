package chrona

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFirstBackupBaseCheckRestoresExpiredSnapshotCleanup(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("ssh=", remote), func(t *testing.T) {
			a := testApp(t)
			p := a.config.EffectivePolicies()[0]
			b := a.config.Backups[0]
			b.Host, b.Port = "local", 0
			endpoint := nativeEndpoint{}
			if remote {
				b.Host, b.Port = "test-peer", 2202
				endpoint = nativeEndpoint{host: b.Host, port: b.Port}
			}
			binding := BackupDataset{Source: p.Dataset, Destination: "backup/target"}
			b.Datasets = []BackupDataset{binding}
			a.config.Backups = []Backup{b}
			a.state.Backups = map[string]BackupState{b.ID: {}}
			a.state.Replicas = nil
			now := time.Now()
			old := now.Add(-40 * 24 * time.Hour)
			base := Snapshot{Name: p.Dataset + "@imported-base", Dataset: p.Dataset, GUID: "42", CreatedAt: old}
			expired := Snapshot{Name: p.Dataset + "@chrona_20261102T120000Z", Dataset: p.Dataset, GUID: "43", CreatedAt: now.Add(-6 * time.Hour), Managed: true, ExpiresAt: now.Add(-time.Hour)}
			keep := Snapshot{Name: p.Dataset + "@KEEP-forever", Dataset: p.Dataset, CreatedAt: old}
			fresh := Snapshot{Name: p.Dataset + "@recent", Dataset: p.Dataset, CreatedAt: now}
			a.state.Snapshots = []Snapshot{base, expired, keep, fresh}
			a.pruneSnapshotDatasetLocked(context.Background(), p.Dataset)
			if len(a.state.Snapshots) != 4 {
				t.Fatal("unchecked receiver lost a possible imported base")
			}
			dir, log := retentionCommands(t)
			receiverName := binding.Destination + "@renamed-base"
			writeRetentionFixture(t, filepath.Join(dir, "list"), metadataLine(base.Name, base.GUID, old, "-", "0")+metadataLine(expired.Name, expired.GUID, expired.CreatedAt, fmt.Sprint(expired.ExpiresAt.Unix()), "0")+metadataLine(receiverName, base.GUID, old, "-", "0"))
			result, err := discoverNativeBackupBase(context.Background(), endpoint, b, binding)
			if err != nil || result.source == nil || result.source.Name != base.Name || result.replica.Name != receiverName {
				t.Fatal("common GUID not discovered", result, err)
			}
			calls, _ := os.ReadFile(log)
			for _, name := range []string{base.Name, receiverName} {
				if !strings.Contains(string(calls), "hold\thold\t"+nativeTag(b.ID)+"\t"+name+"\n") {
					t.Fatal("base not held at both endpoints", name, string(calls))
				}
			}
			if !a.commitNativeBackupBaseLocked(pendingBackupBase{b, binding}, result) {
				t.Fatal("could not commit verified base")
			}
			a.pruneSnapshotDatasetLocked(context.Background(), p.Dataset)
			seen := map[string]bool{}
			for _, s := range a.state.Snapshots {
				seen[s.Name] = true
			}
			if seen[expired.Name] || !seen[base.Name] || !seen[keep.Name] || !seen[fresh.Name] {
				t.Fatal("expired hourly snapshot or protected base handled incorrectly", seen)
			}
			state := a.state.Backups[b.ID]
			if a.hasUninitializedBackupLocked(p.Dataset) || state.Status == "success" || !state.LastSuccess.IsZero() || len(state.Completed) != 0 || state.LastSnapshot != "" {
				t.Fatal("base discovery claimed a successful transfer", state)
			}
			if !a.protectedSnapshotNamesLocked()[receiverName] {
				t.Fatal("actual renamed receiver base was not protected")
			}
			a.persistLocked()
			reloaded, err := NewApp(a.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer reloaded.Close()
			if reloaded.state.Backups[b.ID].Bases[p.Dataset] != base.Name {
				t.Fatal("discovered base lost on restart")
			}
			before, _ := os.ReadFile(log)
			if w := perform(a, "GET", "/api/state", ""); w.Code != 200 {
				t.Fatal(w.Code)
			}
			after, _ := os.ReadFile(log)
			if string(before) != string(after) {
				t.Fatal("page refresh performed SSH metadata I/O")
			}
		})
	}
}

func TestCheckedReceiverWithoutUsableBaseDoesNotDeferCleanup(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint("mismatch=", mismatch), func(t *testing.T) {
			a := testApp(t)
			p := a.config.EffectivePolicies()[0]
			b := a.config.Backups[0]
			b.Host, b.Port = "local", 0
			binding := BackupDataset{Source: p.Dataset, Destination: "backup/empty"}
			b.Datasets = []BackupDataset{binding}
			a.config.Backups = []Backup{b}
			a.state.Backups = map[string]BackupState{b.ID: {}}
			old := time.Now().Add(-40 * 24 * time.Hour)
			a.state.Snapshots = []Snapshot{{Name: p.Dataset + "@expired", Dataset: p.Dataset, GUID: "42", CreatedAt: old}}
			dir, _ := retentionCommands(t)
			list := ""
			if mismatch {
				list = metadataLine(binding.Destination+"@keep-unrelated", "999", old, "-", "0") + metadataLine(p.Dataset+"@expired", "42", old, "-", "0")
			}
			writeRetentionFixture(t, filepath.Join(dir, "list"), list)
			result, err := discoverNativeBackupBase(context.Background(), nativeEndpoint{}, b, binding)
			if err != nil || result.source != nil || (result.warning != "") != mismatch {
				t.Fatal(result, err)
			}
			if !a.commitNativeBackupBaseLocked(pendingBackupBase{b, binding}, result) || a.hasUninitializedBackupLocked(p.Dataset) {
				t.Fatal("checked empty receiver still deferred cleanup")
			}
			a.pruneSnapshotDatasetLocked(context.Background(), p.Dataset)
			if len(a.state.Snapshots) != 0 {
				t.Fatal("new or incompatible target disabled expiry pruning")
			}
			c := NormalizeConfig(a.config)
			c.BackupPolicies[0].At = "12:34"
			if err := a.applyConfigLocked(c); err != nil || a.hasUninitializedBackupLocked(p.Dataset) {
				t.Fatal("time-only edit lost checked empty receiver", err)
			}
			c = NormalizeConfig(a.config)
			c.Backups[0].Datasets[0].Destination = "backup/different"
			if err := a.applyConfigLocked(c); err != nil || !a.hasUninitializedBackupLocked(p.Dataset) {
				t.Fatal("target edit reused old receiver check", err)
			}
		})
	}
}

func TestFailedBaseCheckRetriesWithoutUnlockingCleanup(t *testing.T) {
	a := testApp(t)
	p := a.config.EffectivePolicies()[0]
	b := a.config.Backups[0]
	b.Datasets = []BackupDataset{{Source: p.Dataset, Destination: "backup/offline"}}
	a.config.Backups = []Backup{b}
	a.state.Backups = map[string]BackupState{b.ID: {}}
	dir, log := retentionCommands(t)
	// The missing fixture returns an error rather than a verified empty list.
	if err := os.Remove(filepath.Join(dir, "list")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	a.opts.Mode, a.opts.AllowWrites = "zfs", true
	now := time.Now()
	a.mu.Lock()
	a.scheduleNativeBaseInitializationLocked(now)
	a.mu.Unlock()
	a.workers.Wait()
	if !a.hasUninitializedBackupLocked(p.Dataset) || !strings.HasPrefix(a.state.Backups[b.ID].Warning, baseCheckWarning) {
		t.Fatal("failed probe treated as an empty receiver", a.state.Backups[b.ID])
	}
	before, _ := os.ReadFile(log)
	a.scheduleNativeBaseInitializationLocked(now.Add(time.Minute))
	a.workers.Wait()
	after, _ := os.ReadFile(log)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("metadata failure retried without backoff")
	}
}

func TestUnfinishedReceiveKeepsSourceCleanupDeferred(t *testing.T) {
	a := testApp(t)
	p := a.config.EffectivePolicies()[0]
	b := a.config.Backups[0]
	binding := BackupDataset{Source: p.Dataset, Destination: "backup/resuming"}
	b.Datasets = []BackupDataset{binding}
	a.config.Backups = []Backup{b}
	a.state.Backups = map[string]BackupState{b.ID: {}}
	dir, log := retentionCommands(t)
	command := filepath.Join(dir, "zfs")
	script, err := os.ReadFile(command)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "get) printf '%s\\n' '-';;", "get) printf '%s\\n' 'resume-token';;", 1))
	if err := os.WriteFile(command, script, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = discoverNativeBackupBase(context.Background(), nativeEndpoint{}, b, binding)
	if err == nil || !strings.Contains(err.Error(), "续传") || !a.hasUninitializedBackupLocked(p.Dataset) {
		t.Fatal("unfinished receive did not defer base discovery", err)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "hold\thold") || strings.Contains(string(calls), "destroy\tdestroy") {
		t.Fatal("unfinished receive changed snapshots", string(calls))
	}
}
