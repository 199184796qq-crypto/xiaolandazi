package decisionexecutor

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
)

// Explicit text-only replay. No TTS, queue, plan, session or playback mutations.
func TestReturnBridgePromptComparisonProbe(t *testing.T) {
	if os.Getenv("BRIDGE_EXACT_COMPARISON_PROBE") != "1" {
		t.Skip("explicit paid text probe opt-in required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w := New(store, nil, agentgateway.NewFromEnv().WithSpeechModels(store), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	profile, err := w.bridgeModel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var results []map[string]any
	cases := []struct{ name, before, reply, entry, later string }{
		{"live_half_duck", "刚才在说发货安排。", "一般是当天发，超过下午6点就第2天上午发。我们使用京东物流。", "怕买整只吃不完浪费，这个半只装刚好。", "2号链接是450g半只装。可以按实际食量选择。"},
		{"live_brand_question", "刚才讲到绸都杨鸭子的来历。", "感谢这位家人的点赞支持。咱们家绸都杨鸭子在红花街一号的老店，实打实开了几十年了。现在对外都说我们有七十年的老字号底子，这份底气不是吹出来的，是几十年来街坊邻居吃出来的口碑。", "没听过的牌子，到底靠不靠谱？会不会是那种开两天就关门的临时新店啊？", "咱们家绸都杨鸭子在红花街一号的老店，实打实开了几十年了。"},
	}
	for _, name := range []string{"interaction-return-bridge-baseline-probe.md", "interaction-return-bridge-generation.md"} {
		template, err := interactionpolicy.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			var plan returnBridgePlan
			plan.BeforeText, plan.ReplyText = c.before, c.reply
			json.Unmarshal([]byte(`[{"id":"return1","text":`+quoted(c.entry)+`},{"id":"return2","text":`+quoted(c.later)+`}]`), &plan.Units)
			input, _ := json.Marshal(map[string]any{"切点前完整上下文": plan.BeforeText, "核心回答": plan.ReplyText, "实际回归及后续完整内容": plan.Units, "已跳过完整重复单元": []string{}, "已采用用户纠正": ""})
			for trial := 1; trial <= 4; trial++ {
				messages := []agentgateway.Message{{Role: "system", Content: string(template)}, {Role: "user", Content: string(input)}}
				began := time.Now()
				response, callErr := w.agent.Complete(ctx, agentgateway.Request{Stage: "interaction_bridge", Provider: "anchor:" + profile.ID, Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 128, EnableThinking: false, Timeout: 4 * time.Second})
				r := map[string]any{"template": name, "case": c.name, "trial": trial, "model": profile.Model, "messages": messages, "raw": response.Text, "input_tokens": response.InputTokens, "output_tokens": response.OutputTokens, "latency_ms": time.Since(began).Milliseconds()}
				if callErr != nil {
					r["error"] = callErr.Error()
				} else {
					bridge, e := validateBridge(response.Text, plan)
					if e != nil {
						r["error"] = e.Error()
					} else {
						r["type"], r["text"] = bridge.Type, bridge.Text
						r["exact_adjacent_duplicate"] = contentstrategy.BridgeDuplicatesAdjacent(bridge.Text, plan.ReplyText, c.entry, false)
						// Diagnostic only; this comparison never filters live output.
						copy := strings.TrimPrefix(bridge.Text, "那")
						r["copies_return_opening"] = copy != "" && strings.HasPrefix(c.entry, copy)
					}
				}
				results = append(results, r)
			}
		}
	}
	path := os.Getenv("BRIDGE_EXACT_COMPARISON_OUTPUT")
	if path == "" {
		t.Fatal("probe result path required")
	}
	raw, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("text-only comparison model=%s requests=%d", profile.Model, len(results))
}

func quoted(s string) string { raw, _ := json.Marshal(s); return string(raw) }
