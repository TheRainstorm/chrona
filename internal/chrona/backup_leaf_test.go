package chrona

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBackupPlansDoNotInheritOrExpandDestinationPaths(t *testing.T) {
	c := inheritanceConfig()
	c.Datasets = []DatasetConfig{{Name: "tank/home", Policy: "test"}}
	task := DefaultConfig(time.Now()).Backups[0]
	task.Datasets = []BackupDataset{{Source: "tank/home/apps", Destination: "remote/apps"}}
	c.Backups = []Backup{task}
	inventory := []Dataset{{Name: "tank/home"}, {Name: "tank/home/apps"}, {Name: "tank/home/apps/photos"}, {Name: "tank/home/apps/docs"}}
	if len(c.ResolveBackups("tank/home/apps/photos")) != 0 {
		t.Fatal("child inherited its parent's plan")
	}
	if len(c.EffectiveBackup(task, inventory...).Datasets) != 0 {
		t.Fatal("parent mapping expanded into child transfers")
	}
	task.Datasets = append(task.Datasets, BackupDataset{Source: "tank/home/apps/photos", Destination: "remote/my-photos"})
	c.Backups[0] = task
	resolved := c.ResolveBackups("tank/home/apps/photos")
	if len(resolved) != 1 || resolved[0].Origin != "tank/home/apps/photos" || resolved[0].Binding.Destination != "remote/my-photos" {
		t.Fatal("explicit destination changed", resolved)
	}
	b := c.EffectiveBackup(task, inventory...)
	if len(b.Datasets) != 1 || b.Datasets[0].Source != "tank/home/apps/photos" {
		t.Fatal("only the explicitly assigned leaf may be sent", b.Datasets)
	}
	c.Datasets = append(c.Datasets, DatasetConfig{Name: "tank/home/apps/photos", BackupMode: "none"})
	c = NormalizeConfig(c)
	if len(c.ResolveBackups("tank/home/apps/photos")) != 1 || c.Datasets[1].BackupMode != "" {
		t.Fatal("obsolete inheritance settings affected a direct plan")
	}
	inventory = append(inventory, Dataset{Name: "tank/home/apps/photos/2026"})
	if len(c.EffectiveBackup(task, inventory...).Datasets) != 0 || len(c.ResolveBackups("tank/home/apps/photos/2026")) != 0 {
		t.Fatal("a newly discovered child must not be backed up automatically")
	}
	if err := ValidateConfig(c); err != nil {
		t.Fatal(err)
	}
}

func TestBackupExpiryCoversCalendarNextRunPlusBuffer(t *testing.T) {
	b := DefaultConfig(time.Now()).Backups[0]
	b.Cadence = "monthly"
	b.MonthDay = 31
	b.At = "05:45"
	b.KeepWeekly = 0
	b.KeepMonthly = 1
	b.GraceMinutes = 90
	now := mustTime("2027-02-28T06:00:00+08:00")
	next, until := backupProtection(b, now.Location(), now)
	if !next.Equal(mustTime("2027-03-31T05:45:00+08:00")) || !until.Equal(next.Add(90*time.Minute)) {
		t.Fatal(next, until)
	}
	// A month from Feb 28 is March 28, before the next scheduled backup.
	if !backupReplicaExpiry(b, now.Location(), now).Equal(until) {
		t.Fatal("calendar retention failed to honor the next-run floor")
	}
	b.GraceMinutes = 0
	if backupGrace(b) != time.Hour {
		t.Fatal("omitted buffer must default to one hour")
	}
}

