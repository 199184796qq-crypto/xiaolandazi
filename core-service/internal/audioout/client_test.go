package audioout

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateTestTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if r.URL.Path != "/internal/v1/rooms/12/sessions/dev-session/tasks/test-tone" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("X-Audio-Token") != "audio-secret" {
			t.Fatalf("missing audio token")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["callback_url"] != "http://127.0.0.1:8081/internal/v1/dev/audio/events" {
			t.Fatalf("callback=%v", body["callback_url"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"speech_task_id":"sp-1","room_id":12,"session_id":"dev-session","kind":"test_tone","label":"测试","audio_url":"http://audio/v1/tasks/sp-1/audio.wav","mime_type":"audio/wav","duration_ms":2000,"created_at":"2026-09-24T14:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "audio-secret")
	task, err := client.CreateTestTask(t.Context(), CreateTestTaskInput{
		RoomID:      12,
		SessionID:   "dev-session",
		Label:       "测试",
		DurationMS:  2000,
		CallbackURL: "http://127.0.0.1:8081/internal/v1/dev/audio/events",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "sp-1" || task.RoomID != 12 || task.AudioURL == "" {
		t.Fatalf("unexpected task: %#v", task)
	}
}
