package httpapi

import (
	"slices"
	"testing"

	"livecompanion/core/internal/audioout"
)

func strategyTestProgram() audioout.RoomProgramSnapshot {
	return audioout.RoomProgramSnapshot{
		SafePoints: []audioout.ProgramSafePoint{
			{ID: "sp-10", CutMS: 10000, Grade: "A", Kind: "SENTENCE", Topics: []string{"price"}, NextPreview: "今天活动价一百二十九块九。"},
			{ID: "sp-18", CutMS: 18000, Grade: "A", Kind: "SENTENCE", Topics: []string{"price"}, NextPreview: "中通、极兔、邮政随机发，包邮到家。"},
			{ID: "sp-28", CutMS: 28000, Grade: "A", Kind: "SENTENCE", Topics: []string{"shipping"}, NextPreview: "库存以后台实时数量为准，喜欢的去一号链接。"},
			{ID: "sp-42", CutMS: 42000, Grade: "A", Kind: "SENTENCE", Topics: []string{"quality"}, NextPreview: "这款油是低芥酸、非转基因原料。"},
		},
		Task: &audioout.SpeechTask{DurationMS: 60000},
	}
}

func resumeDedupTestProgram() audioout.RoomProgramSnapshot {
	return audioout.RoomProgramSnapshot{
		SafePoints: []audioout.ProgramSafePoint{
			{ID: "sp-a", CutMS: 10000, Grade: "A", Kind: "SENTENCE", NextPreview: "中通、极兔、邮政随机发，包邮到家。"},
			{ID: "sp-b", CutMS: 20000, Grade: "A", Kind: "SENTENCE", NextPreview: "库存我看一下后台实时数量再告诉大家。"},
			{ID: "sp-c", CutMS: 30000, Grade: "A", Kind: "SENTENCE", NextPreview: "今天直播间活动价是一百二十九块九。"},
			{ID: "sp-d", CutMS: 40000, Grade: "A", Kind: "SENTENCE", NextPreview: "这款油是低芥酸、零胆固醇、非转基因。"},
			{ID: "sp-e", CutMS: 50000, Grade: "A", Kind: "SENTENCE", NextPreview: "老油坊自产自销，喜欢浓香口感的可以试试。"},
			{ID: "sp-end", CutMS: 60000, Grade: "A", Kind: "SENTENCE", NextPreview: ""},
		},
		Task: &audioout.SpeechTask{DurationMS: 60000},
	}
}

func TestResumeCandidatesRequireSemanticOverlapForSkipStrategies(t *testing.T) {
	program := strategyTestProgram()
	withoutOverlap := resumeCandidatesForInteraction(program, 10000, 9000, "coupon")
	if slices.Contains(withoutOverlap, "FUSION_SKIP") || slices.Contains(withoutOverlap, "CROSS_RESUME") {
		t.Fatalf("skip strategies must not be candidates without semantic overlap: %v", withoutOverlap)
	}

	withOverlap := resumeCandidatesForInteraction(program, 5000, 9000, "price")
	if !slices.Contains(withOverlap, "FUSION_SKIP") || !slices.Contains(withOverlap, "CROSS_RESUME") {
		t.Fatalf("expected semantic-overlap skip strategies, got %v", withOverlap)
	}
}

func TestResumeCandidatesAddLongInterruptStrategiesByDuration(t *testing.T) {
	program := strategyTestProgram()
	medium := resumeCandidatesForInteraction(program, 10000, 15000, "coupon")
	if !slices.Contains(medium, "RE_ANCHOR") || slices.Contains(medium, "SWITCH_PLAN") {
		t.Fatalf("15s interaction should add RE_ANCHOR only, got %v", medium)
	}
	long := resumeCandidatesForInteraction(program, 10000, 25000, "coupon")
	if !slices.Contains(long, "RE_ANCHOR") || !slices.Contains(long, "SWITCH_PLAN") {
		t.Fatalf("25s interaction should add RE_ANCHOR and SWITCH_PLAN, got %v", long)
	}
}

func TestFusionSkipMovesPastOverlappingSafePoints(t *testing.T) {
	program := strategyTestProgram()
	resumeMS, reason := resumeOffsetForStrategy(program, 5000, "FUSION_SKIP", "price")
	if resumeMS != 28000 {
		t.Fatalf("expected FUSION_SKIP to pass price-overlap points and resume at 28000ms, got %d", resumeMS)
	}
	if reason != "next_safe_semantic_entry" {
		t.Fatalf("unexpected resume reason %q", reason)
	}
}

func TestDirectAndBridgeKeepSameSafeBoundary(t *testing.T) {
	program := strategyTestProgram()
	for _, strategy := range []string{"DIRECT", "BRIDGE"} {
		resumeMS, reason := resumeOffsetForStrategy(program, 18000, strategy, "price")
		if resumeMS != 18000 || reason != "same_safe_boundary" {
			t.Fatalf("%s should keep same safe boundary, got ms=%d reason=%s", strategy, resumeMS, reason)
		}
	}
}

