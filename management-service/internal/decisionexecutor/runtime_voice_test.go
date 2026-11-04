package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicebatch"
)

type liveHostTestStore struct {
	*fakeStore
	identity model.LiveAgentVoiceIdentity
	err      error
	scope    [3]int64
}

func (s *liveHostTestStore) RuntimeVoiceIdentity(_ context.Context, tenant, room, version int64) (model.LiveAgentVoiceIdentity, error) {
	s.scope = [3]int64{tenant, room, version}
	return s.identity, s.err
}

func TestInteractionUsesOnlineHostAndDefersPendingSwitch(t *testing.T) {
	s := &liveHostTestStore{fakeStore: readyVoiceStore(), identity: model.LiveAgentVoiceIdentity{Name: "B", VoiceID: "online-B", Model: "qwen-audio-3.1-tts-flash", Provider: "aliyun_qwen", Rate: 1.13}}
	w := New(s, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	voice, ready, err := w.readyVoice(t.Context(), 7, 11)
	if err != nil || !ready || voice.VoiceID != "online-B" || s.scope != [3]int64{7, 11, s.publishedVersion.ID} {
		t.Fatal(voice, ready, err, s.scope)
	}
	if s.publishedVersion.VoiceIdentity.VoiceID == "online-B" {
		t.Fatal("switch changed the saved plan")
	}
	enabled := true
	s.identity.EmotionEnabled = &enabled
	if emotion, err := w.emotionEnabledForRoom(t.Context(), 7, 11); err != nil || !emotion {
		t.Fatal("delivery settings did not follow the online host", emotion, err)
	}
	s.err = voicebatch.ErrSwitching
	if _, ready, err := w.readyVoice(t.Context(), 7, 11); ready || !errors.Is(err, voicebatch.ErrSwitching) {
		t.Fatal(ready, err)
	}
	s.err = errors.New("database unavailable")
	if _, ready, err := w.readyVoice(t.Context(), 7, 11); ready || err == nil {
		t.Fatal("fell back to saved voice after a failed runtime read")
	}
}

func TestInteractionPreparedScopeTracksOnlineVoiceOnBothPaths(t *testing.T) {
	s := &liveHostTestStore{fakeStore: readyVoiceStore(), identity: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "qwen-audio-3.1-tts-flash", Rate: 1}}
	w := New(s, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11}
	t.Cleanup(func() { interactionplan.Frozen = interactionplan.Cache{} })
	interactionplan.Frozen = interactionplan.Cache{}
	interactionplan.Frozen.Put(interactionplan.Snapshot{Scope: interactionplan.Scope{Tenant: 7, Room: 11, Session: 9, Plan: 3, Version: s.publishedVersion.ID}, Fingerprint: "voice-test", Voice: model.VoiceProfile{VoiceID: "A", Config: map[string]any{}}})
	envelope, _ := json.Marshal(interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: "answer", Tenant: 7, Room: 11, Session: 9, Plan: 3, Fingerprint: "voice-test"})
	for _, raw := range []json.RawMessage{nil, envelope} {
		item := decisionItem{ID: "same-question", PurposeKnowledge: raw, SampleQuestions: []string{"什么时候发货"}}
		s.identity.VoiceID = "A"
		a, err := w.interactionContentScope(t.Context(), session, item)
		if err != nil {
			t.Fatal(err)
		}
		s.identity.VoiceID = "B"
		b, err := w.interactionContentScope(t.Context(), session, item)
		if err != nil || a == b {
			t.Fatal("prepared A audio survived switching to B", err)
		}
		s.err = voicebatch.ErrSwitching
		if _, err := w.interactionContentScope(t.Context(), session, item); err == nil {
			t.Fatal("pending switch reused old prepared audio")
		}
		s.err = nil
	}
}

type v2LiveHostStore struct {
	*v2Store
	identity model.LiveAgentVoiceIdentity
	err      error
	version  int64
}

func (s *v2LiveHostStore) RuntimeVoiceIdentity(_ context.Context, _, _, version int64) (model.LiveAgentVoiceIdentity, error) {
	s.version = version
	return s.identity, s.err
}

