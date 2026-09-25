package agentgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }

func (stubProvider) Complete(_ context.Context, request Request) (Response, error) {
	return Response{Text: request.Model, Provider: "stub", Model: request.Model}, nil
}

func TestGatewayUsesDefaultAndAlias(t *testing.T) {
	gateway := New("stub_alias", "model-default")
	gateway.Register(stubProvider{}, "stub_alias")
	response, err := gateway.Complete(t.Context(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "model-default" || response.Provider != "stub" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestQwenProviderCompatibleRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing auth")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "qwen3.8-flash" {
			t.Fatalf("model=%v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"测试回答\"}}]}"))
	}))
	defer server.Close()

	provider := NewQwenProvider(QwenConfig{
		BaseURL: server.URL,
		APIKey:  "secret",
		Client:  &http.Client{Timeout: time.Second},
	})
	response, err := provider.Complete(t.Context(), Request{
		Model:     "qwen3.8-flash",
		Messages:  []Message{{Role: "user", Content: "你好"}},
		MaxTokens: 100,
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "测试回答" || response.Provider != ProviderQwen || response.Model != "qwen3.8-flash" {
		t.Fatalf("unexpected response: %#v", response)
	}
}
