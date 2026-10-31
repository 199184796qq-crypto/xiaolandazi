package interactionruntime

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const welcomeWindow = 30 * time.Second

type welcomeEntry struct {
	Person
	At       time.Time
	Sequence uint64
}

// One disposable entitlement per room/epoch. An expired rendition never spends
// the entitlement. No timer accumulates another entitlement behind this one.
type welcomeState struct {
	Entries                map[string]welcomeEntry
	Welcomed               map[string]time.Time
	Sequence               uint64
	Pending                bool
	Task                   string
	LastStart, RetryAt     time.Time
	Interval, Proposed     time.Duration
	ProposedAt, EvaluateAt time.Time
	Failures               int
}

func welcomeInterval(entries, semantic int) time.Duration {
	column := 0
	if entries > 5 {
		column = 1
	}
	if entries > 30 {
		column = 2
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
	return [4][3]time.Duration{{0, 30, 30}, {60, 120, 180}, {120, 180, 300}, {180, 300, 300}}[row][column] * time.Second
}

func recordWelcome(r *room, v Event, now time.Time) {
	if v.UID == "" || v.At.After(now.Add(2*time.Second)) || now.Sub(v.At) >= welcomeWindow {
		return
	}
	if r.Welcome == nil {
		r.Welcome = &welcomeState{Entries: map[string]welcomeEntry{}, Welcomed: map[string]time.Time{}}
	}
	w := r.Welcome
	if old, ok := w.Entries[v.UID]; ok && v.At.Before(old.At) {
		return
	}
	w.Sequence++
	w.Entries[v.UID] = welcomeEntry{Person{v.UID, v.Name}, v.At, w.Sequence}
	for uid, entry := range w.Entries {
		if now.Sub(entry.At) >= time.Minute {
			delete(w.Entries, uid)
		}
	}
	if len(w.Entries) > 4096 {
		var oldest string
		for uid, entry := range w.Entries {
			if oldest == "" || entry.At.Before(w.Entries[oldest].At) {
				oldest = uid
			}
		}
		delete(w.Entries, oldest)
	}
}

func welcomePeople(r *room, now time.Time) []welcomeEntry {
	var entries []welcomeEntry
	if r.Welcome == nil {
		return entries
	}
	for uid, v := range r.Welcome.Entries {
		if v.At.After(now) || now.Sub(v.At) >= welcomeWindow {
			continue
		}
		if at := r.Welcome.Welcomed[uid]; !at.IsZero() && now.Sub(at) < 5*time.Minute {
			continue
		}
		entries = append(entries, v)
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].At.Equal(entries[j].At) {
			return entries[i].At.After(entries[j].At)
		}
		return entries[i].Sequence > entries[j].Sequence
	})
	return entries[:min(len(entries), 10)]
}

func updateWelcomeInterval(r *room, now time.Time) {
	w := r.Welcome
	if !w.EvaluateAt.IsZero() && now.Before(w.EvaluateAt) {
		return
	}
	w.EvaluateAt = now.Add(10 * time.Second)
	entries, semantic := 0, 0
	for uid, v := range w.Entries {
		if now.Sub(v.At) >= time.Minute {
			delete(w.Entries, uid)
		} else if !v.At.After(now) {
			entries++
		}
	}
	for uid, at := range w.Welcomed {
		if now.Sub(at) >= 5*time.Minute {
			delete(w.Welcomed, uid)
		}
	}
	for _, at := range r.SemanticUsers {
		if !at.After(now) && now.Sub(at) < 180*time.Second {
			semantic++
		}
	}
	want := welcomeInterval(entries, semantic)
	if w.LastStart.IsZero() && w.Task == "" {
		w.Interval = want
		w.Proposed = want
		return
	}
	if want == w.Interval {
		w.Proposed = want
		w.ProposedAt = time.Time{}
		return
	}
	if want != w.Proposed || w.ProposedAt.IsZero() {
		w.Proposed = want
		w.ProposedAt = now
		return
	}
	wait := 60 * time.Second
	if want > w.Interval {
		wait = 15 * time.Second
	}
	if now.Sub(w.ProposedAt) >= wait {
		w.Interval = want
		w.ProposedAt = time.Time{}
	}
}

