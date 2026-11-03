package agentgateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseCompletionRequiresSupplierEvidence(t *testing.T) {
	for _, reason := range []string{"stop", "STOP", " end_turn ", "stop_sequence", "completed"} {
		if !(Response{FinishReason: reason}).CompletedNormally() {
			t.Fatalf("normal completion lost: %q", reason)
		}
	}
	for _, reason := range []string{"", "length", "max_tokens", "incomplete", "content_filter", "tool_calls", "error", "unknown"} {
		if (Response{FinishReason: reason}).CompletedNormally() {
			t.Fatalf("unproven completion accepted: %q", reason)
		}
	}
}

func TestSpeechTransportsPreserveFinishReasonAndUsage(t *testing.T) {
	for _, tc := range []struct{ protocol, raw, reason string }{
		{"openai_chat", `{"choices":[{"finish_reason":"stop","message":{"content":"正文。"}}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`, "stop"},
		{"openai_chat", `{"choices":[{"finish_reason":"length","message":{"content":"正文。"}}]}`, "length"},
		{"openai_chat", `{"choices":[{"message":{"content":"正文。"}}]}`, ""},
		{"openai_responses", `{"status":"completed","output":[{"content":[{"type":"output_text","text":"正文。"}]}],"usage":{"input_tokens":12,"output_tokens":5}}`, "completed"},
		{"openai_responses", `{"status":"incomplete","output":[{"content":[{"type":"output_text","text":"正文。"}]}]}`, "incomplete"},
		{"anthropic", `{"stop_reason":"end_turn","content":[{"type":"text","text":"正文。"}],"usage":{"input_tokens":12,"output_tokens":5}}`, "end_turn"},
		{"anthropic", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"正文。"}]}`, "max_tokens"},
		{"gemini", `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"正文。"}]}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":5}}`, "STOP"},
		{"gemini", `{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"正文。"}]}}]}`, "MAX_TOKENS"},
	} {
		t.Run(tc.protocol+"/"+tc.reason, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.raw)
			}))
			defer server.Close()
			p := speechFixture()
			p.Protocol, p.BaseURL = tc.protocol, server.URL
			got, err := completeSpeechEndpoint(context.Background(), p, Request{Model: p.Model, Messages: []Message{{Role: "user", Content: "测试"}}, MaxTokens: 64}, server.Client())
			if err != nil || calls != 1 || got.Text != "正文。" || got.FinishReason != tc.reason {
				t.Fatal(got, err, calls)
			}
			if tc.reason == "stop" || tc.reason == "completed" || tc.reason == "end_turn" || tc.reason == "STOP" {
				if got.InputTokens != 12 || got.OutputTokens != 5 {
					t.Fatalf("supplier metadata must not change accounting: %+v", got)
				}
			}
		})
	}
}
