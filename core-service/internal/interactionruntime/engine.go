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

	"livecompanion/core/internal/roombrain"
)

const Protocol = "interaction_v2"

type Scope struct {
	Tenant, Room, Plan int64
	Epoch              string
}
type Policy struct {
	Active           bool
	Enabled          map[string]bool
	Cooldown         map[string]time.Duration
	SessionStartedAt time.Time
	LastEndedAt      time.Time
	Busy             bool
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
type ContextMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at,omitempty"`
}
type Work struct {
	Scope                                             Scope
	ID, Kind, Text, Original, Clarification, Mainline string
	EngagementToken                                   string
	Revision                                          uint64
	Sequence                                          uint64
	People                                            []Person
	EventIDs                                          []int64
	Recent                                            []Turn
	Context                                           []ContextMessage
	At, Due, Expires                                  time.Time
	CandidateExpires                                  time.Time
	BatchPrepared                                     bool
	WelcomeQuota                                      bool
	Template                                          string
	Count, Priority                                   int
	AllowClarify                                      bool
}
type Intent struct {
	Relation       string `json:"dialogue_relation,omitempty"`
	Relevant       bool   `json:"relevant"`
	Question       bool   `json:"question"`
	Meaning        string `json:"meaning"`
	Type           string `json:"type,omitempty"`
	Priority       string `json:"priority,omitempty"`
	PriorityReason string `json:"priority_reason,omitempty"`
	NeedClarify    bool   `json:"need_clarify"`
	Clarify        string `json:"clarify"`
	Continue       bool   `json:"continue_dialogue"`
	Command        string `json:"engagement_command"`
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
	SelectedAt         time.Time
	ReadyAt            time.Time
	Failure            string
	Attempts           int
	Estimate           time.Duration
	SignalRefs         map[string]uint64
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
	Token       string
	Tokens      []string
	Opened      time.Time
	PromptUntil time.Time
	Last        time.Time
	Users       map[string]bool
}
type room struct {
	Scope                               Scope
	Started, LastEvent, LastInteraction time.Time
	Tasks                               map[string]*Task
	Seen                                map[string]time.Time
	Dialogues                           map[string]*dialogue
	Chat                                []time.Time
	Played                              []sample
	Command                             *command
	Participation                       map[string]*command
	Spoken                              map[string]time.Time
	Carry                               string
	Density                             string
	Cool                                map[string]time.Time
	Policy                              *Policy
	Named                               map[string]time.Time
	DispatchSeq                         uint64
	History                             map[string][]ContextMessage
	SemanticUsers                       map[string]time.Time
	SemanticStarts                      []time.Time
	Responded                           map[string]time.Time
	Welcome                             *welcomeState
	Behaviors                           map[string]*behaviorLane
	Playback                            map[string]playbackActivity
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
		r = &room{Scope: s, Started: now, LastEvent: now, Tasks: map[string]*Task{}, Seen: map[string]time.Time{}, Dialogues: map[string]*dialogue{}, Cool: map[string]time.Time{}, Density: "cold", Named: map[string]time.Time{}}
		e.rooms[s.Room] = r
	}
	return r
}
func (e *Engine) SetPolicy(s Scope, p Policy, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.room(s, now)
	r.Policy = &p
	if !p.SessionStartedAt.IsZero() && !p.SessionStartedAt.After(now) {
		if r.LastEvent.Equal(r.Started) {
			r.LastEvent = p.SessionStartedAt
		}
		r.Started = p.SessionStartedAt
	}
	if p.LastEndedAt.After(r.LastInteraction) {
		r.LastInteraction = p.LastEndedAt
	}
	if !warmupAllowed(r, now) {
		e.cancelWarmups(r, "warmup_activity_changed", "")
	}
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
	for _, lane := range r.Behaviors {
		delete(lane.Entries, uid)
	}
	if r.Welcome != nil {
		delete(r.Welcome.Entries, uid)
	}
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
		return welcomeWindow
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
		return 6
	case "dialogue":
		return 5
	case "welcome":
		return 4
	case "order":
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
	w.Sequence = e.seq
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
	}
	if len(r.Seen) > 8192 {
		for k, at := range r.Seen {
			if now.Sub(at) > 10*time.Minute {
				delete(r.Seen, k)
			}
		}
	}
	if len(r.Seen) >= 16384 {
		return
	}
	if v.ID != "" {
		r.Seen[v.ID] = now
	}
	if v.Kind == "block" {
		delete(r.Dialogues, v.UID)
		for _, lane := range r.Behaviors {
			delete(lane.Entries, v.UID)
		}
		for _, t := range r.Tasks {
			for _, p := range t.Work.People {
				if p.UID == v.UID {
					e.cancel(r, t, "blocked")
				}
			}
		}
		return
	}
	// Explicit completed purchases bypass all text/intent models. Ordinary
	// questions retain their existing semantic path, without an order-model pass.
	if v.Kind == "like" || v.Kind == "follow" || v.Kind == "order" || v.Kind == "order_signal" {
		kind := v.Kind
		if kind == "order_signal" {
			kind = "order"
		}
		recordBehavior(r, v, kind, now)
		return
	}
	if (v.Kind == "chat" || v.Kind == "semantic") && roombrain.IsOrderSignalText(v.Text) {
		recordBehavior(r, v, "order", now)
		return
	}
	if v.Kind == "chat" || v.Kind == "semantic" || v.Kind == "urgent" {
		r.LastEvent = now
		r.Chat = append(r.Chat, now)
		rememberUser(r, v)
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
		if e.consumeParticipation(r, v, now) {
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
	if kind == "welcome" {
		recordWelcome(r, v, now)
		return
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
			// A burst must retain the newest candidates, not freeze the pool
			// around the oldest 256 messages. Active playback is never evicted.
			var oldest *Task
			if semanticKind(kind) {
				for _, task := range r.Tasks {
					if !semanticKind(task.Work.Kind) || task.State == "selected" || task.State == "playing" {
						continue
					}
					if oldest == nil || task.Work.At.Before(oldest.Work.At) || (task.Work.At.Equal(oldest.Work.At) && task.Work.Sequence < oldest.Work.Sequence) {
						oldest = task
					}
				}
			}
			if oldest == nil || v.At.Before(oldest.Work.At) {
				return
			}
			e.cancel(r, oldest, "newer_candidate_at_capacity")
			delete(r.Tasks, oldest.Work.ID)
		}
	}
	// Behavior batches aggregate people. Semantic batches retain separate tasks,
	// histories and outputs and are grouped only when dispatching one model call.
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
	if semanticKind(kind) {
		w.Context = priorContext(r, v.UID)
	}
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
	groups := e.jobs(now, limit, false)
	var jobs []Work
	for _, group := range groups {
		jobs = append(jobs, group...)
	}
	return jobs
}
func (e *Engine) jobs(now time.Time, limit int, batch bool) [][]Work {
	e.mu.Lock()
	defer e.mu.Unlock()
	var tasks []*Task
	for _, r := range e.rooms {
		e.prepareWelcome(r, now)
		e.prepareBehaviors(r, now)
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
		e.finishParticipation(r, now)
		active := false
		preparing := false
		if batch {
			latestCandidates(e, r, now)
			for _, t := range r.Tasks {
				if t.State == "generating" && semanticKind(t.Work.Kind) {
					preparing = true
				}
			}
		}
		for _, t := range r.Tasks {
			if !terminal(t.State) {
				active = true
			}
			if t.State != "playing" && !terminal(t.State) && !now.Before(t.Work.Expires) {
				e.cancel(r, t, "original_event_expired")
				t.State = "expired"
			}
			if t.Work.Template == "" && t.State == "collecting" && enabled(r, t.Work.Kind) && !now.Before(t.Work.Due) && (t.Work.WelcomeQuota || (batch && semanticKind(t.Work.Kind)) || !now.Before(r.Cool[t.Work.Kind])) && !(batch && semanticKind(t.Work.Kind) && preparing) {
				tasks = append(tasks, t)
			}
		}
		if enabled(r, "guide") && !active && warmupAllowed(r, now) {
			e.add(r, Work{Scope: r.Scope, Kind: "guide", Text: "当前没有新的互动，请结合当前讲解做一句自然的相关引导，必要时给一个简短口令。", At: now, Due: now, Expires: now.Add(30 * time.Second)})
		}
	}
	// Priority first, then the least recently serviced room. A busy room
	// cannot consume all model slots while another room has the same band.
	groups := make([][]*Task, 0, min(max(limit, 0), len(tasks)))
	for len(groups) < limit && len(tasks) > 0 {
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
		group := []*Task{selected}
		if batch && semanticKind(selected.Work.Kind) {
			candidates := []*Task{selected}
			remaining := tasks[:0]
			for _, t := range tasks {
				if t.Work.Scope == selected.Work.Scope && semanticKind(t.Work.Kind) {
					candidates = append(candidates, t)
				} else {
					remaining = append(remaining, t)
				}
			}
			tasks = remaining
			sort.Slice(candidates, func(i, j int) bool {
				if !candidates[i].Work.At.Equal(candidates[j].Work.At) {
					return candidates[i].Work.At.After(candidates[j].Work.At)
				}
				return candidates[i].Work.Sequence > candidates[j].Work.Sequence
			})
			group = nil
			seen := map[string]bool{}
			for _, t := range candidates {
				key := "anonymous"
				if len(t.Work.People) == 1 && t.Work.People[0].UID != "" {
					key = "uid:" + t.Work.People[0].UID
				}
				if seen[key] || len(group) >= 10 {
					continue
				}
				seen[key] = true
				group = append(group, t)
			}
		}
		groups = append(groups, group)
		e.dispatchSeq++
		e.rooms[selected.Work.Scope.Room].DispatchSeq = e.dispatchSeq
	}
	jobs := make([][]Work, 0, len(groups))
	for _, group := range groups {
		var work []Work
		for _, t := range group {
			t.State = "generating"
			t.Attempts++
			t.Work.BatchPrepared = batch
			if batch && semanticKind(t.Work.Kind) && t.Work.At.Add(60*time.Second).Before(t.Work.Expires) {
				t.Work.Expires = t.Work.At.Add(60 * time.Second)
			}
			w := t.Work
			if w.Kind == "dialogue" && len(w.People) == 1 {
				w.Context = priorContext(e.rooms[w.Scope.Room], w.People[0].UID)
			}
			r := e.rooms[w.Scope.Room]
			if r.Command != nil {
				w.EngagementToken = commandLabel(r.Command)
			}
			if r.Carry != "" {
				if batch {
					w.Mainline = r.Carry
				} else {
					w.Text += "\n" + r.Carry
				}
			}
			work = append(work, w)
		}
		jobs = append(jobs, work)
	}
	return jobs
}
func sortTasks(ts []*Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i].Work, ts[j].Work
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if contentPriority(ts[i]) != contentPriority(ts[j]) {
			return contentPriority(ts[i]) > contentPriority(ts[j])
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
		if t.Work.WelcomeQuota || t.Work.Template != "" {
			t.State = "failed"
			t.Failure = err.Error()
			return false
		}
		t.Failure = err.Error()
		if t.Attempts < 2 && now.Add(2*time.Second).Before(t.Work.Expires) {
			t.State = "collecting"
			t.Work.Due = now.Add(2 * time.Second)
		} else {
			t.State = "failed"
		}
		return false
	}
	// Classification is independent of playback quota. Raw pending messages,
	// participation and spam must not inflate the confirmed semantic density.
	if semanticKind(w.Kind) && c.Intent.Type == "主动互动" && w.Kind != "dialogue" {
		e.recordParticipation(r, w.Text, firstUID(w), w.At, true)
		t.State = "cancelled"
		t.Failure = "engagement_participation"
		return false
	}
	rememberSemantic(r, w, c.Intent)
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
	// Escalation is a program rule, not a model-provided rank: only a
	// classified complaint with an explicit urgent reason can enter this lane.
	if t.Work.Kind == "semantic" && c.Intent.Type == "投诉" && c.Intent.Priority == "紧急" && strings.TrimSpace(c.Intent.PriorityReason) != "" && enabled(r, "urgent") {
		t.Work.Kind = "urgent"
		t.Work.Priority = band("urgent")
	}
	t.Content = c
	if t.Work.WelcomeQuota {
		rebindDeadline(t)
	}
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
		if t.Work.Kind == "guide" && t.State != "playing" && !terminal(t.State) && !warmupAllowed(r, now) {
			e.cancel(r, t, "warmup_activity_changed")
			continue
		}
		if t.State == "playing" {
			return nil
		}
		if t.State == "selected" {
			selected = t
		}
		if t.State == "body_ready" && enabled(r, t.Work.Kind) && (t.Work.WelcomeQuota || t.Work.Template != "" || !now.Before(r.Cool[t.Work.Kind])) && now.Before(t.Work.Expires) {
			ready = append(ready, t)
		}
	}
	sortReady(r, ready)
	if selected != nil {
		if len(ready) == 0 || !welcomeSupersedes(ready[0], selected) {
			return nil
		}
	}
	budget := 40.0
	if r.Density == "normal" {
		budget = 30
	}
	if r.Density == "fast" {
		budget = 20
	}
	semanticAvailable := semanticQuotaAvailable(r, now, "")
	for _, t := range ready {
		if selected != nil && !welcomeSupersedes(t, selected) {
			continue
		}
		if t.Work.BatchPrepared && semanticKind(t.Work.Kind) && !semanticAvailable {
			continue
		}
		if t.Work.Kind != "urgent" && used+t.Estimate.Seconds() > budget {
			continue
		}
		if t.Work.WelcomeQuota && now.Sub(t.Work.At) >= welcomeWindow {
			e.cancel(r, t, "welcome_names_stale")
			continue
		}
		if selected != nil {
			if t.Work.WelcomeQuota {
				// Keep the ordinary answer as a candidate; only its unclaimed
				// queue reservation yields to the fresh welcome.
				if selected.Decision != "" {
					e.cancelled[r.Scope.Room] = append(e.cancelled[r.Scope.Room], selected.Decision)
				}
				selected.State, selected.Decision = "body_ready", ""
				if !selected.Work.CandidateExpires.IsZero() {
					selected.Work.Expires = selected.Work.CandidateExpires
					selected.Work.CandidateExpires = time.Time{}
					selected.SelectedAt = time.Time{}
					rebindDeadline(selected)
				}
			} else {
				e.cancel(r, selected, "urgent_preempted_unplayed")
			}
		}
		t.State = "selected"
		if t.Work.BatchPrepared && semanticKind(t.Work.Kind) {
			if t.SelectedAt.IsZero() {
				t.SelectedAt = now
				t.Work.CandidateExpires = t.Work.Expires
				t.Work.Expires = now.Add(50 * time.Second)
				rebindDeadline(t)
			}
		}
		if t.Work.WelcomeQuota {
			if now.Sub(t.Work.At) >= welcomeWindow {
				e.cancel(r, t, "welcome_names_stale")
				continue
			}
			if t.SelectedAt.IsZero() {
				t.SelectedAt = now
				t.Work.Expires = now.Add(welcomeWindow)
				rebindDeadline(t)
			}
		}
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
				if !t.Work.CandidateExpires.IsZero() {
					// No queue admission occurred; keep the candidate's original
					// deadline. TTS retries after a successful admission never pass here.
					t.Work.Expires = t.Work.CandidateExpires
					t.Work.CandidateExpires = time.Time{}
					t.SelectedAt = time.Time{}
					rebindDeadline(t)
				}
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
		if t.Work.Kind == "guide" && !warmupAllowed(r, start) {
			e.cancel(r, t, "warmup_activity_changed")
			return false
		}
		if t.Work.WelcomeQuota && t.ReadyAt.IsZero() {
			return false
		}
		if t.Work.BatchPrepared && semanticKind(t.Work.Kind) && !semanticQuotaAvailable(r, start, t.Work.ID) {
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
	if decision == "" || at.IsZero() || !e.recordPlayback(r, decision, status, at) {
		return
	}
	e.receiptsLocked(r, decision, status, at)
}

func (e *Engine) receiptsLocked(r *room, decision, status string, at time.Time) {
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
			if t.Work.WelcomeQuota && t.ReadyAt.IsZero() {
				return
			}
			t.State = "playing"
			t.Started = at
			if t.Work.BatchPrepared && semanticKind(t.Work.Kind) {
				r.SemanticStarts = append(r.SemanticStarts, at)
			}
			if r.Responded == nil {
				r.Responded = map[string]time.Time{}
			}
			for _, p := range t.Work.People {
				if p.UID != "" {
					r.Responded[p.UID] = at
				}
				if p.Name != "" {
					r.Named[p.UID] = at
				}
			}
			if at.After(r.LastInteraction) {
				r.LastInteraction = at
			}
			if t.Work.WelcomeQuota {
				welcomeStarted(r, t, at)
			}
			if t.Work.Template != "" {
				behaviorStarted(r, t, at)
			}
			r.Carry = ""
			r.Cool[t.Work.Kind] = at.Add(map[string]time.Duration{"welcome": 15 * time.Second, "like": 30 * time.Second, "follow": 20 * time.Second, "order": 5 * time.Second}[t.Work.Kind])
			if r.Policy != nil {
				if duration, ok := r.Policy.Cooldown[t.Work.Kind]; ok {
					r.Cool[t.Work.Kind] = at.Add(duration)
				}
			}
			if t.Content.Intent.NeedClarify && len(t.Work.People) == 1 {
				p := t.Work.People[0]
				r.Dialogues[p.UID] = &dialogue{UID: p.UID, Name: p.Name, Original: t.Work.Text, Clarify: t.Content.Body, Opened: at, Deadline: at.Add(90 * time.Second)}
			}
		case "COMPLETED":
			// A trusted completed PCM stream proves it actually played even if
			// its start notification was lost. Reconcile the original task once.
			if t.State == "selected" {
				started := at.Add(-t.Estimate)
				if started.Before(t.SelectedAt) {
					started = t.SelectedAt
				}
				if started.IsZero() || !started.Before(t.Work.Expires) {
					started = t.Work.Expires.Add(-time.Nanosecond)
				}
				e.receiptsLocked(r, decision, "PLAYING", started)
			}
			if t.State != "playing" {
				return
			}
			t.State = "completed"
			t.Completed = at
			if len(t.Work.People) == 1 {
				rememberAnswer(r, t.Work.People[0].UID, t.Content.Body, at)
			}
			seconds := at.Sub(t.Started).Seconds()
			if seconds < 0 {
				seconds = 0
			}
			r.Played = append(r.Played, sample{at, seconds})
			if at.After(r.LastInteraction) {
				r.LastInteraction = at
			}
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
				if at.After(r.LastInteraction) {
					r.LastInteraction = at
				}
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
		if p := r.Playback[decision]; !p.Started.IsZero() && p.Ended.IsZero() {
			e.recordPlayback(r, decision, "FAILED", now)
		}
		for _, t := range r.Tasks {
			if t.Decision == decision && (t.State == "selected" || t.State == "playing") {
				if t.State == "playing" {
					r.Played = append(r.Played, sample{now, max(0, now.Sub(t.Started).Seconds())})
					if now.After(r.LastInteraction) {
						r.LastInteraction = now
					}
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
