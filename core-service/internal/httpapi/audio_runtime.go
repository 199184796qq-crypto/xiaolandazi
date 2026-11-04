package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
	"livecompanion/core/internal/strategycenter"
)

type roomAudioInteractionInput struct {
	Timeline               []audioout.ProgramTimelineSegment `json:"timeline,omitempty"`
	TimeReportValidUntil   time.Time                         `json:"time_report_valid_until,omitempty"`
	AllowConsecutiveRepeat bool                              `json:"allow_consecutive_repeat,omitempty"`
	DeferredReturn         bool                              `json:"deferred_return,omitempty"`
	PreparationCutPlans    json.RawMessage                   `json:"preparation_cut_plans,omitempty"`
	SelectedContentPlan    json.RawMessage                   `json:"selected_content_plan,omitempty"`
	ContentGoal            string                            `json:"content_goal,omitempty"`
	ContentAngle           string                            `json:"content_angle,omitempty"`
	DecisionID             string                            `json:"decision_id"`
	MissionID              string                            `json:"mission_id,omitempty"`
	MissionKind            string                            `json:"mission_kind,omitempty"`
	SessionID              string                            `json:"session_id,omitempty"`
	Action                 string                            `json:"action"`
	AudioURL               string                            `json:"audio_url"`
	Question               string                            `json:"question,omitempty"`
	ReplyText              string                            `json:"reply_text,omitempty"`
	Topic                  string                            `json:"topic,omitempty"`
	InterruptStrategy      string                            `json:"interrupt_strategy,omitempty"`
	ResumeStrategy         string                            `json:"resume_strategy,omitempty"`
	SkipCurrentSentence    bool                              `json:"skip_current_sentence,omitempty"`
	BridgeText             string                            `json:"bridge_text,omitempty"`
	HumanizationStrategy   string                            `json:"humanization_strategy,omitempty"`
	HumanizationKind       string                            `json:"humanization_kind,omitempty"`
	HumanizationApplied    bool                              `json:"humanization_applied,omitempty"`
	SwitchAtMS             int                               `json:"switch_at_ms,omitempty"`
}

func estimatedInteractionDurationMS(text string) int {
	runes := utf8.RuneCountInString(strings.TrimSpace(text))
	if runes <= 0 {
		return 4000
	}
	ms := runes * 230
	if ms < 3000 {
		ms = 3000
	}
	if ms > 60000 {
		ms = 60000
	}
	return ms
}

const maxInteractionSwitchLeadMS = 35000

func validInteractionSwitchLead(currentMS, switchMS int) bool {
	leadMS := switchMS - currentMS
	return leadMS > 0 && leadMS <= maxInteractionSwitchLeadMS
}

// Resume at the same physical PCM boundary, independent of subtitle content.
func roomInteractionCutOffsets(cutMS int) (switchAtMS, resumeOffsetMS *int) {
	switchMS, resumeMS := cutMS, cutMS
	return &switchMS, &resumeMS
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) scheduleRoomAudioInteractionCompletion(roomID int64, task audioout.SpeechTask, decisionID string, waitForMainline bool) {
	if roomID <= 0 || strings.TrimSpace(task.ID) == "" || task.DurationMS <= 0 {
		return
	}
	startedAt := task.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	// Receiver COMPLETED/FAILED events normally close the interaction.
	// This timer is only a fallback for a receiver that disappears, so leave
	// enough grace for buffering/startup delay before force-completing it.
	deadline := startedAt.Add(time.Duration(task.DurationMS+4000) * time.Millisecond)
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		s.expireRoomAudioInteraction(roomID, task, decisionID, startedAt, waitForMainline)
	})
}

func (s *Server) completeInteractionAfterMainlineResume(roomID int64, taskID, decisionID string) {
	if roomID <= 0 || strings.TrimSpace(taskID) == "" || strings.TrimSpace(decisionID) == "" {
		return
	}
	go func() {
		notice := time.NewTimer(30 * time.Second)
		defer notice.Stop()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		s.awaitInteractionMainlineResume(roomID, taskID, decisionID, notice.C, ticker.C)
	}()
}

