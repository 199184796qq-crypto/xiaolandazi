package agentgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQwenProviderFallsBackWhenJSONResponseFormatUnsupported(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			if _, ok := payload["response_format"]; !ok {
				t.Fatal("first request should include response_format")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unsupported response_format"}}`))
			return
		}
		if _, ok := payload["response_format"]; ok {
			t.Fatal("fallback request must remove response_format")
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
		Model:          "another-compatible-model",
		Messages:       []Message{{Role: "user", Content: "return json"}},
		MaxTokens:      100,
		ResponseFormat: ResponseJSON,
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
