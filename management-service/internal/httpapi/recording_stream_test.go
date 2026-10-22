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

func TestRecordingStreamInstructionBranches(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"这个套餐多少钱？", "随口一问"},
		{"赶紧抢，最后两单！", "提醒"},
		{"十人餐团购价337元", "报价"},
		{"欢迎新朋友进来", "打招呼"},
		{"咱们慢慢说，这个菜挺实在", "聊天"},
	}
	for _, tc := range cases {
		got := recordingStreamInstruction(tc.text)
		if !strings.Contains(got, tc.want) || len([]rune(got)) > 50 {
			t.Fatalf("text=%q instruction=%q", tc.text, got)
		}
	}
}

func TestRecordingStreamBlockTargetStartsWithShortBlocks(t *testing.T) {
	// 一句一条 track：分段已按语义切好，合块会让 Core 认不出句界。
	want := map[int]int{0: 1, 1: 1, 3: 1, 6: 1}
	for start, size := range want {
		if got := recordingStreamBlockTarget(start); got != size {
			t.Fatalf("start=%d got=%d want=%d", start, got, size)
		}
	}
}

func TestRecordingInstructionHonorsEmotionFlag(t *testing.T) {
	version := model.LiveAgentPlanVersion{}
	if got := recordingInstructionForVersion(version, "这个多少钱？"); got != "" {
		t.Fatalf("default emotion must be off, got %q", got)
	}
	enabled := true
	version.VoiceIdentity.EmotionEnabled = &enabled
	if got := recordingInstructionForVersion(version, "这个多少钱？"); !strings.Contains(got, "随口一问") {
		t.Fatalf("enabled emotion missing instruction: %q", got)
	}
}
