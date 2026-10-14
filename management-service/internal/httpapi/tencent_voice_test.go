package httpapi

import (
	"livecompanion/management/internal/ttsgateway"
	"net/http/httptest"
	"testing"
)

func TestTencentFormalVoiceUsesServerCatalogueAndKeepsClonesSeparate(t *testing.T) {
	s := &Server{}
	id, model, err := s.resolveFormalVoice(t.Context(), 14, fullShowVariantVoiceInput{Source: "official", VoiceID: "101030", Model: "cosyvoice-v3.5-flash"})
	if err != nil || id != "101030" || model != ttsgateway.TencentPremiumModel {
		t.Fatal(id, model, err)
	}
	if _, _, err := s.resolveFormalVoice(t.Context(), 14, fullShowVariantVoiceInput{Source: "official", VoiceID: "402000"}); err == nil {
		t.Fatal("another Tencent tier accepted")
	}
	if _, err := normalizeVoiceCloneModel(ttsgateway.TencentPremiumModel); err == nil {
		t.Fatal("official voice treated as clone")
	}
	r := httptest.NewRequest("GET", "/api/v1/live/official-voices?model=tencent-premium", nil)
	w := httptest.NewRecorder()
	if _, ok := voiceLibraryQuery(w, r); !ok {
		t.Fatal("Tencent official library rejected")
	}
	if ttsgateway.VoiceLibraryMatches(ttsgateway.ProviderQwen, ttsgateway.TencentPremiumModel) {
		t.Fatal("Tencent leaked into custom Qwen library")
	}
}
