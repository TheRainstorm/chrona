package chrona

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func nativeTag(id string) string { return "chrona:" + id }
func selectNativeBase(local, remote []Snapshot) (*Snapshot, error) {
	if len(remote) == 0 {
		return nil, nil
	}
	latest := remote[len(remote)-1]
	for _, s := range local {
		if s.GUID != "" && s.GUID == latest.GUID {
			copy := s
			return &copy, nil
		}
	}
	return nil, errors.New("目标最新快照没有对应的源 GUID；未回滚或覆盖目标，请检查接收端独立快照或改用新目的地")
}
func (a *App) startNativeBackupLocked(b Backup) error {
	if a.closing {
		return errors.New("服务正在停止")
	}
	if !a.opts.AllowWrites {
		return errors.New("当前为 ZFS 只读模式")
	}
	if a.state.Backups[b.ID].Status == "running" {
		return errors.New("备份任务正在运行")
	}
	if err := validateEffectiveBackupTargets(a.config, a.datasets); err != nil {
		return err
	}
	state := a.state.Backups[b.ID]
	state.Status = "running"
	state.Progress = 0
	state.Bytes = 0
	state.Error = ""
	state.Warning = ""
	a.state.Backups[b.ID] = state
	config := NormalizeConfig(a.config)
	a.record("info", "backup", backupSources(b), "开始真实 ZFS 复制 → "+b.Host)
	a.persistLocked()
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		ctx, cancel := context.WithTimeout(a.workerCtx, 24*time.Hour)
		defer cancel()
		err := a.runNativeBackup(ctx, config, b)
		a.mu.Lock()
		defer a.mu.Unlock()
		if err == nil {
			state := a.state.Backups[b.ID]
			state.Status = "success"
			state.Progress = 100
			a.state.Backups[b.ID] = state
		}
		if err != nil {
			state := a.state.Backups[b.ID]
			state.Status = "error"
			state.Error = err.Error()
			a.state.Backups[b.ID] = state
			a.record("error", "backup", backupSources(b), err.Error())
		}
		if refreshErr := a.refreshRealLocked(context.Background()); refreshErr != nil {
			a.record("error", "inventory", "", refreshErr.Error())
		}
		a.persistLocked()
	}()
	return nil
}

type nativeResult struct {
	binding  BackupDataset
	source   Snapshot
	replica  ReplicaSnapshot
	transfer TransferResult
	bytes    uint64
}

func (a *App) runNativeBackup(ctx context.Context, c Config, b Backup) error {
	endpoint := a.endpoint(b)
	results := []nativeResult{}
	var total uint64
	for i, binding := range b.Datasets {
		result, err := a.nativeCopyLeaf(ctx, c, b, endpoint, binding, func(n uint64) {
			a.mu.Lock()
			s := a.state.Backups[b.ID]
			s.Bytes = total + n
			s.Progress = (i * 100) / len(b.Datasets)
			a.state.Backups[b.ID] = s
			a.mu.Unlock()
		})
		if err != nil {
			return fmt.Errorf("%s → %s: %w", binding.Source, binding.Destination, err)
		}
		results = append(results, result)
		total += result.bytes
	}
	now := time.Now()
	// Every receiver has been verified and protected. Advance all recorded bases together.
	a.mu.Lock()
	state := a.state.Backups[b.ID]
	if state.Bases == nil {
		state.Bases = map[string]string{}
	}
	if state.Completed == nil {
		state.Completed = map[string]time.Time{}
	}
	if state.Transfers == nil {
		state.Transfers = map[string]TransferResult{}
	}
	for _, r := range results {
		state.Bases[r.binding.Source] = r.source.Name
		state.Completed[r.binding.Source] = now
		state.Transfers[r.binding.Source] = r.transfer
		replaced := false
		for i, old := range a.state.Replicas {
			if old.Host == b.Host && old.Port == b.Port && old.Name == r.replica.Name {
				a.state.Replicas[i] = r.replica
				replaced = true
				break
			}
		}
		if !replaced {
			a.state.Replicas = append(a.state.Replicas, r.replica)
		}
	}
	state.Status = "running"
	state.Progress = 99
	state.LastSuccess = now
	state.LastSnapshot = results[0].source.Name
	state.Bytes = total
	state.Error = ""
	a.state.Backups[b.ID] = state
	a.record("success", "backup", backupSources(b), fmt.Sprintf("ZFS 复制完成 · %d 个叶子数据集 · %d 字节", len(results), total))
	a.persistLocked()
	a.mu.Unlock()
	// Release only this job's superseded holds after durable success, then prune owned expired snapshots.
	for _, r := range results {
		if err := cleanupNativeSnapshots(ctx, nativeEndpoint{}, r.binding.Source, r.source.Name, nativeTag(b.ID)); err != nil {
			a.nativeWarning(b.ID, err)
		}
		if err := cleanupNativeSnapshots(ctx, endpoint, r.binding.Destination, r.replica.Name, nativeTag(b.ID)); err != nil {
			a.nativeWarning(b.ID, err)
		}
	}
	return nil
}
func (a *App) nativeWarning(id string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.state.Backups[id]; ok {
		s.Warning = err.Error()
		a.state.Backups[id] = s
	}
	a.record("warning", "backup", id, err.Error())
	a.persistLocked()
}

