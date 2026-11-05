package decisionexecutor

import (
	"context"
	"encoding/json"
	"io"
	"livecompanion/management/internal/model"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type computedDurationCore struct {
	*fakeCore
	retry, text string
	rate        float64
}

func (c *computedDurationCore) DoRoom(ctx context.Context, t, r int64, method, path string, q url.Values, body any) (*http.Response, error) {
	if strings.HasSuffix(path, "/preflight") {
		request := body.(map[string]any)
		c.text, _ = request["text"].(string)
		c.rate, _ = request["rate"].(float64)
		raw, _ := json.Marshal(map[string]any{"allowed": false, "reason": "interaction-scheduler: audio_duration_budget", "duration_ms": 32200, "remaining_ms": 12000, "retry_after_ms": 13750})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	}
	if strings.HasSuffix(path, "/release") {
		c.retry = q.Get("retry_after_ms")
	}
	return c.fakeCore.DoRoom(ctx, t, r, method, path, q, body)
}
func TestFinalSpeechAndRateUseCoreEstimatorAndComputedWait(t *testing.T) {
	core := &computedDurationCore{fakeCore: &fakeCore{}}
	w := New(readyVoiceStore(), core, &fakeAgent{}, &fakeTTS{})
	text := "小王、小李，" + strings.Repeat("字", 130)
	wait, err := w.playoutPreflight(context.Background(), model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11}, decisionItem{ID: "long", ExpiresAt: time.Now().Add(time.Minute)}, text, "tts", 1.4)
	if err != nil || !wait || core.text != text || core.rate != 1.4 || core.retry != "13750" {
		t.Fatal(wait, err, core)
	}
}
