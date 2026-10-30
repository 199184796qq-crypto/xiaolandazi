package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"livecompanion/management/internal/agentunderstanding"
)

// This is the response shape from production event 315 (2026-10-30):
// quantity is a number and audience is an array of text. No model is called.
func TestLiveStrategyIntentModelProductImageReplay(t *testing.T) {
	raw, err := os.ReadFile("testdata/live_strategy_intent_product_image.json")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected model transport: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}},
			"usage":   map[string]int{"prompt_tokens": 3740, "completion_tokens": 231, "total_tokens": 3971},
		})
	}))
	defer server.Close()
	t.Setenv("DASHSCOPE_BASE_URL", server.URL)
	t.Setenv("DASHSCOPE_API_KEY", "local-fixture-only")
	policy := agentunderstanding.DefaultPolicy()
	output, response, err := callLiveStrategyIntentModel(context.Background(), "Only JSON", "添加图片商品", nil, nil, policy)
	if err != nil {
		t.Fatalf("valid product content rejected: %v", err)
	}
	if calls != 1 || response.Text != strings.TrimSpace(string(raw)) || response.TotalTokens != 3971 {
		t.Fatalf("response provenance or request count changed: calls=%d response=%#v", calls, response)
	}
	if output.Kind != "command" || output.Intent != "product.add" || output.Confidence != 0.95 || output.Target.LinkKey != "1" ||
		output.Changes.Quantity != "1" || output.Changes.Audience != "5-6人" || output.Changes.DailyPrice != "56.3" {
		t.Fatalf("product intent/content changed: %#v", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var api struct {
		Changes struct {
			Quantity string `json:"quantity"`
			Audience string `json:"audience"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(encoded, &api); err != nil || api.Changes.Quantity != "1" || api.Changes.Audience != "5-6人" {
		t.Fatalf("existing frontend text contract broken: %s (%v)", encoded, err)
	}
}

func TestLiveStrategyIntentContentNormalization(t *testing.T) {
	for _, tc := range []struct{ name, changes, field, want string }{
		{"quantity", `{"quantity":1}`, "quantity", "1"},
		{"price precision", `{"daily_price":56.30}`, "daily_price", "56.30"},
		{"large quantity", `{"quantity":9007199254740993}`, "quantity", "9007199254740993"},
		{"negative attribute", `{"attribute_value":-18.50}`, "attribute_value", "-18.50"},
		{"activity price", `{"activity_price":45.67}`, "activity_price", "45.67"},
		{"numeric spec", `{"spec":3}`, "spec", "3"},
		{"numeric fact", `{"fact_value":0}`, "fact_value", "0"},
		{"exponent preserved", `{"quantity":1e3}`, "quantity", "1e3"},
		{"audience list", `{"audience":["5-6人","家庭聚餐"]}`, "audience", "5-6人、家庭聚餐"},
		{"audience empty list", `{"audience":[]}`, "audience", ""},
		{"existing audience", `{"audience":" 5-6人 "}`, "audience", " 5-6人 "},
		{"existing units", `{"quantity":"1只，约3斤"}`, "quantity", "1只，约3斤"},
		{"null quantity", `{"quantity":null}`, "quantity", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"kind":"command","intent":"product.add","changes":` + tc.changes + `,"confidence":0.95}`
			output, err := decodeLiveStrategyIntentModelOutput("```json\n" + input + "\n```")
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(output.Changes)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]string
			if err := json.Unmarshal(body, &fields); err != nil || fields[tc.field] != tc.want {
				t.Fatalf("content changed: got=%s want[%s]=%q err=%v", body, tc.field, tc.want, err)
			}
		})
	}
}

