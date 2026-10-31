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
	host := b.Host
	if host == "local" || host == "localhost" || host == "127.0.0.1" || host == a.hostname {
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
	sshArgs = append(sshArgs, "-p", strconv.Itoa(e.port), e.host)
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
		created, _ := strconv.ParseInt(f[2], 10, 64)
		used, _ := strconv.ParseUint(f[3], 10, 64)
		expiry, expiryErr := strconv.ParseInt(f[4], 10, 64)
		suffix := strings.TrimPrefix(f[0], dataset+"@")
		s := Snapshot{Name: f[0], Dataset: dataset, GUID: f[1], CreatedAt: time.Unix(created, 0), Used: used, Managed: strings.HasPrefix(suffix, SnapshotPrefix) && expiryErr == nil && expiry > 0, HeldBy: []string{}}
		if expiry > 0 {
			s.ExpiresAt = time.Unix(expiry, 0)
		}
		if f[5] != "0" && f[5] != "-" {
			s.HeldBy = []string{"ZFS hold"}
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
	old, err := e.run(ctx, "get", "-Hp", "-o", "value", ExpiryProperty, name)
	if err != nil {
		return err
	}
	unix, _ := strconv.ParseInt(strings.TrimSpace(old), 10, 64)
	if unix >= until.Unix() {
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