func TestSwitchPlanMovesToTrackEnd(t *testing.T) {
	program := strategyTestProgram()
	resumeMS, reason := resumeOffsetForStrategy(program, 18000, "SWITCH_PLAN", "price")
	if resumeMS != 60000 || reason != "advance_to_next_mainline_track" {
		t.Fatalf("unexpected switch-plan target ms=%d reason=%s", resumeMS, reason)
	}
}

func TestResumeDedupGateMovesDirectPastRepeatedShippingSentence(t *testing.T) {
	program := resumeDedupTestProgram()
	decision := applyResumeDedupGate(
		program,
		"老乡，您拍的一号链接已经记下了。感谢支持，发货这块中通、极兔、邮政随机发。",
		10000,
		"DIRECT",
	)
	if !decision.Triggered {
		t.Fatalf("expected duplicate gate to trigger: %#v", decision)
	}
	if decision.FinalMS != 20000 || decision.FinalPointID != "sp-b" {
		t.Fatalf("expected resume to advance to sp-b, got %#v", decision)
	}
	if decision.FinalStrategy != "FUSION_SKIP" {
		t.Fatalf("expected DIRECT duplicate to upgrade to FUSION_SKIP, got %s", decision.FinalStrategy)
	}
	if decision.Score < 0.5 {
		t.Fatalf("expected high duplicate score, got %.3f", decision.Score)
	}
}

func TestResumeDedupGateCatchesSemanticQualityRepeatAfterBridge(t *testing.T) {
	program := resumeDedupTestProgram()
	decision := applyResumeDedupGate(
		program,
		"咱们继续看这款新黄菜油，低芥酸非转基因，品质很扎实。",
		40000,
		"BRIDGE",
	)
	if !decision.Triggered {
		t.Fatalf("expected semantic duplicate gate to trigger: %#v", decision)
	}
	if decision.FinalMS != 50000 || decision.FinalPointID != "sp-e" {
		t.Fatalf("expected bridge resume to move to non-overlapping point, got %#v", decision)
	}
	if decision.FinalStrategy != "FUSION_SKIP" {
		t.Fatalf("expected bridge duplicate to become fusion skip, got %s", decision.FinalStrategy)
	}
}

func TestResumeDedupGateLeavesNonRepeatedDirectResumeUntouched(t *testing.T) {
	program := resumeDedupTestProgram()
	decision := applyResumeDedupGate(
		program,
		"老乡，五升装就是日常家庭用量，拍一桶就行。",
		20000,
		"DIRECT",
	)
	if decision.Triggered || decision.FinalMS != 20000 || decision.FinalStrategy != "DIRECT" {
		t.Fatalf("non-overlapping resume must remain untouched: %#v", decision)
	}
}

func TestResumeDedupGatePreservesCrossResumeWhenItMustAdvanceAgain(t *testing.T) {
	program := resumeDedupTestProgram()
	decision := applyResumeDedupGate(
		program,
		"今天活动价就是一百二十九块九，价格我给大家说清楚了。",
		30000,
		"CROSS_RESUME",
	)
	if !decision.Triggered || decision.FinalMS != 40000 {
		t.Fatalf("expected CROSS_RESUME to advance past repeated price segment: %#v", decision)
	}
	if decision.FinalStrategy != "CROSS_RESUME" {
		t.Fatalf("advanced strategy should remain CROSS_RESUME, got %s", decision.FinalStrategy)
	}
}

func TestResumeDedupGateCanSkipMultipleRepeatedSafePoints(t *testing.T) {
	program := audioout.RoomProgramSnapshot{
		SafePoints: []audioout.ProgramSafePoint{
			{ID: "sp-1", CutMS: 10000, NextPreview: "非转基因低芥酸，家里吃着放心。"},
			{ID: "sp-2", CutMS: 20000, NextPreview: "这款也是低芥酸非转基因原料。"},
			{ID: "sp-3", CutMS: 30000, NextPreview: "一号链接今天有试用装活动。"},
		},
		Task: &audioout.SpeechTask{DurationMS: 40000},
	}
	decision := applyResumeDedupGate(program, "这款低芥酸、非转基因，家里吃更安心。", 10000, "DIRECT")
	if !decision.Triggered || decision.FinalMS != 30000 || decision.SkippedPoints != 2 {
		t.Fatalf("expected two repeated safe points to be skipped: %#v", decision)
	}
	if decision.FinalStrategy != "CROSS_RESUME" {
		t.Fatalf("multiple skipped points should upgrade to CROSS_RESUME, got %s", decision.FinalStrategy)
	}
}

func TestResumeDedupGateLeavesSwitchPlanAtTrackEnd(t *testing.T) {
	program := resumeDedupTestProgram()
	decision := applyResumeDedupGate(program, "中通极兔邮政随机发。", 60000, "SWITCH_PLAN")
	if decision.Triggered || decision.FinalMS != 60000 || decision.FinalStrategy != "SWITCH_PLAN" {
		t.Fatalf("track-end switch plan should remain unchanged: %#v", decision)
	}
}
