package chrona

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestBackupLifetimeUsesNextActualRunOnly(t *testing.T) {
	for _, tc := range []struct {
		name, cadence, now, next string
	}{
		{"Monday to Friday", "weekly", "2026-11-02T05:45:00+08:00", "2026-11-06T05:45:00+08:00"},
		{"Friday to Monday", "weekly", "2026-11-06T05:45:01+08:00", "2026-11-09T05:45:00+08:00"},
		{"leap February", "monthly", "2028-01-31T05:45:00+08:00", "2028-02-29T05:45:00+08:00"},
		{"month end recovers", "monthly", "2028-02-29T05:45:00+08:00", "2028-03-31T05:45:00+08:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := customBackupForTest(DefaultConfig(time.Now()), 0)
			b.Cadence, b.MonthDay = tc.cadence, 31
			// Even legacy values cannot extend the new backup's lifetime.
			b.KeepWeekly, b.KeepMonthly = 1000, 1000
			now, next := mustTime(tc.now), mustTime(tc.next)
			actualNext, expiry := backupProtection(b, now.Location(), now)
			if !actualNext.Equal(next) || !expiry.Equal(next.Add(time.Hour)) || !backupReplicaExpiry(b, now.Location(), now).Equal(expiry) {
				t.Fatal("backup lifetime must follow the actual next run", actualNext, expiry)
			}
		})
	}
}