func (a *App) nativeCopyLeaf(ctx context.Context, c Config, b Backup, receiver nativeEndpoint, binding BackupDataset, progress func(uint64)) (nativeResult, error) {
	result := nativeResult{binding: binding}
	source := nativeEndpoint{}
	if receiver.host == "" && (binding.Source == binding.Destination || strings.HasPrefix(binding.Destination, binding.Source+"/") || strings.HasPrefix(binding.Source, binding.Destination+"/")) {
		return result, errors.New("本机源和目标不能相同或互为祖先")
	}
	sourceType, exists, err := source.datasetType(ctx, binding.Source)
	if err != nil {
		return result, err
	}
	if !exists {
		return result, errors.New("源数据集不存在")
	}
	names, err := source.run(ctx, "list", "-H", "-r", "-d", "1", "-o", "name", binding.Source)
	if err != nil {
		return result, err
	}
	for _, name := range strings.Fields(names) {
		if name != binding.Source {
			return result, errors.New("源已成为非叶子数据集，停止传输")
		}
	}
	targetType, targetExists, err := receiver.datasetType(ctx, binding.Destination)
	if err != nil {
		return result, err
	}
	if targetExists && targetType != sourceType {
		return result, errors.New("源和目标数据集类型不一致")
	}
	// Resume a saved interrupted stream without destroying the partial receiver.
	if targetExists {
		token, err := receiver.run(ctx, "get", "-Hp", "-o", "value", "receive_resume_token", binding.Destination)
		if err != nil {
			return result, err
		}
		if token = strings.TrimSpace(token); token != "-" && token != "" {
			transferred, err := nativeStream(ctx, receiver, []string{"send", "-t", token}, []string{"receive", "-u", "-s", binding.Destination}, progress)
			result.bytes += transferred
			if err != nil {
				return result, fmt.Errorf("恢复中断传输: %w", err)
			}
		}
	}
	local, err := source.snapshots(ctx, binding.Source)
	if err != nil {
		return result, err
	}
	remote := []Snapshot{}
	if targetExists {
		remote, err = receiver.snapshots(ctx, binding.Destination)
		if err != nil {
			return result, err
		}
	}
	base, err := selectNativeBase(local, remote)
	if err != nil {
		return result, err
	}
	if targetExists && base == nil {
		return result, errors.New("目标已存在但没有共同快照；为避免覆盖已有数据，请选择新目的地")
	}
	loc, _ := time.LoadLocation(c.Timezone)
	now := time.Now()
	_, until := backupProtection(b, loc, now)
	tag := nativeTag(b.ID)
	if base != nil {
		if err = source.hold(ctx, base.Name, tag); err != nil {
			return result, err
		}
		if err = receiver.hold(ctx, remote[len(remote)-1].Name, tag); err != nil {
			return result, err
		}
		if err = source.expiry(ctx, base.Name, until); err != nil {
			return result, err
		}
		if err = receiver.expiry(ctx, remote[len(remote)-1].Name, until); err != nil {
			return result, err
		}
	}
	policy, _, ok := c.ResolveSnapshot(binding.Source)
	if !ok {
		policy = Policy{Dataset: binding.Source, Keep: Keep{Weekly: b.KeepWeekly, Monthly: b.KeepMonthly}}
	}
	event := ManualEvent(policy, loc, now)
	if event.ExpiresAt.Before(until) {
		event.ExpiresAt = until
	}
	hash := sha256.Sum256([]byte(b.ID))
	suffix := fmt.Sprintf("%s%s_b%x_%x", SnapshotPrefix, now.UTC().Format("20060102T150405Z"), hash[:3], now.Nanosecond())
	name := binding.Source + "@" + suffix
	if _, err = source.run(ctx, "snapshot", "-o", ExpiryProperty+"="+strconv.FormatInt(event.ExpiresAt.Unix(), 10), name); err != nil {
		return result, err
	}
	if err = source.hold(ctx, name, tag); err != nil {
		return result, err
	}
	if !targetExists {
		if err = receiver.ensureParents(ctx, binding.Destination); err != nil {
			return result, err
		}
	}
	receiveArgs := []string{"receive", "-u", "-s"}
	if !targetExists {
		receiveArgs = append(receiveArgs, "-o", "readonly=on")
		if sourceType == "filesystem" {
			receiveArgs = append(receiveArgs, "-o", "mountpoint=none", "-o", "canmount=off", "-x", "sharenfs", "-x", "sharesmb")
		} else {
			receiveArgs = append(receiveArgs, "-o", "volmode=none")
		}
	}
	receiveArgs = append(receiveArgs, binding.Destination)
	sendArgs := []string{"send", "-w", "-p"}
	if base != nil {
		sendArgs = append(sendArgs, "-i", base.Name)
	}
	sendArgs = append(sendArgs, name)
	transferred, err := nativeStream(ctx, receiver, sendArgs, receiveArgs, progress)
	result.bytes += transferred
	if err != nil {
		return result, err
	}
	copied, err := receiver.snapshots(ctx, binding.Destination)
	if err != nil {
		return result, err
	}
	local, err = source.snapshots(ctx, binding.Source)
	if err != nil {
		return result, err
	}
	var sent, received *Snapshot
	for i := range local {
		if local[i].Name == name {
			sent = &local[i]
			break
		}
	}
	for i := range copied {
		if copied[i].Name == binding.Destination+"@"+suffix {
			received = &copied[i]
			break
		}
	}
	if sent == nil || received == nil || sent.GUID == "" || sent.GUID != received.GUID {
		return result, errors.New("接收后 GUID 验证失败；保留原基点")
	}
	now = time.Now()
	next, until := backupProtection(b, loc, now)
	if err = source.expiry(ctx, name, until); err != nil {
		return result, err
	}
	expiry := backupReplicaExpiry(b, loc, now)
	if err = receiver.expiry(ctx, received.Name, expiry); err != nil {
		return result, err
	}
	if err = receiver.hold(ctx, received.Name, tag); err != nil {
		return result, err
	}
	if _, err = receiver.run(ctx, "set", "readonly=on", binding.Destination); err != nil {
		return result, err
	}
	actualExpiry, err := receiver.run(ctx, "get", "-Hp", "-o", "value", ExpiryProperty, received.Name)
	if err != nil {
		return result, err
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(actualExpiry), 10, 64)
	if err != nil {
		return result, err
	}
	expiry = time.Unix(unix, 0)
	sent.HeldBy = []string{tag}
	received.HeldBy = []string{tag}
	received.ExpiresAt = expiry
	received.Managed = true
	result.source = *sent
	result.replica = ReplicaSnapshot{Snapshot: *received, TaskID: b.ID, Host: b.Host, Port: b.Port, SourceSnapshot: sent.Name}
	mode, baseName := "full", ""
	if base != nil {
		mode = "incremental"
		baseName = base.Name
	}
	result.transfer = TransferResult{Mode: mode, Base: baseName, RemoteSnapshot: received.Name, NextBackupAt: next, ProtectedUntil: until}
	return result, nil
}

func cleanupNativeSnapshots(ctx context.Context, endpoint nativeEndpoint, dataset, current, tag string) error {
	snapshots, err := endpoint.snapshots(ctx, dataset)
	if err != nil {
		return err
	}
	for _, s := range snapshots {
		if s.Name == current {
			continue
		}
		if len(s.HeldBy) > 0 {
			held, err := endpoint.hasHold(ctx, s.Name, tag)
			if err != nil {
				return err
			}
			if held {
				if _, err = endpoint.run(ctx, "release", tag, s.Name); err != nil {
					return err
				}
			}
		}
	}
	snapshots, err = endpoint.snapshots(ctx, dataset)
	if err != nil {
		return err
	}
	for _, s := range snapshots {
		if s.Name != current && s.Managed && !s.ExpiresAt.After(time.Now()) && len(s.HeldBy) == 0 {
			if _, err = endpoint.run(ctx, "destroy", s.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
