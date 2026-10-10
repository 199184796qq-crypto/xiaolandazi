package decisionexecutor

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"testing"
)

func TestBridgeProtocolKeepsFiveTypesAndNone(t *testing.T) {
	plan := returnBridgePlan{ReplyText: "一只85元。"}
	for _, kind := range []string{"recall_continue", "same_topic", "topic_shift", "close_return", "reanchor"} {
		if _, err := validateBridge(`{"type":"`+kind+`","text":"咱们接着说刚才的用法。"}`, plan); err != nil {
			t.Fatal(kind, err)
		}
	}
	for _, raw := range []string{`{"type":"other","text":"咱们接着说用法。"}`, `{"type":"none","text":"继续"}`, `{"type":"none"}`, `{"type":"none","text":null}`} {
		if _, err := validateBridge(raw, plan); err == nil {
			t.Fatal("invalid bridge accepted", raw)
		}
	}
	for _, raw := range []string{`{"type":"same_topic","text":"咱们接着讲1号的吃法。","extra":"compatible"}`, `{"type":"same_topic","text":"包邮条件接着给你们讲。"}`} {
		if _, err := validateBridge(raw, plan); err != nil {
			t.Fatal("content hard filter still present", err)
		}
	}
	if _, err := validateBridge(`{"type":"none","text":""}`, plan); err != nil {
		t.Fatal(err)
	}
}

func TestExactDuplicateBridgeDoesNotCallTTSOrDispatch(t *testing.T) {
	store := &bridgeProfileStore{fakeStore: readyVoiceStore()}
	agent := &fakeAgent{responses: []string{`{"type":"topic_shift","text":"怕买整只吃不完浪费，这个半只装刚好。"}`}}
	tts := &fakeTTS{}
	core := &fakeCore{}
	w := New(store, core, agent, tts)
	var plan returnBridgePlan
	json.Unmarshal([]byte(`{"reply_text":"已回答发货安排。","units":[{"id":"next","text":"怕买整只吃不完浪费，这个半只装刚好。半只450g。"}]}`), &plan)
	if err := w.generateAndStageBridge(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, decisionItem{ID: "d1"}, plan, "", ""); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 0 || core.dispatches != 0 {
		t.Fatalf("calls agent=%d tts=%d dispatch=%d", agent.calls, tts.calls, core.dispatches)
	}
}

type bridgeProfileStore struct {
	*fakeStore
	resolved string
}

func (s *bridgeProfileStore) ResolveSpeechModel(_ context.Context, id string) (*agentgateway.SpeechModel, error) {
	s.resolved = id
	return &agentgateway.SpeechModel{ID: "interaction-default", Model: "existing-interaction-model", Enabled: true}, nil
}
func TestBridgeUsesInteractionAPIDefault(t *testing.T) {
	store := &bridgeProfileStore{fakeStore: readyVoiceStore()}
	w := New(store, nil, nil, nil)
	p, err := w.bridgeModel(context.Background())
	if err != nil || p.ID != "interaction-default" || store.resolved != "" {
		t.Fatal(p, err)
	}
}
