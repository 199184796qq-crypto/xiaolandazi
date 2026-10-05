package httpapi

import (
	"bytes"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestContentRefreshQuarterRotatesWithoutReplacingEntireTrack(t *testing.T) {
	timeline := make([]model.LiveAgentPlanTimelineSegment, 8)
	for i := range timeline {
		timeline[i] = model.LiveAgentPlanTimelineSegment{StartMS: int64(i) * 1000, EndMS: int64(i+1) * 1000}
	}
	for generation := 1; generation <= 5; generation++ {
		start, end, err := selectContentRefreshWindow(timeline, 25, generation)
		if err != nil || start != ((generation-1)%4)*2 || end-start != 2 {
			t.Fatalf("generation %d window %d:%d err=%v", generation, start, end, err)
		}
	}
	if _, _, err := selectContentRefreshWindow(timeline[:1], 25, 1); err == nil {
		t.Fatal("single chunk became full replacement")
	}
	if start, end, err := selectContentRefreshWindow(timeline, 100, 1); err != nil || start != 0 || end != 8 {
		t.Fatal("explicit full replacement invalid")
	}
}

func TestContentRefreshKeepsUnchangedAudioByteForByte(t *testing.T) {
	format := testPCMFormat(1000)
	var original []generatedVoiceWAVPiece
	for i := 0; i < 8; i++ {
		original = append(original, generatedVoiceWAVPiece{text: "原声。", fmtData: format, pcmData: bytes.Repeat([]byte{byte(i + 1)}, 1000), byteRate: 1000})
	}
	raw, timeline, _, err := combineGeneratedVoiceWAV("A", original)
	if err != nil {
		t.Fatal(err)
	}
	pieces, err := existingContentVoicePieces(raw, timeline)
	if err != nil {
		t.Fatal(err)
	}
	start, end, err := selectContentRefreshWindow(timeline, 25, 2)
	if err != nil {
		t.Fatal(err)
	}
	newPiece := generatedVoiceWAVPiece{text: "新说法。", fmtData: format, pcmData: bytes.Repeat([]byte{99}, 2000), byteRate: 1000}
	merged := append(append(append([]generatedVoiceWAVPiece{}, pieces[:start]...), newPiece), pieces[end:]...)
	output, newTimeline, duration, err := combineGeneratedVoiceWAV("A", merged)
	if err != nil {
		t.Fatal(err)
	}
	_, pcm, _, _ := parseGeneratedVoiceWAV(output)
	_, oldPCM, _, _ := parseGeneratedVoiceWAV(raw)
	if !bytes.Equal(pcm[:2000], oldPCM[:2000]) || !bytes.Equal(pcm[4000:], oldPCM[4000:]) {
		t.Fatal("untouched 75% audio changed")
	}
	if duration != 8000 || newTimeline[len(newTimeline)-1].EndMS != 8000 {
		t.Fatal("rebuilt timeline invalid")
	}
}

func TestContentRefreshRejectsBrokenTimeline(t *testing.T) {
	format := testPCMFormat(1000)
	raw, timeline, _, _ := combineGeneratedVoiceWAV("A", []generatedVoiceWAVPiece{{text: "原声。", fmtData: format, pcmData: make([]byte, 1000), byteRate: 1000}})
	timeline[0].EndMS = 2000
	if _, err := existingContentVoicePieces(raw, timeline); err == nil {
		t.Fatal("wrong duration accepted")
	}
}

func TestContentRefreshAuditRejectsInventedPriceAndIdenticalText(t *testing.T) {
	context := model.LiveAgentFullShowGenerationContext{UseDynamicFacts: true, RoundMinutes: 1}
	old := "哥哥姐姐们，我们家的菜籽油，大家先看看商品页面，想了解的在公屏说一下哟。"
	for _, text := range []string{old, "哥哥姐姐们，这个菜籽油现在只要9.9元，想要的赶快去下单吧，晚了就没有了。", strings.Repeat("太长", 100)} {
		if err := auditContentRefreshText(context, old, text); err == nil {
			t.Fatalf("unsafe text accepted: %s", text)
		}
	}
}
