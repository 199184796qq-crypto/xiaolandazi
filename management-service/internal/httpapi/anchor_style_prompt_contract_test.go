package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestProjectModeReadsLatestAllMotherFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		load      func() (string, string, error)
	}{
		{anchorStyleAnalysisPromptRelativePath, "ANCHOR_STYLE_ANALYSIS_PROMPT_PATH", loadAnchorStyleAnalysisPrompt},
		{sellingPointExtractionPromptRelativePath, "SELLING_POINT_EXTRACTION_PROMPT_PATH", loadSellingPointExtractionPrompt},
		{sellingPointWheelReviewPromptRelativePath, "SELLING_POINT_WHEEL_REVIEW_PROMPT_PATH", loadSellingPointWheelReviewPrompt},
		{sellingPointWheelExpansionPromptRelativePath, "SELLING_POINT_WHEEL_EXPANSION_PROMPT_PATH", loadSellingPointWheelExpansionPrompt},
		{sellingPointWheelFinalAuditPromptRelativePath, "SELLING_POINT_WHEEL_FINAL_AUDIT_PROMPT_PATH", loadSellingPointWheelFinalAuditPrompt},
		{speechMinerGuidePromptRelativePath, "SPEECH_MINER_GUIDE_PROMPT_PATH", loadSpeechMinerGuidePrompt},
		{factNameSuggestionPromptRelativePath, "FACT_NAME_SUGGESTION_PROMPT_PATH", loadFactNameSuggestionPrompt},
		{"configs/prompts/anchor-style/live-strategy-intent.md", "LIVE_STRATEGY_INTENT_PROMPT_PATH", loadLiveStrategyIntentPrompt},
		{"configs/prompts/anchor-style/cycle-product-focus.md", "ANCHOR_CYCLE_PRODUCT_FOCUS_PROMPT_PATH", loadCycleProductFocusPrompt},
		{"configs/prompts/anchor-style/cycle-product-scope-classification.md", "ANCHOR_CYCLE_PRODUCT_SCOPE_PROMPT_PATH", loadCycleProductScopePrompt},
		{"configs/prompts/anchor-style/cycle-product-scope-repair.md", "ANCHOR_CYCLE_PRODUCT_SCOPE_REPAIR_PROMPT_PATH", func() (string, string, error) {
			return loadReplaceablePromptAsset("ANCHOR_CYCLE_PRODUCT_SCOPE_REPAIR_PROMPT_PATH", "configs/prompts/anchor-style/cycle-product-scope-repair.md", "repair")
		}},
		{"configs/prompts/anchor-style/cycle-product-focus-audit.md", "ANCHOR_CYCLE_PRODUCT_AUDIT_PROMPT_PATH", loadCycleProductAuditPrompt},
		{anchorStyleScorecardPromptRelativePath, "ANCHOR_STYLE_SCORECARD_PROMPT_PATH", loadAnchorStyleScorecardPrompt},
		{anchorStyleReviewPromptRelativePath, "ANCHOR_STYLE_REVIEW_PROMPT_PATH", loadAnchorStyleReviewPrompt},
		{"configs/prompts/anchor-style/cycle-segment-generation.md", "ANCHOR_STYLE_CYCLE_SEGMENT_PROMPT_PATH", func() (string, string, error) {
			return loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_SEGMENT_PROMPT_PATH", "configs/prompts/anchor-style/cycle-segment-generation.md", "cycle")
		}},
	} {
		t.Run(filepath.Base(tc.name), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("ANCHOR_STYLE_PROMPT_DIR", dir)
			t.Setenv(tc.env, "")
			path := filepath.Join(dir, filepath.Base(tc.name))
			one := strings.Repeat("latest-version-one ", 20)
			two := strings.Repeat("latest-version-two ", 20)
			if err := os.WriteFile(path, []byte(one), 0600); err != nil {
				t.Fatal(err)
			}
			got, first, err := resolveRequestPrompt("project", "old-browser-snapshot", tc.load)
			if err != nil || got != strings.TrimSpace(one) || !strings.Contains(first, "#sha256=") {
				t.Fatal(got, first, err)
			}
			if err := os.WriteFile(path, []byte(two), 0600); err != nil {
				t.Fatal(err)
			}
			got, second, err := resolveRequestPrompt("", one, tc.load)
			if err != nil || got != strings.TrimSpace(two) || first == second {
				t.Fatal("cached mother used", got, second, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if _, _, err = tc.load(); err == nil {
				t.Fatal("configured missing mother fell back to old directory")
			}
		})
	}
}

