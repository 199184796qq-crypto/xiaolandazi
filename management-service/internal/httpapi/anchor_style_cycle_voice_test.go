package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func TestCycleVoiceCompleteReplyIsNormalProtocolState(t *testing.T) {
	recorder := httptest.NewRecorder()
	cycleVoiceCompleteReply(recorder, "run-one", "candidate-one")
	var body struct {
		RunID           string                  `json:"run_id"`
		CandidateID     string                  `json:"candidate_id"`
		Voice           *anchorcycle.VoiceAsset `json:"voice"`
		PreviewComplete bool                    `json:"preview_complete"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || body.RunID != "run-one" || body.CandidateID != "candidate-one" || body.Voice != nil || !body.PreviewComplete {
		t.Fatal("unexpected completion response", recorder.Code, body)
	}
}

func TestCycleVoiceBrowserProjectionUsesScopedSameOriginContentURL(t *testing.T) {
	state := cycleTestState(t)
	state.Surface = "generation_preview"
	state.PlanID = 41
	state.RunID = "0123456789abcdef0123456789abcdef"
	state.Accepted = []anchorcycle.Segment{{
		Selected: anchorcycle.Candidate{ID: "abcdef0123456789abcdef0123456789", Text: "已采纳话术。"},
		Voice: &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{
			AudioURL: "https://private-oss.example.invalid/signed.wav?secret=must-not-leak",
		}},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/live-agent-plans/41/realtime-preview?tenant_id=14", nil)
	req.Header.Set("X-Live-Support-Room-ID", "9")
	projected, ok := cycleStateForBrowser(req, &state).Accepted[0].Voice, true
	if !ok || projected == nil {
		t.Fatal("missing projected voice")
	}
	want := "/api/v1/live-agent-plans/41/realtime-preview/0123456789abcdef0123456789abcdef/voices/abcdef0123456789abcdef0123456789/content?support_room_id=9&tenant_id=14"
	if projected.Bundle.AudioURL != want || strings.Contains(projected.Bundle.AudioURL, "oss") || strings.Contains(projected.Bundle.AudioURL, "secret") {
		t.Fatalf("browser audio URL escaped same-origin proxy: %q", projected.Bundle.AudioURL)
	}
}

func TestCycleVoiceCacheObjectRejectsForeignOrMalformedObjects(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	valid := &anchorcycle.VoiceAsset{Status: "ready", CacheID: id, Bundle: model.LiveAgentPlanVersionVariant{AssetManifest: map[string]any{
		"storage_driver":   "oss",
		"storage_bucket":   "private-bucket",
		"audio_object_key": "anchor-voice-cache/v1/realm/t14/" + id + "/audio.wav",
	}}}
	key, bucket, err := cycleVoiceCacheObject(valid)
	if err != nil || bucket != "private-bucket" || !strings.HasSuffix(key, "/"+id+"/audio.wav") {
		t.Fatalf("valid cache object rejected: key=%q bucket=%q err=%v", key, bucket, err)
	}
	for name, mutate := range map[string]func(*anchorcycle.VoiceAsset){
		"public driver": func(v *anchorcycle.VoiceAsset) { v.Bundle.AssetManifest["storage_driver"] = "local" },
		"foreign key":   func(v *anchorcycle.VoiceAsset) { v.Bundle.AssetManifest["audio_object_key"] = "tenants/14/private.wav" },
		"traversal": func(v *anchorcycle.VoiceAsset) {
			v.Bundle.AssetManifest["audio_object_key"] = "anchor-voice-cache/v1/../" + id + "/audio.wav"
		},
		"wrong id": func(v *anchorcycle.VoiceAsset) { v.CacheID = "fedcba9876543210fedcba9876543210" },
	} {
		t.Run(name, func(t *testing.T) {
			clone := *valid
			clone.Bundle = valid.Bundle
			clone.Bundle.AssetManifest = map[string]any{}
			for key, value := range valid.Bundle.AssetManifest {
				clone.Bundle.AssetManifest[key] = value
			}
			mutate(&clone)
			if _, _, err := cycleVoiceCacheObject(&clone); err == nil {
				t.Fatal("unsafe cache object accepted")
			}
		})
	}
}

type testPreviewSynth struct {
	url      string
	requests []ttsgateway.SynthesizeRequest
}

func (f *testPreviewSynth) SynthesizeURL(_ context.Context, r ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	f.requests = append(f.requests, r)
	return ttsgateway.SynthesizeResponse{AudioURL: f.url}, nil
}

func TestCycleTTSUsesFixedAudioTimelineSRTAndSemanticContract(t *testing.T) {
	format := testPCMFormat(1000)
	wav, _, _, err := combineGeneratedVoiceWAV("fixture", []generatedVoiceWAVPiece{{text: "测试。", fmtData: format, pcmData: make([]byte, 1000), byteRate: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer server.Close()
	gateway := &testPreviewSynth{url: server.URL}
	text := strings.Repeat("这一句是待采纳主播的话术，要保留正常的语义句末。", 5)
	voice := anchorcycle.VoiceConfig{VoiceID: "selected-voice", Model: "selected-model", Rate: 1.1}
	raw, timeline, duration, err := synthesizeMainlinePreview(context.Background(), gateway, voice, text, "candidate-one")
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.requests) < 2 || duration != int64(len(gateway.requests))*(1000+generatedVoiceSegmentHeadMS+generatedVoiceSegmentTailMS) {
		t.Fatal("not PCM-derived", duration)
	}
	for _, r := range gateway.requests {
		if r.VoiceID != voice.VoiceID || r.Model != voice.Model || r.Rate != voice.Rate {
			t.Fatal("changed selected voice", r)
		}
	}
	if got, err := generatedVoiceWAVDurationMS(raw); err != nil || got != duration {
		t.Fatal(got, err)
	}
	variant := model.LiveAgentPlanVersionVariant{VariantKey: "test-candidate", AudioDurationMS: duration, Timeline: timeline, SRT: liveAgentTimelineSRT(timeline), SafePoints: buildFullShowSafePoints(timeline), AssetManifest: map[string]any{"version": "formal-voice-bundle-v3", "usage": "test_preview", "published": false}}
	if err := validateLiveAgentVariantTimeline(variant); err != nil {
		t.Fatal("not compatible with mainline timeline", err)
	}
	if err := validateLiveAgentVariantSchedulingAssets(variant); err != nil {
		t.Fatal("not compatible with mainline semantics", err)
	}
	if variant.IsFormal || contentTimelineText(timeline) != text {
		t.Fatal("preview was formal or changed text")
	}
}

func TestCyclePromptCarriesExactHookAndBodyPlanOnRetries(t *testing.T) {
	s := cycleTestState(t)
	s.Config.Rolling = true
	s.Config.BodyMin = 2
	s.Config.BodyMax = 6
	s.BodyTarget = 4
	s.Phase = "body"
	s.PriceHook = &anchorcycle.PriceHook{Task: s.Queue[0], Promise: "下一段算两桶多少钱", SourceText: "已经采纳的价格钩子原稿"}
	s.Attempts = []anchorcycle.Candidate{{ContentIssue: "price_hook_unresolved"}}
	prompt, err := renderAnchorCyclePrompt(&s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"下一段算两桶多少钱", "已经采纳的价格钩子原稿", "4段", "2～6段", "price_hook_unresolved", "塑价兑现"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "{{") {
		t.Fatal("unfilled template")
	}
}
