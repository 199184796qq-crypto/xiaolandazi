package ttsgateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayQwenPreservesHundredthVoiceRate(t *testing.T) {
	for _, rate := range []float64{0.99, 1, 1.01, 1.02, 1.2, 1.21, 1.39, 1.4} {
		t.Run(fmt.Sprintf("%.2f", rate), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Input struct {
						Rate float64 `json:"rate"`
					} `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if payload.Input.Rate != rate {
					t.Errorf("provider received rate %v, want %v", payload.Input.Rate, rate)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"output":{"audio":{"url":"http://fixture.invalid/audio.wav"}}}`))
			}))
			defer server.Close()
			gateway := New(ProviderQwen, "qwen-audio-3.1-tts-flash")
			gateway.Register(NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: server.URL, Client: server.Client()}))
			result, err := gateway.SynthesizeURL(t.Context(), SynthesizeRequest{VoiceID: "fixture-voice", Text: "fixture", Rate: rate})
			if err != nil || result.Rate != rate {
				t.Fatalf("rate changed through gateway: result=%+v err=%v", result, err)
			}
		})
	}
}
