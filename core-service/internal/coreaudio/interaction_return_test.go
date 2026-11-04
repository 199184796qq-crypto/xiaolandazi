package coreaudio

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"

	"testing"
	"time"
)

func TestCompleteUnitCoveragePreservesNewPricesAndQualifiers(t *testing.T) {
	for _, test := range []struct {
		answer, unit string
		want         bool
	}{
		{"一只85.5元。", "一只85.5元。", true},
		{"一只85.5元。", "一只85.5元，运费另算。", false},
		{"一只85.5元。", "一只95.5元。", false},
		{"今天新鲜制作。", "当天卤制，现炸出锅。", false},
	} {
		if got := audioout.CompleteUnitCovered(test.answer, test.unit); got != test.want {
			t.Fatalf("coverage got=%v want=%v unit=%s", got, test.want, test.unit)
		}
	}
	p := &programState{ID: "run", Tracks: []programTrack{{ID: "a", Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "1", StartMS: 0, EndMS: 100, Text: "价格是", SafeCut: false}, {SegmentID: "2", StartMS: 100, EndMS: 200, Text: "85元，运费另算。", SafeCut: true}, {SegmentID: "3", StartMS: 200, EndMS: 400, Text: "接着说保存方法。", SafeCut: true}}}}}
	state := buildInteractionReturn(p, "task", "价格是85元，运费另算。", 0, 2000, false)
	if len(state.snapshot.SkippedUnits) != 1 || state.resumeMS != 200 {
		t.Fatal("complete fragments not grouped before dedup", state)
	}
}

func TestSkipCurrentSentenceStartsAtNextUnit(t *testing.T) {
	p := &programState{ID: "run", Tracks: []programTrack{{ID: "a", Timeline: []audioout.ProgramTimelineSegment{
		{SegmentID: "1", StartMS: 0, EndMS: 100, Text: "第一句。", SafeCut: true},
		{SegmentID: "2", StartMS: 100, EndMS: 200, Text: "第二句。", SafeCut: true},
	}}}}
	state := buildInteractionReturn(p, "task", "回答", 0, 2000, false, true)
	if len(state.snapshot.Units) != 1 || state.snapshot.Units[0].Text != "第二句。" || state.resumeMS != 100 {
		t.Fatalf("skip did not start at next sentence: units=%+v resume=%d skipped=%v", state.snapshot.Units, state.resumeMS, state.snapshot.SkippedUnits)
	}
}

func TestReturnBridgeRejectsChangedRunTokenAndDeadline(t *testing.T) {
	client, p, _, _ := sequentialFixture(t)
	p.Suspended = true
	p.InteractionReturn = buildInteractionReturn(p, p.CurrentTaskID, "核心回答。", 0, 3000, false)
	p.InteractionReturn.snapshot.Deadline = client.now().Add(-time.Second)
	raw, _ := json.Marshal(BridgeReady{TaskID: p.CurrentTaskID, Token: p.InteractionReturn.snapshot.Token, Text: "接着说刚才的用法。", PCM: make([]byte, roomaudio.PCMBytesPerFrame), DurationMS: 20})
	if err := client.handleReturnMessage(context.Background(), messagecaptain.Message{Kind: messagecaptain.ReturnBridgeReady, RoomID: p.RoomID, RunID: p.ID, Payload: raw}); err == nil {
		t.Fatal("late bridge accepted")
	}
	p.InteractionReturn.snapshot.Deadline = client.now().Add(time.Second)
	if err := client.handleReturnMessage(context.Background(), messagecaptain.Message{Kind: messagecaptain.ReturnBridgeReady, RoomID: p.RoomID, RunID: "old-run", Payload: raw}); err == nil {
		t.Fatal("old run bridge accepted")
	}
}
