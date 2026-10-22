package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestSplitRecordingStreamTextHonorsFortyToEightyRunesAndPunctuation(t *testing.T) {
	text := strings.Repeat("这是录音里的一句话，价格和数字不能改。", 6) + strings.Repeat("最后一段没有标点", 4)
	segments := splitRecordingStreamText(text)
	if len(segments) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segments))
	}
	var rebuilt strings.Builder
	for _, segment := range segments {
		count := len([]rune(segment.Text))
		if count > 80 {
			t.Fatalf("segment too long: %d %q", count, segment.Text)
		}
		if count < 40 && len(segments) > 1 {
			// Only the tail may be shorter than 40 runes.
			t.Fatalf("non-tail segment too short: %d %q", count, segment.Text)
		}
		rebuilt.WriteString(segment.Text)
	}
	if strings.ReplaceAll(rebuilt.String(), " ", "") != strings.ReplaceAll(text, " ", "") {
		t.Fatalf("split lost text: %q", rebuilt.String())
	}
}

func TestRecordingVoiceModeOnlyReadsExplicitStreamMode(t *testing.T) {
	version := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": "user_audio"}}
	if mode := recordingVoiceMode(version); mode != "" {
		t.Fatalf("missing flag must stay original, got %q", mode)
	}
	version.GenerationContext["recording_voice_mode"] = recordingStreamMode
	if mode := recordingVoiceMode(version); mode != recordingStreamMode {
		t.Fatalf("stream flag lost, got %q", mode)
	}
	version.GenerationContext["content_mode"] = "ai_dynamic"
	if mode := recordingVoiceMode(version); mode != "" {
		t.Fatalf("non-recording mode must ignore stream flag, got %q", mode)
	}
}

func TestRecordingStreamOwnerAllowsUserAudioOnlyForStreamMode(t *testing.T) {
	original := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": "user_audio"}}
	if liveAnchorOwnerModeAllowed("user_audio", original) {
		t.Fatal("original recording must not enter the stream owner")
	}
	stream := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": recordingStreamMode}}
	if !liveAnchorOwnerModeAllowed("user_audio", stream) {
		t.Fatal("recording stream must be accepted by the owner check")
	}
	if !liveAnchorOwnerModeAllowed("ai_dynamic", original) {
		t.Fatal("realtime dynamic must remain accepted")
	}
}
