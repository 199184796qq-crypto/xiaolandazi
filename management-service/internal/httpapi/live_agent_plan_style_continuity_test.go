package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechruntime"
)

func TestAnchorStyleSegmentStructureRejectsUnscheduledReentry(t *testing.T) {
	spec := speechruntime.SegmentSpec{
		SegmentRole: "middle", NewcomerReentryAllowed: false,
		OpeningAllowed: false, ClosingAllowed: false,
	}
	issues := anchorStyleSegmentStructureIssues(spec, "刚进来的朋友先别走，我重新介绍一下。")
	if len(issues) == 0 {
		t.Fatal("middle segment reentry was accepted")
	}
}

func TestAnchorStyleSegmentStructureAllowsScheduledReentry(t *testing.T) {
	spec := speechruntime.SegmentSpec{
		SegmentRole: "middle", NewcomerReentryAllowed: true,
		OpeningAllowed: false, ClosingAllowed: false,
	}
	issues := anchorStyleSegmentStructureIssues(spec, "刚进来的朋友先听这一点，接着说刚才的规格。")
	if len(issues) != 0 {
		t.Fatalf("scheduled reentry rejected: %v", issues)
	}
}

func TestAnchorStyleSegmentStructureRejectsEarlyRoundClose(t *testing.T) {
	spec := speechruntime.SegmentSpec{SegmentRole: "middle", ClosingAllowed: false}
	issues := anchorStyleSegmentStructureIssues(spec, "这个重点先说到这里，下一轮再给大家讲。")
	if len(issues) == 0 {
		t.Fatal("early round close was accepted")
	}
}

func TestScopeAnchorStyleSegmentContextOnlyExposesAssignedContent(t *testing.T) {
	generation := model.LiveAgentFullShowGenerationContext{
		FormalFacts:  []model.LiveAgentFullShowContextFact{{Key: "产地", Value: "四川"}, {Key: "物流", Value: "快递"}},
		Benefits:     []model.LiveAgentPlanBenefit{{Key: "试吃"}, {Key: "满减"}},
		ProductLinks: []model.LiveAgentPlanProductLink{{LinkKey: "1号链接"}, {LinkKey: "2号链接"}},
		AuthorizedFacts: []model.LiveAgentGenerationFact{
			{SourceKind: "supplemental_fact", SourceKey: "物流", Value: "快递"},
			{SourceKind: "supplemental_fact", SourceKey: "产地", Value: "四川"},
			{SourceKind: "benefit", SourceKey: "试吃", LinkKey: "1号链接", Value: "赠试吃装"},
			{SourceKind: "benefit", SourceKey: "满减", LinkKey: "2号链接", Value: "满减"},
			{SourceKind: "product", SourceKey: "1号链接", LinkKey: "1号链接", Value: "5L"},
			{SourceKind: "product", SourceKey: "2号链接", LinkKey: "2号链接", Value: "3L"},
		},
		ScriptReferences: []model.LiveAgentFullShowContextScriptReference{{ReferenceKey: "reference-1", ContentText: "不应进入分段事实上下文"}},
	}
	step := model.LiveSpeechExpansionStep{FactKeys: []string{"物流"}, BenefitKeys: []string{"试吃"}, LinkKeys: []string{"1号链接"}}
	scoped := scopeAnchorStyleSegmentContext(generation, step)
	if len(scoped.FormalFacts) != 1 || scoped.FormalFacts[0].Key != "物流" {
		t.Fatalf("facts not scoped: %+v", scoped.FormalFacts)
	}
	if len(scoped.Benefits) != 1 || scoped.Benefits[0].Key != "试吃" {
		t.Fatalf("benefits not scoped: %+v", scoped.Benefits)
	}
	if len(scoped.ProductLinks) != 1 || scoped.ProductLinks[0].LinkKey != "1号链接" {
		t.Fatalf("links not scoped: %+v", scoped.ProductLinks)
	}
	if len(scoped.AuthorizedFacts) != 3 {
		t.Fatalf("unified facts not scoped with their source records: %+v", scoped.AuthorizedFacts)
	}
	if len(scoped.ScriptReferences) != 0 {
		t.Fatalf("reference scripts leaked into segment: %+v", scoped.ScriptReferences)
	}
}
