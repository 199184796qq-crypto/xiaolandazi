package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
)

type interactionReturnReader interface {
	InteractionReturnSnapshot(context.Context, int64, string) (audioout.InteractionReturnSnapshot, error)
}

func (s *Server) getInteractionReturn(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenant, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenant, roomID); err != nil {
		writeError(w, 404, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil {
		writeError(w, 409, "audio unavailable")
		return
	}
	reader, ok := state.client.(interactionReturnReader)
	if !ok {
		writeError(w, 409, "deferred return unavailable")
		return
	}
	plan, err := reader.InteractionReturnSnapshot(r.Context(), roomID, r.URL.Query().Get("task_id"))
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	decisionID := r.URL.Query().Get("decision_id")
	if decisionID != "" && plan.DecisionID != decisionID {
		writeError(w, 409, "return decision changed")
		return
	}
	writeJSON(w, 200, plan)
}

func (s *Server) stageInteractionBridge(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenant, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenant, roomID); err != nil {
		writeError(w, 404, "room not found")
		return
	}
	var input struct {
		DecisionID string `json:"decision_id"`
		TaskID     string `json:"task_id"`
		Token      string `json:"token"`
		Text       string `json:"text"`
		AudioURL   string `json:"audio_url"`
	}
	if readJSON(w, r, &input) != nil || input.AudioURL == "" || len([]rune(input.Text)) > 24 {
		writeError(w, 400, "invalid bridge input")
		return
	}
	decision, exists := s.agentDecisions.Get(roomID, input.DecisionID)
	if !exists || decision.Status != "CLAIMED" || (decision.PlaybackTaskID != "" && decision.PlaybackTaskID != input.TaskID) {
		writeError(w, 409, "interaction no longer active")
		return
	}
	state := s.audioDevState()
	if state == nil {
		writeError(w, 409, "audio unavailable")
		return
	}
	reader, ok := state.client.(interactionReturnReader)
	if !ok {
		writeError(w, 409, "deferred return unavailable")
		return
	}
	plan, err := reader.InteractionReturnSnapshot(r.Context(), roomID, input.TaskID)
	if err != nil || plan.DecisionID != input.DecisionID || plan.Token != input.Token || plan.Deadline.IsZero() || !time.Now().Before(plan.Deadline) {
		writeError(w, 409, "bridge scope/deadline changed")
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), plan.Deadline)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, input.AudioURL, nil)
	if err != nil {
		writeError(w, 400, "invalid audio URL")
		return
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		writeError(w, 409, "bridge audio unavailable before deadline")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		writeError(w, 409, "bridge audio unavailable")
		return
	}
	pcm := []byte{}
	collect := func(frame []byte, _ int) error {
		if len(pcm)+len(frame) > roomaudio.PCMBytesPerFrame*400 {
			return fmt.Errorf("bridge audio exceeds eight seconds")
		}
		pcm = append(pcm, frame...)
		return nil
	}
	err = roomaudio.StreamWAVPCM(ctx, io.LimitReader(response.Body, 4<<20), 0, collect)
	if errors.Is(err, roomaudio.ErrPCMNormalizationRequired) {
		pcm = nil
		err = coreaudio.CollectNormalizedPCM(ctx, input.AudioURL, collect)
	}
	if err != nil || len(pcm) == 0 || ctx.Err() != nil {
		writeError(w, 409, "bridge decoding missed deadline")
		return
	}
	ready := coreaudio.BridgeReady{TaskID: input.TaskID, Token: input.Token, Text: input.Text, PCM: pcm, DurationMS: len(pcm) / roomaudio.PCMBytesPerFrame * roomaudio.FrameDurationMS}
	raw, _ := json.Marshal(ready)
	if failures := s.messages.Publish(ctx, messagecaptain.Message{Kind: messagecaptain.ReturnBridgeReady, RoomID: roomID, RunID: plan.ProgramID, TaskID: input.TaskID, Payload: raw}); len(failures) > 0 {
		writeError(w, 409, "bridge no longer accepted")
		return
	}
	writeJSON(w, 200, map[string]any{"staged": true, "duration_ms": ready.DurationMS})
}

func (s *Server) handleReturnPlaybackMessage(_ context.Context, m messagecaptain.Message) error {
	if m.Kind != messagecaptain.ReturnBridgeStaged && m.Kind != messagecaptain.ReturnBridgeStarted {
		return nil
	}
	if s.speechRuntime == nil {
		return nil
	}
	current, err := s.speechRuntime.Snapshot(m.RoomID)
	if err != nil || current.Interrupt.SpeechTaskID != m.TaskID {
		return nil
	}
	var input struct {
		Text       string `json:"text"`
		DurationMS int    `json:"duration_ms"`
	}
	if err = json.Unmarshal(m.Payload, &input); err != nil {
		return err
	}
	part := current.Interrupt
	duration := part.DurationMS
	if input.DurationMS > duration {
		duration = input.DurationMS
	}
	started := part.StartedAt
	_, err = s.speechRuntime.Update(m.RoomID, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: part.Status, Text: part.Text, QuestionText: part.QuestionText, ReplyText: part.ReplyText, ResumeStrategy: part.ResumeStrategy, BridgeText: input.Text, BridgeUsed: m.Kind == messagecaptain.ReturnBridgeStarted, Source: part.Source, AudioURL: part.AudioURL, DecisionID: part.DecisionID, MissionID: part.MissionID, SpeechTaskID: part.SpeechTaskID, SwitchAtMS: part.SwitchAtMS, DurationMS: duration, StartedAt: started})
	if state := s.audioDevState(); state != nil {
		state.mu.Lock()
		if meta := state.interactions[m.TaskID]; meta != nil {
			meta.BridgeText = input.Text
			meta.BridgeUsed = m.Kind == messagecaptain.ReturnBridgeStarted
		}
		state.mu.Unlock()
	}
	return err
}
