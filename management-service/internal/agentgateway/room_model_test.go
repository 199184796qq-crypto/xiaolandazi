package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
func TestRoomModelUsesOneCredentialAcrossSpeechReviewAndPlanning(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "qwen3.7-flash" {
			t.Error("pinned model replaced", body.Model)
		}
		key := r.Header.Get("Authorization")
		if key != "Bearer fixture-account-A" && key != "Bearer fixture-account-B" {
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
		f.profiles[room] = SpeechModel{ID: key, Name: key, Model: "qwen3.7-flash", Protocol: "openai_chat", BaseURL: server.URL + "/v1", APIKey: key, Enabled: true, TimeoutMS: 1000, MaxTokens: 2000}
	}
	g := New("qwen", "unrelated-default").WithSpeechModels(f)
	for _, room := range []int64{101, 102} {
		ctx := WithRoomModelScope(context.Background(), RoomModelScope{TenantID: 14, RoomID: room, RuntimeID: room, RequireLease: true})
		for _, stage := range []string{"mainline_text", "mainline_final_content_review", "mainline_angle_plan", "interaction_agent_answer", "interaction_bridge"} {
			result, err := g.Complete(ctx, Request{Stage: stage, Model: "old-model", Messages: []Message{{Role: "user", Content: "fixture task"}}, Timeout: time.Second})
			if err != nil || result.Model != "qwen3.7-flash" || result.Provider != "anchor:"+f.profiles[room].ID {
				t.Fatal("room model routing failed", stage, result, err)
			}
		}
	}
	f.stopped = true
	ctx := WithRoomModelScope(context.Background(), RoomModelScope{TenantID: 14, RoomID: 101, RuntimeID: 101, RequireLease: true})
	if _, err := g.Complete(ctx, Request{Stage: "mainline_text"}); err == nil {
		t.Fatal("stopped lease fell back to shared credential")
	}
	if requests != 10 {
		t.Fatal("late work called vendor", requests)
	}
}
