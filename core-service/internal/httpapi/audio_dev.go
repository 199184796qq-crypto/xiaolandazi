package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/audioout"
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

type audioDevState struct {
	client       audioTaskClient
	callbackBase string

	mu           sync.Mutex
	events       map[string]audioout.PlaybackEvent
	interactions map[string]*audioInteractionMeta
	history      map[int64][]*audioInteractionRecord
}

type audioInteractionMeta struct {
	RoomID       int64
	Topic        string
	ResumeMode   string
	ResumeUnit   string
	TextDigest   string
	BridgeDigest string
	DecisionID   string
	QuestionText string
	ReplyText    string
	Source       string
	AudioURL     string
	SkipUnits    []string
	AnswerPinned bool
	ResumePinned bool
	Record       *audioInteractionRecord
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
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	var input struct {
		RoomID             int64    `json:"room_id"`
		SessionID          string   `json:"session_id"`
		Label              string   `json:"label"`
		AudioURL           string   `json:"audio_url"`
		ResumeOffsetMS     *int     `json:"resume_offset_ms,omitempty"`
		SwitchAtMS         *int     `json:"switch_at_ms,omitempty"`
		Topic              string   `json:"topic"`
		ResumeMode         string   `json:"resume_mode"`
		ResumeUnit         string   `json:"resume_unit"`
		CoveredTopics      []string `json:"covered_topics"`
		SkipUnits          []string `json:"skip_units"`
		TextDigest         string   `json:"text_digest"`
		BridgeDigest       string   `json:"bridge_digest"`
		Question           string   `json:"question"`
		StrategyChain      []string `json:"strategy_chain"`
		DirectorProgress   string   `json:"director_progress"`
		DirectorAtmosphere string   `json:"director_atmosphere"`
		Humanization       string   `json:"humanization"`
		EntryMode          string   `json:"entry_mode"`
		EntryLead          string   `json:"entry_lead"`
		ReplyCore          string   `json:"reply_core"`
		ResumeTail         string   `json:"resume_tail"`
		FinalText          string   `json:"final_text"`
		TargetSeconds      int      `json:"target_seconds"`
		TTSRate            float64  `json:"tts_rate"`
		QualityScore       int      `json:"quality_score"`
		QualityLevel       string   `json:"quality_level"`
		QualitySummary     string   `json:"quality_summary"`
		QualityIssues      []string `json:"quality_issues"`
		QualityAttempts    int      `json:"quality_attempts"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.RoomID <= 0 || strings.TrimSpace(input.AudioURL) == "" {
		writeError(w, http.StatusBadRequest, "room_id and audio_url are required")
		return
	}
	if len(input.SkipUnits) > 32 || len(strings.TrimSpace(input.ResumeMode)) > 64 || len(strings.TrimSpace(input.ResumeUnit)) > 160 {
		writeError(w, http.StatusBadRequest, "interaction metadata is too long")
		return
	}

	var resolved devResumeResolution
	if input.SwitchAtMS != nil {
		resolved = resolveDevMainlineResume(
			*input.SwitchAtMS,
			input.CoveredTopics,
			input.ReplyCore,
			input.ResumeUnit,
			input.SkipUnits,
		)
		if resolved.Changed {
			input.ResumeMode = resolved.Mode
			input.ResumeUnit = resolved.ResumeUnit
			input.SkipUnits = append([]string(nil), resolved.SkipUnits...)
			input.CoveredTopics = append([]string(nil), resolved.CoveredTopics...)
			if resolved.ResumeOffsetMS > 0 {
				resumeOffset := resolved.ResumeOffsetMS
				input.ResumeOffsetMS = &resumeOffset
			}
		}
	}

	callbackBase := state.callbackBase
	if callbackBase == "" {
		callbackBase = "http://127.0.0.1:8081"
	}
	snapshot, err := state.client.InsertTestProgramInteraction(r.Context(), audioout.InsertInteractionInput{
		RoomID:         input.RoomID,
		SessionID:      strings.TrimSpace(input.SessionID),
		Label:          strings.TrimSpace(input.Label),
		AudioURL:       strings.TrimSpace(input.AudioURL),
		CallbackURL:    callbackBase + "/internal/v1/dev/audio/events",
		ResumeOffsetMS: input.ResumeOffsetMS,
		SwitchAtMS:     input.SwitchAtMS,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "插入互动音频失败: "+err.Error())
		return
	}
	if snapshot.Task == nil || strings.TrimSpace(snapshot.Task.ID) == "" {
		writeError(w, http.StatusBadGateway, "播音分发层未返回互动任务")
		return
	}

	stopMS := snapshot.ResumeOffsetMS
	if input.SwitchAtMS != nil {
		stopMS = *input.SwitchAtMS
	}
	point, hasPoint := devMainlineContextAt(stopMS)
	record := &audioInteractionRecord{
		TaskID:             snapshot.Task.ID,
		RoomID:             input.RoomID,
		SessionID:          strings.TrimSpace(input.SessionID),
		CreatedAt:          time.Now().UTC(),
		Status:             "QUEUED",
		Question:           strings.TrimSpace(input.Question),
		StrategyChain:      append([]string(nil), input.StrategyChain...),
		DirectorProgress:   strings.TrimSpace(input.DirectorProgress),
		DirectorAtmosphere: strings.TrimSpace(input.DirectorAtmosphere),
		Humanization:       strings.TrimSpace(input.Humanization),
		EntryMode:          strings.TrimSpace(strings.ToUpper(input.EntryMode)),
		EntryLead:          strings.TrimSpace(input.EntryLead),
		ResumeMode:         strings.TrimSpace(strings.ToUpper(input.ResumeMode)),
		ResumeUnit:         strings.TrimSpace(input.ResumeUnit),
		CoveredTopics:      append([]string(nil), input.CoveredTopics...),
		SkipUnits:          append([]string(nil), input.SkipUnits...),
		ResumeOffsetMS:     snapshot.ResumeOffsetMS,
		StopMS:             stopMS,
		ReplyCore:          strings.TrimSpace(input.ReplyCore),
		ResumeTail:         strings.TrimSpace(input.ResumeTail),
		FinalText:          strings.TrimSpace(input.FinalText),
		TargetSeconds:      input.TargetSeconds,
		TTSRate:            input.TTSRate,
		ActualDurationMS:   snapshot.Task.DurationMS,
		QualityScore:       input.QualityScore,
		QualityLevel:       strings.TrimSpace(input.QualityLevel),
		QualitySummary:     strings.TrimSpace(input.QualitySummary),
		QualityIssues:      append([]string(nil), input.QualityIssues...),
		QualityAttempts:    input.QualityAttempts,
	}
	if hasPoint {
		record.StopSafePointID = point.ID
		record.StopGrade = point.Grade
		record.StopKind = point.Kind
		record.StopText = point.LeftPreview
		record.ResumeText = point.NextPreview
		if resolved.ResumeText != "" {
			record.ResumeText = resolved.ResumeText
		}
	}

	state.mu.Lock()
	meta := &audioInteractionMeta{
		RoomID:       input.RoomID,
		Topic:        strings.TrimSpace(strings.ToUpper(input.Topic)),
		ResumeMode:   strings.TrimSpace(strings.ToUpper(input.ResumeMode)),
		ResumeUnit:   strings.TrimSpace(input.ResumeUnit),
		TextDigest:   strings.TrimSpace(input.TextDigest),
		BridgeDigest: strings.TrimSpace(input.BridgeDigest),
		SkipUnits:    append([]string(nil), input.SkipUnits...),
		Record:       record,
	}
	state.interactions[snapshot.Task.ID] = meta
	state.history[input.RoomID] = append(state.history[input.RoomID], record)
	if len(state.history[input.RoomID]) > 100 {
		state.history[input.RoomID] = state.history[input.RoomID][len(state.history[input.RoomID])-100:]
	}
	state.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"program": snapshot,
		"task":    snapshot.Task,
		"record":  record,
	})
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
	state.mu.Lock()
	state.events[event.SpeechTaskID] = event
	meta := state.interactions[event.SpeechTaskID]
	var pinAnswer bool
	var pinResume bool
	var completedRecord *audioInteractionRecord
	if meta != nil {
		if meta.Record != nil {
			meta.Record.Status = event.Status
		}
		if (event.Status == "PLAYING" || event.Status == "PROGRESS") && !meta.AnswerPinned {
			meta.AnswerPinned = true
			pinAnswer = true
			if meta.Record != nil && meta.Record.StartedAt == nil {
				at := event.OccurredAt
				meta.Record.StartedAt = &at
			}
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
	if (event.Status == "COMPLETED" || event.Status == "FAILED") && meta != nil {
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
		if s.speechRuntime != nil {
			status := speechruntime.StatusPlaying
			switch event.Status {
			case "COMPLETED":
				status = speechruntime.StatusCompleted
			case "FAILED":
				status = speechruntime.StatusFailed
			case "READY":
				status = speechruntime.StatusReady
			}
			var switchAtMS *int
			var startedAt *time.Time
			if current, snapshotErr := s.speechRuntime.Snapshot(meta.RoomID); snapshotErr == nil {
				switchAtMS = current.Interrupt.SwitchAtMS
				startedAt = current.Interrupt.StartedAt
			}
			if (event.Status == "PLAYING" || event.Status == "PROGRESS") && !event.OccurredAt.IsZero() {
				at := event.OccurredAt.UTC()
				startedAt = &at
			}
			_, _ = s.speechRuntime.Update(meta.RoomID, speechruntime.UpdateInput{
				Track:        speechruntime.TrackInterrupt,
				Status:       status,
				Text:         meta.ReplyText,
				QuestionText: meta.QuestionText,
				ReplyText:    meta.ReplyText,
				Source:       meta.Source,
				AudioURL:     meta.AudioURL,
				DecisionID:   meta.DecisionID,
				SpeechTaskID: event.SpeechTaskID,
				SwitchAtMS:   switchAtMS,
				StartedAt:    startedAt,
			})
		}
		if event.Status == "COMPLETED" && s.agentDecisions != nil {
			_, _ = s.agentDecisions.Complete(meta.RoomID, meta.DecisionID)
		}
		if event.Status == "FAILED" && s.agentDecisions != nil {
			_, _ = s.agentDecisions.Release(meta.RoomID, meta.DecisionID)
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
			}
			if len(meta.SkipUnits) > 0 {
				metadata["skip_units"] = strings.Join(meta.SkipUnits, ",")
			}
			spend := timeline.DebtKind("")
			cooldown := time.Duration(0)
			if meta.Topic != "" {
				spend = timeline.DebtQuestion
				cooldown = 2 * time.Minute
			}
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
