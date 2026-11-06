package paidpipeline

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/messagecaptain"
	"testing"
	"time"
)

func TestWarmupV2QueueDoesNotCancelItsOwnClaim(t *testing.T) {
	now := time.Now()
	p, q, _ := reportProcessor(t, &now, 24)
	prefs := p.policies.RoomInteractionPreferences(29)
	rule := prefs.Schedule.Events["warmup"]
	rule.Enabled = true
	prefs.Schedule.Events["warmup"] = rule
	p.policies.PutRoomInteractionPreferences(29, prefs)
	p.SetWarmupEligibility(func(int64) bool { return true })
	started := now
	p.SetAudioStartedReader(func(int64) time.Time { return started })
	s := p.v2Scope(14, 29)
	e := p.interactionV2.Load().engine
	now = now.Add(181 * time.Second)
	e.SetPolicy(s, p.v2Policy(s), now)
	e.Jobs(now, 1)
	jobs := e.Jobs(now.Add(time.Second), 1)
	if len(jobs) != 1 || jobs[0].Kind != "guide" {
		t.Fatal("missing cold-room guide", jobs)
	}
	w := jobs[0]
	raw, _ := json.Marshal(map[string]any{"protocol": interactionruntime.Protocol, "task": w.ID, "epoch": s.Epoch, "tenant_id": 14, "room_id": 29, "plan_id": s.Plan, "expires_at": w.Expires, "body": "继续看看商品。"})
	if !e.Accept(w, interactionruntime.Content{Body: "继续看看商品。", Intent: interactionruntime.Intent{Relevant: true}, Knowledge: raw}, nil, now.Add(time.Second)) {
		t.Fatal("guide body rejected")
	}
	now = now.Add(2 * time.Second)
	p.tickInteractionV2(context.Background(), now)
	queued := q.Snapshot(29).Queue
	if len(queued) != 1 || queued[0].MissionKind != "warmup" || queued[0].Scene != "interaction_v2" {
		t.Fatal("guide did not use production queue identity", queued)
	}
	claimed, ok := q.ClaimNext(29)
	if !ok {
		t.Fatal("claim")
	}
	for i := 0; i < 5; i++ {
		now = now.Add(time.Second)
		p.tickInteractionV2(context.Background(), now)
		if item, exists := q.Get(29, claimed.ID); !exists || item.Status != agentdecision.StatusClaimed {
			t.Fatal("warmup cancelled itself before TTS/playback", q.Snapshot(29))
		}
	}
	other := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceManual, Topic: "real-question", Question: "哪里发货", MissionKind: "reply_question", Scene: "interaction_v2", TTLSeconds: 60}).Item
	if other == nil {
		t.Fatal("other enqueue")
	}
	now = now.Add(time.Second)
	p.tickInteractionV2(context.Background(), now)
	if _, exists := q.Get(29, claimed.ID); exists {
		t.Fatal("warmup failed to yield to a genuinely different interaction")
	}
}

func TestWarmupBusyPolicyUsesMissionInsteadOfSharedScene(t *testing.T) {
	for _, tc := range []struct {
		mission, event, scene string
		busy                  bool
	}{
		{"warmup", "guide", "interaction_v2", false},
		{"warmup", "", "interaction_v2", false},
		{"", "guide", "interaction_v2", false},
		{"", "", "warmup", false},
		{"reply_question", "semantic", "interaction_v2", true},
		{"welcome_named", "welcome", "interaction_v2", true},
		{"reply_question", "semantic", "warmup", true},
	} {
		t.Run(tc.mission+"/"+tc.event+"/"+tc.scene, func(t *testing.T) {
			now := time.Now()
			p, q, _ := reportProcessor(t, &now, 24)
			q.Enqueue(29, agentdecision.Candidate{Topic: "one", Question: "内容", MissionKind: tc.mission, Scene: tc.scene, TTLSeconds: 60, InteractionDecision: agentdecision.InteractionDecision{PrimaryEvent: tc.event}})
			if got := p.v2Policy(p.v2Scope(14, 29)).Busy; got != tc.busy {
				t.Fatalf("Busy=%v want %v", got, tc.busy)
			}
		})
	}
}

func TestWarmupOwnQueueSurvivesReservationAndTrustedPlaybackReceipts(t *testing.T) {
	now := time.Now()
	started := now
	p, q, _ := reportProcessor(t, &now, 24)
	prefs := p.policies.RoomInteractionPreferences(29)
	rule := prefs.Schedule.Events["warmup"]
	rule.Enabled = true
	prefs.Schedule.Events["warmup"] = rule
	p.policies.PutRoomInteractionPreferences(29, prefs)
	p.SetWarmupEligibility(func(int64) bool { return true })
	p.SetAudioStartedReader(func(int64) time.Time { return started })
	s := p.v2Scope(14, 29)
	e := p.interactionV2.Load().engine
	now = started.Add(181 * time.Second)
	e.SetPolicy(s, p.v2Policy(s), now)
	e.Jobs(now, 1)
	work := e.Jobs(now.Add(time.Second), 1)[0]
	raw, _ := json.Marshal(map[string]any{"protocol": interactionruntime.Protocol, "task": work.ID, "epoch": s.Epoch, "tenant_id": 14, "room_id": 29, "plan_id": s.Plan, "expires_at": work.Expires, "body": "继续看看商品。"})
	if !e.Accept(work, interactionruntime.Content{Body: "继续看看商品。", Intent: interactionruntime.Intent{Relevant: true}, Knowledge: raw}, nil, now.Add(time.Second)) {
		t.Fatal("accept")
	}
	now = now.Add(2 * time.Second)
	p.tickInteractionV2(context.Background(), now)
	claimed, ok := q.ClaimNext(29)
	if !ok {
		t.Fatal("claim")
	}
	request, _ := json.Marshal(map[string]any{"room_id": 29, "tenant_id": 14, "epoch": s.Epoch, "decision_id": claimed.ID, "duration_ms": 3000})
	if err := p.preflightAudio(messagecaptain.Message{RoomID: 29, RunID: s.Epoch, Payload: request}); err != nil {
		t.Fatal("pre-purchase self-cancellation", err)
	}
	if !e.ReserveAudio(29, claimed.ID, 3000, now) {
		t.Fatal("reservation cancelled guide")
	}
	q.BindPlayback(29, claimed.ID, "warmup-audio")
	for _, status := range []string{"PLAYING", "COMPLETED"} {
		now = now.Add(2 * time.Second)
		raw, _ := json.Marshal(map[string]any{"decision_id": claimed.ID, "status": status, "at": now, "played_ms": 3000})
		p.handleRuntimeMessage(context.Background(), messagecaptain.Message{Kind: "interaction.v2.playback", RoomID: 29, RunID: s.Epoch, TaskID: "warmup-audio", Payload: raw})
		want := "playing"
		if status == "COMPLETED" {
			want = "completed"
		}
		if task := e.Snapshot(29)[0]; task.State != want {
			t.Fatal("own queue cancelled trusted receipt", status, task)
		}
	}
	q.Complete(29, claimed.ID)
	end := now
	now = end.Add(179 * time.Second)
	p.tickInteractionV2(context.Background(), now)
	if len(e.Snapshot(29)) != 1 {
		t.Fatal("new guide before 180 seconds from actual end")
	}
}
