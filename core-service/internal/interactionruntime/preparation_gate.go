package interactionruntime

import "time"

func playbackBudgetRemaining(r *room, at time.Time) int {
	budget := 40000
	if r.Density == "normal" {
		budget = 30000
	}
	if r.Density == "fast" {
		budget = 20000
	}
	used := 0.0
	for _, v := range r.Played {
		used += max(0, min(v.Seconds, v.At.Sub(at.Add(-time.Minute)).Seconds()))
	}
	return max(0, budget-int(used*1000))
}
func preparationAllowed(r *room, kind string, at time.Time) bool {
	if r.PreparationReason != "" && !(r.PreparationReason == "audio_duration_budget" && kind == "urgent") {
		return false
	}
	if kind != "urgent" && playbackBudgetRemaining(r, at) < 2000 {
		return false
	}
	return !semanticKind(kind) || semanticQuotaAvailable(r, at, "")
}
func (e *Engine) SetPreparationBlocked(s Scope, reason string, at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.room(s, at).PreparationReason = reason
}

// PreviewPreparation never consumes playback history or renews a deadline.
func (e *Engine) PreviewPreparation(s Scope, decision string, duration int, at time.Time) (string, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[s.Room]
	if r == nil {
		return "", 40000
	}
	if r.Scope != s {
		return "run_changed", 0
	}
	remaining := playbackBudgetRemaining(r, at)
	for _, t := range r.Tasks {
		if t.State == "playing" {
			return "speech_busy", remaining
		}
	}
	for _, t := range r.Tasks {
		if decision != "" && t.Decision == decision {
			if t.State != "selected" || !at.Before(t.Work.Expires) {
				return "original_content_expired", remaining
			}
			if !enabled(r, t.Work.Kind) {
				return "interaction_disabled", remaining
			}
			if t.Work.Kind == "urgent" {
				return "", remaining
			}
		}
	}
	if duration <= 0 {
		duration = 3000
	}
	if duration > remaining {
		return "audio_duration_budget", remaining
	}
	return "", remaining
}