func TestManualPromptMustBeExplicit(t *testing.T) {
	calls := 0
	load := func() (string, string, error) { calls++; return "current-mother", "project", nil }
	manual := strings.Repeat("explicit-manual ", 20)
	got, _, err := resolveRequestPrompt("", manual, load)
	if err != nil || got != "current-mother" || calls != 1 {
		t.Fatal("stale body overrides project")
	}
	got, source, err := resolveRequestPrompt("manual", manual, load)
	if err != nil || got != strings.TrimSpace(manual) || calls != 1 || !strings.HasPrefix(source, "manual-input#sha256=") {
		t.Fatal(got, source, err)
	}
	if _, _, err := resolveRequestPrompt("unknown", manual, load); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestSellingPointAudiencePainRoundTripAndScheduling(t *testing.T) {
	raw := `{"卖点列表":[{"卖点名":"来源背书","目标人群":"新粉","痛点":"不了解来源","角度":["产地介绍"]}]}`
	canonical, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(raw)
	if err != nil || !strings.Contains(canonical, "目标人群") || !strings.Contains(canonical, "不了解来源") {
		t.Fatal(canonical, err)
	}
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(canonical), &wheel); err != nil {
		t.Fatal(err)
	}
	tasks := flattenAnchorStyleWheelTasks(wheel)
	if len(tasks) != 1 || tasks[0].TargetAudience != "新粉" || tasks[0].PainPoint != "不了解来源" {
		t.Fatal(tasks)
	}
	s := cycleTestState(t)
	s.Phase = "body"
	s.Queue[0].TargetAudience = "新粉"
	s.Queue[0].PainPoint = "不了解来源"
	prompt, err := renderAnchorCyclePrompt(&s)
	if err != nil || !strings.Contains(prompt, "当前卖点的目标人群：新粉") || !strings.Contains(prompt, "当前卖点的痛点：不了解来源") {
		t.Fatal("lost in generation scheduling", prompt, err)
	}
	if err := validateSellingPointPromptContract(raw, `"目标人群" "痛点"`); err != nil {
		t.Fatal(err)
	}
	old := `{"卖点列表":[{"卖点名":"来源背书","角度":["产地介绍"]}]}`
	if err := validateSellingPointPromptContract(old, `"目标人群" "痛点"`); err == nil {
		t.Fatal("missing required field accepted")
	}
	if err := validateSellingPointPromptContract(old, "legacy two-field template"); err != nil {
		t.Fatal("old confirmed pool invalidated", err)
	}
	response, err := anchorStyleSellingPointWheelResponseFromAsset(model.LiveSellingPointWheelAsset{OriginalWheelJSON: old, RawResponse: old, ExtractionPromptMD: `"目标人群" "痛点"`, Status: "validation_failed"})
	if err != nil || response.ValidationError == "" || response.RawResponse != old {
		t.Fatal("raw return lost or mismatch silent", response, err)
	}
	for _, bad := range []string{`{"卖点列表":[{"卖点名":"来源背书","目标人群":["新粉"],"角度":["产地介绍"]}]}`, `{"卖点列表":[{"卖点名":"来源背书","痛点":null,"角度":["产地介绍"]}]}`} {
		if _, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(bad); err == nil {
			t.Fatal("invalid field type accepted")
		}
	}
	empty := `{"卖点列表":[{"卖点名":"来源背书","目标人群":"","痛点":"","角度":["产地介绍"]}]}`
	got, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(empty)
	if err != nil || !strings.Contains(got, `"目标人群": ""`) {
		t.Fatal("unsupported facts must stay empty, not vanish", got, err)
	}
}

