package agentgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortableStageRoutingDoesNotUseQwenOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Errorf("bad transport: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["model"] != "replacement-analysis-model" {
			t.Errorf("bad model: %v", payload["model"])
		}
		if _, exists := payload["enable_thinking"]; exists {
			t.Error("vendor-specific option leaked")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"version\":\"anchor-delivery/v1\"}"}}]}`))
	}))
	defer server.Close()
	t.Setenv("AGENT_COMPATIBLE_BASE_URL", server.URL)
	t.Setenv("AGENT_COMPATIBLE_API_KEY", "fixture-secret")
	t.Setenv("ANCHOR_ANALYSIS_PROVIDER", "compatible")
	t.Setenv("ANCHOR_ANALYSIS_MODEL", "replacement-analysis-model")
	result, err := NewFromEnv().Complete(context.Background(), Request{Stage: "style_analysis", Messages: []Message{{Role: "system", Content: "固定协议"}}, ResponseFormat: ResponseJSON})
	if err != nil || result.Provider != "compatible" || result.Model != "replacement-analysis-model" {
		t.Fatalf("route failed: %+v %v", result, err)
	}
}

func TestMissingPortableModelFailsClosed(t *testing.T) {
	t.Setenv("ANCHOR_ANALYSIS_PROVIDER", "compatible")
	t.Setenv("ANCHOR_ANALYSIS_MODEL", "")
	_, err := NewFromEnv().Complete(context.Background(), Request{Stage: "style_analysis"})
	if err == nil {
		t.Fatal("silently used default Qwen model on different provider")
	}
}
