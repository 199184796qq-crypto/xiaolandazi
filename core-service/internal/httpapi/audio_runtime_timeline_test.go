package httpapi

import (
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
)

func TestNextRoomProgramSafeCutUsesSemanticTimeline(t *testing.T) {
	program := audioout.RoomProgramSnapshot{
		Task: &audioout.SpeechTask{DurationMS: 10000},
		Timeline: []audioout.ProgramTimelineSegment{
			{SegmentID: "A-001", Index: 1, StartMS: 0, EndMS: 5000, Text: "第一句话。", SafeCut: true},
			{SegmentID: "A-002", Index: 2, StartMS: 5000, EndMS: 10000, Text: "第二句话。", SafeCut: true},
		},
	}

	cut, ok := nextRoomProgramSafeCut(program, 1200, 35*time.Second)
	if !ok || cut != 5000 {
		t.Fatalf("cut=%d ok=%v, want 5000 true", cut, ok)
	}

	// The exact end of a track is intentionally not returned as an interrupt
	// cut point because the normal mainline advance owns that transition.
	if cut, ok := nextRoomProgramSafeCut(program, 6200, 35*time.Second); ok {
		t.Fatalf("cut=%d ok=%v, want no cut inside final semantic segment", cut, ok)
	}
}

func TestNextRoomProgramSafeCutHonorsWaitWindow(t *testing.T) {
	program := audioout.RoomProgramSnapshot{
		Task: &audioout.SpeechTask{DurationMS: 60000},
		Timeline: []audioout.ProgramTimelineSegment{
			{SegmentID: "B-001", Index: 1, StartMS: 0, EndMS: 40000, Text: "很长的一段。", SafeCut: true},
			{SegmentID: "B-002", Index: 2, StartMS: 40000, EndMS: 60000, Text: "结尾。", SafeCut: true},
		},
	}
	if cut, ok := nextRoomProgramSafeCut(program, 1000, 35*time.Second); ok {
		t.Fatalf("cut=%d ok=%v, want no safe cut more than 35 seconds away", cut, ok)
	}
}

func TestResolveQuickRoomProgramSafeCutLeavesVisibleLead(t *testing.T) {
	program := audioout.RoomProgramSnapshot{
		Task: &audioout.SpeechTask{DurationMS: 30000},
		SafePoints: []audioout.ProgramSafePoint{
			{ID: "SP001", CutMS: 5200, Grade: "A", Score: 96},
			{ID: "SP002", CutMS: 9800, Grade: "A", Score: 96},
			{ID: "SP003", CutMS: 16000, Grade: "A", Score: 96},
		},
	}
	cut, ok := resolveQuickRoomProgramSafeCut(program, 0, 4300)
	if !ok {
		t.Fatal("expected quick safe cut")
	}
	if cut != 9800 {
		t.Fatalf("cut=%d, want 9800 so the UI has visible lead before stop", cut)
	}
}
