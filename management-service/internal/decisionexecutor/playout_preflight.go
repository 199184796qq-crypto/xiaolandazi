package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechmission"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// This endpoint is deliberately fail-closed. A missing/malformed Core response
// cannot authorize a new paid model/TTS operation.
func (w *Worker) playoutPreflight(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, text, stage string, rates ...float64) (bool, error) {
	rate := 1.0
	if len(rates) > 0 {
		rate = rates[0]
	}
	until := item.ExpiresAt
	if !item.CompletionUntil.IsZero() {
		until = item.CompletionUntil
	}
	if item.AudioPrepared && !item.PreparedUntil.IsZero() {
		until = item.PreparedUntil
	}
	if isTimeReport(item) && !item.TimeReportValidUntil.IsZero() && item.TimeReportValidUntil.Before(until) {
		until = item.TimeReportValidUntil
	}
	// Core owns the common estimator. Send final spoken text, including the
	// late address, and the exact rate that will be sent to TTS.
	request := map[string]any{"duration_ms": 3000, "until": until, "stage": stage, "text": text, "rate": rate}
	if stage == "tts" {
		request["preparation_ms"] = w.estimateTTPreparation(text).Milliseconds()
	}
	resp, err := w.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodPost, fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/preflight", session.RoomID, item.ID), tenantQuery(session.TenantID), request)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		return false, fmt.Errorf("playout preflight http %d", resp.StatusCode)
	}
	var result struct {
		Allowed      *bool  `json:"allowed"`
		Reason       string `json:"reason"`
		DurationMS   int    `json:"duration_ms"`
		RemainingMS  int    `json:"remaining_ms"`
		RetryAfterMS int    `json:"retry_after_ms"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result) != nil || result.Allowed == nil {
		return false, fmt.Errorf("invalid playout preflight response")
	}
	if *result.Allowed {
		return false, nil
	}
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = "preflight_unavailable"
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.release(releaseCtx, session, item.ID, "preflight:"+reason, strconv.Itoa(result.RetryAfterMS)); err != nil {
		return false, err
	}
	if reason == "welcome_tts_time_insufficient" || reason == "welcome_names_deadline_missing" {
		w.transitionMission(&item, speechmission.StateFailed, reason, "欢迎剩余有效时间不足，未购买TTS，等待重新收集最近观众")
	} else {
		w.transitionMission(&item, speechmission.StateCreated, "preparation_yield", reason)
	}
	log.Printf("interaction preparation deferred tenant=%d room=%d decision=%s stage=%s reason=%s duration_ms=%d remaining_ms=%d retry_ms=%d", session.TenantID, session.RoomID, item.ID, stage, reason, result.DurationMS, result.RemainingMS, result.RetryAfterMS)
	return true, nil
}

// A budget/cut change between model and TTS must not buy the same body again.
// This bounded disposable draft retains its original deadline and full content
// identity. Reports use their existing timestamp-aware draft cache instead.
func (w *Worker) savePreflightDraft(session model.LiveRuntimeSession, item decisionItem, text, scope, memory string) {
	if isTimeReport(item) || strings.TrimSpace(text) == "" || item.ExpiresAt.IsZero() || !w.now().Before(item.ExpiresAt) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.preflightDrafts == nil {
		w.preflightDrafts = map[preparedInteractionKey]preparedInteraction{}
	}
	for key, v := range w.preflightDrafts {
		if !w.now().Before(v.ExpiresAt) {
			delete(w.preflightDrafts, key)
		}
	}
	if len(w.preflightDrafts) >= maxPreparedInteractions {
		var oldest preparedInteractionKey
		var until time.Time
		for key, v := range w.preflightDrafts {
			if until.IsZero() || v.ExpiresAt.Before(until) {
				oldest, until = key, v.ExpiresAt
			}
		}
		delete(w.preflightDrafts, oldest)
	}
	w.preflightDrafts[preparationKey(session, item)] = preparedInteraction{Item: item, Text: text, Scope: scope, Memory: memory, ExpiresAt: item.ExpiresAt}
}
func (w *Worker) preflightDraft(session model.LiveRuntimeSession, item *decisionItem, scope, memory string) (string, bool) {
	if isTimeReport(*item) {
		return "", false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	key := preparationKey(session, *item)
	v, ok := w.preflightDrafts[key]
	delete(w.preflightDrafts, key)
	if !ok || !w.now().Before(v.ExpiresAt) || v.Scope != scope || v.Memory != memory {
		return "", false
	}
	current, resume, segment := item.CurrentMainline, item.ResumeMainline, item.ResumeSegmentID
	*item = v.Item
	item.CurrentMainline, item.ResumeMainline, item.ResumeSegmentID = current, resume, segment
	return v.Text, true
}