func welcomeRetry(r *room, now time.Time) {
	if r.Welcome == nil {
		return
	}
	w := r.Welcome
	w.Task = ""
	w.Pending = true
	w.Failures++
	w.RetryAt = now.Add(time.Duration(min(30, 5*(1<<min(w.Failures-1, 3)))) * time.Second)
}

func (e *Engine) prepareWelcome(r *room, now time.Time) {
	w := r.Welcome
	if w == nil {
		return
	}
	updateWelcomeInterval(r, now)
	if !enabled(r, "welcome") {
		if t := r.Tasks[w.Task]; t != nil && t.State != "playing" && !terminal(t.State) {
			e.cancel(r, t, "welcome_disabled")
		}
		return
	}
	if w.Task != "" {
		t := r.Tasks[w.Task]
		if t != nil && !terminal(t.State) && t.State != "playing" && !now.Before(t.Work.Expires) {
			e.cancel(r, t, "welcome_rendition_expired")
			t.State = "expired"
		}
		if t != nil && !terminal(t.State) {
			return
		}
		if t != nil && !t.Started.IsZero() {
			w.Task = ""
		} else {
			welcomeRetry(r, now)
		}
	}
	if !enabled(r, "welcome") || now.Before(w.RetryAt) {
		return
	}
	if !w.Pending {
		if !w.LastStart.IsZero() && now.Before(w.LastStart.Add(w.Interval)) {
			return
		}
		w.Pending = true
	}
	// Do not create paid work while an interaction owns the playback lane.
	for _, t := range r.Tasks {
		if t.State == "playing" || (t.State == "selected" && t.Work.Priority >= band("welcome")) {
			return
		}
	}
	entries := welcomePeople(r, now)
	if len(entries) == 0 {
		return
	}
	if len(r.Tasks) >= 256 {
		for id, t := range r.Tasks {
			if terminal(t.State) {
				delete(r.Tasks, id)
			}
		}
	}
	if len(r.Tasks) >= 256 {
		return
	}
	people := make([]Person, len(entries))
	for i, v := range entries {
		people[i] = v.Person
	}
	t := e.add(r, Work{Scope: r.Scope, Kind: "welcome", WelcomeQuota: true, Text: fmt.Sprintf("请按本场方案生成一条简短欢迎正文。近期有%d名不同的新进观众，单人和多人共用一条正文，称呼由后置阶段处理；不要逐人分别回答，不要在正文加入昵称。", len(people)), People: people, Count: len(people), At: entries[0].At, Due: now, Expires: now.Add(welcomeWindow)})
	w.Task = t.Work.ID
}

// ReadyWelcome is called only after address, join and whole-utterance TTS have
// finished. The thirty-second playback deadline is assigned exactly once.
func (e *Engine) ReadyWelcome(roomID int64, decision string, now time.Time) (Task, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[roomID]
	if r == nil || r.Welcome == nil || !enabled(r, "welcome") {
		return Task{}, false
	}
	t := r.Tasks[r.Welcome.Task]
	if t == nil || !t.Work.WelcomeQuota || t.Decision != decision || t.State != "selected" || !now.Before(t.Work.Expires) {
		return Task{}, false
	}
	if t.ReadyAt.IsZero() {
		// A slow provider must not admit names that were already stale before
		// the finished greeting reached the playback queue.
		if now.Sub(t.Work.At) >= welcomeWindow {
			return Task{}, false
		}
		t.ReadyAt = now
		t.Work.Expires = now.Add(welcomeWindow)
		rebindDeadline(t)
	}
	copy := *t
	copy.Content.Knowledge = append(json.RawMessage(nil), t.Content.Knowledge...)
	return copy, true
}

func welcomeStarted(r *room, t *Task, at time.Time) {
	w := r.Welcome
	if w == nil || w.Task != t.Work.ID {
		return
	}
	w.Pending = false
	w.LastStart = at
	w.Failures = 0
	w.RetryAt = time.Time{}
	for _, p := range t.Work.People {
		w.Welcomed[p.UID] = at
	}
}

func welcomeSupersedes(next, current *Task) bool {
	if current.Work.Kind == "urgent" {
		return false
	}
	if next.Work.Kind == "urgent" {
		return true
	}
	return next.Work.WelcomeQuota && next.Work.Priority > current.Work.Priority
}
