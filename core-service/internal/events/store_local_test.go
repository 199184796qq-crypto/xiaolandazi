package events

import (
	"context"
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
