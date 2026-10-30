// Package interactionruntime owns disposable interaction scheduling. All times
// and results enter explicitly; the same state machine runs in tests and Core.
package interactionruntime

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const Protocol = "interaction_v2"

type Scope struct {
	Tenant, Room, Plan int64
	Epoch              string
}
type Policy struct {
	Active   bool
	Enabled  map[string]bool
	Cooldown map[string]time.Duration
}
type Person struct{ UID, Name string }
type Event struct {
	ID                    string
	UID, Name, Kind, Text string
	At                    time.Time
	Urgent                bool
}
type Turn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
type Work struct {
	Scope                                             Scope
	ID, Kind, Text, Original, Clarification, Mainline string
	Revision                                          uint64
	People                                            []Person
	EventIDs                                          []int64
	Recent                                            []Turn
	At, Due, Expires                                  time.Time
	Count, Priority                                   int
	AllowClarify                                      bool
}
type Intent struct {
	Relation    string `json:"dialogue_relation,omitempty"`
	Relevant    bool   `json:"relevant"`
	Question    bool   `json:"question"`
	Meaning     string `json:"meaning"`
	NeedClarify bool   `json:"need_clarify"`
	Clarify     string `json:"clarify"`
	Continue    bool   `json:"continue_dialogue"`
	Command     string `json:"engagement_command"`
}
type Content struct {
	Body      string
	Intent    Intent
	Knowledge json.RawMessage
}
type Task struct {
	Work               Work
	Content            Content
	State, Decision    string
	Started, Completed time.Time
	Failure            string
	Attempts           int
	Estimate           time.Duration
}
type dialogue struct {
	UID, Name, Original, Clarify  string
	Turns                         []Turn
	Opened, Deadline, First, Last time.Time
	Inputs                        []string
	Round                         int
	Revision                      uint64
	Pending                       string
}
type sample struct {
	At      time.Time
	Seconds float64
}
type command struct {
	Token string
	Last  time.Time
	Users map[string]bool
}
type room struct {
	Scope                                          Scope
	Started, LastEvent, LastInteraction, LastGuide time.Time
	Tasks                                          map[string]*Task
	Seen                                           map[string]time.Time
	Dialogues                                      map[string]*dialogue
	Chat                                           []time.Time
	Played                                         []sample
	Command                                        *command
	Carry                                          string
	Density                                        string
	Cool                                           map[string]time.Time
	Policy                                         *Policy
	Named                                          map[string]time.Time
	DispatchSeq                                    uint64
}
type Engine struct {
	mu          sync.Mutex
	rooms       map[int64]*room
	seq         uint64
	dispatchSeq uint64
	cancelled   map[int64][]string
}

