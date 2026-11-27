package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestAutoScriptFactsRespectObjectAndConditions(t *testing.T) {
	links := []planAutoLink{{ID: 101, Label: "1号链接", Name: "整只", Spec: "900g", Price: "87.6元"}, {ID: 102, Label: "2号链接", Name: "礼品装", Spec: "900g", Price: "92.6元"}}
	claim := func(id, link int64, key, topic, content string) model.PlanMaterialClaim {
		return model.PlanMaterialClaim{ID: id, ClaimKey: key, Topic: topic, Content: content, ProductLinkID: link, Status: "active", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact"}}
	}
	global := claim(1, 0, "craft", "工艺", "老卤慢炖")
	global.ScopeKind = "plan"
	first := claim(2, 101, "taste", "口味", "五香味")
	second := claim(3, 102, "taste", "口味", "麻辣味")
	conditional := claim(4, 0, "voucher", "优惠券", "领券减十元")
	conditional.IsBenefit = true
	conditional.Conditions = json.RawMessage(`["领到券时"]`)
	risky := claim(5, 101, "stock", "库存", "仅剩十只")
	risky.RiskFlags = []string{"unverified"}
	claims := []model.PlanMaterialClaim{global, first, second, conditional, risky}
	catalog := autoScriptCatalog(claims, links)
	for _, item := range catalog {
		if item.Attribute == "voucher" || item.Attribute == "stock" {
			t.Fatalf("unsafe claim in catalog: %+v", item)
		}
	}
	text := buildAutoFacts(claims, links, planAutoScriptPlan{Objects: []string{"1号链接"}, Attributes: []string{"craft", "taste", "voucher"}})
	for _, wanted := range []string{"1号链接：商品名：整只", "全局·工艺：老卤慢炖", "1号链接·口味：五香味"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("missing %q in %q", wanted, text)
		}
	}
	for _, forbidden := range []string{"2号链接", "麻辣味", "优惠券", "仅剩十只"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("leaked %q in %q", forbidden, text)
		}
	}
}

func TestAutoScriptMultipleTargetsAndPlanConsistency(t *testing.T) {
	links := []planAutoLink{{ID: 101, Label: "1号链接"}, {ID: 102, Label: "2号链接"}}
	shared := model.PlanMaterialClaim{ID: 7, ClaimKey: "gift", Topic: "礼品包装", Content: "可用礼盒", Status: "active", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}, {Kind: "product", LinkID: 102}}}}
	facts := buildAutoFacts([]model.PlanMaterialClaim{shared}, links, planAutoScriptPlan{Objects: []string{"2号链接"}, Attributes: []string{"gift"}})
	if !strings.Contains(facts, "2号链接·礼品包装：可用礼盒") || strings.Contains(facts, "1号链接·礼品包装") {
		t.Fatal(facts)
	}
	catalog := autoScriptCatalog([]model.PlanMaterialClaim{shared}, links)
	plan := planAutoScriptPlan{Objects: []string{"1号链接"}, Attributes: []string{"other"}, WritingRequirements: []string{"讲清礼品包装"}}
	if autoScriptValidatePlan(plan, catalog, links) == nil {
		t.Fatal("unselected factual requirement accepted")
	}
	plan.Attributes = []string{"gift"}
	if err := autoScriptValidatePlan(plan, catalog, links); err != nil {
		t.Fatal(err)
	}
}

func TestAutoScriptLiteralGuardRejectsNewNumbersAndOtherLink(t *testing.T) {
	links := []planAutoLink{{ID: 101, Label: "1号链接"}, {ID: 102, Label: "2号链接"}}
	facts := "1号链接：商品名：整只；规格：900g；价格：87.6元"
	if err := autoScriptCheckLiteralFacts("1号链接是900g，87.6元。", facts, []string{"1号链接"}, links); err != nil {
		t.Fatal(err)
	}
	if err := autoScriptCheckLiteralFacts("1号链接只要77.6元。", facts, []string{"1号链接"}, links); err == nil {
		t.Fatal("invented price accepted")
	}
	if err := autoScriptCheckLiteralFacts("2号链接也是900g。", facts, []string{"1号链接"}, links); err == nil {
		t.Fatal("unselected object accepted")
	}
}

func TestAutoScriptAvoidsRecentOpeningsAndObjectCombos(t *testing.T) {
	links := []planAutoLink{{ID: 101, Label: "1号链接"}, {ID: 102, Label: "2号链接"}}
	recent := []string{"今天说说1号链接和2号链接。价格清楚。"}
	if !autoScriptRepeatedDirection(planAutoScriptPlan{Objects: []string{"1号链接", "2号链接"}, Hook: "换个开头"}, recent, links) {
		t.Fatal("same object combination accepted")
	}
	if !autoScriptRepeatedDirection(planAutoScriptPlan{Objects: []string{"1号链接"}, Hook: "今天说说1号链接"}, recent, links) {
		t.Fatal("same opening accepted")
	}
	if autoScriptRepeatedDirection(planAutoScriptPlan{Objects: []string{"1号链接"}, Hook: "另一种角度"}, recent, links) {
		t.Fatal("different direction rejected")
	}
}
