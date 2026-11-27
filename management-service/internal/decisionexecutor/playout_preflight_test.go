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
	blockStage  string
	malformed   bool
	checks      int
	reason      string
	answerReady bool
	releaseItem *decisionItem
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
		c.answerReady = q.Get("answer_ready") == "true"
	}
	if strings.HasSuffix(path, "/release") && c.releaseItem != nil {
		resp, err := c.fakeCore.DoRoom(ctx, t, r, method, path, q, body)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
		raw, _ := json.Marshal(map[string]any{"ok": true, "item": c.releaseItem})
		resp.Body = io.NopCloser(strings.NewReader(string(raw)))
		return resp, nil
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

func TestDeferredUnpreparedQuickReplyChecksTimingBeforeFirstModelCall(t *testing.T) {
	w, _, base, agent, tts, session, envelope := v2Fixture(t, 10423)
	setManualRecoveryAnswerFlow(w, envelope)
	agent.responses = []string{envelope.Body}
	now := time.Now()
	item := decisionItem{ID: "deferred-quick", Topic: "FAMILY:发货物流", ManualAction: "quick", MissionKind: "reply_question",
		SampleQuestions: []string{"什么时候发货"}, ExpiresAt: now.Add(time.Minute), AnswerDeferredUntil: now.Add(30 * time.Second)}
	raw, _ := json.Marshal(map[string]any{"claimed": true, "item": item})
	base.claimRaw = string(raw)
	core := &playoutGateCore{fakeCore: base, blockStage: "model"}
	w.core = core
	w.now = func() time.Time { return now }
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 0 || tts.calls != 0 || core.checks != 1 || core.answerReady {
		t.Fatal("waiting task started paid work", agent.calls, tts.calls, core.checks)
	}
	core.blockStage = ""
	now = now.Add(5 * time.Second)
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 1 || core.dispatches == 0 {
		t.Fatal("unprepared task was not allowed to produce its first answer", agent.calls, tts.calls, core.dispatches)
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
			item.AnswerDeferredUntil = item.ExpiresAt
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

func TestDeferredV2AnswerReusesTextThenAudioAcrossCutRetries(t *testing.T) {
	w, _, base, agent, tts, session, envelope := v2Fixture(t, 10422)
	setManualRecoveryAnswerFlow(w, envelope)
	agent.responses = []string{envelope.Body}
	core := &playoutGateCore{fakeCore: base, blockStage: "tts"}
	w.core = core
	now := time.Now()
	w.now = func() time.Time { return now }
	item := decisionItem{ID: "deferred-manual", Topic: "manual", MissionKind: "reply_question", ManualAction: "quick", SampleQuestions: []string{"哪天发货"}, ExpiresAt: now.Add(time.Second)}
	deferred := item
	deferred.AnswerDeferredUntil, deferred.AnswerDraftExpected = now.Add(30*time.Second), true
	core.releaseItem = &deferred
	claim := func() {
		raw, _ := json.Marshal(map[string]any{"claimed": true, "item": item})
		base.claimRaw = string(raw)
		if err := w.processRoom(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	claim()
	if agent.calls != 1 || tts.calls != 0 || !core.answerReady {
		t.Fatalf("first defer: model=%d tts=%d ready=%v", agent.calls, tts.calls, core.answerReady)
	}
	if expires := w.preflightDrafts[preparationKey(session, item)].ExpiresAt; !expires.Equal(deferred.AnswerDeferredUntil) {
		t.Fatalf("local draft did not adopt Core's deadline: %v", expires)
	}
	item = deferred
	now = now.Add(5 * time.Second)
	claim()
	core.blockStage = ""
	base.dispatchStatus, base.dispatchRaw = http.StatusConflict, `{"error":"没有真实PCM声学切点"}`
	now = now.Add(5 * time.Second)
	claim()
	if agent.calls != 1 || tts.calls != 1 || tts.last.Text == "" || base.releases != 3 {
		t.Fatalf("cut defer: model=%d tts=%d releases=%d", agent.calls, tts.calls, base.releases)
	}
	item.AudioPrepared, item.PreparedUntil = true, item.AnswerDeferredUntil
	base.dispatchStatus = 0
	now = now.Add(5 * time.Second)
	claim()
	if agent.calls != 1 || tts.calls != 1 || base.dispatches != 4 || !strings.Contains(base.dispatch["reply_text"].(string), envelope.Body) || base.dispatch["audio_url"] == "" {
		t.Fatalf("did not play previously prepared audio: model=%d tts=%d payload=%+v", agent.calls, tts.calls, base.dispatch)
	}
}

func TestDeferredAnswerPastDeadlineCannotReuseAudio(t *testing.T) {
	item := decisionItem{ID: "expired", AnswerDeferredUntil: time.Now().Add(-time.Second), AnswerDraftExpected: true, AudioPrepared: true}
	raw, _ := json.Marshal(map[string]any{"claimed": true, "item": item})
	core := &fakeCore{claimRaw: string(raw)}
	agent, tts := &fakeAgent{}, &fakeTTS{}
	w := New(readyVoiceStore(), core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	w.savePreparedInteraction(session, item, "旧答案", "https://audio.invalid/old.wav", "", "", "", time.Now().Add(time.Minute))
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 0 || tts.calls != 0 || core.dispatches != 0 || len(core.releaseReasons) != 1 {
		t.Fatal("expired preparation did not stop at the deadline", agent.calls, tts.calls, core.dispatches, core.releaseReasons)
	}
}

func TestDeferredAnswerWithMissingCacheIsDiscardedWithoutModelOrTTS(t *testing.T) {
	until := time.Now().Add(20 * time.Second).UTC().Format(time.RFC3339Nano)
	for _, prepared := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{
			"id": "lost", "topic": "FAMILY:发货物流", "sample_questions": []string{"什么时候发货"},
			"answer_draft_expected": true, "answer_deferred_until": until, "audio_prepared": prepared,
		}})
		core := &fakeCore{claimRaw: string(raw)}
		agent, tts := &fakeAgent{}, &fakeTTS{}
		w := New(readyVoiceStore(), core, agent, tts)
		session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
		if err := w.processRoom(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		if agent.calls != 0 || tts.calls != 0 || core.dispatches != 0 || len(core.releaseReasons) != 1 || core.releaseReasons[0] != "deferred_answer_unavailable" {
			t.Fatalf("lost preparation was not discarded: model=%d tts=%d releases=%v", agent.calls, tts.calls, core.releaseReasons)
		}
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
