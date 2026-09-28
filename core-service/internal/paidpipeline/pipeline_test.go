package paidpipeline

import (
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
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
