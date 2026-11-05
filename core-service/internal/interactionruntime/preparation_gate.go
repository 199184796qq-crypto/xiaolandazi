package interactionruntime

import (
	"math"
	"time"
	"unicode/utf8"
)

// EstimateDurationMS is shared by initial selection and the final-text HTTP
// preflight. The latter includes the resolved address and current voice rate.
// Only PCM receipts, never this estimate, debit played-duration history.
func EstimateDurationMS(text string, rate float64) int {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0.5 || rate > 2 {
		rate = 1
	}
	return max(3000, int(math.Ceil(float64(utf8.RuneCountInString(text)*230)/rate)))
}

func taskPlayedSeconds(t *Task, at time.Time) float64 {
	if t.PCMKnown {
		return float64(t.PCMPlayedMS) / 1000
	}
	// Compatibility with legacy producers/tests without PCM counters. New Core
	// producers carry explicit counters; estimates never fill a missing counter.
	return max(0, at.Sub(t.Started).Seconds())
}

func playbackBudgetLimit(r *room) int {
	if r.Density == "normal" {
		return 30000
	}
	if r.Density == "fast" {
		return 20000
	}
	return 40000
}

func playbackBudgetRemaining(r *room, at time.Time) int {
	budget := playbackBudgetLimit(r)
	used := 0.0
	for _, v := range r.Played {
		used += max(0, min(v.Seconds, v.At.Sub(at.Add(-time.Minute)).Seconds()))
	}
	return max(0, budget-int(math.Ceil(used*1000-0.000001)))
}

// One indivisible speech may borrow beyond a minute's quota only when that
// quota is entirely available. Its full actual PCM duration remains charged;
// subsequent speeches wait for the rolling window instead of borrowing again.
func durationBudgetAllowed(r *room, duration int, at time.Time) bool {
	return playbackBudgetRemaining(r, at) >= min(max(1, duration), playbackBudgetLimit(r))
}

func durationBudgetRetryMS(r *room, duration int, at time.Time) int {
	if durationBudgetAllowed(r, duration, at) {
		return 0
	}
	lo, hi := 1, 60000
	for lo < hi {
		mid := lo + (hi-lo)/2
		if durationBudgetAllowed(r, duration, at.Add(time.Duration(mid)*time.Millisecond)) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
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
	reason, remaining, _ := e.PreviewPreparationRetry(s, decision, duration, at)
	return reason, remaining
}

func (e *Engine) PreviewPreparationRetry(s Scope, decision string, duration int, at time.Time) (string, int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.rooms[s.Room]
	if r == nil {
		return "", 40000, 0
	}
	if r.Scope != s {
		return "run_changed", 0, 5000
	}
	remaining := playbackBudgetRemaining(r, at)
	for _, t := range r.Tasks {
		if t.State == "playing" {
			return "speech_busy", remaining, 5000
		}
	}
	for _, t := range r.Tasks {
		if decision != "" && t.Decision == decision {
			if t.State != "selected" || !at.Before(t.Work.Expires) {
				return "original_content_expired", remaining, 0
			}
			if !enabled(r, t.Work.Kind) {
				return "interaction_disabled", remaining, 0
			}
			if t.Work.Kind == "urgent" {
				return "", remaining, 0
			}
		}
	}
	if duration <= 0 {
		duration = 3000
	}
	if !durationBudgetAllowed(r, duration, at) {
		return "audio_duration_budget", remaining, durationBudgetRetryMS(r, duration, at)
	}
	return "", remaining, 0
}
