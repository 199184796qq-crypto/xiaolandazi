package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestSelectLiveAgentPlanWorkspaceVersionPrefersNewerDraft(t *testing.T) {
	items := []model.LiveAgentPlanVersion{
		{ID: 5, VersionNo: 5, LifecycleStatus: "draft"},
		{ID: 4, VersionNo: 4, LifecycleStatus: "published"},
		{ID: 3, VersionNo: 3, LifecycleStatus: "superseded"},
	}
	version, source := selectLiveAgentPlanWorkspaceVersion(items)
	if version == nil || version.ID != 5 || source != "draft" {
		t.Fatalf("version=%+v source=%q, want V5 draft", version, source)
	}
}

func TestSelectLiveAgentPlanWorkspaceVersionIgnoresStaleDraft(t *testing.T) {
	items := []model.LiveAgentPlanVersion{
		{ID: 8, VersionNo: 8, LifecycleStatus: "published"},
		{ID: 7, VersionNo: 7, LifecycleStatus: "draft"},
		{ID: 6, VersionNo: 6, LifecycleStatus: "superseded"},
	}
	version, source := selectLiveAgentPlanWorkspaceVersion(items)
	if version == nil || version.ID != 8 || source != "published" {
		t.Fatalf("version=%+v source=%q, want V8 published", version, source)
	}
}

func TestSelectLatestLiveAgentPlanWorkspaceTemplateUsesMostRecentUsableVersion(t *testing.T) {
	items := []model.LiveAgentPlanVersion{
		{ID: 21, RoomID: 20, VersionNo: 1, LifecycleStatus: "published"},
		{ID: 19, RoomID: 15, VersionNo: 8, LifecycleStatus: "superseded"},
		{ID: 18, RoomID: 15, VersionNo: 7, LifecycleStatus: "draft"},
	}
	version, source := selectLatestLiveAgentPlanWorkspaceTemplate(items)
	if version == nil || version.ID != 21 || source != "published" {
		t.Fatalf("version=%+v source=%q, want most recent usable version id=21 published", version, source)
	}
}

func TestValidateLiveAgentPlanVersionKeepsNonFormalDraftWithoutAudio(t *testing.T) {
	input := model.CreateLiveAgentPlanVersionInput{
		RoomID:          9,
		DurationMinutes: 60,
		RoundMinutes:    6,
		VoiceIdentity: model.LiveAgentVoiceIdentity{
			Name: "测试主播", Version: "V1", Source: "official",
			Provider: "aliyun_qwen", VoiceID: "longanlingxin",
			Model: "qwen-audio-3.0-tts-plus", Rate: 1,
		},
		Variants: []model.LiveAgentPlanVersionVariant{
			{
				Index: 1, VariantKey: "A", IsFormal: true, Text: "正式稿。",
				AudioAssetID: 12, AudioDurationMS: 2000,
				Timeline: []model.LiveAgentPlanTimelineSegment{
					{SegmentID: "A-001", Index: 1, StartMS: 0, EndMS: 2000, Text: "正式稿。", SafeCut: true},
				},
				SRT: "1\n00:00:00,000 --> 00:00:02,000\n正式稿。\n",
				SafePoints: []model.LiveAgentPlanSafePoint{
					{ID: "SP001", CutMS: 2000, Score: 96, Grade: "A", Kind: "SENTENCE", SentenceID: "A-001", LeftPreview: "正式稿。"},
				},
				AssetManifest: map[string]any{"version": "formal-voice-bundle-v3"},
			},
			{
				Index: 2, VariantKey: "B", IsFormal: false, Text: "尚未选为正式稿",
			},
		},
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		t.Fatal(err)
	}
	if input.Variants[1].AudioAssetID != 0 || len(input.Variants[1].Timeline) != 0 {
		t.Fatalf("non-formal draft unexpectedly retained audio: %+v", input.Variants[1])
	}
}

func TestValidateLiveAgentPlanVersionAllowsIndependentRealtimeVoice(t *testing.T) {
	input := model.CreateLiveAgentPlanVersionInput{
		RoomID:          15,
		DurationMinutes: 60,
		RoundMinutes:    6,
		VoiceIdentity: model.LiveAgentVoiceIdentity{
			Name: "互动声音", Version: "V1", Source: "clone",
			Provider: "qwen_audio", VoiceID: "flash-voice-id", ProfileID: 2, BindingID: 9,
			Model: "qwen-audio-3.1-tts-flash", Rate: 1.08,
		},
		Variants: []model.LiveAgentPlanVersionVariant{
			{
				Index: 1, VariantKey: "CUSTOM", IsFormal: true, Text: "这是预先录制好的主线。",
				AudioAssetID: 19, AudioDurationMS: 2000,
				VoiceIdentityKey: "recorded-mainline-independent-voice",
				Timeline: []model.LiveAgentPlanTimelineSegment{
					{SegmentID: "CUSTOM-001", Index: 1, StartMS: 0, EndMS: 2000, Text: "这是预先录制好的主线。", SafeCut: true},
				},
				SRT: "1\n00:00:00,000 --> 00:00:02,000\n这是预先录制好的主线。\n",
				SafePoints: []model.LiveAgentPlanSafePoint{
					{ID: "SP001", CutMS: 2000, Score: 96, Grade: "A", Kind: "SENTENCE", SentenceID: "CUSTOM-001", LeftPreview: "这是预先录制好的主线。"},
				},
				AssetManifest: map[string]any{"version": "formal-voice-bundle-v3"},
			},
		},
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		t.Fatalf("independent realtime voice rejected: %v", err)
	}
	if got := input.Variants[0].VoiceIdentityKey; got != "recorded-mainline-independent-voice" {
		t.Fatalf("mainline voice identity key changed: %q", got)
	}
}
