package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type interactionReuseStore struct {
	*preparedTestStore
	enabled bool
	err     error
}

func (s *interactionReuseStore) GenerationCacheReuseEnabledForTenant(_ context.Context, tenant int64, key string) (bool, error) {
	if tenant != 7 || key != model.InteractionGenerationCacheReuseSetting {
		panic("wrong cache tenant or policy")
	}
	return s.enabled, s.err
}
func (s *interactionReuseStore) AnchorVoiceCacheRetentionHoursForTenant(context.Context, int64) (int, error) {
	return 5, nil
}

func TestWorkerDifferentEventKeysShareAudioWithoutAnotherPaidReceipt(t *testing.T) {
	var paid, before atomic.Int64
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, "RIFF0000WAVEfmt retained source")
			return
		}
		paid.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"audio": map[string]any{"url": srv.URL + "/audio"}}})
	}))
	defer srv.Close()
	w, store, _, _, _, session := preparedTestWorker()
	policy := &interactionReuseStore{preparedTestStore: store, enabled: true}
	w.store = policy
	g := ttsgateway.New(ttsgateway.ProviderQwen, "qwen-audio-3.1-tts-flash")
	g.Register(ttsgateway.NewQwenProvider(ttsgateway.QwenConfig{APIKey: "fixture", TTSBaseURL: srv.URL}))
	w.tts = g.WithInteractionAudioCache(ttsgateway.NewInteractionAudioCache(t.TempDir(), "worker"))
	w.SetPaidRequestAdapters(func(c context.Context, _ model.LiveRuntimeSession, _ string, _ int64, _ string) context.Context {
		return ttsgateway.WithSynthesisObserver(c, workerReuseObserver{&before})
	}, nil)
	for i := 0; i < 2; i++ {
		c := withWorkerPaidScope(t.Context(), session, 3, fmt.Sprint("event", i))
		out, e := w.synthesizePaidAudio(c, session, "interaction_tts", fmt.Sprint(i), ttsgateway.SynthesizeRequest{Text: "谢谢老乡们的关注，欢迎常来。", VoiceID: "goose"})
		if e != nil || out.AudioReused != (i == 1) {
			t.Fatal(out, e)
		}
	}
	if paid.Load() != 1 || before.Load() != 1 {
		t.Fatal("different events paid twice", paid.Load(), before.Load())
	}
	policy.err = errors.New("policy unavailable")
	c := withWorkerPaidScope(t.Context(), session, 3, "event3")
	_, e := w.synthesizePaidAudio(c, session, "interaction_tts", "3", ttsgateway.SynthesizeRequest{Text: "谢谢老乡们的关注，欢迎常来。", VoiceID: "goose"})
	if e == nil || paid.Load() != 1 {
		t.Fatal("failed policy ignored")
	}
}

type workerReuseObserver struct{ before *atomic.Int64 }

func (o workerReuseObserver) BeforeSynthesis(context.Context, ttsgateway.SynthesisAttempt) (string, error) {
	o.before.Add(1)
	return "ticket", nil
}
func (workerReuseObserver) AfterSynthesis(context.Context, ttsgateway.SynthesisReceipt) error {
	return nil
}

func TestRoomGuardRetainsLatePaidResponseOwnerAndStillRecovers(t *testing.T) {
	w, _, _, _, _, session := preparedTestWorker()
	now := time.Now()
	w.now = func() time.Time { return now }
	first, ok := w.beginRoom(session.RoomID)
	if !ok {
		t.Fatal("first worker rejected")
	}
	now = now.Add(100 * time.Second)
	if _, ok = w.beginRoom(session.RoomID); ok {
		t.Fatal("second worker entered while admitted TTS could still drain")
	}
	now = now.Add(90 * time.Second)
	second, ok := w.beginRoom(session.RoomID)
	if !ok {
		t.Fatal("stale worker never recovered")
	}
	w.endRoom(session.RoomID, first)
	if _, ok = w.beginRoom(session.RoomID); ok {
		t.Fatal("old completion cleared new worker")
	}
	w.endRoom(session.RoomID, second)
	if _, ok = w.beginRoom(session.RoomID); !ok {
		t.Fatal("actual owner did not release room")
	}
}
