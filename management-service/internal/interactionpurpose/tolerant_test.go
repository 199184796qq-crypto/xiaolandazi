package interactionpurpose

import (
	"testing"

	"livecompanion/management/internal/contentstrategy"
)

// A model that invents an extra condition field must not cost the viewer their
// whole question: the usable part of the intent has to survive.
func TestTolerantDecodeKeepsIntentWithUnknownConditionField(t *testing.T) {
	raw := `{"question":true,"relevant":true,"confidence":0.9,"purpose_id":"P01",
		"purpose":"了解10人餐价格","scope":"10人餐",
		"task_angles":[{"目的编号":"P01","角度":"说明价格","讲述任务":"报出10人餐价格",
		"关联事实":["F020"],"采用条件":[{"类型":"question","说明":"额外字段"}],"适用条件":""}]}`
	result, err := decodeTolerantResult(raw, map[string]bool{"F020": true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Question || result.Purpose != "了解10人餐价格" || len(result.Angles) != 1 {
		t.Fatalf("intent lost: %+v", result)
	}
	if len(result.Angles[0].Conditions) != 1 || result.Angles[0].Conditions[0].Type != "question" {
		t.Fatalf("conditions not sanitised: %+v", result.Angles[0].Conditions)
	}
}

func TestPruneAnglesDropsUnusableRowsOnly(t *testing.T) {
	rows := []contentstrategy.Row{
		{Angle: "说明价格", Task: "报价", FactIDs: []string{"F020"}},
		{Angle: "说明价格", Task: "报价", FactIDs: []string{"F999"}},
		{Angle: "", Task: "缺角度", FactIDs: []string{"F020"}},
	}
	kept := pruneAngles(rows, map[string]bool{"F020": true})
	if len(kept) != 1 || kept[0].FactIDs[0] != "F020" {
		t.Fatalf("unexpected kept rows: %+v", kept)
	}
}

// The model no longer names facts; the purpose's configured facts must fill in.
func TestAssignAngleFactsFromPurpose(t *testing.T) {
	input := Input{
		Facts: "- [brand/官方身份] 本直播间是四川绸都杨鸭子官方直播间。\n- [price/售价] 整只装900g日常价87.6元。",
		Purposes: []contentstrategy.Purpose{
			{ID: "P01", Name: "了解售价", FactIDs: []string{"F002"}},
		},
	}
	result := Result{
		PurposeID: "P01",
		Angles:    []contentstrategy.Row{{Angle: "直接说明售价", Task: "回答价格"}},
	}
	got := assignAngleFacts(input, result)
	if len(got) != 1 {
		t.Fatalf("angle should survive with program-assigned facts: %+v", got)
	}
	if len(got[0].FactIDs) != 1 || got[0].FactIDs[0] != "F002" {
		t.Fatalf("fact ids not taken from purpose: %+v", got[0].FactIDs)
	}
}
