package chrona

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testPolicy() Policy {
	return Policy{ID: "test", Dataset: "tank/home", Label: "Home", StartAt: "2026-01-01T00:00:00+08:00", Enabled: true, Keep: Keep{4, 3, 7, 4, 12}}
}
func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCoincidentTiersCreateOneMonthlyEvent(t *testing.T) {
	p := testPolicy()
	anchor := mustTime(p.StartAt)
	events := NextEvents(p, anchor.Location(), 0, anchor.Add(-time.Second), 6)
	if len(events) != 6 || events[0].Tier != "monthly" || len(events[0].Merged) != 5 {
		t.Fatalf("expected a single five-tier monthly event: %+v", events)
	}
	if !events[0].ExpiresAt.Equal(addMonthsClamped(anchor, 12)) {
		t.Fatal("monthly expiration must use its own count")
	}
	if events[1].Tier != "quarter_hourly" || events[4].Tier != "hourly" || len(events[4].Merged) != 2 {
		t.Fatal("quarter-hour and hourly slots did not merge correctly")
	}
	for i := 1; i < len(events); i++ {
		if !events[i].RunAt.After(events[i-1].RunAt) {
			t.Fatal("duplicate or unordered run time")
		}
	}
}

func TestMonthlySlotsClampWithoutDrifting(t *testing.T) {
	p := testPolicy()
	p.StartAt = "2024-01-31T10:20:00+08:00"
	p.Keep = Keep{Monthly: 1}
	anchor := mustTime(p.StartAt)
	events := NextEvents(p, anchor.Location(), 0, anchor.Add(-time.Second), 4)
	for i, want := range []string{"2024-01-31T10:20:00+08:00", "2024-02-29T10:20:00+08:00", "2024-03-31T10:20:00+08:00", "2024-04-30T10:20:00+08:00"} {
		if !events[i].RunAt.Equal(mustTime(want)) {
			t.Fatalf("month %d: got %s, want %s", i, events[i].RunAt, want)
		}
	}
	if !events[1].ExpiresAt.Equal(events[2].RunAt) {
		t.Fatal("Feb 29 must expire at the anchored March 31 slot")
	}
}

func TestMonthlyDoesNotInventAWeeklyCoincidence(t *testing.T) {
	p := testPolicy()
	p.StartAt = "2026-01-01T00:00:00+08:00"
	after := mustTime("2026-02-01T00:00:00+08:00").Add(-time.Second)
	e := NextEvents(p, mustTime(p.StartAt).Location(), 0, after, 1)[0]
	if e.Tier != "monthly" || len(e.Merged) != 4 {
		t.Fatalf("31 days is not a whole number of weeks: %+v", e)
	}
}

func TestJitterIsStableAndDoesNotMoveLogicalSlots(t *testing.T) {
	p := testPolicy()
	anchor := mustTime(p.StartAt)
	a := NextEvents(p, anchor.Location(), 45, anchor.Add(-time.Second), 10)
	b := NextEvents(p, anchor.Location(), 45, anchor.Add(-time.Second), 10)
	for i := range a {
		if !a[i].RunAt.Equal(b[i].RunAt) || a[i].OffsetSeconds < 0 || a[i].OffsetSeconds > 45 {
			t.Fatal("unstable jitter")
		}
		if a[i].RunAt.Sub(a[i].PlannedAt) != time.Duration(a[0].OffsetSeconds)*time.Second {
			t.Fatal("per-tier jitter broke merge alignment")
		}
		if i > 0 && a[i].PlannedAt.Sub(a[i-1].PlannedAt) != 15*time.Minute {
			t.Fatal("schedule drifted")
		}
	}
}

