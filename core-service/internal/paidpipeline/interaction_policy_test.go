package paidpipeline

import (
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/strategycenter"
	"testing"
	"time"
)

func TestDirectClassificationAndFollowupScope(t *testing.T) {
	r := agentwork.New()
	q := agentdecision.New()
	p := New(r, q, strategycenter.New())
	r.Set(7, agentwork.StateWorking)
	events := []struct {
		kind, text string
		s          basepipeline.Signal
	}{{"chat", "东西坏了", basepipeline.Signal{IsNegative: true}}, {"chat", "我下单了", basepipeline.Signal{IsOrderHint: true}}, {"order", "订单事件", basepipeline.Signal{}}, {"chat", "多少钱", basepipeline.Signal{IsQuestion: true, Topic: "FAMILY:价格费用"}}, {"chat", "两件多少钱", basepipeline.Signal{IsQuestion: true, Topic: "FAMILY:价格费用"}}}
	for i, e := range events {
		e.s.Content = e.text
		p.Handle(model.RoomEvent{ID: int64(i + 1), RoomID: 7, TenantID: 2, EventType: e.kind, Content: e.text, OccurredAt: time.Now().UTC()}, e.s)
	}
	items := q.Snapshot(7).Queue
	if len(items) != 5 {
		t.Fatalf("classification/followup lost: %d", len(items))
	}
	if items[0].Scene != "complaint" || items[0].PriorityClass != "P0" {
		t.Fatal("non-question complaint treated as chat")
	}
	sources := map[string]bool{}
	for _, i := range items {
		if i.Scene == "order" {
			sources[i.OrderSource] = true
		}
	}
	if !sources["viewer_claim"] || !sources["platform_order"] {
		t.Fatal("order sources mixed")
	}
}
func TestRoomConfigurationFrozenUntilNextStart(t *testing.T) {
	r := agentwork.New()
	q := agentdecision.New()
	policies := strategycenter.New()
	cfg, err := strategycenter.LoadInteractionMother()
	if err != nil {
		t.Fatal(err)
	}
	prefs := strategycenter.DefaultRoomInteractionPreferences(7)
	prefs.TenantID = 2
	prefs.Schedule = &cfg
	policies.PutRoomInteractionPreferences(7, prefs)
	p := New(r, q, policies)
	r.Set(7, agentwork.StateWorking)
	first, err := p.schedule(7, 2)
	if err != nil {
		t.Fatal(err)
	}
	rule := cfg.Events["product_question"]
	rule.Weight = 10
	cfg.Events["product_question"] = rule
	cfg.Version = 2
	prefs.Schedule = &cfg
	policies.PutRoomInteractionPreferences(7, prefs)
	same, _ := p.schedule(7, 2)
	if same.Events["product_question"].Weight != first.Events["product_question"].Weight {
		t.Fatal("running session changed silently")
	}
	r.Set(7, agentwork.StateStopped)
	r.Set(7, agentwork.StateWorking)
	next, _ := p.schedule(7, 2)
	if next.Version != 2 || next.Events["product_question"].Weight != 10 {
		t.Fatal("next start did not use saved configuration")
	}
}
func TestWindowCountsOnlyActuallyCompletedSpeech(t *testing.T) {
	now := time.Now().UTC()
	r := agentwork.New()
	q := agentdecision.NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	p := New(r, q, strategycenter.New())
	r.Set(7, agentwork.StateWorking)
	p.Handle(model.RoomEvent{ID: 1, RoomID: 7, TenantID: 2, EventType: "follow", OccurredAt: now}, basepipeline.Signal{})
	now = now.Add(10 * time.Second)
	p.FlushInteractionWindows(now)
	item, ok := q.ClaimNext(7)
	if !ok {
		t.Fatal("missing follow mission")
	}
	window := p.interactionWindows[7]["reply_follow"]
	if window.EmittedCount != 0 || !window.LastEmittedAt.IsZero() {
		t.Fatal("enqueue spent cooldown")
	}
	q.Complete(7, item.ID)
	p.FlushInteractionWindows(now)
	if window.EmittedCount != 1 || window.LastEmittedAt.IsZero() {
		t.Fatal("completion did not spend cooldown")
	}
	p.FlushInteractionWindows(now)
	if window.EmittedCount != 1 {
		t.Fatal("completion counted twice")
	}
}
