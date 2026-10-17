package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestExplicitFactScopeOverridesModelAndSurvivesSafetyJSON(t *testing.T) {
	links := []model.LiveAgentPlanProductLink{{ID: 21, TenantID: 14, PlanID: 4, LinkKey: "1号链接", Status: "active"}, {ID: 22, TenantID: 14, PlanID: 4, LinkKey: "2号链接", Status: "active"}}
	facts := []model.LiveAgentPlanFact{{ID: 7, TenantID: 14, PlanID: 4, Category: "other", Key: "门店条件", Value: "每桌一券\n周末不可用", SafeRewrite: "必须保留条件", Status: "active", Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkID: 22}}, {ID: 8, TenantID: 14, PlanID: 4, Category: "other", Key: "公共", Value: "停车免费", Status: "active", Scope: &model.LiveAgentPlanFactScope{Kind: "common"}}}
	render := renderAnchorStyleWorkbenchFacts(model.LiveAgentPlan{Name: "测试"}, facts, nil, nil)
	if _, err := classifyProductCommonFacts(context.Background(), render, links, func(context.Context, map[string]any) (string, error) {
		t.Fatal("missing bound product reached model")
		return "", nil
	}); err == nil {
		t.Fatal("missing product silently became public")
	}
	render = renderAnchorStyleWorkbenchFacts(model.LiveAgentPlan{Name: "测试"}, facts, nil, links)
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	input, err := buildDerivedInput(render, "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	// Classification and a JSON round trip must retain the complete fact blocks.
	scoped, err := classifyProductCommonFacts(context.Background(), input.Facts, links, func(context.Context, map[string]any) (string, error) {
		t.Fatal("user scope reached model")
		return "", nil
	})
	if err != nil || len(scoped) != 4 {
		t.Fatal(scoped, err)
	}
	raw, _ := json.Marshal(scoped)
	var restored []anchorcycle.ScopedFact
	if json.Unmarshal(raw, &restored) != nil {
		t.Fatal("snapshot unreadable")
	}
	focus := &anchorcycle.ProductFocus{Facts: restored, Products: []anchorcycle.FocusProduct{{LinkKey: "1号链接"}, {LinkKey: "2号链接"}}}
	if err = validateProductFactSnapshot(render, focus); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(classifiedProductFactPackage("2号链接", restored), "周末不可用") || strings.Contains(classifiedProductFactPackage("1号链接", restored), "周末不可用") {
		t.Fatal("product scope or condition lost")
	}
	focus.Facts[0].Shared = true
	focus.Facts[0].Links = nil
	if validateProductFactSnapshot(render, focus) == nil {
		t.Fatal("manual product scope overwritten")
	}
	if json.Unmarshal(raw, &restored) != nil {
		t.Fatal("snapshot unreadable")
	}
	focus.Facts = restored
	focus.Facts[1].Shared = false
	focus.Facts[1].Links = []string{"1号链接"}
	if validateProductFactSnapshot(render, focus) == nil {
		t.Fatal("manual common scope overwritten")
	}
	links[1].LinkKey = "自定义二号"
	render = renderAnchorStyleWorkbenchFacts(model.LiveAgentPlan{}, facts, nil, links)
	selected, explicit, err := explicitPlanFactScope(productFactBlocks(render)[0])
	if !explicit || err != nil || len(selected.Links) != 1 || selected.Links[0] != "自定义二号" {
		t.Fatal("binding followed stale display name", selected, err)
	}
}

func TestUnassignedFactKeepsExistingModelClassification(t *testing.T) {
	called := 0
	facts, err := classifyProductCommonFacts(context.Background(), "- [other/停车] 免费停车", nil, func(context.Context, map[string]any) (string, error) { called++; return `{"归属":"公共"}`, nil })
	if err != nil || called != 1 || len(facts) != 1 || !facts[0].Shared {
		t.Fatal(facts, called, err)
	}
}

func TestFactScopeGenerationManifestKeepsExplicitProductAndCommon(t *testing.T) {
	links := []model.LiveAgentPlanProductLink{{ID: 21, TenantID: 14, PlanID: 4, LinkKey: "1号链接", ProductName: "当前商品", Status: "active"}}
	facts := []model.LiveAgentPlanFact{
		{ID: 7, Key: "使用条件", Value: "每桌一券", Status: "active", VersionNo: 2, Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkID: 21}},
		{ID: 8, Key: "停车", Value: "免费停车", Status: "active", VersionNo: 3, Scope: &model.LiveAgentPlanFactScope{Kind: "common"}},
		{ID: 9, Key: "旧商品", Value: "过期商品事实", Status: "active", VersionNo: 1, Scope: &model.LiveAgentPlanFactScope{Kind: "product", ProductLinkID: 999}},
	}
	generation := compileFullShowContext(model.LiveAgentPlan{}, facts, nil, links, nil, model.LiveAgentFullShowPreviewInput{DurationMinutes: 60, RoundMinutes: 6, VariantCount: 1})
	if len(generation.FormalFacts) != 2 || generation.FormalFacts[0].Scope.ProductLinkID != 21 {
		t.Fatal("formal scope lost", generation.FormalFacts)
	}
	got := map[string]model.LiveAgentGenerationFact{}
	for _, f := range generation.AuthorizedFacts {
		if f.SourceKind == "supplemental_fact" {
			got[f.SourceKey] = f
		}
	}
	if len(got) != 2 || got["使用条件"].ScopeKind != "product_link" || got["使用条件"].ProductName != "当前商品" || got["停车"].ScopeKind != "plan" {
		t.Fatal("manifest widened explicit product", got)
	}
}