// Exercise the real V2 worker rather than only readyVoice: the V2 TTS branch
// previously bypassed readyVoice and selected the frozen opening host.
func TestV2SynthesisFollowsAudibleHost(t *testing.T) {
	w, store, core, agent, tts, session, e := v2Fixture(t, 10101)
	live := &v2LiveHostStore{v2Store: store, identity: model.LiveAgentVoiceIdentity{VoiceID: "audible-B", Provider: "aliyun_qwen", Model: "qwen-audio-3.1-tts-flash", Rate: 1.17}}
	w.store = live
	claimV2(core, e)
	if err := w.processRoom(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 1 || tts.last.VoiceID != "audible-B" || tts.last.Model != live.identity.Model || tts.last.Rate != 1.17 {
		t.Fatalf("V2 used the frozen host: calls=%d request=%+v", tts.calls, tts.last)
	}
	if agent.calls != 0 || core.dispatches != 1 {
		t.Fatal(agent.calls, core.dispatches)
	}
	frozen, _ := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	if frozen.Voice.VoiceID != "voice-1" || frozen.Fingerprint != e.Fingerprint || live.version != frozen.Scope.Version {
		t.Fatal("content snapshot or version changed", frozen, live.version)
	}
}

func TestV2HostSwitchDefersWithoutPaidFallback(t *testing.T) {
	for _, readErr := range []error{voicebatch.ErrSwitching, errors.New("runtime voice unavailable")} {
		w, store, core, _, tts, session, e := v2Fixture(t, 10102)
		w.store = &v2LiveHostStore{v2Store: store, err: readErr}
		claimV2(core, e)
		if err := w.processRoom(t.Context(), session); !errors.Is(err, readErr) {
			t.Fatalf("lost runtime error: %v", err)
		}
		if tts.calls != 0 || core.dispatches != 0 {
			t.Fatal("old voice fallback was sent", tts.calls, core.dispatches)
		}
	}
}

func TestV2PreparedAudioReusedOnlyWhileHostUnchanged(t *testing.T) {
	for _, changed := range []bool{false, true} {
		w, store, core, agent, tts, session, e := v2Fixture(t, 10103)
		live := &v2LiveHostStore{v2Store: store, identity: model.LiveAgentVoiceIdentity{VoiceID: "A", Provider: "aliyun_qwen", Model: "qwen-audio-3.1-tts-flash", Rate: 1}}
		w.store = live
		core.dispatchStatus, core.dispatchRaw = http.StatusConflict, `{"error":"主线已换段，请重新选择切点"}`
		claimV2(core, e)
		if err := w.processRoom(t.Context(), session); err != nil {
			t.Fatal(err)
		}
		if tts.calls != 1 || len(w.prepared) != 1 {
			t.Fatal("audio was not retained for cut retry", tts.calls, len(w.prepared))
		}
		if changed {
			live.identity.VoiceID = "B"
		}
		core.dispatchStatus = 0
		claimV2(core, e)
		if err := w.processRoom(t.Context(), session); err != nil {
			t.Fatal(err)
		}
		wantCalls, wantVoice := 1, "A"
		if changed {
			wantCalls, wantVoice = 2, "B"
		}
		if tts.calls != wantCalls || tts.last.VoiceID != wantVoice || agent.calls != 0 {
			t.Fatal("prepared audio followed wrong host or repeated model", changed, tts.calls, tts.last.VoiceID, agent.calls)
		}
	}
}

type hostSwitchDuringTTS struct {
	base   *fakeTTS
	change func()
}

type v2AudibleRateStore struct {
	*v2LiveHostStore
	rate float64
}

func (s *v2AudibleRateStore) GetPlanVoiceRate(context.Context, int64, int64) (model.PlanVoiceRate, error) {
	return model.PlanVoiceRate{Rate: s.rate}, nil
}

func TestV2LiveSpeedDoesNotRestoreFrozenModel(t *testing.T) {
	w, store, core, _, tts, session, e := v2Fixture(t, 10105)
	w.store = &v2AudibleRateStore{v2LiveHostStore: &v2LiveHostStore{v2Store: store, identity: model.LiveAgentVoiceIdentity{VoiceID: "B", Provider: "aliyun_qwen", Model: "qwen-audio-3.1-tts-flash", Rate: 1.1}}, rate: 1.4}
	claimV2(core, e)
	if err := w.processRoom(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if tts.last.VoiceID != "B" || tts.last.Model != "qwen-audio-3.1-tts-flash" || tts.last.Rate != 1.4 {
		t.Fatalf("speed restored opening identity: %+v", tts.last)
	}
}

func (s *hostSwitchDuringTTS) SynthesizeURL(ctx context.Context, req ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	a, err := s.base.SynthesizeURL(ctx, req)
	s.change()
	return a, err
}

func TestV2HostChangesDuringPaidSynthesisNeverDispatchesOldAudio(t *testing.T) {
	w, store, core, _, tts, session, e := v2Fixture(t, 10104)
	live := &v2LiveHostStore{v2Store: store, identity: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "qwen-audio-3.1-tts-flash", Provider: "aliyun_qwen", Rate: 1}}
	w.store = live
	w.tts = &hostSwitchDuringTTS{base: tts, change: func() { live.identity.VoiceID = "B" }}
	claimV2(core, e)
	if err := w.processRoom(t.Context(), session); err == nil {
		t.Fatal("accepted stale voice after paid return")
	}
	if tts.calls != 1 || core.dispatches != 0 || len(w.prepared) != 0 {
		t.Fatal("old paid audio dispatched/retained or bought twice", tts.calls, core.dispatches, len(w.prepared))
	}
}