// The warning marks a slow return, not the end of observation. Channels keep
// the actual physical transitions testable without waiting 30 real seconds.
func (s *Server) awaitInteractionMainlineResume(roomID int64, taskID, decisionID string, noticeC, pollC <-chan time.Time) {
	hardAfter := 60 * time.Second
	if s.speechRuntime != nil {
		if speech, err := s.speechRuntime.Snapshot(roomID); err == nil && speech.Interrupt.DurationMS > 0 {
			hardAfter = time.Duration(speech.Interrupt.DurationMS)*time.Millisecond + 12*time.Second + 500*time.Millisecond
		}
	}
	if hardAfter > 2*time.Minute {
		hardAfter = 2 * time.Minute
	}
	hardC := time.NewTimer(hardAfter)
	defer hardC.Stop()
	for {
		if s.speechRuntime == nil || s.roomAudio == nil {
			return
		}
		speech, err := s.speechRuntime.Snapshot(roomID)
		if err != nil || speech.Interrupt.SpeechTaskID != taskID || speech.Interrupt.DecisionID != decisionID {
			return
		}
		if speech.Interrupt.Status == speechruntime.StatusIdle || speech.Interrupt.Status == speechruntime.StatusCompleted || speech.Interrupt.Status == speechruntime.StatusFailed {
			return
		}
		engine := s.roomAudio.Snapshot(roomID)
		if speech.Interrupt.Status == speechruntime.StatusReturning && engine.Phase == roomaudio.PhaseMainline && engine.ActiveSource == roomaudio.SourceMainline && engine.MainlineCursorMS > 0 {
			if _, completed := s.speechRuntime.CompleteReturning(roomID, taskID, decisionID); !completed {
				return
			}
			decisionRecorded := false
			if s.agentDecisions != nil {
				_, decisionRecorded = s.agentDecisions.Complete(roomID, decisionID)
			}
			log.Printf("interaction lifecycle completed after mainline resume room=%d task=%s decision=%s decision_recorded=%t", roomID, taskID, decisionID, decisionRecorded)
			return
		}

		select {
		case <-noticeC:
			// A slow physical return is not a terminal failure. Keep observing
			// until actual mainline PCM resumes, or this task is stopped/replaced.
			// Releasing merely because 30 seconds elapsed could overlap audio.
			log.Printf("interaction lifecycle still returning after timeout room=%d task=%s decision=%s phase=%s source=%s continuing_watch=true", roomID, taskID, decisionID, engine.Phase, engine.ActiveSource)
			noticeC = nil
		case <-hardC.C:
			s.forceAbortRoomAudioInteraction(roomID, taskID, decisionID, "interaction_hard_deadline")
			return
		case _, open := <-pollC:
			if !open {
				return
			}
		}
	}
}

