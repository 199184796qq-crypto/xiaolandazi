package httpapi

import (
	"livecompanion/management/internal/anchorstylescore"
	"strings"
	"testing"
)

func TestRollingFeedbackModelResponseValidation(t *testing.T) {
	issues := []anchorstylescore.RollingIssue{{Category: "opening"}}
	valid := `{"corrections":[{"category":"opening","instruction":"下一段直接接话，老乡可放在句中，不要扎堆补语气词。"}],"selected_addresses":["老乡"]}`
	text, selected, err := normalizeRollingFeedback(valid, []string{"老乡"}, issues)
	if err != nil || text == "" || len(selected) != 1 {
		t.Fatalf("valid response: %v", err)
	}
	for _, raw := range []string{
		`{"corrections":[{"category":"opening","instruction":"用家人们开头"}],"selected_addresses":["家人们"]}`,
		`bad`,
	} {
		if _, _, err := normalizeRollingFeedback(raw, []string{"老乡"}, issues); err == nil {
			t.Fatalf("invalid response accepted: %s", raw)
		}
	}
}

func TestRollingMissingDuplicateAndExtraCategoriesDoNotStopRun(t *testing.T) {
	issues := []anchorstylescore.RollingIssue{{Category: "opening", Actual: "哥哥姐姐们：3/5"}, {Category: "style", Actual: "自指：偏少"}, {Category: "style", Actual: "啊：偏多"}}
	for _, raw := range []string{
		`{"corrections":[{"category":"开头轮换","instruction":"下一段直接接话。"}],"selected_addresses":[]}`,
		`{"corrections":[{"category":"opening","instruction":"下一段直接接话。"},{"category":"opening","instruction":"另一句重复建议。"},{"category":"ending","instruction":"不应触发的结尾。"}],"selected_addresses":[]}`,
		`{"corrections":[{"category":"opening","instruction":"大幅减少称呼"},{"category":"style","instruction":""}],"selected_addresses":[]}`,
		`{"corrections":[{"category":"opening","instruction":"` + strings.Repeat("啊", 111) + `"}],"selected_addresses":[]}`,
	} {
		before := raw
		prompt, _, warnings, sources, err := normalizeRollingFeedbackDetailed(raw, nil, issues)
		if err != nil || prompt == "" || len(warnings) == 0 || len(sources) != 2 || sources["style"] != "program_fallback" || strings.Contains(prompt, "不应触发") || strings.Contains(prompt, "大幅") || raw != before {
			t.Fatalf("bad repair: %q %v %v %v", prompt, warnings, sources, err)
		}
	}
}

func TestRollingFallbackCannotAddAbsentCategoryOrChooseAddress(t *testing.T) {
	prompt, addresses, _, sources, err := normalizeRollingFeedbackDetailed(`{"corrections":[],"selected_addresses":[]}`, []string{"老乡"}, []anchorstylescore.RollingIssue{{Category: "style", Actual: "自指：偏少"}})
	if err != nil || len(sources) != 1 || sources["style"] != "program_fallback" || len(addresses) != 0 || strings.Contains(prompt, "老乡") {
		t.Fatalf("fallback overstepped: %q %v %v", prompt, sources, err)
	}
	if got := rollingExpectedCategories([]anchorstylescore.RollingIssue{{Category: "style"}, {Category: "style"}, {Category: "opening"}}); strings.Join(got, ",") != "opening,style" {
		t.Fatal(got)
	}
}

func TestRollingFeedbackCombinesOnlyTriggeredCategories(t *testing.T) {
	issues := []anchorstylescore.RollingIssue{{Category: "opening"}, {Category: "style"}, {Category: "style"}}
	raw := `{"corrections":[{"category":"opening","instruction":"下一段换起头。"},{"category":"style","instruction":"自指在句中自然穿插。"}],"selected_addresses":[]}`
	prompt, _, err := normalizeRollingFeedback(raw, nil, issues)
	if err != nil || prompt != "下一段换起头。\n自指在句中自然穿插。" {
		t.Fatalf("combined correction: %q %v", prompt, err)
	}
}

