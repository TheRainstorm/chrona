package chrona

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func inheritanceConfig() Config {
	p := testPolicy()
	p.Dataset = ""
	return Config{Version: 3, Timezone: "Asia/Shanghai", JitterSeconds: 45, Policies: []Policy{p}, Datasets: []DatasetConfig{
		{Name: "tank/home", Policy: p.ID},
		{Name: "tank/home/apps", Snapshot: &SnapshotSettings{StartAt: p.StartAt, Keep: Keep{Hourly: 24, Daily: 7}}},
		{Name: "tank/home/apps/special", Policy: p.ID},
		{Name: "tank/home/paused", Disabled: true},
		{Name: "tank/home/excluded", SnapshotMode: "none"},
		{Name: "tank/home/excluded/opt-in", Policy: p.ID},
	}}
}

func TestSnapshotInheritanceUsesNearestRuleAndWholePathComponents(t *testing.T) {
	c := inheritanceConfig()
	if err := ValidateConfig(c); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source string
		hourly       int
		active       bool
	}{
		{"tank/home", "tank/home", 3, true},
		{"tank/home/new-child", "tank/home", 3, true},
		{"tank/home/apps/photos/raw", "tank/home/apps", 24, true},
		{"tank/home/apps/special/child", "tank/home/apps/special", 3, true},
		{"tank/home/paused", "tank/home", 3, false},
		{"tank/home/paused/child", "tank/home", 3, true},
		{"tank/home/excluded", "tank/home/excluded", 0, false},
		{"tank/home/excluded/child", "tank/home/excluded", 0, false},
		{"tank/home/excluded/opt-in/child", "tank/home/excluded/opt-in", 3, true},
		{"tank/home-other", "", 0, false},
		{"tank/home-other/child", "", 0, false},
		{"other/home", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, source, ok := c.ResolveSnapshot(tc.name)
			if source != tc.source || p.Keep.Hourly != tc.hourly || ok != (tc.hourly != 0) || p.Enabled != tc.active {
				t.Fatalf("got source=%s ok=%v policy=%+v", source, ok, p)
			}
			if ok && p.Dataset != tc.name {
				t.Fatal("resolved policy must target the exact child dataset")
			}
		})
	}
	// The inherited origin stays fixed; its child's name supplies independent jitter.
	p, _, _ := c.ResolveSnapshot("tank/home/apps/photos")
	if p.ID != localPolicyID("tank/home/apps") {
		t.Fatal("an inherited custom rule lost its stable origin")
	}
}

func TestDiscoveredChildrenAreResolvedOnceAndNewChildrenJoinAutomatically(t *testing.T) {
	c := inheritanceConfig()
	inventory := []Dataset{{Name: "tank/home"}, {Name: "tank/home/apps/photos"}, {Name: "tank/home/apps/photos"}, {Name: "tank/home/new"}, {Name: "tank/home-other/ignored"}}
	policies := c.EffectivePolicies(inventory...)
	seen := map[string]int{}
	for _, p := range policies {
		seen[p.Dataset]++
	}
	if seen["tank/home"] != 1 || seen["tank/home/apps/photos"] != 1 || seen["tank/home/new"] != 1 || seen["tank/home-other/ignored"] != 0 || seen["tank/home/excluded"] != 0 {
		t.Fatal("discovered descendants were omitted, duplicated or crossed a path boundary", seen)
	}
	before := len(c.Datasets)
	inventory = append(inventory, Dataset{Name: "tank/home/added-later"})
	if len(c.EffectivePolicies(inventory...)) != len(policies)+1 || len(c.Datasets) != before {
		t.Fatal("new children must join without copying settings into config")
	}
}

func TestInheritedDatasetAPIManualRunAndRestoreAcrossRestart(t *testing.T) {
	a := testApp(t)
	c := inheritanceConfig()
	child := "tank/home/apps/photos"
	a.datasets = []Dataset{{Name: child}}
	body, _ := json.Marshal(c)
	if w := perform(a, "PUT", "/api/config", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// The same resolver drives the editor's unsaved preview and the scheduler.
	preview := perform(a, "POST", "/api/dataset-preview", `{"name":"tank/home/apps/photos"}`)
	var resolved struct {
		Snapshot Policy  `json:"snapshot"`
		Source   string  `json:"snapshot_source"`
		Events   []Event `json:"events"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &resolved); err != nil || preview.Code != 200 || resolved.Source != "tank/home/apps" || resolved.Snapshot.Keep.Hourly != 24 || len(resolved.Events) == 0 {
		t.Fatal("preview did not resolve inherited rule", preview.Code, preview.Body.String(), err)
	}
	before := len(a.state.Snapshots)
	w := perform(a, "POST", "/api/policies/"+resolved.Snapshot.ID+"/run?dataset="+url.QueryEscape(child), "")
	if w.Code != 200 || len(a.state.Snapshots) != before+1 || a.state.Snapshots[0].Dataset != child {
		t.Fatal("manual inherited snapshot must affect only selected child", w.Code, w.Body.String())
	}
	// Local overrides survive parent edits; clearing an override resumes inheritance.
	c.Datasets = append(c.Datasets, DatasetConfig{Name: child, Snapshot: &SnapshotSettings{StartAt: testPolicy().StartAt, Keep: Keep{Hourly: 48, Daily: 7}}})
	c.Datasets[1].Snapshot.Keep.Hourly = 30
	body, _ = json.Marshal(c)
	if w := perform(a, "PUT", "/api/config", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if p, _, _ := a.config.ResolveSnapshot(child); p.Keep.Hourly != 48 {
		t.Fatal("parent edit overwrote local settings")
	}
	c.Datasets[len(c.Datasets)-1] = DatasetConfig{Name: child}
	body, _ = json.Marshal(c)
	if w := perform(a, "PUT", "/api/config", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted, err := NewApp(a.opts)
	if err != nil {
		t.Fatal(err)
	}
	if p, source, _ := restarted.config.ResolveSnapshot(child); p.Keep.Hourly != 30 || source != "tank/home/apps" {
		t.Fatal("inheritance was not restored after restart", source, p)
	}
	if time.Until(resolved.Events[0].RunAt) < -time.Second {
		t.Fatal("preview returned a past event")
	}
}

func TestInheritanceModesRejectAmbiguousOverrides(t *testing.T) {
	for _, d := range []DatasetConfig{
		{Name: "tank/home", SnapshotMode: "bad"},
		{Name: "tank/home", SnapshotMode: "inherit", Policy: "test"},
		{Name: "tank/home", SnapshotMode: "none", Policy: "test"},
		{Name: "tank/home", SnapshotMode: "inherit", Snapshot: &SnapshotSettings{StartAt: testPolicy().StartAt, Keep: testPolicy().Keep}},
	} {
		c := inheritanceConfig()
		c.Datasets[0] = d
		if err := ValidateConfig(c); err == nil {
			t.Fatal("ambiguous inheritance accepted", d)
		}
	}
}
