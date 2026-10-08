package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewAudioRunsToNextSafeSemanticBoundary(t *testing.T) {
	format := testPCMFormat(1000)
	raw, timeline, duration, err := combineGeneratedVoiceWAV("x", []generatedVoiceWAVPiece{{text: "第一句。", fmtData: format, pcmData: make([]byte, 7000), byteRate: 1000}, {text: "第二句！", fmtData: format, pcmData: make([]byte, 6000), byteRate: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	clipped, segments, ms, text, truncated, err := limitPreviewVoice(raw, timeline, duration, 10000, "第一句。\n第二句！")
	if err != nil || !truncated || ms != timeline[1].EndMS || len(segments) != 2 || !segments[1].SafeCut || text != "第一句。\n第二句！" {
		t.Fatal(ms, text, truncated, err)
	}
	actual, err := generatedVoiceWAVDurationMS(clipped)
	if err != nil || actual != timeline[1].EndMS {
		t.Fatal("safe preview boundary mismatch", actual, err)
	}
	short, shortTimeline, shortMS, shortText, shortTruncated, err := limitPreviewVoice(raw, timeline, duration, 1000, "第一句。\n第二句！")
	if err != nil || !shortTruncated || shortMS != timeline[0].EndMS || len(shortTimeline) != 1 || !shortTimeline[0].SafeCut || !strings.Contains(shortText, "第一句") {
		t.Fatal("preview did not use first safe ending", shortMS, shortText, err)
	}
	if actual, err = generatedVoiceWAVDurationMS(short); err != nil || actual != timeline[0].EndMS {
		t.Fatal("short safe preview cut failed", actual, err)
	}
}

func TestPreviewGenerationCanFinishSentenceButPlaybackCutsAtBudget(t *testing.T) {
	_, state, segment, _ := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	state.Surface = "generation_preview"
	state.PreviewLimitMS = 120000
	text := "哥哥姐姐们，我们家就在这个镇上啊。人少可以选择半只装！喜欢就去链接看看。"
	segment.Selected.Text = text
	format := testPCMFormat(1000)
	raw, timeline, duration, err := combineGeneratedVoiceWAV("preview", []generatedVoiceWAVPiece{
		{text: "哥哥姐姐们，我们家就在这个镇上啊。", fmtData: format, pcmData: make([]byte, 7000), byteRate: 1000},
		{text: "人少可以选择半只装！", fmtData: format, pcmData: make([]byte, 6000), byteRate: 1000},
		{text: "喜欢就去链接看看。", fmtData: format, pcmData: make([]byte, 6000), byteRate: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, generatedTimeline, generatedMS, generatedText, truncated, err := limitPreviewVoice(raw, timeline, duration, 10000, text)
	if err != nil || !truncated || generatedMS <= 10000 || generatedText != "哥哥姐姐们，我们家就在这个镇上啊。人少可以选择半只装！" {
		t.Fatal(generatedMS, generatedText, err)
	}
	segment.Voice = &anchorcycle.VoiceAsset{Status: "ready", PreviewTruncated: true, PreviewReservedMS: 10000, Bundle: model.LiveAgentPlanVersionVariant{
		Text: generatedText, AudioDurationMS: generatedMS, Timeline: generatedTimeline,
	}}
	if err := checkCycleVoiceP0(state, segment); err != nil {
		t.Fatal("generated safe prefix must remain downloadable", err)
	}
	projected := cycleVoiceForBrowser(httptest.NewRequest("GET", "/", nil), state, segment.Selected.ID, segment.Voice)
	if projected.Status != "ready" || projected.Bundle.AudioDurationMS != 10000 || projected.Bundle.Timeline[len(projected.Bundle.Timeline)-1].SafeCut {
		t.Fatal("browser must end mid-sentence at its exact budget", projected)
	}
	if segment.Voice.Bundle.AudioDurationMS != generatedMS || segment.Voice.Bundle.Text != generatedText || !segment.Voice.Bundle.Timeline[len(generatedTimeline)-1].SafeCut {
		t.Fatal("projection changed generated assets")
	}
	playback, err := hardLimitPreviewWAV(generated, 10000)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := generatedVoiceWAVDurationMS(playback)
	if err != nil || actual != 10000 {
		t.Fatal("download crossed playback deadline", actual, err)
	}
	segment.Voice.PreviewTruncated = false
	if checkCycleVoiceP0(state, segment) == nil {
		t.Fatal("unmarked mismatched audio was accepted")
	}
	segment.Voice.PreviewTruncated = true
	segment.Voice.Bundle.Text = "另一份声音。"
	if checkCycleVoiceP0(state, segment) == nil {
		t.Fatal("foreign audio prefix was accepted")
	}
}

func TestPreviewPlaybackCanCutInsideFirstSentence(t *testing.T) {
	format := testPCMFormat(48000)
	raw, _, _, err := combineGeneratedVoiceWAV("one", []generatedVoiceWAVPiece{{text: "第一句话还没说完。", fmtData: format, pcmData: make([]byte, 48000*3), byteRate: 48000}})
	if err != nil {
		t.Fatal(err)
	}
	clipped, err := hardLimitPreviewWAV(raw, 1234)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := generatedVoiceWAVDurationMS(clipped)
	if err != nil || ms > 1234 || ms < 1233 {
		t.Fatal(ms, err)
	}
}
