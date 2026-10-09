package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/agentgateway"
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
