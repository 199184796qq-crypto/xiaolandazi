package httpapi

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/planprompt"
)

func TestDanmakuSimulatedHistoryReachesIntentAndAnswer(t *testing.T) {
	const body = `{"question":"那包邮吗？","context":[{"role":"user","text":"我在上海","at":"2026-11-14T10:20:00Z"},{"role":"user","text":"想买这个礼盒","at":"2026-11-14T10:20:01Z"}]}`
	var input danmakuInput
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	if err := validateDanmakuInput(&input, false); err != nil {
		t.Fatal(err)
	}

	var stages []string
	out, err := runDanmakuTest(context.Background(), input, danmakuLibrary{}, nil, func(_ context.Context, stage string, messages []agentgateway.Message) (agentgateway.Response, error) {
		stages = append(stages, stage)
		var payload struct {
			Question         string               `json:"question"`
			CustomerQuestion map[string]string    `json:"customer_question"`
			Context          []answerflow.Message `json:"context"`
			HistoryPolicy    string               `json:"history_policy"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(payload.Context, input.Context) || payload.HistoryPolicy == "" {
			t.Fatalf("%s lost simulated context: %+v", stage, payload.Context)
		}
		if stage == "danmaku_test_intent" {
			if payload.Question != input.Question {
				t.Fatal(payload.Question)
			}
			return agentgateway.Response{Text: `{"meaning":"询问礼盒寄往上海是否包邮","speech_act":"提问","asks":[{"text":"是否包邮"}]}`}, nil
		}
		if payload.CustomerQuestion["original"] != input.Question {
			t.Fatal(payload.CustomerQuestion)
		}
		return agentgateway.Response{Text: "意图：询问包邮\n类型：提问\n歧义：否\n澄清句：\n话术：请以结算页运费为准。", FinishReason: "stop"}, nil
	})
	if err != nil || out.Answer != "请以结算页运费为准。" || !slices.Equal(stages, []string{"danmaku_test_intent", "danmaku_test_answer"}) {
		t.Fatal(out, err, stages)
	}
}

func TestDanmakuAndLiveShareLegacyFiveFieldOutput(t *testing.T) {
	for _, tc := range []struct {
		kind, raw, body string
		clarify         bool
	}{
		{"请求", "意图：请求晚点发货\n类型：请求\n歧义：否\n澄清句：\n话术：可以先确认希望发出的日期。", "可以先确认希望发出的日期。", false},
		{"投诉", "意图：反馈包装破损\n类型：投诉\n歧义：否\n澄清句：\n话术：先别着急，具体情况需要核实。", "先别着急，具体情况需要核实。", false},
		{"下单意向", "意图：准备购买\n类型：下单意向\n歧义：否\n澄清句：\n话术：可以先选适合自己的规格。", "可以先选适合自己的规格。", false},
		{"提问", "意图：询问收货地点\n类型：提问\n歧义：是\n话术：\n澄清句：准备寄到哪里？", "准备寄到哪里？", true},
	} {
		t.Run(tc.kind+tc.body, func(t *testing.T) {
			const intro = "本场介绍商品和到店服务。"
			const wantPrefix = "【主播人设】\n以店员身份发言\n\n【红线禁止】\n不承诺库存\n\n【互动要求】\n只回答当前问题"
			calls := 0
			service := liveFlowFixture(func(_ context.Context, stage string, messages []agentgateway.Message) (agentgateway.Response, error) {
				calls++
				var payload struct {
					RoomIntro string `json:"room_intro"`
				}
				if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil || payload.RoomIntro != intro || strings.Count(messages[1].Content, intro) != 1 || strings.Contains(messages[0].Content, intro) {
					t.Fatalf("%s did not send the intro once as context: %q, %v", stage, payload.RoomIntro, err)
				}
				if stage == "danmaku_test_intent" {
					if strings.Contains(messages[0].Content, wantPrefix) {
						t.Fatal("intent received the answer rules")
					}
					return agentgateway.Response{Text: `{"meaning":"当前诉求","category":"问题","speech_act":"提问","asks":[{"text":"当前问题"}]}`}, nil
				}
				if !strings.HasPrefix(messages[0].Content, wantPrefix+"\n\n") || strings.Count(messages[0].Content, wantPrefix) != 1 {
					t.Fatal("answer did not receive all three saved rules exactly once")
				}
				if !strings.Contains(messages[0].Content, planprompt.InteractionOutputContract) || strings.Contains(messages[0].Content, `"action"`) {
					t.Fatal("new answer prompt differs from live contract")
				}
				return agentgateway.Response{Text: tc.raw, FinishReason: "stop"}, nil
			})
			service.AnswerSystemPrefix = answerPlanBlock("以店员身份发言", "不承诺库存", "只回答当前问题")
			input := httpBatchFixture(1)
			items, err := batchInputsV2("batch", input, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			webInput := liveAnswerInput(input[0])
			webInput.RoomIntro = intro
			web, err := service.Analyze(context.Background(), answerflow.Request{Scope: answerflow.Scope{Tenant: 14, Plan: 7}, Input: webInput})
			if err != nil || web.Answer != tc.body || web.InteractionIntent == nil || web.InteractionIntent.Type != tc.kind || web.InteractionIntent.NeedClarify != tc.clarify || web.AnswerModelOutput != tc.raw {
				t.Fatal(web, err)
			}
			live, _, err := preparedAnswerFlowResultsV2(context.Background(), httpBatchSnapshot(t), input, items, service, intro)
			if err != nil || len(live) != 1 || live[0].Envelope.Body != web.Answer || live[0].Envelope.Intent.Type != tc.kind || live[0].Envelope.Intent.NeedClarify != tc.clarify || live[0].Parse.RawSHA256 != web.AnswerParse.RawSHA256 || calls != 4 {
				t.Fatal(live, err, calls)
			}
			encoded, _ := json.Marshal(live[0].Envelope)
			decoded, ok := interactionplan.Decode(encoded)
			if !ok || decoded.Body != tc.body || strings.Contains(decoded.Body, "类型：") || strings.Contains(decoded.Body, "意图：") {
				t.Fatal("old executor cannot decode new result", decoded)
			}
		})
	}
}

func TestDanmakuLegacyContractRejectsJSONAndTruncatedAnswerWithoutRepurchase(t *testing.T) {
	for _, tc := range []struct{ raw, finish string }{
		{`{"action":"answer","answer":"不该接受的JSON"}`, "stop"},
		{"意图：咨询价格\n类型：提问\n歧义：否\n澄清句：\n话术：未完成的正文", "length"},
	} {
		calls := 0
		service := liveFlowFixture(func(_ context.Context, stage string, _ []agentgateway.Message) (agentgateway.Response, error) {
			calls++
			if stage == "danmaku_test_intent" {
				return agentgateway.Response{Text: `{"meaning":"咨询","asks":[{"text":"咨询"}]}`}, nil
			}
			return agentgateway.Response{Text: tc.raw, FinishReason: tc.finish}, nil
		})
		_, err := service.Analyze(context.Background(), answerflow.Request{Scope: answerflow.Scope{Tenant: 14, Plan: 7}, Input: answerflow.Input{Question: "咨询"}})
		if err == nil || calls != 2 {
			t.Fatal("bad output accepted or retried", err, calls)
		}
	}
}
