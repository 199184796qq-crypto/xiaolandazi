package douyin

import (
	"context"
	"errors"
	"testing"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

type testBrowserRuntime struct {
	session *BrowserSession
}

func (r *testBrowserRuntime) StartRoom(
	context.Context,
	model.Room,
) (*BrowserSession, error) {
	return r.session, nil
}

func (r *testBrowserRuntime) RequestPreview(
	context.Context,
	int64,
) ([]byte, string, error) {
	return nil, "", errors.New("not implemented")
}

func (r *testBrowserRuntime) Stream(
	int64,
) (collector.StreamSource, error) {
	return collector.StreamSource{}, errors.New("not implemented")
}

func TestBrowserCollectorTransportNeedsLiveEvidence(t *testing.T) {
	room := model.Room{ID: 10, TenantID: 14}
	roomState := &roomSession{
		frames:    make(chan []byte, 4),
		errors:    make(chan error, 1),
		transport: make(chan struct{}),
		states:    make(chan roomStateSignal, 4),
	}
	manager := NewBrowserManager("", true)
	manager.sessions[room.ID] = roomState

	runtime := &testBrowserRuntime{
		session: &BrowserSession{
			manager: manager,
			roomID:  room.ID,
			session: roomState,
		},
	}
	runner := &BrowserCollector{
		browser:      runtime,
		frameTimeout: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	liveCalled := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(
			ctx,
			room,
			func(context.Context) error {
				liveCalled <- struct{}{}
				return nil
			},
			func(context.Context, model.CreateEventInput) error {
				return nil
			},
		)
	}()

	roomState.markTransport()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-liveCalled:
		t.Fatal("transport alone must not mark a room live")
	default:
	}

	roomState.sendState("live", "stream_flv")
	select {
	case <-liveCalled:
	case <-time.After(time.Second):
		t.Fatal("collector did not mark room live after live-state evidence")
	}

	// Once actual live evidence is established, the old startup timeout must
	// no longer flap a quiet but genuinely live room offline.
	time.Sleep(140 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("collector exited after live evidence was established: %v", err)
	default:
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("collector exit error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector did not stop after cancellation")
	}
}

func TestBrowserCollectorOfflineStateReturnsOffline(t *testing.T) {
	room := model.Room{ID: 11, TenantID: 14}
	roomState := &roomSession{
		frames:    make(chan []byte, 2),
		errors:    make(chan error, 1),
		transport: make(chan struct{}),
		states:    make(chan roomStateSignal, 2),
	}
	manager := NewBrowserManager("", true)
	manager.sessions[room.ID] = roomState
	runner := &BrowserCollector{
		browser: &testBrowserRuntime{session: &BrowserSession{
			manager: manager,
			roomID:  room.ID,
			session: roomState,
		}},
		frameTimeout: time.Second,
	}

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(
			context.Background(),
			room,
			func(context.Context) error { return nil },
			func(context.Context, model.CreateEventInput) error { return nil },
		)
	}()

	roomState.markTransport()
	roomState.sendState("offline", "page_live_ended")

	select {
	case err := <-done:
		if !errors.Is(err, collector.ErrOffline) {
			t.Fatalf("collector error = %v, want ErrOffline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector did not stop after offline state")
	}
}

func TestBrowserManagerStopRoomClearsStreamCandidate(t *testing.T) {
	manager := NewBrowserManager("", true)
	const roomID int64 = 22

	manager.sessions[roomID] = &roomSession{}
	manager.setStreamCandidate(roomID, collector.StreamSource{
		Protocol: "hls",
		URL:      "https://example.test/live.m3u8",
	})

	if _, err := manager.Stream(roomID); err != nil {
		t.Fatalf("stream before stop: %v", err)
	}

	manager.StopRoom(roomID)

	if _, err := manager.Stream(roomID); err == nil {
		t.Fatal("stopped room must not keep a stale stream candidate")
	}
}
