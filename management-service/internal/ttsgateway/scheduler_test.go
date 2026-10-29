package ttsgateway

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/ttsscheduler"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type schedulerFixture func(context.Context, ttsscheduler.Work) (ttsscheduler.Permit, error)

func (f schedulerFixture) Acquire(c context.Context, w ttsscheduler.Work) (ttsscheduler.Permit, error) {
	return f(c, w)
}
func TestTTSQueueCancellationDoesNotReserveFeeOrSend(t *testing.T) {
	var calls, before atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	g := New(ProviderQwen, "qwen-audio-3.1-tts-flash")
	g.Register(NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: srv.URL}))
	g.scheduler = schedulerFixture(func(ctx context.Context, _ ttsscheduler.Work) (ttsscheduler.Permit, error) {
		<-ctx.Done()
		return ttsscheduler.Permit{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	ctx = WithSynthesisObserver(ctx, receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { before.Add(1); return "ticket", nil }, after: func(context.Context, SynthesisReceipt) error { return nil }})
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "original", Text: "测试。"})
	if !errors.Is(err, context.DeadlineExceeded) || !DefinitelyNotSent(err) || calls.Load() != 0 || before.Load() != 0 {
		t.Fatalf("queued request sent or billed: %v %d %d", err, calls.Load(), before.Load())
	}
}
func TestTTSUsesSelectedAccountAndVoiceAfterAdmission(t *testing.T) {
	var calls atomic.Int64
	finished := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer account-b-fixture" {
			t.Error("did not use selected account")
		}
		var body struct {
			Parameters struct {
				Voice string `json:"voice"`
			} `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Parameters.Voice != "voice-b" {
			t.Error("voice mapping not used")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"audio":{"url":"https://audio.invalid/test.wav"}},"request_id":"fixture"}`))
	}))
	defer srv.Close()
	g := New(ProviderQwen, "qwen-audio-3.1-tts-flash")
	// A pool account must work even when the legacy environment key is absent.
	g.Register(NewQwenProvider(QwenConfig{APIKey: "", TTSBaseURL: srv.URL, Client: srv.Client()}))
	g.scheduler = schedulerFixture(func(ctx context.Context, w ttsscheduler.Work) (ttsscheduler.Permit, error) {
		if w.VoiceID != "original" || w.RoomID != 29 {
			t.Error("logical request scope lost")
		}
		// Queue time must not consume the provider's timeout.
		select {
		case <-time.After(35 * time.Millisecond):
		case <-ctx.Done():
			return ttsscheduler.Permit{}, ctx.Err()
		}
		return ttsscheduler.Permit{Credential: ttsscheduler.Credential{AccountID: "b", APIKey: "account-b-fixture", VoiceID: "voice-b"}, Finish: func(s string) { finished = s }}, nil
	})
	ctx := WithSynthesisTimeout(WithQueueScope(t.Context(), ttsscheduler.Scope{RoomID: 29}), 25*time.Millisecond)
	result, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "original", Text: "测试。"})
	if err != nil || result.AudioURL == "" || calls.Load() != 1 || finished != "completed" {
		t.Fatalf("routing failed: %v calls=%d state=%s", err, calls.Load(), finished)
	}
}
