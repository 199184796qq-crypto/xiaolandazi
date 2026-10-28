package httpapi

import (
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestPublishedVoiceRateHundredths(t *testing.T) {
	for cents := 80; cents <= 120; cents++ {
		rate := float64(cents) / 100
		if got, ok := normalizePublishedVoiceRate(rate); !ok || got != rate {
			t.Fatalf("published rate lost precision: rate=%v got=%v ok=%v", rate, got, ok)
		}
	}
	for _, rate := range []float64{0.79, 1.21, 1.015, math.NaN(), math.Inf(1)} {
		if _, ok := normalizePublishedVoiceRate(rate); ok {
			t.Fatalf("invalid published rate accepted: %v", rate)
		}
	}
}

func TestHundredthVoiceRatesKeepSeparateAudioCaches(t *testing.T) {
	voiceKeys, libraryKeys := map[string]bool{}, map[string]bool{}
	var contentKey string
	for _, rate := range []float64{0.99, 1, 1.01, 1.02} {
		version := model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{VoiceID: "fixture-voice", Model: "fixture-model", Rate: rate}}
		voice, content := recordingCurrentCacheIdentity(version, []string{"fixture"}, nil)
		library := libraryVoiceSHA(&anchorcycle.State{Config: anchorcycle.Config{TTS: &anchorcycle.VoiceConfig{VoiceID: "fixture-voice", Model: "fixture-model", Rate: rate}}})
		if voiceKeys[voice] || libraryKeys[library] {
			t.Fatalf("different rate reused an audio cache: rate=%v", rate)
		}
		voiceKeys[voice], libraryKeys[library] = true, true
		if contentKey != "" && content != contentKey {
			t.Fatal("changing only the voice rate invalidated text content")
		}
		contentKey = content
	}
}

func TestPlanVoiceRateRequiresAuthentication(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/live-agent-plans/1/voice-rate", strings.NewReader(`{"rate":1.1}`))
		s.liveAgentPlanVoiceRate(w, r)
		if w.Code != 401 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
}
