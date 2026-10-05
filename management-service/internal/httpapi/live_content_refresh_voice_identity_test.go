package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestContentRefreshVoiceIdentityRejectsMixedOrUnknownAudio(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(map[string]any, *model.LiveAgentVoiceIdentity)
		wantError bool
	}{
		{"matching archive", func(map[string]any, *model.LiveAgentVoiceIdentity) {}, false},
		{"different voice", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_id"] = "old-voice" }, true},
		{"different model", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["tts_model"] = "old-model" }, true},
		{"different rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = 1.0 }, true},
		{"legacy missing rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { delete(m, "voice_rate") }, true},
		{"missing voice", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { delete(m, "voice_id") }, true},
		{"missing model", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { delete(m, "tts_model") }, true},
		{"nonstring voice", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_id"] = 123 }, true},
		{"blank model", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["tts_model"] = "  " }, true},
		{"zero archived rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = 0.0 }, true},
		{"null archived rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = nil }, true},
		{"invalid archived rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = "unknown" }, true},
		{"NaN archived rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = math.NaN() }, true},
		{"infinite archived rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = math.Inf(1) }, true},
		{"invalid expected rate", func(_ map[string]any, v *model.LiveAgentVoiceIdentity) { v.Rate = 0 }, true},
		{"NaN expected rate", func(_ map[string]any, v *model.LiveAgentVoiceIdentity) { v.Rate = math.NaN() }, true},
		{"blank expected model", func(_ map[string]any, v *model.LiveAgentVoiceIdentity) { v.Model = "" }, true},
		{"JSON numeric rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = json.Number("1.1") }, false},
		{"numeric string rate", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = "1.10" }, false},
		{"float32 serialization", func(m map[string]any, _ *model.LiveAgentVoiceIdentity) { m["voice_rate"] = float32(1.1) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := map[string]any{"voice_id": "confirmed-voice", "tts_model": "confirmed-model", "voice_rate": 1.1}
			voice := model.LiveAgentVoiceIdentity{VoiceID: "confirmed-voice", Model: "confirmed-model", Rate: 1.1}
			tt.mutate(metadata, &voice)
			err := validateContentRefreshVoiceIdentity(model.MediaAsset{Metadata: metadata}, voice)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v wantError=%v", err, tt.wantError)
			}
			if err != nil && !strings.Contains(err.Error(), "重新生成") {
				t.Fatalf("failure does not explain safe recovery: %v", err)
			}
		})
	}
}

func TestArchivedVoiceRateDoesNotGuessLegacyDefaults(t *testing.T) {
	for _, value := range []any{nil, 0.0, 0.49, 2.01, "NaN", math.Inf(-1), true, map[string]any{}} {
		if _, ok := archivedVoiceRate(map[string]any{"voice_rate": value}); ok {
			t.Fatalf("invalid rate accepted: %v", value)
		}
	}
	if _, ok := archivedVoiceRate(nil); ok {
		t.Fatal("legacy missing rate silently became 1.0")
	}
	for _, value := range []any{1, int64(1), float64(1), json.Number("1"), "1.0"} {
		if rate, ok := archivedVoiceRate(map[string]any{"voice_rate": value}); !ok || rate != 1 {
			t.Fatalf("numeric archived rate lost: %v rate=%v ok=%v", value, rate, ok)
		}
	}
}

func TestOptionalArchivedVoiceRateKeepsUnknownDistinctFromOne(t *testing.T) {
	if rate, err := optionalArchivedVoiceRate(nil); err != nil || rate != 0 {
		t.Fatalf("legacy archive should stay unknown: rate=%v err=%v", rate, err)
	}
	if rate, err := optionalArchivedVoiceRate([]float64{1}); err != nil || rate != 1 {
		t.Fatalf("explicit actual rate lost: rate=%v err=%v", rate, err)
	}
	for _, rates := range [][]float64{{0}, {0.49}, {2.01}, {math.NaN()}, {math.Inf(1)}, {1, 1}} {
		if _, err := optionalArchivedVoiceRate(rates); err == nil {
			t.Fatalf("invalid actual rates accepted: %v", rates)
		}
	}
}

func TestArchiveFullShowVoiceRejectsInvalidActualRateBeforeStorage(t *testing.T) {
	s := &Server{}
	_, err := s.archiveFullShowVoice(context.Background(), 7, 1, 3, 11, "A", "voice", "model", "official", nil, 1000, nil, math.NaN())
	if err == nil || !strings.Contains(err.Error(), "实际语速") {
		t.Fatalf("invalid rate did not fail before storage: %v", err)
	}
}
