package httpapi

import (
	"strings"
	"testing"
)

func TestAnchorStyleWorkbenchWheelDoesNotRepeatCombination(t *testing.T) {
	points := []anchorStyleWorkbenchPoint{
		{Key: "smell", Label: "香味", Value: "菜籽香", Angles: []string{"冷吃", "热炒"}},
		{Key: "spec", Label: "规格", Value: "5L", Angles: []string{"家庭用量", "选择"}},
	}
	state := anchorStyleWorkbenchState{}
	seen := map[string]bool{}
	for range 4 {
		task := nextAnchorStyleWorkbenchTask(points, state)
		combo := task.SellingPointKey + "×" + task.Angle
		if seen[combo] {
			t.Fatalf("combination repeated before wheel was exhausted: %s", combo)
		}
		seen[combo] = true
		state.Round++
		state.UsedCombinations = append(state.UsedCombinations, combo)
	}
}

func TestAnchorStyleAnalysisPromptLoadsReplaceableMarkdown(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_ANALYSIS_PROMPT_PATH", "")
	prompt, source, err := loadAnchorStyleAnalysisPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.ReplaceAll(strings.Split(source, "#sha256=")[0], "\\", "/"), "/configs/prompts/anchor-style/style-meta-prompt-extraction.md") {
		t.Fatalf("unexpected style extraction template: %s", source)
	}
	for _, required := range []string{"提取腔调五维", "判断情绪", "区分风格层 vs 内容层", "全局规则"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt %s is missing %q", source, required)
		}
	}
}

func TestAnchorStyleAnalysisRequestInjectsRealSampleAfterMarkdown(t *testing.T) {
	request, err := renderAnchorStyleAnalysisRequest("基础方法", "prompt.md", "带货直播", "这是真实主播话术啊")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"基础方法", "prompt.md", "带货直播", "这是真实主播话术啊", "meta_prompt_md", "monitoring"} {
		if !strings.Contains(request, required) {
			t.Fatalf("rendered request is missing %q: %s", required, request)
		}
	}
}

func TestAnchorStyleWorkbenchPromptKeepsStyleAndFactsSeparated(t *testing.T) {
	system, user := renderAnchorStyleWorkbenchStepPrompt(
		"# 风格\n多用哥哥姐姐们",
		"# 正式事实包\n- 价格=79块9",
		anchorStyleWorkbenchTask{Round: 1, SellingPointLabel: "规格", Angle: "家庭用量", MainlinePhase: "body", BodyFunction: "product", CTAIntensity: "light", Callout: true},
		anchorStyleWorkbenchState{}, nil, 260,
	)
	if !strings.Contains(system, "元提示词只决定怎么说") || !strings.Contains(system, "正式事实包只决定说什么") {
		t.Fatalf("system prompt does not preserve the architecture boundary: %s", system)
	}
	if !strings.Contains(user, "# 已确认的主播风格元提示词") || !strings.Contains(user, "# 正式事实包") || !strings.Contains(user, "卖点：规格") {
		t.Fatalf("user prompt is missing a required layer: %s", user)
	}
}

func TestAnchorStyleWorkbenchDirectPromptAddsNothingElse(t *testing.T) {
	meta := "【口语风格】\n只按样本说话"
	facts := "# 正式事实包\n- 1号链接=菜籽油"
	got := renderAnchorStyleWorkbenchDirectPrompt(meta, facts)
	want := meta + "\n\n" + facts
	if got != want {
		t.Fatalf("direct prompt must be exact meta + facts only\nwant=%q\n got=%q", want, got)
	}
}

func TestNormalizeAnchorStyleSellingPointWheelJSON(t *testing.T) {
	raw := `{"卖点列表":[{"卖点名":"老坊背书","角度":["39年老油坊","河清镇自产自销","39年老油坊"]},{"卖点名":"当季新油","角度":["二六年新黄菜油"]}]}`
	canonical, points, combinations, err := normalizeAnchorStyleSellingPointWheelJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if points != 2 || combinations != 3 {
		t.Fatalf("unexpected counts: points=%d combinations=%d", points, combinations)
	}
	if strings.Count(canonical, "39年老油坊") != 1 {
		t.Fatalf("duplicate angle was not removed: %s", canonical)
	}
}

func TestNormalizeAnchorStyleSellingPointWheelJSONRejectsExtraFields(t *testing.T) {
	_, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(`{"卖点列表":[{"卖点名":"老坊背书","角度":["39年老油坊"],"说明":"不允许"}]}`)
	if err == nil {
		t.Fatal("expected extra field validation error")
	}
}