func TestLiveStrategyIntentNormalizationPreservesPresenceAndControlTypes(t *testing.T) {
	raw := `{"kind":"command","intent":"fact.update","target":{"fact_key":"售后","plan_id":7},"changes":{"forbidden_wording":"不要夸大","safe_rewrite":"","room_roles":["traffic"],"attribute_display_priority":2},"missing":["fact_value"],"confidence":0.95}`
	var strict liveStrategyIntentModelOutput
	if err := decodeProductJSON(raw, &strict); err != nil {
		t.Fatal(err)
	}
	output, err := decodeLiveStrategyIntentModelOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(strict)
	got, _ := json.Marshal(output)
	if string(got) != string(want) || output.Changes.SafeRewrite == nil || *output.Changes.SafeRewrite != "" || output.Target.PlanID != 7 {
		t.Fatalf("valid typed/presence contract changed: got=%s want=%s", got, want)
	}
	output, err = decodeLiveStrategyIntentModelOutput(`{"kind":"chat","intent":"chat","changes":{"safe_rewrite":null}}`)
	if err != nil || output.Changes.SafeRewrite != nil || output.Changes.ForbiddenWording != nil || output.Changes.FactValue != "" {
		t.Fatalf("omitted/null fields invented: %#v err=%v", output, err)
	}
}

func TestLiveStrategyIntentNormalizationRejectsUnsafeShapes(t *testing.T) {
	for name, raw := range map[string]string{
		"object quantity":   `{"changes":{"quantity":{"value":1}}}`,
		"boolean quantity":  `{"changes":{"quantity":true}}`,
		"array quantity":    `{"changes":{"quantity":[1]}}`,
		"mixed audience":    `{"changes":{"audience":["家庭",3]}}`,
		"null audience row": `{"changes":{"audience":[null]}}`,
		"object audience":   `{"changes":{"audience":{"text":"家庭"}}}`,
		"nontext name":      `{"changes":{"product_name":123}}`,
		"role string":       `{"changes":{"room_roles":"traffic"}}`,
		"priority string":   `{"changes":{"attribute_display_priority":"2"}}`,
		"numeric link":      `{"target":{"link_key":1}}`,
		"string ID":         `{"target":{"plan_id":"7"}}`,
		"string confidence": `{"confidence":"0.95"}`,
		"unknown change":    `{"changes":{"execute":true}}`,
		"unknown envelope":  `{"execute":true}`,
		"truncated":         `{"changes":{"quantity":1`,
		"extra JSON":        `{"changes":{"quantity":1}} {"kind":"command"}`,
	} {
		t.Run(name, func(t *testing.T) {
			output, err := decodeLiveStrategyIntentModelOutput(raw)
			if !errors.Is(err, errLiveStrategyIntentModelResponse) || output.Kind != "" || output.Intent != "" || output.Changes.Quantity != "" {
				t.Fatalf("invalid shape leaked a partial intent: %#v err=%v", output, err)
			}
		})
	}
}

func TestLiveStrategyIntentModelFailureClassification(t *testing.T) {
	program := agentunderstanding.UnifiedIntent{Kind: "clarify", Intent: "unknown"}
	_, parseErr := decodeLiveStrategyIntentModelOutput(`{"changes":{"quantity":{}}}`)
	parseFailure := liveStrategyIntentModelFallback(program, "system", parseErr)
	if parseFailure.Code != "model_response_invalid" || !strings.Contains(parseFailure.Reply, "已返回结果") ||
		strings.Contains(parseFailure.Reply, "暂时不可用") || strings.Contains(parseFailure.Reply, "目标和字段") ||
		parseFailure.Kind != "clarify" || parseFailure.State != agentStateResponded {
		t.Fatalf("misleading parse failure: %#v", parseFailure)
	}
	callFailure := liveStrategyIntentModelFallback(program, "system", errors.New("private upstream diagnostics"))
	if callFailure.Code != "model_call_failed" || strings.Contains(callFailure.Reply, "private") || callFailure.Engine != "program_fallback" {
		t.Fatalf("call failure confused with parsing or leaked diagnostics: %#v", callFailure)
	}
	program.Reply = "请明确是哪个商品。"
	if preserved := liveStrategyIntentModelFallback(program, "system", parseErr); preserved.Reply != program.Reply {
		t.Fatalf("existing program clarification overwritten: %#v", preserved)
	}
}