func TestSellingPointFactGapRoundTripAndContract(t *testing.T) {
	raw := `{"卖点列表":[{"卖点名":"规格选择","目标人群":"正在比较规格的消费者","痛点":"不清楚规格差异","角度":["整只与半只规格对比"]}],"事实补充建议":[{"主题":"补充保存信息","适用范围":"当前商品","建议分类":"product","待补事实":"保存方式与保质期","补充价值":"确认后可增加保存与开封处理角度","填写提示":["未开封保质期多久？","开封后如何保存？"]}]}`
	canonical, points, combinations, err := normalizeAnchorStyleSellingPointWheelJSON(raw)
	if err != nil || points != 1 || combinations != 1 || !strings.Contains(canonical, `"事实补充建议"`) || !strings.Contains(canonical, `"保存方式与保质期"`) {
		t.Fatal(canonical, points, combinations, err)
	}
	if err := validateSellingPointPromptContract(raw, `"目标人群" "痛点" "事实补充建议"`); err != nil {
		t.Fatal(err)
	}
	missing := `{"卖点列表":[{"卖点名":"规格选择","目标人群":"正在比较规格的消费者","痛点":"不清楚规格差异","角度":["整只与半只规格对比"]}]}`
	if err := validateSellingPointPromptContract(missing, `"目标人群" "痛点" "事实补充建议"`); err == nil {
		t.Fatal("missing fact gaps accepted")
	}
	bad := `{"卖点列表":[{"卖点名":"规格选择","目标人群":"消费者","痛点":"规格差异","角度":["规格对比"]}],"事实补充建议":[{"主题":"补充信息","适用范围":"当前商品","建议分类":"unknown","待补事实":"保存方式","补充价值":"增加角度","填写提示":["怎么保存？"]}]}`
	if _, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(bad); err == nil {
		t.Fatal("invalid fact gap category accepted")
	}
}

func TestSellingPointCoverageTargetsAndExpansionRequest(t *testing.T) {
	wheel := `{"卖点列表":[{"卖点名":"规格选择","目标人群":"正在比较规格的消费者","痛点":"不清楚规格差异","角度":["整只900g","半只450g","核对链接"]},{"卖点名":"品牌实体","目标人群":"首次了解品牌的消费者","痛点":"不了解实体依据","角度":["门店数量","总店地址","经营时间","证据组合","互动提问"]}],"事实补充建议":[]}`
	targets, err := sellingPointWheelCoverageTargets(wheel, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Name != "规格选择" || targets[0].CurrentAngles != 3 || targets[0].MinimumAngles != 5 {
		t.Fatal(targets)
	}
	rendered := renderSellingPointWheelExpansionRequest("# 正式事实包\n- 900g", wheel, targets)
	var payload struct {
		FactPackage string                            `json:"fact_package"`
		Wheel       anchorStyleSellingPointWheel      `json:"reviewed_wheel"`
		Targets     []sellingPointWheelCoverageTarget `json:"coverage_targets"`
	}
	if err := json.Unmarshal([]byte(rendered), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.FactPackage == "" || len(payload.Wheel.Items) != 2 || len(payload.Targets) != 1 {
		t.Fatal(rendered)
	}
}

func TestSellingPointFactBoundaryIgnoresLabelsAndChecksBody(t *testing.T) {
	facts := "# 正式事实包\n- [other/每日现做现发] 每天新鲜制作，现卤现炸\n- [fulfillment/物流快递] 使用京东物流"
	unsafeWheel := `{"卖点列表":[{"卖点名":"每日制作","目标人群":"关注制作信息的消费者","痛点":"不清楚制作信息","角度":["每天新鲜制作并现发"]}],"事实补充建议":[]}`
	violations := sellingPointFactBoundaryViolations(unsafeWheel, facts)
	if len(violations) != 1 || !strings.Contains(violations[0], "现发") {
		t.Fatal(violations)
	}
	safeWheel := `{"卖点列表":[{"卖点名":"每日制作","目标人群":"关注制作信息的消费者","痛点":"不清楚制作信息","角度":["每天新鲜制作，现卤现炸"]}],"事实补充建议":[]}`
	if violations := sellingPointFactBoundaryViolations(safeWheel, facts); len(violations) != 0 {
		t.Fatal(violations)
	}
	explicitFacts := facts + "\n- [other/发货方式] 每天现发"
	if violations := sellingPointFactBoundaryViolations(unsafeWheel, explicitFacts); len(violations) != 0 {
		t.Fatal(violations)
	}
	payload := renderSellingPointWheelFinalAuditRequest(facts, unsafeWheel, violations)
	if !strings.Contains(payload, "hard_boundary_violations") || !strings.Contains(payload, "现发") {
		t.Fatal(payload)
	}
	mixedWheel := `{"卖点列表":[{"卖点名":"每日制作","目标人群":"关注制作信息的消费者","痛点":"不清楚制作信息","角度":["每天新鲜制作并现发","每天新鲜制作，现卤现炸"]}],"事实补充建议":[]}`
	filtered, removed, points, angles, err := removeSellingPointFactBoundaryViolations(mixedWheel, facts)
	if err != nil || len(removed) != 1 || points != 1 || angles != 1 || strings.Contains(filtered, "现发") || !strings.Contains(filtered, "现卤现炸") {
		t.Fatal(filtered, removed, points, angles, err)
	}
}
