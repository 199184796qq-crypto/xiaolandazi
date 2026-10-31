package paidpipeline

import (
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"testing"
	"time"
)

func TestCurrentRuntimeObservesActualPCMOpeningWindow(t *testing.T) {
	r := agentwork.New()
	r.SetPlan(29, 7, "plan")
	r.SetMode(29, agentwork.ModeAnchor)
	r.StartAgent(29, 0)
	p := New(r, agentdecision.New())
	now := time.Now()
	p.now = func() time.Time { return now }
	started := time.Time{}
	p.SetAudioStartedReader(func(int64) time.Time { return started })
	s := p.v2Scope(14, 29)
	if p.v2Policy(s).Active {
		t.Fatal("interaction admitted before actual PCM")
	}
	started = now
	now = now.Add(119 * time.Second)
	if p.v2Policy(s).Active {
		t.Fatal("opening observed from collection instead of actual audio")
	}
	now = now.Add(time.Second)
	if !p.v2Policy(s).Active {
		t.Fatal("opening window never released")
	}
}

func TestCurrentSessionLifecycleResetsSchedulerAndKeepsTimedQueueOwnership(t *testing.T) {
	r := agentwork.New()
	r.SetPlan(29, 7, "plan")
	r.StartAgent(29, 0)
	q := agentdecision.New()
	p := New(r, q)
	timed := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceManual, Topic: "timed", Question: "人工定时任务", TTLSeconds: 120}).Item
	p.Handle(model.RoomEvent{ID: 1, RoomID: 29, TenantID: 14, EventType: "chat", Content: "哪里发货", OccurredAt: time.Now()}, basepipeline.Signal{})
	if len(p.interactionV2.Load().engine.Snapshot(29)) != 1 {
		t.Fatal("current task missing")
	}
	p.Handle(model.RoomEvent{RoomID: 29, EventType: "session_start"}, basepipeline.Signal{})
	if len(p.interactionV2.Load().engine.Snapshot(29)) != 0 {
		t.Fatal("prior-run scheduler leaked")
	}
	if _, ok := q.Get(29, timed.ID); !ok {
		t.Fatal("session start erased timed queue item")
	}
	p.Handle(model.RoomEvent{RoomID: 29, EventType: "session_end"}, basepipeline.Signal{})
	if r.IsWorking(29) {
		t.Fatal("session end left paid runtime working")
	}
	if _, ok := q.Get(29, timed.ID); !ok {
		t.Fatal("session end erased timed queue ownership")
	}
}
