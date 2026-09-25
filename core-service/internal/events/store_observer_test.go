package events

import (
	"testing"
	"time"

	"livecompanion/core/internal/model"
)

func TestObserveNotifiesWithoutPersistence(t *testing.T) {
	store := NewStore(nil, 100)
	var got model.RoomEvent
	store.SetObserver(func(event model.RoomEvent) { got = event })
	at := time.Date(2026, 9, 25, 1, 2, 3, 0, time.UTC)
	store.Observe(7, 44, model.CreateEventInput{
		EventType:  "room",
		Content:    "online",
		OccurredAt: at,
	})
	if got.TenantID != 7 || got.RoomID != 44 || got.EventType != "room" || !got.OccurredAt.Equal(at) {
		t.Fatalf("unexpected event: %#v", got)
	}
}
