package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func TestVoiceModelPriceCatalogMatchesOfficialRates(t *testing.T) {
	got := map[string]voiceModelPrice{}
	for _, item := range voiceModelPriceCatalog() {
		got[item.Model] = item
	}
	for model, want := range map[string]float64{
		"cosyvoice-v3.5-flash":     0.8,
		"cosyvoice-v3.5-plus":      1.5,
		"qwen-audio-3.0-tts-plus":  1.4,
		"qwen-audio-3.0-tts-flash": 1.0,
		"qwen3-tts-flash":          0.8,
	} {
		if got[model].YuanPer10K != want {
			t.Fatalf("%s price=%v want=%v", model, got[model].YuanPer10K, want)
		}
	}
	if got["qwen-audio-3.1-tts-flash"].Unit != "tokens" || got["qwen-audio-3.1-tts-flash"].InputYuanPerMillion != 1.5 || got["qwen-audio-3.1-tts-flash"].OutputYuanPerMillion != 12 {
		t.Fatalf("token model price=%+v", got["qwen-audio-3.1-tts-flash"])
	}
}

func TestVoiceSettingsRequireAuthenticatedAdministrator(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, handler := range []http.HandlerFunc{s.systemVoiceModelsGet, s.systemVoiceModelsPut, s.liveVoiceSettings} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/voice-models", nil))
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestCrossEngineVoiceRequiresActualSampleReclone(t *testing.T) {
	asset := int64(7)
	p := model.VoiceProfile{ID: 2, TenantID: 14, CloneStatus: "ready", VoiceID: "old-voice", SampleAssetID: &asset, Config: map[string]any{"target_model": qwenCloneModelPlus}}
	b := model.VoiceModelBinding{ID: 9, TenantID: 14, ProfileID: 2, SampleAssetID: 7, Provider: ttsgateway.ProviderCosyVoice, Model: ttsgateway.CosyVoiceFlash, VoiceID: "new-provider-voice", Status: "ready", Config: map[string]any{"sample_recloned": true}}
	input := fullShowVariantVoiceInput{BindingID: 9, Model: b.Model, VoiceID: b.VoiceID}
	if voice, target, err := resolveSelectedCloneVoice(p, input, []model.VoiceModelBinding{b}); err != nil || voice != b.VoiceID || target != b.Model {
		t.Fatal(voice, target, err)
	}
	for _, change := range []func(*model.VoiceModelBinding){
		func(b *model.VoiceModelBinding) { b.Config = nil },
		func(b *model.VoiceModelBinding) { b.SampleAssetID = 8 },
		func(b *model.VoiceModelBinding) { b.VoiceID = p.VoiceID },
		func(b *model.VoiceModelBinding) { b.Provider = ttsgateway.ProviderQwen },
		func(b *model.VoiceModelBinding) { b.TenantID = 15 },
	} {
		bad := b
		change(&bad)
		if _, _, err := resolveSelectedCloneVoice(p, fullShowVariantVoiceInput{BindingID: 9}, []model.VoiceModelBinding{bad}); err == nil {
			t.Fatal("unsafe cross-engine binding accepted", bad)
		}
	}
	if p.VoiceID != "old-voice" || p.Config["target_model"] != qwenCloneModelPlus {
		t.Fatal("original voice was rewritten")
	}
}
