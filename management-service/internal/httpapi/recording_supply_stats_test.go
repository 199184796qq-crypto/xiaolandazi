package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/ttsgateway"
)

func TestRecordingSupplyStatsSeparateSourceUpdatesUnitsAndFallback(t *testing.T) {
	states := []recordingSentenceState{{Source: "第一句。"}, {Source: "第二句。"}, {Source: "第三句。"}, {Source: "第四句。"}}
	s := newRecordingSupplyStats(14, 29, 480, 5, 2, "tab-stable", states, []bool{true, true, false, true}, 25, 25)
	s.observe(states[0].Source, true, recordingReuseResult{Generated: true, Units: make([]recordingReadyUnit, 3)}, nil)
	s.observe(states[1].Source, true, recordingReuseResult{CacheHit: true, GenerationErr: errLiveCostOptionalDeclined, Units: make([]recordingReadyUnit, 1)}, nil)
	s.observe(states[2].Source, false, recordingReuseResult{Units: make([]recordingReadyUnit, 1)}, nil)
	s.observe(states[3].Source, true, recordingReuseResult{}, errors.New("unavailable"))
	s.write(true)
	if s.Generated != 1 || s.Updated != 1 || s.UpdatedCharacters != 4 || s.Reused != 2 || s.PersistentReused != 1 || s.ReadyUnits != 5 || s.RefreshFailed != 2 || s.BudgetDeclined != 1 || s.Unavailable != 1 || !s.Complete {
		t.Fatalf("split units/fallback inflated successful updates: %+v", s)
	}
	partial := newRecordingSupplyStats(14, 29, 480, 5, 3, "tab-stable", states, nil, 25, 0)
	partial.observe(states[0].Source, false, recordingReuseResult{Units: make([]recordingReadyUnit, 1)}, nil)
	partial.write(false)
	if partial.Complete || !partial.BudgetPaused || partial.Generated != 0 {
		t.Fatalf("partial/capped tab misreported: %+v", partial)
	}
}

func TestRecordingCostAttributionSurvivesModelAndTTSRequestHashing(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	a := &livecost.Attribution{Source: "recording_mainline", Round: 3, TabID: "stable-tab", SourceIndex: 7}
	scope := liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 480, Key: "stable-request", Priority: livecost.PriorityOptional, Attribution: a}
	ctx := withLiveCostScope(context.Background(), scope)
	if err := s.beginLiveModelCost(ctx, 1, "polish", "", agentgateway.Request{Stage: "polish", Provider: "qwen", Model: "m", MaxTokens: 10, Messages: []agentgateway.Message{{Role: "user", Content: "原文"}}}); err != nil {
		t.Fatal(err)
	}
	s.finishLiveModelCost(ctx, 1, agentgateway.Response{Provider: "qwen", Model: "m", InputTokens: 5, OutputTokens: 2}, 5, 2, nil)
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "m")
	g.Register(provider)
	if _, err := g.SynthesizeURL(s.withLiveCostTTS(ctx, scope), ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "测试。"}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 2 {
		t.Fatal(store.requests)
	}
	for _, r := range store.requests {
		raw, _ := json.Marshal(r)
		var saved livecost.Request
		if err := json.Unmarshal(raw, &saved); err != nil || saved.Attribution == nil || *saved.Attribution != *a || saved.SessionID != 480 || saved.Priority != livecost.PriorityOptional {
			t.Fatalf("cannot attribute a hashed paid request to its round: %s %v", raw, err)
		}
	}
}