func TestLegacyBackupCountsAreIgnoredAndOmittedWithoutRewritingOnLoad(t *testing.T) {
	c := DefaultConfig(time.Now())
	if err := ValidateConfig(c); err != nil {
		t.Fatal("backup without retention counts is invalid", err)
	}
	c.Backups[0].KeepWeekly, c.Backups[0].KeepMonthly = -10, 10001
	legacy, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	legacy = append([]byte("# Preserve my original comments and obsolete input fields until I save.\n"), legacy...)
	dir := t.TempDir()
	opts := Options{Mode: "demo", ConfigPath: filepath.Join(dir, "config.yaml"), StatePath: filepath.Join(dir, "state.json")}
	if err := os.WriteFile(opts.ConfigPath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(opts)
	if err != nil {
		t.Fatal("old config failed to load", err)
	}
	after, err := os.ReadFile(opts.ConfigPath)
	if err != nil || string(after) != string(legacy) {
		t.Fatal("startup rewrote the user's config", err)
	}
	for _, b := range a.config.Backups {
		if b.KeepWeekly != 0 || b.KeepMonthly != 0 {
			t.Fatal("obsolete input was not normalized", b)
		}
	}
	w := perform(a, "GET", "/api/config", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "keep_weekly") || strings.Contains(w.Body.String(), "keep_monthly") {
		t.Fatal("obsolete counters leaked into API", w.Code, w.Body.String())
	}
	// The strict JSON input also remains compatible with old clients.
	payload, _ := json.Marshal(c)
	w = perform(a, "PUT", "/api/config", string(payload))
	if w.Code != 200 {
		t.Fatal("old JSON input rejected", w.Code, w.Body.String())
	}
	saved, err := os.ReadFile(opts.ConfigPath)
	if err != nil || strings.Contains(string(saved), "keep_weekly") || strings.Contains(string(saved), "keep_monthly") {
		t.Fatal("normalized save retained obsolete counters", err, string(saved))
	}
}

func TestBackupSnapshotsAreIndependentOfLongSnapshotPolicies(t *testing.T) {
	a := testApp(t)
	b := a.config.ResolveBackup(a.config.Backups[0])
	loc, _ := time.LoadLocation(a.config.Timezone)
	now := mustTime("2026-11-02T06:00:17+08:00")
	source := b.Datasets[0].Source
	p, _, ok := a.config.ResolveSnapshot(source)
	if !ok || p.Keep.Monthly < 12 {
		t.Fatal("fixture needs a long snapshot policy")
	}
	scheduled := Snapshot{Name: source + "@" + SnapshotPrefix + now.UTC().Format("20060102T150405Z"), Dataset: source, CreatedAt: now, ExpiresAt: now.AddDate(1, 0, 0), Managed: true}
	a.state.Snapshots = append(a.state.Snapshots, scheduled)
	if err := a.finishDemoBackupLocked(b, now); err != nil {
		t.Fatal(err)
	}
	first := a.state.Backups[b.ID].Bases[source]
	_, want := backupProtection(b, loc, now)
	for _, s := range a.state.Snapshots {
		if s.Name == first && (!s.ExpiresAt.Equal(want) || s.Name == scheduled.Name) {
			t.Fatal("backup reused snapshot-policy retention", s)
		}
		if s.Name == scheduled.Name && !s.ExpiresAt.Equal(scheduled.ExpiresAt) {
			t.Fatal("backup modified the scheduled snapshot")
		}
	}
	r := findReplica(a.state.Replicas, b, b.Datasets[0], first)
	if r == nil || !r.ExpiresAt.Equal(want) {
		t.Fatal("receiver lifetime differs from source backup lifetime", r)
	}
	// Manual repeats at the same instant must still create distinct bases.
	if err := a.finishDemoBackupLocked(b, now); err != nil {
		t.Fatal(err)
	}
	second := a.state.Backups[b.ID]
	if second.Bases[source] == first || second.Transfers[source].Base != first || second.Transfers[source].Mode != "incremental" {
		t.Fatal("repeated manual backup did not create a new incremental base", second)
	}
}

func TestSuccessfulBackupPrunesExpiredSourcesAfterReplacingBase(t *testing.T) {
	a := testApp(t)
	b := a.config.ResolveBackup(a.config.Backups[0])
	now := time.Now()
	if err := a.finishDemoBackupLocked(b, now); err != nil {
		t.Fatal(err)
	}
	source := b.Datasets[0].Source
	base := a.state.Backups[b.ID].Bases[source]
	for i := range a.state.Snapshots {
		if a.state.Snapshots[i].Name == base {
			a.state.Snapshots[i].ExpiresAt = now.Add(-time.Hour)
		}
	}
	for i := range a.state.Replicas {
		if a.state.Replicas[i].SourceSnapshot == base {
			a.state.Replicas[i].ExpiresAt = now.Add(-time.Hour)
		}
	}
	keep := Snapshot{Name: source + "@KEEP-forever", Dataset: source, CreatedAt: now.Add(-60 * 24 * time.Hour), Managed: true, ExpiresAt: now.Add(-time.Hour)}
	foreign := Snapshot{Name: source + "@foreign-held", Dataset: source, CreatedAt: keep.CreatedAt, Managed: true, ExpiresAt: keep.ExpiresAt, HeldBy: []string{"other-tool"}}
	a.state.Snapshots = append(a.state.Snapshots, keep, foreign)
	a.pruneDemoBackupSourcesLocked(b, now)
	if !a.protectedSnapshotNamesLocked()[base] {
		t.Fatal("current expired base lost its protection")
	}
	if err := a.finishDemoBackupLocked(b, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range a.state.Snapshots {
		seen[s.Name] = true
	}
	if seen[base] || findReplica(a.state.Replicas, b, b.Datasets[0], base) != nil {
		t.Fatal("obsolete expired source or receiver survived successful replacement")
	}
	if !seen[keep.Name] || !seen[foreign.Name] || !seen[a.state.Backups[b.ID].Bases[source]] {
		t.Fatal("cleanup lost a permanent, held, or current base")
	}
}

func TestLongBatchRefreshesBothExpiriesWithoutUnnecessaryCommands(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("ssh=", remote), func(t *testing.T) {
			dir, log := retentionCommands(t)
			b := customBackupForTest(DefaultConfig(time.Now()), 0)
			start := mustTime("2026-11-02T05:44:00+08:00")
			finish := mustTime("2026-11-02T05:46:00+08:00")
			oldNext, oldUntil := backupProtection(b, start.Location(), start)
			next, until := backupProtection(b, finish.Location(), finish)
			r := nativeResult{source: Snapshot{Name: "tank/src@source-base", ExpiresAt: oldUntil}, replica: ReplicaSnapshot{Snapshot: Snapshot{Name: "backup/dst@receiver-base", ExpiresAt: oldUntil}}, transfer: TransferResult{NextBackupAt: oldNext, ProtectedUntil: oldUntil}}
			for _, s := range []Snapshot{r.source, r.replica.Snapshot} {
				writeRetentionFixture(t, filepath.Join(dir, "exact-"+strings.SplitN(s.Name, "@", 2)[1]), metadataLine(s.Name, "123", start, fmt.Sprint(oldUntil.Unix()), "1"))
			}
			e := nativeEndpoint{}
			if remote {
				e = nativeEndpoint{host: "test-peer", port: 2202}
			}
			if err := refreshNativeBackupProtection(context.Background(), b, start.Location(), e, &r, finish); err != nil {
				t.Fatal(err)
			}
			if !r.source.ExpiresAt.Equal(until) || !r.replica.ExpiresAt.Equal(until) || !r.transfer.NextBackupAt.Equal(next) || !r.transfer.ProtectedUntil.Equal(until) {
				t.Fatal("batch crossed a schedule without extending both bases", r)
			}
			calls, _ := os.ReadFile(log)
			for _, name := range []string{r.source.Name, r.replica.Name} {
				if !strings.Contains(string(calls), fmt.Sprintf("set\tset\t%s=%d\t%s\n", ExpiryProperty, until.Unix(), name)) {
					t.Fatal("expiry not written", name, string(calls))
				}
			}
			writeRetentionFixture(t, log, "")
			if err := refreshNativeBackupProtection(context.Background(), b, start.Location(), e, &r, finish.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			calls, _ = os.ReadFile(log)
			if len(calls) != 0 {
				t.Fatal("unchanged schedule caused extra local/SSH commands", string(calls))
			}
		})
	}
}
