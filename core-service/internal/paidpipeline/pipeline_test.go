package paidpipeline

import (
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

func TestSessionEndStopsPaidPipelineWithoutTouchingBaseState(t *testing.T) {
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
	if got := decisions.Snapshot(7).Queue; len(got) != 0 {
		t.Fatalf("session end must clear paid queue: %#v", got)
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
