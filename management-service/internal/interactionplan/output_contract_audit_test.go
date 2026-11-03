package interactionplan

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/planprompt"
)

func unifiedBlock(id string) string {
	return "编号：" + id + "\n意图：咨询发货｜置信度：85｜建议：按事实答复\n意图：咨询收货｜置信度：35｜建议：不承诺到货\n类型：提问\n优先程度：普通\n优先原因：普通咨询\n歧义：否\n候选：\n澄清句：\n话术要求：简短说明\n话术：按当前方案安排发货。\n结束：" + id
}

func TestAuditCompiledInteractionUsesOneRuntimeContractAndUnifiedBody(t *testing.T) {
	s := fixture(t)
	s.Prompt = (planprompt.Sections{FactsMD: "按当前方案发货。", InteractionRequirementsMD: "只回答当前问题"}).Interaction()
	got, err := GenerateBatch(context.Background(), s, batchFixture(2), func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		prompt := req.Messages[0].Content
		if strings.Count(prompt, "【输出格式") != 1 || strings.Contains(prompt, "回答：可直接口播") {
			t.Error("conflicting contracts in actual request")
		}
		if !strings.Contains(prompt, s.P0) {
			t.Error("complete effective P0 missing from generation request")
		}
		return agentgateway.Response{Text: unifiedBlock("U02") + "\n" + unifiedBlock("U01")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got.Items {
		if item.Err != nil || item.Body != "按当前方案安排发货。" || item.Intent.Meaning != "咨询发货" {
			t.Errorf("canonical multi-intent result lost: %+v", item)
		}
		if len(item.Intent.Alternatives) != 2 || item.Intent.Alternatives[0].Confidence == nil || *item.Intent.Alternatives[0].Confidence != 85 || item.Intent.Requirements != "简短说明" {
			t.Error("metadata was overwritten", item)
		}
	}
}

func TestAuditInteractionMalformedBlocksFailLocally(t *testing.T) {
	valid := unifiedBlock("U01")
	for name, raw := range map[string]string{
		"body aliases":         strings.Replace(valid, "结束：U01", "回答：另一正文。\n结束：U01", 1),
		"unknown after body":   strings.Replace(valid, "结束：U01", "解释：这是后台说明\n结束：U01", 1),
		"invalid ambiguity":    strings.Replace(valid, "歧义：否", "歧义：不确定", 1),
		"bad confidence":       strings.Replace(valid, "置信度：85", "置信度：101", 1),
		"four intents":         strings.Replace(valid, "类型：", "意图：丙｜置信度：20｜建议：忽略\n意图：丁｜置信度：10｜建议：忽略\n类型：", 1),
		"body before metadata": strings.Replace(valid, "话术要求：简短说明\n话术：按当前方案安排发货。", "话术：按当前方案安排发货。\n话术要求：简短说明", 1),
	} {
		t.Run(name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(2), raw+"\n"+unifiedBlock("U02"))
			if got.Items[0].Err == nil || got.Items[0].Body != "" || got.Items[1].Err != nil {
				t.Fatalf("bad person must fail without losing good person: %+v", got)
			}
		})
	}
}

func TestAuditUnifiedClarificationAndLegacyAnswer(t *testing.T) {
	base := "意图：核对需求｜置信度：90｜建议：澄清\n歧义：是\n候选：配送地区、订单操作\n澄清句：您指配送地区，还是订单操作？\n话术要求：只问一句\n话术："
	got := parseBatch(fixture(t), batchFixture(1), base)
	if got.Items[0].Err != nil || got.Items[0].Body != "您指配送地区，还是订单操作？" {
		t.Fatal(got)
	}
	for _, bad := range []string{base + "多余回答。", strings.Replace(base, "配送地区、订单操作", "", 1)} {
		if result := parseBatch(fixture(t), batchFixture(1), bad); result.Items[0].Err == nil {
			t.Fatal("invalid clarification accepted", result)
		}
	}
	legacy := parseBatch(fixture(t), batchFixture(1), "意图：咨询\n歧义：否\n回答：旧协议继续使用。")
	if legacy.Items[0].Err != nil || legacy.Items[0].Body != "旧协议继续使用。" {
		t.Fatal(legacy)
	}
}
