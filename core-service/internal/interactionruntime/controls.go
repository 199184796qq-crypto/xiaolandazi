package interactionruntime

import "time"

func configuredInterval(r *room, kind string) time.Duration {
	if r.Policy == nil {
		return 0
	}
	if kind == "engagement" {
		kind = "semantic"
	}
	return r.Policy.Minimum[kind]
}
func (e *Engine) clearDisabled(r *room, old *Policy) {
	for _, t := range r.Tasks {
		on := controlsEnabled(r, t.Work.Kind)
		if t.State == "collecting" || t.State == "generating" {
			on = classificationControlsEnabled(r, t.Work.Kind)
		}
		expired := controlGenerationExpired(r, t, t.Work.Kind)
		if t.Work.Kind == "semantic" && (t.State == "collecting" || t.State == "generating") {
			expired = expired || controlGenerationExpired(r, t, "urgent") || (controlChanged(old, r.Policy, "semantic") && !controlsEnabled(r, "semantic")) || (controlChanged(old, r.Policy, "urgent") && !controlsEnabled(r, "urgent"))
		}
		if (!on || expired) && !terminal(t.State) && t.State != "playing" {
			e.cancel(r, t, "kind_disabled")
		}
	}
	if controlChanged(old, r.Policy, "welcome") && r.Welcome != nil {
		r.Welcome.Entries = map[string]welcomeEntry{}
		r.Welcome.Task = ""
		r.Welcome.Pending = false
		r.Welcome.RetryAt = time.Time{}
	}
	for k, lane := range r.Behaviors {
		if controlChanged(old, r.Policy, k) {
			lane.Entries = map[string]behaviorEntry{}
			lane.Pending = false
			lane.Retry = false
			lane.Task = ""
			lane.RetryAt = time.Time{}
		}
	}
	if !controlsEnabled(r, "dialogue") {
		r.Dialogues = map[string]*dialogue{}
	}
	if !controlsEnabled(r, "engagement") {
		r.Command = nil
		r.Participation = nil
	}
}
func classificationEnabled(r *room, kind string) bool {
	if kind == "semantic" {
		return enabled(r, kind) || enabled(r, "urgent")
	}
	return enabled(r, kind)
}
func minimumReady(r *room, kind string, now time.Time) bool {
	last := r.Starts[kind]
	if kind == "semantic" && r.Starts["engagement"].After(last) {
		last = r.Starts["engagement"]
	}
	if kind == "engagement" && r.Starts["semantic"].After(last) {
		last = r.Starts["semantic"]
	}
	return last.IsZero() || !now.Before(last.Add(configuredInterval(r, kind)))
}

func controlsEnabled(r *room, kind string) bool { return r.Policy == nil || !r.Policy.Disabled[kind] }
func classificationControlsEnabled(r *room, kind string) bool {
	if kind == "semantic" {
		return controlsEnabled(r, kind) || controlsEnabled(r, "urgent")
	}
	return controlsEnabled(r, kind)
}
func controlChanged(a, b *Policy, kind string) bool {
	before := a != nil && a.Disabled[kind]
	after := b != nil && b.Disabled[kind]
	return before != after
}

func controlGenerationExpired(r *room, t *Task, kind string) bool {
	return r.Policy != nil && r.Policy.DisabledAt[kind] > t.ControlsRevision
}

// HasPreparingWork avoids submitting a batch whose whole generation was revoked
// while waiting for the goroutine/provider slot. An accepted vendor request may
// still finish, but Accept cannot revive its cancelled tasks.
func (e *Engine) HasPreparingWork(works []Work, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, w := range works {
		if r := e.rooms[w.Scope.Room]; r != nil && r.Scope == w.Scope {
			if t := r.Tasks[w.ID]; t != nil && t.State == "generating" && t.Work.Revision == w.Revision && now.Before(t.Work.Expires) {
				return true
			}
		}
	}
	return false
}
