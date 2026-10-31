package interactionruntime

import (
	"fmt"
	"sort"
	"time"
)

type behaviorEntry struct {
	Person
	At       time.Time
	Sequence uint64
}
type behaviorLane struct {
	Entries                map[string]behaviorEntry
	Seen                   map[string]bool // order claims: one known person per live run
	Task                   string
	Sequence               uint64
	Pending, Retry         bool
	RetryAt, LastStart     time.Time
	Interval, Proposed     time.Duration
	ProposedAt, EvaluateAt time.Time
}

func behaviorState(r *room, kind string) *behaviorLane {
	if r.Behaviors == nil {
		r.Behaviors = map[string]*behaviorLane{}
	}
	if r.Behaviors[kind] == nil {
		r.Behaviors[kind] = &behaviorLane{Entries: map[string]behaviorEntry{}, Seen: map[string]bool{}}
	}
	return r.Behaviors[kind]
}

func recordBehavior(r *room, v Event, kind string, now time.Time) {
	window := 30 * time.Second
	if kind == "order" {
		window = time.Minute
	}
	if v.UID == "" || v.At.After(now.Add(2*time.Second)) || now.Sub(v.At) >= window {
		return
	}
	w := behaviorState(r, kind)
	if kind == "order" && (w.Seen[v.UID] || w.Entries[v.UID].UID != "" || len(w.Seen) >= 65536) {
		return
	}
	if old, ok := w.Entries[v.UID]; ok && v.At.Before(old.At) {
		return
	}
	// Never evict an order already owed to a pending report.
	if len(w.Entries) >= 8192 && w.Entries[v.UID].UID == "" {
		return
	}
	w.Sequence++
	w.Entries[v.UID] = behaviorEntry{Person{v.UID, v.Name}, v.At, w.Sequence}
}

func behaviorInterval(kind string, entries, semantic int) time.Duration {
	col := 0
	if entries > 5 {
		col = 1
	}
	if entries > 30 {
		col = 2
	}
	row := 0
	if semantic >= 5 {
		row = 1
	}
	if semantic > 10 {
		row = 2
	}
	if semantic > 20 {
		row = 3
	}
	if kind == "follow" {
		return [4][3]time.Duration{{20, 45, 90}, {60, 120, 180}, {120, 180, 300}, {180, 300, 600}}[row][col] * time.Second
	}
	return [4][3]time.Duration{{30, 60, 120}, {90, 180, 300}, {180, 300, 600}, {300, 600, 600}}[row][col] * time.Second
}

func updateBehaviorInterval(r *room, w *behaviorLane, kind string, now time.Time) {
	if !w.EvaluateAt.IsZero() && now.Before(w.EvaluateAt) {
		return
	}
	w.EvaluateAt = now.Add(10 * time.Second)
	entries, semantic := 0, 0
	if r.Welcome != nil {
		for _, v := range r.Welcome.Entries {
			if !v.At.After(now) && now.Sub(v.At) < time.Minute {
				entries++
			}
		}
	}
	for _, at := range r.SemanticUsers {
		if !at.After(now) && now.Sub(at) < 3*time.Minute {
			semantic++
		}
	}
	want := behaviorInterval(kind, entries, semantic)
	if w.LastStart.IsZero() {
		w.Interval = want
		return
	}
	if want == w.Interval {
		w.ProposedAt = time.Time{}
		return
	}
	if want != w.Proposed || w.ProposedAt.IsZero() {
		w.Proposed = want
		w.ProposedAt = now
		return
	}
	wait := time.Minute
	if want > w.Interval {
		wait = 15 * time.Second
	}
	if now.Sub(w.ProposedAt) >= wait {
		w.Interval = want
		w.ProposedAt = time.Time{}
	}
}

