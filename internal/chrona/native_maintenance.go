package chrona

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Config edits retire only this service's holds and extend surviving common bases.
// Metadata I/O runs outside the HTTP/config mutex, including when a peer is offline.
func (a *App) scheduleNativeMaintenanceLocked(old, new Config) {
	if !a.opts.AllowWrites || a.closing {
		return
	}
	old = NormalizeConfig(old)
	new = NormalizeConfig(new)
	inventory := append([]Dataset{}, a.datasets...)
	bases := map[string]map[string]string{}
	for id, state := range a.state.Backups {
		bases[id] = map[string]string{}
		for source, base := range state.Bases {
			bases[id][source] = base
		}
	}
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		ctx, cancel := context.WithTimeout(a.workerCtx, 2*time.Minute)
		defer cancel()
		newer := map[string]Backup{}
		for _, b := range new.Backups {
			newer[b.ID] = new.EffectiveBackup(b, inventory...)
		}
		// A grouped job may have been split while retaining the exact same target.
		// Establish its new holds before retiring the old job's tags.
		moved := map[string]bool{}
		for _, oldTask := range old.Backups {
			previous := old.EffectiveBackup(oldTask, inventory...)
			for _, binding := range previous.Datasets {
				for id, b := range newer {
					if id == previous.ID {
						continue
					}
					for _, nextBinding := range b.Datasets {
						if !sameBackupTarget(previous, binding, b, nextBinding) {
							continue
						}
						base := bases[id][binding.Source]
						if base == "" {
							continue
						}
						key := previous.ID + ":" + binding.Source
						moved[key] = false
						remoteName := binding.Destination + "@" + strings.SplitN(base, "@", 2)[1]
						err := (nativeEndpoint{}).hold(ctx, base, nativeTag(id))
						if err == nil {
							err = a.endpoint(b).hold(ctx, remoteName, nativeTag(id))
						}
						if err != nil {
							a.nativeWarning(id, fmt.Errorf("接管基点 hold 失败，原 hold 继续保留: %w", err))
							continue
						}
						moved[key] = true
					}
				}
			}
		}
		for _, oldTask := range old.Backups {
			previous := old.EffectiveBackup(oldTask, inventory...)
			next, exists := newer[previous.ID]
			for _, binding := range previous.Datasets {
				if safe, moving := moved[previous.ID+":"+binding.Source]; moving && !safe {
					continue
				}
				keepTarget, keepSource := false, false
				if exists {
					for _, d := range next.Datasets {
						if d.Source == binding.Source {
							keepSource = true
							keepTarget = d.Destination == binding.Destination && next.Host == previous.Host && next.Port == previous.Port
						}
					}
				}
				if !keepTarget {
					if err := releaseNativeTag(ctx, a.endpoint(previous), binding.Destination, nativeTag(previous.ID)); err != nil {
						a.nativeWarning(previous.ID, err)
					}
				}
				if !keepSource {
					if err := releaseNativeTag(ctx, nativeEndpoint{}, binding.Source, nativeTag(previous.ID)); err != nil {
						a.nativeWarning(previous.ID, err)
					}
				}
			}
		}
		loc, _ := time.LoadLocation(new.Timezone)
		for _, b := range newer {
			next, until := backupProtection(b, loc, time.Now())
			for _, binding := range b.Datasets {
				base := bases[b.ID][binding.Source]
				if base == "" {
					continue
				}
				endpoint := a.endpoint(b)
				err := (nativeEndpoint{}).expiry(ctx, base, until)
				remoteName := binding.Destination + "@" + strings.SplitN(base, "@", 2)[1]
				if err == nil {
					err = endpoint.expiry(ctx, remoteName, until)
				}
				if err != nil {
					a.nativeWarning(b.ID, fmt.Errorf("延长基点期限失败，原 hold 继续保留: %w", err))
					continue
				}
				a.mu.Lock()
				state := a.state.Backups[b.ID]
				if state.Bases[binding.Source] == base {
					if t, ok := state.Transfers[binding.Source]; ok {
						t.NextBackupAt = next
						t.ProtectedUntil = until
						state.Transfers[binding.Source] = t
					}
					a.state.Backups[b.ID] = state
					for i := range a.state.Replicas {
						r := &a.state.Replicas[i]
						if r.Host == b.Host && r.Port == b.Port && r.Name == remoteName && r.ExpiresAt.Before(until) {
							r.ExpiresAt = until
						}
					}
					a.persistLocked()
				}
				a.mu.Unlock()
			}
		}
	}()
}

func releaseNativeTag(ctx context.Context, endpoint nativeEndpoint, dataset, tag string) error {
	_, exists, err := endpoint.datasetType(ctx, dataset)
	if err != nil || !exists {
		return err
	}
	snapshots, err := endpoint.snapshots(ctx, dataset)
	if err != nil {
		return err
	}
	for _, s := range snapshots {
		if len(s.HeldBy) == 0 {
			continue
		}
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
	return nil
}
