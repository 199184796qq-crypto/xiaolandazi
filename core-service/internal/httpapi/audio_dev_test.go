package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/timeline"
)

type fakeAudioTaskClient struct {
	input            audioout.CreateTestTaskInput
	interactionInput audioout.InsertInteractionInput
	completedRoomID  int64
	completedTaskID  string
}

type fakeRoomBrain struct {
	pins   []timeline.Pin
	spends []timeline.DebtKind
}

func (f *fakeRoomBrain) Ingest(model.RoomEvent) {}
func (f *fakeRoomBrain) Snapshot(roomID int64) (roombrain.View, error) {
	return roombrain.View{RoomID: roomID}, nil
}
func (f *fakeRoomBrain) RecordPin(_ int64, pin timeline.Pin, spend timeline.DebtKind, _ time.Duration) {
	f.pins = append(f.pins, pin)
	f.spends = append(f.spends, spend)
}
func (f *fakeRoomBrain) Reset(int64) {}

func (f *fakeAudioTaskClient) Enabled() bool { return true }

func (f *fakeAudioTaskClient) CreateTestTask(_ context.Context, input audioout.CreateTestTaskInput) (audioout.SpeechTask, error) {
	f.input = input
	return audioout.SpeechTask{
		ID:         "sp-http-1",
		RoomID:     input.RoomID,
		SessionID:  input.SessionID,
		Kind:       "test_tone",
		Label:      input.Label,
		AudioURL:   "http://127.0.0.1:8082/v1/tasks/sp-http-1/audio.wav",
		MimeType:   "audio/wav",
		DurationMS: 1200,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (f *fakeAudioTaskClient) CreateExternalTask(_ context.Context, input audioout.CreateExternalTaskInput) (audioout.SpeechTask, error) {
	return audioout.SpeechTask{
		ID:         "standalone-interaction-1",
		RoomID:     input.RoomID,
		SessionID:  input.SessionID,
		Kind:       "interaction_tts",
		Label:      input.Label,
		AudioURL:   "http://127.0.0.1:8082/v1/tasks/standalone-interaction-1/audio.wav",
		MimeType:   "audio/wav",
		DurationMS: 900,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (f *fakeAudioTaskClient) StartTestProgram(_ context.Context, roomID int64, sessionID, label, _ string) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{
		ProgramID: "program-1",
		RoomID:    roomID,
		Running:   true,
		Sequence:  1,
		Slot:      "A",
		Task: &audioout.SpeechTask{
			ID: "program-task-1", RoomID: roomID, SessionID: sessionID,
			Kind: "test_wav_program", Label: label, DurationMS: 1200,
		},
		ServerTime: time.Now().UTC(),
	}, nil
}

func (f *fakeAudioTaskClient) InsertTestProgramInteraction(_ context.Context, input audioout.InsertInteractionInput) (audioout.RoomProgramSnapshot, error) {
	f.interactionInput = input
	return audioout.RoomProgramSnapshot{
		ProgramID: "program-1",
		RoomID:    input.RoomID,
		Running:   true,
		Suspended: true,
		Sequence:  2,
		Slot:      "B",
		Task: &audioout.SpeechTask{
			ID: "interaction-1", RoomID: input.RoomID, SessionID: input.SessionID,
			Kind: "interaction_tts", Label: input.Label, DurationMS: 900,
			Slot: "B", ProgramID: "program-1", Sequence: 2,
		},
		ServerTime: time.Now().UTC(),
	}, nil
}

func (f *fakeAudioTaskClient) ProgramSnapshot(_ context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	now := time.Now().UTC()
	return audioout.RoomProgramSnapshot{
		ProgramID: "program-1",
		RoomID:    roomID,
		Running:   true,
		Sequence:  1,
		Slot:      "A",
		Task: &audioout.SpeechTask{
			ID: "program-task-1", RoomID: roomID, SessionID: "session-a",
			Kind: "test_wav_program", Label: "mainline", DurationMS: 120000, StartedAt: now,
		},
		ServerTime: now,
	}, nil
}

func (f *fakeAudioTaskClient) CompleteProgramInteraction(_ context.Context, roomID int64, taskID string) (audioout.RoomProgramSnapshot, error) {
	f.completedRoomID = roomID
	f.completedTaskID = taskID
	return f.ProgramSnapshot(context.Background(), roomID)
}

func (f *fakeAudioTaskClient) StopTestProgram(_ context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{ProgramID: "program-1", RoomID: roomID, Running: false, Sequence: 1, Slot: "A", ServerTime: time.Now().UTC()}, nil
}

func TestPublicAudioCompletedResumesInteractionProgram(t *testing.T) {
	client := &fakeAudioTaskClient{}
	server := New(nil, nil, nil, nil, nil, "development", "core-secret")
	hub := audiohub.New()
	server.SetAudioHub(hub)
	server.SetAudioClient(client, "http://core.local")

	task, err := hub.Publish(audiohub.Task{
		ID:         "interaction-public-1",
		RoomID:     44,
		SessionID:  "session-a",
		Kind:       "interaction_tts",
		Label:      "回答",
		AudioURL:   "https://tts.example/reply.wav",
		MimeType:   "audio/wav",
		DurationMS: 900,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := server.audioDevState()
	state.mu.Lock()
	state.interactions[task.ID] = &audioInteractionMeta{RoomID: 44}
	state.mu.Unlock()

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tasks/"+task.ID+"/events",
		strings.NewReader(`{"receiver_id":"browser-a","status":"COMPLETED","progress_ms":900}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if client.completedRoomID != 44 || client.completedTaskID != task.ID {
		t.Fatalf("completion room=%d task=%q", client.completedRoomID, client.completedTaskID)
	}
}

func TestDevAudioRoundTripState(t *testing.T) {
	client := &fakeAudioTaskClient{}
	server := New(nil, nil, nil, nil, nil, "development", "core-secret")
	server.SetAudioClient(client, "http://core.local")

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/dev/audio/test", strings.NewReader(`{"room_id":44,"session_id":"session-a","label":"phase-one","duration_ms":1200}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Core-Token", "core-secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	if client.input.CallbackURL != "http://core.local/internal/v1/dev/audio/events" {
		t.Fatalf("callback=%s", client.input.CallbackURL)
	}
	if client.input.RoomID != 44 || client.input.SessionID != "session-a" {
		t.Fatalf("unexpected input: %#v", client.input)
	}

	callbackBody := `{"speech_task_id":"sp-http-1","room_id":44,"session_id":"session-a","receiver_id":"pc-1","status":"COMPLETED","progress_ms":1200,"occurred_at":"2026-09-24T14:00:00Z"}`
	callback := httptest.NewRequest(http.MethodPost, "/internal/v1/dev/audio/events", strings.NewReader(callbackBody))
	callback.Header.Set("Content-Type", "application/json")
	callback.Header.Set("X-Core-Token", "core-secret")
	callbackRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(callbackRec, callback)
	if callbackRec.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%s", callbackRec.Code, callbackRec.Body.String())
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/internal/v1/dev/audio/tasks/sp-http-1", nil)
	stateReq.Header.Set("X-Core-Token", "core-secret")
	stateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", stateRec.Code, stateRec.Body.String())
	}
	var state struct {
		Status string                 `json:"status"`
		Event  audioout.PlaybackEvent `json:"event"`
	}
	if err := json.Unmarshal(stateRec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != "COMPLETED" || state.Event.ReceiverID != "pc-1" {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestDevAudioInteractionWritesTimelinePins(t *testing.T) {
	t.Setenv("CORE_DEV_INTERACTION_LOG", filepath.Join(t.TempDir(), "interactions.jsonl"))
	client := &fakeAudioTaskClient{}
	brain := &fakeRoomBrain{}
	server := New(nil, nil, nil, nil, nil, "development", "core-secret")
	server.SetAudioClient(client, "http://core.local")
	server.SetRoomBrain(brain)

	body := `{
		"room_id":44,
		"session_id":"session-a",
		"label":"真实TTS互动",
		"audio_url":"https://tts.example/answer.wav",
		"topic":"price",
		"resume_mode":"FUSION_SKIP",
		"resume_unit":"meat",
		"skip_units":["feeding"],
		"text_digest":"reply-digest",
		"bridge_digest":"bridge-digest"
	}`
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/dev/audio/interaction", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Core-Token", "core-secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("interaction status=%d body=%s", rec.Code, rec.Body.String())
	}
	if client.interactionInput.RoomID != 44 ||
		client.interactionInput.AudioURL != "https://tts.example/answer.wav" ||
		client.interactionInput.CallbackURL != "http://core.local/internal/v1/dev/audio/events" {
		t.Fatalf("interaction input=%#v", client.interactionInput)
	}

	playing := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/dev/audio/events",
		strings.NewReader(`{
			"speech_task_id":"interaction-1",
			"room_id":44,
			"session_id":"session-a",
			"receiver_id":"pc-a",
			"status":"PLAYING",
			"occurred_at":"2026-09-25T01:00:00Z"
		}`),
	)
	playing.Header.Set("Content-Type", "application/json")
	playing.Header.Set("X-Core-Token", "core-secret")
	playingRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(playingRec, playing)
	if playingRec.Code != http.StatusOK {
		t.Fatalf("playing callback=%d body=%s", playingRec.Code, playingRec.Body.String())
	}
	if len(brain.pins) != 1 || brain.pins[0].Kind != timeline.PinAnswer {
		t.Fatalf("answer pins=%#v", brain.pins)
	}
	if brain.pins[0].Topic != "PRICE" || brain.pins[0].Strategy != "interaction.fusion_skip" {
		t.Fatalf("answer pin=%#v", brain.pins[0])
	}
	if brain.spends[0] != timeline.DebtQuestion {
		t.Fatalf("answer spend=%q", brain.spends[0])
	}

	completed := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/dev/audio/events",
		strings.NewReader(`{
			"speech_task_id":"interaction-1",
			"room_id":44,
			"session_id":"session-a",
			"receiver_id":"pc-a",
			"status":"COMPLETED",
			"progress_ms":900,
			"occurred_at":"2026-09-25T01:00:01Z"
		}`),
	)
	completed.Header.Set("Content-Type", "application/json")
	completed.Header.Set("X-Core-Token", "core-secret")
	completedRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(completedRec, completed)
	if completedRec.Code != http.StatusOK {
		t.Fatalf("completed callback=%d body=%s", completedRec.Code, completedRec.Body.String())
	}
	if len(brain.pins) != 2 || brain.pins[1].Kind != timeline.PinResume {
		t.Fatalf("resume pins=%#v", brain.pins)
	}
	if brain.pins[1].Strategy != "FUSION_SKIP" || brain.pins[1].Key != "meat" {
		t.Fatalf("resume pin=%#v", brain.pins[1])
	}
	if brain.spends[1] != "" {
		t.Fatalf("resume should not spend debt: %q", brain.spends[1])
	}
}

func TestAudioEventCallbackIsAvailableOutsideDevelopment(t *testing.T) {
	server := New(nil, nil, nil, nil, nil, "production", "core-secret")
	server.SetAudioClient(&fakeAudioTaskClient{}, "http://core.local")
	req := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/audio/events",
		strings.NewReader(`{"speech_task_id":"interaction-prod-1","room_id":44,"session_id":"session-a","receiver_id":"pc-a","status":"COMPLETED","progress_ms":900}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Core-Token", "core-secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("production callback=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDevAudioRequiresInternalToken(t *testing.T) {
	server := New(nil, nil, nil, nil, nil, "development", "core-secret")
	server.SetAudioClient(&fakeAudioTaskClient{}, "http://core.local")
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/dev/audio/test", strings.NewReader(`{"room_id":44}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}
}
