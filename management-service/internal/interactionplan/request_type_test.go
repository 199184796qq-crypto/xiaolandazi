package interactionplan

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func TestRequestTypeSingleRetainsActionableReply(t *testing.T) {
	for _, label := range []string{"请求", "要求"} {
		t.Run(label, func(t *testing.T) {
			calls := 0
			body := "可以联系客服备注，具体以客服确认为准。"
			snapshot := fixture(t)
			// Existing saved plans may still contain the previous system format.
			// Runtime must replace that reserved section rather than require customer data rewrites.
			snapshot.Prompt += "\n\n【输出格式（系统保留，不可删除或修改）】\n类型：提问、下单意向、催单、投诉、闲聊、主动互动、刷屏之一"
			intent, got, err := Generate(context.Background(), snapshot, Input{Kind: "semantic", Text: "帮我备注晚点发货", AllowClarify: true}, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
				calls++
				if !strings.Contains(req.Messages[0].Content, "类型：提问、请求、") || strings.Count(req.Messages[0].Content, "类型：") != 1 {
					t.Error("model request omitted the supported request type")
				}
				return agentgateway.Response{Text: "意图：请求备注晚点发货\n类型：" + label + "\n歧义：否\n澄清句：\n话术：" + body}, nil
			})
			if err != nil || calls != 1 || got != body || intent.Type != "请求" || !intent.Relevant || !intent.Question || intent.NeedClarify || intent.Priority != "普通" {
				t.Fatalf("request response changed the normal reply path: calls=%d intent=%+v body=%q err=%v", calls, intent, got, err)
			}
		})
	}
}

func TestRequestTypeKeepsClarificationPermission(t *testing.T) {
	for _, allow := range []bool{true, false} {
		in := Input{Kind: "semantic", Text: "帮我改一下", AllowClarify: allow}
		intent, body, err := Generate(context.Background(), fixture(t), in, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
			return agentgateway.Response{Text: "意图：请求修改但未说明修改内容\n类型：要求\n歧义：是\n话术：\n澄清句：你想修改哪一项？"}, nil
		})
		if !allow {
			if err == nil || body != "" {
				t.Fatal("request bypassed the existing clarification prohibition", intent, body, err)
			}
			continue
		}
		if err != nil || intent.Type != "请求" || !intent.NeedClarify || body != "你想修改哪一项？" {
			t.Fatal("request clarification was not preserved", intent, body, err)
		}
	}
}

func TestRequestTypeBatchKeepsIndependentBodies(t *testing.T) {
	items := batchFixture(2)
	first := "可以联系客服备注，具体以客服确认为准。"
	second := "你想修改哪一项？"
	raw := "编号：U01\n意图：请求备注晚点发货\n类型：请求\n歧义：否\n澄清句：\n话术：" + first + "\n结束：U01\n" +
		"编号：U02\n意图：请求修改内容\n类型：要求\n歧义：是\n话术：\n澄清句：" + second + "\n结束：U02"
	got := parseBatch(fixture(t), items, raw, "stop")
	if len(got.Items) != 2 || got.SpeechGroups != 1 || got.ClarificationGroups != 1 {
		t.Fatal(got)
	}
	for i, item := range got.Items {
		want := first
		if i == 1 {
			want = second
		}
		if item.Err != nil || item.ID != items[i].ID || item.Intent.Type != "请求" || item.Intent.Priority != "普通" || item.Body != want || item.Intent.NeedClarify != (i == 1) {
			t.Fatalf("request batch changed item identity or reply branch: %+v", item)
		}
	}
}
