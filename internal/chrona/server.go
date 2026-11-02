package chrona

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"homelab/chrona/internal/buildinfo"

	"go.yaml.in/yaml/v3"
)

type Options struct {
	ConfigPath, StatePath, Mode string
	AllowWrites                 bool
	AuthSecret, SSHControlDir   string
}
type App struct {
	closing       bool
	mu            sync.RWMutex
	config        Config
	state         State
	opts          Options
	pools         []Pool
	datasets      []Dataset
	started       time.Time
	hostname      string
	lastCheck     time.Time
	workerCtx     context.Context
	cancelWorkers context.CancelFunc
	workers       sync.WaitGroup
	configHash    [32]byte
}

func NewApp(opts Options) (*App, error) {
	if opts.Mode != "demo" && opts.Mode != "zfs" {
		return nil, errors.New("mode 必须是 demo 或 zfs")
	}
	data, err := os.ReadFile(opts.ConfigPath)
	var config Config
	if errors.Is(err, os.ErrNotExist) {
		if opts.Mode != "demo" {
			return nil, errors.New("真实 ZFS 模式需要已有配置文件")
		}
		config = DefaultConfig(time.Now())
		if err = saveConfig(opts.ConfigPath, config); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		config, err = DecodeConfig(data)
		if err != nil {
			return nil, err
		}
	}
	data, err = os.ReadFile(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	var original struct {
		Version int `yaml:"version"`
	}
	if yaml.Unmarshal(data, &original) == nil && original.Version > 0 && original.Version < 3 {
		if err = atomicWrite(fmt.Sprintf("%s.v%d.bak", opts.ConfigPath, original.Version), data); err != nil {
			return nil, err
		}
		if err = saveConfig(opts.ConfigPath, config); err != nil {
			return nil, err
		}
		data, err = os.ReadFile(opts.ConfigPath)
		if err != nil {
			return nil, err
		}
	}
	a := &App{config: config, opts: opts, started: time.Now(), lastCheck: time.Now(), hostname: "nas-pve"}
	a.workerCtx, a.cancelWorkers = context.WithCancel(context.Background())
	a.configHash = sha256.Sum256(data)
	if opts.Mode == "demo" {
		a.pools, a.datasets = demoInventory()
		if data, e := os.ReadFile(opts.StatePath); e == nil {
			if e = json.Unmarshal(data, &a.state); e != nil {
				return nil, fmt.Errorf("演示状态文件无法解析：%w", e)
			}
		} else if errors.Is(e, os.ErrNotExist) {
			a.state = seedState(config, time.Now())
		} else {
			return nil, e
		}
		if a.state.Backups == nil {
			a.state.Backups = map[string]BackupState{}
		}
		for id, b := range a.state.Backups {
			if b.Status == "running" {
				b.Status = "error"
				b.Error = "演示服务重启，上次模拟传输已中断"
				a.state.Backups[id] = b
			}
		}
		for _, b := range config.Backups {
			state := a.state.Backups[b.ID]
			if state.Bases == nil {
				state.Bases = map[string]string{}
				for _, binding := range b.Datasets {
					if strings.HasPrefix(state.LastSnapshot, binding.Source+"@") {
						state.Bases[binding.Source] = state.LastSnapshot
					}
				}
				a.state.Backups[b.ID] = state
			}
			if state.Completed == nil {
				state.Completed = map[string]time.Time{}
				for source := range state.Bases {
					state.Completed[source] = state.LastSuccess
				}
				a.state.Backups[b.ID] = state
			}
		}
		initializeDemoReplicas(config, &a.state, a.datasets, time.Now())
		a.persistLocked()
	} else {
		a.hostname, _ = os.Hostname()
		a.state.Backups = map[string]BackupState{}
		if saved, e := os.ReadFile(opts.StatePath); e == nil {
			if err = json.Unmarshal(saved, &a.state); err != nil {
				return nil, err
			}
			if a.state.Mode != "zfs" {
				return nil, errors.New("真实模式不能使用演示状态文件")
			}
			if a.state.Backups == nil {
				a.state.Backups = map[string]BackupState{}
			}
			for id, state := range a.state.Backups {
				if state.Status == "running" {
					state.Status = "error"
					state.Error = "服务重启，未完成的传输将在下次运行恢复"
					a.state.Backups[id] = state
				}
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		if opts.SSHControlDir != "" {
			if err = os.MkdirAll(opts.SSHControlDir, 0700); err != nil {
				return nil, err
			}
		}
		if err = a.refreshRealLocked(context.Background()); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *App) record(level, action, dataset, message string) {
	a.state.Logs = append([]Log{{ID: strconv.FormatInt(time.Now().UnixNano(), 36), At: time.Now(), Level: level, Action: action, Dataset: dataset, Message: message}}, a.state.Logs...)
	if len(a.state.Logs) > 1000 {
		a.state.Logs = a.state.Logs[:1000]
	}
}

func (a *App) persistLocked() {
	a.state.Mode = a.opts.Mode
	if a.opts.StatePath != "" {
		if err := saveState(a.opts.StatePath, a.state); err != nil {
			log.Printf("save demo state: %v", err)
		}
	}
}

func (a *App) refreshRealLocked(ctx context.Context) error {
	p, d, s, err := realInventory(ctx)
	if err != nil {
		return err
	}
	a.pools, a.datasets, a.state.Snapshots = p, d, s
	return nil
}

func (a *App) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	refresh := time.NewTicker(30 * time.Second)
	defer refresh.Stop()
	reload := time.NewTicker(5 * time.Second)
	defer reload.Stop()
	for {
		select {
		case <-ctx.Done():
			a.cancelWorkers()
			return
		case now := <-ticker.C:
			a.mu.Lock()
			loc, _ := time.LoadLocation(a.config.Timezone)
			if a.opts.Mode == "demo" || a.opts.AllowWrites {
				for _, p := range a.config.EffectivePolicies(a.datasets...) {
					if !p.Enabled {
						continue
					}
					events := NextEvents(p, loc, a.config.JitterSeconds, a.lastCheck, 1)
					if len(events) > 0 && !events[0].RunAt.After(now) {
						if err := a.snapshotLocked(ctx, p, events[0]); err != nil {
							a.record("error", "snapshot", p.Dataset, err.Error())
						}
					}
				}
			}
			for _, task := range a.config.Backups {
				b := a.config.EffectiveBackup(task, a.datasets...)
				active := false
				for _, d := range b.Datasets {
					active = active || !d.Disabled
				}
				if b.Enabled && active {
					next := NextBackup(b, loc, a.lastCheck)
					if !next.After(now) && !next.IsZero() {
						if err := a.startBackupLocked(b); err != nil {
							a.record("error", "backup", backupSources(b), err.Error())
						}
					}
				}
			}
			a.lastCheck = now
			a.mu.Unlock()
		case <-refresh.C:
			if a.opts.Mode == "zfs" {
				a.mu.Lock()
				if err := a.refreshRealLocked(ctx); err != nil {
					a.record("error", "inventory", "", err.Error())
				}
				a.mu.Unlock()
			}
		case <-reload.C:
			a.mu.Lock()
			if err := a.reloadConfigLocked(); err != nil {
				a.record("error", "config", "", err.Error())
			}
			a.mu.Unlock()
		}
	}
}

func (a *App) snapshotLocked(ctx context.Context, p Policy, event Event) error {
	if a.opts.Mode == "zfs" && !a.opts.AllowWrites {
		return errors.New("当前为 ZFS 只读模式")
	}
	name := p.Dataset + "@" + SnapshotPrefix + event.PlannedAt.UTC().Format("20060102T150405Z")
	for _, s := range a.state.Snapshots {
		if s.Name == name {
			return nil
		}
	}
	if a.opts.Mode == "zfs" {
		var err error
		name, err = createRealSnapshot(ctx, p.Dataset, event)
		if err != nil {
			return err
		}
		if err = a.refreshRealLocked(ctx); err != nil {
			return err
		}
	} else {
		a.state.Snapshots = append(a.state.Snapshots, Snapshot{Name: name, Dataset: p.Dataset, CreatedAt: time.Now(), ExpiresAt: event.ExpiresAt, Tier: event.Tier, Used: mockBytes(name), HeldBy: []string{}, Managed: true})
	}
	a.record("success", "snapshot", p.Dataset, fmt.Sprintf("%s档快照已创建 · %d 档合并 · 到期 %s", event.Label, len(event.Merged), event.ExpiresAt.Format("01-02 15:04")))
	// Only clean this exact dataset after the new snapshot has succeeded.
	kept := make([]Snapshot, 0, len(a.state.Snapshots))
	pruned := 0
	protected := a.protectedSnapshotNamesLocked()
	deferCleanup := a.hasUninitializedBackupLocked(p.Dataset)
	for _, s := range a.state.Snapshots {
		s = withSnapshotRetention(s)
		if !deferCleanup && s.Dataset == p.Dataset && !protected[s.Name] && snapshotExpired(s, time.Now()) {
			if a.opts.Mode == "zfs" {
				deleted, err := destroyRealSnapshot(ctx, s)
				if err != nil {
					a.record("error", "prune", s.Dataset, err.Error())
				}
				if !deleted {
					kept = append(kept, s)
					continue
				}
			}
			pruned++
			continue
		}
		kept = append(kept, s)
	}
	a.state.Snapshots = kept
	sortSnapshots(a.state.Snapshots)
	if pruned > 0 {
		a.record("success", "prune", p.Dataset, fmt.Sprintf("已清理 %d 张到期快照", pruned))
	}
	a.persistLocked()
	return nil
}

func (a *App) startBackupLocked(b Backup) error {
	b = a.config.ResolveBackup(b)
	active := []BackupDataset{}
	for _, binding := range b.Datasets {
		if binding.Disabled {
			continue
		}
		if !a.config.IsLeafDataset(binding.Source, a.datasets...) {
			return errors.New("只有叶子数据集可以执行备份；父级只用于层级展示")
		}
		active = append(active, binding)
	}
	b.Datasets = active
	if len(b.Datasets) == 0 {
		return errors.New("这个数据集的备份已暂停、不是叶子节点或尚未分配")
	}
	if a.opts.Mode == "zfs" {
		return a.startNativeBackupLocked(b)
	}
	if err := validateEffectiveBackupTargets(a.config, a.datasets); err != nil {
		return err
	}
	state := a.state.Backups[b.ID]
	if state.Status == "running" {
		return errors.New("这个备份任务正在运行")
	}
	a.pruneReplicasLocked(time.Now())
	state.Status, state.Progress, state.Error = "running", 0, ""
	a.state.Backups[b.ID] = state
	a.record("info", "backup", backupSources(b), "开始模拟传输 → "+b.Host)
	a.persistLocked()
	go func() {
		for i := 1; i <= 5; i++ {
			time.Sleep(350 * time.Millisecond)
			a.mu.Lock()
			s, exists := a.state.Backups[b.ID]
			if !exists || s.Status != "running" {
				a.mu.Unlock()
				return
			}
			s.Progress = i * 20
			a.state.Backups[b.ID] = s
			a.mu.Unlock()
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if err := a.finishDemoBackupLocked(b, time.Now()); err != nil {
			s := a.state.Backups[b.ID]
			s.Status = "error"
			s.Error = err.Error()
			a.state.Backups[b.ID] = s
			a.record("error", "backup", backupSources(b), err.Error())
			a.persistLocked()
		}
	}()
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func readJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}

func (a *App) Handler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", a.handleState)
	mux.HandleFunc("GET /api/snapshots", a.handleSnapshots)
	mux.HandleFunc("GET /api/dataset-plan", a.handleDatasetPlan)
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		writeJSON(w, 200, a.state.Logs)
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		data, err := yaml.Marshal(a.config)
		if err != nil {
			fail(w, 500, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("PUT /api/config", a.handleConfig)
	mux.HandleFunc("POST /api/preview", a.handlePreview)
	mux.HandleFunc("POST /api/dataset-preview", a.handleDatasetPreview)
	mux.HandleFunc("POST /api/policies", a.handleSavePolicy)
	mux.HandleFunc("DELETE /api/policies/{id}", a.handleDeletePolicy)
	mux.HandleFunc("POST /api/policies/{id}/run", a.handleRunPolicy)
	mux.HandleFunc("POST /api/backups", a.handleSaveBackup)
	mux.HandleFunc("DELETE /api/backups/{id}", a.handleDeleteBackup)
	mux.HandleFunc("POST /api/backups/{id}/run", a.handleRunBackup)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		result := map[string]string{"status": "ok", "mode": a.opts.Mode, "version": buildinfo.Version}
		if buildinfo.Commit != "" {
			result["commit"] = buildinfo.Commit
		}
		writeJSON(w, 200, result)
	})
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return authorize(a.opts.AuthSecret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					fail(w, 403, errors.New("不允许跨站修改配置"))
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	}))
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now()
	loc, _ := time.LoadLocation(a.config.Timezone)
	policies := make([]map[string]any, 0, len(a.config.Policies))
	active := 0
	for _, p := range a.config.Policies {
		var next *Event
		bindings := []map[string]any{}
		for _, dataset := range p.Datasets {
			var event *Event
			if p.Enabled {
				resolved := p
				resolved.Dataset = dataset
				events := NextEvents(resolved, loc, a.config.JitterSeconds, now, 1)
				if len(events) > 0 {
					e := events[0]
					event = &e
					if next == nil || e.RunAt.Before(next.RunAt) {
						next = &e
					}
				}
			}
			bindings = append(bindings, map[string]any{"dataset": dataset, "next": event})
		}
		if p.Enabled {
			active++
		}
		policies = append(policies, map[string]any{"policy": p, "next": next, "bindings": bindings})
	}
	backups := make([]map[string]any, 0, len(a.config.Backups))
	for _, task := range a.config.Backups {
		b := a.config.ResolveBackup(task)
		var next any
		if b.Enabled {
			next = NextBackup(b, loc, now)
		}
		backups = append(backups, map[string]any{"task": b, "state": a.state.Backups[b.ID], "next": next})
	}
	replicas := make([]ReplicaSnapshot, 0, len(a.state.Replicas))
	for _, replica := range a.state.Replicas {
		replicas = append(replicas, *replicaWithRetention(&replica))
	}
	var bytes uint64
	recent, permanent := 0, 0
	counts, latest := map[string]int{}, map[string]Snapshot{}
	for _, s := range a.state.Snapshots {
		bytes += s.Used
		if keepSnapshot(s.Name) {
			permanent++
		}
		counts[s.Dataset]++
		if s.CreatedAt.After(latest[s.Dataset].CreatedAt) {
			latest[s.Dataset] = withSnapshotRetention(s)
		}
		if s.CreatedAt.After(now.Add(-24 * time.Hour)) {
			recent++
		}
	}
	logs := a.state.Logs
	if len(logs) > 5 {
		logs = logs[:5]
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	rows := []map[string]any{}
	effective := a.config.EffectivePolicies(a.datasets...)
	byName := a.config.datasetIndex()
	for _, d := range a.visibleDatasetsLocked() {
		var snapshot *Policy
		var event *Event
		for _, p := range effective {
			if p.Dataset == d.Name {
				copy := p
				snapshot = &copy
				if p.Enabled {
					events := NextEvents(p, loc, a.config.JitterSeconds, now, 1)
					if len(events) > 0 {
						e := events[0]
						event = &e
					}
				}
				break
			}
		}
		leaf := a.config.IsLeafDataset(d.Name, a.datasets...)
		rowBackups := []map[string]any{}
		for _, resolved := range a.config.ResolveBackups(d.Name) {
			b, binding := resolved.Task, resolved.Binding
			var next any
			if leaf && b.Enabled && !binding.Disabled {
				next = NextBackup(b, loc, now)
			}
			state := a.state.Backups[b.ID]
			rowBackups = append(rowBackups, map[string]any{"task": b, "destination": binding.Destination, "origin": resolved.Origin, "inherited": resolved.Origin != d.Name, "state": state, "next": next, "disabled": binding.Disabled, "replica": replicaWithRetention(findReplica(a.state.Replicas, b, binding, state.Bases[d.Name]))})
		}
		_, source, _ := a.config.resolveSnapshot(d.Name, byName)
		explicit := false
		for _, configured := range a.config.Datasets {
			explicit = explicit || configured.Name == d.Name
		}
		rows = append(rows, map[string]any{"config": d, "managed": a.config.IsManagedDataset(d.Name), "is_leaf": leaf, "snapshot_count": counts[d.Name], "latest_snapshot": latest[d.Name], "snapshot": snapshot, "snapshot_source": source, "inherited": source != "" && source != d.Name, "explicit": explicit, "managed_ancestor": a.config.ManagedAncestor(d.Name), "next": event, "backups": rowBackups})
	}
	writeJSON(w, 200, map[string]any{"config": a.config, "dataset_rows": rows, "mode": a.opts.Mode, "writes_enabled": a.opts.Mode == "demo" || a.opts.AllowWrites, "hostname": a.hostname, "timezone": a.config.Timezone, "jitter_seconds": a.config.JitterSeconds, "now": now, "pools": a.pools, "datasets": a.datasets, "policies": policies, "backups": backups, "logs": logs, "stats": map[string]any{"active": active, "snapshots": len(a.state.Snapshots), "recent": recent, "permanent": permanent, "snapshot_bytes": bytes, "heap_bytes": mem.Alloc, "uptime_seconds": int(time.Since(a.started).Seconds())}, "config_path": filepath.Base(a.opts.ConfigPath), "replicas": replicas})
}

func (a *App) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	q := strings.ToLower(r.URL.Query().Get("q"))
	dataset := r.URL.Query().Get("dataset")
	tier := r.URL.Query().Get("tier")
	items := []Snapshot{}
	for _, s := range a.state.Snapshots {
		if q != "" && !strings.Contains(strings.ToLower(s.Name), q) {
			continue
		}
		if dataset != "" && s.Dataset != dataset {
			continue
		}
		if tier != "" && s.Tier != tier {
			continue
		}
		items = append(items, withSnapshotRetention(s))
	}
	sortSnapshots(items)
	total := len(items)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	start := (page - 1) * 20
	if start > len(items) {
		start = len(items)
	}
	end := start + 20
	if end > len(items) {
		end = len(items)
	}
	writeJSON(w, 200, map[string]any{"items": items[start:end], "total": total, "page": page})
}

func (a *App) handlePreview(w http.ResponseWriter, r *http.Request) {
	var p Policy
	if err := readJSON(w, r, &p); err != nil {
		fail(w, 400, err)
		return
	}
	if err := ValidatePolicy(p); err != nil {
		fail(w, 422, err)
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	loc, _ := time.LoadLocation(a.config.Timezone)
	if p.ID == "preview" && p.Dataset != "" {
		p.ID = localPolicyID(p.Dataset)
	}
	if len(p.Datasets) > 0 {
		p.Dataset = p.Datasets[0]
	}
	writeJSON(w, 200, map[string]any{"events": NextEvents(p, loc, a.config.JitterSeconds, time.Now(), 16), "valid": true})
}

// Preview resolves an unsaved dataset override with the same rules as the scheduler.
func (a *App) handleDatasetPreview(w http.ResponseWriter, r *http.Request) {
	var d DatasetConfig
	if err := readJSON(w, r, &d); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	c := NormalizeConfig(a.config)
	found := false
	for i, entry := range c.Datasets {
		if entry.Name == d.Name {
			c.Datasets[i] = d
			found = true
			break
		}
	}
	if !found {
		c.Datasets = append(c.Datasets, d)
	}
	if err := ValidateConfig(c); err != nil {
		fail(w, 422, err)
		return
	}
	p, source, ok := c.ResolveSnapshot(d.Name)
	var snapshot *Policy
	events := []Event{}
	if ok {
		snapshot = &p
		loc, _ := time.LoadLocation(c.Timezone)
		if p.Enabled {
			events = NextEvents(p, loc, c.JitterSeconds, time.Now(), 16)
		}
	}
	writeJSON(w, 200, map[string]any{"snapshot": snapshot, "snapshot_source": source, "inherited": source != "" && source != d.Name, "events": events, "valid": true})
}

func (a *App) applyConfigLocked(c Config) error {
	if a.closing {
		return errors.New("服务正在停止")
	}
	for _, state := range a.state.Backups {
		if state.Status == "running" {
			return errors.New("请等待备份任务完成再修改配置")
		}
	}
	c = NormalizeConfig(c)
	if err := ValidateConfig(c); err != nil {
		return err
	}
	if err := validateEffectiveBackupTargets(c, a.datasets); err != nil {
		return err
	}
	if err := saveConfig(a.opts.ConfigPath, c); err != nil {
		return err
	}
	a.reconcileBackupsLocked(a.config, c)
	a.config = c
	a.extendBackupProtectionLocked(time.Now())
	if data, err := os.ReadFile(a.opts.ConfigPath); err == nil {
		a.configHash = sha256.Sum256(data)
	}
	a.record("success", "config", "", "配置已校验并保存到 YAML 文件")
	a.persistLocked()
	return nil
}

// Hand-edited files use the same validation as the UI. Bad files never replace
// the in-memory working configuration; log one error per changed file revision.
func (a *App) reloadConfigLocked() error {
	data, err := os.ReadFile(a.opts.ConfigPath)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	if hash == a.configHash {
		return nil
	}
	for _, b := range a.state.Backups {
		if b.Status == "running" {
			return nil
		}
	}
	a.configHash = hash
	c, err := DecodeConfig(data)
	if err != nil {
		return fmt.Errorf("文件修改未生效，继续使用上次有效配置：%w", err)
	}
	if err := validateEffectiveBackupTargets(c, a.datasets); err != nil {
		return err
	}
	a.reconcileBackupsLocked(a.config, c)
	a.config = c
	a.extendBackupProtectionLocked(time.Now())
	a.record("success", "config", "", "检测到 YAML 文件修改，配置已重新加载")
	a.persistLocked()
	return nil
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, err)
		return
	}
	c, err := DecodeConfig(data)
	if err != nil {
		fail(w, 422, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.state.Backups {
		if s.Status == "running" {
			fail(w, 409, errors.New("请等待备份完成后再替换整个配置"))
			return
		}
	}
	if err = a.applyConfigLocked(c); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleSavePolicy(w http.ResponseWriter, r *http.Request) {
	var p Policy
	if err := readJSON(w, r, &p); err != nil {
		fail(w, 400, err)
		return
	}
	if p.Dataset != "" {
		p.Datasets = append(p.Datasets, p.Dataset)
		p.Dataset = ""
	}
	if err := ValidatePolicy(p); err != nil {
		fail(w, 422, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.config
	c.Policies = append([]Policy{}, c.Policies...)
	wanted := map[string]bool{}
	for _, dataset := range p.Datasets {
		wanted[dataset] = true
	}
	found := false
	for i := range c.Policies {
		if c.Policies[i].ID == p.ID {
			c.Policies[i] = p
			found = true
			continue
		}
		remaining := []string{}
		for _, dataset := range c.Policies[i].Datasets {
			if !wanted[dataset] {
				remaining = append(remaining, dataset)
			}
		}
		c.Policies[i].Datasets = remaining
	}
	if !found {
		c.Policies = append(c.Policies, p)
	}
	if err := a.applyConfigLocked(c); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	c := a.config
	c.Policies = []Policy{}
	found := false
	for _, p := range a.config.Policies {
		if p.ID == id {
			found = true
		} else {
			c.Policies = append(c.Policies, p)
		}
	}
	if !found {
		fail(w, 404, errors.New("策略不存在"))
		return
	}
	if err := a.applyConfigLocked(c); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleRunPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ran := 0
	loc, _ := time.LoadLocation(a.config.Timezone)
	selected := r.URL.Query().Get("dataset")
	for _, p := range a.config.EffectivePolicies(a.datasets...) {
		if p.ID != r.PathValue("id") || (selected != "" && p.Dataset != selected) {
			continue
		}
		if err := a.snapshotLocked(r.Context(), p, ManualEvent(p, loc, time.Now())); err != nil {
			fail(w, 400, err)
			return
		}
		ran++
	}
	if ran == 0 {
		fail(w, 400, errors.New("请选择这个策略中的数据集"))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "datasets": ran})
}

func (a *App) handleSaveBackup(w http.ResponseWriter, r *http.Request) {
	var b Backup
	if err := readJSON(w, r, &b); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// This endpoint saves concrete jobs, not public time definitions.
	if b.Source != "" {
		b.Datasets = append(b.Datasets, BackupDataset{Source: b.Source, Destination: b.Destination})
		b.Source, b.Destination = "", ""
	}
	if a.state.Backups[b.ID].Status == "running" {
		fail(w, 409, errors.New("请等待备份运行结束"))
		return
	}
	c := a.config
	c.Backups = append([]Backup{}, c.Backups...)
	found := false
	for i := range c.Backups {
		if c.Backups[i].ID == b.ID {
			c.Backups[i] = b
			found = true
			break
		}
	}
	if !found {
		c.Backups = append(c.Backups, b)
	}
	if err := a.applyConfigLocked(c); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	if a.state.Backups[id].Status == "running" {
		fail(w, 409, errors.New("请等待备份运行结束"))
		return
	}
	c := a.config
	c.Backups = []Backup{}
	found := false
	for _, b := range a.config.Backups {
		if b.ID == id {
			found = true
		} else {
			c.Backups = append(c.Backups, b)
		}
	}
	if !found {
		fail(w, 404, errors.New("任务不存在"))
		return
	}
	if err := a.applyConfigLocked(c); err != nil {
		fail(w, 500, err)
		return
	}
	delete(a.state.Backups, id)
	for i := range a.state.Snapshots {
		held := []string{}
		for _, h := range a.state.Snapshots[i].HeldBy {
			if h != id {
				held = append(held, h)
			}
		}
		a.state.Snapshots[i].HeldBy = held
	}
	a.persistLocked()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleRunBackup(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, b := range a.config.Backups {
		if b.ID == r.PathValue("id") {
			b = a.config.EffectiveBackup(b, a.datasets...)
			if selected := r.URL.Query().Get("dataset"); selected != "" {
				bindings := []BackupDataset{}
				for _, binding := range b.Datasets {
					if binding.Source == selected {
						bindings = append(bindings, binding)
					}
				}
				b.Datasets = bindings
			}
			if err := a.startBackupLocked(b); err != nil {
				fail(w, 400, err)
				return
			}
			writeJSON(w, 202, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, errors.New("备份任务不存在"))
}

// Stable ordering for export and tests.
func sortedPolicyIDs(policies []Policy) []string {
	ids := []string{}
	for _, p := range policies {
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)
	return ids
}

func (a *App) Close() {
	a.mu.Lock()
	a.closing = true
	if a.cancelWorkers != nil {
		a.cancelWorkers()
	}
	a.mu.Unlock()
	a.workers.Wait()
}
