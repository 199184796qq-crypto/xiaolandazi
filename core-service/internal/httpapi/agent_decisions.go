package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/speechruntime"
)

func (s *Server) SetAgentDecisionQueue(queue *agentdecision.Queue) {
	if queue == nil {
		queue = agentdecision.New()
	}
	s.agentDecisions = queue
}

func (s *Server) getRoomAgentDecisions(w http.ResponseWriter, r *http.Request) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	writeJSON(w, http.StatusOK, s.agentDecisions.Snapshot(roomID))
}

func (s *Server) enqueueRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	s.enqueueAgentDecision(w, r, agentdecision.SourceAgent)
}

func (s *Server) enqueueRoomManualDecision(w http.ResponseWriter, r *http.Request) {
	s.enqueueAgentDecision(w, r, agentdecision.SourceManual)
}

func (s *Server) enqueueAgentDecision(w http.ResponseWriter, r *http.Request, source agentdecision.Source) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	room, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(room.Status), "live") {
		writeError(w, http.StatusConflict, "直播间当前未开播")
		return
	}
	if s.events != nil {
		stats, statsErr := s.events.GetSessionStats(r.Context(), roomID)
		if statsErr != nil {
			writeError(w, http.StatusInternalServerError, "read session state failed")
			return
		}
		if stats.ResumePending {
			writeError(w, http.StatusConflict, "请先选择续接上一场或作为新直播")
			return
		}
	}
	if s.agentWork == nil || !s.agentWork.IsWorking(roomID) {
		writeError(w, http.StatusConflict, "直播搭子尚未工作")
		return
	}
	var input agentdecision.Candidate
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.Source = source
	if strings.TrimSpace(input.Question) == "" && strings.TrimSpace(input.Topic) == "" {
		writeError(w, http.StatusBadRequest, "question or topic is required")
		return
	}
	manualOrigin := strings.ToLower(strings.TrimSpace(input.ManualOrigin))
	if !observationExempt(manualOrigin) && s.inOpeningObservation(r.Context(), roomID, tenantID) {
		writeError(w, http.StatusConflict, "实际起播后的开场观察窗尚未结束")
		return
	}
	if source == agentdecision.SourceManual &&
		manualOrigin != "agent_input" &&
		manualOrigin != "agent_input_preview" &&
		manualOrigin != "test_simulation" &&
		!s.manualCandidateTTSEligible(r.Context(), tenantID, roomID, input) {
		writeError(w, http.StatusConflict, "这个问题已不在当前直播问题池中")
		return
	}
	result := s.agentDecisions.Enqueue(roomID, input)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) claimRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	room, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(room.Status), "live") {
		writeError(w, http.StatusConflict, "直播间当前未开播")
		return
	}
	if s.events != nil {
		stats, statsErr := s.events.GetSessionStats(r.Context(), roomID)
		if statsErr != nil {
			writeError(w, http.StatusInternalServerError, "read session state failed")
			return
		}
		if stats.ResumePending {
			writeError(w, http.StatusConflict, "请先选择直播续接方式")
			return
		}
	}
	if s.agentWork == nil || !s.agentWork.IsWorking(roomID) {
		writeError(w, http.StatusConflict, "直播搭子尚未工作")
		return
	}

	queue := s.agentDecisions.Snapshot(roomID).Queue
	if len(queue) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "empty"})
		return
	}
	// Waiting deadlines cannot evict an already claimed/bound playback. Let the
	// queue select only claimable work before validating its admission deadline.
	head := s.agentDecisions.ClaimableHead(roomID)
	if head == nil {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "deferred"})
		return
	}
	queue = []agentdecision.Item{*head}
	if !s.validInteractionV2Item(*head, roomID, tenantID, time.Now()) {
		s.agentDecisions.FailUnstarted(roomID, head.ID, "interaction_v2_expired")
		writeJSON(w, 200, map[string]any{"claimed": false, "reason": "interaction_v2_expired"})
		return
	}
	manualOrigin := strings.ToLower(strings.TrimSpace(queue[0].ManualOrigin))
	if !observationExempt(manualOrigin) && s.inOpeningObservation(r.Context(), roomID, tenantID) {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "opening_observation"})
		return
	}
	if manualOrigin == "test_simulation" || manualOrigin == "agent_input_preview" {
		item, claimed := s.agentDecisions.ClaimExpected(roomID, queue[0].ID)
		s.refreshClaimInteractionContext(roomID, item)
		writeJSON(w, http.StatusOK, map[string]any{
			"claimed": claimed,
			"item":    item,
		})
		return
	}
	if s.speechRuntime != nil {
		if speech, speechErr := s.speechRuntime.Snapshot(roomID); speechErr == nil && speechSnapshotBusy(speech) {
			writeJSON(w, http.StatusOK, map[string]any{
				"claimed": false,
				"reason":  "speech_busy",
				"item":    queue[0],
			})
			return
		}
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "audio_unavailable", "item": queue[0]})
		return
	}
	if s.agentWork.Mode(roomID) == agentwork.ModeControl {
		item, claimed := s.agentDecisions.ClaimExpected(roomID, queue[0].ID)
		s.refreshClaimInteractionContext(roomID, item)
		writeJSON(w, http.StatusOK, map[string]any{
			"claimed": claimed,
			"item":    item,
		})
		return
	}
	program, programErr := state.client.ProgramSnapshot(r.Context(), roomID)
	if programErr != nil || !program.Running || program.Task == nil {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "mainline_unavailable", "item": queue[0]})
		return
	}
	lowFlow := queue[0].InteractionDecision.UniqueChatters30s <= 2
	// Do not start an ordinary interaction on the last few seconds of mainline.
	// The old path could start a 13s answer while only 5s of mainline remained,
	// then exhaust the queue and strand the engine in interrupt.
	if queue[0].ManualAction != "quick" && !lowFlow && (program.WaitingForContent || program.BufferedMainlineMS < 15000) {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "mainline_low_buffer", "item": queue[0], "buffered_mainline_ms": program.BufferedMainlineMS})
		return
	}
	preparation := audioout.InteractionPreflightRequest{RoomID: roomID, TenantID: tenantID, PlanID: s.agentWork.Get(roomID).PlanID, Epoch: s.currentInteractionEpoch(roomID), DecisionID: head.ID, Until: head.ExpiresAt, DurationMS: 3000, Stage: "claim"}
	check := s.InteractionPreparationWindow(r.Context(), preparation)
	if check.Allowed && s.messages != nil {
		raw, _ := json.Marshal(preparation)
		if errs := s.messages.Publish(r.Context(), messagecaptain.Message{Kind: "interaction.v2.preflight", RoomID: roomID, RunID: preparation.Epoch, Payload: raw}); len(errs) > 0 {
			check.Allowed = false
			check.Reason = errs[0].Error()
			var blocked *audioout.InteractionPreparationError
			if errors.As(errs[0], &blocked) {
				check.RemainingMS, check.RetryAfterMS = blocked.RemainingMS, blocked.RetryAfterMS
			}
		}
	}
	if !check.Allowed {
		s.agentDecisions.YieldPreparation(roomID, head.ID, check.Reason, time.Duration(check.RetryAfterMS)*time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": check.Reason, "retry_after_ms": check.RetryAfterMS})
		return
	}
	plannedSwitchMS := 0
	currentMainline := ""
	resumeMainline := ""
	resumeSegmentID := ""
	var preparationPlans []interactionCutPlan
	// Text timelines are display-only. Claiming generation never waits for
	// a legacy text preparation window; dispatch rechecks physical PCM cuts.
	item, claimed := s.agentDecisions.ClaimExpected(roomID, queue[0].ID)
	if lowFlow && item != nil {
		// Low-flow rooms (including an empty room with only a greeting event) do
		// not wait for a semantic safe point once the answer audio is ready; the
		// current sentence may be cut and skipped. This applies to both answers
		// and named/batch greetings so a new viewer hears a reply immediately.
		item.ManualAction = "quick"
	}
	s.refreshClaimInteractionContext(roomID, item)
	writeJSON(w, http.StatusOK, map[string]any{
		"claimed":           claimed,
		"item":              item,
		"switch_at_ms":      plannedSwitchMS,
		"current_mainline":  currentMainline,
		"resume_mainline":   resumeMainline,
		"resume_segment_id": resumeSegmentID,
		"cut_plans":         preparationPlans,
	})
}

