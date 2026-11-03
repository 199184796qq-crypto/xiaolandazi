package planprompt

import (
	"strings"
	"testing"
)

func TestRewritePlanningEnvelopePreservesRuntimePlaceholders(t *testing.T) {
	sections := Sections{PersonaMD: "  本店主播  ", FactsMD: "八十七元六角", RedLinesMD: "不编造承诺", SpeechRequirementsMD: "主线专用", InteractionRequirementsMD: "互动专用"}
	planning := "  只保存规则正文\r\n{{已有开场}}\n{{开场方式}}\n〔提示〕保留原文😀\n  "
	prompt := CompileRewritePlanning(sections, planning)
	want := RewriteIntro + "\n\n【主播人设】\n本店主播\n\n【事实依据】\n八十七元六角\n\n【红线禁止】\n不编造承诺\n\n【样本话术】\n{{样本话术}}\n\n" + strings.TrimSpace(strings.ReplaceAll(planning, "\r\n", "\n")) + "\n\n" + RewriteOutputContract
	if prompt != want {
		t.Fatalf("context, sample placeholder or raw planning changed: %q", prompt)
	}
	if strings.Contains(prompt, "主线专用") || strings.Contains(prompt, "互动专用") {
		t.Fatal("other prompt requirements leaked")
	}
}

func TestLegacyRewritePlanningRecognizesOnlyKnownEnvelope(t *testing.T) {
	sections := Sections{PersonaMD: "人设", FactsMD: "事实", RedLinesMD: "红线"}
	rules := "--------------------------------------------------\n第一步：抽取直播流程骨架\n【事实依据】这个标题属于规则正文\n{{已有开场}}"
	full := CompileRewritePlanning(sections, rules)
	for _, prompt := range []string{full, strings.Replace(full, "{{样本话术}}", "", 1), strings.ReplaceAll(full, "\n", "\r\n")} {
		got, ok := LegacyRewritePlanning(prompt, sections)
		if !ok || got != rules {
			t.Fatalf("legacy planning not recovered: %q %v", got, ok)
		}
	}
	raw := "  自定义规则\r\n【主播人设】仍在本页\r\n  "
	if got, ok := LegacyRewritePlanning(raw, sections); !ok || got != raw {
		t.Fatal("standalone original was altered")
	}
	if _, ok := LegacyRewritePlanning(full, Sections{PersonaMD: "不同人设"}); ok {
		t.Fatal("mismatched full prompt was guessed from headings")
	}
	if _, ok := LegacyRewritePlanning("", sections); ok {
		t.Fatal("absent planning confused with explicitly saved empty original")
	}
}
