package httpapi

import (
	"context"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/events"
	"livecompanion/core/internal/model"
)

func TestManualCandidateTTSEligibleAcceptsRecentDirectDanmaku(t *testing.T) {
	store := events.NewStore(nil, 5000)
	event := store.BuildEvent(7, 11, model.CreateEventInput{
		EventType:  "chat",
		UserID:     "u-1",
		Nickname:   "viewer",
		Content:    "这个什么时候发货",
		OccurredAt: time.Now().UTC().Add(-2 * time.Minute),
	})
	store.Accept(event)
	server := &Server{events: store}

	if !server.manualCandidateTTSEligible(context.Background(), 7, 11, agentdecision.Candidate{
		EventID:  event.ID,
		Question: event.Content,
	}) {
		t.Fatal("recent direct danmaku should be TTS eligible even when it is not in a question bucket")
	}
}

func TestManualCandidateTTSEligibleAcceptsOldRetainedDirectDanmaku(t *testing.T) {
	store := events.NewStore(nil, 5000)
	event := store.BuildEvent(7, 11, model.CreateEventInput{
		EventType:  "comment",
		UserID:     "u-1",
		Content:    "太久以前的弹幕",
		OccurredAt: time.Now().UTC().Add(-31 * time.Minute),
	})
	store.Accept(event)
	server := &Server{events: store}

	if !server.manualCandidateTTSEligible(context.Background(), 7, 11, agentdecision.Candidate{
		EventID:  event.ID,
		Question: event.Content,
	}) {
		t.Fatal("retained direct danmaku must remain manually answerable without a 30-minute cutoff")
	}
}

func TestManualCandidateTTSEligibleAcceptsChatEvictedFromRecentWindow(t *testing.T) {
	store := events.NewStoreWithImportantLimit(nil, 2, 20)
	chat := store.BuildEvent(7, 11, model.CreateEventInput{
		EventType:  "chat",
		UserID:     "u-1",
		Content:    "哪年的菜籽",
		OccurredAt: time.Now().UTC(),
	})
	store.Accept(chat)
	for index := 0; index < 3; index++ {
		store.Accept(store.BuildEvent(7, 11, model.CreateEventInput{
			EventType:  "member",
			UserID:     "u-member",
			Content:    "进入直播间",
			OccurredAt: time.Now().UTC(),
		}))
	}
	server := &Server{events: store}

	if !server.manualCandidateTTSEligible(context.Background(), 7, 11, agentdecision.Candidate{
		EventID:  chat.ID,
		Question: chat.Content,
	}) {
		t.Fatal("chat retained in important history must remain manually answerable after leaving recent window")
	}
}

func TestManualCandidateTTSEligibleRejectsNonChatEvent(t *testing.T) {
	store := events.NewStore(nil, 5000)
	event := store.BuildEvent(7, 11, model.CreateEventInput{
		EventType:  "like",
		UserID:     "u-1",
		Content:    "like",
		OccurredAt: time.Now().UTC(),
	})
	store.Accept(event)
	server := &Server{events: store}

	if server.manualCandidateTTSEligible(context.Background(), 7, 11, agentdecision.Candidate{
		EventID:  event.ID,
		Question: event.Content,
	}) {
		t.Fatal("non-chat event must not use direct danmaku manual answer path")
	}
}