func TestDemoBackupWritesReceiverExpiryAndRequiresACommonBase(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[0]
	b.KeepWeekly = 0
	b.KeepMonthly = 1
	b.GraceMinutes = 120
	a.config.Backups[0] = b
	a.state.Replicas = []ReplicaSnapshot{}
	now := time.Now()
	if err := a.finishDemoBackupLocked(b, now); err != nil {
		t.Fatal(err)
	}
	source := b.Datasets[0].Source
	first := a.state.Backups[b.ID]
	if first.Transfers[source].Mode != "full" || first.Transfers[source].Base != "" {
		t.Fatal("missing remote base must use full transfer", first)
	}
	replica := findReplica(a.state.Replicas, b, b.Datasets[0], first.Bases[source])
	if replica == nil {
		t.Fatal("receiver snapshot was not recorded")
	}
	next, until := backupProtection(b, time.FixedZone("CST", 8*3600), now)
	if replica.ExpiresAt.Before(until) || !until.After(next) {
		t.Fatal("receiver expiry does not cover next backup plus buffer")
	}
	sourceFound := false
	for _, snapshot := range a.state.Snapshots {
		if snapshot.Name == first.Bases[source] {
			sourceFound = true
			if snapshot.ExpiresAt.Before(until) || len(snapshot.HeldBy) == 0 {
				t.Fatal("local base can expire before the next backup")
			}
		}
	}
	if !sourceFound || strings.SplitN(replica.Name, "@", 2)[1] != strings.SplitN(first.Bases[source], "@", 2)[1] {
		t.Fatal("receiver did not copy the same snapshot")
	}
	if err := a.finishDemoBackupLocked(b, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	second := a.state.Backups[b.ID]
	if second.Transfers[source].Mode != "incremental" || second.Transfers[source].Base != first.Bases[source] {
		t.Fatal("second transfer did not use its common base")
	}
	oldReplica := findReplica(a.state.Replicas, b, b.Datasets[0], first.Bases[source])
	newReplica := findReplica(a.state.Replicas, b, b.Datasets[0], second.Bases[source])
	if oldReplica == nil || len(oldReplica.HeldBy) != 0 || newReplica == nil || len(newReplica.HeldBy) == 0 {
		t.Fatal("receiver holds did not advance after successful transfer")
	}
	// An expired current base stays protected while an obsolete expired replica is pruned.
	for i := range a.state.Replicas {
		a.state.Replicas[i].ExpiresAt = now.Add(-time.Hour)
	}
	a.pruneReplicasLocked(now)
	if len(a.state.Replicas) != 1 || a.state.Replicas[0].SourceSnapshot != second.Bases[source] {
		t.Fatal("receiver pruning lost protected current base")
	}
}

func TestFailedBatchLeavesBothPreviousBasesProtected(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[1]
	before, _ := json.Marshal(a.state.Backups[b.ID])
	replicas, _ := json.Marshal(a.state.Replicas)
	// The first leaf can stage its snapshot, but the second has become a path node.
	a.datasets = append(a.datasets, Dataset{Name: b.Datasets[1].Source + "/child"})
	if err := a.finishDemoBackupLocked(b, time.Now()); err == nil {
		t.Fatal("a newly non-leaf source must abort the batch")
	}
	after, _ := json.Marshal(a.state.Backups[b.ID])
	afterReplicas, _ := json.Marshal(a.state.Replicas)
	if string(before) != string(after) || string(replicas) != string(afterReplicas) {
		t.Fatal("failed staging advanced a common base or changed receiver snapshots")
	}
	for _, binding := range b.Datasets {
		base := a.state.Backups[b.ID].Bases[binding.Source]
		protected := false
		for _, snapshot := range a.state.Snapshots {
			if snapshot.Name == base {
				for _, id := range snapshot.HeldBy {
					protected = protected || id == b.ID
				}
			}
		}
		if !protected {
			t.Fatal("previous local base was released on failure")
		}
	}
}

func TestScheduleEditExtendsExistingCommonBaseExpiry(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[0]
	if err := a.finishDemoBackupLocked(b, time.Now()); err != nil {
		t.Fatal(err)
	}
	c := NormalizeConfig(a.config)
	c.Backups[0].Cadence = "monthly"
	c.Backups[0].MonthDay = 28
	c.Backups[0].GraceMinutes = 180
	before := a.state.Backups[b.ID].Bases[b.Datasets[0].Source]
	payload, _ := json.Marshal(c)
	if w := perform(a, "PUT", "/api/config", string(payload)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := a.state.Backups[b.ID]
	next, until := backupProtection(c.Backups[0], time.FixedZone("CST", 8*3600), time.Now().Add(-time.Second))
	if state.Bases[b.Datasets[0].Source] != before || state.Transfers[b.Datasets[0].Source].ProtectedUntil.Before(until.Add(-time.Second)) {
		t.Fatal("schedule edit lost or shortened the common base", next, state)
	}
}

func TestSnapshotPlanReportsAllTiersAndOnlyFutureMergedEvents(t *testing.T) {
	a := testApp(t)
	w := perform(a, "GET", "/api/dataset-plan?dataset=Saturn%2FArchive", "")
	var p struct {
		Events []Event          `json:"events"`
		Tiers  []map[string]any `json:"tiers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || w.Code != 200 {
		t.Fatal(err, w.Code, w.Body.String())
	}
	if len(p.Events) != 16 || len(p.Tiers) != 5 {
		t.Fatal("plan must include the five horizons, not only quarter-hour slots")
	}
	for i, e := range p.Events {
		if !e.RunAt.After(time.Now().Add(-time.Second)) || !e.ExpiresAt.After(e.RunAt) {
			t.Fatal("invalid projected event", e)
		}
		if i > 0 && !e.RunAt.After(p.Events[i-1].RunAt) {
			t.Fatal("duplicate event")
		}
	}
}
