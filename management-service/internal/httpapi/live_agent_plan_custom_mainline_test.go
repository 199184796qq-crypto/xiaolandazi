package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechasr"
)

func TestCustomMainlineTimelineFromASRPreservesSpeechGaps(t *testing.T) {
	result := speechasr.Result{
		Text: "第一句 第二句",
		Sentences: []speechasr.Sentence{
			{BeginTime: 500, EndTime: 1800, Text: "第一句"},
			{BeginTime: 2600, EndTime: 4000, Text: "第二句"},
		},
	}
	timeline, transcript, durationMS, err := customMainlineTimelineFromASR(result, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if durationMS != 5000 {
		t.Fatalf("duration=%d", durationMS)
	}
	if len(timeline) != 2 {
		t.Fatalf("timeline=%#v", timeline)
	}
	if timeline[0].StartMS != 500 || timeline[0].EndMS != 1800 {
		t.Fatalf("first segment=%#v", timeline[0])
	}
	if timeline[1].StartMS != 2600 || timeline[1].EndMS != 4000 {
		t.Fatalf("second segment=%#v", timeline[1])
	}
	if !strings.Contains(transcript, "第一句。") || !strings.Contains(transcript, "第二句。") {
		t.Fatalf("transcript=%q", transcript)
	}
	srt := liveAgentTimelineSRT(timeline)
	if !strings.Contains(srt, "00:00:00,500 --> 00:00:01,800") ||
		!strings.Contains(srt, "00:00:02,600 --> 00:00:04,000") {
		t.Fatalf("srt=%q", srt)
	}
	points := buildFullShowSafePoints(timeline)
	if len(points) != 2 || points[0].CutMS != 1800 || points[1].CutMS != 4000 {
		t.Fatalf("safe points=%#v", points)
	}
}

func TestNormalizeCustomMainlineEditedTimelineAllowsSilenceGap(t *testing.T) {
	timeline, err := normalizeCustomMainlineEditedTimeline([]model.LiveAgentPlanTimelineSegment{
		{StartMS: 400, EndMS: 1600, Text: "第一段"},
		{StartMS: 2200, EndMS: 3600, Text: "第二段？"},
	}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if timeline[0].StartMS != 400 || timeline[1].StartMS != 2200 {
		t.Fatalf("timeline=%#v", timeline)
	}
	if timeline[0].Text != "第一段。" || timeline[1].Text != "第二段？" {
		t.Fatalf("normalized text=%#v", timeline)
	}
}
