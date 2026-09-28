package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/speechruntime"
)

type roomAudioInteractionInput struct {
	DecisionID string `json:"decision_id"`
	SessionID  string `json:"session_id,omitempty"`
	Action     string `json:"action"`
	AudioURL   string `json:"audio_url"`
	Question   string `json:"question,omitempty"`
	ReplyText  string `json:"reply_text,omitempty"`
	Topic      string `json:"topic,omitempty"`
	SwitchAtMS int    `json:"switch_at_ms,omitempty"`
}

func nextRoomProgramSafeCut(program audioout.RoomProgramSnapshot, currentMS int, maxWait time.Duration) (int, bool) {
	if currentMS < 0 {
		currentMS = 0
	}
	maxWaitMS := int(maxWait.Milliseconds())
	if maxWaitMS <= 0 {
		maxWaitMS = 35000
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 1, maxWaitMS, minInt(maxWaitMS, 12000)); ok {
		return point.CutMS, true
	}
	// Development/test WAVs still use the legacy static map.
	if len(program.SafePoints) == 0 && len(program.Timeline) == 0 {
		if point, exists := devMainlineNextSafePoint(currentMS, maxWait); exists {
			return point.CutMS, true
		}
	}
	return 0, false
}

func plannedRoomProgramSafeCut(program audioout.RoomProgramSnapshot, currentMS int) (int, bool) {
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 30000, 55000, 42000); ok {
		return point.CutMS, true
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 24000, 50000, 36000); ok {
		return point.CutMS, true
	}
	return 0, false
}

func requestedRoomProgramSafeCut(program audioout.RoomProgramSnapshot, requestedMS, currentMS int, maxWait time.Duration) (int, bool) {
	if requestedMS <= currentMS || requestedMS <= 0 {
		return 0, false
	}
	maxWaitMS := int(maxWait.Milliseconds())
	if maxWaitMS <= 0 {
		maxWaitMS = 35000
	}
	if requestedMS-currentMS > maxWaitMS {
		return 0, false
	}
	for _, point := range effectiveRoomProgramSafePoints(program) {
		if point.CutMS == requestedMS {
			return requestedMS, true
		}
	}
	return 0, false
}

func resolveRoomProgramSafeCutAfterGeneration(program audioout.RoomProgramSnapshot, preferredMS, currentMS int) (int, bool) {
	if preferredMS > 0 {
		lead := preferredMS - currentMS
		if lead >= 4000 && lead <= 33000 {
			if cut, ok := requestedRoomProgramSafeCut(program, preferredMS, currentMS, 33*time.Second); ok {
				return cut, true
			}
		}
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 5000, 30000, 12000); ok {
		return point.CutMS, true
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 3000, 33000, 10000); ok {
		return point.CutMS, true
	}
	return 0, false
}

func resolveQuickRoomProgramSafeCut(program audioout.RoomProgramSnapshot, preferredMS, currentMS int) (int, bool) {
	if preferredMS > 0 {
		lead := preferredMS - currentMS
		if lead >= 3000 && lead <= 24000 {
			if cut, ok := requestedRoomProgramSafeCut(program, preferredMS, currentMS, 24*time.Second); ok {
				return cut, true
			}
		}
	}
	// 抢答也必须等到完整句末再切。至少预留约 3 秒，让前端能够明确标出
	// “停止前”这一句；避免 TTS 刚准备好就立刻硬切，用户只听到突然停止。
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 3000, 16000, 5500); ok {
		return point.CutMS, true
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 1500, 24000, 6500); ok {
		return point.CutMS, true
	}
	return 0, false
}

func chooseRoomProgramSafePointWindow(
	program audioout.RoomProgramSnapshot,
	currentMS, minLeadMS, maxLeadMS, targetLeadMS int,
) (audioout.ProgramSafePoint, bool) {
	durationMS := 0
	if program.Task != nil {
		durationMS = program.Task.DurationMS
	}
	points := effectiveRoomProgramSafePoints(program)
	candidates := make([]audioout.ProgramSafePoint, 0, len(points))
	for _, point := range points {
		lead := point.CutMS - currentMS
		if lead < minLeadMS || lead > maxLeadMS {
			continue
		}
		if durationMS > 0 && point.CutMS >= durationMS {
			continue
		}
		candidates = append(candidates, point)
	}
	for _, grade := range []string{"A", "B", "C"} {
		best, ok := bestRoomProgramSafePoint(candidates, grade, currentMS, targetLeadMS)
		if ok {
			return best, true
		}
	}
	return bestRoomProgramSafePoint(candidates, "", currentMS, targetLeadMS)
}

