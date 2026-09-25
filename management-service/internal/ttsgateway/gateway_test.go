package ttsgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }
func (stubProvider) SynthesizeURL(_ context.Context, request SynthesizeRequest) (SynthesizeResponse, error) {
	return SynthesizeResponse{AudioURL: "http://audio", Provider: "stub", Model: request.Model, VoiceID: request.VoiceID, Rate: request.Rate}, nil
}
func (stubProvider) CloneVoice(_ context.Context, request CloneRequest) (CloneResponse, error) {
	return CloneResponse{VoiceID: "voice-1", Provider: "stub", Model: request.TargetModel}, nil
}

func TestGatewayAlias(t *testing.T) {
	g := New("stub_alias", "default-model")
	g.Register(stubProvider{}, "stub_alias")
	out, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Model: "m", VoiceID: "v", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Provider != "stub" || out.AudioURL == "" {
		t.Fatalf("unexpected output: %#v", out)
	}
	if out.Rate != 1.0 {
		t.Fatalf("default rate=%v", out.Rate)
	}
}

func TestGatewayRejectsOutOfRangeRate(t *testing.T) {
	g := New("stub", "default-model")
	g.Register(stubProvider{})
	if _, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{
		Model: "m", VoiceID: "v", Text: "hello", Rate: 2.1,
	}); err == nil {
		t.Fatal("expected rate validation error")
	}
}

func TestQwenProviderSynthesizeURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		input, _ := payload["input"].(map[string]any)
		if input["rate"] != 1.2 {
			t.Fatalf("rate=%v", input["rate"])
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"output\":{\"audio\":{\"url\":\"http://example/audio.wav\"}}}"))
	}))
	defer server.Close()
	p := NewQwenProvider(QwenConfig{APIKey: "secret", TTSBaseURL: server.URL, CustomizationURL: server.URL, Client: server.Client()})
	out, err := p.SynthesizeURL(t.Context(), SynthesizeRequest{Model: "model", VoiceID: "voice", Text: "hello", Rate: 1.2})
	if err != nil {
		t.Fatal(err)
	}
	if out.AudioURL != "http://example/audio.wav" {
		t.Fatalf("url=%s", out.AudioURL)
	}
	if out.Rate != 1.2 {
		t.Fatalf("rate=%v", out.Rate)
	}
}
