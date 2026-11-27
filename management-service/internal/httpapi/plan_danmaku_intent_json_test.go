package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"strings"
	"testing"
)

func TestDanmakuIntentModelJSONRetainsOriginalFieldsBeforeNormalization(t *testing.T) {
	const modelJSON = `{"meaning":"  理解当前表达  ","speech_act":"提问","asks":[{"text":"  现在是否可用  ","subjects":[],"attribute":"状态"}],"uncertainties":[],"provider_only":{"number":9007199254740993,"note":"<b>原始返回</b>"}}`
	for _, fenced := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "fenced JSON"}[fenced], func(t *testing.T) {
			calls := 0
			complete := func(_ context.Context, stage string, messages []agentgateway.Message) (agentgateway.Response, error) {
				calls++
				if calls == 1 {
					if stage != "danmaku_test_intent" {
						t.Fatal("unexpected first stage", stage)
					}
					text := modelJSON
					if fenced {
						text = "```json\n" + text + "\n```"
					}
					return agentgateway.Response{Text: text, FinishReason: "stop"}, nil
				}
				if stage != "danmaku_test_answer" || strings.Contains(messages[1].Content, "provider_only") || strings.Contains(messages[1].Content, "intent_model_json") {
					t.Fatal("raw model JSON must not be added to the answer model input", stage)
				}
				return agentgateway.Response{Text: `意图：理解当前诉求
类型：提问
歧义：否
澄清句：
话术：需要确认当前状态。`, FinishReason: "stop"}, nil
			}
			out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "现在是否可用"}, danmakuLibrary{}, nil, complete)
			if err != nil || calls != 2 {
				t.Fatal(err, calls)
			}
			var formatted bytes.Buffer
			if err := json.Indent(&formatted, []byte(modelJSON), "", "  "); err != nil {
				t.Fatal(err)
			}
			if out.IntentModelJSON != formatted.String() || out.Intent.Meaning != "理解当前表达" || out.Intent.Asks[0].Text != "现在是否可用" {
				t.Fatalf("original JSON was replaced by normalized intent: %+v", out)
			}
			encoded, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				JSON string `json:"intent_model_json"`
			}
			if err := json.Unmarshal(encoded, &response); err != nil || response.JSON != formatted.String() {
				t.Fatal("raw JSON missing from API response", err, response.JSON)
			}
		})
	}
}
