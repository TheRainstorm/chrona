package chrona

import (
	"hash/fnv"
	"sort"
	"time"
)

type Tier struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Seconds int64  `json:"seconds"`
}

var Tiers = []Tier{
	{"quarter_hourly", "15 分钟", 900},
	{"hourly", "小时", 3600},
	{"daily", "天", 86400},
	{"weekly", "周", 604800},
	{"monthly", "月", 0},
}

type Event struct {
	PlannedAt     time.Time `json:"planned_at"`
	RunAt         time.Time `json:"run_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Tier          string    `json:"tier"`
	Label         string    `json:"label"`
	Merged        []string  `json:"merged"`
	OffsetSeconds int       `json:"offset_seconds"`
	rank          int
}

func addMonthsClamped(anchor time.Time, months int) time.Time {
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, first.Location()).Day()
	day := anchor.Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, first.Hour(), first.Minute(), first.Second(), first.Nanosecond(), first.Location())
}

func stableOffset(id, dataset string, max int) int {
	if max <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id + "|" + dataset))
	return int(h.Sum32() % uint32(max+1))
}

func slotAt(anchor time.Time, rank, index int) time.Time {
	if rank == 4 {
		return addMonthsClamped(anchor, index)
	}
	return time.Unix(anchor.Unix()+int64(index)*Tiers[rank].Seconds, 0).In(anchor.Location())
}

func lastIndex(anchor, at time.Time, rank int) int {
	if at.Before(anchor) {
		return -1
	}
	if rank != 4 {
		return int((at.Unix() - anchor.Unix()) / Tiers[rank].Seconds)
	}
	n := (at.Year()-anchor.Year())*12 + int(at.Month()-anchor.Month())
	if slotAt(anchor, rank, n).After(at) {
		n--
	}
	return n
}

func policyAnchor(p Policy, location *time.Location) time.Time {
	t, _ := time.Parse(time.RFC3339, p.StartAt)
	return t.In(location)
}

// An extra manual snapshot uses the shortest enabled interval's lifetime.
func ManualEvent(p Policy, location *time.Location, now time.Time) Event {
	for rank, count := range p.Keep.Values() {
		if count == 0 {
			continue
		}
		expires := now.Add(time.Duration(int64(count)*Tiers[rank].Seconds) * time.Second)
		if rank == 4 {
			expires = addMonthsClamped(now.In(location), count)
		}
		return Event{PlannedAt: now.Truncate(time.Second), RunAt: now, ExpiresAt: expires, Tier: Tiers[rank].Key, Label: Tiers[rank].Label, Merged: []string{Tiers[rank].Key}}
	}
	return Event{}
}

// NextEvents combines logical slots BEFORE applying the per-policy stable jitter.
// All schedules derive from the original anchor, never the previous actual run.
func NextEvents(p Policy, location *time.Location, jitter int, after time.Time, count int) []Event {
	if count < 1 {
		return []Event{}
	}
	anchor := policyAnchor(p, location)
	offset := stableOffset(p.ID, p.Dataset, jitter)
	logicalAfter := after.Add(-time.Duration(offset) * time.Second)
	counts := p.Keep.Values()
	indices := make([]int, len(Tiers))
	active := false
	for i, keep := range counts {
		if keep > 0 {
			active = true
			indices[i] = lastIndex(anchor, logicalAfter.In(location), i) + 1
		}
	}
	if !active {
		return []Event{}
	}
	events := make([]Event, 0, count)
	for len(events) < count {
		var first time.Time
		for i, keep := range counts {
			if keep == 0 {
				continue
			}
			slot := slotAt(anchor, i, indices[i])
			if first.IsZero() || slot.Before(first) {
				first = slot
			}
		}
		event := Event{PlannedAt: first, RunAt: first.Add(time.Duration(offset) * time.Second), OffsetSeconds: offset, Merged: []string{}}
		for i, keep := range counts {
			if keep == 0 || !slotAt(anchor, i, indices[i]).Equal(first) {
				continue
			}
			event.rank, event.Tier, event.Label = i, Tiers[i].Key, Tiers[i].Label
			event.ExpiresAt = slotAt(anchor, i, indices[i]+keep).Add(time.Duration(offset) * time.Second)
			event.Merged = append(event.Merged, Tiers[i].Key)
			indices[i]++
		}
		events = append(events, event)
	}
	return events
}

func NextBackup(b Backup, location *time.Location, after time.Time) time.Time {
	tod, _ := time.Parse("15:04", b.At)
	after = after.In(location)
	for day := 0; day < 370; day++ {
		d := after.AddDate(0, 0, day)
		candidate := time.Date(d.Year(), d.Month(), d.Day(), tod.Hour(), tod.Minute(), 0, 0, location)
		if !candidate.After(after) {
			continue
		}
		if b.Cadence == "monthly" {
			last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, location).Day()
			wanted := b.MonthDay
			if wanted > last {
				wanted = last
			}
			if d.Day() == wanted {
				return candidate
			}
		} else {
			for _, weekday := range b.Weekdays {
				if int(d.Weekday()) == weekday {
					return candidate
				}
			}
		}
	}
	return time.Time{}
}

func sortSnapshots(items []Snapshot) {
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
}
