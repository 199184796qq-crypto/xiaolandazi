package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"net/http"
	"strings"
	"time"
)

// InteractionPreparationWindow uses scanned PCM only. It never arms a cut,
// downloads audio, buys a model/TTS call or reserves played-duration history.
func (s *Server) InteractionPreparationWindow(ctx context.Context, req audioout.InteractionPreflightRequest) audioout.InteractionPreflightResult {
	now := time.Now()
	r := audioout.InteractionPreflightResult{CheckedAt: now, RetryAfterMS: 5000}
	deny := func(reason string) audioout.InteractionPreflightResult { r.Reason = reason; return r }
	if s.agentWork == nil || !s.agentWork.IsWorking(req.RoomID) {
		return deny("run_changed")
	}
	run := s.agentWork.Get(req.RoomID)
	if (req.PlanID > 0 && run.PlanID != req.PlanID) || (req.Epoch != "" && s.currentInteractionEpoch(req.RoomID) != req.Epoch) {
		return deny("run_changed")
	}
	if !req.Until.IsZero() && !now.Add(500*time.Millisecond).Before(req.Until) {
		return deny("original_content_expired")
	}
	if s.speechRuntime != nil {
		if speech, e := s.speechRuntime.Snapshot(req.RoomID); e == nil && speechSnapshotBusy(speech) {
			return deny("speech_busy")
		}
	}
	if s.agentWork.Mode(req.RoomID) == agentwork.ModeControl {
		r.Allowed = true
		return r
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		return deny("audio_unavailable")
	}
	p, e := state.client.ProgramSnapshot(ctx, req.RoomID)
	if e != nil || !p.Running || p.ManualPaused || p.Suspended || p.Task == nil {
		return deny("mainline_unavailable")
	}
	r.ProgramID, r.TrackID = p.ProgramID, p.TrackID
	if !p.AcousticReady {
		return deny("acoustic_scan_pending")
	}
	maxLead := 15000
	if !req.Until.IsZero() {
		maxLead = min(maxLead, int(req.Until.Sub(now).Milliseconds())-500)
	}
	cut, ok := chooseAcousticInteractionCut(p.AcousticCuts, p.CurrentMS, maxLead)
	if !ok {
		return deny("no_pcm_cut")
	}
	r.CutMS = cut
	r.Allowed = true
	return r
}

func (s *Server) preflightRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	room, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenant, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenant, room); err != nil {
		writeError(w, 404, "room not found")
		return
	}
	var req audioout.InteractionPreflightRequest
	if readJSON(w, r, &req) != nil {
		writeError(w, 400, "invalid preflight")
		return
	}
	item, ok := s.agentDecisions.Get(room, strings.TrimSpace(r.PathValue("decisionID")))
	if !ok || item.Status != "CLAIMED" || item.PlaybackTaskID != "" {
		writeError(w, 409, "decision_changed")
		return
	}
	req.RoomID, req.TenantID, req.DecisionID = room, tenant, item.ID
	req.PlanID = s.agentWork.Get(room).PlanID
	req.Epoch = s.currentInteractionEpoch(room)
	if !s.validInteractionV2Item(*item, room, tenant, time.Now()) {
		writeJSON(w, 200, audioout.InteractionPreflightResult{Reason: "original_content_expired", RetryAfterMS: 5000})
		return
	}
	deadline := item.ExpiresAt
	if !item.CompletionUntil.IsZero() {
		deadline = item.CompletionUntil
	}
	if item.AudioPrepared && !item.PreparedUntil.IsZero() {
		deadline = item.PreparedUntil
	}
	if !req.Until.IsZero() && req.Until.Before(deadline) {
		deadline = req.Until
	}
	req.Until = deadline
	result := s.InteractionPreparationWindow(r.Context(), req)
	if result.Allowed && s.messages != nil {
		raw, _ := json.Marshal(req)
		failures := s.messages.Publish(r.Context(), messagecaptain.Message{Kind: "interaction.v2.preflight", RoomID: room, RunID: req.Epoch, Payload: raw})
		if len(failures) > 0 {
			result.Allowed = false
			result.Reason = failures[0].Error()
		}
	}
	writeJSON(w, 200, result)
}
