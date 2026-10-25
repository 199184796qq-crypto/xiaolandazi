package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/ttsgateway"
)

// Repeated wording in two distinct parts of an accepted realtime candidate
// is legal. Their business IDs must differ, while retrying either part keeps
// its own ID even if the payload changes after an unknown response.
func TestLiveCostRealtimeIdenticalPartsHaveDistinctStableIdentities(t *testing.T) {
	wav, _, _, err := combineGeneratedVoiceWAV("fixture", []generatedVoiceWAVPiece{{text: "测试。", fmtData: testPCMFormat(1000), pcmData: make([]byte, 1000), byteRate: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer audio.Close()
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: audio.URL}}
	gateway := ttsgateway.New(provider.Name(), "model")
	gateway.Register(provider)
	part := strings.Repeat("讲", 31) + "。"
	text := part + part
	if parts := splitFullShowVoiceText(text); len(parts) != 2 || parts[0] != parts[1] {
		t.Fatalf("fixture must have two identical parts: %v", parts)
	}
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 999, RunID: "realtime", Key: "accepted-candidate", Priority: livecost.PriorityNecessary, Reason: "已采纳主线声音"})
	_, _, _, err = synthesizeMainlinePreview(ctx, journaledMainlineSynthesizer{s: s, gateway: gateway}, anchorcycle.VoiceConfig{VoiceID: "voice", Model: "model", Rate: 1}, text, "accepted-candidate")
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 2 || len(store.requests) != 2 || store.requests[0].Key == store.requests[1].Key {
		t.Fatalf("two separate part positions collided: calls=%d requests=%+v", provider.calls.Load(), store.requests)
	}
}
