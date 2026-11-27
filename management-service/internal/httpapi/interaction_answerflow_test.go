package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

func TestLiveAnswerReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		test, valid   bool
		want          string
	}{
		{"live answer defaults low", `{"text":"两款价格和物流有什么区别"}`, false, true, "low"},
		{"explicit low", `{"answer_reasoning_effort":"low"}`, false, true, "low"},
		{"test defaults low", `{"text":"两款价格和物流有什么区别"}`, true, true, "low"},
		{"test rejects low", `{"answer_reasoning_effort":"low"}`, true, false, ""},
		{"unknown level rejected", `{"answer_reasoning_effort":"high"}`, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in interactionV2Input
			if err := json.Unmarshal([]byte(tc.payload), &in); err != nil {
				t.Fatal(err)
			}
			in.Test, in.Kind = tc.test, "semantic"
			if err := validateAnswerReasoningEffort([]interactionV2Input{in}); (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if !tc.valid {
				return
			}
			if got := liveAnswerInput(in).AnswerReasoningEffort; got != tc.want {
				t.Fatalf("effort = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestManualAnswerFlowContextKeepsOnlyPriorCommentsAndTime(t *testing.T) {
	now := time.Now()
	turns := []model.InteractionRecentTurn{
		{Question: " 1号和2号有什么区别 ", Answer: "1号整只，2号礼品装", At: now},
		{Question: "1号多少钱", Answer: ""},
		{Question: "1号可以切吗", Answer: "支持切块"},
	}
	got := manualAnswerFlowContext("1号多少钱", turns)
	if len(got) != 2 || got[0].Role != "user" || got[0].Text != "1号和2号有什么区别" || !got[0].At.Equal(now) || got[1].Text != "1号可以切吗" {
		t.Fatal(got)
	}
}

func TestQuestionRoutingUsesTaskKindRatherThanPlanSettingOrEngagement(t *testing.T) {
	for _, kind := range []string{"semantic", "dialogue", "urgent"} {
		if !interactionQuestionUsesAnswerFlow(interactionV2Input{Kind: kind, EngagementToken: "活动口令"}) {
			t.Fatal("question did not use answer flow", kind)
		}
	}
	for _, kind := range []string{"welcome", "welcome_batch", "like", "follow", "order", "engagement", "warmup", "guide", "time_report"} {
		if interactionQuestionUsesAnswerFlow(interactionV2Input{Kind: kind}) {
			t.Fatal("behavior used question flow", kind)
		}
	}
	if interactionQuestionUsesAnswerFlow(interactionV2Input{Kind: "semantic", Template: "like_v1"}) ||
		interactionQuestionUsesAnswerFlow(interactionV2Input{Kind: "semantic", Test: true, PreviewInstruction: "预览改法"}) {
		t.Fatal("dedicated templates and instruction previews must keep their own generator")
	}
}

func TestLiveQAUsesFrozenP0AndOnlyLowThinkingForFallbackAnswer(t *testing.T) {
	for _, saved := range []string{"一般次日上午发货。", "保证百分百有效。"} {
		t.Run(saved, func(t *testing.T) {
			inputs := httpBatchFixture(1)
			items, err := batchInputsV2("batch", inputs, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			lib := danmakuLibrary{QARecords: []qapair.Record{{
				Entry: qapair.Entry{ID: "known", Question: "什么时候发货", Answer: saved, Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}},
				Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{{1, 0}},
			}}}
			embeds, answers := 0, 0
			var stages interactionplan.ModelStages
			service := newDanmakuAnswerService(lib, danmakuFakeEmbed{calls: &embeds}, nil)
			snapshot := httpBatchSnapshot(t)
			service.ValidateQA = func(answer string) error { return interactionplan.Validate(snapshot, answer) }
			service.Complete = func(_ context.Context, stage answerflow.Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
				request := liveAnswerFlowRequest(stage, messages, 1, "low")
				if request.Stage != "interaction_answerflow_intent" || request.EnableThinking || request.ReasoningEffort != "" || request.ResponseFormat != agentgateway.ResponseJSON {
					t.Fatal(request)
				}
				stages.Record(request.Stage, agentgateway.Response{ThinkingMode: "off"}, 120*time.Millisecond)
				return agentgateway.Response{Text: `{"meaning":"询问发货时间","category":"问题","asks":[{"text":"什么时候发货"}]}`}, nil
			}
			service.CompleteAnswer = func(_ context.Context, effort string, messages []agentgateway.Message) (agentgateway.Response, error) {
				answers++
				request := liveAnswerFlowRequest(answerflow.AnswerStage, messages, 1, effort)
				if request.ReasoningEffort != "low" || request.ResponseFormat != agentgateway.ResponseText || !strings.Contains(messages[1].Content, "询问发货时间") ||
					strings.Contains(messages[1].Content, "保证百分百有效") {
					t.Fatal(request)
				}
				return agentgateway.Response{Text: "意图：询问发货时间\n类型：提问\n歧义：否\n澄清句：\n话术：发货时间还需确认。"}, nil
			}
			out, _, err := preparedAnswerFlowResultsV2(t.Context(), snapshot, inputs, items, service, "")
			if err != nil || embeds != 1 || stages.Intent == nil || stages.Intent.Thinking != "off" || stages.Intent.ElapsedMS != 120 || stages.Answer != nil {
				t.Fatal(out, err, embeds, stages)
			}
			if saved == "一般次日上午发货。" {
				if out[0].Envelope.Body != saved || answers != 0 || out[0].Parse.Reason != "qa_pair_match" {
					t.Fatal(out, answers)
				}
			} else if out[0].Envelope.Body != "发货时间还需确认。" || answers != 1 {
				t.Fatal(out, answers)
			}
		})
	}
}

func TestNewAnswerFlowBindsOnlySelectedBodyAndKeepsOwnersContext(t *testing.T) {
	for _, n := range []int{1, 2, 10} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			inputs := httpBatchFixture(n)
			for i := range inputs {
				inputs[i].Recent = []interactionplan.Turn{{Question: fmt.Sprintf("history-%d", i), Answer: fmt.Sprintf("answer-%d", i)}}
				inputs[i].Context = []interactionplan.ContextMessage{{Role: "user", Text: fmt.Sprintf("context-%d", i)}}
				inputs[i].CurrentMainline = "共享现场背景"
			}
			items, err := batchInputsV2("batch", inputs, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			s := liveFlowFixture(func(ctx context.Context, stage string, m []agentgateway.Message) (agentgateway.Response, error) {
				calls++
				if strings.Contains(m[1].Content, "program-task-") || strings.Contains(m[1].Content, "tenant_id") {
					t.Fatal("program identity entered model")
				}
				if stage == "danmaku_test_batch_intent" {
					var input struct {
						Items []struct {
							ID    string          `json:"item_id"`
							Input json.RawMessage `json:"input"`
						}
					}
					if err := json.Unmarshal([]byte(m[1].Content), &input); err != nil || len(input.Items) != n {
						t.Fatal(input, err)
					}
					result := []map[string]any{}
					for i := n - 1; i >= 0; i-- {
						own := string(input.Items[i].Input)
						for j := range inputs {
							if j != i && strings.Contains(own, fmt.Sprintf("history-%d", j)) {
								t.Fatal("cross-person history", own)
							}
						}
						priority := "normal"
						if i == n-1 {
							priority = "high"
						}
						result = append(result, map[string]any{"item_id": items[i].ID, "relevant": true, "priority": priority, "reason": "及时回复", "intent": map[string]any{"meaning": "发货咨询", "category": "问题", "speech_act": "提问", "asks": []any{map[string]any{"text": "咨询发货"}}}})
					}
					raw, _ := json.Marshal(map[string]any{"items": result})
					return agentgateway.Response{Text: string(raw)}, nil
				}
				if stage == "danmaku_test_intent" {
					if n != 1 {
						t.Fatal("repeated selected intent")
					}
					return agentgateway.Response{Text: `{"meaning":"发货咨询","category":"问题","speech_act":"提问","asks":[{"text":"咨询发货"}]}`}, nil
				}
				if stage != "danmaku_test_answer" || !strings.Contains(m[1].Content, fmt.Sprintf("history-%d", n-1)) {
					t.Fatal("wrong selected context", stage, m)
				}
				for i := 0; i < n-1; i++ {
					if strings.Contains(m[1].Content, fmt.Sprintf("history-%d", i)) {
						t.Fatal("other history in answer", m)
					}
				}
				deadline, ok := ctx.Deadline()
				if !ok || !deadline.Equal(inputs[n-1].ExpiresAt) {
					t.Fatal("selected deadline lost")
				}
				return agentgateway.Response{Text: `意图：理解当前诉求
类型：提问
歧义：否
澄清句：
话术：所选问题的回答。`}, nil
			})
			snapshot := httpBatchSnapshot(t)
			out, _, err := preparedAnswerFlowResultsV2(context.Background(), snapshot, inputs, items, s, "")
			if err != nil || len(out) != n || calls != 2 {
				t.Fatal(out, err, calls)
			}
			for i, r := range out {
				if r.Envelope.Task != inputs[i].Task || r.Envelope.Revision != inputs[i].Revision || r.Envelope.Fingerprint != snapshot.Fingerprint || !r.Envelope.ExpiresAt.Equal(inputs[i].ExpiresAt) || r.Envelope.Session != snapshot.Scope.Session {
					t.Fatal("transport binding lost", r)
				}
				if (r.Envelope.Body != "") != (i == n-1) || r.Envelope.Intent.Relevant != (i == n-1) {
					t.Fatal("body assigned to wrong viewer", r)
				}
			}
		})
	}
}

// Use the real HTTP adapter's decoder/prompts while replacing all suppliers.
func liveFlowFixture(complete danmakuComplete) answerflow.Service {
	service := newDanmakuAnswerService(danmakuLibrary{}, nil, complete)
	service.QuestionLimit = 2400
	return service
}

func TestNewAnswerFlowDeadlineStopsBeforeAnyCall(t *testing.T) {
	in := httpBatchFixture(1)
	in[0].ExpiresAt = time.Now().Add(-time.Second)
	items, _ := batchInputsV2("batch", httpBatchFixture(1), time.Now())
	calls := 0
	s := liveFlowFixture(func(context.Context, string, []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{}, nil
	})
	if _, _, err := preparedAnswerFlowResultsV2(context.Background(), httpBatchSnapshot(t), in, items, s, ""); err == nil || calls != 0 {
		t.Fatal(err, calls)
	}
}

func TestNewAnswerInputDoesNotShareSliceStorage(t *testing.T) {
	in := httpBatchFixture(1)[0]
	in.Recent = []interactionplan.Turn{{Question: "原问题", Answer: "原回答"}}
	in.Context = []interactionplan.ContextMessage{{Role: "user", Text: "原对话", At: time.Now()}}
	out := liveAnswerInput(in)
	if out.Question != in.Text || strings.Contains(out.RoomIntro, "program-task") {
		t.Fatal(out)
	}
	if !out.Context[0].At.Equal(in.Context[0].At) {
		t.Fatal("context timestamp lost")
	}
	out.Recent[0].Answer = "修改副本"
	out.Context[0].Text = "修改副本"
	if in.Recent[0].Answer != "原回答" || in.Context[0].Text != "原对话" {
		t.Fatal("shared conversation storage")
	}
}
