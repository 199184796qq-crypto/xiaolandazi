package paidpipeline

import (
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/events"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/roomintel"
	"livecompanion/core/internal/strategycenter"
	"livecompanion/core/internal/timeline"
)

func TestPickDebtQuestionUsesNewestUnansweredRealQuestion(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC)
	view := roombrain.View{
		Intelligence: roombrain.IntelligenceView{
			TopTopics: []roombrain.TopicView{
				{
					Topic:          "FAMILY:价格费用",
					Count:          3,
					UniqueUsers:    2,
					LastAnsweredAt: now.Add(-4 * time.Minute),
					TTSQuestions: []roomintel.TopicQuestion{
						{EventID: 1, UserID: "old", Content: "旧问题", OccurredAt: now.Add(-6 * time.Minute)},
						{EventID: 2, UserID: "u2", Nickname: "小王", Content: "现在多少钱", OccurredAt: now.Add(-90 * time.Second)},
					},
				},
				{
					Topic:       "FAMILY:物流发货",
					Count:       1,
					UniqueUsers: 1,
					TTSQuestions: []roomintel.TopicQuestion{
						{EventID: 3, UserID: "u3", Content: "发什么快递", OccurredAt: now.Add(-30 * time.Second)},
					},
				},
			},
		},
	}
	got, ok := pickDebtQuestion(view, now, 0)
	if !ok {
		t.Fatal("expected question backlog")
	}
	if got.EventID != 3 || got.Content != "发什么快递" {
		t.Fatalf("picked=%#v", got)
	}
	if got.Topic != "FAMILY:物流发货" {
		t.Fatalf("topic=%s", got.Topic)
	}
}

func TestEnqueueQuestionDebtBacklogCreatesReplyQuestionMission(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC)
	runtime := agentwork.New()
	decisions := agentdecision.NewWithClock(func() time.Time { return now }, 5*time.Minute, 90*time.Second, 64)
	policies := strategycenter.New()
	p := New(runtime, decisions, policies)
	roomID := int64(15)

	view := roombrain.View{
		Intelligence: roombrain.IntelligenceView{
			Heat: "WARM",
			TopTopics: []roombrain.TopicView{
				{
					Topic:       "FAMILY:价格费用",
					Count:       2,
					UniqueUsers: 2,
					TTSQuestions: []roomintel.TopicQuestion{
						{EventID: 88, UserID: "u-88", Nickname: "测试用户", Content: "这个多少钱", OccurredAt: now.Add(-3 * time.Minute)},
					},
				},
			},
		},
	}
	debt := roombrain.DebtView{Value: 0.90, LastRaised: now.Add(-time.Minute)}
	if !p.enqueueQuestionDebtBacklog(roomID, view, debt, now) {
		t.Fatal("expected debt backlog question to enqueue")
	}
	queue := decisions.Snapshot(roomID).Queue
	if len(queue) != 1 {
		t.Fatalf("queue=%#v", queue)
	}
	if queue[0].MissionKind != "reply_question" {
		t.Fatalf("mission_kind=%s", queue[0].MissionKind)
	}
	if queue[0].SampleQuestions[0] != "这个多少钱" {
		t.Fatalf("questions=%#v", queue[0].SampleQuestions)
	}
	if queue[0].InteractionDecision.PrimaryEvent != "question_debt_backlog" {
		t.Fatalf("decision=%#v", queue[0].InteractionDecision)
	}
}

func TestPickDebtInteractionEventOnlyUsesNewRealEvent(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC)
	lastCompleted := now.Add(-2 * time.Minute)
	items := []model.RoomEvent{
		{ID: 1, RoomID: 15, EventType: "chat", Content: "太早的聊天", OccurredAt: now.Add(-3 * time.Minute)},
		{ID: 2, RoomID: 15, EventType: "chat", Content: "这一条可以互动", OccurredAt: now.Add(-90 * time.Second)},
		{ID: 3, RoomID: 15, EventType: "session_start", OccurredAt: now.Add(-30 * time.Second)},
	}
	event, key, ok := pickDebtInteractionEvent(items, now, lastCompleted, 0)
	if !ok {
		t.Fatal("expected interaction event")
	}
	if event.ID != 2 || key != "reply_chat" {
		t.Fatalf("event=%#v key=%s", event, key)
	}
	if _, _, ok := pickDebtInteractionEvent(items, now, lastCompleted, 2); ok {
		t.Fatal("same already-used event must not be selected again")
	}
}

