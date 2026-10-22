package httpapi

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func testPCMFormat(byteRate uint32) []byte {
	fmtData := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtData[0:2], 1)
	binary.LittleEndian.PutUint16(fmtData[2:4], 1)
	binary.LittleEndian.PutUint32(fmtData[4:8], byteRate)
	binary.LittleEndian.PutUint32(fmtData[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtData[12:14], 1)
	binary.LittleEndian.PutUint16(fmtData[14:16], 8)
	return fmtData
}

func TestCombineGeneratedVoiceWAVBuildsGuardedContiguousTimeline(t *testing.T) {
	fmtData := testPCMFormat(1000)
	pieces := []generatedVoiceWAVPiece{
		{text: "第一段。", fmtData: fmtData, pcmData: make([]byte, 1000), byteRate: 1000},
		{text: "第二段。", fmtData: fmtData, pcmData: make([]byte, 1500), byteRate: 1000},
	}
	combined, timeline, durationMS, err := combineGeneratedVoiceWAV("A", pieces)
	if err != nil {
		t.Fatal(err)
	}
	if durationMS != 3340 {
		t.Fatalf("duration=%d want=3340", durationMS)
	}
	if len(timeline) != 2 {
		t.Fatalf("timeline segments=%d want=2", len(timeline))
	}
	if timeline[0].StartMS != 0 || timeline[0].EndMS != 1420 || !timeline[0].SafeCut {
		t.Fatalf("first timeline=%+v", timeline[0])
	}
	if timeline[1].StartMS != 1420 || timeline[1].EndMS != 3340 || !timeline[1].SafeCut {
		t.Fatalf("second timeline=%+v", timeline[1])
	}
	parsedDuration, err := generatedVoiceWAVDurationMS(combined)
	if err != nil {
		t.Fatal(err)
	}
	if parsedDuration != durationMS {
		t.Fatalf("parsed duration=%d timeline duration=%d", parsedDuration, durationMS)
	}
	_, pcm, _, err := parseGeneratedVoiceWAV(combined)
	if err != nil {
		t.Fatal(err)
	}
	wantHead := bytes.Repeat([]byte{0x80}, int(generatedVoiceSegmentHeadMS))
	wantTail := bytes.Repeat([]byte{0x80}, int(generatedVoiceSegmentTailMS))
	if !bytes.Equal(pcm[:120], wantHead) || !bytes.Equal(pcm[1120:1420], wantTail) || !bytes.Equal(pcm[len(pcm)-300:], wantTail) {
		t.Fatal("each generated segment must retain an exact silent guard tail")
	}
	points := buildFullShowSafePoints(timeline)
	if len(points) != 2 || points[0].CutMS != 1420 || points[1].CutMS != 3340 {
		t.Fatalf("safe cuts must follow guard tails: %+v", points)
	}
}

func TestNormalizeGeneratedVoicePieceClampsProviderSilence(t *testing.T) {
	fmtData := testPCMFormat(1000)
	providerPCM := append(bytes.Repeat([]byte{0x80}, 900), bytes.Repeat([]byte{0x00}, 1000)...)
	providerPCM = append(providerPCM, bytes.Repeat([]byte{0x80}, 1100)...)
	normalized, err := normalizeGeneratedVoicePiecePCM(fmtData, providerPCM, 1000)
	if err != nil {
		t.Fatal(err)
	}
	wantDuration := int(generatedVoiceSegmentHeadMS + 1000 + generatedVoiceSegmentTailMS)
	if len(normalized) != wantDuration {
		t.Fatalf("normalized bytes=%d want=%d", len(normalized), wantDuration)
	}
	if !bytes.Equal(normalized[:generatedVoiceSegmentHeadMS], bytes.Repeat([]byte{0x80}, int(generatedVoiceSegmentHeadMS))) {
		t.Fatal("leading provider silence was not normalized")
	}
	if !bytes.Equal(normalized[len(normalized)-int(generatedVoiceSegmentTailMS):], bytes.Repeat([]byte{0x80}, int(generatedVoiceSegmentTailMS))) {
		t.Fatal("trailing provider silence was not normalized")
	}
}

func TestNormalizeGeneratedVoicePieceRejectsSilentOutput(t *testing.T) {
	fmtData := testPCMFormat(1000)
	if _, err := normalizeGeneratedVoicePiecePCM(fmtData, bytes.Repeat([]byte{0x80}, 1000), 1000); err == nil {
		t.Fatal("fully silent generated audio must be rejected")
	}
}

