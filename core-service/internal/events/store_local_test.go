package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"livecompanion/core/internal/model"
)

func TestAcceptKeepsRecentEventsWithoutRedis(t *testing.T) {
	store := NewStore(nil, 10)
	first := store.BuildEvent(7, 44, model.CreateEventInput{EventType: "chat", Content: "first"})
	second := store.BuildEvent(7, 44, model.CreateEventInput{EventType: "chat", Content: "second"})
	store.Accept(first)
	store.Accept(second)

	items, err := store.ListRecent(context.Background(), nil, 44, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Content != "second" || items[1].Content != "first" {
		t.Fatalf("unexpected recent events: %#v", items)
	}
	if second.ID <= first.ID {
		t.Fatalf("event ids are not ordered: first=%d second=%d", first.ID, second.ID)
	}
	if second.ID > 9_007_199_254_740_991 {
		t.Fatalf("event id exceeds JavaScript safe integer: %d", second.ID)
	}
}

func TestBuildEventUsesProvidedOccurrenceTime(t *testing.T) {
	store := NewStore(nil, 10)
	at := time.Date(2026, 9, 25, 1, 2, 3, 0, time.UTC)
	event := store.BuildEvent(1, 2, model.CreateEventInput{EventType: "chat", OccurredAt: at})
	if !event.OccurredAt.Equal(at) {
		t.Fatalf("occurred_at=%v want=%v", event.OccurredAt, at)
	}
}

func TestSessionLiveSecondsAreCoreDerivedAndFreezeAtEnd(t *testing.T) {
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	ended := start.Add(2 * time.Hour)
	stats := SessionStats{StartedAt: start, EndedAt: &ended}
	got := stats.withLiveSecondsAt(start.Add(8 * time.Hour))
	if got.LiveSeconds != 7200 {
		t.Fatalf("live_seconds=%d want=7200", got.LiveSeconds)
	}
}

func TestSessionLiveSecondsExcludeMergedInterruption(t *testing.T) {
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	stats := SessionStats{StartedAt: start, InterruptedSeconds: 20 * 60}
	got := stats.withLiveSecondsAt(start.Add(3 * time.Hour))
	if got.LiveSeconds != 9600 {
		t.Fatalf("live_seconds=%d want=9600", got.LiveSeconds)
	}
}

func TestSessionLiveSecondsStayFrozenWhileResumeDecisionPending(t *testing.T) {
	start := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	reopened := start.Add(90 * time.Minute)
	stats := SessionStats{StartedAt: start, ResumePending: true, ReopenedAt: &reopened}
	got := stats.withLiveSecondsAt(start.Add(4 * time.Hour))
	if got.LiveSeconds != 5400 {
		t.Fatalf("live_seconds=%d want=5400", got.LiveSeconds)
	}
}

func TestImportantHistoryAndSessionStatsAreIndependentFromRecentWindow(t *testing.T) {
	store := NewStoreWithImportantLimit(nil, 2, 20)
	ctx := context.Background()
	roomID := int64(55)
	at := time.Date(2026, 9, 25, 1, 2, 3, 0, time.UTC)
	accept := func(eventType, content string, payload map[string]any) {
		raw, _ := json.Marshal(payload)
		event := store.BuildEvent(1, roomID, model.CreateEventInput{EventType: eventType, Content: content, Payload: raw, OccurredAt: at})
		store.Accept(event)
	}
	accept("session_start", "", nil)
	accept("member", "进房", nil)
	accept("chat", "在哪个地方", nil)
	accept("like", "点赞 × 4", map[string]any{"count": 4})
	accept("follow", "关注", nil)
	accept("gift", "礼物", nil)
	accept("order_signal", "已拍", map[string]any{"verified_order": false})
	accept("member", "进房", nil)
	accept("member", "进房", nil)

	chats, err := store.ListImportant(ctx, nil, roomID, "chat", 0, 20)
	if err != nil || len(chats) != 1 || chats[0].Content != "在哪个地方" {
		t.Fatalf("important chat history lost behind member flood: items=%#v err=%v", chats, err)
	}
	stats, err := store.GetSessionStats(ctx, roomID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.EventCount != 7 || stats.Entries != 3 || stats.Chats != 1 || stats.Likes != 4 || stats.Follows != 1 || stats.Gifts != 1 || stats.OrderSignals != 1 {
		t.Fatalf("unexpected session stats: %#v", stats)
	}
}