func (e *Engine) prepareBehaviors(r *room, now time.Time) {
	for _, kind := range []string{"order", "like", "follow"} {
		w := r.Behaviors[kind]
		if w == nil {
			continue
		}
		if kind != "order" {
			updateBehaviorInterval(r, w, kind, now)
		}
		for uid, v := range w.Entries {
			window := 30 * time.Second
			if kind == "order" {
				window = time.Minute
			}
			if (kind != "order" || !w.Pending) && now.Sub(v.At) >= window {
				delete(w.Entries, uid)
			}
		}
		if w.Task != "" {
			t := r.Tasks[w.Task]
			if t != nil && t.State != "playing" && !terminal(t.State) && (!now.Before(t.Work.Expires) || !enabled(r, kind)) {
				e.cancel(r, t, "behavior_expired_or_disabled")
			}
			if t != nil && !terminal(t.State) {
				continue
			}
			if kind == "order" && (t == nil || t.Started.IsZero()) {
				w.Pending = true
				w.Retry = true
				w.RetryAt = now.Add(10 * time.Second)
			}
			if kind != "order" && t != nil && t.State == "failed" {
				w.RetryAt = now.Add(5 * time.Second)
			}
			w.Task = ""
		}
		if !enabled(r, kind) || now.Before(w.RetryAt) {
			continue
		}
		if kind != "order" && !w.LastStart.IsZero() && now.Before(w.LastStart.Add(w.Interval)) {
			continue
		}
		if kind == "order" && !w.Pending && now.Before(r.Cool[kind]) {
			continue
		}
		busy := false
		for _, t := range r.Tasks {
			if t.State == "selected" || t.State == "playing" {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		var entries []behaviorEntry
		for _, v := range w.Entries {
			if !v.At.After(now) {
				entries = append(entries, v)
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].At.Equal(entries[j].At) {
				return entries[i].Sequence > entries[j].Sequence
			}
			return entries[i].At.After(entries[j].At)
		})
		if kind != "order" {
			entries = entries[:min(len(entries), 10)]
		}
		if len(entries) == 0 {
			continue
		}
		if len(r.Tasks) >= 256 {
			for id, t := range r.Tasks {
				if terminal(t.State) {
					delete(r.Tasks, id)
				}
			}
		}
		if len(r.Tasks) >= 256 {
			continue
		}
		people := make([]Person, 0, min(len(entries), 10))
		refs := map[string]uint64{}
		expires := now.Add(30 * time.Second)
		for _, v := range entries {
			refs[v.UID] = v.Sequence
			if len(people) < 10 {
				people = append(people, v.Person)
			}
			if kind != "order" && v.At.Add(30*time.Second).Before(expires) {
				expires = v.At.Add(30 * time.Second)
			}
		}
		template := kind + "_v1"
		if kind == "order" {
			template = "order_single_v1"
			if len(entries) > 1 || w.Retry {
				template = "order_count_v1"
			}
		}
		t := e.add(r, Work{Scope: r.Scope, Kind: kind, Template: template, People: people, Count: len(entries), At: entries[len(entries)-1].At, Due: now, Expires: expires})
		t.SignalRefs = refs
		w.Task = t.Work.ID
		if kind == "order" {
			w.Pending = true
		}
	}
}

// This lane uses HTTP only to freeze plan/voice/P0 and build fixed templates;
// it never occupies a model slot or calls a text/name model.
func (e *Engine) TemplateBatches(now time.Time, limit int) []Batch {
	e.mu.Lock()
	defer e.mu.Unlock()
	var candidates []*Task
	for _, r := range e.rooms {
		for _, t := range r.Tasks {
			if t.Work.Template != "" && t.State == "collecting" && enabled(r, t.Work.Kind) && now.Before(t.Work.Expires) {
				candidates = append(candidates, t)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Work.Priority != candidates[j].Work.Priority {
			return candidates[i].Work.Priority > candidates[j].Work.Priority
		}
		return candidates[i].Work.Sequence < candidates[j].Work.Sequence
	})
	var out []Batch
	for _, t := range candidates[:min(max(limit, 0), len(candidates))] {
		t.State = "generating"
		t.Attempts++
		out = append(out, Batch{ID: fmt.Sprintf("bt-%s", t.Work.ID), Works: []Work{t.Work}})
	}
	return out
}

func behaviorStarted(r *room, t *Task, now time.Time) {
	w := r.Behaviors[t.Work.Kind]
	if w == nil || w.Task != t.Work.ID {
		return
	}
	w.LastStart = now
	w.Task = "" // played history may be compacted; no outstanding reservation remains
	for uid, seq := range t.SignalRefs {
		if v, ok := w.Entries[uid]; ok && v.Sequence == seq {
			delete(w.Entries, uid)
		}
		if t.Work.Kind == "order" {
			w.Seen[uid] = true
		}
	}
	if t.Work.Kind == "order" {
		w.Pending = false
		w.Retry = false
		w.RetryAt = time.Time{}
	}
}
