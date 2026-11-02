package chrona

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// Primitive schedule tests intentionally use a detached time definition.
func customBackupForTest(c Config, index int) Backup {
	b := c.ResolveBackup(c.Backups[index])
	b.Policy, b.PolicyName = "", ""
	return b
}

func TestLegacyBackupPolicyMigrationPreservesTargetsAndFile(t *testing.T) {
	c := DefaultConfig(time.Now())
	for i := range c.Backups {
		c.Backups[i] = customBackupForTest(c, i)
	}
	c.Backups[0].Enabled = false
	c.Backups[1].Datasets[1].Disabled = true
	c.Backups[1].Host, c.Backups[1].Port = c.Backups[0].Host, c.Backups[0].Port+1
	c.BackupPolicies = nil
	// Reproduce the old format with the new field absent.
	payload, _ := yaml.Marshal(c)
	text := strings.Replace(string(payload), "backup_policies: []\n", "", 1)
	if strings.Contains(text, "backup_policies:") {
		t.Fatal("fixture is not legacy", text)
	}
	dir := t.TempDir()
	opts := Options{Mode: "demo", ConfigPath: filepath.Join(dir, "config.yaml"), StatePath: filepath.Join(dir, "state.json")}
	if err := os.WriteFile(opts.ConfigPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	after, _ := os.ReadFile(opts.ConfigPath)
	if string(after) != text {
		t.Fatal("load rewrote config")
	}
	if len(a.config.BackupPolicies) != 2 {
		t.Fatal(a.config.BackupPolicies)
	}
	for i, b := range a.config.Backups {
		old := c.Backups[i]
		resolved := a.config.ResolveBackup(b)
		if b.ID != old.ID || b.Host != old.Host || b.Port != old.Port || b.Enabled != old.Enabled || !reflect.DeepEqual(b.Datasets, old.Datasets) || b.Policy != old.ID {
			t.Fatal("target/identity/pause changed", b, old)
		}
		if resolved.Cadence != old.Cadence || resolved.At != old.At || !reflect.DeepEqual(resolved.Weekdays, old.Weekdays) {
			t.Fatal("time changed")
		}
	}
	normalized, _ := json.Marshal(a.config)
	var doc struct {
		BackupPolicies []map[string]any `json:"backup_policies"`
		Backups        []map[string]any `json:"backups"`
	}
	if err := json.Unmarshal(normalized, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range doc.BackupPolicies {
		for _, key := range []string{"host", "port", "datasets", "enabled"} {
			if _, ok := p[key]; ok {
				t.Fatal("destination leaked into policy", key)
			}
		}
	}
	for _, b := range doc.Backups {
		for _, key := range []string{"cadence", "weekdays", "month_day", "at", "grace_minutes"} {
			if _, ok := b[key]; ok {
				t.Fatal("inline time leaked into reference", key)
			}
		}
	}
	body, _ := json.Marshal(a.config)
	if w := perform(a, "PUT", "/api/config", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, _ := os.ReadFile(opts.ConfigPath)
	if !strings.Contains(string(saved), "backup_policies:") {
		t.Fatal("save omitted definitions")
	}
	if _, err := DecodeConfig(saved); err != nil {
		t.Fatal("saved config cannot reload", err)
	}
}

func TestSharedBackupPolicyChangesTimeAcrossDifferentMachinesOnly(t *testing.T) {
	a := testApp(t)
	c := NormalizeConfig(a.config)
	c.Backups[1].Policy = c.Backups[0].Policy
	c.Backups[1].Host, c.Backups[1].Port = "local", 22
	if err := a.applyConfigLocked(c); err != nil {
		t.Fatal(err)
	}
	for _, b := range a.config.Backups {
		if err := a.finishDemoBackupLocked(b, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string]BackupState{}
	for id, s := range a.state.Backups {
		before[id] = s
	}
	old := NormalizeConfig(a.config)
	c = NormalizeConfig(a.config)
	c.BackupPolicies[0].Cadence, c.BackupPolicies[0].MonthDay, c.BackupPolicies[0].At, c.BackupPolicies[0].GraceMinutes = "monthly", 31, "18:39", 73
	payload, _ := json.Marshal(c)
	if w := perform(a, "PUT", "/api/config", string(payload)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for i, task := range a.config.Backups {
		b := a.config.ResolveBackup(task)
		if b.Host != old.Backups[i].Host || b.Port != old.Backups[i].Port || !reflect.DeepEqual(b.Datasets, old.Backups[i].Datasets) || b.ID != old.Backups[i].ID {
			t.Fatal("time edit changed endpoint", b)
		}
		if b.Cadence != "monthly" || b.MonthDay != 31 || b.At != "18:39" || b.GraceMinutes != 73 {
			t.Fatal("shared time did not propagate", b)
		}
		state := a.state.Backups[b.ID]
		if !reflect.DeepEqual(state.Bases, before[b.ID].Bases) || !reflect.DeepEqual(state.Completed, before[b.ID].Completed) {
			t.Fatal("time edit lost existing bases or history")
		}
	}
	if a.endpoint(a.config.ResolveBackup(a.config.Backups[1])).host != "" {
		t.Fatal("local used SSH")
	}
	w := perform(a, "GET", "/api/state", "")
	var state struct {
		Backups []struct {
			Task Backup `json:"task"`
		} `json:"backups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, entry := range state.Backups {
		if entry.Task.PolicyName == "" || entry.Task.At != "18:39" {
			t.Fatal("state didn't resolve shared time", entry)
		}
	}
}

func TestInvalidBackupPolicyReferencesDoNotChangeConfiguration(t *testing.T) {
	a := testApp(t)
	before, _ := os.ReadFile(a.opts.ConfigPath)
	for _, variant := range []string{"missing", "delete-used", "duplicate", "mixed", "host-in-policy"} {
		c := NormalizeConfig(a.config)
		switch variant {
		case "missing":
			c.Backups[0].Policy = "missing"
		case "delete-used":
			c.BackupPolicies = c.BackupPolicies[1:]
		case "duplicate":
			c.BackupPolicies = append(c.BackupPolicies, c.BackupPolicies[0])
		case "mixed":
			c.Backups[0].At = "13:00"
		}
		payload, _ := json.Marshal(c)
		if variant == "host-in-policy" {
			payload = []byte(strings.Replace(string(payload), `"backup_policies":[{`, `"backup_policies":[{"host":"bad",`, 1))
		}
		if w := perform(a, "PUT", "/api/config", string(payload)); w.Code != 422 {
			t.Fatal(variant, w.Code, w.Body.String())
		}
		after, _ := os.ReadFile(a.opts.ConfigPath)
		if string(after) != string(before) {
			t.Fatal(variant, "replaced valid config")
		}
	}
}

func TestCustomBackupTimeAndLocalDefaultRemainIndependent(t *testing.T) {
	c := DefaultConfig(time.Now())
	b := customBackupForTest(c, 0)
	b.ID, b.Host, b.Port, b.Private = "custom-local", "local", 0, true
	b.Cadence, b.MonthDay, b.At, b.GraceMinutes = "monthly", 28, "22:17", 19
	c.Backups = append(c.Backups, b)
	c = NormalizeConfig(c)
	if err := ValidateConfig(c); err != nil {
		t.Fatal(err)
	}
	if c.Backups[2].Port != 22 || c.Backups[2].Policy != "" {
		t.Fatal("local/custom normalization failed")
	}
	c.BackupPolicies[0].At = "09:09"
	if c.ResolveBackup(c.Backups[2]).At != "22:17" {
		t.Fatal("custom time followed public rule")
	}
}

func TestSplittingGroupedBackupKeepsOnlySameTargetsHistoryAndHolds(t *testing.T) {
	for _, changeTarget := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-target-new-time", true: "different-target"}[changeTarget], func(t *testing.T) {
			a := testApp(t)
			old := a.config.Backups[1]
			if err := a.finishDemoBackupLocked(old, time.Now()); err != nil {
				t.Fatal(err)
			}
			selected, other := old.Datasets[0].Source, old.Datasets[1].Source
			prior := a.state.Backups[old.ID]
			c := NormalizeConfig(a.config)
			split := old
			split.ID, split.Policy, split.Datasets = "separated-binding", c.BackupPolicies[0].ID, []BackupDataset{old.Datasets[0]}
			if changeTarget {
				split.Host = "different-peer"
			}
			c.Backups[1].Datasets = []BackupDataset{old.Datasets[1]}
			c.Backups = append(c.Backups, split)
			if err := a.applyConfigLocked(c); err != nil {
				t.Fatal(err)
			}
			sibling := a.state.Backups[old.ID]
			if sibling.Bases[other] != prior.Bases[other] || !sibling.Completed[other].Equal(prior.Completed[other]) {
				t.Fatal("split affected sibling")
			}
			if sibling.Bases[selected] != "" {
				t.Fatal("old job still owns removed binding")
			}
			got := a.state.Backups[split.ID]
			if changeTarget {
				if got.Bases[selected] != "" || !got.Completed[selected].IsZero() {
					t.Fatal("new target inherited old success")
				}
				if !a.hasUninitializedBackupLocked(selected) {
					t.Fatal("new target must defer source cleanup")
				}
				return
			}
			if got.Bases[selected] != prior.Bases[selected] || !got.Completed[selected].Equal(prior.Completed[selected]) || got.Transfers[selected].RemoteSnapshot != prior.Transfers[selected].RemoteSnapshot {
				t.Fatal("time-only split lost prior base or history", got)
			}
			replica := findReplica(a.state.Replicas, split, split.Datasets[0], got.Bases[selected])
			if replica == nil || replica.TaskID != split.ID || !reflect.DeepEqual(replica.HeldBy, []string{split.ID}) {
				t.Fatal("receiver hold not moved", replica)
			}
			for _, s := range a.state.Snapshots {
				if s.Name == got.Bases[selected] && !reflect.DeepEqual(s.HeldBy, []string{split.ID}) {
					t.Fatal("source hold not moved", s)
				}
			}
			if err := a.finishDemoBackupLocked(split, time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if a.state.Backups[split.ID].Transfers[selected].Mode != "incremental" {
				t.Fatal("split fell back to full transfer")
			}
		})
	}
}

func TestNativeSplitProtectsBothEndsBeforeRetiringOldTag(t *testing.T) {
	for _, failReceiver := range []bool{false, true} {
		t.Run(map[bool]string{false: "both-held", true: "receiver-unavailable"}[failReceiver], func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "commands")
			script := `#!/bin/sh
printf '%s' "$1" >> "$CHRONA_SPLIT_LOG"
for arg in "$@"; do last="$arg"; printf '\t%s' "$arg" >> "$CHRONA_SPLIT_LOG"; done
printf '\n' >> "$CHRONA_SPLIT_LOG"
case "$1" in
 holds) printf '%s\tchrona:previous\t1\n' "$last";;
 hold) if [ "$CHRONA_SPLIT_FAIL" = yes ] && [ "$last" = backup/dst@base ]; then exit 1; fi;;
 release|set) ;;
 list)
  case "$*" in
   *name,type*) printf '%s\tfilesystem\n' "$last";;
   *) case "$last" in *@*) name="$last";; *) name="$last@base";; esac
      printf '%s\t123\t1700000000\t0\t4102444800\t1\n' "$name";;
  esac;;
 *) exit 1;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "zfs"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			t.Setenv("CHRONA_SPLIT_LOG", log)
			if failReceiver {
				t.Setenv("CHRONA_SPLIT_FAIL", "yes")
			} else {
				t.Setenv("CHRONA_SPLIT_FAIL", "no")
			}
			a := testApp(t)
			a.opts.Mode, a.opts.AllowWrites = "zfs", true
			a.datasets = []Dataset{{Name: "tank/src"}, {Name: "backup/dst"}}
			b := customBackupForTest(a.config, 0)
			b.ID, b.Host, b.Port, b.Datasets = "previous", "local", 22, []BackupDataset{{Source: "tank/src", Destination: "backup/dst"}}
			old := a.config
			old.Backups = []Backup{b}
			b.ID = "replacement"
			next := old
			next.Backups = []Backup{b}
			a.state.Backups = map[string]BackupState{b.ID: {Bases: map[string]string{"tank/src": "tank/src@base"}}}
			a.scheduleNativeMaintenanceLocked(old, next)
			a.workers.Wait()
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			calls := string(data)
			if failReceiver {
				if strings.Contains(calls, "release\trelease\tchrona:previous") {
					t.Fatal("released old hold before new peer hold existed", calls)
				}
				if a.state.Backups[b.ID].Warning == "" {
					t.Fatal("failed protection was not reported")
				}
			} else {
				local := strings.Index(calls, "hold\thold\tchrona:replacement\ttank/src@base")
				remote := strings.Index(calls, "hold\thold\tchrona:replacement\tbackup/dst@base")
				release := strings.Index(calls, "release\trelease\tchrona:previous")
				if local < 0 || remote < 0 || release < local || release < remote {
					t.Fatal("old hold retired before both new holds", calls)
				}
			}
			a.Close()
		})
	}
}
