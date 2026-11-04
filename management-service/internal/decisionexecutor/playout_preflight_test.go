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

type playoutGateCore struct {
	*fakeCore
	blockStage string
	malformed  bool
	checks     int
	reason     string
}

func (c *playoutGateCore) DoRoom(ctx context.Context, t, r int64, method, path string, q url.Values, body any) (*http.Response, error) {
	if strings.HasSuffix(path, "/preflight") {
		c.checks++
		request, ok := body.(map[string]any)
		if !ok {
			panic("preflight must be JSON object, not encoded bytes")
		}
		allowed := request["stage"] != c.blockStage
		raw, _ := json.Marshal(map[string]any{"allowed": allowed, "reason": "no_pcm_cut"})
		if c.malformed {
			raw = []byte(`{}`)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	}
	if strings.HasSuffix(path, "/release") && q.Get("reason") == "preflight" {
		c.reason = q.Get("block_reason")
	}
	return c.fakeCore.DoRoom(ctx, t, r, method, path, q, body)
}
func TestNoPlayoutWindowPreventsModelAndTTSAndYieldsWithoutRoomBackoff(t *testing.T) {
	for _, stage := range []string{"model", "tts"} {
		t.Run(stage, func(t *testing.T) {
			core := &playoutGateCore{fakeCore: &fakeCore{}, blockStage: stage}
			agent := &fakeAgent{}
			tts := &fakeTTS{}
			w := New(readyVoiceStore(), core, agent, tts)
			s := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
			if err := w.processRoom(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			want := 0
			if stage == "tts" {
				want = 1
			}
			if agent.calls != want || tts.calls != 0 || core.dispatches != 0 || core.reason != "no_pcm_cut" {
				t.Fatal("blocked paid stage was called", agent.calls, tts.calls, core.dispatches, core.reason)
			}
		})
	}
}
func TestMissingPreflightContractCannotAuthorizePaidWork(t *testing.T) {
	core := &playoutGateCore{fakeCore: &fakeCore{}, malformed: true}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	w := New(readyVoiceStore(), core, agent, tts)
	if err := w.processRoom(context.Background(), model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}); err == nil {
		t.Fatal("malformed response allowed paid work")
	}
	if agent.calls != 0 || tts.calls != 0 {
		t.Fatal("malformed preflight consumed provider")
	}
}

func TestPreflightDraftDoesNotRenewDeadlineOrCrossContentIdentity(t *testing.T) {
	now := time.Now()
	w := New(readyVoiceStore(), &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	w.now = func() time.Time { return now }
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11}
	item := decisionItem{ID: "one", ExpiresAt: now.Add(20 * time.Second)}
	for _, scenario := range []string{"reuse", "expired", "facts", "memory", "session"} {
		t.Run(scenario, func(t *testing.T) {
			now = time.Now()
			item.ExpiresAt = now.Add(20 * time.Second)
			w.savePreflightDraft(session, item, "原正文", "facts", "memory")
			scope, mem := "facts", "memory"
			sess := session
			switch scenario {
			case "expired":
				now = now.Add(20 * time.Second)
			case "facts":
				scope = "new"
			case "memory":
				mem = "new"
			case "session":
				sess.ID++
			}
			got := item
			text, ok := w.preflightDraft(sess, &got, scope, mem)
			if ok != (scenario == "reuse") {
				t.Fatal("draft identity/deadline guard", ok)
			}
			if ok && (text != "原正文" || !got.ExpiresAt.Equal(item.ExpiresAt)) {
				t.Fatal("draft renewed or changed")
			}
		})
	}
}

func TestRepeatedTTSPreflightDenialReusesPaidBody(t *testing.T) {
	until := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	core := &playoutGateCore{fakeCore: &fakeCore{claimRaw: `{"claimed":true,"item":{"id":"d-draft","topic":"FAMILY:发货物流","sample_questions":["什么时候发货"],"manual_action":"answer","expires_at":"` + until + `"}}`}, blockStage: "tts"}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	w := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	for i := 0; i < 3; i++ {
		if err := w.processRoom(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	if agent.calls != 1 || tts.calls != 0 || core.dispatches != 0 {
		t.Fatal("re-bought body during TTS wait", agent.calls, tts.calls, core.dispatches)
	}
}
