package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
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
	// Each fixture represents a separate process. Session IDs below intentionally
	// exercise different cases, not consecutive shows in the same global cache.
	interactionplan.Frozen = interactionplan.Cache{}
	store := &v2Store{readyVoiceStore(), "发货安排：第二天上午发货，特殊要求以客服确认为准。"}
	core := &fakeCore{}
	agent := &fakeAgent{}
	tts := &fakeTTS{}
	worker := New(store, core, agent, tts)
	worker.orderAudioComposer = func(context.Context, string) (ttsgateway.OrderBellAudio, error) {
		return ttsgateway.OrderBellAudio{}, ttsgateway.ErrOrderBellMissing
	}
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
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	// 称呼由程序前置拼接，不再额外调一次模型。
	if agent.calls != 0 || tts.calls != 1 {
		t.Fatal("body or intent reran after selection", agent.calls, tts.calls)
	}
	if !strings.HasSuffix(tts.last.Text, e.Body) {
		t.Fatal("address must sit before the frozen body", tts.last.Text)
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
func TestManualSelectedQuestionUsesConfiguredAnswerFlowAndFrozenPlaybackEnvelope(t *testing.T) {
	w, _, _, agent, _, session, envelope := v2Fixture(t, 10012)
	item := decisionItem{ID: "chosen-1", MissionKind: "reply_question", ManualPromoted: true,
		SampleQuestions: []string{"1号多少钱"}, UserIDs: []string{"viewer-1"},
		CurrentMainline: "正在介绍1号", ExpiresAt: time.Now().Add(time.Minute)}
	calls := 0
	w.SetManualAnswerFlow(func(_ context.Context, in ManualAnswerFlowInput, _ interactionplan.Complete) (interactionplan.Envelope, error) {
		calls++
		if in.Task != item.ID || in.Question != "1号多少钱" || len(in.UserIDs) != 1 || in.UserIDs[0] != "viewer-1" ||
			in.Session.ID != session.ID || in.Snapshot.Scope.Plan != 3 || in.CurrentMainline != item.CurrentMainline || !in.ExpiresAt.Equal(item.ExpiresAt) {
			t.Fatalf("manual identity or context lost: %#v", in)
		}
		envelope.Task, envelope.Epoch, envelope.Body = item.ID, "manual", "1号链接价格是87.6元。"
		return envelope, nil
	})
	text, err := w.generateInteractionV2(context.Background(), session, &item)
	if err != nil || calls != 1 || agent.calls != 0 || !strings.HasSuffix(text, envelope.Body) {
		t.Fatal(text, err, calls, agent.calls)
	}
	stored, ok := interactionplan.Decode(item.PurposeKnowledge)
	if !ok || stored.Task != item.ID || stored.Body != envelope.Body || stored.Fingerprint != envelope.Fingerprint {
		t.Fatal("answerflow body did not enter the existing speech contract", stored)
	}
}

func TestManualSelectedQuestionUsesExistingTTSAndDispatch(t *testing.T) {
	w, _, core, agent, tts, session, envelope := v2Fixture(t, 10015)
	core.claimRaw = `{"claimed":true,"item":{"id":"chosen-4","topic":"manual","mission_kind":"reply_question","manual_promoted":true,"sample_questions":["1号多少钱"],"user_ids":["viewer-1"],"nicknames":["小王"]}}`
	w.SetManualAnswerFlow(func(_ context.Context, in ManualAnswerFlowInput, _ interactionplan.Complete) (interactionplan.Envelope, error) {
		envelope.Task, envelope.Epoch, envelope.Body = in.Task, "manual", "1号链接价格是87.6元。"
		return envelope, nil
	})
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 0 || tts.calls != 1 || core.dispatches != 1 || !strings.HasSuffix(tts.last.Text, envelope.Body) {
		t.Fatal("manual answer bypassed existing speech path", agent.calls, tts.calls, core.dispatches, tts.last.Text)
	}
}

func TestUnpreparedQuestionDoesNotFallBackOnAnswerFlowFailure(t *testing.T) {
	for _, configured := range []bool{false, true} {
		w, _, _, agent, _, session, _ := v2Fixture(t, 10013)
		item := decisionItem{ID: "unprepared-question", MissionKind: "reply_question", SampleQuestions: []string{"哪天发货"}}
		if configured {
			w.SetManualAnswerFlow(func(context.Context, ManualAnswerFlowInput, interactionplan.Complete) (interactionplan.Envelope, error) {
				return interactionplan.Envelope{}, errors.New("新版回答不可用")
			})
		}
		text, err := w.generateInteractionV2(context.Background(), session, &item)
		if err == nil || agent.calls != 0 || text != "" {
			t.Fatal("unprepared question used the generic generator", text, err, agent.calls)
		}
	}
}

func TestManualSelectionDoesNotReanswerPreparedBodyOrWelcome(t *testing.T) {
	w, _, _, agent, _, session, envelope := v2Fixture(t, 10014)
	w.SetManualAnswerFlow(func(context.Context, ManualAnswerFlowInput, interactionplan.Complete) (interactionplan.Envelope, error) {
		t.Fatal("prepared body called answerflow again")
		return interactionplan.Envelope{}, nil
	})
	envelope.Task = "chosen-3"
	raw, _ := json.Marshal(envelope)
	item := decisionItem{ID: envelope.Task, MissionKind: "reply_question", ManualPromoted: true, PurposeKnowledge: raw}
	text, err := w.generateInteractionV2(context.Background(), session, &item)
	if err != nil || !strings.HasSuffix(text, envelope.Body) || agent.calls != 0 {
		t.Fatal(text, err, agent.calls)
	}
}
func TestInteractionV2FrozenVoiceAndPlanIgnoreLaterAuthorEdits(t *testing.T) {
	w, store, _, agent, _, session, e := v2Fixture(t, 10011)
	store.prompt = "已经修改"
	store.publishedVersion.VoiceIdentity.VoiceID = "other-voice"
	raw, _ := json.Marshal(e)
	item := decisionItem{ID: "d", PurposeKnowledge: raw}
	text, err := w.generateInteractionV2(context.Background(), session, &item)
	// 群称会前置到冻结正文之前，正文本身必须逐字保留，且不得再调模型。
	if err != nil || !strings.HasSuffix(text, e.Body) || agent.calls != 0 {
	}
	voice, ok, voiceErr := w.v2Voice(context.Background(), session, item)
	if voiceErr != nil || !ok || voice.VoiceID != "voice-1" {
		t.Fatal(voice, ok)
	}
}

type v2RateStore struct {
	*v2Store
	rate float64
}

func (s *v2RateStore) GetPlanVoiceRate(context.Context, int64, int64) (model.PlanVoiceRate, error) {
	return model.PlanVoiceRate{Rate: s.rate}, nil
}
func TestV2ExplicitSpeedAffectsNewSynthesisWithoutMutatingFrozenIdentity(t *testing.T) {
	w, store, _, _, _, session, e := v2Fixture(t, 10030)
	raw, _ := json.Marshal(e)
	item := decisionItem{PurposeKnowledge: raw}
	w.store = &v2RateStore{store, 1.4}
	voice, ok, voiceErr := w.v2Voice(context.Background(), session, item)
	if voiceErr != nil || !ok || voice.Config["rate"] != 1.4 || voice.VoiceID != "voice-1" {
		t.Fatal(voice, ok)
	}
	frozen, _ := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	if frozen.Voice.Config["rate"] == 1.4 {
		t.Fatal("live control mutated frozen snapshot")
	}
}
