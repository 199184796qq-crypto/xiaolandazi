package speechasr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTranscribeAsyncFileTask(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/result.json" && r.Header.Get("Authorization") != "Bearer test-key" {
			got := r.Header.Get("Authorization")
			t.Fatalf("Authorization=%q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/services/audio/asr/transcription":
			if got := r.Header.Get("X-DashScope-Async"); got != "enable" {
				t.Fatalf("X-DashScope-Async=%q", got)
			}
			var payload struct {
				Model string `json:"model"`
				Input struct {
					FileURLs []string `json:"file_urls"`
				} `json:"input"`
				Parameters map[string]any `json:"parameters"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Model != defaultModel {
				t.Fatalf("model=%q", payload.Model)
			}
			if len(payload.Input.FileURLs) != 1 || payload.Input.FileURLs[0] != "https://example.invalid/recording.wav" {
				t.Fatalf("file_urls=%v", payload.Input.FileURLs)
			}
			if payload.Parameters == nil {
				t.Fatal("parameters missing")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"task-1","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/task-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"subtask_status":"SUCCEEDED","transcription_url":"` + server.URL + `/result.json"}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/result.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"transcripts":[{"text":"欢迎来到直播间，今天给大家讲这个产品。","sentences":[{"begin_time":0,"end_time":2100,"text":"欢迎来到直播间"},{"begin_time":2100,"end_time":5200,"text":"今天给大家讲这个产品"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{
		baseURL: server.URL + "/api/v1",
		apiKey:  "test-key",
		model:   defaultModel,
		http:    server.Client(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := client.Transcribe(ctx, "https://example.invalid/recording.wav")
	if err != nil {
		t.Fatalf("Transcribe() error=%v", err)
	}
	if result.TaskID != "task-1" {
		t.Fatalf("TaskID=%q", result.TaskID)
	}
	if !strings.Contains(result.Text, "欢迎来到直播间") {
		t.Fatalf("Text=%q", result.Text)
	}
	if len(result.Sentences) != 2 {
		t.Fatalf("sentences=%d", len(result.Sentences))
	}
}

func TestConfiguredRequiresAPIKey(t *testing.T) {
	client := &Client{baseURL: defaultBaseURL, model: defaultModel, http: http.DefaultClient}
	if err := client.Configured(); err == nil {
		t.Fatal("Configured() accepted empty API key")
	}
}
