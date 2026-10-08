package httpapi

import (
	"testing"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
)

func TestInteractionResumeDedupDoesNotMoveTheInterruptCut(t *testing.T) {
	for _, resumeStrategy := range []string{"DIRECT", "BRIDGE", "SWITCH_PLAN"} {
		t.Run(resumeStrategy, func(t *testing.T) {
			program := audioout.RoomProgramSnapshot{
				Task: &audioout.SpeechTask{DurationMS: 32527},
				SafePoints: []audioout.ProgramSafePoint{
					{ID: "SP003", CutMS: 26376, NextPreview: "麻辣五香都有，京东物流包邮到家。"},
					{ID: "TRACK_END", CutMS: 32527},
				},
			}
			switchMS, resumeMS := roomInteractionCutOffsets(26376)
			dedup := applyResumeDedupGate(program, "麻辣五香都有，京东物流包邮到家。", *resumeMS, resumeStrategy)
			*resumeMS = dedup.FinalMS
			if *resumeMS != program.Task.DurationMS {
				t.Fatalf("expected next-track resume, got %d", *resumeMS)
			}
			if *switchMS != 26376 || *switchMS >= program.Task.DurationMS {
				t.Fatalf("resume dedup corrupted the interrupt boundary: cut=%d resume=%d", *switchMS, *resumeMS)
			}
			if !validInteractionSwitchLead(12500, *switchMS) {
				t.Fatal("original sentence boundary became inadmissible")
			}
		})
	}
}

func TestPreparedAudioCanClaimNearbySafePointAfterMainlineChanges(t *testing.T) {
	program := audioout.RoomProgramSnapshot{
		Task:       &audioout.SpeechTask{ID: "new-track", DurationMS: 30000},
		SafePoints: []audioout.ProgramSafePoint{{ID: "new-cut", CutMS: 25000, Grade: "A"}},
	}
	if _, ok := plannedDecisionSafeCut(program, 20000, agentdecision.Item{}); ok {
		t.Fatal("fixture should lack a long generation-preparation window")
	}
	cut, ok := plannedDecisionSafeCut(program, 20000, agentdecision.Item{AudioPrepared: true, PreparedTaskID: "old-track", PreparedCutMS: 1234})
	if !ok || cut != 25000 {
		t.Fatalf("prepared audio waited for generation or reused a stale task: cut=%d ok=%t", cut, ok)
	}
}
