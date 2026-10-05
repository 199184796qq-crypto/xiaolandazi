package httpapi

import (
	"context"
	"errors"
	"testing"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	eventstore "livecompanion/core/internal/events"
)

type failingDeletionAudioClient struct{ fakeAudioTaskClient }

func (f *failingDeletionAudioClient) StopTestProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{}, errors.New("audio stop unavailable")
}

func TestRoomCleanupFailureIsRetryableAndStillReleasesLocalResources(t *testing.T) {
	hub := eventstore.NewHub()
	events, cancel := hub.Subscribe(15)
	defer cancel()
	s := New(nil, eventstore.NewStore(nil, 100), hub, nil, nil, "test", "")
	s.SetAudioClient(&failingDeletionAudioClient{}, "")
	defer audioDevStates.Delete(s)
	if _, err := s.audioHub.RegisterReceiver(audiohub.Receiver{ReceiverID: "test", RoomID: 15}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.roomAudio.StartMainline(15, "s"); err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupDeletedRoomRuntime(context.Background(), 15); err == nil {
		t.Fatal("cleanup failure must propagate to the durable retry worker")
	}
	if _, ok := <-events; ok {
		t.Fatal("event subscriber leaked after partial failure")
	}
	if s.roomAudio.Metrics().Rooms != 0 || len(s.audioHub.RoomReceivers(15)) != 0 {
		t.Fatal("local cleanup stopped after first error")
	}
	s.SetAudioClient(&fakeAudioTaskClient{}, "")
	if err := s.cleanupDeletedRoomRuntime(context.Background(), 15); err != nil {
		t.Fatalf("idempotent cleanup retry: %v", err)
	}
}