func New() *Engine { return &Engine{rooms: map[int64]*room{}, cancelled: map[int64][]string{}} }
func (e *Engine) room(s Scope, now time.Time) *room {
	r := e.rooms[s.Room]
	if r == nil || r.Scope != s {
		r = &room{Scope: s, Started: now, LastEvent: now, LastInteraction: now, Tasks: map[string]*Task{}, Seen: map[string]time.Time{}, Dialogues: map[string]*dialogue{}, Cool: map[string]time.Time{}, Density: "cold", Named: map[string]time.Time{}}
		e.rooms[s.Room] = r
	}
	return r
}
func (e *Engine) SetPolicy(s Scope, p Policy, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.room(s, now).Policy = &p
}
func enabled(r *room, kind string) bool {
	if r.Policy == nil {
		return true
	}
	if !r.Policy.Active {
		return false
	}
	v, ok := r.Policy.Enabled[kind]
	return !ok || v
}
func (e *Engine) Reset(roomID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.rooms, roomID)
	delete(e.cancelled, roomID)
}
func (e *Engine) CancelUser(s Scope, uid string, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.room(s, now)
	delete(r.Dialogues, uid)
	for _, t := range r.Tasks {
		for _, p := range t.Work.People {
			if p.UID == uid && t.State != "playing" && t.State != "completed" {
				e.cancel(r, t, "blocked")
			}
		}
	}
}
func (e *Engine) cancel(r *room, t *Task, reason string) {
	if t.State == "playing" || t.State == "completed" {
		return
	}
	t.State = "cancelled"
	t.Failure = reason
	if t.Decision != "" {
		e.cancelled[r.Scope.Room] = append(e.cancelled[r.Scope.Room], t.Decision)
	}
}
func ttl(kind string) time.Duration {
	switch kind {
	case "welcome":
		return 20 * time.Second
	case "like", "follow":
		return 30 * time.Second
	case "urgent":
		return 120 * time.Second
	default:
		return 60 * time.Second
	}
}
func band(kind string) int {
	switch kind {
	case "urgent":
		return 5
	case "dialogue":
		return 4
	case "engagement":
		return 3
	case "semantic":
		return 2
	case "guide":
		return 0
	default:
		return 1
	}
}
func (e *Engine) add(r *room, w Work) *Task {
	e.seq++
	w.ID = fmt.Sprintf("iv2-%d-%d", w.Scope.Room, e.seq)
	w.Revision = 1
	w.Priority = band(w.Kind)
	w.AllowClarify = w.Kind == "semantic" || w.Kind == "urgent"
	t := &Task{Work: w, State: "collecting"}
	r.Tasks[w.ID] = t
	return t
}
func (e *Engine) Add(s Scope, v Event, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.room(s, now)
	if v.At.IsZero() {
		v.At = now
	}
	if v.At.After(now.Add(2*time.Second)) || now.Sub(v.At) > ttl(func() string {
		if v.Urgent {
			return "urgent"
		}
		return "semantic"
	}()) || utf8.RuneCountInString(v.Text) > 2400 {
		return
	}
	if v.ID != "" {
		if _, ok := r.Seen[v.ID]; ok {
			return
		}
		r.Seen[v.ID] = now
	}
	if len(r.Seen) > 8192 {
		for k, at := range r.Seen {
			if now.Sub(at) > 10*time.Minute {
				delete(r.Seen, k)
			}
		}
	}
	if len(r.Seen) > 16384 {
		return
	}
	if v.Kind == "block" {
		delete(r.Dialogues, v.UID)
		for _, t := range r.Tasks {
			for _, p := range t.Work.People {
				if p.UID == v.UID {
					e.cancel(r, t, "blocked")
				}
			}
		}
		return
	}
	if v.Kind == "chat" || v.Kind == "semantic" || v.Kind == "urgent" {
		r.LastEvent = now
		r.Chat = append(r.Chat, now)
		// Stable UID owns dialogue before short numeric/command filtering.
		if d := r.Dialogues[v.UID]; d != nil && v.UID != "" && now.Before(d.Deadline) && now.Sub(d.Opened) < 180*time.Second && d.Round < 3 && !v.Urgent {
			if old := r.Tasks[d.Pending]; old != nil && old.State != "playing" && old.State != "completed" {
				e.cancel(r, old, "new_dialogue_input")
			}
			if len(d.Inputs) == 0 {
				d.First = now
			}
			d.Last = now
			d.Revision++
			d.Name = v.Name
			d.Inputs = append(d.Inputs, v.Text)
			if len(d.Inputs) > 5 {
				d.Inputs = d.Inputs[len(d.Inputs)-5:]
			}
			d.Pending = ""
			return
		}
		if r.Command != nil && strings.TrimSpace(v.Text) == r.Command.Token && v.UID != "" {
			r.Command.Users[v.UID] = true
			r.Command.Last = now
			return
		}
	}
	kind := v.Kind
	switch kind {
	case "member":
		kind = "welcome"
	case "chat":
		kind = "semantic"
	case "order", "order_signal":
		kind = "order"
	}
	if v.Urgent {
		kind = "urgent"
	}
	switch kind {
	case "semantic", "urgent", "welcome", "like", "follow", "order":
	default:
		return
	}
	if len(r.Tasks) >= 256 {
		for id, t := range r.Tasks {
			if t.State == "completed" || t.State == "cancelled" || t.State == "expired" || t.State == "failed" {
				delete(r.Tasks, id)
			}
		}
		if len(r.Tasks) >= 256 {
			return
		}
	}
	// Only behavior batches aggregate. Independent questions never share a model
	// response or a fabricated result-to-index mapping.
	if band(kind) == 1 {
		for _, t := range r.Tasks {
			if t.Work.Kind == kind && t.State == "collecting" && now.Before(t.Work.Due) && t.Work.Count < 10 {
				t.Work.Count++
				if v.UID != "" {
					t.Work.People = appendPerson(t.Work.People, Person{v.UID, v.Name})
				}
				return
			}
		}
	}
	wait := time.Second
	if r.Density == "normal" {
		wait = 3 * time.Second
	}
	if r.Density == "fast" {
		wait = 5 * time.Second
	}
	if kind == "urgent" {
		wait = 0
	}
	w := Work{Scope: s, Kind: kind, Text: v.Text, At: v.At, Due: now.Add(wait), Expires: v.At.Add(ttl(kind)), Count: 1}
	if v.UID != "" {
		w.People = []Person{{v.UID, v.Name}}
	}
	e.add(r, w)
}
func appendPerson(p []Person, v Person) []Person {
	for _, old := range p {
		if old.UID == v.UID {
			return p
		}
	}
	if len(p) < 10 {
		return append(p, v)
	}
	return p
}
func terminal(s string) bool {
	return s == "completed" || s == "cancelled" || s == "expired" || s == "failed"
}
func (e *Engine) Jobs(now time.Time, limit int) []Work {
	e.mu.Lock()
	defer e.mu.Unlock()
	var tasks []*Task
	for _, r := range e.rooms {
		if r.Policy != nil && !r.Policy.Active {
			continue
		}
		cutoff := now.Add(-10 * time.Minute)
		kept := r.Chat[:0]
		for _, at := range r.Chat {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		r.Chat = kept
		minutes := now.Sub(r.Started).Minutes()
		if minutes < 1 {
			minutes = 1
		}
		if minutes > 10 {
			minutes = 10
		}
		rate := float64(len(r.Chat)) / minutes
		switch r.Density {
		case "fast":
			if rate < 3 {
				r.Density = "normal"
			}
		case "normal":
			if rate > 3.5 {
				r.Density = "fast"
			} else if rate < 1 {
				r.Density = "cold"
			}
		default:
			if rate > 3.5 {
				r.Density = "fast"
			} else if rate > 1.5 {
				r.Density = "normal"
			}
		}
		for uid, d := range r.Dialogues {
			if !now.Before(d.Deadline) || now.Sub(d.Opened) >= 180*time.Second || d.Round >= 3 {
				delete(r.Dialogues, uid)
				continue
			}
			if len(d.Inputs) > 0 && (now.Sub(d.Last) >= 5*time.Second || now.Sub(d.First) >= 10*time.Second) {
				t := e.add(r, Work{Scope: r.Scope, Kind: "dialogue", Text: strings.Join(d.Inputs, "；"), Original: d.Original, Clarification: d.Clarify, Recent: append([]Turn(nil), d.Turns...), People: []Person{{d.UID, d.Name}}, At: d.First, Due: now, Expires: d.First.Add(60 * time.Second), Count: len(d.Inputs)})
				t.Work.Revision = d.Revision
				t.Work.AllowClarify = false
				d.Pending = t.Work.ID
				d.Inputs = nil
			}
		}
		if c := r.Command; c != nil && now.Sub(c.Last) >= 10*time.Second {
			count := len(c.Users)
			r.Command = nil
			if count >= 3 {
				e.add(r, Work{Scope: r.Scope, Kind: "engagement", Text: fmt.Sprintf("刚才口令%s，共有%d位不同观众参与，请自然回应统计结果。", c.Token, count), At: now, Due: now, Expires: now.Add(30 * time.Second), Count: count})
			} else if count > 0 {
				r.Carry = fmt.Sprintf("刚才有%d位观众回应口令%s，可在本次正文中简短接住。", count, c.Token)
			}
		}
		active := false
		for _, t := range r.Tasks {
			if !terminal(t.State) {
				active = true
			}
			if t.State != "playing" && !terminal(t.State) && !now.Before(t.Work.Expires) {
				e.cancel(r, t, "original_event_expired")
				t.State = "expired"
			}
			if t.State == "collecting" && enabled(r, t.Work.Kind) && !now.Before(t.Work.Due) && !now.Before(r.Cool[t.Work.Kind]) {
				tasks = append(tasks, t)
			}
		}
		if enabled(r, "guide") && !active && now.Sub(r.LastEvent) >= 60*time.Second && now.Sub(r.LastInteraction) >= 60*time.Second && now.Sub(r.LastGuide) >= 45*time.Second {
			e.add(r, Work{Scope: r.Scope, Kind: "guide", Text: "当前没有新的互动，请结合当前讲解做一句自然的相关引导，必要时给一个简短口令。", At: now, Due: now, Expires: now.Add(30 * time.Second)})
		}
	}
	// Priority first, then the least recently serviced room. A busy room
	// cannot consume all model slots while another room has the same band.
	chosen := make([]*Task, 0, min(max(limit, 0), len(tasks)))
	for len(chosen) < limit && len(tasks) > 0 {
		sort.Slice(tasks, func(i, j int) bool {
			a, b := tasks[i].Work, tasks[j].Work
			if a.Priority != b.Priority {
				return a.Priority > b.Priority
			}
			ra, rb := e.rooms[a.Scope.Room], e.rooms[b.Scope.Room]
			if ra.DispatchSeq != rb.DispatchSeq {
				return ra.DispatchSeq < rb.DispatchSeq
			}
			if !a.Expires.Equal(b.Expires) {
				return a.Expires.Before(b.Expires)
			}
			return a.ID < b.ID
		})
		selected := tasks[0]
		tasks = tasks[1:]
		chosen = append(chosen, selected)
		e.dispatchSeq++
		e.rooms[selected.Work.Scope.Room].DispatchSeq = e.dispatchSeq
	}
	tasks = chosen
	jobs := make([]Work, 0, len(tasks))
	for _, t := range tasks {
		t.State = "generating"
		t.Attempts++
		w := t.Work
		r := e.rooms[w.Scope.Room]
		if r.Carry != "" {
			w.Text += "\n" + r.Carry
		}
		jobs = append(jobs, w)
	}
	return jobs
}
func sortTasks(ts []*Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i].Work, ts[j].Work
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.Expires.Before(b.Expires)
	})
}
func (e *Engine) Accept(w Work, c Content, err error, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[w.Scope.Room]
	if r == nil || r.Scope != w.Scope {
		return false
	}
	t := r.Tasks[w.ID]
	if t == nil || t.State != "generating" || t.Work.Revision != w.Revision || !now.Before(t.Work.Expires) {
		return false
	}
	if err != nil {
		t.Failure = err.Error()
		if t.Attempts < 2 && now.Add(2*time.Second).Before(t.Work.Expires) {
			t.State = "collecting"
			t.Work.Due = now.Add(2 * time.Second)
		} else {
			t.State = "failed"
		}
		return false
	}
	if w.Kind == "semantic" {
		kind := "chat"
		if c.Intent.Question {
			kind = "question"
		}
		if !enabled(r, kind) {
			t.State = "cancelled"
			return false
		}
	}
	if !c.Intent.Relevant || strings.TrimSpace(c.Body) == "" {
		t.State = "cancelled"
		if w.Kind == "dialogue" && len(w.People) > 0 {
			delete(r.Dialogues, w.People[0].UID)
		}
		return false
	}
	if w.Kind == "dialogue" && c.Intent.Relation == "new" {
		if len(w.People) > 0 {
			delete(r.Dialogues, w.People[0].UID)
		}
		t.Work.Kind = "semantic"
		t.Work.Priority = band("semantic")
		t.Work.Original = ""
		t.Work.Clarification = ""
		t.Work.Recent = nil
	}
	t.Content = c
	t.State = "body_ready"
	t.Estimate = time.Duration(max(2, min(30, utf8.RuneCountInString(c.Body)/5))) * time.Second
	return true
}
func (e *Engine) Select(s Scope, now time.Time) *Task {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.room(s, now)
	var ready []*Task
	var selected *Task
	used := 0.0
	kept := r.Played[:0]
	for _, v := range r.Played {
		if now.Sub(v.At) < 60*time.Second {
			kept = append(kept, v)
			seconds := v.Seconds
			if overlap := v.At.Sub(now.Add(-60 * time.Second)).Seconds(); seconds > overlap {
				seconds = overlap
			}
			used += max(0, seconds)
		}
	}
	r.Played = kept
	for _, t := range r.Tasks {
		if t.State == "playing" {
			return nil
		}
		if t.State == "selected" {
			selected = t
		}
		if t.State == "body_ready" && enabled(r, t.Work.Kind) && !now.Before(r.Cool[t.Work.Kind]) && now.Before(t.Work.Expires) {
			ready = append(ready, t)
		}
	}
	sortTasks(ready)
	if selected != nil {
		if len(ready) == 0 || ready[0].Work.Kind != "urgent" || selected.Work.Kind == "urgent" {
			return nil
		}
		e.cancel(r, selected, "urgent_preempted_unplayed")
	}
	budget := 40.0
	if r.Density == "normal" {
		budget = 30
	}
	if r.Density == "fast" {
		budget = 20
	}
	for _, t := range ready {
		if t.Work.Kind != "urgent" && used+t.Estimate.Seconds() > budget {
			continue
		}
		t.State = "selected"
		copy := *t
		copy.Work.People = append([]Person(nil), t.Work.People...)
		for i, p := range copy.Work.People {
			if at := r.Named[p.UID]; !at.IsZero() && now.Sub(at) < 5*time.Minute {
				copy.Work.People[i].Name = ""
			}
		}
		t.Work.People = append([]Person(nil), copy.Work.People...)
		return &copy
	}
	return nil
}
func (e *Engine) Bind(w Work, decision string, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r := e.rooms[w.Scope.Room]; r != nil && r.Scope == w.Scope {
		if t := r.Tasks[w.ID]; t != nil && t.State == "selected" {
			t.Decision = decision
			if decision == "" {
				t.State = "body_ready"
			}
			return
		}
	}
}
func (e *Engine) ReserveAudio(roomID int64, decision string, durationMS int, start time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[roomID]
	if r == nil || durationMS <= 0 {
		return false
	}
	for _, t := range r.Tasks {
		if t.Decision != decision {
			continue
		}
		if t.State != "selected" || !start.Before(t.Work.Expires) {
			return false
		}
		used := 0.0
		for _, v := range r.Played {
			overlap := min(v.Seconds, v.At.Sub(start.Add(-60*time.Second)).Seconds())
			used += max(0, overlap)
		}
		budget := 40.0
		if r.Density == "normal" {
			budget = 30
		}
		if r.Density == "fast" {
			budget = 20
		}
		t.Estimate = time.Duration(durationMS) * time.Millisecond
		if t.Work.Kind != "urgent" && float64(durationMS)/1000 > 40 {
			e.cancel(r, t, "audio_exceeds_interaction_window")
			return false
		}
		return t.Work.Kind == "urgent" || used+float64(durationMS)/1000 <= budget
	}
	return false
}
func (e *Engine) Receipts(roomID int64, decision, status string, at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[roomID]
	if r == nil {
		return
	}
	for _, t := range r.Tasks {
		if t.Decision != decision || decision == "" {
			continue
		}
		switch status {
		case "PLAYING", "PROGRESS":
			if t.State != "selected" {
				return
			}
			if !at.Before(t.Work.Expires) {
				e.cancel(r, t, "late_playback")
				return
			}
			t.State = "playing"
			t.Started = at
			for _, p := range t.Work.People {
				if p.Name != "" {
					r.Named[p.UID] = at
				}
			}
			r.LastInteraction = at
			r.Carry = ""
			r.Cool[t.Work.Kind] = at.Add(map[string]time.Duration{"welcome": 15 * time.Second, "like": 30 * time.Second, "follow": 20 * time.Second, "order": 5 * time.Second}[t.Work.Kind])
			if r.Policy != nil {
				if duration, ok := r.Policy.Cooldown[t.Work.Kind]; ok {
					r.Cool[t.Work.Kind] = at.Add(duration)
				}
			}
			if t.Work.Kind == "guide" {
				r.LastGuide = at
			}
			if t.Content.Intent.NeedClarify && len(t.Work.People) == 1 {
				p := t.Work.People[0]
				r.Dialogues[p.UID] = &dialogue{UID: p.UID, Name: p.Name, Original: t.Work.Text, Clarify: t.Content.Body, Opened: at, Deadline: at.Add(90 * time.Second)}
			}
			if token := strings.TrimSpace(t.Content.Intent.Command); t.Work.Kind == "guide" && token != "" && utf8.RuneCountInString(token) <= 8 && strings.Contains(t.Content.Body, token) {
				r.Command = &command{Token: token, Last: at, Users: map[string]bool{}}
			}
		case "COMPLETED":
			if t.State != "playing" {
				return
			}
			t.State = "completed"
			t.Completed = at
			seconds := at.Sub(t.Started).Seconds()
			if seconds < 0 {
				seconds = 0
			}
			r.Played = append(r.Played, sample{at, seconds})
			r.LastInteraction = at
			if len(t.Work.People) == 1 {
				uid := t.Work.People[0].UID
				if d := r.Dialogues[uid]; d != nil {
					if t.Content.Intent.NeedClarify {
						d.Deadline = at.Add(60 * time.Second)
					} else if t.Work.Kind == "dialogue" {
						d.Round++
						d.Turns = append(d.Turns, Turn{t.Work.Text, t.Content.Body})
						if len(d.Turns) > 5 {
							d.Turns = d.Turns[len(d.Turns)-5:]
						}
						d.Deadline = at.Add(60 * time.Second)
						if !t.Content.Intent.Continue && len(d.Inputs) == 0 {
							delete(r.Dialogues, uid)
						}
					}
				}
			}
		case "FAILED":
			if t.State == "playing" {
				r.Played = append(r.Played, sample{at, max(0, at.Sub(t.Started).Seconds())})
				r.LastInteraction = at
			}
			t.State = "failed"
			t.Failure = "playback_failed"
		}
		return
	}
}
func (e *Engine) Lost(roomID int64, decision string, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r := e.rooms[roomID]; r != nil {
		for _, t := range r.Tasks {
			if t.Decision == decision && (t.State == "selected" || t.State == "playing") {
				if t.State == "playing" {
					r.Played = append(r.Played, sample{now, max(0, now.Sub(t.Started).Seconds())})
					r.LastInteraction = now
				}
				t.State = "failed"
				t.Failure = "queue_task_lost"
			}
		}
	}
}
func (e *Engine) Cancelled(roomID int64) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := append([]string(nil), e.cancelled[roomID]...)
	delete(e.cancelled, roomID)
	return v
}
func (e *Engine) Snapshot(roomID int64) []Task {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[roomID]
	var out []Task
	if r != nil {
		for _, t := range r.Tasks {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Work.ID < out[j].Work.ID })
	return out
}
