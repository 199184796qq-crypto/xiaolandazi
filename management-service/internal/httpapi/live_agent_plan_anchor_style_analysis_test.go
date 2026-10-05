package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/stylecontract"
)

func TestAnchorStyleOnlyDimensionsExcludeBusinessAndInterruptionStrategy(t *testing.T) {
	spec := anchorStyleOnlyDimensionsSpec()
	for _, forbidden := range []string{"price_expression", "link_handoff", "cta_style", "mainline_resume", "product_explanation_path"} {
		if strings.Contains(spec, forbidden) {
			t.Fatalf("business or interruption dimension leaked into style-only prompt: %s", forbidden)
		}
	}
	for _, required := range []string{"sentence_rhythm", "audience_address", "self_address", "catchphrases", "pause_chunking"} {
		if !strings.Contains(spec, required) {
			t.Fatalf("style dimension missing: %s", required)
		}
	}
}

func TestNormalizeAnchorStyleAnalysisQCOwnsReleaseGate(t *testing.T) {
	source := strings.Repeat("哥哥姐姐们啊，我们家接着说明哟，你看嘛。", 4)
	profile := stylecontract.Normalize(model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: stylecontract.Version,
		Habits: []model.LiveAnchorLiteralHabit{
			{Kind: "audience_address", Text: "哥哥姐姐们", Position: "句首", When: "自然开场或转场时"},
			{Kind: "self_address", Text: "我们家", Position: "句中", When: "自然说明时"},
		},
	}}, source)
	qc := normalizeAnchorStyleAnalysisQC(anchorStyleAnalysisQC{
		Passed: true, CoverageScore: 90, PurityScore: 95,
	}, agentgateway.Response{Model: styleOverlayInterpreterModel, LatencyMS: 12}, profile, source)
	if !qc.Passed || !qc.Available || qc.Model != styleOverlayInterpreterModel {
		t.Fatalf("valid QC rejected: %+v", qc)
	}

	profile.ReusableRules = append(profile.ReusableRules, "反复强调活动价格和库存")
	qc = normalizeAnchorStyleAnalysisQC(anchorStyleAnalysisQC{
		Passed: true, CoverageScore: 90, PurityScore: 95,
	}, agentgateway.Response{Model: styleOverlayInterpreterModel}, profile, source)
	if qc.Passed || !containsString(qc.IssueCodes, "business_content_leak") {
		t.Fatalf("Go purity gate was bypassed: %+v", qc)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