func TestCombineGeneratedVoiceWAVDoesNotDuplicateExistingGuardTail(t *testing.T) {
	fmtData := testPCMFormat(1000)
	first, timeline, duration, err := combineGeneratedVoiceWAV("A", []generatedVoiceWAVPiece{{text: "第一段。", fmtData: fmtData, pcmData: make([]byte, 1000), byteRate: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	pieces, err := existingContentVoicePieces(first, timeline)
	if err != nil {
		t.Fatal(err)
	}
	second, secondTimeline, secondDuration, err := combineGeneratedVoiceWAV("A", pieces)
	if err != nil {
		t.Fatal(err)
	}
	if secondDuration != duration || secondTimeline[0].EndMS != timeline[0].EndMS || !bytes.Equal(first, second) {
		t.Fatalf("guard tail duplicated: first=%d second=%d", duration, secondDuration)
	}
}

func TestSplitFullShowVoiceTextKeepsSemanticBoundaries(t *testing.T) {
	text := strings.Repeat("家人们先看这个产品的实际信息，", 3) +
		"这一段先讲清楚。然后我们再看大家最关心的使用场景，具体以页面信息为准！最后再把下单注意事项给大家说一下。"
	segments := splitFullShowVoiceText(text)
	if len(segments) < 2 {
		t.Fatalf("segments=%v, want multiple semantic chunks", segments)
	}
	joined := strings.Join(segments, "")
	if joined != strings.TrimSpace(text) {
		t.Fatalf("joined text changed\n got=%q\nwant=%q", joined, strings.TrimSpace(text))
	}
	for index, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			t.Fatalf("segment %d is empty", index)
		}
	}
}

func TestFormalVoiceTimelineOnlyMarksStrongSentenceCutsAndBuildsSRT(t *testing.T) {
	fmtData := testPCMFormat(1000)
	pieces := []generatedVoiceWAVPiece{
		{text: "先把这一点说明白，", fmtData: fmtData, pcmData: make([]byte, 800), byteRate: 1000},
		{text: "然后这一句完整结束。", fmtData: fmtData, pcmData: make([]byte, 1200), byteRate: 1000},
	}
	_, timeline, _, err := combineGeneratedVoiceWAV("B", pieces)
	if err != nil {
		t.Fatal(err)
	}
	if timeline[0].SafeCut {
		t.Fatalf("comma chunk should not be a safe cut: %+v", timeline[0])
	}
	if !timeline[1].SafeCut {
		t.Fatalf("strong sentence ending should be safe: %+v", timeline[1])
	}
	srt := liveAgentTimelineSRT(timeline)
	if !strings.Contains(srt, "00:00:00,000 --> 00:00:01,220") || !strings.Contains(srt, "00:00:01,220 --> 00:00:02,840") {
		t.Fatalf("unexpected srt:\n%s", srt)
	}
	if !strings.Contains(srt, "先把这一点说明白，") || !strings.Contains(srt, "然后这一句完整结束。") {
		t.Fatalf("srt text missing:\n%s", srt)
	}
}

func TestRebuildFormalVoiceSubtitleTimelineOnlyChangesText(t *testing.T) {
	original := []model.LiveAgentPlanTimelineSegment{
		{SegmentID: "A-001", Index: 1, StartMS: 0, EndMS: 1000, Text: "原字幕。", SafeCut: true},
		{SegmentID: "A-002", Index: 2, StartMS: 1000, EndMS: 2200, Text: "第二句。", SafeCut: true},
	}
	edited := []model.LiveAgentPlanTimelineSegment{
		{SegmentID: "A-001", Index: 1, StartMS: 0, EndMS: 1000, Text: "校对后的字幕", SafeCut: true},
		{SegmentID: "A-002", Index: 2, StartMS: 1000, EndMS: 2200, Text: "第二句！", SafeCut: true},
	}
	result, err := rebuildFormalVoiceSubtitleTimeline(original, edited)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Text != "校对后的字幕" || result[0].SafeCut {
		t.Fatalf("first=%+v", result[0])
	}
	if result[1].Text != "第二句！" || !result[1].SafeCut {
		t.Fatalf("second=%+v", result[1])
	}
	if result[0].StartMS != 0 || result[0].EndMS != 1000 || result[1].StartMS != 1000 || result[1].EndMS != 2200 {
		t.Fatalf("time positions changed: %+v", result)
	}
}

func TestRebuildFormalVoiceSubtitleTimelineRejectsTimeChange(t *testing.T) {
	original := []model.LiveAgentPlanTimelineSegment{
		{SegmentID: "A-001", Index: 1, StartMS: 0, EndMS: 1000, Text: "原字幕。", SafeCut: true},
	}
	edited := []model.LiveAgentPlanTimelineSegment{
		{SegmentID: "A-001", Index: 1, StartMS: 10, EndMS: 1000, Text: "改文字。", SafeCut: true},
	}
	if _, err := rebuildFormalVoiceSubtitleTimeline(original, edited); err == nil {
		t.Fatal("expected time change to be rejected")
	}
}

func TestCombineGeneratedVoiceWAVAllowsDeliberateSilence(t *testing.T) {
	format := testPCMFormat(1000)
	raw, timeline, duration, err := combineGeneratedVoiceWAV("pause", []generatedVoiceWAVPiece{
		{text: "第一句。", fmtData: format, pcmData: make([]byte, 1000), byteRate: 1000},
		{text: "", fmtData: format, pcmData: make([]byte, 320), byteRate: 1000},
		{text: "第二句。", fmtData: format, pcmData: make([]byte, 1000), byteRate: 1000},
	})
	if err != nil || len(raw) == 0 || len(timeline) != 2 || duration <= 2000 {
		t.Fatalf("silence piece rejected: duration=%d timeline=%d err=%v", duration, len(timeline), err)
	}
	if timeline[1].StartMS != timeline[0].EndMS {
		t.Fatalf("pause left a timeline gap: first=%+v second=%+v", timeline[0], timeline[1])
	}
}
