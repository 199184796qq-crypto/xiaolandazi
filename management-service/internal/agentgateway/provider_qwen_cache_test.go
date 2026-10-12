package agentgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Verify that CacheSystemPrompt marks only the leading system message(s) with
// cache_control, leaves user messages untouched, and that the provider reports
// cached input tokens back from usage.prompt_tokens_details.cached_tokens.
func TestQwenProviderCacheSystemPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role         string         `json:"role"`
				Content      string         `json:"content"`
				CacheControl map[string]any `json:"cache_control"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(payload.Messages))
		}
		if payload.Messages[0].Role != "system" || payload.Messages[1].Role != "system" || payload.Messages[2].Role != "user" {
			t.Fatalf("unexpected roles: %+v", payload.Messages)
		}
		if typ, _ := payload.Messages[0].CacheControl["type"]; typ != "ephemeral" {
			t.Fatalf("first system should carry cache_control, got %+v", payload.Messages[0].CacheControl)
		}
		if typ, _ := payload.Messages[1].CacheControl["type"]; typ != "ephemeral" {
			t.Fatalf("second system should carry cache_control, got %+v", payload.Messages[1].CacheControl)
		}
		if payload.Messages[2].CacheControl != nil {
			t.Fatalf("user message must not carry cache_control, got %+v", payload.Messages[2].CacheControl)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"通过"}}],"usage":{"prompt_tokens":1000,"completion_tokens":5,"total_tokens":1005,"prompt_tokens_details":{"cached_tokens":512}}}`))
	}))
	defer server.Close()

	provider := NewQwenProvider(QwenConfig{BaseURL: server.URL, APIKey: "secret", Client: &http.Client{Timeout: time.Second}})
	response, err := provider.Complete(context.Background(), Request{
		Model: "qwen3.7-plus",
		Messages: []Message{
			{Role: "system", Content: "固定P0规则"},
			{Role: "system", Content: "固定审核母版"},
			{Role: "user", Content: "变化的候选正文"},
		},
		MaxTokens:        100,
		CacheSystemPrompt: true,
		Timeout:          time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.CachedInputTokens != 512 {
		t.Fatalf("expected cached tokens 512, got %d", response.CachedInputTokens)
	}
}

// When caching is off, no message carries cache_control.
func TestQwenProviderNoCacheWithoutFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				CacheControl map[string]any `json:"cache_control"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		for _, m := range payload.Messages {
			if m.CacheControl != nil {
				t.Fatalf("cache_control should be absent, got %+v", m.CacheControl)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"通过"}}]}`))
	}))
	defer server.Close()

	provider := NewQwenProvider(QwenConfig{BaseURL: server.URL, APIKey: "secret", Client: &http.Client{Timeout: time.Second}})
	_, err := provider.Complete(context.Background(), Request{
		Model:    "qwen3.7-plus",
		Messages: []Message{{Role: "system", Content: "固定审核母版"}, {Role: "user", Content: "候选"}},
		MaxTokens: 100,
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
}
