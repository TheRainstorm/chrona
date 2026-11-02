package chrona

import "time"

// A config edit cannot inherit a successful backup status for a different target.
// Removing one source never removes the other sources' protected bases.
func (a *App) reconcileBackupsLocked(old, new Config) {
	oldByID, newByID := map[string]Backup{}, map[string]Backup{}
	oldStates := map[string]BackupState{}
	for id, state := range a.state.Backups {
		oldStates[id] = state
	}
	for _, b := range old.Backups {
		oldByID[b.ID] = old.EffectiveBackup(b, a.datasets...)
	}
	for _, b := range new.Backups {
		newByID[b.ID] = new.EffectiveBackup(b, a.datasets...)
	}
	for id, state := range a.state.Backups {
		b, exists := newByID[id]
		if !exists {
			delete(a.state.Backups, id)
			continue
		}
		previous := oldByID[id]
		targets, previousTargets := map[string]string{}, map[string]string{}
		for _, d := range b.Datasets {
			targets[d.Source] = d.Destination
		}
		for _, d := range previous.Datasets {
			previousTargets[d.Source] = d.Destination
		}
		changed := previous.Host != b.Host || previous.Port != b.Port || len(targets) != len(previousTargets)
		bases, completed, transfers := map[string]string{}, map[string]time.Time{}, map[string]TransferResult{}
		for source, destination := range targets {
			if previousTargets[source] != destination || previous.Host != b.Host || previous.Port != b.Port {
				changed = true
				continue
			}
			if base := state.Bases[source]; base != "" {
				bases[source] = base
			}
			if at, ok := state.Completed[source]; ok {
				completed[source] = at
			}
			if result, ok := state.Transfers[source]; ok {
				transfers[source] = result
			}
		}
		state.Bases, state.Completed, state.Transfers = bases, completed, transfers
		if changed {
			state.Status = "pending"
			state.Progress = 0
			state.LastSuccess = time.Time{}
			state.LastSnapshot = ""
			state.Bytes = 0
			state.Error = ""
		}
		a.state.Backups[id] = state
	}
	// When the UI separates one binding from an old grouped job, the endpoint
	// is unchanged. Move that binding's history; never copy history to a new target.
	for id, b := range newByID {
		if _, existed := oldByID[id]; existed {
			continue
		}
		state := BackupState{Status: "pending", Bases: map[string]string{}, Completed: map[string]time.Time{}, Transfers: map[string]TransferResult{}}
		for _, binding := range b.Datasets {
			previousID := ""
			matches := 0
			for oldID, previous := range oldByID {
				for _, oldBinding := range previous.Datasets {
					if sameBackupTarget(previous, oldBinding, b, binding) {
						previousID = oldID
						matches++
					}
				}
			}
			if matches != 1 {
				continue
			}
			previous := oldStates[previousID]
			base := previous.Bases[binding.Source]
			if base == "" {
				continue
			}
			state.Bases[binding.Source] = base
			if at, ok := previous.Completed[binding.Source]; ok {
				state.Completed[binding.Source] = at
				if at.After(state.LastSuccess) {
					state.LastSuccess, state.LastSnapshot = at, base
				}
			}
			if transfer, ok := previous.Transfers[binding.Source]; ok {
				state.Transfers[binding.Source] = transfer
			}
			for i := range a.state.Replicas {
				r := &a.state.Replicas[i]
				if r.TaskID == previousID && r.Host == b.Host && r.Port == b.Port && r.Dataset == binding.Destination && r.SourceSnapshot == base {
					r.TaskID = id
				}
			}
		}
		if len(state.Bases) > 0 {
			if len(state.Completed) == len(b.Datasets) {
				state.Status, state.Progress = "success", 100
			}
			a.state.Backups[id] = state
		}
	}
	if a.opts.Mode != "demo" {
		a.scheduleNativeMaintenanceLocked(old, new)
		return
	}
	for i := range a.state.Snapshots {
		s := &a.state.Snapshots[i]
		held := []string{}
		for _, id := range s.HeldBy {
			if _, owned := oldByID[id]; !owned {
				held = append(held, id)
				continue
			}
			if a.state.Backups[id].Bases[s.Dataset] == s.Name {
				held = append(held, id)
			}
		}
		s.HeldBy = held
	}
	for i := range a.state.Replicas {
		r := &a.state.Replicas[i]
		held := []string{}
		for _, id := range r.HeldBy {
			if _, owned := oldByID[id]; !owned {
				held = append(held, id)
				continue
			}
			b, exists := newByID[id]
			if !exists || b.Host != r.Host || b.Port != r.Port {
				continue
			}
			for _, binding := range b.Datasets {
				if binding.Destination == r.Dataset && a.state.Backups[id].Bases[binding.Source] == r.SourceSnapshot {
					held = append(held, id)
					break
				}
			}
		}
		r.HeldBy = held
	}
	a.syncBackupHoldsForConfigLocked(new)
}

func sameBackupTarget(a Backup, x BackupDataset, b Backup, y BackupDataset) bool {
	return a.Host == b.Host && a.Port == b.Port && x.Source == y.Source && x.Destination == y.Destination
}
