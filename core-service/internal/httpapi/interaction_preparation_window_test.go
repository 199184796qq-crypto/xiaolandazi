package httpapi

import (
	"fmt"
	"livecompanion/core/internal/audioout"
	"strings"
	"testing"
)

func TestPreparationUsesContinuousShortTracksAndCompleteReturnUnits(t *testing.T) {
	program := audioout.RoomProgramSnapshot{TrackID: "T0", Task: &audioout.SpeechTask{DurationMS: 8000}, CurrentMS: 1000}
	for i := 0; i < 6; i++ {
		program.FutureWindow = append(program.FutureWindow, audioout.ProgramWindowTrack{TrackID: fmt.Sprintf("T%d", i), TrackIndex: i, OffsetMS: i * 8000, DurationMS: 8000, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: fmt.Sprintf("T%dS1", i), EndMS: 4000, Text: fmt.Sprintf("第%d轨第一完整句。", i), SafeCut: true}, {SegmentID: fmt.Sprintf("T%dS2", i), StartMS: 4000, EndMS: 8000, Text: fmt.Sprintf("第%d轨第二完整句。", i), SafeCut: true}}, SafePoints: []audioout.ProgramSafePoint{{CutMS: 4000, Grade: "B"}, {CutMS: 8000, Grade: "A"}}})
	}
	plan, plans, ok := preparationContext(program, 1000)
	if !ok || plan.TrackIndex < 3 || plan.LeadMS < 24000 || len(plans) != 3 {
		t.Fatalf("short-track preparation failed: %+v %+v", plan, plans)
	}
	if !strings.Contains(plan.CurrentText, "第一完整句") || !strings.Contains(plan.CurrentText, "第二完整句") || !strings.Contains(plan.ResumeText, "第一完整句") || !strings.Contains(plan.ResumeText, "第二完整句") {
		t.Fatalf("truncated semantic context: %+v", plan)
	}
}

func TestShortReadyWindowCanPrepareAtDegradedSentenceEnd(t *testing.T) {
	program := audioout.RoomProgramSnapshot{TrackID: "short", Task: &audioout.SpeechTask{DurationMS: 10000}, Timeline: []audioout.ProgramTimelineSegment{{EndMS: 5000, Text: "第一完整句。", SafeCut: true}, {StartMS: 5000, EndMS: 10000, Text: "回归后的完整句。", SafeCut: true}}}
	plan, _, ok := preparationContext(program, 0)
	if !ok || plan.Level != "C" || plan.CutMS != 5000 {
		t.Fatal("24 seconds became an admission barrier", plan)
	}
}

func TestReadyAudioUsesNearestSentenceIncludingTrackEnd(t *testing.T) {
	program := audioout.RoomProgramSnapshot{Task: &audioout.SpeechTask{DurationMS: 10000}, SafePoints: []audioout.ProgramSafePoint{{CutMS: 6000, Grade: "B"}, {CutMS: 10000, Grade: "A"}}}
	if cut, ok := resolveRoomProgramSafeCutAfterGeneration(program, 10000, 1000); !ok || cut != 10000 {
		t.Fatal("valid original safe cut was discarded", cut, ok)
	}
	if cut, ok := resolveRoomProgramSafeCutAfterGeneration(program, 6000, 6500); !ok || cut != 10000 {
		t.Fatal("short track end unavailable", cut, ok)
	}
	program.SafePoints = []audioout.ProgramSafePoint{{CutMS: 9999, Grade: "C", LeftPreview: "没说完的半句话"}}
	if _, ok := resolveRoomProgramSafeCutAfterGeneration(program, 0, 1000); ok {
		t.Fatal("C authorized half-sentence cut")
	}
}
