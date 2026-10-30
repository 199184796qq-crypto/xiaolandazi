package decisionexecutor

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

func TestV2SingleViewerCannotReceivePluralAddressSuffix(t *testing.T) {
	w, store, _, _, _, session, _ := v2Fixture(t, 10020)
	store.addressingPrefs = model.RoomAddressingPreferences{PreferredTerms: []string{"哥姐", "兄弟姐妹", "家人们"}}
	snapshot, _ := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	address := w.v2Address(context.Background(), session, &decisionItem{ID: "three", Nicknames: []string{"小王"}}, snapshot)
	if strings.Contains(address, "哥姐") || strings.Contains(address, "兄弟姐妹") || strings.Contains(address, "家人们") {
		t.Fatal(address)
	}
}

type v2Store struct {
	*fakeStore
	prompt string
}

func (s *v2Store) GetLiveAgentPlanPrompt(context.Context, int64, int64) (string, time.Time, error) {
	return s.prompt, time.Time{}, nil
}
func v2Fixture(t *testing.T, id int64) (*Worker, *v2Store, *fakeCore, *fakeAgent, *fakeTTS, model.LiveRuntimeSession, interactionplan.Envelope) {
	t.Helper()
	store := &v2Store{readyVoiceStore(), "发货安排：第二天上午发货，特殊要求以客服确认为准。"}
	core := &fakeCore{}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	worker := New(store, core, agent, tts)
	session := model.LiveRuntimeSession{ID: id, TenantID: 7, RoomID: 11, Status: "running"}
	snapshot, err := interactionplan.Freeze(context.Background(), store, session)
	if err != nil {
		t.Fatal(err)
	}
	e := interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: "body-task", Epoch: "boot:3:1", Revision: 1, Tenant: 7, Room: 11, Session: id, Plan: 3, Fingerprint: snapshot.Fingerprint, Body: "第二天上午发货，特殊要求以客服确认为准。", Intent: interactionplan.Intent{Relevant: true, Question: true}, ExpiresAt: time.Now().Add(time.Minute)}
	return worker, store, core, agent, tts, session, e
}
func claimV2(core *fakeCore, e interactionplan.Envelope) {
	raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "d-1", "topic": "body-task", "mission_kind": "reply_question", "sample_questions": []string{"哪天发货"}, "nicknames": []string{"小王"}, "purpose_knowledge": e, "expires_at": e.ExpiresAt}})
	core.claimRaw = string(raw)
}
func TestInteractionV2RealWorkerReusesEarlyBodyAndOnlyJoinsOnce(t *testing.T) {
	w, _, core, agent, tts, session, e := v2Fixture(t, 10001)
	claimV2(core, e)
	agent.responses = []string{"小王，" + e.Body}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || agent.requests[0].Stage != "interaction_single_greet" || tts.calls != 1 {
		t.Fatal("body or intent reran after selection", agent.calls, tts.calls)
	}
	if !strings.Contains(tts.last.Text, e.Body) || core.dispatches < 1 || core.dispatch["deferred_return"] != false {
		t.Fatal("old bridge path still active or body changed", core.dispatch)
	}
	if core.releases != 0 {
		t.Fatal("successful dispatch released twice")
	}
}
func TestInteractionV2OldAddressCannotLeakIntoRegeneratedBody(t *testing.T) {
	w, _, core, agent, tts, session, e := v2Fixture(t, 10002)
	claimV2(core, e)
	agent.responses = []string{"小李，保证今天发货。"}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tts.last.Text, e.Body) || strings.Contains(tts.last.Text, "小李") {
		t.Fatal(tts.last.Text)
	}
}
func TestInteractionV2ExpiredOrCrossSessionBodyNeverCallsTTS(t *testing.T) {
	for i, kind := range []string{"expiry", "session", "fingerprint"} {
		t.Run(kind, func(t *testing.T) {
			w, _, core, agent, tts, session, e := v2Fixture(t, int64(10003+i))
			switch kind {
			case "expiry":
				e.ExpiresAt = time.Now().Add(-time.Second)
			case "session":
				e.Session++
			case "fingerprint":
				e.Fingerprint = "stale"
			}
			claimV2(core, e)
			if err := w.processRoom(context.Background(), session); err == nil {
				t.Fatal("accepted invalid body")
			}
			if agent.calls != 0 || tts.calls != 0 || core.dispatches != 0 {
				t.Fatal(agent.calls, tts.calls, core.dispatches)
			}
		})
	}
}
func TestInteractionV2ManualRequestsUseSamePlanProtocol(t *testing.T) {
	w, _, core, agent, tts, session, _ := v2Fixture(t, 10010)
	agent.responses = []string{`{"relevant":true,"question":true,"meaning":"发货时间"}`, "第二天上午发货。", "小王，第二天上午发货。"}
	core.claimRaw = `{"claimed":true,"item":{"id":"d-1","topic":"manual","mission_kind":"reply_question","sample_questions":["哪天发"],"nicknames":["小王"]}}`
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 3 || tts.calls != 1 {
		t.Fatal(agent.calls, tts.calls)
	}
}
func TestInteractionV2FrozenVoiceAndPlanIgnoreLaterAuthorEdits(t *testing.T) {
	w, store, _, agent, _, session, e := v2Fixture(t, 10011)
	store.prompt = "已经修改"
	store.publishedVersion.VoiceIdentity.VoiceID = "other-voice"
	raw, _ := json.Marshal(e)
	item := decisionItem{ID: "d", PurposeKnowledge: raw}
	text, err := w.generateInteractionV2(context.Background(), session, &item)
	if err != nil || text != e.Body || agent.calls != 0 {
		t.Fatal(text, err, agent.calls)
	}
	voice, ok := w.v2Voice(session, item)
	if !ok || voice.VoiceID != "voice-1" {
		t.Fatal(voice, ok)
	}
}
