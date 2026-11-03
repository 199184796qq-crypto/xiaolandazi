package agentgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Providers that predate json_schema must not fail the call: the gateway drops
// to json_object first and only then to plain text.
func TestQwenProviderDegradesSchemaToJSONObject(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		format, _ := payload["response_format"].(map[string]any)
		if calls == 1 {
			if format["type"] != "json_schema" {
				t.Fatalf("first request type=%v want json_schema", format["type"])
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"json_schema is not supported"}}`))
			return
		}
		if format["type"] != "json_object" {
			t.Fatalf("fallback type=%v want json_object", format["type"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer server.Close()

	provider := NewQwenProvider(QwenConfig{
		BaseURL: server.URL,
		APIKey:  "secret",
		Client:  &http.Client{Timeout: time.Second},
	})
	response, err := provider.Complete(t.Context(), Request{
		Model:          "compatible-model",
		Messages:       []Message{{Role: "user", Content: "return json"}},
		MaxTokens:      100,
		ResponseFormat: ResponseJSON,
		ResponseSchema: []byte(`{"name":"anchor_reply","strict":true,"schema":{"type":"object","properties":{"回答":{"type":"string"}},"required":["回答"]}}`),
		Timeout:        time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2", calls)
	}
	if response.Text != `{"ok":true}` {
		t.Fatalf("text=%q", response.Text)
	}
}

func TestStructuredOutputHint(t *testing.T) {
	if _, ok := structuredOutputHint(nil); ok {
		t.Fatal("empty schema must not produce a hint")
	}
	if _, ok := structuredOutputHint([]byte(`{"name":"x"}`)); ok {
		t.Fatal("descriptor without schema must not produce a hint")
	}
	hint, ok := structuredOutputHint([]byte(`{"schema":{"type":"object"}}`))
	if !ok || hint["type"] != "json_schema" {
		t.Fatalf("hint=%v ok=%v", hint, ok)
	}
	descriptor, _ := hint["json_schema"].(map[string]any)
	if descriptor["name"] != "structured_output" {
		t.Fatalf("missing default name: %v", descriptor)
	}
}
