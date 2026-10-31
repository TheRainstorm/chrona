package chrona

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestDatasetRulesAreIndependentAndPublicReferencesStayLive(t *testing.T) {
	a := testApp(t)
	c := NormalizeConfig(a.config)
	c.Datasets[0].Policy = ""
	c.Datasets[0].Snapshot = &SnapshotSettings{StartAt: c.Policies[0].StartAt, Keep: c.Policies[0].Keep}
	c.Datasets[0].Snapshot.Keep.Hourly = 96
	c.Datasets[0].Disabled = true
	c.Policies[0].Keep.Hourly = 120
	body, _ := json.Marshal(c)
	if w := perform(a, "PUT", "/api/config", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	rules := a.config.EffectivePolicies()
	if rules[0].Keep.Hourly != 96 || rules[0].Enabled || rules[1].Keep.Hourly != 120 || !rules[1].Enabled {
		t.Fatalf("independent edit or dataset pause leaked to another dataset: %+v", rules[:2])
	}
	if rules[0].ID == rules[1].ID || rules[0].ID != localPolicyID(c.Datasets[0].Name) {
		t.Fatal("independent rule needs a stable dataset identity")
	}
	encoded, _ := os.ReadFile(a.opts.ConfigPath)
	reloaded, err := NewApp(a.opts)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.config.Datasets[0].Snapshot.Keep.Hourly != 96 || reloaded.config.Datasets[1].Policy != c.Policies[0].ID {
		t.Fatalf("file did not preserve dataset ownership: %v", err)
	}
	var document struct {
		Policies []map[string]any `yaml:"policies"`
	}
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, policy := range document.Policies {
		if _, ok := policy["datasets"]; ok {
			t.Fatal("public definitions must not own dataset assignments")
		}
	}
}

func TestInvalidDatasetReferenceDoesNotReplaceConfig(t *testing.T) {
	a := testApp(t)
	before, _ := os.ReadFile(a.opts.ConfigPath)
	for _, variant := range []string{"missing", "both"} {
		c := NormalizeConfig(a.config)
		if variant == "missing" {
			c.Datasets[0].Policy = "unknown"
		} else {
			c.Datasets[0].Snapshot = &SnapshotSettings{StartAt: c.Policies[0].StartAt, Keep: c.Policies[0].Keep}
		}
		body, _ := json.Marshal(c)
		if w := perform(a, "PUT", "/api/config", string(body)); w.Code != 422 {
			t.Fatal(variant, w.Code, w.Body.String())
		}
		after, _ := os.ReadFile(a.opts.ConfigPath)
		if string(before) != string(after) {
			t.Fatal("invalid reference replaced the working file")
		}
	}
}

func TestVersionTwoMigrationPreservesDatasetPause(t *testing.T) {
	c := Config{Version: 2, Timezone: "Asia/Shanghai", Policies: []Policy{{ID: "shared", Label: "Shared", Datasets: []string{"tank/a", "tank/b"}, StartAt: testPolicy().StartAt, Keep: testPolicy().Keep}}}
	c = NormalizeConfig(c)
	if err := ValidateConfig(c); err != nil {
		t.Fatal(err)
	}
	if c.Version != 3 || len(c.Datasets) != 2 || !c.Datasets[0].Disabled || !c.Datasets[1].Disabled {
		t.Fatal("migration lost paused datasets")
	}
	if len(c.Policies[0].Datasets) != 0 || c.Datasets[0].Policy != "shared" {
		t.Fatal("migration did not move references to datasets")
	}
}

func TestRunningOneDatasetBackupPreservesOtherSourcesBase(t *testing.T) {
	a := testApp(t)
	b := a.config.Backups[1]
	selected, other := b.Datasets[0].Source, b.Datasets[1].Source
	oldBase, oldTime := a.state.Backups[b.ID].Bases[other], a.state.Backups[b.ID].Completed[other]
	w := perform(a, "POST", "/api/backups/"+b.ID+"/run?dataset="+selected, "")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.RLock()
		state := a.state.Backups[b.ID]
		done := state.Status == "success"
		a.mu.RUnlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("backup did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := a.state.Backups[b.ID]
	if state.Bases[other] != oldBase || !state.Completed[other].Equal(oldTime) {
		t.Fatal("running one source changed the other source's base or completion time")
	}
	held := false
	for _, s := range a.state.Snapshots {
		if s.Name == oldBase && strings.Contains(strings.Join(s.HeldBy, ","), b.ID) {
			held = true
		}
	}
	if !held {
		t.Fatal("running one source released the other source's incremental base")
	}
}
