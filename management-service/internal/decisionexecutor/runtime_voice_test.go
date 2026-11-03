package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
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
	envelope, _ := json.Marshal(interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: "answer", Tenant: 7, Room: 11, Session: 9})
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
