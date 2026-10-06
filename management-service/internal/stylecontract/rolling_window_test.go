package stylecontract

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestRollingWindowWaitsForSustainedTextAndTracksTermDistribution(t *testing.T) {
	sourceUnit := "哥哥姐姐们，你们听我们家慢慢说啊，大家先把重点听明白哟。"
	source := strings.Repeat(sourceUnit, 45)
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: Version}}, source)
	short := EvaluateRollingWindow(profile, source, strings.Repeat(sourceUnit, 5), 70)
	if short.Ready || short.WindowChars >= RollingWindowChars {
		t.Fatalf("short fragments were treated as a stable window: %+v", short)
	}
	good := EvaluateRollingWindow(profile, source, strings.Repeat("哥哥姐姐们，你们先听我们家说明白啊，大家顺着重点往下听哟。", 55), 70)
	bad := EvaluateRollingWindow(profile, source, strings.Repeat("先说明当前内容，再继续补充相关内容。", 90), 70)
	if !good.Ready || !bad.Ready || good.StyleScore <= bad.StyleScore || good.TermScore <= bad.TermScore {
		t.Fatalf("rolling statistics did not distinguish sustained delivery: good=%+v bad=%+v", good, bad)
	}
	guidance := RenderRollingWindowGuidance(profile, strings.Repeat("先说明当前内容。", 20), 180, 70)
	for _, expected := range []string{"滑动窗口", "跨小段统计指导", "后续可在合适语境自然补足", "不属于本窗口"} {
		if !strings.Contains(guidance, expected) {
			t.Fatalf("rolling guidance missing %q: %s", expected, guidance)
		}
	}
}

func TestHundredPercentFidelityBecomesAnEightyFivePointReleaseGate(t *testing.T) {
	unit := "哥哥姐姐们，你们听我们家慢慢说啊，大家先把重点听明白哟。"
	source := strings.Repeat(unit, 45)
	profile := Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{Version: Version}}, source)
	good, goodIssues := StrictFidelityIssues(profile, strings.Repeat(unit, 55), 100)
	if !good.Strict || good.TargetScore != 85 || !good.Passed || len(goodIssues) != 0 {
		t.Fatalf("matching delivery did not pass strict target: state=%+v issues=%v", good, goodIssues)
	}
	bad, badIssues := StrictFidelityIssues(profile, strings.Repeat("先说明当前内容，再继续补充相关信息。", 80), 100)
	if !bad.Strict || bad.TargetScore != 85 || bad.Passed || len(badIssues) == 0 {
		t.Fatalf("low-style delivery escaped strict target: state=%+v issues=%v", bad, badIssues)
	}
	advisory, advisoryIssues := StrictFidelityIssues(profile, strings.Repeat("先说明当前内容。", 80), 85)
	if advisory.Strict || len(advisoryIssues) != 0 {
		t.Fatalf("non-strict setting unexpectedly blocked: state=%+v issues=%v", advisory, advisoryIssues)
	}
}