func TestFlushDebtBacklogQuestionLifecycle(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	clockNow := now
	runtime := agentwork.New()
	if _, err := runtime.Set(15, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	decisions := agentdecision.NewWithClock(func() time.Time { return clockNow }, 5*time.Minute, 90*time.Second, 64)
	brain := roombrain.NewManager()
	brain.Ingest(model.RoomEvent{RoomID: 15, EventType: "session_start", OccurredAt: now.Add(-4 * time.Minute)})
	for index := 0; index < 6; index++ {
		at := now.Add(-70*time.Second + time.Duration(index)*11*time.Second)
		brain.Ingest(model.RoomEvent{
			ID:         int64(index + 1),
			TenantID:   7,
			RoomID:     15,
			EventType:  "chat",
			UserID:     "question-user",
			Nickname:   "测试用户",
			Content:    "这个多少钱？",
			OccurredAt: at,
		})
	}
	view, err := brain.Snapshot(15)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Timeline.Debts[string(timeline.DebtQuestion)].Value; got < debtBacklogThreshold {
		t.Fatalf("question debt=%v want >=%v", got, debtBacklogThreshold)
	}

	p := New(runtime, decisions, strategycenter.New())
	p.SetBrain(brain)
	p.now = func() time.Time { return clockNow }
	clockNow = now.Add(3 * time.Minute)
	p.FlushDebtBacklog(clockNow)

	queued := decisions.Snapshot(15).Queue
	if len(queued) != 1 {
		t.Fatalf("queue=%#v want one debt mission", queued)
	}
	if queued[0].MissionKind != "reply_question" || queued[0].InteractionDecision.PrimaryEvent != "question_debt_backlog" {
		t.Fatalf("unexpected debt mission=%#v", queued[0])
	}
	if queued[0].LinkedEventIDs[len(queued[0].LinkedEventIDs)-1] != 6 {
		t.Fatalf("debt mission did not use newest real question: %#v", queued[0].LinkedEventIDs)
	}

	// A pending mission owns the queue; the one-second production loop must not
	// enqueue another debt mission while it is waiting or executing.
	p.FlushDebtBacklog(clockNow.Add(time.Second))
	if got := decisions.Snapshot(15).Queue; len(got) != 1 {
		t.Fatalf("repeated flush duplicated debt mission: %#v", got)
	}

	claimed, ok := decisions.ClaimNext(15)
	if !ok {
		t.Fatal("debt mission was not claimable")
	}
	completedAt := clockNow.Add(30 * time.Second)
	brain.RecordPin(15, timeline.Pin{
		At:       completedAt,
		Kind:     timeline.PinAnswer,
		Topic:    claimed.Topic,
		Key:      claimed.ID,
		Strategy: "interaction.answer",
	}, timeline.DebtQuestion, 90*time.Second)
	clockNow = completedAt
	if _, ok := decisions.Complete(15, claimed.ID); !ok {
		t.Fatal("claimed debt mission was not completed")
	}

	clockNow = completedAt.Add(3 * time.Minute)
	p.FlushDebtBacklog(clockNow)
	if got := decisions.Snapshot(15).Queue; len(got) != 0 {
		t.Fatalf("completed debt was re-enqueued: %#v", got)
	}
}

func TestEnqueueInteractionDebtBacklogUsesRealEventOnce(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	runtime := agentwork.New()
	decisions := agentdecision.NewWithClock(func() time.Time { return now }, 5*time.Minute, 90*time.Second, 64)
	p := New(runtime, decisions, strategycenter.New())
	sessions := events.NewStore(nil, 500)
	sessions.Accept(model.RoomEvent{
		ID:         501,
		TenantID:   7,
		RoomID:     15,
		EventType:  "chat",
		UserID:     "chat-user",
		Nickname:   "直播观众",
		Content:    "这个看起来真不错",
		OccurredAt: now.Add(-time.Minute),
	})
	p.SetSessions(sessions)
	view := roombrain.View{Intelligence: roombrain.IntelligenceView{Heat: "WARM"}}
	debt := roombrain.DebtView{Value: 0.90, LastRaised: now.Add(-time.Minute)}

	if !p.enqueueInteractionDebtBacklog(15, view, debt, now.Add(-2*time.Minute), now) {
		t.Fatal("expected interaction debt backlog to enqueue")
	}
	queue := decisions.Snapshot(15).Queue
	if len(queue) != 1 || queue[0].MissionKind != "reply_chat" {
		t.Fatalf("unexpected interaction debt queue=%#v", queue)
	}
	if queue[0].InteractionDecision.PrimaryEvent != "reply_chat" || queue[0].LinkedEventIDs[0] != 501 {
		t.Fatalf("interaction debt did not preserve real event: %#v", queue[0])
	}
	if p.enqueueInteractionDebtBacklog(15, view, debt, now.Add(-2*time.Minute), now.Add(time.Second)) {
		t.Fatal("same interaction event must not enqueue twice")
	}
}
