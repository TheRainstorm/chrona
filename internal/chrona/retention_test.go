package chrona

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSnapshotRetentionRules(t *testing.T) {
	now := mustTime("2026-11-02T00:00:00Z")
	old := now.Add(-31 * 24 * time.Hour)
	tests := []struct {
		name, creation, expiry, refs, source string
		permanent, due                       bool
	}{
		{"tank/data@legacy", fmt.Sprint(old.Unix()), "-", "0", "default", false, true},
		{"tank/data@recent", fmt.Sprint(now.Add(-29 * 24 * time.Hour).Unix()), "-", "0", "default", false, false},
		{"tank/data@legacy-property", fmt.Sprint(now.Add(-time.Hour).Unix()), fmt.Sprint(now.Add(-time.Minute).Unix()), "0", "property", false, true},
		{"tank/data@future-property", fmt.Sprint(old.Unix()), fmt.Sprint(now.Add(time.Hour).Unix()), "0", "property", false, false},
		{"tank/data@preKeep-release", fmt.Sprint(old.Unix()), fmt.Sprint(now.Add(-time.Hour).Unix()), "0", "keep", true, false},
		{"tank/data@KEEP", fmt.Sprint(old.Unix()), "-", "0", "keep", true, false},
		{"tank/keep-data@legacy", fmt.Sprint(old.Unix()), "-", "0", "default", false, true},
		{"tank/data@held", fmt.Sprint(old.Unix()), "-", "1", "default", false, false},
		{"tank/data@unknown-hold", fmt.Sprint(old.Unix()), "-", "-", "default", false, false},
		{"tank/data@invalid-expiry", fmt.Sprint(old.Unix()), "not-a-date", "0", "default", false, true},
		{"tank/data@zero-expiry", fmt.Sprint(old.Unix()), "0", "0", "default", false, true},
		{"tank/data@invalid-creation", "garbage", "-", "0", "unknown", false, false},
		{"tank/data@zero-creation", "0", "-", "0", "unknown", false, false},
		{"tank/data@invalid-both", "garbage", "garbage", "0", "unknown", false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, err := snapshotMetadata(test.name, "7", test.creation, "100", test.expiry, test.refs)
			if err != nil {
				t.Fatal(err)
			}
			if s.RetentionSource != test.source || s.Permanent != test.permanent || snapshotExpired(s, now) != test.due {
				t.Fatalf("unexpected retention: %+v due=%v", s, snapshotExpired(s, now))
			}
			if test.source == "default" && !s.ExpiresAt.Equal(s.CreatedAt.Add(30*24*time.Hour)) {
				t.Fatal("legacy expiry not based on creation")
			}
			if test.permanent && !s.ExpiresAt.IsZero() {
				t.Fatal("permanent snapshot exposes finite expiry")
			}
			if test.source == "property" && !s.Managed {
				t.Fatal("explicit expiry on legacy name not recognized")
			}
		})
	}
}

func TestRetentionReadsNormalizeDemoWithoutChangingStoredSnapshots(t *testing.T) {
	a := testApp(t)
	now := time.Now()
	a.state.Snapshots = []Snapshot{
		{Name: "Saturn/Archive@legacy", Dataset: "Saturn/Archive", CreatedAt: now.Add(-40 * 24 * time.Hour)},
		{Name: "Saturn/Archive@KEEP-important", Dataset: "Saturn/Archive", CreatedAt: now.Add(-time.Hour), Managed: true, ExpiresAt: now.Add(-time.Minute)},
	}
	response := perform(a, "GET", "/api/snapshots?dataset=Saturn%2FArchive", "")
	var out struct {
		Items []Snapshot `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || !out.Items[0].Permanent || out.Items[1].RetentionSource != "default" {
		t.Fatalf("retention missing from API: %+v", out)
	}
	response = perform(a, "GET", "/api/state", "")
	var state struct {
		Rows []struct {
			Config DatasetConfig `json:"config"`
			Latest Snapshot      `json:"latest_snapshot"`
		} `json:"dataset_rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range state.Rows {
		if row.Config.Name == "Saturn/Archive" {
			found = true
			if !row.Latest.Permanent || row.Latest.RetentionSource != "keep" {
				t.Fatal("latest snapshot not normalized", row.Latest)
			}
		}
	}
	if !found {
		t.Fatal("dataset missing")
	}
	if !a.state.Snapshots[0].ExpiresAt.IsZero() || a.state.Snapshots[1].ExpiresAt.IsZero() {
		t.Fatal("read mutated original state")
	}
}