func TestRollingActualFailedResponsePreservesBothStyleInstructions(t *testing.T) {
	raw := `{"corrections":[{"category":"opening","instruction":"避开刚用过的'老乡'，改用'新粉丝们'或'叔叔阿姨们'起头，打破相邻重复。"},{"category":"style","instruction":"最新段'啊'扎堆，下一段减少'啊'的使用，恢复自然分布，不要刻意补偿。"},{"category":"style","instruction":"当前主力称呼密度偏低，下一段在句中多穿插'哥哥姐姐们'或'哥哥姐们'，提升密度。"}],"selected_addresses":["新粉丝们","哥哥姐姐们"]}`
	before := raw
	issues := []anchorstylescore.RollingIssue{{Category: "opening", Key: "opening_rotation"}, {Category: "style", Key: "style_burst"}, {Category: "style", Key: "style_density"}}
	prompt, addresses, warnings, sources, err := normalizeRollingFeedbackDetailed(raw, []string{"老乡", "新粉丝们", "叔叔阿姨们", "哥哥姐姐们", "哥哥姐们"}, issues)
	if err != nil || len(addresses) != 2 || len(warnings) != 1 || sources["opening"] != "model" || sources["style"] != "model" || raw != before {
		t.Fatalf("actual failed response not repaired: %q %v %v %v", prompt, warnings, sources, err)
	}
	for _, text := range []string{"避开刚用过的'老乡'", "下一段减少'啊'的使用", "下一段在句中多穿插'哥哥姐姐们'"} {
		if !strings.Contains(prompt, text) {
			t.Fatalf("lost correction %q in %q", text, prompt)
		}
	}
}

func TestRollingSameInstructionDeduplicatedWithoutLosingAnother(t *testing.T) {
	raw := `{"corrections":[{"category":"style","instruction":"语气词轻微减少。"},{"category":"风格校正","instruction":"语气词轻微减少。"},{"category":"style_density","instruction":"称呼放在句中。"}],"selected_addresses":[]}`
	prompt, _, _, sources, err := normalizeRollingFeedbackDetailed(raw, nil, []anchorstylescore.RollingIssue{{Category: "style"}})
	if err != nil || prompt != "语气词轻微减少。\n称呼放在句中。" || sources["style"] != "model" {
		t.Fatalf("deduplication failed: %q %v %v", prompt, sources, err)
	}
}

func TestRollingMergeOverflowUsesCategoryFallback(t *testing.T) {
	raw := `{"corrections":[{"category":"style","instruction":"` + strings.Repeat("甲", 110) + `"},{"category":"style","instruction":"` + strings.Repeat("乙", 110) + `"},{"category":"style","instruction":"` + strings.Repeat("丙", 110) + `"},{"category":"style","instruction":"稍微调整。"}],"selected_addresses":[]}`
	prompt, _, warnings, sources, err := normalizeRollingFeedbackDetailed(raw, nil, []anchorstylescore.RollingIssue{{Category: "style", Actual: "称呼：偏少"}})
	if err != nil || sources["style"] != "program_fallback" || !strings.Contains(prompt, "称呼") || strings.Contains(prompt, "甲") || strings.Contains(prompt, "稍微调整") || len(warnings) == 0 {
		t.Fatalf("merge overflow failed: %q %v %v %v", prompt, warnings, sources, err)
	}
}

func TestFlashSmokeResponsePassesSameRuntimeValidator(t *testing.T) {
	raw := `{"corrections":[{"category":"opening","instruction":"换用'新粉丝们'或'老乡'起头，避免与上一段重复。"},{"category":"style","instruction":"增加'我们家'的使用频次至每百字约1次，增强亲切感。"}],"selected_addresses":["新粉丝们","老乡"]}`
	prompt, addresses, err := normalizeRollingFeedback(raw, []string{"新粉丝们", "老乡"}, []anchorstylescore.RollingIssue{{Category: "opening"}, {Category: "style"}})
	if err != nil || len(addresses) != 2 || !strings.Contains(prompt, "\n") {
		t.Fatalf("live response rejected: %q %v", prompt, err)
	}
}

func TestRollingFeedbackTemplateIsProjectOwned(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_ROLLING_FEEDBACK_PROMPT_PATH", "")
	md, source, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ROLLING_FEEDBACK_PROMPT_PATH", "configs/prompts/anchor-style/rolling-feedback-generation.md", "三项回灌")
	if err != nil || !strings.Contains(source, "configs") || !strings.Contains(md, "report.allowed_addresses") || !strings.Contains(md, "不重新评分") {
		t.Fatalf("template unavailable: %s %v", source, err)
	}
}
