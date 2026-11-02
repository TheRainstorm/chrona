package chrona

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type nativeEndpoint struct {
	host       string
	port       int
	controlDir string
}

func (a *App) endpoint(b Backup) nativeEndpoint {
	b = normalizeBackupTarget(b)
	host := b.Host
	if literalLocalHost(host) || host == a.hostname {
		host = ""
	}
	return nativeEndpoint{host: host, port: b.Port, controlDir: a.opts.SSHControlDir}
}
func shellArg(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (e nativeEndpoint) command(ctx context.Context, args ...string) *exec.Cmd {
	if e.host == "" {
		c := exec.CommandContext(ctx, "zfs", args...)
		c.Env = append(os.Environ(), "LC_ALL=C")
		return c
	}
	sshArgs := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}
	if e.controlDir != "" {
		sshArgs = append(sshArgs, "-o", "ControlMaster=auto", "-o", "ControlPersist=600", "-o", "ControlPath="+filepath.Join(e.controlDir, "%C"))
	}
	if e.port != 0 {
		sshArgs = append(sshArgs, "-p", strconv.Itoa(e.port))
	}
	sshArgs = append(sshArgs, e.host)
	remote := []string{"env", "LC_ALL=C", "zfs"}
	remote = append(remote, args...)
	quoted := make([]string, len(remote))
	for i, s := range remote {
		quoted[i] = shellArg(s)
	}
	sshArgs = append(sshArgs, strings.Join(quoted, " "))
	return exec.CommandContext(ctx, "ssh", sshArgs...)
}
func (e nativeEndpoint) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := e.command(ctx, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("zfs %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
func missingDataset(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "dataset does not exist"))
}
func (e nativeEndpoint) datasetType(ctx context.Context, name string) (string, bool, error) {
	out, err := e.run(ctx, "list", "-Hp", "-o", "name,type", name)
	if missingDataset(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	f := strings.Fields(out)
	if len(f) != 2 || f[0] != name {
		return "", false, fmt.Errorf("无法识别数据集 %s", name)
	}
	return f[1], true, nil
}
func (e nativeEndpoint) snapshots(ctx context.Context, dataset string) ([]Snapshot, error) {
	out, err := e.run(ctx, "list", "-Hp", "-t", "snapshot", "-d", "1", "-s", "createtxg", "-o", "name,guid,creation,used,"+ExpiryProperty+",userrefs", dataset)
	if err != nil {
		return nil, err
	}
	snapshots := []Snapshot{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || !strings.HasPrefix(f[0], dataset+"@") {
			continue
		}
		s, err := snapshotMetadata(f[0], f[1], f[2], f[3], f[4], f[5])
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, nil
}
func (e nativeEndpoint) hasHold(ctx context.Context, name, tag string) (bool, error) {
	out, err := e.run(ctx, "holds", "-H", name)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == tag {
			return true, nil
		}
	}
	return false, nil
}
func (e nativeEndpoint) hold(ctx context.Context, name, tag string) error {
	held, err := e.hasHold(ctx, name, tag)
	if err != nil || held {
		return err
	}
	_, err = e.run(ctx, "hold", tag, name)
	if err != nil {
		if held, _ := e.hasHold(ctx, name, tag); held {
			return nil
		}
	}
	return err
}
func (e nativeEndpoint) expiry(ctx context.Context, name string, until time.Time) error {
	if keepSnapshot(name) {
		return nil
	}
	out, err := e.run(ctx, "list", "-Hp", "-t", "snapshot", "-o", "name,guid,creation,used,"+ExpiryProperty+",userrefs", name)
	if err != nil {
		return err
	}
	fields := strings.Fields(out)
	if len(fields) != 6 || fields[0] != name {
		return fmt.Errorf("无法读取快照期限：%s", name)
	}
	old, err := snapshotMetadata(fields[0], fields[1], fields[2], fields[3], fields[4], fields[5])
	if err != nil {
		return err
	}
	// Promoting a legacy base to an explicit protection floor must not shorten
	// its default thirty-day lifetime.
	if !old.ExpiresAt.IsZero() && !old.ExpiresAt.Before(until) {
		return nil
	}
	_, err = e.run(ctx, "set", ExpiryProperty+"="+strconv.FormatInt(until.Unix(), 10), name)
	return err
}
func (e nativeEndpoint) ensureParents(ctx context.Context, dataset string) error {
	parent := parentDataset(dataset)
	if parent == "" {
		return fmt.Errorf("目的地必须是池下的数据集")
	}
	_, exists, err := e.datasetType(ctx, parent)
	if err != nil || exists {
		return err
	}
	if !strings.Contains(parent, "/") {
		return fmt.Errorf("目标存储池不存在：%s", parent)
	}
	if err = e.ensureParents(ctx, parent); err != nil {
		return err
	}
	_, err = e.run(ctx, "create", "-o", "mountpoint=none", "-o", "canmount=off", parent)
	return err
}

type streamCounter struct {
	dst    io.Writer
	count  atomic.Uint64
	report func(uint64)
	last   atomic.Int64
}

func (w *streamCounter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	total := w.count.Add(uint64(n))
	now := time.Now().UnixNano()
	previous := w.last.Load()
	if now-previous > int64(time.Second) && w.last.CompareAndSwap(previous, now) {
		w.report(total)
	}
	return n, err
}
func nativeStream(ctx context.Context, endpoint nativeEndpoint, sendArgs, receiveArgs []string, report func(uint64)) (uint64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sender := nativeEndpoint{}.command(ctx, sendArgs...)
	receiver := endpoint.command(ctx, receiveArgs...)
	stdin, err := receiver.StdinPipe()
	if err != nil {
		return 0, err
	}
	counter := &streamCounter{dst: stdin, report: report}
	sender.Stdout = counter
	var sendErr, recvErr bytes.Buffer
	sender.Stderr = &sendErr
	receiver.Stderr = &recvErr
	if err = receiver.Start(); err != nil {
		stdin.Close()
		return 0, err
	}
	if err = sender.Start(); err != nil {
		stdin.Close()
		cancel()
		_ = receiver.Wait()
		return 0, err
	}
	done := make(chan error, 2)
	go func() {
		err := sender.Wait()
		_ = stdin.Close()
		if err != nil {
			cancel()
		}
		done <- err
	}()
	go func() {
		err := receiver.Wait()
		if err != nil {
			cancel()
		}
		done <- err
	}()
	first, second := <-done, <-done
	if first != nil || second != nil {
		return counter.count.Load(), fmt.Errorf("send/recv 失败: %v; %v; %s %s", first, second, strings.TrimSpace(sendErr.String()), strings.TrimSpace(recvErr.String()))
	}
	return counter.count.Load(), nil
}