func speechSnapshotBusy(snapshot speechruntime.Snapshot) bool {
	// 同一直播间的插播 TTS 必须严格串行：上一条完整结束前，任何下一条
	// （包括“抢答”）都只能继续排队，不能覆盖或截断正在播放的 TTS。
	return snapshot.Interrupt.Status == speechruntime.StatusPlaying ||
		snapshot.Interrupt.Status == speechruntime.StatusReady ||
		snapshot.Interrupt.Status == speechruntime.StatusReturning
}

func (s *Server) manualCandidateTTSEligible(ctx context.Context, tenantID, roomID int64, input agentdecision.Candidate) bool {
	// Direct per-danmaku manual actions are allowed independently from semantic
	// question clustering. There is no artificial 30-minute answer cutoff: if
	// the event is still present in Core's retained event stream it can be used.
	if input.EventID > 0 && s.events != nil {
		matchesDirectChat := func(items []model.RoomEvent) bool {
			for _, event := range items {
				if event.ID != input.EventID {
					continue
				}
				eventType := strings.ToLower(strings.TrimSpace(event.EventType))
				return eventType == "chat" || eventType == "comment"
			}
			return false
		}

		// The hot recent channel is intentionally small and can be displaced by
		// high-frequency member/entry events. Manual answerability follows the
		// longer-lived important chat channel so a danmaku that is still visible
		// in the current session does not become unanswerable just because it fell
		// out of the recent 500-event window.
		if items, err := s.events.ListRecent(ctx, &tenantID, roomID, 5000); err == nil && matchesDirectChat(items) {
			return true
		}
		if items, err := s.events.ListImportant(ctx, &tenantID, roomID, "chat", 0, 20000); err == nil && matchesDirectChat(items) {
			stats, statsErr := s.events.GetSessionStats(ctx, roomID)
			if statsErr != nil || stats.StartedAt.IsZero() {
				return true
			}
			for _, event := range items {
				if event.ID == input.EventID {
					return !event.OccurredAt.Before(stats.StartedAt)
				}
			}
		}
	}
	if s.brain == nil {
		return false
	}
	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		return false
	}
	topic := strings.TrimSpace(input.Topic)
	question := strings.TrimSpace(input.Question)
	for _, bucket := range view.Intelligence.TopTopics {
		if topic != "" && bucket.Topic != topic {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(input.ManualOrigin), "question_cluster") {
			return len(bucket.Questions) > 0
		}
		if input.EventID <= 0 && question == "" {
			return len(bucket.Questions) > 0
		}
		for _, candidate := range bucket.Questions {
			if input.EventID > 0 && candidate.EventID == input.EventID {
				return true
			}
			if input.EventID <= 0 && question != "" && strings.EqualFold(strings.TrimSpace(candidate.Content), question) {
				return true
			}
		}
	}
	return false
}

