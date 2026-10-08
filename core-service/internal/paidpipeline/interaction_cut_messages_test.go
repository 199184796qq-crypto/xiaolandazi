package paidpipeline

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/model"
	"testing"
	"time"
)

func TestBroadcastCutAdmissionUsesFreshViewersAndKeepsOneLowMission(t *testing.T) {
	now := time.Now().UTC()
	r := agentwork.New()
	r.Set(7, agentwork.StateWorking)
	r.SetMode(7, agentwork.ModeAnchor)
	q := agentdecision.NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	p := New(r, q)
	p.now = func() time.Time { return now }
	c := messagecaptain.New()
	p.SetMessageCaptain(c)
	lead := 30000
	requests := 0
	unrelated := 0
	c.Register("unrelated-module", func(_ context.Context, m messagecaptain.Message) error { unrelated++; return nil })
	c.Register("mainline-fixture", func(ctx context.Context, m messagecaptain.Message) error {
		if m.Kind != messagecaptain.CutRequested {
			return nil
		}
		requests++
		var request messagecaptain.CutRequest
		json.Unmarshal(m.Payload, &request)
		payload, _ := json.Marshal(messagecaptain.CutOffer{RequestID: request.RequestID, Epoch: request.Epoch, CutMS: 42000, LeadMS: lead})
		c.Publish(ctx, messagecaptain.Message{Kind: messagecaptain.CutOffered, RoomID: m.RoomID, RunID: "program", TaskID: "mainline-1", OccurredAt: m.OccurredAt, Payload: payload})
		return nil
	})
	p.accumulateInteraction(model.RoomEvent{ID: 1, RoomID: 7, EventType: "member", Nickname: "已离开的旧人", OccurredAt: now.Add(-25 * time.Second)}, "welcome_named", "")
	p.accumulateInteraction(model.RoomEvent{ID: 2, RoomID: 7, EventType: "member", Nickname: "当前新人", OccurredAt: now.Add(-6 * time.Second)}, "welcome_named", "")
	p.accumulateInteraction(model.RoomEvent{ID: 2, RoomID: 7, EventType: "member", Nickname: "当前新人", OccurredAt: now.Add(-6 * time.Second)}, "welcome_batch", "")
	p.FlushInteractionWindows(now)
	if len(q.Snapshot(7).Queue) != 0 || requests == 0 {
		t.Fatal("far cut must publish intent without queueing a viewer")
	}
	lead = 15000
	p.FlushInteractionWindows(now)
	items := q.Snapshot(7).Queue
	if len(items) != 1 || len(items[0].Nicknames) != 1 || items[0].Nicknames[0] != "当前新人" {
		t.Fatalf("fresh admission=%#v", items)
	}
	if items[0].PreparedTaskID != "mainline-1" || items[0].PreparedCutMS != 42000 {
		t.Fatal("cut offer must follow the existing queue item")
	}
	if !items[0].ExpiresAt.Equal(now.Add(14 * time.Second)) {
		t.Fatalf("admission extended original event lifetime: %s", items[0].ExpiresAt)
	}
	for _, key := range []string{"welcome_named", "welcome_batch"} {
		if p.interactionWindows[7][key].PendingCount != 0 {
			t.Fatal("same arrivals must not be greeted again through the other welcome window")
		}
	}
	p.accumulateInteraction(model.RoomEvent{ID: 3, RoomID: 7, EventType: "like", OccurredAt: now.Add(-12 * time.Second)}, "reply_like", "")
	p.FlushInteractionWindows(now)
	if len(q.Snapshot(7).Queue) != 1 {
		t.Fatal("one cut may reserve only one low-value mission")
	}
	if unrelated < requests {
		t.Fatal("messages must also reach members which discard them")
	}
}

func TestCutIntentWaitsWhileQuestionsQueuedOrClaimed(t *testing.T) {
	now := time.Now().UTC()
	r := agentwork.New()
	r.Set(7, agentwork.StateWorking)
	r.SetMode(7, agentwork.ModeAnchor)
	q := agentdecision.NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	p := New(r, q)
	p.now = func() time.Time { return now }
	c := messagecaptain.New()
	p.SetMessageCaptain(c)
	requests := 0
	c.Register("observer", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == messagecaptain.CutRequested {
			requests++
		}
		return nil
	})
	q.Enqueue(7, agentdecision.Candidate{Source: agentdecision.SourceAgent, Scene: "product_question", Topic: "规格区别", PriorityClass: "P1"})
	p.Handle(model.RoomEvent{ID: 2, RoomID: 7, EventType: "member", Nickname: "小王", OccurredAt: now.Add(-6 * time.Second)}, basepipeline.Signal{})
	p.FlushInteractionWindows(now)
	if requests != 0 || len(q.Snapshot(7).Queue) != 1 {
		t.Fatal("greeting must wait behind pending question")
	}
	q.ClaimNext(7)
	p.FlushInteractionWindows(now)
	if requests != 0 || q.Snapshot(7).Queue[0].Status != agentdecision.StatusClaimed {
		t.Fatal("active answer must retain ownership")
	}
	now = now.Add(21 * time.Second)
	p.FlushInteractionWindows(now)
	if p.interactionWindows[7]["welcome_named"].PendingCount != 0 {
		t.Fatal("waiting greeting should expire without creating backlog")
	}
}

func TestPreparationP95AndObsoleteCutOffer(t *testing.T) {
	p := New(agentwork.New(), agentdecision.New())
	c := messagecaptain.New()
	p.SetMessageCaptain(c)
	for i := 0; i < 20; i++ {
		raw, _ := json.Marshal(messagecaptain.Preparation{DecisionID: time.Duration(i).String(), ElapsedMS: 10000 + i*100})
		c.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.InteractionPrepared, RoomID: 7, Payload: raw})
		c.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.InteractionPrepared, RoomID: 7, Payload: raw})
	}
	if len(p.cutMessages.samples[7]) != 20 || p.preparationLeadLocked(7) != 14800 {
		t.Fatal("use bounded observed P95 plus margin, without counting retries twice")
	}
	p.cutMessages.intents[7] = &cutIntent{request: messagecaptain.CutRequest{RequestID: "new", Epoch: "current"}}
	raw, _ := json.Marshal(messagecaptain.CutOffer{RequestID: "old", Epoch: "previous", LeadMS: 15000})
	c.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.CutOffered, RoomID: 7, TaskID: "old-task", Payload: raw})
	if p.cutMessages.intents[7].taskID != "" {
		t.Fatal("old scene offer must be discarded")
	}
}
