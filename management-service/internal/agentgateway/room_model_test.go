package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type poolRoomFixture struct {
	profiles map[int64]SpeechModel
	stopped  bool
}

func (f *poolRoomFixture) ResolveTextRole(ctx context.Context, _ string) (string, error) {
	scope := ContextRoomModelScope(ctx)
	if f.stopped || scope.RuntimeID == 0 {
		return "", errors.New("lease stopped")
	}
	return f.profiles[scope.RoomID].ID, nil
}
func (f *poolRoomFixture) ResolveSpeechModel(ctx context.Context, _ string) (*SpeechModel, error) {
	p := f.profiles[ContextRoomModelScope(ctx).RoomID]
	if p.ID == "" || f.stopped {
		return nil, errors.New("lease stopped")
	}
	return &p, nil
}

func (f *poolRoomFixture) ResolveOperatorSpeechModel(context.Context, string) (*SpeechModel, error) {
	return nil, errors.New("live stage selected an operator profile")
}

func TestRoomModelUsesOneCredentialAcrossSpeechReviewAndPlanning(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "qwen3.7-flash" {
			t.Error("pinned model replaced", body.Model)
		}
		if body.MaxTokens != 6000 {
			t.Error("request did not use leased output cap", body.MaxTokens)
		}
		key := r.Header.Get("Authorization")
		if (key != "Bearer fixture-account-A" && key != "Bearer fixture-account-B") ||
			r.URL.Path != "/"+strings.TrimPrefix(key, "Bearer ")+"/v1/chat/completions" {
			t.Error("wrong account credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fixture result"}}]}`))
	}))
	defer server.Close()
	originalClient := speechHTTPClient
	speechHTTPClient = server.Client()
	defer func() { speechHTTPClient = originalClient }()
	f := &poolRoomFixture{profiles: map[int64]SpeechModel{}}
	for room, key := range map[int64]string{101: "fixture-account-A", 102: "fixture-account-B"} {
		f.profiles[room] = SpeechModel{ID: key, Name: key, Model: "qwen3.7-flash", Protocol: "openai_chat", BaseURL: server.URL + "/" + key + "/v1", APIKey: key, Enabled: true, TimeoutMS: 1000, MaxTokens: 6000}
	}
	g := New("qwen", "unrelated-default").WithSpeechModels(f)
	stages := []string{
		"mainline_text", "mainline_final_content_review", "mainline_angle_plan", "interaction_bridge",
		"interaction_single_direct_answer", "interaction_single_greet", "interaction_single_answer",
		"interaction_no_fact_boundary", "interaction_boundary_line", "interaction_covered_facts",
		"interaction_addressing_plan", "interaction_p0_sentence_rewrite", "interaction_purpose_judge",
		"interaction_purpose_resolution", "interaction_answerflow_intent", "interaction_answerflow_intent_qa", "interaction_answerflow_batch_intent", "interaction_answerflow_answer", "interaction_answerflow_recall_query",
	}
	for _, room := range []int64{101, 102} {
		ctx := WithRoomModelScope(context.Background(), RoomModelScope{TenantID: 14, RoomID: room, RuntimeID: room, RequireLease: true})
		for _, stage := range stages {
			provider, model, err := g.ResolveStageModel(ctx, stage)
			if err != nil || model != f.profiles[room].Model || provider != "anchor:"+f.profiles[room].ID {
				t.Fatal("billing model differs from room allocation", stage, provider, model, err)
			}
			result, err := g.Complete(ctx, Request{Stage: stage, Model: "old-model", MaxTokens: 9000, Messages: []Message{{Role: "user", Content: "fixture task"}}, Timeout: time.Second})
			if err != nil || result.Model != "qwen3.7-flash" || result.Provider != "anchor:"+f.profiles[room].ID {
				t.Fatal("room model routing failed", stage, result, err)
			}
		}
	}
	f.stopped = true
	ctx := WithRoomModelScope(context.Background(), RoomModelScope{TenantID: 14, RoomID: 101, RuntimeID: 101, RequireLease: true})
	for _, stage := range stages {
		if _, err := g.Complete(ctx, Request{Stage: stage}); err == nil {
			t.Fatal("stopped lease fell back to shared credential", stage)
		}
	}
	if calls := int(requests.Load()); calls != len(stages)*len(f.profiles) {
		t.Fatal("late work called vendor", calls)
	}
}
