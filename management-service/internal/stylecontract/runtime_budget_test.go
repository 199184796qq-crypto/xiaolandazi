package stylecontract

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func runtimeFixture(source string) model.LiveAgentPlanAnchorStyleProfile {
	return Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: Version,
		Instructions: []string{
			"先承接再解释", "短句与补充句交替", "称呼用于转场", "自称保留原词",
			"语气词只放自然句尾", "连接词用于换角度", "短答先给结论", "严肃场景收住促销表达",
		},
		Habits: []model.LiveAnchorLiteralHabit{
			{Kind: "self_address", Text: "我们家", Position: "句中"},
			{Kind: "self_address", Text: "我们", Position: "句中"},
			{Kind: "audience_address", Text: "哥哥姐姐们", Position: "句首"},
			{Kind: "particle", Text: "哟", Position: "句尾"},
			{Kind: "connector", Text: "你看嘛", Position: "句首"},
			{Kind: "catchphrase", Text: "给大家说一下", Position: "句中"},
		},
	}}, source)
}

func TestCompileRuntimeBudgetScalesHeatAndScene(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家给大家说一下哟。你看嘛，我们把原因讲清楚。", 8)
	profile := runtimeFixture(source)
	cold := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 240, Heat: 0, Scene: RuntimeSceneMainline})
	hot := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 240, Heat: 100, Scene: RuntimeSceneMainline})
	if cold.TotalHabitMax != 0 || hot.TotalHabitMax <= cold.TotalHabitMax {
		t.Fatalf("heat did not change runtime density: cold=%+v hot=%+v", cold, hot)
	}
	if hot.SentenceCharsMin <= 0 || hot.SentenceCharsMax < hot.SentenceCharsMin {
		t.Fatalf("invalid sentence budget: %+v", hot)
	}
	for _, group := range hot.HabitGroups {
		if group.Kind == "self_address" && group.SourceCount != 16 {
			t.Fatalf("overlapping source variants inflated the budget: %+v", group)
		}
	}
	interaction := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 80, Heat: 100, Scene: RuntimeSceneInteraction})
	if interaction.TotalHabitMin != 0 || interaction.TotalHabitMax > 2 {
		t.Fatalf("short interaction was forced to stack habits: %+v", interaction)
	}
	serious := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 120, Heat: 100, Scene: RuntimeSceneSerious})
	for _, group := range serious.HabitGroups {
		if (group.Kind == "particle" || group.Kind == "catchphrase") && group.MaxCount != 0 {
			t.Fatalf("serious scene kept promotional filler: %+v", group)
		}
	}
	if rendered := RenderRuntimeBudget(interaction); !strings.Contains(rendered, "变体是替代关系") || !strings.Contains(rendered, "风格热度=100/100") {
		t.Fatalf("runtime prompt is not executable: %s", rendered)
	}
}

func TestJensenShannonScorePenalizesVariantMixDrift(t *testing.T) {
	matching := jensenShannonScore([]float64{8, 2}, []float64{8, 2})
	drifted := jensenShannonScore([]float64{8, 2}, []float64{2, 8})
	if matching != 100 || drifted >= 75 {
		t.Fatalf("variant distribution drift was not separated: matching=%d drifted=%d", matching, drifted)
	}
}

func TestRuntimeEvaluationAllowsStyleWithoutCopying(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家给大家说一下哟。你看嘛，我们把原因讲清楚。", 8)
	profile := runtimeFixture(source)
	budget := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 160, Heat: 35, Scene: RuntimeSceneMainline})
	candidate := "哥哥姐姐们，先别着急，我们家今天把能确认的内容讲清楚。资料里写明的部分，我给大家说一下；没有写明的，咱们就不往外猜哟。你看嘛，先看适合什么场景，再看怎么选择，最后把注意事项补完整。这样听起来不绕，回头需要比较的时候也容易找到重点。已经确认的内容照实说，临时变化的部分就以现场信息为准哟。"
	result := EvaluateRuntimeCandidate(budget, source, candidate)
	if !result.Passed || result.StyleScore < 65 || result.CopyContainmentPct >= 35 {
		t.Fatalf("natural restyling rejected: %+v", result)
	}
	for _, group := range result.HabitGroups {
		if group.Kind == "self_address" && group.ActualCount != 1 {
			t.Fatalf("overlapping self-address variants were double counted: %+v", group)
		}
	}
}

func TestRuntimeEvaluationRejectsCopyAndHabitStacking(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家给大家说一下哟。你看嘛，我们把原因讲清楚。", 12)
	profile := runtimeFixture(source)
	budget := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 180, Heat: 35, Scene: RuntimeSceneMainline})
	copied := string([]rune(source)[:180])
	copyResult := EvaluateRuntimeCandidate(budget, source, copied)
	if copyResult.Passed || copyResult.CopyContainmentPct < 55 {
		t.Fatalf("source copy was not rejected: %+v", copyResult)
	}
	stacked := strings.Repeat("这件事给大家说明白", 8) + strings.Repeat("哟", 12) + "哥哥姐姐们，我们家按确认内容介绍，其他信息不猜。"
	stackResult := EvaluateRuntimeCandidate(budget, source, stacked)
	if stackResult.Passed {
		t.Fatalf("habit stacking was not rejected: %+v", stackResult)
	}
	found := false
	for _, issue := range stackResult.Issues {
		if issue.Code == "habit_overuse" || issue.Code == "habit_total_overuse" {
			found = true
		}
	}
	if !found {
		t.Fatalf("habit stacking did not produce a specific issue: %+v", stackResult)
	}
}

func TestRuntimeEvaluationRejectsInventedSelfAddressVariant(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家把内容慢慢讲清楚哟。", 16)
	profile := runtimeFixture(source)
	budget := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 180, Heat: 70, Scene: RuntimeSceneMainline})
	result := EvaluateRuntimeCandidate(budget, source, "哥哥姐姐们，咱家把当前内容慢慢说明白啊。没有确认的部分先不往外猜，已经确认的部分接着给大家讲清楚哟。")
	found := false
	for _, issue := range result.Issues {
		if issue.Code == "ungrounded_self_address" {
			found = true
		}
	}
	if result.Passed || !found {
		t.Fatalf("invented self-address variant escaped: %+v", result)
	}
}

func TestRuntimeEvaluationRejectsDialectOverrunAndSpeakerIdentityDrift(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家把内容慢慢讲清楚哟。", 20) + "这个说法没得问题。另一个地方也没得问题。"
	profile := runtimeFixture(source)
	profile = Normalize(profile, source)
	budget := CompileRuntimeBudget(profile, RuntimeOptions{TargetChars: 220, Heat: 100, Scene: RuntimeSceneMainline})
	candidate := "他们家先把这个事情讲清楚啊。哥哥姐姐们，你们晓得是啥子意思就行，屋头平时怎么用，再跟到自己的需要慢慢看哟。我们家只说已经确认的部分，没有依据的内容不往外猜。"
	result := EvaluateRuntimeCandidate(budget, source, candidate)
	codes := map[string]bool{}
	for _, issue := range result.Issues {
		codes[issue.Code] = true
	}
	if !codes["speaker_identity_drift"] || !codes["unsupported_dialect_marker"] {
		t.Fatalf("identity or dialect drift escaped: %+v", result)
	}
	allowedFound := false
	for _, group := range budget.HabitGroups {
		if group.Kind == "dialect_marker" && len(group.Terms) == 1 && group.Terms[0].Text == "没得" {
			allowedFound = true
		}
	}
	if !allowedFound {
		t.Fatalf("source-grounded sparse dialect was not preserved: %+v", budget.HabitGroups)
	}
}
