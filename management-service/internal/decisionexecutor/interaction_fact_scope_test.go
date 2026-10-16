package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/model"
)

func interactionScopeFixture() (string, map[string]any, *anchorcycle.ProductFocus) {
	rows := []anchorcycle.ScopedFact{
		{Text: "- [公共] 免费停车；仅堂食。\n保留公共条件：高峰期可能排队。", Shared: true},
		{Text: "- [商品卡] 1号链接；【盛宴欢聚】10人餐；销售价337元；有效期30天。", Links: []string{"1号链接"}, Applicable: true},
		{Text: "- [商品卡] 2号链接；【限量回馈】6人餐；销售价187元。", Links: []string{"2号链接"}, Applicable: true},
		{Text: "- [商品卡] 11号链接；双人体验餐；销售价99元。", Links: []string{"11号链接"}, Applicable: true},
		{Text: "- [使用条件] 每桌限用1张券。\n不能在包间使用，不与其他优惠叠加。", Links: []string{"1号链接", "2号链接"}, Applicable: true},
		{Text: "- [福利卡] 2号专属赠品哨兵。", Links: []string{"2号链接"}, Applicable: true},
		{Text: "- [比较] 对比1号和2号的比较哨兵。", Links: []string{"1号链接", "2号链接"}},
	}
	texts := []string{}
	for _, r := range rows {
		texts = append(texts, r.Text)
	}
	facts := "# 正式事实包\n方案：测试\n" + strings.Join(texts, "\n") + "\n边界：不能编造。"
	focus := &anchorcycle.ProductFocus{MaterialVersion: "product-common-v1", FactsSHA: contentstrategy.SHA(facts), Facts: rows, Products: []anchorcycle.FocusProduct{{LinkKey: "1号链接", Name: "【盛宴欢聚】10人餐（含22道荤素菜）"}, {LinkKey: "2号链接", Name: "【限量回馈】6人餐（含15道荤素菜）"}, {LinkKey: "11号链接", Name: "双人体验餐"}}}
	generation := map[string]any{"derived_safety": map[string]any{"source_sha256": "approved"}, "anchor_generation": map[string]any{"product_focus": focus}}
	return facts, generation, focus
}

func TestInteractionScopeSelectsCompleteProductAndCommonFacts(t *testing.T) {
	for _, question := range []string{"1号多少钱？", "一号链接可以在包间用吗？", "１号链接有效期多久？", "10人餐多少钱？", "盛宴欢聚怎么用？"} {
		t.Run(question, func(t *testing.T) {
			full, generation, _ := interactionScopeFixture()
			got, scope := narrowInteractionFacts(generation, full, question)
			for _, required := range []string{"337元", "有效期30天", "免费停车", "高峰期可能排队", "每桌限用1张券", "不能在包间使用", "不与其他优惠叠加"} {
				if !strings.Contains(got, required) {
					t.Fatal("lost complete material", required, got)
				}
			}
			for _, forbidden := range []string{"187元", "99元", "专属赠品哨兵", "比较哨兵"} {
				if strings.Contains(got, forbidden) {
					t.Fatal("another product leaked", got)
				}
			}
			if len(scope.Links) != 1 || scope.Links[0] != "1号链接" || scope.FullFacts != full {
				t.Fatal(scope)
			}
		})
	}
}

func TestInteractionScopeComparisonAndUnknownFallback(t *testing.T) {
	full, generation, _ := interactionScopeFixture()
	got, scope := narrowInteractionFacts(generation, full, "1号和2号有什么区别？")
	if len(scope.Links) != 2 || !strings.Contains(got, "337元") || !strings.Contains(got, "187元") || !strings.Contains(got, "比较哨兵") || strings.Contains(got, "99元") {
		t.Fatal(got, scope)
	}
	for _, question := range []string{"1、2号各多少钱", "一和二号有什么区别"} {
		got, scope = narrowInteractionFacts(generation, full, question)
		if len(scope.Links) != 2 || !strings.Contains(got, "337元") || !strings.Contains(got, "187元") {
			t.Fatal("combined link list missed first product", got)
		}
	}
	for _, question := range []string{"这个多少钱", "能停车吗", "999号链接多少钱", "1号和999号哪个好", "16人餐多少钱", "1号和另一个哪个好", "1、999号哪个好", "是宰了才抽真空吗"} {
		got, scope = narrowInteractionFacts(generation, full, question)
		if got != full || len(scope.Links) != 0 {
			t.Fatal("unknown did not retain exact complete package", question, got)
		}
	}
	got, scope = narrowInteractionFacts(generation, full, "11号多少钱")
	if len(scope.Links) != 1 || !strings.Contains(got, "99元") || strings.Contains(got, "337元") {
		t.Fatal("11 matched 1", got)
	}
	if interactionLinkNumber("一〇一") != "101" || interactionLinkNumber("十二") != "12" {
		t.Fatal("spoken link number parsing")
	}
}

type editorScopedFactsStore struct {
	*snapshotFactStore
	scopePlan       int64
	scopeGeneration map[string]any
}

func (s *editorScopedFactsStore) CurrentInteractionFacts(context.Context, int64, int64, int64) (string, error) {
	return s.asset.SafeFacts, nil
}
func (s *editorScopedFactsStore) CurrentInteractionScopeVersion(context.Context, int64, int64, int64, string) (model.LiveAgentPlanVersion, error) {
	return model.LiveAgentPlanVersion{PlanID: s.scopePlan, GenerationContext: s.scopeGeneration}, nil
}
func (s *editorScopedFactsStore) CurrentSpeechExpressionConstraints(context.Context, int64, int64) (contentpools.Sources, error) {
	return contentpools.Sources{Products: []contentpools.Product{{ID: 10, Link: "1"}, {ID: 20, Link: "2"}}, Corrections: []contentpools.Correction{{Products: []int64{10}, Content: contentpools.CorrectionContent{Forbidden: "1号保留纠正"}}, {Products: []int64{20}, Content: contentpools.CorrectionContent{Forbidden: "2号纠正哨兵"}}}}, nil
}