func TestLegacyCleanupKeepsExactScopeHoldsAndPersistedBases(t *testing.T) {
	a := testApp(t)
	p := a.config.EffectivePolicies()[0]
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour)
	b := a.config.Backups[0]
	b.Datasets = []BackupDataset{{Source: p.Dataset, Destination: "backup/target"}}
	a.config.Backups = []Backup{b}
	base := p.Dataset + "@old-base"
	a.state.Backups[b.ID] = BackupState{Bases: map[string]string{p.Dataset: base}}
	a.state.Snapshots = []Snapshot{
		{Name: p.Dataset + "@expired-legacy", Dataset: p.Dataset, CreatedAt: old},
		{Name: p.Dataset + "@recent-legacy", Dataset: p.Dataset, CreatedAt: now.Add(-24 * time.Hour)},
		{Name: p.Dataset + "@manual-expiry", Dataset: p.Dataset, CreatedAt: now, Managed: true, ExpiresAt: now.Add(-time.Hour)},
		{Name: p.Dataset + "@chrona_keep-forever", Dataset: p.Dataset, CreatedAt: old, Managed: true, ExpiresAt: now.Add(-time.Hour)},
		{Name: p.Dataset + "@foreign-held", Dataset: p.Dataset, CreatedAt: old, HeldBy: []string{"external"}},
		{Name: p.Dataset + "/child@expired-legacy", Dataset: p.Dataset + "/child", CreatedAt: old},
		{Name: base, Dataset: p.Dataset, CreatedAt: old},
		{Name: p.Dataset + "@unknown-age", Dataset: p.Dataset},
	}
	e := Event{PlannedAt: now, RunAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := a.snapshotLocked(context.Background(), p, e); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range a.state.Snapshots {
		got[s.Name] = true
	}
	for _, suffix := range []string{"expired-legacy", "manual-expiry"} {
		if got[p.Dataset+"@"+suffix] {
			t.Fatal("expired snapshot retained", suffix)
		}
	}
	for _, name := range []string{base, p.Dataset + "@recent-legacy", p.Dataset + "@chrona_keep-forever", p.Dataset + "@foreign-held", p.Dataset + "@unknown-age", p.Dataset + "/child@expired-legacy"} {
		if !got[name] {
			t.Fatal("protected snapshot removed", name)
		}
	}
	if !a.protectedSnapshotNamesLocked()["backup/target@old-base"] {
		t.Fatal("receiver base not protected")
	}
	// A second configured destination without a known common base defers cleanup.
	other := b
	other.ID = "new-peer"
	other.Datasets = []BackupDataset{{Source: p.Dataset, Destination: "backup/new-target"}}
	a.config.Backups = append(a.config.Backups, other)
	pending := Snapshot{Name: p.Dataset + "@unknown-peer-base", Dataset: p.Dataset, CreatedAt: old}
	a.state.Snapshots = append(a.state.Snapshots, pending)
	e.PlannedAt = now.Add(time.Second)
	if err := a.snapshotLocked(context.Background(), p, e); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range a.state.Snapshots {
		found = found || s.Name == pending.Name
	}
	if !found {
		t.Fatal("first backup common seed deleted before identification")
	}
}

