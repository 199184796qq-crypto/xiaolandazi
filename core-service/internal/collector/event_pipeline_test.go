package collector

import (
	"context"
	"testing"
	"time"

	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/model"
)

func TestEventPipelineDoesNotBlockCaptureOnObserver(t *testing.T) {
	store := eventstore.NewStore(nil, 100)
	hub := eventstore.NewHub()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	store.SetObserver(func(model.RoomEvent) {
		entered <- struct{}{}
		<-release
	})
	pipeline := newEventPipeline(store, hub, false)
	room := model.Room{ID: 9, TenantID: 3}

	started := time.Now()
	_, accepted := pipeline.Enqueue(room, model.CreateEventInput{
		EventType: "chat",
		Content:   "多少钱",
	}, false, false)
	if !accepted {
		t.Fatal("event was not accepted")
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("capture enqueue blocked for %v", elapsed)
	}

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("observer was not invoked")
	}
	close(release)
	pipeline.Close()

	stats := pipeline.Stats()
	if stats.Delivered != 1 || stats.IngressDropped != 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestEventPipelineFiltersBeforeStoreAndObserver(t *testing.T) {
	store := eventstore.NewStore(nil, 100)
	hub := eventstore.NewHub()
	observed := make(chan model.RoomEvent, 2)
	store.SetObserver(func(event model.RoomEvent) {
		observed <- event
	})
	pipeline := newEventPipeline(store, hub, false)
	pipeline.SetFilter(func(event model.RoomEvent) bool {
		return event.UserID == "blocked-user"
	})
	room := model.Room{ID: 11, TenantID: 4}

	_, accepted := pipeline.Enqueue(room, model.CreateEventInput{
		EventType: "chat",
		UserID:    "blocked-user",
		Nickname:  "blocked",
		Content:   "这条不应进入业务链",
	}, false, false)
	if accepted {
		t.Fatal("blocked event was accepted")
	}
	select {
	case event := <-observed:
		t.Fatalf("blocked event reached observer: %#v", event)
	case <-time.After(30 * time.Millisecond):
	}

	_, accepted = pipeline.Enqueue(room, model.CreateEventInput{
		EventType: "chat",
		UserID:    "allowed-user",
		Nickname:  "allowed",
		Content:   "正常事件",
	}, false, false)
	if !accepted {
		t.Fatal("allowed event was rejected")
	}
	select {
	case event := <-observed:
		if event.UserID != "allowed-user" {
			t.Fatalf("unexpected observed event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("allowed event did not reach observer")
	}

	items, err := store.ListRecent(context.Background(), nil, room.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].UserID != "allowed-user" {
		t.Fatalf("blocked event leaked into store: %#v", items)
	}
	pipeline.Close()
}
