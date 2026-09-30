package paidpipeline

import (
	"strings"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/strategycenter"
)

func TestPaidPipelineOnlyQueuesWhileRuntimeWorking(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	pipeline := New(runtime, decisions)
	event := model.RoomEvent{
		ID:         9,
		RoomID:     22,
		EventType:  "chat",
		UserID:     "u-1",
		Content:    "什么时候发货？",
		OccurredAt: time.Now().UTC(),
	}
	signal := basepipeline.Signal{
		RoomID:     22,
		EventID:    9,
		UserID:     "u-1",
		Content:    event.Content,
		Topic:      "FAMILY:发货物流",
		IsQuestion: true,
	}

	pipeline.Handle(event, signal)
	if got := decisions.Snapshot(22).Queue; len(got) != 0 {
		t.Fatalf("paid decisions must stay empty while runtime stopped: %#v", got)
	}

	if _, err := runtime.GrantLease(22, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(22, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.SetMode(22, agentwork.ModeAnchor); err != nil {
		t.Fatal(err)
	}
	pipeline.Handle(event, signal)
	if got := decisions.Snapshot(22).Queue; len(got) != 1 {
		t.Fatalf("expected one paid decision while running: %#v", got)
	}

	if _, err := runtime.Set(22, agentwork.StateStopped); err != nil {
		t.Fatal(err)
	}
	pipeline.Handle(model.RoomEvent{RoomID: 22, EventType: "chat", Content: "多少钱？"}, basepipeline.Signal{
		RoomID:     22,
		Content:    "多少钱？",
		Topic:      "FAMILY:价格费用",
		IsQuestion: true,
	})
	if got := decisions.Snapshot(22).Queue; len(got) != 1 {
		t.Fatalf("stopped runtime must not add paid work: %#v", got)
	}
}

func TestControlModeAlsoAutoQueuesQuestions(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	pipeline := New(runtime, decisions)
	if _, err := runtime.GrantLease(5, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(5, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	pipeline.Handle(model.RoomEvent{RoomID: 5, EventType: "chat", Content: "多少钱？"}, basepipeline.Signal{
		RoomID: 5, Content: "多少钱？", Topic: "FAMILY:价格费用", IsQuestion: true,
	})
	if got := decisions.Snapshot(5).Queue; len(got) != 1 {
		t.Fatalf("control mode should auto queue monitor-agent decisions: %#v", got)
	}
	if _, err := runtime.SetMode(5, agentwork.ModeAnchor); err != nil {
		t.Fatal(err)
	}
	pipeline.Handle(model.RoomEvent{RoomID: 5, EventType: "chat", Content: "什么时候发货？"}, basepipeline.Signal{
		RoomID: 5, Content: "什么时候发货？", Topic: "FAMILY:发货物流", IsQuestion: true,
	})
	if got := decisions.Snapshot(5).Queue; len(got) != 2 {
		t.Fatalf("anchor mode should auto queue paid decisions: %#v", got)
	}
}

func TestSessionEndStopsPaidPipelineWithoutClearingTimedInteractionTasks(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	pipeline := New(runtime, decisions)
	if _, err := runtime.GrantLease(7, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(7, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	decisions.Enqueue(7, agentdecision.Candidate{
		Source:   agentdecision.SourceAgent,
		Topic:    "FAMILY:发货物流",
		Question: "什么时候发货",
	})
	pipeline.Handle(model.RoomEvent{RoomID: 7, EventType: "session_end"}, basepipeline.Signal{RoomID: 7})
	stopped := runtime.Get(7)
	if stopped.State != agentwork.StateStopped {
		t.Fatalf("session end state=%q want stopped", stopped.State)
	}
	if stopped.StopReason != agentwork.StopReasonLiveFinished {
		t.Fatalf("session end stop reason=%q want live_finished", stopped.StopReason)
	}
	if got := decisions.Snapshot(7).Queue; len(got) != 1 {
		t.Fatalf("session end must keep timed interaction tasks until their own expiry: %#v", got)
	}
}

func TestSessionStartDoesNotClearTimedInteractionTasks(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	pipeline := New(runtime, decisions)
	decisions.Enqueue(8, agentdecision.Candidate{
		Source:   agentdecision.SourceAgent,
		Topic:    "FAMILY:价格费用",
		Question: "多少钱",
	})

	pipeline.Handle(model.RoomEvent{
		RoomID: 8, EventType: "session_start", OccurredAt: time.Now().UTC(),
	}, basepipeline.Signal{RoomID: 8})
	if got := decisions.Snapshot(8).Queue; len(got) != 1 {
		t.Fatalf("session start must not clear timed interaction tasks: %#v", got)
	}
}

func TestLikeInteractionEmitsByMaxWaitWithoutProbabilityRoll(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	policies := strategycenter.New()
	pipeline := New(runtime, decisions, policies)
	roomID := int64(31)
	if _, err := runtime.GrantLease(roomID, 3600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(roomID, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	pipeline.Handle(model.RoomEvent{
		ID: 1, TenantID: 14, RoomID: roomID, EventType: "like", OccurredAt: base,
	}, basepipeline.Signal{RoomID: roomID})

	if got := decisions.Snapshot(roomID).Queue; len(got) != 0 {
		t.Fatalf("like should wait for interaction window before speech mission: %#v", got)
	}
	pipeline.FlushInteractionWindows(base.Add(59 * time.Second))
	if got := decisions.Snapshot(roomID).Queue; len(got) != 0 {
		t.Fatalf("like should not emit before max wait: %#v", got)
	}
	pipeline.FlushInteractionWindows(base.Add(60 * time.Second))
	got := decisions.Snapshot(roomID).Queue
	if len(got) != 1 {
		t.Fatalf("like must emit when max wait expires: %#v", got)
	}
	if got[0].MissionKind != "reply_like" || got[0].MissionEventCount != 1 {
		t.Fatalf("unexpected like mission: %#v", got[0])
	}

	stats := policies.StageStatsForTenant(roomID, 14)
	var like *strategycenter.InteractionWindowStat
	for i := range stats.InteractionItems {
		if stats.InteractionItems[i].Key == "reply_like" {
			like = &stats.InteractionItems[i]
			break
		}
	}
	if like == nil || like.TotalEvents != 1 || like.EmittedCount != 1 || like.PendingCount != 0 {
		t.Fatalf("unexpected like interaction stats: %#v", like)
	}
}

func TestNamedWelcomeUsesTimeWindowInsteadOfHeatGate(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	policies := strategycenter.New()
	pipeline := New(runtime, decisions, policies)
	roomID := int64(32)
	if _, err := runtime.GrantLease(roomID, 3600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(roomID, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	pipeline.Handle(model.RoomEvent{
		ID: 2, TenantID: 14, RoomID: roomID, EventType: "member",
		Nickname: "小王", UserID: "u-2", OccurredAt: base,
	}, basepipeline.Signal{RoomID: roomID})

	pipeline.FlushInteractionWindows(base.Add(9 * time.Second))
	if got := decisions.Snapshot(roomID).Queue; len(got) != 0 {
		t.Fatalf("named welcome should respect its window: %#v", got)
	}
	pipeline.FlushInteractionWindows(base.Add(10 * time.Second))
	got := decisions.Snapshot(roomID).Queue
	if len(got) != 1 || got[0].MissionKind != "welcome_named" {
		t.Fatalf("named welcome must emit at max wait without heat gate: %#v", got)
	}
}

func TestInteractionWeightsDriveRuntimePriorityAndCanDisableWindow(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	policies := strategycenter.New()
	policy := strategycenter.DefaultPolicy()
	for i := range policy.Rules {
		if policy.Rules[i].Category != "interaction" {
			continue
		}
		switch policy.Rules[i].Key {
		case "reply_like":
			policy.Rules[i].BaseProbability = 100
		case "reply_follow":
			policy.Rules[i].Enabled = false
			policy.Rules[i].BaseProbability = 0
		}
	}
	policies.Put(0, policy)
	pipeline := New(runtime, decisions, policies)
	roomID := int64(45)
	if _, err := runtime.GrantLease(roomID, 3600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(roomID, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	pipeline.Handle(model.RoomEvent{ID: 1, TenantID: 14, RoomID: roomID, EventType: "follow", OccurredAt: base}, basepipeline.Signal{RoomID: roomID})
	if got := decisions.Snapshot(roomID).Queue; len(got) != 0 {
		t.Fatalf("disabled follow interaction should not create work: %#v", got)
	}
	pipeline.Handle(model.RoomEvent{ID: 2, TenantID: 14, RoomID: roomID, EventType: "like", OccurredAt: base}, basepipeline.Signal{RoomID: roomID})
	pipeline.FlushInteractionWindows(base.Add(60 * time.Second))
	got := decisions.Snapshot(roomID).Queue
	if len(got) != 1 || got[0].MissionKind != "reply_like" {
		t.Fatalf("expected weighted like mission: %#v", got)
	}
	if got[0].Priority != interactionEffectivePriority(defaultInteractionRules["reply_like"].Priority, 100) {
		t.Fatalf("unexpected weighted priority=%d", got[0].Priority)
	}

	stats := policies.StageStatsForTenant(roomID, 14)
	var like, follow *strategycenter.InteractionWindowStat
	for i := range stats.InteractionItems {
		switch stats.InteractionItems[i].Key {
		case "reply_like":
			like = &stats.InteractionItems[i]
		case "reply_follow":
			follow = &stats.InteractionItems[i]
		}
	}
	if like == nil || like.ConfiguredWeight != 100 || like.EffectiveWeight != 100 {
		t.Fatalf("like weights not exposed in stats: %#v", like)
	}
	if follow == nil || follow.State != "disabled" || follow.EffectiveWeight != 0 {
		t.Fatalf("disabled follow state not exposed: %#v", follow)
	}
}

func TestInteractionStatsInitializeAllWindowStrategies(t *testing.T) {
	policies := strategycenter.New()
	pipeline := New(agentwork.New(), agentdecision.New(), policies)
	pipeline.RefreshInteractionStats(44)
	stats := policies.StageStatsForTenant(44, 14)
	if len(stats.InteractionItems) != 5 {
		t.Fatalf("interaction window catalog size=%d want=5: %#v", len(stats.InteractionItems), stats.InteractionItems)
	}
}

func TestQuestionDebtGrowsForRepeatedTopicAndMultipleUsers(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	pipeline := New(runtime, decisions, strategycenter.New())
	roomID := int64(71)
	if _, err := runtime.GrantLease(roomID, 3600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(roomID, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 29, 19, 10, 0, 0, time.UTC)
	firstEvent := model.RoomEvent{
		ID: 101, RoomID: roomID, EventType: "chat", UserID: "u-1", Nickname: "小陈",
		Content: "这个多少钱？", OccurredAt: base,
	}
	firstSignal := basepipeline.Signal{
		RoomID: roomID, EventID: 101, UserID: "u-1", Content: firstEvent.Content,
		Topic: "FAMILY:价格费用", IsQuestion: true,
	}
	pipeline.Handle(firstEvent, firstSignal)
	first := decisions.Snapshot(roomID).Queue
	if len(first) != 1 || first[0].InteractionDecision.QuestionDebt == nil {
		t.Fatalf("first question debt missing: %#v", first)
	}
	firstValue := first[0].InteractionDecision.EventValue
	firstPriority := first[0].Priority

	secondEvent := model.RoomEvent{
		ID: 102, RoomID: roomID, EventType: "chat", UserID: "u-2", Nickname: "阿芳",
		Content: "多少钱一桶？", OccurredAt: base.Add(20 * time.Second),
	}
	secondSignal := basepipeline.Signal{
		RoomID: roomID, EventID: 102, UserID: "u-2", Content: secondEvent.Content,
		Topic: "FAMILY:价格费用", IsQuestion: true,
	}
	pipeline.Handle(secondEvent, secondSignal)
	queue := decisions.Snapshot(roomID).Queue
	if len(queue) != 1 {
		t.Fatalf("same topic should merge into one decision: %#v", queue)
	}
	debt := queue[0].InteractionDecision.QuestionDebt
	if debt == nil || debt.RepeatCount != 2 || debt.UniqueUsers != 2 || debt.WaitingSeconds < 20 {
		t.Fatalf("question debt did not grow: %#v", debt)
	}
	if queue[0].InteractionDecision.EventValue <= firstValue {
		t.Fatalf("repeated question should increase event value: first=%v second=%v", firstValue, queue[0].InteractionDecision.EventValue)
	}
	if queue[0].Priority <= firstPriority {
		t.Fatalf("repeated question should increase priority: first=%d second=%d", firstPriority, queue[0].Priority)
	}
	if len(queue[0].InteractionDecision.MergedEventIDs) != 2 {
		t.Fatalf("merged event ids=%v want two events", queue[0].InteractionDecision.MergedEventIDs)
	}
}

func TestInteractionBudgetReservesHighValueSlot(t *testing.T) {
	pipeline := New(agentwork.New(), agentdecision.New(), strategycenter.New())
	roomID := int64(72)
	now := time.Date(2026, 9, 29, 19, 20, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		_, allowed, _ := pipeline.tryConsumeInteractionBudget(roomID, "WARM", false, now)
		if !allowed {
			t.Fatalf("normal slot %d should be allowed", i+1)
		}
	}
	budget, allowed, _ := pipeline.tryConsumeInteractionBudget(roomID, "WARM", false, now)
	if allowed {
		t.Fatalf("normal event should be held after normal capacity is consumed: %#v", budget)
	}
	budget, allowed, _ = pipeline.tryConsumeInteractionBudget(roomID, "WARM", true, now)
	if !allowed || budget.RemainingSlots != 0 {
		t.Fatalf("reserved high-value slot should remain available: allowed=%v budget=%#v", allowed, budget)
	}
}

func TestDueInteractionWindowsCompeteByEventValue(t *testing.T) {
	runtime := agentwork.New()
	decisions := agentdecision.New()
	pipeline := New(runtime, decisions, strategycenter.New())
	roomID := int64(73)
	if _, err := runtime.GrantLease(roomID, 3600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Set(roomID, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 19, 30, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, allowed, _ := pipeline.tryConsumeInteractionBudget(roomID, "WARM", false, now); !allowed {
			t.Fatalf("failed to pre-consume normal budget slot %d", i+1)
		}
	}
	pipeline.interactionMu.Lock()
	pipeline.interactionWindows[roomID] = map[string]*interactionWindow{
		"reply_follow": {
			TenantID: 14, RoomID: roomID, Key: "reply_follow", PendingCount: 3,
			FirstPending: now.Add(-45 * time.Second), LastEventAt: now,
			LatestEventID: 201, EventIDs: []int64{199, 200, 201},
		},
		"reply_like": {
			TenantID: 14, RoomID: roomID, Key: "reply_like", PendingCount: 30,
			FirstPending: now.Add(-60 * time.Second), LastEventAt: now,
			LatestEventID: 301, EventIDs: []int64{301},
		},
	}
	pipeline.interactionMu.Unlock()

	pipeline.FlushInteractionWindows(now)
	queue := decisions.Snapshot(roomID).Queue
	if len(queue) != 1 || queue[0].MissionKind != "reply_follow" {
		t.Fatalf("only higher-value due mission should pass remaining normal budget: %#v", queue)
	}
	pipeline.interactionMu.Lock()
	like := pipeline.interactionWindows[roomID]["reply_like"]
	follow := pipeline.interactionWindows[roomID]["reply_follow"]
	pipeline.interactionMu.Unlock()
	if follow.PendingCount != 0 || !follow.LastBudgetAllowed {
		t.Fatalf("follow should be emitted: %#v", follow)
	}
	if like.PendingCount != 30 || like.LastBudgetAllowed || !strings.Contains(like.LastDecisionReason, "保护主线") {
		t.Fatalf("lower-value like window should remain pending with budget reason: %#v", like)
	}
}
