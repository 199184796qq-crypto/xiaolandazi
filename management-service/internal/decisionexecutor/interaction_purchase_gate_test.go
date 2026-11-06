package decisionexecutor

import (
	"context"
	"errors"
	"io"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type warmupPurchaseCore struct {
	*fakeCore
	status int
	raw    string
	calls  int
}

func (c *warmupPurchaseCore) DoRoom(ctx context.Context, t, r int64, method, path string, q url.Values, body any) (*http.Response, error) {
	if strings.HasSuffix(path, "/preflight") {
		c.calls++
		return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.raw))}, nil
	}
	return c.fakeCore.DoRoom(ctx, t, r, method, path, q, body)
}

type warmupPurchaseProvider struct{ calls int }

func (*warmupPurchaseProvider) Name() string { return "warmup-fixture" }
func (p *warmupPurchaseProvider) SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	p.calls++
	return ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid/warmup.wav"}, nil
}
func (*warmupPurchaseProvider) CloneVoice(context.Context, ttsgateway.CloneRequest) (ttsgateway.CloneResponse, error) {
	return ttsgateway.CloneResponse{}, nil
}

func TestWarmupActualTTSAttemptRechecksTaskAndNeverBuysAfterCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		raw    string
		want   bool
	}{
		{"valid", 200, `{"allowed":true}`, true},
		{"removed", 409, `{"error":"decision_changed"}`, false},
		{"other_activity", 200, `{"allowed":false,"reason":"warmup_activity_changed"}`, false},
		{"lost_window", 200, `{"allowed":false,"reason":"no_pcm_cut"}`, false},
		{"missing_contract", 200, `{}`, false},
		{"unavailable", 503, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &warmupPurchaseCore{fakeCore: &fakeCore{}, status: tc.status, raw: tc.raw}
			p := &warmupPurchaseProvider{}
			g := ttsgateway.New(p.Name(), "model")
			g.Register(p)
			w := New(readyVoiceStore(), c, &fakeAgent{}, g)
			s := model.LiveRuntimeSession{ID: 652, TenantID: 14, RoomID: 33}
			ctx := withWorkerPaidScope(t.Context(), s, 4, "warmup")
			item := decisionItem{ID: "warmup", MissionKind: "warmup", ExpiresAt: time.Now().Add(time.Minute)}
			for _, stage := range []string{"interaction_tts", "interaction_fact_refresh_tts"} {
				_, err := w.synthesizeInteractionAudioStage(ctx, &item, stage, ttsgateway.SynthesizeRequest{Provider: p.Name(), Model: "model", VoiceID: "voice", Text: "继续看看商品。", Rate: 1})
				if (err == nil) != tc.want {
					t.Fatal(err)
				}
				if !tc.want && (!errors.Is(err, errWarmupPurchaseNotAllowed) || !ttsgateway.DefinitelyNotSent(err)) {
					t.Fatal("missing definitely-unsent classification", err)
				}
			}
			want := 0
			if tc.want {
				want = 2
			}
			if p.calls != want || c.calls != 2 {
				t.Fatal("unexpected synthesis/preflight attempts", p.calls, c.calls)
			}
		})
	}
}
