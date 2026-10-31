package chrona

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	"go.yaml.in/yaml/v3"
)

type Pool struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Health string `json:"health"`
	Size   uint64 `json:"size"`
	Used   uint64 `json:"used"`
	Free   uint64 `json:"free"`
}

type Dataset struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}

type Snapshot struct {
	GUID      string    `json:"guid,omitempty"`
	Name      string    `json:"name"`
	Dataset   string    `json:"dataset"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Tier      string    `json:"tier"`
	Used      uint64    `json:"used"`
	HeldBy    []string  `json:"held_by"`
	Managed   bool      `json:"managed"`
}

type Log struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Action  string    `json:"action"`
	Dataset string    `json:"dataset"`
	Message string    `json:"message"`
}

type BackupState struct {
	Status       string                    `json:"status"`
	Progress     int                       `json:"progress"`
	LastSuccess  time.Time                 `json:"last_success"`
	LastSnapshot string                    `json:"last_snapshot"`
	Bases        map[string]string         `json:"bases"`
	Completed    map[string]time.Time      `json:"completed"`
	Bytes        uint64                    `json:"bytes"`
	Warning      string                    `json:"warning,omitempty"`
	Error        string                    `json:"error,omitempty"`
	Transfers    map[string]TransferResult `json:"transfers,omitempty"`
}

type TransferResult struct {
	Mode           string    `json:"mode"`
	Base           string    `json:"base,omitempty"`
	RemoteSnapshot string    `json:"remote_snapshot"`
	NextBackupAt   time.Time `json:"next_backup_at"`
	ProtectedUntil time.Time `json:"protected_until"`
}

type ReplicaSnapshot struct {
	Snapshot
	TaskID         string `json:"task_id"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	SourceSnapshot string `json:"source_snapshot"`
}

type State struct {
	Mode      string                 `json:"mode,omitempty"`
	Snapshots []Snapshot             `json:"snapshots"`
	Logs      []Log                  `json:"logs"`
	Backups   map[string]BackupState `json:"backups"`
	Replicas  []ReplicaSnapshot      `json:"replicas"`
}

// Write a sibling temporary file, sync, and rename so a failed write keeps the old config.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".chrona-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	if d, e := os.Open(filepath.Dir(path)); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func saveConfig(path string, c Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}
func saveState(path string, state State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func DefaultConfig(now time.Time) Config {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	t := now.In(loc)
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -3, 0).Format(time.RFC3339)
	high := Keep{96, 72, 35, 8, 12}
	ssd := Keep{4, 12, 7, 4, 3}
	return NormalizeConfig(Config{Version: 2, Timezone: "Asia/Shanghai", JitterSeconds: 45,
		Policies: []Policy{
			{ID: "fine", Label: "精细保留", Datasets: []string{"Saturn/Archive", "Saturn/APP/Immich"}, StartAt: start, Enabled: true, Keep: high},
			{ID: "files", Label: "日常文件", Datasets: []string{"Saturn/Disk1"}, StartAt: start, Enabled: true, Keep: Keep{12, 24, 14, 8, 12}},
			{ID: "ssd", Label: "SSD 轻量保护", Datasets: []string{"rpool/data/vm-201-ares-root", "rpool/data/vm-201-ares-docker", "rpool/ROOT/pve-1"}, StartAt: start, Enabled: true, Keep: ssd},
		},
		Backups: []Backup{
			{ID: "archive-n5105", Name: "文档异机备份", Datasets: []BackupDataset{{Source: "Saturn/Archive", Destination: "rpool/backup/Archive"}}, Host: "n5105", Port: 2202, Cadence: "weekly", Weekdays: []int{1, 5}, MonthDay: 1, At: "05:45", Enabled: true, KeepWeekly: 8, KeepMonthly: 12},
			{ID: "ares-saturn", Name: "Ares 系统与应用", Datasets: []BackupDataset{{Source: "rpool/data/vm-201-ares-root", Destination: "Saturn/backup/ares-root"}, {Source: "rpool/data/vm-201-ares-docker", Destination: "Saturn/backup/ares-docker"}}, Host: "nas-pve", Port: 2202, Cadence: "monthly", Weekdays: []int{0}, MonthDay: 1, At: "04:30", Enabled: true, KeepWeekly: 0, KeepMonthly: 6},
		},
	})
}

