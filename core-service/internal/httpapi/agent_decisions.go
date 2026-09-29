package httpapi

import (
	"context"
	"net/http"
	"strings"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
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
	manualOrigin := strings.ToLower(strings.TrimSpace(queue[0].ManualOrigin))
	if manualOrigin == "test_simulation" || manualOrigin == "agent_input_preview" {
		item, claimed := s.agentDecisions.ClaimNext(roomID)
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
		item, claimed := s.agentDecisions.ClaimNext(roomID)
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
	plannedSwitchMS := 0
	currentMainline := ""
	resumeMainline := ""
	resumeSegmentID := ""
	if queue[0].ManualAction != "quick" {
		currentMS := program.CurrentMS
		if currentMS <= 0 {
			currentMS = program.Task.StartMS
		}
		cutMS, exists := plannedRoomProgramSafeCut(program, currentMS)
		if !exists {
			writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "reason": "waiting_safe_point", "item": queue[0]})
			return
		}
		plannedSwitchMS = cutMS
		currentMainline, resumeMainline, resumeSegmentID = roomProgramCutContext(program, cutMS)
	}
	item, claimed := s.agentDecisions.ClaimNext(roomID)
	writeJSON(w, http.StatusOK, map[string]any{
		"claimed":           claimed,
		"item":              item,
		"switch_at_ms":      plannedSwitchMS,
		"current_mainline":  currentMainline,
		"resume_mainline":   resumeMainline,
		"resume_segment_id": resumeSegmentID,
	})
}

func roomProgramCutContext(program audioout.RoomProgramSnapshot, cutMS int) (string, string, string) {
	for _, point := range program.SafePoints {
		if point.CutMS != cutMS {
			continue
		}
		return strings.TrimSpace(point.LeftPreview), strings.TrimSpace(point.NextPreview), strings.TrimSpace(point.SentenceID)
	}
	for index, segment := range program.Timeline {
		if segment.EndMS != cutMS {
			continue
		}
		current := strings.TrimSpace(segment.Text)
		if index+1 >= len(program.Timeline) {
			return current, "", ""
		}
		next := program.Timeline[index+1]
		return current, strings.TrimSpace(next.Text), strings.TrimSpace(next.SegmentID)
	}
	return "", "", ""
}

func speechSnapshotBusy(snapshot speechruntime.Snapshot) bool {
	// 同一直播间的插播 TTS 必须严格串行：上一条完整结束前，任何下一条
	// （包括“抢答”）都只能继续排队，不能覆盖或截断正在播放的 TTS。
	return snapshot.Interrupt.Status == speechruntime.StatusPlaying || snapshot.Interrupt.Status == speechruntime.StatusReady
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
	item, released := s.agentDecisions.Release(roomID, id)
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
