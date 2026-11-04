package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
	"livecompanion/core/internal/timeline"
)

type audioTaskClient interface {
	CreateTestTask(context.Context, audioout.CreateTestTaskInput) (audioout.SpeechTask, error)
	CreateExternalTask(context.Context, audioout.CreateExternalTaskInput) (audioout.SpeechTask, error)
	StartTestProgram(context.Context, int64, string, string, string) (audioout.RoomProgramSnapshot, error)
	InsertTestProgramInteraction(context.Context, audioout.InsertInteractionInput) (audioout.RoomProgramSnapshot, error)
	ProgramSnapshot(context.Context, int64) (audioout.RoomProgramSnapshot, error)
	StopTestProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
	Enabled() bool
}

type audioInteractionCompleter interface {
	CompleteProgramInteraction(context.Context, int64, string) (audioout.RoomProgramSnapshot, error)
}

func interactionDebtForMission(missionKind, topic string) (timeline.DebtKind, time.Duration) {
	switch strings.ToLower(strings.TrimSpace(missionKind)) {
	case "reply_question":
		return timeline.DebtQuestion, 2 * time.Minute
	case "reply_chat", "welcome_named", "welcome_batch":
		return timeline.DebtInteraction, 2 * time.Minute
	case "reply_like":
		return timeline.DebtLikeCTA, 90 * time.Second
	case "reply_follow":
		return timeline.DebtFollowCTA, 90 * time.Second
	case "conversion_signal":
		return timeline.DebtConversion, 2 * time.Minute
	}
	normalizedTopic := strings.ToUpper(strings.TrimSpace(topic))
	switch {
	case strings.HasPrefix(normalizedTopic, "SIGNAL:ORDER"):
		return timeline.DebtConversion, 2 * time.Minute
	case strings.HasPrefix(normalizedTopic, "INTERACTION:"):
		return timeline.DebtInteraction, 2 * time.Minute
	case normalizedTopic != "":
		return timeline.DebtQuestion, 2 * time.Minute
	default:
		return "", 0
	}
}

type audioDevState struct {
	client       audioTaskClient
	callbackBase string

	mu           sync.Mutex
	events       map[string]audioout.PlaybackEvent
	interactions map[string]*audioInteractionMeta
	history      map[int64][]*audioInteractionRecord
}

type audioInteractionMeta struct {
	RoomID               int64
	RuntimeEpoch         string
	MissionID            string
	MissionKind          string
	HumanizationStrategy string
	HumanizationKind     string
	HumanizationApplied  bool
	Topic                string
	ResumeMode           string
	BridgeText           string
	BridgeUsed           bool
	ResumeUnit           string
	TextDigest           string
	BridgeDigest         string
	DecisionID           string
	QuestionText         string
	ReplyText            string
	Source               string
	AudioURL             string
	WaitForMainline      bool
	SkipUnits            []string
	AnswerPinned         bool
	ResumePinned         bool
	Record               *audioInteractionRecord
}

var audioDevStates sync.Map

func (s *Server) SetAudioClient(client audioTaskClient, callbackBase string) {
	audioDevStates.Store(s, &audioDevState{
		client:       client,
		callbackBase: strings.TrimRight(strings.TrimSpace(callbackBase), "/"),
		events:       make(map[string]audioout.PlaybackEvent),
		interactions: make(map[string]*audioInteractionMeta),
		history:      make(map[int64][]*audioInteractionRecord),
	})
	if s.messages == nil {
		s.messages = messagecaptain.New()
	}
	if s.messageUnregister != nil {
		s.messageUnregister()
	}
	removeLifecycle := s.messages.Register("interaction-lifecycle", func(ctx context.Context, message messagecaptain.Message) error {
		if message.Kind == messagecaptain.ReturnBridgeStaged || message.Kind == messagecaptain.ReturnBridgeStarted {
			return s.handleReturnPlaybackMessage(ctx, message)
		}
		if message.Kind != messagecaptain.AudioPlayback {
			return nil
		}
		var event audioout.PlaybackEvent
		if err := json.Unmarshal(message.Payload, &event); err != nil {
			return err
		}
		if message.RoomID != event.RoomID || message.TaskID != event.SpeechTaskID {
			return fmt.Errorf("audio message scope mismatch")
		}
		if event.Status == "STARTING" {
			return s.validatePCMInteractionStart(ctx, event)
		}
		s.applyAudioInteractionPlaybackEvent(ctx, s.audioDevState(), event)
		return nil
	})
	s.messageUnregister = func() { removeLifecycle() }
	if source, ok := client.(interface{ SetMessageCaptain(*messagecaptain.Captain) }); ok {
		source.SetMessageCaptain(s.messages)
	}
}

