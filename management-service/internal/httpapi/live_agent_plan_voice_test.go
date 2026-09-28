package httpapi

import (
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

func TestCombineGeneratedVoiceWAVBuildsExactContiguousTimeline(t *testing.T) {
	fmtData := testPCMFormat(1000)
	pieces := []generatedVoiceWAVPiece{
		{text: "第一段。", fmtData: fmtData, pcmData: make([]byte, 1000), byteRate: 1000},
		{text: "第二段。", fmtData: fmtData, pcmData: make([]byte, 1500), byteRate: 1000},
	}
	combined, timeline, durationMS, err := combineGeneratedVoiceWAV("A", pieces)
	if err != nil {
		t.Fatal(err)
	}
	if durationMS != 2500 {
		t.Fatalf("duration=%d want=2500", durationMS)
	}
	if len(timeline) != 2 {
		t.Fatalf("timeline segments=%d want=2", len(timeline))
	}
	if timeline[0].StartMS != 0 || timeline[0].EndMS != 1000 || !timeline[0].SafeCut {
		t.Fatalf("first timeline=%+v", timeline[0])
	}
	if timeline[1].StartMS != 1000 || timeline[1].EndMS != 2500 || !timeline[1].SafeCut {
		t.Fatalf("second timeline=%+v", timeline[1])
	}
	parsedDuration, err := generatedVoiceWAVDurationMS(combined)
	if err != nil {
		t.Fatal(err)
	}
	if parsedDuration != durationMS {
		t.Fatalf("parsed duration=%d timeline duration=%d", parsedDuration, durationMS)
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
	if !strings.Contains(srt, "00:00:00,000 --> 00:00:00,800") || !strings.Contains(srt, "00:00:00,800 --> 00:00:02,000") {
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