func TestRetentionConstraintAndDisabledTiers(t *testing.T) {
	tests := []struct {
		name  string
		keep  Keep
		valid bool
	}{
		{"valid", Keep{4, 3, 7, 4, 12}, true},
		{"hourly exceeds daily", Keep{0, 720, 7, 4, 12}, false},
		{"quarter exceeds hourly", Keep{96, 12, 7, 4, 12}, false},
		{"skip disabled weekly", Keep{0, 3, 7, 0, 1}, true},
		{"calendar year permits 52 weeks", Keep{0, 0, 0, 52, 12}, true},
		{"one month cannot promise 30 days", Keep{0, 720, 0, 0, 1}, false},
		{"all disabled", Keep{}, false},
		{"negative", Keep{-1, 3, 7, 4, 12}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := testPolicy()
			p.Keep = tc.keep
			err := ValidatePolicy(p)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestNextBackupHandlesWeekdaysAndCalendarMonthEnd(t *testing.T) {
	b := DefaultConfig(time.Now()).Backups[0]
	loc, _ := time.LoadLocation("Asia/Shanghai")
	got := NextBackup(b, loc, mustTime("2026-11-02T05:45:00+08:00"))
	if !got.Equal(mustTime("2026-11-06T05:45:00+08:00")) {
		t.Fatalf("wrong weekday: %s", got)
	}
	b.Cadence = "monthly"
	b.MonthDay = 31
	b.At = "10:00"
	if got = NextBackup(b, loc, mustTime("2027-02-01T00:00:00+08:00")); !got.Equal(mustTime("2027-02-28T10:00:00+08:00")) {
		t.Fatalf("wrong month end: %s", got)
	}
}

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a, err := NewApp(Options{Mode: "demo", ConfigPath: filepath.Join(dir, "config.yaml"), StatePath: filepath.Join(dir, "state.json")})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func perform(a *App, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	a.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("Chrona")}}).ServeHTTP(w, r)
	return w
}

func TestInvalidYAMLDoesNotReplaceWorkingConfig(t *testing.T) {
	a := testApp(t)
	before, _ := os.ReadFile(a.opts.ConfigPath)
	for _, body := range []string{"version: 1\nunknown: true\n", string(before) + "\n---\nversion: 1\n"} {
		w := perform(a, http.MethodPut, "/api/config", body)
		if w.Code != 422 {
			t.Fatalf("expected validation error, got %d %s", w.Code, w.Body.String())
		}
		after, _ := os.ReadFile(a.opts.ConfigPath)
		if string(before) != string(after) {
			t.Fatal("invalid config replaced good file")
		}
	}
}

func TestPolicyMutationPersistsAcrossRestart(t *testing.T) {
	a := testApp(t)
	c := NormalizeConfig(a.config)
	c.Policies[0].Keep.Hourly = 96
	c.Datasets[0].Disabled = true
	body, _ := json.Marshal(c)
	w := perform(a, "PUT", "/api/config", string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted, err := NewApp(a.opts)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.config.Datasets[0].Disabled || restarted.config.Policies[0].Keep.Hourly != 96 {
		t.Fatal("dataset config was not restored")
	}
}

func TestPruningRemovesExpiredUnheldSnapshotsInExactDataset(t *testing.T) {
	a := testApp(t)
	p := a.config.EffectivePolicies()[0]
	now := time.Now()
	a.state.Snapshots = []Snapshot{
		{Name: p.Dataset + "@chrona_expired", Dataset: p.Dataset, Managed: true, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second)},
		{Name: p.Dataset + "@chrona_held", Dataset: p.Dataset, Managed: true, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second), HeldBy: []string{"backup"}},
		{Name: p.Dataset + "@manual", Dataset: p.Dataset, Managed: false, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second)},
		{Name: p.Dataset + "/child@chrona_expired", Dataset: p.Dataset + "/child", Managed: true, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second)},
	}
	e := Event{PlannedAt: now.Truncate(time.Second), RunAt: now, ExpiresAt: now.Add(time.Hour), Tier: "hourly", Label: "小时", Merged: []string{"hourly"}}
	if err := a.snapshotLocked(context.Background(), p, e); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Snapshots) != 4 {
		t.Fatalf("expected 3 protected/external plus the new snapshot, got %d", len(a.state.Snapshots))
	}
	for _, s := range a.state.Snapshots {
		if s.Name == p.Dataset+"@chrona_expired" {
			t.Fatal("expired snapshot survived")
		}
	}
}

