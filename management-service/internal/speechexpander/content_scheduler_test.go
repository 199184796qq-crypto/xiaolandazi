package speechexpander

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestResolveContentStrategyPreservesExplicitZeroControls(t *testing.T) {
	strategy := ResolveContentStrategy(ContentStrategyInput{
		LiveType: "commerce", IndustryCode: "服装", ConversionIntensity: 0, ExpansionFreedom: 0,
	}, nil)
	if strategy.ConversionIntensity != 0 || strategy.ExpansionFreedom != 0 {
		t.Fatalf("explicit zero controls were replaced: %+v", strategy)
	}
	if got := strings.Count(strings.Join(strategy.RoleSequence, ","), "action"); got != 1 {
		t.Fatalf("zero conversion should keep only the final action role, got %d in %v", got, strategy.RoleSequence)
	}
}

func schedulerFact(id, kind, source, predicate, label, value, link string) model.LiveAgentGenerationFact {
	return model.LiveAgentGenerationFact{FactID: id, SourceKind: kind, SourceKey: source, Predicate: predicate, Label: label, Value: value, LinkKey: link, CanGenerate: true}
}

func TestResolveContentStrategySeparatesTypeIndustryPlanAndRuntime(t *testing.T) {
	strategy := ResolveContentStrategy(ContentStrategyInput{LiveType: "commerce", IndustryCode: "服装", PlanGoal: "讲通勤衬衫", ConversionIntensity: 80, ExpansionFreedom: 65}, nil)
	if strategy.Version != ContentStrategyVersion || strategy.LiveType != "commerce" || strategy.IndustryCode != "apparel" {
		t.Fatalf("unexpected strategy: %+v", strategy)
	}
	if strategy.ConversionIntensity != 80 || strategy.ExpansionFreedom != 65 || len(strategy.Layers) != 5 {
		t.Fatalf("layer inputs were not preserved: %+v", strategy)
	}
	if len(strategy.RoleSequence) < 8 {
		t.Fatalf("apparel sequence too short: %+v", strategy.RoleSequence)
	}
}

func TestScheduleContentUsesPurposeAndPrimarySupportFacts(t *testing.T) {
	plans := BuildFixedPlans(Input{DurationMinutes: 8, TargetChars: 2000, VariantCount: 1})
	manifest := []model.LiveAgentGenerationFact{
		schedulerFact("product-name", "product", "1号链接", "product_name", "商品名称", "通勤衬衫", "1号链接"),
		schedulerFact("material", "product", "1号链接", "attribute.material", "面料", "70%棉30%聚酯纤维", "1号链接"),
		schedulerFact("size", "product", "1号链接", "spec", "尺码", "S到XL", "1号链接"),
		schedulerFact("scenario", "supplemental_fact", "通勤场景", "scenario", "场景", "日常通勤和周末出行", ""),
		schedulerFact("shipping", "supplemental_fact", "物流", "shipping", "发货", "中通、极兔、申通", ""),
	}
	scheduled, strategy := ScheduleContent(plans, manifest, ContentStrategyInput{LiveType: "commerce", IndustryCode: "服装", ConversionIntensity: 35, ExpansionFreedom: 60})
	if len(scheduled) != 1 || len(scheduled[0].Steps) != 8 {
		t.Fatalf("unexpected scheduled plans: %+v", scheduled)
	}
	if strategy.RoleSequence[0] != "orient" {
		t.Fatalf("unexpected opening role: %+v", strategy.RoleSequence)
	}
	for index, step := range scheduled[0].Steps {
		if step.ContentRole == "action" && len(step.LinkKeys) == 0 && len(step.BenefitKeys) == 0 {
			t.Fatalf("action unit has no action material at %d: %+v", index, step)
		}
		if step.PrimaryFactID == "shipping" && step.ContentRole != "action" && step.ContentRole != "decision" {
			t.Fatalf("shipping was incorrectly used as a general opening: %+v", step)
		}
		if len(step.SupportFactIDs) > 1 {
			t.Fatalf("too many support facts: %+v", step)
		}
	}
	// A new request starts from the cursor, so the first primary fact must not
	// repeat the previous request's recent material merely because row order is
	// unchanged.
	continued, _ := ScheduleContent(BuildFixedPlans(Input{DurationMinutes: 1, TargetChars: 250, VariantCount: 1}), manifest, ContentStrategyInput{
		LiveType: "commerce", IndustryCode: "服装", ConversionIntensity: 35,
		Cursor: ContentScheduleCursor{CompletedUnits: 8, RecentFactIDs: []string{"product-name", "material", "size"}, RecentRoles: []string{"value", "decision", "evidence"}},
	})
	if continued[0].Steps[0].PrimaryFactID == "product-name" || continued[0].Steps[0].PrimaryFactID == "material" || continued[0].Steps[0].PrimaryFactID == "size" {
		t.Fatalf("continuation ignored recent fact cursor: %+v", continued[0].Steps[0])
	}
}

func TestScheduleContentDoesNotForceActionWithoutActionMaterial(t *testing.T) {
	plans := BuildFixedPlans(Input{DurationMinutes: 2, TargetChars: 500, VariantCount: 1})
	manifest := []model.LiveAgentGenerationFact{schedulerFact("origin", "supplemental_fact", "产地", "origin", "产地", "四川", "")}
	scheduled, _ := ScheduleContent(plans, manifest, ContentStrategyInput{LiveType: "commerce", ConversionIntensity: 90})
	for _, step := range scheduled[0].Steps {
		if step.ContentRole == "action" {
			t.Fatalf("action was kept without a product/offer/choice fact: %+v", step)
		}
	}
}

func TestProductRoomPositionShapesVariableProductPlan(t *testing.T) {
	products := []model.LiveAgentPlanProductLink{
		{LinkKey: "1号链接", ProductName: "主商品", RoomRoles: []string{model.LiveRoomProductRoleMain}, Status: "active"},
		{LinkKey: "2号链接", ProductName: "福利商品", RoomRoles: []string{model.LiveRoomProductRoleBenefit}, Status: "active"},
	}
	manifest := []model.LiveAgentGenerationFact{
		schedulerFact("main-name", "product", "1号链接", "product_name", "商品名称", "主商品", "1号链接"),
		schedulerFact("benefit-name", "product", "2号链接", "product_name", "商品名称", "福利商品", "2号链接"),
		schedulerFact("benefit-offer", "benefit", "2号福利", "gift", "赠品", "试用装", "2号链接"),
	}
	plans := BuildFixedPlans(Input{DurationMinutes: 1, TargetChars: 250, VariantCount: 1})
	scheduled, strategy := ScheduleContent(plans, manifest, ContentStrategyInput{LiveType: "commerce", ProductLinks: products})
	if len(strategy.ProductPlan) != 2 || strategy.ProductPlan[0].Emphasis != "high" || len(strategy.ProductPlan[1].Transitions) != 1 || strategy.ProductPlan[1].Transitions[0].TargetLinkKey != "1号链接" {
		t.Fatalf("unexpected generated product plan: %+v", strategy.ProductPlan)
	}
	if scheduled[0].Steps[0].PrimaryFactID != "main-name" {
		t.Fatalf("main positioning should bias the initial fallback without fixing every later step: %+v", scheduled[0].Steps[0])
	}
}
