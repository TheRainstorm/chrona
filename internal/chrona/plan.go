package chrona

import (
	"errors"
	"net/http"
	"time"
)

func (a *App) handleDatasetPlan(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("dataset")
	a.mu.RLock()
	defer a.mu.RUnlock()
	known := a.config.IsManagedDataset(name)
	for _, d := range a.datasets {
		known = known || d.Name == name
	}
	if !datasetPattern.MatchString(name) || !known {
		fail(w, 404, errors.New("数据集尚未纳入管理"))
		return
	}
	now := time.Now()
	p, origin, ok := a.config.ResolveSnapshot(name)
	var snapshot *Policy
	events := []Event{}
	tiers := []map[string]any{}
	if ok {
		snapshot = &p
		loc, _ := time.LoadLocation(a.config.Timezone)
		if p.Enabled {
			events = NextEvents(p, loc, a.config.JitterSeconds, now, 16)
			for rank, count := range p.Keep.Values() {
				if count == 0 {
					continue
				}
				individual := p
				counts := [5]int{}
				counts[rank] = count
				individual.Keep = Keep{counts[0], counts[1], counts[2], counts[3], counts[4]}
				next := NextEvents(individual, loc, a.config.JitterSeconds, now, 1)
				tiers = append(tiers, map[string]any{"tier": Tiers[rank].Key, "label": Tiers[rank].Label, "keep": count, "next": next[0]})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"dataset": name, "snapshot": snapshot, "snapshot_source": origin, "now": now, "events": events, "tiers": tiers})
}
