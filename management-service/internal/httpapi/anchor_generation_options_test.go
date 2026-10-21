package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorgeneration"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

func TestRealtimeCustomerOptionsStrictBoundsAndWhitelist(t *testing.T) {
	defaults := realtimeOptionsDefaults(anchorgeneration.Default())
	valid := `{"buffer_seconds":45,"body_min":3,"body_max":5,"max_chars":400}`
	options, err := parseRealtimeOptions(json.RawMessage(valid), defaults)
	if err != nil || options.BufferSeconds != 45 || options.BodyMin != 3 || options.BodyMax != 5 || options.MaxChars != 400 {
		t.Fatal("override failed", err)
	}
	got, err := parseRealtimeOptions(nil, defaults)
	if err != nil || got != defaults {
		t.Fatal("old client defaults lost", err)
	}
	for _, raw := range []string{
		`null`, `{}`, `[]`, `"text"`, valid + `{}`,
		`{"buffer_seconds":19,"body_min":2,"body_max":6,"max_chars":300}`,
		`{"buffer_seconds":601,"body_min":2,"body_max":6,"max_chars":300}`,
		`{"buffer_seconds":120,"body_min":1,"body_max":6,"max_chars":300}`,
		`{"buffer_seconds":120,"body_min":5,"body_max":3,"max_chars":300}`,
		`{"buffer_seconds":120,"body_min":2,"body_max":7,"max_chars":300}`,
		`{"buffer_seconds":120,"body_min":2,"body_max":6,"max_chars":79}`,
		`{"buffer_seconds":120,"body_min":2,"body_max":6,"max_chars":1001}`,
		`{"buffer_seconds":120.5,"body_min":2,"body_max":6,"max_chars":300}`,
		`{"buffer_seconds":"120","body_min":2,"body_max":6,"max_chars":300}`,
		strings.TrimSuffix(valid, "}") + `,"credential_profile_id":"fake"}`,
		strings.TrimSuffix(valid, "}") + `,"threshold":0}`,
		strings.TrimSuffix(valid, "}") + `,"tts":{"voice_id":"fake"}}`,
	} {
		if _, err := parseRealtimeOptions(json.RawMessage(raw), defaults); err == nil {
			t.Fatalf("unsafe options accepted: %s", raw)
		}
	}
}

func TestRealtimeOptionsIsolatedCopiesAndInternalSettingsStayServerOwned(t *testing.T) {
	settings := anchorgeneration.Default()
	settings.Cycle.CredentialID, settings.Cycle.WorkspaceID = "credential", "workspace"
	settings.Cycle.TTS = &anchorcycle.VoiceConfig{VoiceID: "saved-voice", BufferSeconds: 120}
	a := applyRealtimeOptions(settings.Cycle, realtimeGenerationOptions{30, 2, 4, 200, ""})
	b := applyRealtimeOptions(settings.Cycle, realtimeGenerationOptions{90, 3, 6, 500, "他们家"})
	if a.TTS == b.TTS || a.TTS == settings.Cycle.TTS {
		t.Fatal("shared voice config mutated")
	}
	if settings.Cycle.MaxChars != 300 || settings.Cycle.TTS.BufferSeconds != 120 {
		t.Fatal("platform defaults mutated")
	}
	if a.MaxChars != 200 || b.MaxChars != 500 || a.TTS.BufferSeconds != 30 || b.TTS.BufferSeconds != 90 {
		t.Fatal("room options leaked")
	}
	if a.CredentialID != "credential" || b.WorkspaceID != "workspace" || a.Threshold != 40 || a.MaxRetries != 3 || a.TTS.VoiceID != "saved-voice" {
		t.Fatal("internal controls overridden")
	}
	// An existing run's snapshot does not follow the next version's values.
	aSnapshot := a
	b.MaxChars = 600
	if aSnapshot.MaxChars != 200 {
		t.Fatal("old snapshot changed")
	}
	raw, _ := json.Marshal(realtimeOptionsDefaults(settings))
	if strings.Contains(string(raw), "credential") || strings.Contains(string(raw), "workspace") || strings.Contains(string(raw), "saved-voice") {
		t.Fatal("secret/internal settings projected")
	}
}

func TestRealtimeOptionsVersionContextAndStaticModes(t *testing.T) {
	defaults := realtimeOptionsDefaults(anchorgeneration.Default())
	options, err := realtimeOptionsFromContext(map[string]any{"anchor_generation_options": map[string]any{"buffer_seconds": 60, "body_min": 2, "body_max": 5, "max_chars": 240}}, defaults)
	if err != nil || options.MaxChars != 240 {
		t.Fatal(err)
	}
	if _, err = realtimeOptionsFromContext(map[string]any{"anchor_generation_options": nil}, defaults); err == nil {
		t.Fatal("null override accepted")
	}
	input := model.CreateLiveAgentPlanVersionInput{GenerationContext: map[string]any{"content_mode": "ai_pregenerated", "anchor_generation": map[string]any{"config": "fake"}, "anchor_generation_options": options}}
	if err = (&Server{}).captureRealtimeScheme(context.Background(), 1, 2, &input); err != nil {
		t.Fatal(err)
	}
	if _, exists := input.GenerationContext["anchor_generation_options"]; exists {
		t.Fatal("static scheme retained realtime overrides")
	}
	if _, exists := input.GenerationContext["anchor_generation"]; exists {
		t.Fatal("client internal snapshot accepted")
	}
}

func TestRealtimeOptionsReadRequiresAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	w := httptest.NewRecorder()
	s.liveRealtimeGenerationOptions(w, httptest.NewRequest("GET", "/api/v1/live-agent-plans/1/realtime-options", nil))
	if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unauthenticated settings exposed")
	}
}
