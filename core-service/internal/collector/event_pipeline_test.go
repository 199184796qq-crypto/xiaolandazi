package collector

import (
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