func (s *Server) releaseRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("decisionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	var item *agentdecision.Item
	var released bool
	if r.URL.Query().Get("reason") == "preflight" {
		retryMS, _ := strconv.Atoi(r.URL.Query().Get("retry_after_ms"))
		item, released = s.agentDecisions.YieldPreparation(roomID, id, r.URL.Query().Get("block_reason"), time.Duration(max(0, min(60000, retryMS)))*time.Millisecond)
	} else if r.URL.Query().Get("reason") == "cut_reschedule" {
		deadline, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("prepared_until"))
		item, released = s.agentDecisions.RescheduleCut(roomID, id, deadline)
	} else {
		item, released = s.agentDecisions.Release(roomID, id)
	}
	if !released {
		writeError(w, http.StatusNotFound, "decision is no longer claimed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (s *Server) removeRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("decisionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	item, removed := s.agentDecisions.Remove(roomID, id)
	if !removed {
		writeError(w, http.StatusNotFound, "decision is no longer in queue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (s *Server) completeRoomAgentSimulation(w http.ResponseWriter, r *http.Request) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("decisionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	var input struct {
		Reply            string `json:"reply"`
		ExecutionMode    string `json:"execution_mode"`
		PlanName         string `json:"plan_name"`
		UserLayerVersion uint64 `json:"user_layer_version"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid simulation result")
		return
	}
	result, completed := s.agentDecisions.CompleteSimulation(roomID, id, input.Reply, input.ExecutionMode, input.PlanName, input.UserLayerVersion)
	if !completed {
		writeError(w, http.StatusNotFound, "simulation decision is no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (s *Server) completeRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("decisionID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "decision id is required")
		return
	}
	if item, exists := s.agentDecisions.Get(roomID, id); exists && item.MissionKind == "time_report" {
		writeError(w, http.StatusConflict, "报时配额只能由实际播报完成链路核销")
		return
	}
	recent, completed := s.agentDecisions.Complete(roomID, id)
	if !completed {
		writeError(w, http.StatusNotFound, "decision is no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"recently_answered": recent,
	})
}