func TestFailedRealSnapshotNeverStartsCleanup(t *testing.T) {
	a := testApp(t)
	a.opts.Mode = "zfs"
	a.opts.AllowWrites = true
	p := a.config.EffectivePolicies()[0]
	a.state.Snapshots = []Snapshot{{Name: p.Dataset + "@chrona_expired", Dataset: p.Dataset, Managed: true, ExpiresAt: time.Now().Add(-time.Hour)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.snapshotLocked(ctx, p, Event{PlannedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	if err == nil {
		t.Fatal("expected canceled ZFS creation failure")
	}
	if len(a.state.Snapshots) != 1 {
		t.Fatal("creation failure caused cleanup")
	}
}

func TestReadOnlyModeRejectsSnapshotWrites(t *testing.T) {
	a := testApp(t)
	a.opts.Mode = "zfs"
	a.opts.AllowWrites = false
	w := perform(a, "POST", "/api/policies/fine/run", "")
	if w.Code == 200 {
		t.Fatal("read-only mode accepted a write")
	}
}

func TestSingleExpiryPropertyAndNoRecursiveDestroy(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$CHRONA_TEST_CALLS\"\nif [ \"$1\" = list ]; then printf '%s\\n' \"$CHRONA_TEST_METADATA\"; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "zfs"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHRONA_TEST_CALLS", logPath)
	e := Event{PlannedAt: mustTime("2026-11-01T00:00:00Z"), ExpiresAt: mustTime("2027-11-01T00:00:00Z")}
	name, err := createRealSnapshot(context.Background(), "tank/home", e)
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Hour)
	t.Setenv("CHRONA_TEST_METADATA", fmt.Sprintf("%s\t123\t%d\t0\t%d\t0", name, expired.Add(-time.Hour).Unix(), expired.Unix()))
	if deleted, err := destroyRealSnapshot(context.Background(), Snapshot{Name: name, Dataset: "tank/home", GUID: "123", Managed: true, ExpiresAt: expired}); err != nil || !deleted {
		t.Fatal("single-snapshot deletion failed", deleted, err)
	}
	data, _ := os.ReadFile(logPath)
	s := string(data)
	if strings.Count(s, "snapshot\n-o\nchrona:expires=") != 1 || !strings.Contains(s, "chrona:expires=") {
		t.Fatalf("wrong property contract: %s", s)
	}
	if strings.Contains(s, "-r\n") || strings.Contains(s, "-R\n") || strings.Contains(s, "tier=") {
		t.Fatal("unexpected recursion or extra metadata")
	}
}

func TestCrossOriginMutationsAreRejected(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest("PUT", "http://localhost/api/config", strings.NewReader("version: 1"))
	r.Header.Set("Origin", "https://example.org")
	w := httptest.NewRecorder()
	a.Handler(fstest.MapFS{}).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin mutation returned %d", w.Code)
	}
}

func TestHealthAndSnapshotPagination(t *testing.T) {
	a := testApp(t)
	w := perform(a, "GET", "/api/snapshots?page=1", "")
	var result struct {
		Items []Snapshot `json:"items"`
		Total int        `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) > 20 || result.Total <= 20 {
		t.Fatal("pagination failed")
	}
	w = perform(a, "GET", "/api/health", "")
	body, _ := io.ReadAll(w.Body)
	if w.Code != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatal("health check failed")
	}
}

func TestHandEditedConfigReloadAndInvalidRevision(t *testing.T) {
	a := testApp(t)
	before, _ := os.ReadFile(a.opts.ConfigPath)
	changed := strings.Replace(string(before), "jitter_seconds: 45", "jitter_seconds: 17", 1)
	if err := os.WriteFile(a.opts.ConfigPath, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.reloadConfigLocked(); err != nil {
		t.Fatal(err)
	}
	if a.config.JitterSeconds != 17 {
		t.Fatal("hand-edited config did not reload")
	}
	if err := os.WriteFile(a.opts.ConfigPath, []byte("version: 9\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.reloadConfigLocked(); err == nil {
		t.Fatal("invalid revision was accepted")
	}
	if a.config.JitterSeconds != 17 {
		t.Fatal("invalid revision changed running configuration")
	}
	if err := a.reloadConfigLocked(); err != nil {
		t.Fatal("unchanged bad revision should not log repeatedly")
	}
}

func TestManualSnapshotLifetimeUsesShortestEnabledTier(t *testing.T) {
	p := testPolicy()
	now := mustTime("2026-11-01T10:06:00+08:00")
	e := ManualEvent(p, now.Location(), now)
	if e.Tier != "quarter_hourly" || e.ExpiresAt.Sub(now) != time.Hour {
		t.Fatalf("wrong manual lifetime: %+v", e)
	}
}

func TestSharedPolicyAssignmentsAndAtomicReassignment(t *testing.T) {
	a := testApp(t)
	c := NormalizeConfig(a.config)
	if c.Datasets[0].Policy != c.Datasets[1].Policy {
		t.Fatal("fixture must share a rule")
	}
	c.Datasets[0].Policy = "files"
	body, _ := json.Marshal(c)
	w := perform(a, "PUT", "/api/config", string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.config.Datasets[0].Policy != "files" || a.config.Datasets[1].Policy != "fine" {
		t.Fatal("assignment affected other datasets")
	}
	restarted, err := NewApp(a.opts)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.config.Datasets[0].Policy != "files" {
		t.Fatal("reference did not persist")
	}
}

func TestLegacyConfigurationMigrationKeepsOriginalBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	old := `version: 1
timezone: Asia/Shanghai
jitter_seconds: 45
policies:
  - id: first
    dataset: tank/home
    label: Home
    start_at: "2026-01-01T00:00:00+08:00"
    enabled: true
    keep: {hourly: 3, daily: 7}
  - id: second
    dataset: tank/docs
    label: Documents
    start_at: "2026-01-01T00:00:00+08:00"
    enabled: true
    keep: {hourly: 3, daily: 7}
backups:
  - id: old-backup
    name: Offsite
    source: tank/home
    destination: backup/home
    host: other
    port: 22
    cadence: weekly
    weekdays: [1]
    month_day: 1
    at: "05:00"
    enabled: true
    keep_weekly: 4
    keep_monthly: 3
`
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(Options{Mode: "demo", ConfigPath: path, StatePath: filepath.Join(dir, "state.json")})
	if err != nil {
		t.Fatal(err)
	}
	if a.config.Version != 3 || len(a.config.Policies) != 1 || len(a.config.Datasets) != 2 {
		t.Fatalf("bad migration: %+v", a.config)
	}
	if a.config.Backups[0].Datasets[0].Destination != "backup/home" {
		t.Fatal("backup target lost")
	}
	backup, err := os.ReadFile(path + ".v1.bak")
	if err != nil || string(backup) != old {
		t.Fatal("original config backup missing")
	}
}

func TestSharedPolicyManualRunRespectsSelectedDataset(t *testing.T) {
	a := testApp(t)
	before := len(a.state.Snapshots)
	w := perform(a, "POST", "/api/policies/fine/run?dataset=Saturn%2FArchive", "")
	if w.Code != 200 || len(a.state.Snapshots) != before+1 {
		t.Fatal(w.Code, w.Body.String())
	}
	latest := a.state.Snapshots[0]
	if latest.Dataset != "Saturn/Archive" {
		t.Fatal("manual run targeted the wrong dataset")
	}
	w = perform(a, "POST", "/api/policies/fine/run?dataset=rpool%2FROOT%2Fpve-1", "")
	if w.Code != 400 {
		t.Fatal("a dataset outside the policy was accepted")
	}
}

func TestDuplicateYAMLAssignmentsAreRejected(t *testing.T) {
	c := DefaultConfig(time.Now())
	c.Datasets = append(c.Datasets, c.Datasets[0])
	if err := ValidateConfig(c); err == nil {
		t.Fatal("duplicate dataset accepted")
	}
}

func TestMultiDatasetBackupKeepsAnIndependentBaseForEachDataset(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[1]
	a.mu.Lock()
	err := a.startBackupLocked(b)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.RLock()
		done := a.state.Backups[b.ID].Status == "success"
		a.mu.RUnlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("backup did not complete")
		}
		time.Sleep(30 * time.Millisecond)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := a.state.Backups[b.ID]
	if len(state.Bases) != 2 {
		t.Fatalf("expected two distinct bases: %+v", state)
	}
	for _, binding := range b.Datasets {
		held := 0
		for _, snapshot := range a.state.Snapshots {
			if snapshot.Dataset != binding.Source {
				continue
			}
			for _, id := range snapshot.HeldBy {
				if id == b.ID {
					held++
					if state.Bases[binding.Source] != snapshot.Name {
						t.Fatal("wrong protected base")
					}
				}
			}
		}
		if held != 1 {
			t.Fatalf("%s has %d protected bases", binding.Source, held)
		}
	}
}

func TestChangingBackupTargetDoesNotClaimItWasAlreadyBackedUp(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[1]
	if a.state.Backups[b.ID].Status != "success" {
		t.Fatal("fixture must have a previous successful backup")
	}
	b.Host = "new-backup-host"
	body, _ := json.Marshal(b)
	w := perform(a, "POST", "/api/backups", string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := a.state.Backups[b.ID]
	if state.Status != "pending" || !state.LastSuccess.IsZero() || len(state.Bases) != 0 {
		t.Fatalf("new destination inherited old backup success: %+v", state)
	}
}