func TestSellingPointExtractionPromptInjectsFactPackage(t *testing.T) {
	prompt, source, err := loadSellingPointExtractionPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "{{FACT_PACKAGE}}") || !strings.Contains(source, "qwen-selling-point-angle-extraction.md") {
		t.Fatalf("unexpected selling-point prompt asset: %s", source)
	}
	rendered := renderSellingPointExtractionRequest(prompt, "# 正式事实包\n- 5L")
	if strings.Contains(rendered, "{{FACT_PACKAGE}}") || !strings.Contains(rendered, "- 5L") {
		t.Fatalf("fact package was not injected: %s", rendered)
	}
}

func TestFlattenAnchorStyleWheelTasksInterleavesSellingPoints(t *testing.T) {
	wheel := anchorStyleSellingPointWheel{Items: []anchorStyleSellingPointWheelItem{
		{Name: "老坊背书", Angles: []string{"老坊1", "老坊2"}},
		{Name: "当季新油", Angles: []string{"新油1", "新油2"}},
	}}
	tasks := flattenAnchorStyleWheelTasks(wheel)
	want := []string{"老坊背书×老坊1", "当季新油×新油1", "老坊背书×老坊2", "当季新油×新油2"}
	if len(tasks) != len(want) {
		t.Fatalf("unexpected task count: %d", len(tasks))
	}
	for index, key := range want {
		if tasks[index].Key != key {
			t.Fatalf("task %d = %q, want %q", index, tasks[index].Key, key)
		}
	}
}

func TestRenderAnchorStyleWheelTestPromptIsMinimalAndBounded(t *testing.T) {
	prompt := renderAnchorStyleWheelTestPrompt("风格元提示词", "正式事实包", anchorStyleWheelTask{SellingPoint: "健康品质", Angle: "低芥酸"}, 300)
	for _, required := range []string{"风格元提示词", "正式事实包", "【健康品质】", "【低芥酸】", "最多300个中文字符"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing %q in prompt: %s", required, prompt)
		}
	}
}

func TestRenderAnchorStyleWheelTestPromptUnlimitedOmitsCharacterLimit(t *testing.T) {
	prompt := renderAnchorStyleWheelTestPrompt("风格元提示词", "正式事实包", anchorStyleWheelTask{SellingPoint: "健康品质", Angle: "低芥酸"}, 0)
	if strings.Contains(prompt, "最多") || strings.Contains(prompt, "字数") {
		t.Fatalf("unlimited prompt contains a character limit: %s", prompt)
	}
}

func TestAnchorStyleWorkbenchFeedbackIsCapped(t *testing.T) {
	stats := anchorStyleWorkbenchStats{WindowChars: 1000, Forbidden: []string{"朋友"}, Metrics: []anchorStyleWorkbenchMetric{
		{Text: "哥哥姐姐们", Status: "low"},
		{Text: "我们家", Status: "low"},
		{Text: "啊", Status: "low"},
		{Text: "哟", Status: "high"},
	}}
	feedback := feedbackAnchorStyleWorkbench(stats)
	if len(feedback) != 3 {
		t.Fatalf("feedback must be rate limited to three corrections, got %d: %#v", len(feedback), feedback)
	}
	if !strings.Contains(strings.Join(feedback, " "), "不要扎堆") {
		t.Fatalf("density feedback must explicitly prevent clustering: %#v", feedback)
	}
}

func TestAnchorStyleWorkbenchPostprocessShowsChanges(t *testing.T) {
	monitor := anchorStyleWorkbenchMonitor{Terms: []anchorStyleWorkbenchTerm{
		{Kind: "audience_address", Text: "哥哥姐姐们", SamplePer1000: 4},
		{Kind: "particle", Text: "啊", SamplePer1000: 12},
	}, ForbiddenTerms: []string{"朋友"}}
	output, changes := postprocessAnchorStyleWorkbench("朋友，先看规格。再看用量。", monitor, 260)
	if strings.Contains(output, "朋友") || !strings.Contains(output, "哥哥姐姐们") || !strings.Contains(output, "啊") || !strings.Contains(output, "！") {
		t.Fatalf("unexpected postprocessed output: %s", output)
	}
	if len(changes) < 2 {
		t.Fatalf("postprocess must expose every correction, got %#v", changes)
	}
}