func (s *Server) audioDevState() *audioDevState {
	value, ok := audioDevStates.Load(s)
	if !ok {
		return nil
	}
	state, _ := value.(*audioDevState)
	return state
}

func (s *Server) stopRoomAudio(ctx context.Context, roomID int64) error {
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		return nil
	}
	_, err := state.client.StopTestProgram(ctx, roomID)
	return err
}

func (s *Server) clearRoomAudioState(roomID int64) {
	if roomID <= 0 {
		return
	}
	state := s.audioDevState()
	if state == nil {
		return
	}
	state.mu.Lock()
	for id, event := range state.events {
		if event.RoomID == roomID {
			delete(state.events, id)
		}
	}
	for id, meta := range state.interactions {
		if meta != nil && meta.RoomID == roomID {
			delete(state.interactions, id)
		}
	}
	delete(state.history, roomID)
	state.mu.Unlock()
}

func (s *Server) requireDevAudio(w http.ResponseWriter) (*audioDevState, bool) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "播音分发层尚未配置")
		return nil, false
	}
	return state, true
}

func (s *Server) createDevAudioTest(w http.ResponseWriter, r *http.Request) {
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	var input struct {
		RoomID       int64  `json:"room_id"`
		SessionID    string `json:"session_id"`
		SpeechTaskID string `json:"speech_task_id"`
		Label        string `json:"label"`
		DurationMS   int    `json:"duration_ms"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.SessionID == "" {
		input.SessionID = "dev-room-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	if len(input.SessionID) > 160 || len(strings.TrimSpace(input.SpeechTaskID)) > 160 || len([]rune(input.Label)) > 200 {
		writeError(w, http.StatusBadRequest, "test audio input is too long")
		return
	}
	callbackBase := state.callbackBase
	if callbackBase == "" {
		callbackBase = "http://127.0.0.1:8081"
	}
	task, err := state.client.CreateTestTask(r.Context(), audioout.CreateTestTaskInput{
		RoomID:       input.RoomID,
		SessionID:    input.SessionID,
		SpeechTaskID: strings.TrimSpace(input.SpeechTaskID),
		Label:        strings.TrimSpace(input.Label),
		DurationMS:   input.DurationMS,
		CallbackURL:  callbackBase + "/internal/v1/dev/audio/events",
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "提交播音任务失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"task":        task,
		"core_status": "DISPATCHED",
	})
}

func (s *Server) startDevAudioProgram(w http.ResponseWriter, r *http.Request) {
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil || input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	callbackBase := state.callbackBase
	if callbackBase == "" {
		callbackBase = "http://127.0.0.1:8081"
	}
	snapshot, err := state.client.StartTestProgram(
		r.Context(),
		input.RoomID,
		fmt.Sprintf("dev-room-%d-continuous", input.RoomID),
		"同音色 TTS 主线 · 0.9 倍 · 连续测试",
		callbackBase+"/internal/v1/dev/audio/events",
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "启动连续播音失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) insertDevAudioInteraction(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "旧文字时间轴插播入口已永久移除，请使用真实PCM声学插播")
}

func (s *Server) stopDevAudioProgram(w http.ResponseWriter, r *http.Request) {
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil || input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	snapshot, err := state.client.StopTestProgram(r.Context(), input.RoomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "停止连续播音失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) receiveDevAudioEvent(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.receiveAudioEvent(w, r)
}

func (s *Server) receiveAudioEvent(w http.ResponseWriter, r *http.Request) {
	state := s.audioDevState()
	if state == nil {
		writeError(w, http.StatusServiceUnavailable, "播音分发层尚未配置")
		return
	}
	var event audioout.PlaybackEvent
	if err := readJSON(w, r, &event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid playback event")
		return
	}
	event.SpeechTaskID = strings.TrimSpace(event.SpeechTaskID)
	event.ReceiverID = strings.TrimSpace(event.ReceiverID)
	event.Status = strings.ToUpper(strings.TrimSpace(event.Status))
	if event.SpeechTaskID == "" || event.RoomID <= 0 || event.SessionID == "" || event.ReceiverID == "" {
		writeError(w, http.StatusBadRequest, "playback event identity is incomplete")
		return
	}
	switch event.Status {
	case "READY", "PLAYING", "PROGRESS", "COMPLETED", "FAILED":
	default:
		writeError(w, http.StatusBadRequest, "invalid playback status")
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	s.applyAudioInteractionPlaybackEvent(r.Context(), state, event)
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true})
}

func (s *Server) applyAudioInteractionPlaybackEvent(ctx context.Context, state *audioDevState, event audioout.PlaybackEvent) {
	if state == nil {
		return
	}
	if event.ReceiverID != "core-pcm-output" {
		if reader, ok := state.client.(interactionPCMReader); ok {
			if _, err := reader.InteractionPlaybackSnapshot(ctx, event.RoomID, event.SpeechTaskID); err == nil {
				return
			}
		}
		if reader, ok := state.client.(interactionReturnReader); ok {
			if _, err := reader.InteractionReturnSnapshot(ctx, event.RoomID, event.SpeechTaskID); err == nil {
				return
			}
		}
	}
	state.mu.Lock()
	previous := state.events[event.SpeechTaskID]
	duplicateTerminal := previous.Status == event.Status && (event.Status == "COMPLETED" || event.Status == "FAILED")
	if (previous.Status == "COMPLETED" || previous.Status == "FAILED") && previous.Status != event.Status {
		state.mu.Unlock()
		return
	}
	if (previous.Status == "PLAYING" || previous.Status == "PROGRESS") && event.Status == "READY" {
		state.mu.Unlock()
		return
	}
	meta := state.interactions[event.SpeechTaskID]
	if meta != nil && meta.RoomID != event.RoomID {
		state.mu.Unlock()
		return
	}
	if meta != nil && meta.DecisionID != "" && s.agentDecisions != nil {
		item, ok := s.agentDecisions.Get(meta.RoomID, meta.DecisionID)
		if !ok || item.Status != "CLAIMED" || (item.PlaybackTaskID != "" && item.PlaybackTaskID != event.SpeechTaskID) {
			meta = nil
		}
		if meta != nil && meta.RuntimeEpoch != "" && s.currentInteractionEpoch(meta.RoomID) != meta.RuntimeEpoch {
			meta = nil
		}
		if meta != nil && s.speechRuntime != nil {
			if current, err := s.speechRuntime.Snapshot(meta.RoomID); err == nil && (current.Interrupt.Status == speechruntime.StatusPaused || (current.Interrupt.DecisionID != "" && current.Interrupt.DecisionID != meta.DecisionID) || (current.Interrupt.SpeechTaskID != "" && current.Interrupt.SpeechTaskID != event.SpeechTaskID)) {
				meta = nil
			}
		}
	}
	if meta != nil && meta.DecisionID != "" {
		if s.speechRuntime != nil {
			status := speechruntime.StatusPlaying
			switch event.Status {
			case "COMPLETED":
				if meta.WaitForMainline {
					status = speechruntime.StatusReturning
				} else {
					status = speechruntime.StatusCompleted
				}
			case "FAILED":
				status = speechruntime.StatusFailed
			case "READY":
				status = speechruntime.StatusReady
			}
			var switchAtMS *int
			var preparedUntil *time.Time
			var startedAt *time.Time
			durationMS := 0
			if current, snapshotErr := s.speechRuntime.Snapshot(meta.RoomID); snapshotErr == nil && current.Interrupt.DecisionID == meta.DecisionID && (current.Interrupt.SpeechTaskID == "" || current.Interrupt.SpeechTaskID == event.SpeechTaskID) {
				switchAtMS = current.Interrupt.SwitchAtMS
				preparedUntil = current.Interrupt.PreparedUntil
				startedAt = current.Interrupt.StartedAt
				durationMS = current.Interrupt.DurationMS
			}
			if event.Status == "PLAYING" && startedAt == nil && !event.OccurredAt.IsZero() {
				at := event.OccurredAt.UTC()
				startedAt = &at
			}
			if startedAt == nil && event.Status == "PROGRESS" && event.ProgressMS > 0 && !event.OccurredAt.IsZero() {
				at := event.OccurredAt.UTC().Add(-time.Duration(event.ProgressMS) * time.Millisecond)
				startedAt = &at
			}
			_, updateErr := s.speechRuntime.Update(meta.RoomID, speechruntime.UpdateInput{
				Track:          speechruntime.TrackInterrupt,
				ExpectedTaskID: event.SpeechTaskID, ExpectedDecisionID: meta.DecisionID, RequireIdentity: meta.RuntimeEpoch != "",
				Status:         status,
				Text:           meta.ReplyText,
				QuestionText:   meta.QuestionText,
				ReplyText:      meta.ReplyText,
				ResumeStrategy: meta.ResumeMode,
				BridgeText:     meta.BridgeText,
				BridgeUsed:     meta.BridgeUsed,
				Source:         meta.Source,
				AudioURL:       meta.AudioURL,
				DecisionID:     meta.DecisionID,
				MissionID:      meta.MissionID,
				SpeechTaskID:   event.SpeechTaskID,
				SwitchAtMS:     switchAtMS,
				DurationMS:     durationMS,
				StartedAt:      startedAt, PreparedUntil: preparedUntil,
			})
			if updateErr != nil {
				state.mu.Unlock()
				return
			}
		}
	}
	state.events[event.SpeechTaskID] = event
	var pinAnswer bool
	var pinResume bool
	var completedRecord *audioInteractionRecord
	if meta != nil {
		if meta.Record != nil {
			meta.Record.Status = event.Status
		}
		if (event.Status == "PLAYING" || event.Status == "PROGRESS") && meta.Record != nil && meta.Record.StartedAt == nil {
			at := event.OccurredAt
			meta.Record.StartedAt = &at
		}
		if event.Status == "COMPLETED" && !meta.AnswerPinned {
			meta.AnswerPinned = true
			pinAnswer = true
		}
		if event.Status == "COMPLETED" && !meta.ResumePinned {
			meta.ResumePinned = true
			pinResume = true
			if meta.Record != nil {
				at := event.OccurredAt
				meta.Record.CompletedAt = &at
				copy := *meta.Record
				copy.StrategyChain = append([]string(nil), meta.Record.StrategyChain...)
				completedRecord = &copy
			}
		}
	}
	state.mu.Unlock()
	if meta != nil && meta.DecisionID != "" && s.messages != nil && !duplicateTerminal {
		raw, _ := json.Marshal(map[string]any{"decision_id": meta.DecisionID, "status": event.Status, "at": event.OccurredAt})
		s.messages.Publish(ctx, messagecaptain.Message{Kind: "interaction.v2.playback", RoomID: meta.RoomID, RunID: meta.RuntimeEpoch, TaskID: event.SpeechTaskID, Payload: raw})
	}
	if (event.Status == "COMPLETED" || event.Status == "FAILED") && meta != nil && !duplicateTerminal {
		if event.Status == "COMPLETED" {
			// A receiver reports completion exactly at the media boundary. Give its
			// device/output buffer the same guard tail used by the PCM mirror before
			// replacing the interaction with resumed mainline audio.
			time.Sleep(time.Duration(roomaudio.VoiceGuardTailMS) * time.Millisecond)
		}
		if completer, ok := state.client.(audioInteractionCompleter); ok {
			if snapshot, resumeErr := completer.CompleteProgramInteraction(ctx, meta.RoomID, event.SpeechTaskID); resumeErr != nil {
				log.Printf("core audio receiver-terminal resume room=%d task=%s status=%s failed: %v", meta.RoomID, event.SpeechTaskID, event.Status, resumeErr)
			} else {
				log.Printf(
					"core audio receiver-terminal resume room=%d task=%s status=%s current_ms=%d track=%s",
					meta.RoomID, event.SpeechTaskID, event.Status, snapshot.CurrentMS, snapshot.TrackID,
				)
			}
		}
	}
	if meta != nil && meta.DecisionID != "" {

		if event.Status == "PLAYING" {
			s.scheduleRoomAudioInteractionCompletion(meta.RoomID, audioout.SpeechTask{ID: event.SpeechTaskID, DurationMS: event.DurationMS}, meta.DecisionID, meta.WaitForMainline)
		}
		if event.Status == "COMPLETED" && s.agentDecisions != nil {
			if meta.WaitForMainline {
				s.completeInteractionAfterMainlineResume(meta.RoomID, event.SpeechTaskID, meta.DecisionID)
			} else {
				_, _ = s.agentDecisions.Complete(meta.RoomID, meta.DecisionID)
			}
		}
		if event.Status == "FAILED" && s.agentDecisions != nil {
			s.agentDecisions.FailPlayback(meta.RoomID, meta.DecisionID, event.SpeechTaskID, event.Error)
		}
	}
	if completedRecord != nil {
		appendDevInteractionRecord(*completedRecord)
	}

	if s.brain != nil && meta != nil {
		if pinAnswer {
			metadata := map[string]string{
				"resume_mode":   meta.ResumeMode,
				"resume_unit":   meta.ResumeUnit,
				"bridge_digest": meta.BridgeDigest,
				"mission_kind":  meta.MissionKind,
			}
			if len(meta.SkipUnits) > 0 {
				metadata["skip_units"] = strings.Join(meta.SkipUnits, ",")
			}
			spend, cooldown := interactionDebtForMission(meta.MissionKind, meta.Topic)
			s.brain.RecordPin(meta.RoomID, timeline.Pin{
				At:         event.OccurredAt,
				Kind:       timeline.PinAnswer,
				Strategy:   "interaction." + strings.ToLower(meta.ResumeMode),
				Topic:      meta.Topic,
				TextDigest: meta.TextDigest,
				Metadata:   metadata,
			}, spend, cooldown)
		}
		if pinResume {
			s.brain.RecordPin(meta.RoomID, timeline.Pin{
				At:       event.OccurredAt,
				Kind:     timeline.PinResume,
				Strategy: meta.ResumeMode,
				Topic:    meta.Topic,
				Key:      meta.ResumeUnit,
				Metadata: map[string]string{
					"interaction_task_id": event.SpeechTaskID,
				},
			}, "", 0)
		}
		if !duplicateTerminal && event.Status == "COMPLETED" && meta.HumanizationApplied && strings.TrimSpace(meta.HumanizationStrategy) != "" {
			s.brain.RecordPin(meta.RoomID, timeline.Pin{
				At:       event.OccurredAt,
				Kind:     timeline.PinHumanization,
				Strategy: meta.HumanizationStrategy,
				Key:      meta.HumanizationKind,
				Topic:    meta.Topic,
				Metadata: map[string]string{
					"interaction_task_id": event.SpeechTaskID,
					"mission_id":          meta.MissionID,
				},
			}, "", 0)
		}
	}
}

func (s *Server) getDevAudioTaskState(w http.ResponseWriter, r *http.Request) {
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	if taskID == "" || len(taskID) > 160 {
		writeError(w, http.StatusBadRequest, "invalid speech task id")
		return
	}
	state.mu.Lock()
	event, exists := state.events[taskID]
	state.mu.Unlock()
	if !exists {
		writeJSON(w, http.StatusOK, map[string]any{
			"speech_task_id": taskID,
			"status":         "DISPATCHED",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"speech_task_id": taskID,
		"status":         event.Status,
		"event":          event,
	})
}