func (s *Server) dispatchRoomAudioInteraction(w http.ResponseWriter, r *http.Request) {
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

	var input roomAudioInteractionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Legacy return/skip flags are accepted only for wire compatibility.
	// No runtime mode may use them to restore text-timeline control.
	input.DeferredReturn, input.SkipCurrentSentence = false, false
	input.DecisionID = strings.TrimSpace(input.DecisionID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.AudioURL = strings.TrimSpace(input.AudioURL)
	input.Question = strings.TrimSpace(input.Question)
	input.ReplyText = strings.TrimSpace(input.ReplyText)
	input.MissionID = strings.TrimSpace(input.MissionID)
	input.MissionKind = strings.ToLower(strings.TrimSpace(input.MissionKind))
	if input.MissionID == "" {
		input.MissionID = input.DecisionID
	}
	input.Topic = strings.TrimSpace(input.Topic)
	input.InterruptStrategy = strings.ToLower(strings.TrimSpace(input.InterruptStrategy))
	input.ResumeStrategy = strings.ToUpper(strings.TrimSpace(input.ResumeStrategy))
	input.BridgeText = strings.TrimSpace(input.BridgeText)
	if input.SwitchAtMS < 0 {
		input.SwitchAtMS = 0
	}
	if input.DecisionID == "" || input.AudioURL == "" {
		writeError(w, http.StatusBadRequest, "decision_id and audio_url are required")
		return
	}
	if input.Action != "quick" {
		input.Action = "answer"
	}
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	decision, exists := s.agentDecisions.Get(roomID, input.DecisionID)
	if !exists || decision.Status != "CLAIMED" {
		writeError(w, http.StatusConflict, "待打断任务已被移除或不再执行")
		return
	}
	if !s.validInteractionV2Item(*decision, roomID, tenantID, time.Now()) {
		s.agentDecisions.Remove(roomID, decision.ID)
		writeError(w, http.StatusConflict, "互动原始事件已过期或场次已变化")
		return
	}
	if !s.validateInteractionDecision(w, roomID, input) {
		return
	}
	if !bindTimeReportDeadline(s.agentDecisions, roomID, *decision, input.TimeReportValidUntil, time.Now()) {
		writeError(w, http.StatusConflict, "报时话术时间已过期或缺少有效期，保留配额重新生成")
		return
	}
	if s.speechRuntime != nil {
		if speech, speechErr := s.speechRuntime.Snapshot(roomID); speechErr == nil && speechSnapshotBusy(speech) {
			writeError(w, http.StatusConflict, "上一条TTS尚未播放完成，请继续排队")
			return
		}
	}

	// Waiting TTL is checked when claiming. A claimed answer has its own
	// execution lease, so a welcome cannot expire midway through TTS/cut wait.
	dispatchCtx, dispatchCancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer dispatchCancel()
	if decision.MissionKind == "time_report" {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithDeadline(dispatchCtx, input.TimeReportValidUntil)
		defer cancel()
		if s.messages == nil {
			writeError(w, http.StatusServiceUnavailable, "报时时间有效期校验尚未配置")
			return
		}
	}
	controlMode := s.agentWork.Mode(roomID) == agentwork.ModeControl
	state := s.audioDevState()
	if !controlMode && (state == nil || state.client == nil || !state.client.Enabled()) {
		writeError(w, http.StatusServiceUnavailable, "主线播音调度尚未配置")
		return
	}
	if controlMode && s.audioHub == nil {
		writeError(w, http.StatusServiceUnavailable, "Core声音广播尚未配置")
		return
	}
	var program audioout.RoomProgramSnapshot
	var switchAtMS *int
	var resumeOffsetMS *int
	acousticMode := false
	resumeMode := "DIRECT"
	bridgeUsed := false
	resumeDedupTriggered := false
	resumeDuplicateScore := 0.0
	strategySignals := strategycenter.Signals{}
	if s.brain != nil {
		if view, viewErr := s.brain.Snapshot(roomID); viewErr == nil {
			strategySignals.Entries30s = view.Intelligence.Entries30s
			strategySignals.Likes30s = view.Intelligence.Likes30s
			strategySignals.Follows30s = view.Intelligence.Follows30s
			strategySignals.Heat = view.Intelligence.Heat
		}
	}
	interruptStrategy := input.InterruptStrategy
	interruptStrategySource := "management"
	knownInterrupt := false
	switch interruptStrategy {
	case "read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm":
		knownInterrupt = true
	}
	if !knownInterrupt || (s.strategyPolicies != nil && !s.strategyPolicies.Allowed(tenantID, "interrupt", interruptStrategy)) {
		interruptStrategy = ""
		interruptStrategySource = "core_fallback"
	}
	if interruptStrategy == "" && s.strategyPolicies != nil {
		selected := s.strategyPolicies.Pick(
			tenantID,
			"interrupt",
			[]string{"read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm"},
			strategySignals,
			input.DecisionID,
		)
		interruptStrategy = selected.Key
		log.Printf("core interrupt strategy room=%d decision=%s selected=%s source=%s requested=%s candidates=%v roll=%d/%d", roomID, input.DecisionID, selected.Key, interruptStrategySource, input.InterruptStrategy, selected.Candidates, selected.Roll, selected.Total)
	} else if interruptStrategy != "" {
		log.Printf("core interrupt strategy room=%d decision=%s selected=%s source=%s requested=%s", roomID, input.DecisionID, interruptStrategy, interruptStrategySource, input.InterruptStrategy)
	}
	if !controlMode {
		program, err = state.client.ProgramSnapshot(r.Context(), roomID)
		if err != nil || !program.Running || program.Task == nil {
			writeError(w, http.StatusConflict, "当前没有可插播的主线音频")
			return
		}
		currentMS := program.CurrentMS
		if currentMS <= 0 {
			currentMS = program.Task.StartMS
		}
		cutMS := 0
		exists := false
		// All mainline modes use physical PCM cuts. Sequential=false, legacy
		// safe_points, text timeline flags and caller offsets cannot reactivate
		// semantic switching or skipping.
		acousticMode = true
		maxLead := 30000
		deadline := decision.ExpiresAt
		if decision.AudioPrepared && !decision.PreparedUntil.IsZero() {
			deadline = decision.PreparedUntil
		} else if !decision.CompletionUntil.IsZero() {
			deadline = decision.CompletionUntil
		}
		if !deadline.IsZero() {
			maxLead = min(maxLead, int(time.Until(deadline).Milliseconds())-200)
		}
		cutMS, exists = chooseAcousticInteractionCut(program.AcousticCuts, currentMS, maxLead)
		resumeMode = "EXACT"
		if !exists {
			// Keep the existing management retry contract; its prepared audio
			// must survive a missing cut instead of becoming a dispatch failure.
			writeError(w, http.StatusConflict, "当前可用窗口内没有真实PCM声学切点，保留已合成声音继续排队")
			return
		}
		// Both quick and ordinary answers wait for the same physical sentence
		// ending. A later resume position can never authorize an immediate cut.
		switchAtMS, resumeOffsetMS = roomInteractionCutOffsets(cutMS)
		if switchAtMS != nil {
			if input.Action != "quick" && !validInteractionSwitchLead(currentMS, *switchAtMS) {
				log.Printf(
					"core audio interaction rejected unsafe switch lead room=%d action=%s current_ms=%d switch_ms=%d lead_ms=%d",
					roomID, input.Action, currentMS, *switchAtMS, *switchAtMS-currentMS,
				)
				writeError(w, http.StatusConflict, "安全句末切点已失效，继续排队")
				return
			}
			log.Printf("interaction cut selected room=%d decision=%s grade=%s track=%s cut_ms=%d", roomID, input.DecisionID, actualInteractionCutLevel(program, *switchAtMS), program.TrackID, *switchAtMS)
			log.Printf(
				"core audio interaction plan room=%d action=%s current_ms=%d switch_ms=%d lead_ms=%d",
				roomID, input.Action, currentMS, *switchAtMS, *switchAtMS-currentMS,
			)
		}
	}

	source := "manual_answer"
	label := "回答"
	if !controlMode && resumeMode == "DIRECT" {
		resumeMode = "SAFE"
	}
	if !bridgeUsed {
		input.BridgeText = ""
	}
	if input.Action == "quick" {
		source = "manual_quick"
		label = "抢答"
		if !acousticMode {
			resumeMode = "HARD"
		}
	}
	if s.speechRuntime != nil {
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:          speechruntime.TrackInterrupt,
			Status:         speechruntime.StatusReady,
			Text:           input.ReplyText,
			QuestionText:   input.Question,
			ReplyText:      input.ReplyText,
			ResumeStrategy: resumeMode,
			BridgeText:     input.BridgeText,
			BridgeUsed:     bridgeUsed,
			Source:         source,
			AudioURL:       input.AudioURL,
			DecisionID:     input.DecisionID,
			MissionID:      input.MissionID,
			SwitchAtMS:     switchAtMS,
		})
	}

	var task audioout.SpeechTask
	if controlMode {
		sessionID := input.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("control-%d", roomID)
		}
		if (hasInteractionV2Body(*decision) || decision.MissionKind == "time_report") && s.messages != nil {
			duration, _, probeErr := s.audioHub.ProbeExternalWAV(r.Context(), input.AudioURL)
			if probeErr != nil {
				writeError(w, 502, "无法读取互动音频时长")
				return
			}
			raw, _ := json.Marshal(map[string]any{"decision_id": input.DecisionID, "duration_ms": duration, "start_at": time.Now()})
			if failures := s.messages.Publish(r.Context(), messagecaptain.Message{Kind: "interaction.v2.reserve-audio", RoomID: roomID, Payload: raw}); len(failures) > 0 {
				writeError(w, 409, "互动播出额度或原始期限暂不允许，保留已合成声音继续排队")
				return
			}
		}
		hubTask, hubErr := s.audioHub.CreateExternalTask(
			r.Context(), roomID, sessionID, fmt.Sprintf("%s · %s", label, input.Topic), input.AudioURL,
		)
		err = hubErr
		if hubErr == nil {
			task = audioout.SpeechTask{
				ID:         hubTask.ID,
				RoomID:     hubTask.RoomID,
				SessionID:  hubTask.SessionID,
				Kind:       hubTask.Kind,
				Label:      hubTask.Label,
				AudioURL:   hubTask.AudioURL,
				MimeType:   hubTask.MimeType,
				DurationMS: hubTask.DurationMS,
				StartMS:    hubTask.StartMS,
				Sequence:   hubTask.Sequence,
				StartedAt:  hubTask.StartedAt,
				CreatedAt:  hubTask.CreatedAt,
			}
		}
	} else {
		callbackBase := state.callbackBase
		if callbackBase == "" {
			callbackBase = "http://127.0.0.1:8081"
		}
		callbackURL := callbackBase + "/internal/v1/audio/events"
		var snapshot audioout.RoomProgramSnapshot
		snapshot, err = state.client.InsertTestProgramInteraction(dispatchCtx, audioout.InsertInteractionInput{
			AcousticCut:    acousticMode,
			ExpectedTaskID: program.Task.ID,
			RoomID:         roomID,
			SessionID:      program.Task.SessionID,
			Label:          fmt.Sprintf("%s · %s", label, input.Topic),
			AudioURL:       input.AudioURL,
			DeferredReturn: input.DeferredReturn, DecisionID: input.DecisionID, AllowConsecutiveRepeat: input.AllowConsecutiveRepeat,
			Text:                strings.TrimSpace(input.ReplyText),
			Timeline:            input.Timeline,
			CallbackURL:         callbackURL,
			ResumeOffsetMS:      resumeOffsetMS,
			SwitchAtMS:          switchAtMS,
			SkipCurrentSentence: false,
		})
		if err == nil && snapshot.Task != nil {
			task = *snapshot.Task
		}
	}
	if err != nil {
		if s.speechRuntime != nil {
			_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
				Track:          speechruntime.TrackInterrupt,
				Status:         speechruntime.StatusFailed,
				Text:           input.ReplyText,
				QuestionText:   input.Question,
				ReplyText:      input.ReplyText,
				ResumeStrategy: resumeMode,
				BridgeText:     input.BridgeText,
				BridgeUsed:     bridgeUsed,
				Source:         source,
				AudioURL:       input.AudioURL,
				DecisionID:     input.DecisionID,
				MissionID:      input.MissionID,
			})
		}
		if strings.Contains(err.Error(), "互动播出额度或原始期限暂不允许") {
			writeError(w, http.StatusConflict, "互动播出额度或原始期限暂不允许，保留已合成声音继续排队")
			return
		}
		if errors.Is(err, audioout.ErrInteractionSwitchStale) {
			writeError(w, http.StatusConflict, "主线已换段，请重新选择切点")
			return
		}
		writeError(w, http.StatusBadGateway, "提交插播失败: "+err.Error())
		return
	}
	if strings.TrimSpace(task.ID) == "" {
		writeError(w, http.StatusBadGateway, "播音分发层未返回任务")
		return
	}

	s.agentDecisions.BindPlayback(roomID, input.DecisionID, task.ID)

	if switchAtMS != nil {
		s.agentDecisions.SetExecutionReceipt(roomID, input.DecisionID, actualInteractionCutLevel(program, *switchAtMS), input.SelectedContentPlan, input.PreparationCutPlans)
	}
	if state != nil {
		state.mu.Lock()
		state.interactions[task.ID] = &audioInteractionMeta{
			RoomID:               roomID,
			RuntimeEpoch:         s.currentInteractionEpoch(roomID),
			MissionID:            input.MissionID,
			MissionKind:          input.MissionKind,
			HumanizationStrategy: input.HumanizationStrategy,
			HumanizationKind:     input.HumanizationKind,
			HumanizationApplied:  input.HumanizationApplied,
			Topic:                input.Topic,
			ResumeMode:           resumeMode,
			BridgeText:           input.BridgeText,
			BridgeUsed:           bridgeUsed,
			DecisionID:           input.DecisionID,
			QuestionText:         input.Question,
			ReplyText:            input.ReplyText,
			Source:               source,
			AudioURL:             input.AudioURL,
			WaitForMainline:      !controlMode,
		}
		state.mu.Unlock()
	}

	if s.speechRuntime != nil {
		startedAt := task.StartedAt
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		durationMS := task.DurationMS
		if durationMS <= 0 {
			durationMS = estimatedInteractionDurationMS(input.ReplyText)
		}
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:          speechruntime.TrackInterrupt,
			Status:         speechruntime.StatusPlaying,
			Text:           input.ReplyText,
			QuestionText:   input.Question,
			ReplyText:      input.ReplyText,
			ResumeStrategy: resumeMode,
			BridgeText:     input.BridgeText,
			BridgeUsed:     bridgeUsed,
			Source:         source,
			AudioURL:       input.AudioURL,
			DecisionID:     input.DecisionID,
			MissionID:      input.MissionID,
			SwitchAtMS:     switchAtMS,
			SpeechTaskID:   task.ID,
			DurationMS:     durationMS,
			StartedAt:      &startedAt,
		})
	}
	s.scheduleRoomAudioInteractionCompletion(roomID, task, input.DecisionID, !controlMode)
	actualResumeSegmentID := ""
	actualSkipCount := 0
	writeJSON(w, http.StatusOK, map[string]any{
		"mission_id":        input.MissionID,
		"dispatched":        true,
		"deferred_return":   input.DeferredReturn && program.Sequential && s.roomAudio != nil,
		"action":            input.Action,
		"cut_level":         actualCutLevel(program, switchAtMS),
		"switch_at_ms":      switchAtMS,
		"resume_offset_ms":  resumeOffsetMS,
		"resume_strategy":   resumeMode,
		"resume_segment_id": actualResumeSegmentID,
		"actual_skip_count": actualSkipCount,
		"bridge_used":       bridgeUsed,
		"dedup_triggered":   resumeDedupTriggered,
		"duplicate_score":   resumeDuplicateScore,
		"task":              task,
	})
}