func demoInventory() ([]Pool, []Dataset) {
	pools := []Pool{{"Saturn", "HDD · RAIDZ1", "ONLINE", 63995012710400, 22826034675712, 41168978034688}, {"rpool", "NVMe · SSD", "ONLINE", 854698491904, 387447263232, 467251228672}}
	datasets := []Dataset{
		{"Saturn", "filesystem", 22826034675712, 29774397822400},
		{"Saturn/APP", "filesystem", 136086986304, 29774397822400},
		{"Saturn/Archive", "filesystem", 47076778560, 29774397822400},
		{"Saturn/Disk1", "filesystem", 12085444589184, 29774397822400},
		{"Saturn/APP/Immich", "filesystem", 97562838912, 29774397822400},
		{"Saturn/APP/Seafile", "filesystem", 38524147392, 29774397822400},
		{"Saturn/VM", "filesystem", 916806648000, 29774397822400},
		{"rpool", "filesystem", 387447263232, 440692482048},
		{"rpool/data", "filesystem", 160101621760, 440692482048},
		{"rpool/ROOT", "filesystem", 16752951296, 440692482048},
		{"rpool/data/vm-201-ares-root", "volume", 119047020544, 440692482048},
		{"rpool/data/vm-201-ares-docker", "volume", 41054601216, 440692482048},
		{"rpool/ROOT/pve-1", "filesystem", 16752951296, 440692482048},
	}
	return pools, datasets
}

func mockBytes(seed string) uint64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return uint64(h.Sum32()%900+16) * 1024 * 1024
}

func seedState(c Config, now time.Time) State {
	s := State{Snapshots: []Snapshot{}, Logs: []Log{}, Backups: map[string]BackupState{}}
	loc, _ := time.LoadLocation(c.Timezone)
	_, inventory := demoInventory()
	for _, p := range c.EffectivePolicies(inventory...) {
		anchor := policyAnchor(p, loc)
		values := p.Keep.Values()
		merged := map[int64]Event{}
		offset := stableOffset(p.ID, p.Dataset, c.JitterSeconds)
		for rank, keep := range values {
			if keep == 0 {
				continue
			}
			index := lastIndex(anchor, now.Add(-time.Duration(offset)*time.Second), rank)
			for k := 0; k < keep && k < 200 && index-k >= 0; k++ {
				n := index - k
				at := slotAt(anchor, rank, n)
				key := at.Unix()
				e := Event{PlannedAt: at, RunAt: at.Add(time.Duration(offset) * time.Second), ExpiresAt: slotAt(anchor, rank, n+keep).Add(time.Duration(offset) * time.Second), Tier: Tiers[rank].Key, rank: rank}
				old, exists := merged[key]
				if !exists || rank > old.rank {
					merged[key] = e
				}
			}
		}
		for _, e := range merged {
			if !e.ExpiresAt.After(now) {
				continue
			}
			name := p.Dataset + "@" + SnapshotPrefix + e.PlannedAt.UTC().Format("20060102T150405Z")
			s.Snapshots = append(s.Snapshots, Snapshot{Name: name, Dataset: p.Dataset, CreatedAt: e.RunAt, ExpiresAt: e.ExpiresAt, Tier: e.Tier, Used: mockBytes(name), HeldBy: []string{}, Managed: true})
		}
	}
	sortSnapshots(s.Snapshots)
	for i, task := range c.Backups {
		b := c.EffectiveBackup(task, inventory...)
		state := BackupState{Status: "success", LastSuccess: now.Add(-time.Duration(24+i*12) * time.Hour), Bytes: uint64(12+i*3) * 1024 * 1024 * 1024, Bases: map[string]string{}, Completed: map[string]time.Time{}}
		for _, binding := range b.Datasets {
			for j := range s.Snapshots {
				if s.Snapshots[j].Dataset == binding.Source {
					s.Snapshots[j].HeldBy = append(s.Snapshots[j].HeldBy, b.ID)
					state.Bases[binding.Source] = s.Snapshots[j].Name
					state.Completed[binding.Source] = state.LastSuccess
					if state.LastSnapshot == "" {
						state.LastSnapshot = s.Snapshots[j].Name
					}
					break
				}
			}
		}
		s.Backups[b.ID] = state
	}
	for i, p := range c.EffectivePolicies(inventory...) {
		if !p.Enabled {
			continue
		}
		s.Logs = append(s.Logs, Log{ID: fmt.Sprintf("seed-%d", i), At: now.Add(-time.Duration(i*13+2) * time.Minute), Level: "success", Action: "snapshot", Dataset: p.Dataset, Message: "演示快照已创建，过期时间已写入"})
	}
	initializeDemoReplicas(c, &s, inventory, now)
	return s
}
