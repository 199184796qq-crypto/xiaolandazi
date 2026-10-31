package interactionruntime

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type Batch struct {
	ID    string
	Works []Work
}

// One dispatch slot means one model request, containing 1..10 independent people.
func (e *Engine) Batches(now time.Time, limit int) []Batch {
	groups := e.jobs(now, limit, true)
	out := make([]Batch, 0, len(groups))
	for _, group := range groups {
		if len(group) > 0 {
			out = append(out, Batch{ID: fmt.Sprintf("ib2-%s-%d-%d", group[0].ID, group[0].Revision, now.UnixNano()), Works: group})
		}
	}
	return out
}

func semanticKind(kind string) bool {
	return kind == "semantic" || kind == "urgent" || kind == "dialogue"
}

func rememberUser(r *room, v Event) {
	if v.UID == "" {
		return
	}
	if r.History == nil {
		r.History = map[string][]ContextMessage{}
	}
	if r.SemanticUsers == nil {
		r.SemanticUsers = map[string]time.Time{}
	}
	if len(r.History) >= 2048 {
		for uid, h := range r.History {
			if len(h) == 0 || v.At.Sub(h[len(h)-1].At) > 10*time.Minute {
				delete(r.History, uid)
				delete(r.SemanticUsers, uid)
				delete(r.Responded, uid)
			}
		}
		if len(r.History) >= 2048 {
			oldest := ""
			var at time.Time
			for uid, h := range r.History {
				if len(h) > 0 && (oldest == "" || h[len(h)-1].At.Before(at)) {
					oldest = uid
					at = h[len(h)-1].At
				}
			}
			delete(r.History, oldest)
			delete(r.SemanticUsers, oldest)
			delete(r.Responded, oldest)
		}
	}
	h := append(r.History[v.UID], ContextMessage{Role: "user", Text: v.Text, At: v.At})
	if len(h) > 6 {
		h = h[len(h)-6:]
	}
	r.History[v.UID] = h
}

func priorContext(r *room, uid string) []ContextMessage {
	h := r.History[uid]
	if len(h) < 2 {
		return nil
	}
	h = h[:len(h)-1] // current text is already supplied separately
	if len(h) > 5 {
		h = h[len(h)-5:]
	}
	return append([]ContextMessage(nil), h...)
}
func rememberAnswer(r *room, uid, body string, at time.Time) {
	if uid == "" || r.History == nil {
		return
	}
	h := append(r.History[uid], ContextMessage{Role: "assistant", Text: body, At: at})
	if len(h) > 6 {
		h = h[len(h)-6:]
	}
	r.History[uid] = h
}

// Only the freshest unscheduled message for each known person is eligible.
// Selected/playing tasks already own their execution lifetime and are untouched.
func latestCandidates(e *Engine, r *room, now time.Time) {
	kept := r.SemanticStarts[:0]
	for _, at := range r.SemanticStarts {
		if now.Sub(at) < 5*time.Minute {
			kept = append(kept, at)
		}
	}
	r.SemanticStarts = kept
	latest := map[string]*Task{}
	var candidates []*Task
	for _, t := range r.Tasks {
		if !semanticKind(t.Work.Kind) || terminal(t.State) || t.State == "selected" || t.State == "playing" {
			continue
		}
		if !now.Before(t.Work.At.Add(60 * time.Second)) {
			e.cancel(r, t, "candidate_window_expired")
			t.State = "expired"
			continue
		}
		candidates = append(candidates, t)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i].Work, candidates[j].Work
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		return a.Sequence > b.Sequence
	})
	for _, t := range candidates {
		if len(t.Work.People) != 1 || t.Work.People[0].UID == "" {
			continue
		}
		uid := t.Work.People[0].UID
		if latest[uid] != nil {
			e.cancel(r, t, "newer_message_from_same_user")
		} else {
			latest[uid] = t
		}
	}
}

func sortReady(r *room, ts []*Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if a.Work.Priority != b.Work.Priority {
			return a.Work.Priority > b.Work.Priority
		}
		if contentPriority(a) != contentPriority(b) {
			return contentPriority(a) > contentPriority(b)
		}
		last := func(t *Task) time.Time {
			if len(t.Work.People) == 1 {
				return r.Responded[t.Work.People[0].UID]
			}
			return time.Time{}
		}
		if !last(a).Equal(last(b)) {
			return last(a).Before(last(b))
		}
		if !a.Work.Expires.Equal(b.Work.Expires) {
			return a.Work.Expires.Before(b.Work.Expires)
		}
		return a.Work.Sequence < b.Work.Sequence
	})
}

// Business class is primary. A reasoned model urgency hint is a bounded
// adjustment inside that class; it cannot bypass quota, lifetime or playback.
func contentPriority(t *Task) int {
	base := map[string]int{"投诉": 50, "催单": 40, "下单意向": 30, "提问": 20, "闲聊": 10}[t.Content.Intent.Type]
	if t.Content.Intent.PriorityReason != "" {
		switch t.Content.Intent.Priority {
		case "较高":
			base += 2
		case "紧急":
			base += 4
		}
	}
	return base
}

func semanticQuotaAvailable(r *room, now time.Time, except string) bool {
	users := 0
	for _, at := range r.SemanticUsers {
		if !at.After(now) && now.Sub(at) < 180*time.Second {
			users++
		}
	}
	if users < 5 {
		return true
	}
	window := 60 * time.Second
	if users > 10 {
		window = 180 * time.Second
	}
	if users > 20 {
		window = 300 * time.Second
	}
	used := 0
	for _, at := range r.SemanticStarts {
		if !at.After(now) && now.Sub(at) < window {
			used++
		}
	}
	for _, t := range r.Tasks {
		if t.Work.ID != except && t.State == "selected" && now.Before(t.Work.Expires) && semanticKind(t.Work.Kind) {
			used++
		}
	}
	return used < 2
}

func rebindDeadline(t *Task) {
	if len(t.Content.Knowledge) == 0 {
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(t.Content.Knowledge, &envelope) != nil {
		return
	}
	expires, _ := json.Marshal(t.Work.Expires)
	envelope["expires_at"] = expires
	t.Content.Knowledge, _ = json.Marshal(envelope)
}
