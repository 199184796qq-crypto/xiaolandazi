package speechexpander

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestDynamicDecisionChoosesMenuNotFixedSequence(t *testing.T) {
	facts := []model.LiveAgentGenerationFact{
		schedulerFact("shirt", "product", "1", "product_name", "名称", "衬衫", "1"),
		schedulerFact("size", "product", "1", "spec", "规格", "S到XL", "1"),
		schedulerFact("shipping", "supplemental_fact", "快递", "shipping", "物流", "中通", ""),
		schedulerFact("other", "product", "2", "spec", "规格", "5L", "2"),
	}
	strategy := ResolveContentStrategy(ContentStrategyInput{LiveType: "commerce", ConversionIntensity: 55}, facts)
	original := model.LiveSpeechExpansionStep{Stage: "orient", Index: 4, StartSecond: 60, EndSecond: 90, StyleCapabilities: []string{"self-correction"}}
	step, err := ApplyContentDecision(original, ContentDecision{Role: "decision", PrimaryFactID: "size"}, facts, strategy)
	if err != nil || step.ContentRole != "decision" || step.PrimaryFactID != "size" || step.StartSecond != 60 || len(step.StyleCapabilities) != 1 {
		t.Fatalf("choice=%+v err=%v", step, err)
	}
	for _, choice := range []ContentDecision{
		{Role: "value", PrimaryFactID: "invented"}, {Role: "unknown", PrimaryFactID: "shirt"},
		{Role: "orient", PrimaryFactID: "shipping"},
		{Role: "decision", PrimaryFactID: "size", SupportFactIDs: []string{"other"}},
		{Role: "decision", PrimaryFactID: "size", SupportFactIDs: []string{"size"}},
		{Role: "decision", PrimaryFactID: "size", SupportFactIDs: []string{"shirt", "other"}},
	} {
		if _, err := ApplyContentDecision(original, choice, facts, strategy); err == nil {
			t.Fatalf("accepted invalid choice: %+v", choice)
		}
	}
}

func TestPlanningFactsBoundsPreserveDiverseFamilies(t *testing.T) {
	facts := []model.LiveAgentGenerationFact{}
	for i := 0; i < 60; i++ {
		facts = append(facts, schedulerFact(string(rune('A'+i)), "product", "1", "attribute", "卖点", "商品内容", "1"))
	}
	facts = append(facts, schedulerFact("shipping", "supplemental_fact", "快递", "shipping", "快递", "申通", ""))
	denied := schedulerFact("denied", "product", "2", "spec", "规格", "5L", "2")
	denied.CanGenerate = false
	facts = append(facts, denied)
	got := PlanningFacts(facts, ContentScheduleCursor{RecentFactIDs: []string{"A"}})
	if len(got) != 40 || got[0].FactID == "A" {
		t.Fatalf("unbounded or no cooling: %+v", got)
	}
	found := false
	for _, fact := range got {
		if fact.FactID == "denied" {
			t.Fatal("unauthorized fact included")
		}
		found = found || fact.FactID == "shipping"
	}
	if !found {
		t.Fatal("material family lost")
	}
}

func TestEmptyFactsAndNoncommerceMenu(t *testing.T) {
	strategy := ResolveContentStrategy(ContentStrategyInput{LiveType: "conversation", ConversionIntensity: 0}, nil)
	if _, err := ApplyContentDecision(model.LiveSpeechExpansionStep{}, ContentDecision{Role: "scenario"}, nil, strategy); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyContentDecision(model.LiveSpeechExpansionStep{}, ContentDecision{Role: "action"}, nil, strategy); err == nil {
		t.Fatal("unexpected action accepted")
	}
}
