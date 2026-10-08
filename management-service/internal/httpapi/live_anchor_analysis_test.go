package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestEditedAnalysisMapsToAllEffectiveFilesAndDropsDeletedMonitoring(t *testing.T) {
	md := "【口语表达】\n1. 称呼：各位朋友。\n2. 句式：短句。"
	profile := model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{MetaPromptMD: "旧正文", Rulebook: "旧规范", MonitoringJSON: `{"terms":[{"text":"各位朋友"},{"text":"同学们"}],"forbidden_terms":["旧禁用"]}`, AddressingOverride: &model.LiveAnchorAddressingOverride{AudienceMode: "inherit"}}}
	got, err := mapEditedAnchorAnalysisProfile(profile, md, `{"updated":true}`, "score-mother")
	if err != nil {
		t.Fatal(err)
	}
	if got.Delivery.MetaPromptMD != md || got.Delivery.Rulebook != md || len(got.ReusableRules) != 1 || got.ReusableRules[0] != md || got.Delivery.ScoringBaselineJSON != `{"updated":true}` || got.Delivery.ScorecardPromptSource != "score-mother" {
		t.Fatalf("stale file mapping %+v", got)
	}
	var monitor anchorStyleWorkbenchMonitor
	if err = json.Unmarshal([]byte(got.Delivery.MonitoringJSON), &monitor); err != nil || len(monitor.Terms) != 1 || monitor.Terms[0].Text != "各位朋友" || len(monitor.ForbiddenTerms) != 0 {
		t.Fatalf("deleted rules remained %+v %v", monitor, err)
	}
	if got.Delivery.AddressingOverride.AudienceMode != "inherit" {
		t.Fatal("unrelated addressing settings changed")
	}
	profile.Delivery.MonitoringJSON = "invalid-json"
	if _, err = mapEditedAnchorAnalysisProfile(profile, md, "{}", "mother"); err == nil {
		t.Fatal("invalid file silently accepted")
	}
}

func TestAnalysisDocumentRebuildsStructureAndPreservesText(t *testing.T) {
	text, err := renderAnchorAnalysisSections([]anchorAnalysisSection{{Title: "情绪", Points: []string{"基调：温暖。\n不急促。", "称呼：同学们"}}, {Title: "口语风格", Points: []string{"不要机械复读"}}})
	if err != nil || !strings.Contains(text, "【情绪】\n1. 基调：温暖。\n不急促。\n2. 称呼：同学们") || !strings.Contains(text, "【口语风格】\n1. 不要机械复读") {
		t.Fatal(text, err)
	}
	for _, bad := range [][]anchorAnalysisSection{nil, {{Title: "情绪"}}, {{Title: "非法\n标题", Points: []string{"点"}}}, {{Title: "情绪", Points: []string{""}}}} {
		if _, err := renderAnchorAnalysisSections(bad); err == nil {
			t.Fatal("accepted invalid structure")
		}
	}
}
func TestFusionIncludesEverySampleOrRejects(t *testing.T) {
	a := model.LiveAnchorStyleSample{ID: 7, ReadableText: "原文甲", AnalysisStatus: "analyzed", Analysis: model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{MetaPromptMD: "分析甲"}}}
	b := a
	b.ID = 8
	b.ReadableText = "原文乙"
	b.Analysis.Delivery = &model.LiveAnchorDeliverySpec{MetaPromptMD: "分析乙"}
	corpus, ids, analyses, err := analyzedAnchorCorpus([]model.LiveAnchorStyleSample{b, a})
	if err != nil || corpus != "原文甲\n\n原文乙" || len(ids) != 2 || ids[0] != 7 || len(analyses) != 2 {
		t.Fatal(corpus, ids, err)
	}
	for _, status := range []string{"pending", "failed", "analyzing"} {
		b.AnalysisStatus = status
		if _, _, _, err := analyzedAnchorCorpus([]model.LiveAnchorStyleSample{a, b}); err == nil {
			t.Fatal("must not omit", status)
		}
	}
}
