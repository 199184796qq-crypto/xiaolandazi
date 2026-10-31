package interactionplan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func TestBatchParticipationHasExplicitTypeWithoutSpokenBody(t *testing.T) {
	items := batchFixture(3)
	for i := range items {
		items[i].EngagementToken = "收到"
	}
	items[0].Text = "收到"
	items[1].Text = "准备买礼盒，2号多少钱"
	items[2].Text = "哪里发货"
	got, err := GenerateBatch(context.Background(), fixture(t), items, func(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
		var p map[string]any
		json.Unmarshal([]byte(r.Messages[1].Content), &p)
		if p["engagement_token"] != "收到" || strings.Count(r.Messages[1].Content, `"engagement_token"`) != 1 {
			t.Fatal("active prompt must be shared once", p)
		}
		for _, part := range []string{"主动互动", "下单意向", "kind为dialogue", "不同人问同一句真实问题"} {
			if !strings.Contains(r.Messages[0].Content, part) {
				t.Fatal("missing routing contract", part)
			}
		}
		raw := "编号：U01\n意图：跟随已播口令\n类型：主动互动\n优先程度：紧急\n优先原因：模型随意升级\n歧义：否\n回答：不应逐人口播这句话。\n结束：U01\n" + strings.Replace(blockText("U02", "按商品卡。"), "类型：提问", "类型：下单意向", 1) + blockText("U03", "按保存方案。")
		return agentgateway.Response{Text: raw}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Err != nil || got.Items[0].Intent.Type != "主动互动" || !got.Items[0].Intent.Relevant || got.Items[0].Intent.Question || got.Items[0].Body != "" || got.Items[0].Intent.Priority != "普通" {
		t.Fatal(got)
	}
	if got.Items[1].Intent.Type != "下单意向" || got.Items[2].Intent.Type != "提问" {
		t.Fatal(got)
	}
}

func TestNumericDialogueCannotBeSilentlyDiscardedAsParticipation(t *testing.T) {
	in := Input{Kind: "dialogue", Text: "2", Context: []ContextMessage{{Role: "assistant", Text: "您要一只还是两只？"}}}
	_, _, err := parseSingleContent(fixture(t), in, map[string]string{"意图": "参与口令", "类型": "主动互动", "歧义": "否"})
	if err == nil {
		t.Fatal("numeric clarification silently discarded")
	}
	intent, body, err := parseSingleContent(fixture(t), in, map[string]string{"意图": "要两只", "类型": "下单意向", "歧义": "否", "回答": "明白，按这个数量。"})
	if err != nil || intent.Type != "下单意向" || body == "" {
		t.Fatal(intent, body, err)
	}
	_, _, err = parseSingleContent(fixture(t), Input{Kind: "engagement", Text: "100位参与统计", EventCount: 100}, map[string]string{"意图": "主动互动", "类型": "主动互动", "歧义": "否"})
	if err == nil {
		t.Fatal("aggregate announcement silently discarded as an individual vote")
	}
	_, body, err = parseSingleContent(fixture(t), Input{Kind: "engagement", Text: "100位参与统计", EventCount: 100}, map[string]string{"意图": "回应统计", "类型": "闲聊", "歧义": "否", "回答": "看到大家的参与啦。"})
	if err != nil || body == "" {
		t.Fatal(body, err)
	}
}

func TestBatchDifferentPromptContextRejectedBeforeModel(t *testing.T) {
	items := batchFixture(2)
	items[0].EngagementToken = "1"
	items[1].EngagementToken = "2"
	calls := 0
	_, err := GenerateBatch(context.Background(), fixture(t), items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{}, nil
	})
	if err == nil || calls != 0 {
		t.Fatal(err, calls)
	}
}