func effectiveRoomProgramSafePoints(program audioout.RoomProgramSnapshot) []audioout.ProgramSafePoint {
	if len(program.SafePoints) > 0 {
		return program.SafePoints
	}
	points := make([]audioout.ProgramSafePoint, 0, len(program.Timeline))
	for index, segment := range program.Timeline {
		if !segment.SafeCut || segment.EndMS <= segment.StartMS {
			continue
		}
		nextPreview := ""
		if index+1 < len(program.Timeline) {
			nextPreview = strings.TrimSpace(program.Timeline[index+1].Text)
		}
		points = append(points, audioout.ProgramSafePoint{
			ID:          fmt.Sprintf("LEGACY-SP%03d", len(points)+1),
			CutMS:       segment.EndMS,
			Score:       90,
			Grade:       "B",
			Kind:        "SENTENCE",
			SentenceID:  segment.SegmentID,
			LeftPreview: strings.TrimSpace(segment.Text),
			NextPreview: nextPreview,
		})
	}
	return points
}

func bestRoomProgramSafePoint(
	candidates []audioout.ProgramSafePoint,
	grade string,
	currentMS, targetLeadMS int,
) (audioout.ProgramSafePoint, bool) {
	var best audioout.ProgramSafePoint
	bestDistance := int(^uint(0) >> 1)
	found := false
	for _, point := range candidates {
		if grade != "" && strings.ToUpper(strings.TrimSpace(point.Grade)) != grade {
			continue
		}
		distance := point.CutMS - currentMS - targetLeadMS
		if distance < 0 {
			distance = -distance
		}
		if !found || distance < bestDistance || (distance == bestDistance && point.Score > best.Score) {
			best = point
			bestDistance = distance
			found = true
		}
	}
	return best, found
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) scheduleRoomAudioInteractionCompletion(roomID int64, task audioout.SpeechTask, decisionID string) {
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
		if s.speechRuntime == nil {
			return
		}
		snapshot, err := s.speechRuntime.Snapshot(roomID)
		if err != nil || snapshot.Interrupt.SpeechTaskID != task.ID {
			return
		}
		if snapshot.Interrupt.Status != speechruntime.StatusPlaying && snapshot.Interrupt.Status != speechruntime.StatusReady {
			return
		}
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:        speechruntime.TrackInterrupt,
			Status:       speechruntime.StatusCompleted,
			Text:         snapshot.Interrupt.Text,
			QuestionText: snapshot.Interrupt.QuestionText,
			ReplyText:    snapshot.Interrupt.ReplyText,
			Source:       snapshot.Interrupt.Source,
			AudioURL:     snapshot.Interrupt.AudioURL,
			DecisionID:   snapshot.Interrupt.DecisionID,
			SpeechTaskID: snapshot.Interrupt.SpeechTaskID,
			SwitchAtMS:   snapshot.Interrupt.SwitchAtMS,
			StartedAt:    snapshot.Interrupt.StartedAt,
		})
		if s.agentDecisions != nil && strings.TrimSpace(decisionID) != "" {
			_, _ = s.agentDecisions.Complete(roomID, decisionID)
		}
	})
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
	input.DecisionID = strings.TrimSpace(input.DecisionID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.AudioURL = strings.TrimSpace(input.AudioURL)
	input.Question = strings.TrimSpace(input.Question)
	input.ReplyText = strings.TrimSpace(input.ReplyText)
	input.Topic = strings.TrimSpace(input.Topic)
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
	if s.speechRuntime != nil {
		if speech, speechErr := s.speechRuntime.Snapshot(roomID); speechErr == nil && speechSnapshotBusy(speech) {
			writeError(w, http.StatusConflict, "上一条TTS尚未播放完成，请继续排队")
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
		if input.Action == "answer" {
			cutMS, exists = resolveRoomProgramSafeCutAfterGeneration(program, input.SwitchAtMS, currentMS)
		} else {
			cutMS, exists = resolveQuickRoomProgramSafeCut(program, input.SwitchAtMS, currentMS)
		}
		if input.Action == "answer" {
			if !exists {
				writeError(w, http.StatusConflict, "当前35秒内没有安全句末切点，继续排队")
				return
			}
			value := cutMS
			switchAtMS = &value
			resumeOffsetMS = &value
		} else if exists {
			// 抢答也在完整句末切换；切点同时用于停止主线和插播后的恢复位置。
			value := cutMS
			switchAtMS = &value
			resumeOffsetMS = &value
		} else if program.Task.DurationMS > 1 {
			// 已处于本稿最后语义段时，抢答后直接跨到下一稿，避免回到半句和残留 1ms 杂音。
			value := program.Task.DurationMS
			resumeOffsetMS = &value
		}
		if switchAtMS != nil {
			log.Printf(
				"core audio interaction plan room=%d action=%s current_ms=%d switch_ms=%d lead_ms=%d",
				roomID, input.Action, currentMS, *switchAtMS, *switchAtMS-currentMS,
			)
		}
	}

	source := "manual_answer"
	label := "回答"
	resumeMode := "DIRECT"
	if !controlMode {
		resumeMode = "SAFE"
	}
	if input.Action == "quick" {
		source = "manual_quick"
		label = "抢答"
		resumeMode = "HARD"
	}
	if s.speechRuntime != nil {
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:        speechruntime.TrackInterrupt,
			Status:       speechruntime.StatusReady,
			Text:         input.ReplyText,
			QuestionText: input.Question,
			ReplyText:    input.ReplyText,
			Source:       source,
			AudioURL:     input.AudioURL,
			DecisionID:   input.DecisionID,
			SwitchAtMS:   switchAtMS,
		})
	}

	var task audioout.SpeechTask
	if controlMode {
		sessionID := input.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("control-%d", roomID)
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
		snapshot, err = state.client.InsertTestProgramInteraction(r.Context(), audioout.InsertInteractionInput{
			RoomID:         roomID,
			SessionID:      program.Task.SessionID,
			Label:          fmt.Sprintf("%s · %s", label, input.Topic),
			AudioURL:       input.AudioURL,
			CallbackURL:    callbackURL,
			ResumeOffsetMS: resumeOffsetMS,
			SwitchAtMS:     switchAtMS,
		})
		if err == nil && snapshot.Task != nil {
			task = *snapshot.Task
		}
	}
	if err != nil {
		if s.speechRuntime != nil {
			_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
				Track:        speechruntime.TrackInterrupt,
				Status:       speechruntime.StatusFailed,
				Text:         input.ReplyText,
				QuestionText: input.Question,
				ReplyText:    input.ReplyText,
				Source:       source,
				AudioURL:     input.AudioURL,
				DecisionID:   input.DecisionID,
			})
		}
		writeError(w, http.StatusBadGateway, "提交插播失败: "+err.Error())
		return
	}
	if strings.TrimSpace(task.ID) == "" {
		writeError(w, http.StatusBadGateway, "播音分发层未返回任务")
		return
	}

	if state != nil {
		state.mu.Lock()
		state.interactions[task.ID] = &audioInteractionMeta{
			RoomID:       roomID,
			Topic:        input.Topic,
			ResumeMode:   resumeMode,
			DecisionID:   input.DecisionID,
			QuestionText: input.Question,
			ReplyText:    input.ReplyText,
			Source:       source,
			AudioURL:     input.AudioURL,
		}
		state.mu.Unlock()
	}

	if s.speechRuntime != nil {
		startedAt := task.StartedAt
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:        speechruntime.TrackInterrupt,
			Status:       speechruntime.StatusPlaying,
			Text:         input.ReplyText,
			QuestionText: input.Question,
			ReplyText:    input.ReplyText,
			Source:       source,
			AudioURL:     input.AudioURL,
			DecisionID:   input.DecisionID,
			SwitchAtMS:   switchAtMS,
			SpeechTaskID: task.ID,
			StartedAt:    &startedAt,
		})
	}
	s.scheduleRoomAudioInteractionCompletion(roomID, task, input.DecisionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"dispatched":       true,
		"action":           input.Action,
		"switch_at_ms":     switchAtMS,
		"resume_offset_ms": resumeOffsetMS,
		"task":             task,
	})
}
