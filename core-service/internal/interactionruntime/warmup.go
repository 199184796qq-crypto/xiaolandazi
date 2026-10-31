package interactionruntime

import "time"

// WarmupMinGap is measured from the end of every actually played interaction,
// including manual speech. First warmup starts no earlier than this run's start.
const WarmupMinGap = 180 * time.Second

type playbackActivity struct{ Started, Ended time.Time }

// Reconcile even manual playback when its queue item was cancelled or lost
// before a terminal callback. An obsolete run never touches a replacement room.
func (e *Engine) ReconcilePlayback(s Scope, queued map[string]bool, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[s.Room]
	if r == nil || r.Scope != s {
		return
	}
	for id, p := range r.Playback {
		if !p.Started.IsZero() && p.Ended.IsZero() && !queued[id] {
			e.recordPlayback(r, id, "FAILED", now)
		}
	}
}

func warmupAllowed(r *room, now time.Time) bool {
	if r.Policy != nil && r.Policy.Busy {
		return false
	}
	for _, p := range r.Playback {
		if !p.Started.IsZero() && p.Ended.IsZero() {
			return false
		}
	}
	last := r.LastInteraction
	if last.Before(r.Started) {
		last = r.Started
	}
	return !now.Before(last.Add(WarmupMinGap)) && now.Sub(r.LastEvent) >= 60*time.Second
}

func (e *Engine) cancelWarmups(r *room, reason, except string) {
	for _, t := range r.Tasks {
		if t.Work.Kind == "guide" && (except == "" || t.Decision != except) && t.State != "playing" && !terminal(t.State) {
			e.cancel(r, t, reason)
		}
	}
}

// False means duplicate or out-of-order evidence. A failed unstarted request
// does not count as playback. Completed streams remain authoritative without a
// start receipt; queue/session fences are checked by the production adapter.
func (e *Engine) recordPlayback(r *room, decision, status string, at time.Time) bool {
	if r.Playback == nil {
		r.Playback = map[string]playbackActivity{}
	}
	p := r.Playback[decision]
	if !p.Ended.IsZero() {
		return false
	}
	switch status {
	case "PLAYING", "PROGRESS":
		if !p.Started.IsZero() {
			return false
		}
		p.Started = at
		e.cancelWarmups(r, "another_interaction_started", decision)
	case "COMPLETED":
		p.Ended = at
	case "FAILED":
		if !p.Started.IsZero() {
			p.Ended = at
		} else {
			return true
		}
	default:
		return false
	}
	if p.Ended.After(r.LastInteraction) {
		r.LastInteraction = p.Ended
	}
	r.Playback[decision] = p
	for key, v := range r.Playback {
		if !v.Ended.IsZero() && at.Sub(v.Ended) > 15*time.Minute {
			delete(r.Playback, key)
		}
	}
	return true
}
