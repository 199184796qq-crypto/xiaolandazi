package rerank

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Config{Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "qwen3.7-text-rerank", Timeout: 2 * time.Second})
}

func TestRerankMapsScoresBackToRequestPositions(t *testing.T) {
	var requestBody rerankRequest
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != rerankPath {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization=%q", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &requestBody); err != nil {
			t.Fatal(err)
		}
		// Results arrive sorted by score, not by request index.
		_, _ = w.Write([]byte(`{"output":{"results":[
			{"index":2,"relevance_score":0.91},
			{"index":0,"relevance_score":0.42},
			{"index":1,"relevance_score":0.07}
		]},"usage":{"total_tokens":42},"request_id":"fixture"}`))
	})
	scores, err := client.Rerank(context.Background(), " 怎么退货 ", []string{"退款流程", " 快递时效 ", "售后电话"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 3 || scores[0] != 0.42 || scores[1] != 0.07 || scores[2] != 0.91 {
		t.Fatalf("scores=%v", scores)
	}
	if requestBody.Model != "qwen3.7-text-rerank" || requestBody.Input.Query != "怎么退货" {
		t.Fatalf("request=%+v", requestBody)
	}
	if len(requestBody.Input.Documents) != 3 || requestBody.Input.Documents[1] != "快递时效" {
		t.Fatalf("documents=%v", requestBody.Input.Documents)
	}
	if requestBody.Parameters.ReturnDocuments {
		t.Fatal("return_documents must stay false to keep the response small")
	}
}

func TestRerankHTTPErrorSurfacesProviderMessage(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"InvalidParameter","message":"input.documents is invalid"}`))
	})
	_, err := client.Rerank(context.Background(), "问题", []string{"文档"})
	if err == nil || !strings.Contains(err.Error(), "input.documents is invalid") {
		t.Fatalf("err=%v", err)
	}
}

func TestRerankRejectsIncompleteOrDuplicateIndexes(t *testing.T) {
	for _, body := range []string{
		`{"output":{"results":[{"index":0,"relevance_score":0.5}]}}`,
		`{"output":{"results":[{"index":0,"relevance_score":0.5},{"index":0,"relevance_score":0.6}]}}`,
		`{"output":{"results":[{"index":0,"relevance_score":0.5},{"index":7,"relevance_score":0.6}]}}`,
	} {
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		})
		if _, err := client.Rerank(context.Background(), "问题", []string{"甲", "乙"}); err == nil {
			t.Fatalf("body %s must be rejected", body)
		}
	}
}

func TestRerankDisabledWithoutAPIKey(t *testing.T) {
	client := New(Config{Enabled: true, BaseURL: "http://127.0.0.1:1", Model: "qwen3.7-text-rerank"})
	if client.Enabled() {
		t.Fatal("client without API key must be disabled")
	}
	if _, err := client.Rerank(context.Background(), "问题", []string{"文档"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRerankRejectsTooManyDocumentsWithoutCallingTheProvider(t *testing.T) {
	calls := 0
	client := testClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	client.maxDocuments = 2
	_, err := client.Rerank(context.Background(), "问题", []string{"甲", "乙", "丙"})
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRerankEmptyInputSkipsTheProvider(t *testing.T) {
	calls := 0
	client := testClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	if scores, err := client.Rerank(context.Background(), "  ", []string{"文档"}); err != nil || scores != nil {
		t.Fatalf("scores=%v err=%v", scores, err)
	}
	if scores, err := client.Rerank(context.Background(), "问题", nil); err != nil || scores != nil {
		t.Fatalf("scores=%v err=%v", scores, err)
	}
	if calls != 0 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestRerankClampsScoresIntoUnitRange(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"results":[
			{"index":0,"relevance_score":1.7},
			{"index":1,"relevance_score":-0.2}
		]}}`))
	})
	scores, err := client.Rerank(context.Background(), "问题", []string{"甲", "乙"})
	if err != nil || scores[0] != 1 || scores[1] != 0 {
		t.Fatalf("scores=%v err=%v", scores, err)
	}
}

func TestConfigFromEnvDefaultsToPublicEndpointAndModel(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("DASHSCOPE_RERANK_ENABLED", "")
	t.Setenv("DASHSCOPE_RERANK_BASE_URL", "")
	t.Setenv("DASHSCOPE_RERANK_MODEL", "")
	t.Setenv("DASHSCOPE_RERANK_TIMEOUT_MS", "")
	t.Setenv("DASHSCOPE_RERANK_MAX_DOCUMENTS", "")
	config := ConfigFromEnv()
	if !config.Enabled || config.APIKey != "test-key" || config.BaseURL != defaultBaseURL || config.Model != defaultModel {
		t.Fatalf("config=%+v", config)
	}
	if config.Timeout != defaultTimeoutMS*time.Millisecond || config.MaxDocuments != defaultMaxDocuments {
		t.Fatalf("config=%+v", config)
	}
}

func TestConfigFromEnvHonorsKillSwitch(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("DASHSCOPE_RERANK_ENABLED", "false")
	client := New(ConfigFromEnv())
	if client.Enabled() {
		t.Fatal("DASHSCOPE_RERANK_ENABLED=false must disable rerank")
	}
}
