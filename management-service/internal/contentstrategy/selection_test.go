package contentstrategy

import (
	"testing"
	"time"
)

func TestSelectionHonorsOfferConditionsPrioritiesAndProductFactScope(t *testing.T) {
	facts := "- [商品卡] 1号129.9元\n- [商品卡] 2号85.5元\n- [有效福利] 福利=正式活动；活动=真实优惠；结束=2026-10-08T00:00:00Z"
	s := &Snapshot{Version: 2, Enabled: true, Facts: Facts(facts), Purposes: []Purpose{{ID: "P01", Name: "理解价格", Concern: "预算", FactIDs: []string{"F001", "F002"}}}, Rows: []Row{
		{PurposeID: "P01", Angle: "单品价格", Task: "直接给价格", FactIDs: []string{"F001"}, When: "价格问题", Priority: 100, Role: "required", Conditions: []Condition{{Type: "always"}}},
		{PurposeID: "P01", Angle: "另一商品", Task: "2号价格", FactIDs: []string{"F002"}, When: "价格问题", Priority: 100, Role: "required", Conditions: []Condition{{Type: "always"}}},
		{PurposeID: "P01", Angle: "优惠推进", Task: "有效优惠", FactIDs: []string{"F001", "F003"}, When: "有效优惠", Priority: 99, Role: "optional", Conditions: []Condition{{Type: "active_benefit"}}},
		{PurposeID: "P01", Angle: "单次预算", Task: "帮助预算判断", FactIDs: []string{"F001"}, When: "价格问题", Priority: 80, Role: "optional", Conditions: []Condition{{Type: "always"}}},
	}}
	active := ActiveBenefitFacts(s.Facts, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	selected := s.Select(SelectionRequest{Mode: "互动", AllowedFacts: s.Facts[0].Text + "\n" + s.Facts[2].Text, PurposeID: "P01", Question: "多少钱", ActiveBenefit: len(active) > 0, ActiveFactIDs: active})
	if len(selected.Required) != 1 || selected.Required[0].Angle != "单品价格" || len(selected.Optional) != 1 || selected.Optional[0].Angle != "单次预算" {
		t.Fatal("scope/conditions/priority not applied", selected)
	}
	if err := ValidateRowPolicy(Row{Priority: 80, Role: "optional", Conditions: []Condition{{Type: "invented"}}}, true); err == nil {
		t.Fatal("unknown condition accepted")
	}
	if err := ValidateRowPolicy(Row{}, true); err == nil {
		t.Fatal("new schema silently defaulted missing conditions")
	}
}

func TestRecentUseChoosesDifferentOptionalAngle(t *testing.T) {
	s := testSnapshot("- [商品卡] 一只85.5元")
	s.Rows[0].Priority = 95
	s.Rows[0].Role = "optional"
	s.Rows = append(s.Rows, Row{PurposeID: "P01", Angle: "按斤理解", Task: "按已确认斤价作选购判断", FactIDs: []string{"F001"}, When: "价格问题", Priority: 80, Role: "optional"})
	selected := s.Select(SelectionRequest{Mode: "互动", AllowedFacts: s.Facts[0].Text, PurposeID: "P01", Recent: []string{s.Rows[0].Angle}})
	if len(selected.Optional) != 1 || selected.Optional[0].Angle != "按斤理解" {
		t.Fatal("recent angle repeated", selected)
	}
}
