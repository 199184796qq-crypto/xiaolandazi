package decisionexecutor

import (
	"encoding/json"
	"errors"
	"log"
	"sort"
	"time"
	"unicode/utf8"
)

var errWelcomeTTSTimeInsufficient = errors.New("欢迎剩余有效时间不足以完成TTS，等待最近观众重新调度")

// A conservative cold estimate grows with spoken input length. A bounded recent
// p95 can only raise it; cached/instant responses never erase the cold floor.
func (w *Worker) estimateTTPreparation(text string) time.Duration {
	estimate := max(5*time.Second, time.Second+time.Duration(utf8.RuneCountInString(text))*40*time.Millisecond)
	w.mu.Lock()
	samples := append([]time.Duration(nil), w.ttsPreparationSamples...)
	w.mu.Unlock()
	if len(samples) > 0 {
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		p95 := samples[(len(samples)*95+99)/100-1]
		estimate = max(estimate, p95+p95/4)
	}
	return min(35*time.Second, estimate)
}

func (w *Worker) observeTTPreparation(elapsed time.Duration) {
	if elapsed < 250*time.Millisecond {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ttsPreparationSamples = append(w.ttsPreparationSamples, min(35*time.Second, elapsed))
	if len(w.ttsPreparationSamples) > 32 {
		w.ttsPreparationSamples = w.ttsPreparationSamples[len(w.ttsPreparationSamples)-32:]
	}
}

// Runs at the actual purchase boundary, again after preparation/voice work and
// on SSML fallback. The original names deadline cannot be refreshed by retries.
func (w *Worker) welcomeTTSPurchaseDeadline(item decisionItem, text string) (time.Time, error) {
	welcome, ready := welcomeV2(item)
	if !welcome || ready {
		return time.Time{}, nil
	}
	var metadata struct {
		NamesUntil time.Time `json:"welcome_names_expires_at"`
	}
	if json.Unmarshal(item.PurposeKnowledge, &metadata) != nil || metadata.NamesUntil.IsZero() {
		return time.Time{}, errWelcomeTTSTimeInsufficient
	}
	until := metadata.NamesUntil
	if !item.ExpiresAt.IsZero() && item.ExpiresAt.Before(until) {
		until = item.ExpiresAt
	}
	now, estimate := w.now(), w.estimateTTPreparation(text)
	if !now.Add(estimate + time.Second).Before(until) {
		log.Printf("welcome tts purchase skipped decision=%s remaining_ms=%d preparation_ms=%d admission_margin_ms=1000 reason=welcome_tts_time_insufficient", item.ID, until.Sub(now).Milliseconds(), estimate.Milliseconds())
		return time.Time{}, errWelcomeTTSTimeInsufficient
	}
	return until.Add(-time.Second), nil
}
