package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/ttsgateway"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type queuedInteractionTTS struct {
	started chan struct{}
	ready   chan struct{}
	once    sync.Once
}

func (q *queuedInteractionTTS) SynthesizeURL(ctx context.Context, _ ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	q.once.Do(func() { close(q.started) })
	select {
	case <-ctx.Done():
		return ttsgateway.SynthesizeResponse{}, ctx.Err()
	case <-q.ready:
		return ttsgateway.SynthesizeResponse{AudioURL: "http://audio.local/ready.wav"}, nil
	}
}

type queueGuardCore struct {
	*fakeCore
	stops atomic.Int64
}

func (c *queueGuardCore) DoRoom(ctx context.Context, tenant, room int64, method, path string, query url.Values, body any) (*http.Response, error) {
	if strings.Contains(path, "/stop") || strings.Contains(path, "/pause") {
		c.stops.Add(1)
	}
	return c.fakeCore.DoRoom(ctx, tenant, room, method, path, query, body)
}
func TestQueuedInteractionDoesNotInterruptMainlineUntilAudioReady(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "cancelled"}[cancelled], func(t *testing.T) {
			worker, _, core, _, _, session := preparedTestWorker()
			core.dispatchStatus = 0
			guarded := &queueGuardCore{fakeCore: core}
			worker.core = guarded
			queue := &queuedInteractionTTS{started: make(chan struct{}), ready: make(chan struct{})}
			worker.tts = queue
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- worker.processRoom(ctx, session) }()
			select {
			case <-queue.started:
			case <-time.After(2 * time.Second):
				t.Fatal("worker did not reach TTS queue")
			}
			if core.dispatches != 0 || guarded.stops.Load() != 0 {
				t.Fatal("mainline interrupted before interaction audio was ready")
			}
			if cancelled {
				cancel()
			} else {
				close(queue.ready)
			}
			select {
			case err := <-done:
				if cancelled {
					if core.dispatches != 0 {
						t.Fatal("cancelled interaction dispatched")
					}
				} else if err != nil || core.dispatches != 1 {
					t.Fatalf("ready interaction not dispatched: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("interaction did not finish")
			}
			if guarded.stops.Load() != 0 {
				t.Fatal("TTS queue stopped mainline")
			}
		})
	}
}
