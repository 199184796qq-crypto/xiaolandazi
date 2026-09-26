package httpapi

import (
	"fmt"
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
}

func (s *Server) scheduleRoomAudioInteractionCompletion(roomID int64, task audioout.SpeechTask, decisionID string) {
	if roomID <= 0 || strings.TrimSpace(task.ID) == "" || task.DurationMS <= 0 {
		return
	}
	startedAt := task.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	deadline := startedAt.Add(time.Duration(task.DurationMS) * time.Millisecond)
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
	if !controlMode {
		program, err = state.client.ProgramSnapshot(r.Context(), roomID)
		if err != nil || !program.Running || program.Task == nil {
			writeError(w, http.StatusConflict, "当前没有可插播的主线音频")
			return
		}
		if input.Action == "answer" {
			currentMS := program.Task.StartMS
			if !program.Task.StartedAt.IsZero() {
				elapsed := int(time.Since(program.Task.StartedAt).Milliseconds())
				if elapsed > 0 {
					currentMS += elapsed
				}
			}
			point, exists := devMainlineNextSafePoint(currentMS, 35*time.Second)
			if !exists {
				writeError(w, http.StatusConflict, "当前35秒内没有安全句末切点，继续排队")
				return
			}
			value := point.CutMS
			switchAtMS = &value
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
			RoomID:      roomID,
			SessionID:   program.Task.SessionID,
			Label:       fmt.Sprintf("%s · %s", label, input.Topic),
			AudioURL:    input.AudioURL,
			CallbackURL: callbackURL,
			SwitchAtMS:  switchAtMS,
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
			SpeechTaskID: task.ID,
			StartedAt:    &startedAt,
		})
	}
	s.scheduleRoomAudioInteractionCompletion(roomID, task, input.DecisionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"dispatched":   true,
		"action":       input.Action,
		"switch_at_ms": switchAtMS,
		"task":         task,
	})
}
