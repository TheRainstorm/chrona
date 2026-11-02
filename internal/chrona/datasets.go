package chrona

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

type SnapshotSettings struct {
	StartAt string `json:"start_at" yaml:"start_at"`
	Keep    Keep   `json:"keep" yaml:"keep"`
}

type DatasetConfig struct {
	Name         string            `json:"name" yaml:"name"`
	Disabled     bool              `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	Policy       string            `json:"policy,omitempty" yaml:"policy,omitempty"`
	Snapshot     *SnapshotSettings `json:"snapshot,omitempty" yaml:"snapshot,omitempty"`
	SnapshotMode string            `json:"snapshot_mode,omitempty" yaml:"snapshot_mode,omitempty"`
	BackupMode   string            `json:"-" yaml:"backup_mode,omitempty"`
}

func localPolicyID(dataset string) string {
	return fmt.Sprintf("dataset-%x", sha256.Sum256([]byte(dataset)))[:24]
}

func NormalizeConfig(c Config) Config {
	if c.Version == 1 {
		c = normalizeLegacyConfig(c)
	}
	r := c
	r.Policies = append([]Policy{}, c.Policies...)
	r.BackupPolicies = append([]BackupPolicy{}, c.BackupPolicies...)
	for i := range r.BackupPolicies {
		r.BackupPolicies[i].Weekdays = append([]int{}, c.BackupPolicies[i].Weekdays...)
	}
	r.Backups = append([]Backup{}, c.Backups...)
	r.Datasets = append([]DatasetConfig{}, c.Datasets...)
	for i := range r.Datasets {
		// Accept old files without retaining obsolete backup inheritance settings.
		if r.Datasets[i].BackupMode == "inherit" || r.Datasets[i].BackupMode == "none" {
			r.Datasets[i].BackupMode = ""
		}
		if r.Datasets[i].Snapshot != nil {
			s := *r.Datasets[i].Snapshot
			r.Datasets[i].Snapshot = &s
		}
	}
	for i := range r.Backups {
		r.Backups[i].Datasets = append([]BackupDataset{}, r.Backups[i].Datasets...)
		r.Backups[i].KeepWeekly = 0
		r.Backups[i].KeepMonthly = 0
		r.Backups[i].Weekdays = append([]int{}, r.Backups[i].Weekdays...)
		r.Backups[i].PolicyName = ""
		if r.Backups[i].Host == "local" && r.Backups[i].Port == 0 {
			r.Backups[i].Port = 22
		}
	}
	if c.Version == 2 {
		for i, p := range r.Policies {
			for _, name := range p.Datasets {
				r.Datasets = append(r.Datasets, DatasetConfig{Name: name, Policy: p.ID, Disabled: !p.Enabled})
			}
			r.Policies[i].Dataset = ""
			r.Policies[i].Datasets = nil
		}
		for _, b := range r.Backups {
			for _, binding := range b.Datasets {
				found := false
				for _, d := range r.Datasets {
					if d.Name == binding.Source {
						found = true
						break
					}
				}
				if !found {
					r.Datasets = append(r.Datasets, DatasetConfig{Name: binding.Source})
				}
			}
		}
		r.Version = 3
	}
	for i := range r.Policies {
		r.Policies[i].Enabled = true
	}
	if c.BackupPolicies == nil {
		migrateLegacyBackupPolicies(&r)
	}
	return r
}

func validateDatasetConfig(c Config, d DatasetConfig) error {
	if d.BackupMode != "" {
		return errors.New("backup_mode 已废弃，请直接为数据集设置备份计划")
	}
	if d.SnapshotMode != "" && d.SnapshotMode != "inherit" && d.SnapshotMode != "none" {
		return errors.New("snapshot_mode 只能是 inherit 或 none")
	}
	if d.SnapshotMode != "" && (d.Policy != "" || d.Snapshot != nil) {
		return errors.New("继承或停止继承时不能同时设置本级策略")
	}
	if d.Policy != "" && d.Snapshot != nil {
		return errors.New("引用公共策略与独立策略只能选择一种")
	}
	if d.Policy != "" {
		for _, p := range c.Policies {
			if p.ID == d.Policy {
				return nil
			}
		}
		return errors.New("引用的公共策略不存在")
	}
	if d.Snapshot != nil {
		return ValidatePolicy(Policy{ID: localPolicyID(d.Name), Label: d.Name, StartAt: d.Snapshot.StartAt, Keep: d.Snapshot.Keep})
	}
	return nil
}

func parentDataset(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return ""
}

// Dataset paths form a tree. Only complete path components are ancestors.
func (c Config) ManagedAncestor(name string) string {
	for candidate := parentDataset(name); candidate != ""; candidate = parentDataset(candidate) {
		for _, d := range c.Datasets {
			if d.Name == candidate {
				return candidate
			}
		}
	}
	return ""
}

func (c Config) IsManagedDataset(name string) bool {
	for _, d := range c.Datasets {
		if d.Name == name {
			return true
		}
	}
	return c.ManagedAncestor(name) != ""
}

// Explicit entries keep their order. Inventory descendants join their managed
// parent's scope without copying policy values into the configuration file.
func (c Config) ManagedDatasets(inventory ...Dataset) []DatasetConfig {
	result := append([]DatasetConfig{}, c.Datasets...)
	seen := map[string]bool{}
	for _, d := range result {
		seen[d.Name] = true
	}
	for _, d := range inventory {
		if seen[d.Name] {
			continue
		}
		for candidate := parentDataset(d.Name); candidate != ""; candidate = parentDataset(candidate) {
			if seen[candidate] {
				result = append(result, DatasetConfig{Name: d.Name})
				seen[d.Name] = true
				break
			}
		}
	}
	return result
}

// ResolveSnapshot returns the closest explicit rule and the dataset that owns
// it. Pausing is local to a dataset; snapshot_mode:none is an inheritance barrier.
func (c Config) ResolveSnapshot(name string) (Policy, string, bool) {
	return c.resolveSnapshot(name, c.datasetIndex())
}

func (c Config) datasetIndex() map[string]DatasetConfig {
	byName := map[string]DatasetConfig{}
	for _, d := range c.Datasets {
		byName[d.Name] = d
	}
	return byName
}

func (c Config) resolveSnapshot(name string, byName map[string]DatasetConfig) (Policy, string, bool) {
	for candidate := name; candidate != ""; candidate = parentDataset(candidate) {
		d, exists := byName[candidate]
		if !exists {
			continue
		}
		if d.SnapshotMode == "none" {
			return Policy{}, candidate, false
		}
		var p Policy
		if d.Snapshot != nil {
			p = Policy{ID: localPolicyID(candidate), Label: candidate, StartAt: d.Snapshot.StartAt, Keep: d.Snapshot.Keep}
		} else if d.Policy != "" {
			for _, shared := range c.Policies {
				if shared.ID == d.Policy {
					p = shared
					break
				}
			}
		} else {
			continue
		}
		p.Dataset = name
		p.Datasets = nil
		p.Enabled = !byName[name].Disabled
		return p, candidate, true
	}
	return Policy{}, "", false
}

func (c Config) EffectivePolicies(inventory ...Dataset) []Policy {
	result := []Policy{}
	byName := c.datasetIndex()
	for _, d := range c.ManagedDatasets(inventory...) {
		if p, _, ok := c.resolveSnapshot(d.Name, byName); ok {
			result = append(result, p)
		}
	}
	return result
}

// Keep the user-facing file centered on datasets and references. Runtime and
// compatibility fields on Policy are not serialized into public templates.
func (c Config) MarshalYAML() (any, error) {
	if c.Version != 3 {
		type plain Config
		return plain(c), nil
	}
	type rule struct {
		ID      string `yaml:"id"`
		Label   string `yaml:"label"`
		StartAt string `yaml:"start_at"`
		Keep    Keep   `yaml:"keep"`
	}
	policies := []rule{}
	for _, p := range c.Policies {
		policies = append(policies, rule{p.ID, p.Label, p.StartAt, p.Keep})
	}
	return struct {
		Version        int             `yaml:"version"`
		Timezone       string          `yaml:"timezone"`
		JitterSeconds  int             `yaml:"jitter_seconds"`
		Policies       []rule          `yaml:"policies"`
		Datasets       []DatasetConfig `yaml:"datasets"`
		BackupPolicies []BackupPolicy  `yaml:"backup_policies"`
		Backups        []Backup        `yaml:"backups"`
	}{c.Version, c.Timezone, c.JitterSeconds, policies, c.Datasets, c.BackupPolicies, c.Backups}, nil
}

// Inventory rows are visible without registering them or enabling scheduling.
func (a *App) visibleDatasetsLocked() []DatasetConfig {
	result := append([]DatasetConfig{}, a.config.Datasets...)
	seen := map[string]bool{}
	for _, d := range result {
		seen[d.Name] = true
	}
	for _, d := range a.datasets {
		if !seen[d.Name] {
			result = append(result, DatasetConfig{Name: d.Name})
			seen[d.Name] = true
		}
	}
	return result
}
