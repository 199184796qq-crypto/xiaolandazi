package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func TestDanmakuStageMetricsUseRequestStage(t *testing.T) {
	deepseek := agentgateway.SpeechModel{
		Protocol: "openai_chat",
		Model:    "deepseek-flash",
		BaseURL:  "https://api.deepseek.com/v1",
	}
	for _, stage := range []string{"danmaku_test_intent", "danmaku_test_batch_intent", "danmaku_test_intent_qa"} {
		t.Run(stage, func(t *testing.T) {
			var stages danmakuModelStages
			stages.record(deepseek, agentgateway.Request{Stage: stage}, 248*time.Millisecond)
			stages.record(deepseek, agentgateway.Request{Stage: "danmaku_test_answer", ReasoningEffort: "low"}, 1734*time.Millisecond)

			body := httptest.NewRecorder()
			writeJSON(body, 200, danmakuTestResponse{ModelStages: stages})
			var result struct {
				ModelStages danmakuModelStages `json:"model_stages"`
			}
			if err := json.Unmarshal(body.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ModelStages.Intent == nil || *result.ModelStages.Intent != (danmakuModelStage{Thinking: "off", ElapsedMS: 248}) {
				t.Fatalf("intent metrics: %s", body.Body.String())
			}
			if result.ModelStages.Answer == nil || *result.ModelStages.Answer != (danmakuModelStage{Thinking: "low", ElapsedMS: 1734}) {
				t.Fatalf("answer metrics: %s", body.Body.String())
			}
		})
	}
}

func TestDanmakuStageMetricsOmitUncalledStages(t *testing.T) {
	var stages danmakuModelStages
	profile := agentgateway.SpeechModel{Protocol: "openai_chat", Model: "qwen3.6-plus"}
	stages.record(profile, agentgateway.Request{Stage: "qa_pairs_organize"}, time.Millisecond)
	if stages.Intent != nil || stages.Answer != nil {
		t.Fatal("unrelated model call was recorded")
	}
	stages.record(profile, agentgateway.Request{Stage: "danmaku_test_answer", ReasoningEffort: "low"}, 12*time.Millisecond)
	body := httptest.NewRecorder()
	writeJSON(body, 200, danmakuTestResponse{ModelStages: stages})
	if !strings.Contains(body.Body.String(), `"model_stages":{"answer":{"thinking":"low_unavailable","elapsed_ms":12}}`) {
		t.Fatal(body.Body.String())
	}
}