func TestInteractionScopeEditorUsesCurrentFactsAndScopedCorrections(t *testing.T) {
	full, generation, _ := interactionScopeFixture()
	s := &editorScopedFactsStore{snapshotFactStore: &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: full}}, scopePlan: 2, scopeGeneration: generation}
	w := New(s, nil, nil, nil)
	session := model.LiveRuntimeSession{TenantID: 7, RoomID: 11}
	if audit, err := w.interactionProductContext(context.Background(), session, 2, true); err != nil || audit != full {
		t.Fatal("audit must retain complete current facts", audit, err)
	}
	item := decisionItem{SampleQuestions: []string{"1号多少钱"}, BusinessCoreOnly: true, BusinessPlanning: true}
	prompt, err := w.answerPrompt(context.Background(), session, item)
	if err != nil || !strings.Contains(prompt, "337元") || !strings.Contains(prompt, "1号保留纠正") || strings.Contains(prompt, "187元") || strings.Contains(prompt, "2号纠正哨兵") {
		t.Fatal(prompt, err)
	}
	s.scopeGeneration = nil
	prompt, err = w.answerPrompt(context.Background(), session, item)
	if err != nil || !strings.Contains(prompt, "187元") || !strings.Contains(prompt, "2号纠正哨兵") {
		t.Fatal("unverified editor scope did not fall back", prompt, err)
	}
	s.scopePlan = 3
	if _, err = w.answerPrompt(context.Background(), session, item); err == nil {
		t.Fatal("editor used another plan scope")
	}
}

func TestInteractionScopeIncompleteLegacyAndAmbiguousDataFallBack(t *testing.T) {
	for _, change := range []func(map[string]any, *anchorcycle.ProductFocus){
		func(g map[string]any, _ *anchorcycle.ProductFocus) { delete(g, "anchor_generation") },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.MaterialVersion = "" },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.FactsSHA = "changed" },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.Facts = p.Facts[:len(p.Facts)-1] },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.Facts[0].Text = "foreign fact" },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.Facts[0].Links = []string{"unknown"} },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.Facts[1].Links = nil },
		func(_ map[string]any, p *anchorcycle.ProductFocus) { p.Products = append(p.Products, p.Products[0]) },
	} {
		full, generation, focus := interactionScopeFixture()
		change(generation, focus)
		got, scope := narrowInteractionFacts(generation, full, "1号多少钱")
		if got != full || len(scope.Links) != 0 {
			t.Fatal("invalid scope lost fallback", got)
		}
	}
	full, generation, focus := interactionScopeFixture()
	focus.Products[1].Name = focus.Products[0].Name
	if got, scope := narrowInteractionFacts(generation, full, "10人餐多少钱"); got != full || len(scope.Links) != 0 {
		t.Fatal("ambiguous name guessed", got)
	}
}

func TestInteractionScopeReachesPlannerLiveAndSimulation(t *testing.T) {
	full, generation, _ := interactionScopeFixture()
	s := &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}, publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: generation}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: full}}
	w := New(s, nil, nil, nil)
	for _, planning := range []bool{false, true} {
		for _, simulation := range []bool{false, true} {
			item := decisionItem{SampleQuestions: []string{"1号有效期多久"}, BusinessPlanning: planning, BusinessCoreOnly: simulation}
			if !planning {
				item.BusinessPlan = &interactionBusinessPlan{Basis: []string{"有效期30天"}}
			}
			prompt, err := w.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, item)
			if err != nil || !strings.Contains(prompt, "337元") || !strings.Contains(prompt, "每桌限用1张券") || strings.Contains(prompt, "187元") || strings.Contains(prompt, "专属赠品哨兵") {
				t.Fatal("scope lost in prompt", planning, simulation, prompt, err)
			}
			item.SampleQuestions = []string{"这个怎么用"}
			prompt, err = w.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, item)
			if err != nil || !strings.Contains(prompt, "337元") || !strings.Contains(prompt, "187元") || !strings.Contains(prompt, "99元") {
				t.Fatal("unknown fallback lost in prompt", prompt, err)
			}
		}
	}
}

func TestInteractionScopePreservesStrategyWithoutOtherProductFacts(t *testing.T) {
	full, generation, _ := interactionScopeFixture()
	strategy := contentstrategy.Snapshot{Version: 1, Enabled: true, FactsSHA: contentstrategy.SHA(full), Facts: contentstrategy.Facts(full), Purposes: []contentstrategy.Purpose{{ID: "P01", Name: "查看套餐", Concern: "选哪个", FactIDs: []string{"F002", "F003"}}}, Rows: []contentstrategy.Row{{PurposeID: "P01", Angle: "1号方案指引", Task: "说明1号", FactIDs: []string{"F002"}, When: "商品咨询"}, {PurposeID: "P01", Angle: "2号方案指引哨兵", Task: "说明2号", FactIDs: []string{"F003"}, When: "商品咨询"}}}
	raw, _ := json.Marshal(strategy)
	s := &strategyFactStore{snapshotFactStore: &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}, publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: generation}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: full}}, raw: string(raw)}
	prompt, err := New(s, nil, nil, nil).answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, decisionItem{SampleQuestions: []string{"1号怎么选"}, BusinessPlanning: true})
	if err != nil || !strings.Contains(prompt, "1号方案指引") || strings.Contains(prompt, "2号方案指引哨兵") || strings.Contains(prompt, "187元") {
		t.Fatal("strategy leaked or disappeared", prompt, err)
	}
}
