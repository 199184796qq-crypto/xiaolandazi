package decisionexecutor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type controlTestStore struct {
	*preparedTestStore
	mu       sync.Mutex
	controls model.RoomInteractionControls
	err      error
}

func (s *controlTestStore) GetRoomInteractionControls(context.Context, int64, int64) (model.RoomInteractionControls, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controls, s.err
}
func (s *controlTestStore) setControls(c model.RoomInteractionControls) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.controls = c
}

func TestControlCloseBeforePaidSubmissionAndRapidReopen(t *testing.T) {
	w, store, _, _, tts, session := preparedTestWorker()
	source := &controlTestStore{preparedTestStore: store}
	w.store = source
	ctx, stop, err := w.watchInteractionControls(context.Background(), session, decisionItem{MissionKind: "welcome"})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	source.setControls(model.RoomInteractionControls{DisabledAt: map[string]uint64{"welcome": 2}, Revision: 3}) // off and on between polls
	_, err = w.synthesizePaidAudio(ctx, session, "tts", "x", ttsgateway.SynthesizeRequest{Text: "不应购买"})
	if !errors.Is(err, errInteractionDisabled) || tts.calls != 0 {
		t.Fatal("late work bought audio after re-enable", err, tts.calls)
	}
}
func TestControlsCancelQueuedTTSWithoutStoppingMainline(t *testing.T) {
	w, store, core, _, _, session := preparedTestWorker()
	source := &controlTestStore{preparedTestStore: store}
	w.store = source
	guardedCore := &queueGuardCore{fakeCore: core}
	w.core = guardedCore
	queue := &queuedInteractionTTS{started: make(chan struct{}), ready: make(chan struct{})}
	w.tts = queue
	core.claimRaw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","mission_kind":"reply_question","sample_questions":["什么时候发货"]}}`
	done := make(chan error, 1)
	go func() { done <- w.processRoom(t.Context(), session) }()
	select {
	case <-queue.started:
	case <-time.After(3 * time.Second):
		t.Fatal("not queued")
	}
	source.setControls(model.RoomInteractionControls{Switches: map[string]bool{"semantic": false}, DisabledAt: map[string]uint64{"semantic": 1}, Revision: 1})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("queued TTS not cancelled")
	}
	if core.dispatches != 0 || guardedCore.stops.Load() != 0 || len(w.prepared) != 0 {
		t.Fatal("disabled audio dispatched or mainline stopped")
	}
}
func TestControlsReadFailureBlocksPaidAndManualRemainsIndependent(t *testing.T) {
	w, store, _, _, _, session := preparedTestWorker()
	w.store = &controlTestStore{preparedTestStore: store, err: errors.New("db unavailable")}
	if _, _, err := w.watchInteractionControls(t.Context(), session, decisionItem{MissionKind: "like"}); err == nil {
		t.Fatal("unconfirmed settings allowed paid work")
	}
	if _, stop, err := w.watchInteractionControls(t.Context(), session, decisionItem{Sources: []string{"manual"}}); err != nil {
		t.Fatal("manual speech blocked", err)
	} else {
		stop()
	}
}
