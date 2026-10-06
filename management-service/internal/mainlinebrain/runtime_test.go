package mainlinebrain

import (
	"testing"
	"time"
)

func TestMemorySnapshotUsesProgressiveDetailAndPinnedFacts(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	items := []MemoryItem{
		{ID: "hot", Topic: "版型", Summary: "还没讲完尺码", ExactText: "上一段原话", CreatedAt: now.Add(-30 * time.Second)},
		{ID: "warm", Topic: "面料", Summary: "讲过通勤场景", ExactText: "不应进入温记忆", CreatedAt: now.Add(-5 * time.Minute)},
		{ID: "cool", Topic: "颜色", Summary: "讲过颜色选择", Angle: "不应进入冷记忆", ExactText: "不应进入冷记忆", CreatedAt: now.Add(-20 * time.Minute)},
		{ID: "cold", Topic: "价格", Summary: "不应进入冷记忆", ExactText: "不应进入冷记忆", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "pinned", Topic: "价格", ExactText: "固定事实不能衰减", Pinned: true, CreatedAt: now.Add(-2 * time.Hour)},
	}
	snapshot := BuildMemorySnapshot(items, now, DefaultMemoryPolicy())
	if len(snapshot.Hot) != 1 || snapshot.Hot[0].ExactText == "" {
		t.Fatalf("hot memory lost exact text: %+v", snapshot.Hot)
	}
	if len(snapshot.Warm) != 1 || snapshot.Warm[0].ExactText != "" || snapshot.Warm[0].Summary == "" {
		t.Fatalf("warm memory was not blurred: %+v", snapshot.Warm)
	}
	if len(snapshot.Cool) != 1 || snapshot.Cool[0].Angle != "" || snapshot.Cool[0].ExactText != "" {
		t.Fatalf("cool memory was not blurred: %+v", snapshot.Cool)
	}
	if len(snapshot.Cold) != 1 || snapshot.Cold[0].Summary != "" || snapshot.Cold[0].Topic != "价格" {
		t.Fatalf("cold memory was not reduced to topic: %+v", snapshot.Cold)
	}
	if len(snapshot.Pinned) != 1 || snapshot.Pinned[0].ExactText == "" {
		t.Fatalf("pinned memory decayed: %+v", snapshot.Pinned)
	}
}

func TestContinuousMemoryCommitsOnlyAfterPlayback(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	state := NewSessionState("session-1", ModeContinuous, now)
	task := SegmentTask{ID: "segment-1", SessionID: state.ID, Topic: "版型", Purpose: "解释版型", Sequence: 1}
	if err := state.AcceptSegment(task, "这一段已经通过审计，但还没有播放。", now); err != nil {
		t.Fatal(err)
	}
	if got := len(state.Memory); got != 0 {
		t.Fatalf("accepted continuous segment entered memory too early: %d", got)
	}
	if err := state.QueueSegment(task.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkPlayed(task.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := len(state.Memory); got != 1 || state.Memory[0].ExactText == "" {
		t.Fatalf("played segment did not enter memory: %+v", state.Memory)
	}
}

func TestPreviewMemoryCommitsAtAcceptance(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	state := NewSessionState("preview-1", ModePreview, now)
	if state.MemoryCommitMode != MemoryOnAccepted {
		t.Fatalf("preview commit mode=%q", state.MemoryCommitMode)
	}
	if err := state.AcceptSegment(SegmentTask{ID: "segment-1", SessionID: state.ID}, "预览已经接受的片段。", now); err != nil {
		t.Fatal(err)
	}
	if len(state.Memory) != 1 {
		t.Fatalf("preview segment did not enter memory: %+v", state.Memory)
	}
}

func TestActiveEngagementCooldownAndNoFakeReply(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	state := NewSessionState("session-1", ModeContinuous, now)
	engagement := Engagement{ID: "eng-1", Kind: EngagementCommentPrompt, Prompt: "认同的朋友公屏扣666"}
	if err := state.IssueEngagement(engagement, now); err != nil {
		t.Fatal(err)
	}
	if err := state.IssueEngagement(Engagement{ID: "eng-2", Prompt: "再次发起"}, now.Add(time.Second)); err == nil {
		t.Fatal("active engagement was not blocked")
	}
	state.ExpireEngagement(now.Add(20 * time.Second))
	if state.Engagement.Status != EngagementExpired {
		t.Fatalf("status=%q", state.Engagement.Status)
	}
	if len(state.EngagementHistory) != 1 || state.EngagementHistory[0].ResponseCounts != nil {
		t.Fatalf("expired engagement incorrectly claimed a response: %+v", state.EngagementHistory)
	}
	if err := state.IssueEngagement(Engagement{ID: "eng-2", Prompt: "再发起"}, now.Add(21*time.Second)); err == nil {
		t.Fatal("engagement cooldown was not enforced")
	}
	if err := state.IssueEngagement(Engagement{ID: "eng-2", Prompt: "再发起"}, now.Add(36*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := state.ObserveEngagement("eng-2", map[string]int{"yes": 4}, now.Add(37*time.Second)); err != nil {
		t.Fatal(err)
	}
	if state.EngagementHistory[len(state.EngagementHistory)-1].ResponseCounts["yes"] != 4 {
		t.Fatalf("response was not recorded: %+v", state.EngagementHistory)
	}
}

func TestDirectiveExpiresWithoutClearingMainlineMemory(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	state := NewSessionState("session-1", ModeContinuous, now)
	if err := state.AddMemory(MemoryItem{ID: "segment:old", Topic: "版型", ExactText: "已有内容", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := state.SetDirective(Directive{ID: "directive-1", Text: "未来十分钟重点讲尺码", ExpiresAt: now.Add(10 * time.Minute)}, now); err != nil {
		t.Fatal(err)
	}
	if state.ActiveDirective(now.Add(5*time.Minute)) == nil {
		t.Fatal("active directive disappeared early")
	}
	if state.ActiveDirective(now.Add(11*time.Minute)) != nil {
		t.Fatal("expired directive remained active")
	}
	if state.Directive.Status != DirectiveExpired || len(state.Memory) != 1 {
		t.Fatalf("directive expiry changed unrelated memory: directive=%+v memory=%+v", state.Directive, state.Memory)
	}
}

func TestConversionPressureHonorsOfferReadinessAndRepetition(t *testing.T) {
	moderate := ComputeConversionPressure(ConversionPressureInput{BaseIntensity: 45, OfferValidity: 0.8, ReadinessSignal: 0.4})
	quiet := ComputeConversionPressure(ConversionPressureInput{BaseIntensity: 45, OfferValidity: 0.8, ReadinessSignal: 0.4, RepetitionPenalty: 1, UncertaintyPenalty: 1})
	if moderate <= quiet || ConversionLevelForPressure(moderate) == ConversionNatural {
		t.Fatalf("pressure did not respond to signals: moderate=%d quiet=%d", moderate, quiet)
	}
	if got := ComputeConversionPressure(ConversionPressureInput{BaseIntensity: 100, OfferValidity: 1, ReadinessSignal: 1}); got != 100 {
		t.Fatalf("pressure was not clamped: %d", got)
	}
}
