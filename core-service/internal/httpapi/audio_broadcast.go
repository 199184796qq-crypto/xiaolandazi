package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
)

func (s *Server) audioPublic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAudioHub(w http.ResponseWriter) (*audiohub.Hub, bool) {
	if s.audioHub == nil {
		writeError(w, http.StatusServiceUnavailable, "Core声音广播尚未初始化")
		return nil, false
	}
	return s.audioHub, true
}

func (s *Server) registerAudioReceiver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	var input audiohub.Receiver
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "声音终端注册格式错误")
		return
	}
	item, err := hub.RegisterReceiver(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) heartbeatAudioReceiver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "声音终端心跳格式错误")
		return
	}
	item, err := hub.HeartbeatReceiver(r.PathValue("receiverID"), input.RoomID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) unregisterAudioReceiver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "声音终端注销格式错误")
		return
	}
	hub.UnregisterReceiver(r.PathValue("receiverID"), input.RoomID)
	writeJSON(w, http.StatusOK, map[string]any{"unregistered": true})
}

func (s *Server) listAudioReceivers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(r.PathValue("roomID"), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid room_id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hub.RoomReceivers(roomID)})
}

func writeAudioSSE(w http.ResponseWriter, event string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
	return err
}

func (s *Server) streamAudioRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(r.PathValue("roomID"), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid room_id")
		return
	}
	receiverID := strings.TrimSpace(r.URL.Query().Get("receiver_id"))
	receiver, exists := hub.Receiver(receiverID)
	if receiverID == "" || !exists || receiver.RoomID != roomID {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		_ = writeAudioSSE(w, "unregistered", map[string]any{"room_id": roomID})
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	ch, latest, cancel := hub.Subscribe(roomID)
	defer cancel()
	controlCh, cancelControls := hub.SubscribeControls(roomID)
	defer cancelControls()
	_ = writeAudioSSE(w, "connected", map[string]any{
		"room_id":     roomID,
		"receiver_id": receiverID,
		"server_time": time.Now().UTC(),
	})
	if latest != nil {
		_ = writeAudioSSE(w, "task", latest)
	}
	flusher.Flush()

	keepalive := time.NewTicker(12 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case task, open := <-ch:
			if !open {
				return
			}
			if err := writeAudioSSE(w, "task", task); err != nil {
				return
			}
			flusher.Flush()
		case control, open := <-controlCh:
			if !open {
				return
			}
			if err := writeAudioSSE(w, "control", control); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			receiver, exists := hub.Receiver(receiverID)
			if !exists || receiver.RoomID != roomID {
				_ = writeAudioSSE(w, "unregistered", map[string]any{"room_id": roomID})
				flusher.Flush()
				return
			}
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) syncAudioRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(r.PathValue("roomID"), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid room_id")
		return
	}
	task := hub.ActiveTask(roomID)
	payload := map[string]any{
		"room_id":     roomID,
		"running":     task != nil,
		"task":        task,
		"server_time": time.Now().UTC(),
	}
	if task != nil {
		payload["program_id"] = task.ProgramID
		payload["sequence"] = task.Sequence
		payload["slot"] = task.Slot
		payload["suspended"] = task.ProgramID != "" && (task.Kind == "interaction_audio" || task.Kind == "interaction_tts")
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) getAudioTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	snapshot, exists := hub.Snapshot(r.PathValue("taskID"))
	if !exists {
		writeError(w, http.StatusNotFound, "audio task not found")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type coreTestAudioSource interface {
	TestAudio() ([]byte, string, int, bool)
}

func (s *Server) serveCoreTestAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil {
		writeError(w, http.StatusServiceUnavailable, "Core测试音频尚未配置")
		return
	}
	source, ok := state.client.(coreTestAudioSource)
	if !ok {
		writeError(w, http.StatusNotFound, "Core测试音频不可用")
		return
	}
	audio, version, _, available := source.TestAudio()
	if !available || len(audio) == 0 {
		writeError(w, http.StatusNotFound, "Core测试音频不可用")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-store")
	if version != "" {
		w.Header().Set("ETag", `"`+version+`"`)
	}
	http.ServeContent(w, r, "test-audio.wav", time.Time{}, bytes.NewReader(audio))
}

func (s *Server) reportAudioTaskEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hub, ok := s.requireAudioHub(w)
	if !ok {
		return
	}
	var event audiohub.PlaybackEvent
	if err := readJSON(w, r, &event); err != nil {
		writeError(w, http.StatusBadRequest, "声音终端回报格式错误")
		return
	}
	if err := hub.ReportEvent(r.PathValue("taskID"), event); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Core-native browser/device receivers report directly to the audio hub,
	// so feed those events into the same interaction state machine used by the
	// legacy callback path. This makes actual PLAYING/COMPLETED authoritative.
	if snapshot, exists := hub.Snapshot(r.PathValue("taskID")); exists &&
		(strings.EqualFold(strings.TrimSpace(snapshot.Task.Kind), "interaction_audio") ||
			strings.EqualFold(strings.TrimSpace(snapshot.Task.Kind), "interaction_tts")) {
		if state := s.audioDevState(); state != nil {
			occurredAt := event.OccurredAt
			if occurredAt.IsZero() {
				occurredAt = time.Now().UTC()
			}
			s.applyAudioInteractionPlaybackEvent(r.Context(), state, audioout.PlaybackEvent{
				SpeechTaskID: snapshot.Task.ID,
				RoomID:       snapshot.Task.RoomID,
				SessionID:    snapshot.Task.SessionID,
				ReceiverID:   strings.TrimSpace(event.ReceiverID),
				Status:       strings.ToUpper(strings.TrimSpace(event.Status)),
				ProgressMS:   event.ProgressMS,
				Error:        event.Error,
				OccurredAt:   occurredAt,
			})
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}