// A small command fixture exercises both local argv and quoted SSH transport.
func retentionCommands(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf '%s' "$1" >> "$CHRONA_RETENTION_LOG"
for arg in "$@"; do last="$arg"; printf '\t%s' "$arg" >> "$CHRONA_RETENTION_LOG"; done
printf '\n' >> "$CHRONA_RETENTION_LOG"
case "$1" in
 list)
  case "$last" in *@*) /bin/cat "$CHRONA_RETENTION_FIXTURE/exact-${last##*@}";; *) /bin/cat "$CHRONA_RETENTION_FIXTURE/list";; esac
  ;;
 holds) printf '%s\tforeign\t1\n' "$last";;
 get) printf '%s\n' '-';;
 destroy|release|set) ;;
 *) exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "zfs"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ssh := `#!/bin/sh
for arg in "$@"; do last="$arg"; done
exec /bin/sh -c "$last"
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(ssh), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("CHRONA_RETENTION_LOG", log)
	t.Setenv("CHRONA_RETENTION_FIXTURE", dir)
	return dir, log
}
func writeRetentionFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}
func metadataLine(name, guid string, created time.Time, expiry string, refs string) string {
	return fmt.Sprintf("%s\t%s\t%d\t0\t%s\t%s\n", name, guid, created.Unix(), expiry, refs)
}

func TestExactDeletionRevalidatesGUIDExpiryAndHolds(t *testing.T) {
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour)
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("ssh=", remote), func(t *testing.T) {
			dir, log := retentionCommands(t)
			e := nativeEndpoint{}
			if remote {
				e = nativeEndpoint{host: "test-peer", port: 2202}
			}
			expected := Snapshot{Name: "tank/src@legacy", Dataset: "tank/src", GUID: "123", CreatedAt: old}
			cases := []struct {
				name, guid, expiry, refs string
				deleted                  bool
			}{
				{"expired-default", "123", "-", "0", true},
				{"recreated", "456", "-", "0", false},
				{"retention-extended", "123", fmt.Sprint(now.Add(time.Hour).Unix()), "0", false},
				{"hold-added", "123", "-", "1", false},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					writeRetentionFixture(t, filepath.Join(dir, "exact-legacy"), metadataLine(expected.Name, test.guid, old, test.expiry, test.refs))
					writeRetentionFixture(t, log, "")
					deleted, err := e.destroyExpiredSnapshot(context.Background(), expected, now)
					if err != nil || deleted != test.deleted {
						t.Fatal(deleted, err)
					}
					calls, _ := os.ReadFile(log)
					hasDestroy := strings.Contains(string(calls), "destroy\tdestroy\ttank/src@legacy\n")
					if hasDestroy != test.deleted {
						t.Fatal("unexpected destroy", string(calls))
					}
					if strings.Contains(string(calls), "\t-r\t") || strings.Contains(string(calls), "\t-R\t") || strings.Contains(string(calls), "\t-f\t") {
						t.Fatal("recursive or forced delete")
					}
				})
			}
			writeRetentionFixture(t, filepath.Join(dir, "exact-legacy"), fmt.Sprintf("tank/src@legacy\t123\tinvalid\t0\t%d\t0\n", now.Add(-time.Hour).Unix()))
			if deleted, err := e.destroyExpiredSnapshot(context.Background(), expected, now); err != nil || deleted {
				t.Fatal("invalid creation metadata was not protected", deleted, err)
			}
			writeRetentionFixture(t, log, "")
			kept := expected
			kept.Name = "tank/src@KEEP-legacy"
			if deleted, err := e.destroyExpiredSnapshot(context.Background(), kept, now); err == nil || deleted {
				t.Fatal("keep deletion accepted")
			}
			calls, _ := os.ReadFile(log)
			if len(calls) != 0 {
				t.Fatal("keep issued commands")
			}
			if err := e.expiry(context.Background(), kept.Name, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			calls, _ = os.ReadFile(log)
			if len(calls) != 0 {
				t.Fatal("keep received finite expiry property")
			}
		})
	}
}

func TestNativeCleanupKeepsCurrentLatestForeignHoldsAndOtherBases(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("ssh=", remote), func(t *testing.T) {
			dir, log := retentionCommands(t)
			e := nativeEndpoint{}
			if remote {
				e = nativeEndpoint{host: "test-peer", port: 2202}
			}
			now := time.Now()
			old := now.Add(-40 * 24 * time.Hour)
			names := []string{"legacy", "keep-forever", "foreign-held", "other-job-base", "current", "latest"}
			var lines []string
			for i, suffix := range names {
				refs := "0"
				if suffix == "foreign-held" {
					refs = "1"
				}
				line := metadataLine("tank/src@"+suffix, fmt.Sprint(i+1), old, "-", refs)
				lines = append(lines, line)
				writeRetentionFixture(t, filepath.Join(dir, "exact-"+suffix), line)
			}
			// A recursive command result must not leak child snapshots into the exact scope.
			lines = append(lines, metadataLine("tank/src/child@legacy-child", "99", old, "-", "0"))
			writeRetentionFixture(t, filepath.Join(dir, "list"), strings.Join(lines, ""))
			if err := cleanupNativeSnapshots(context.Background(), e, "tank/src", "tank/src@current", "chrona:job", map[string]bool{"tank/src@other-job-base": true}, false); err != nil {
				t.Fatal(err)
			}
			calls, _ := os.ReadFile(log)
			var destroyed []string
			for _, line := range strings.Split(string(calls), "\n") {
				if strings.HasPrefix(line, "destroy\t") {
					destroyed = append(destroyed, line)
				}
			}
			if !reflect.DeepEqual(destroyed, []string{"destroy\tdestroy\ttank/src@legacy"}) {
				t.Fatal("wrong cleanup targets", destroyed, string(calls))
			}
			writeRetentionFixture(t, log, "")
			if err := cleanupNativeSnapshots(context.Background(), e, "tank/src", "tank/src@current", "chrona:job", nil, true); err != nil {
				t.Fatal(err)
			}
			calls, _ = os.ReadFile(log)
			if strings.Contains(string(calls), "destroy\t") {
				t.Fatal("deferred cleanup destroyed snapshots")
			}
		})
	}
}

func TestBaseProtectionNeverShortensLegacyDefaultLifetime(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("ssh=", remote), func(t *testing.T) {
			dir, log := retentionCommands(t)
			e := nativeEndpoint{}
			if remote {
				e = nativeEndpoint{host: "test-peer", port: 2202}
			}
			now := time.Now()
			name := "tank/src@legacy"
			writeRetentionFixture(t, filepath.Join(dir, "exact-legacy"), metadataLine(name, "9", now.Add(-time.Hour), "-", "0"))
			if err := e.expiry(context.Background(), name, now.Add(24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), "set\t") {
				t.Fatal("protection shortened default lifetime", string(calls))
			}
			until := now.Add(60 * 24 * time.Hour)
			if err := e.expiry(context.Background(), name, until); err != nil {
				t.Fatal(err)
			}
			calls, _ = os.ReadFile(log)
			if !strings.Contains(string(calls), fmt.Sprintf("set\tset\tchrona:expires=%d\ttank/src@legacy", until.Unix())) {
				t.Fatal("protection did not extend expiry", string(calls))
			}
		})
	}
}

func TestReplicaCleanupKeepsPermanentAndPersistedUnheldBase(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[0]
	binding := b.Datasets[0]
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour)
	sourceBase := binding.Source + "@current"
	a.state.Backups[b.ID] = BackupState{Bases: map[string]string{binding.Source: sourceBase}}
	a.state.Replicas = []ReplicaSnapshot{
		{Snapshot: Snapshot{Name: binding.Destination + "@current", Dataset: binding.Destination, CreatedAt: old}, TaskID: b.ID, Host: b.Host, Port: b.Port, SourceSnapshot: sourceBase},
		{Snapshot: Snapshot{Name: binding.Destination + "@KEEP-old", Dataset: binding.Destination, CreatedAt: old, Managed: true, ExpiresAt: old}, TaskID: b.ID},
		{Snapshot: Snapshot{Name: binding.Destination + "@legacy-expired", Dataset: binding.Destination, CreatedAt: old}, TaskID: b.ID},
	}
	a.pruneReplicasLocked(now)
	if len(a.state.Replicas) != 2 {
		t.Fatalf("wrong receiver cleanup count: %+v", a.state.Replicas)
	}
	for _, r := range a.state.Replicas {
		if strings.HasSuffix(r.Name, "legacy-expired") {
			t.Fatal("expired replica survived")
		}
	}
}
