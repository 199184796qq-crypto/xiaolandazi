package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/planprompt"
)

func TestAuditRewriteAmbiguousBodiesAndTailNeverSpoken(t *testing.T) {
	base := "话术骨架：先讲重点→自然结束\n一句话上下文：围绕使用场景展开\n话术：这段是可播正文。"
	for _, raw := range []string{base + "\n改写内容：另一段正文。", base + "\n说明：后台解释。", base + "\n话术要求：这句不能播。", base + "\n额外内容。"} {
		if got, err := parseScriptRewriteOutput(raw); err == nil || got.Script != "" {
			t.Errorf("metadata/body alias leaked into speech: %+v %v", got, err)
		}
	}
}

func TestAuditFreshRewriteRequiresMetadataButLegacyReaderRemainsCompatible(t *testing.T) {
	for _, raw := range []string{"旧的纯正文。", "话术骨架：旧骨架\n话术内容：旧正文。", "话术骨架：新骨架\n一句话上下文：简介\n话术：\n正文没有同字段在一行。"} {
		if _, err := parseScriptRewriteResponse(raw); err == nil {
			t.Fatal("new request accepted incomplete contract", raw)
		}
		if _, err := parseScriptRewriteOutput(raw); err != nil {
			t.Fatal("historical reader lost compatibility", raw, err)
		}
	}
}

func TestAuditRewriteContractIsEnforcedAtRequestNotOnlySave(t *testing.T) {
	for _, rules := range []string{"业务规则，样本：{{样本话术}}", "业务规则\n【样本话术】\n{{样本话术}}\n" + planprompt.RewriteOutputContract} {
		got, err := renderScriptRewriteRequest(rules, "测试样本。", "", "", scriptRewriteOutput{Skeleton: "上次结构", Context: "上次角度"})
		if err != nil || strings.Count(got, "【输出格式") != 1 || !strings.HasSuffix(got, planprompt.RewriteOutputContract) || !strings.Contains(got, "测试样本。") || !strings.Contains(got, "上次角度") {
			t.Errorf("missing/duplicate/nonfinal runtime contract: %q %v", got, err)
		}
		messages := scriptRewriteMessages(got)
		if len(messages) != 2 || messages[0].Role != "system" || messages[0].Content != planprompt.RewriteOutputContract || strings.Contains(messages[1].Content, planprompt.RewriteOutputContract) {
			t.Fatal("contract must be system-owned and sent once", messages)
		}
	}
	got, err := renderBackgroundScriptRewritePrompt("业务规则", "补回缺失的样本", "已有开场含{{样本话术}}字面文本", "开场角度", scriptRewriteOutput{})
	if err != nil || !strings.HasSuffix(got, planprompt.RewriteOutputContract) || !strings.Contains(got, "补回缺失的样本") {
		t.Fatal("background supplemental instructions override contract", got, err)
	}
	if !strings.Contains(got, "已有开场含{{样本话术}}字面文本") {
		t.Fatal("runtime data was treated as template", got)
	}
}
