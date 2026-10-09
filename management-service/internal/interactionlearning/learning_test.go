package interactionlearning

import (
	"livecompanion/management/internal/model"
	"testing"
)

func TestAnswerKeepsMetadataOutOfSpeech(t *testing.T) {
	a, err := DecodeAnswer(`{"reply":"只走京东物流。","needs_review":false,"review":null}`)
	if err != nil || a.Reply != "只走京东物流。" || a.Review != nil {
		t.Fatal(a, err)
	}
	a, err = DecodeAnswer(`{"reply":"您是问能否选不抽真空的包装吧？","needs_review":true,"review":{"kind":"missing_fact","meaning":"能否选择非真空包装","context":"杨鸭子食品包装","reason":"只有耐放性，没有包装可选条件","suggested_rule":"","needed_facts":["是否支持不抽真空"]}}`)
	if err != nil || a.Review == nil || a.Review.Kind != "missing_fact" {
		t.Fatal(a, err)
	}
	for _, raw := range []string{`只走京东。`, `{"reply":"","needs_review":false,"review":null}`, `{"reply":"京东","needs_review":false,"review":null} {}`} {
		if _, err := DecodeAnswer(raw); err == nil {
			t.Fatalf("invalid protocol accepted: %s", raw)
		}
	}
}

func TestAdvisoryMetadataCannotVetoValidReply(t *testing.T) {
	for _, raw := range []string{`{"reply":"默认抽真空"}`, `{"reply":"默认抽真空","needs_review":true,"review":null}`, `{"reply":"默认抽真空","needs_review":false,"review":null,"unknown":1}`, `{"reply":"默认抽真空","needs_review":true,"review":{"kind":"unsupported","context":"包装","reason":"未知"}}`} {
		a, err := DecodeAnswer(raw)
		if err != nil || a.Reply != "默认抽真空" || a.MetadataError == "" || a.Review != nil {
			t.Fatalf("advisory metadata blocked speech or error was lost: %#v %v", a, err)
		}
	}
}

func TestPendingKeyPreservesPurposeAndScope(t *testing.T) {
	o := model.InteractionLearningObservation{TenantID: 14, RoomID: 8, Question: "不抽可以吗？", Review: model.InteractionLearningReview{PlanID: 4, Kind: "missing_fact", Meaning: "能否选择非真空包装", Context: "包装"}}
	a := PendingKey(o)
	same := o
	same.Question = " 不抽可以吗? "
	if a != PendingKey(same) {
		t.Fatal("punctuation should fold")
	}
	for _, change := range []func(*model.InteractionLearningObservation){func(o *model.InteractionLearningObservation) { o.TenantID++ }, func(o *model.InteractionLearningObservation) { o.RoomID++ }, func(o *model.InteractionLearningObservation) { o.Review.PlanID++ }, func(o *model.InteractionLearningObservation) { o.Review.Context = "抽奖" }, func(o *model.InteractionLearningObservation) { o.Question = "已经拍了还能改不抽吗" }, func(o *model.InteractionLearningObservation) { o.Question = "要抽可以吗" }} {
		b := o
		change(&b)
		if a == PendingKey(b) {
			t.Fatal("different scope/modality merged")
		}
	}
}

func TestConfirmedCorrectionsStayInOwnPlan(t *testing.T) {
	m := []model.AgentMemoryItem{{ID: 1, CurrentVersion: &model.AgentMemoryVersion{Structured: map[string]any{"review_plan_id": float64(4)}}}, {ID: 2, CurrentVersion: &model.AgentMemoryVersion{Structured: map[string]any{"review_plan_id": int64(5)}}}, {ID: 3, CurrentVersion: &model.AgentMemoryVersion{}}}
	got := MemoriesForPlan(m, 4)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatal(got)
	}
}

func TestScopedCorrectionShadowsSharedRuleOnlyInsideItsPlan(t *testing.T) {
	m := []model.AgentMemoryItem{{ID: 1, CurrentVersion: &model.AgentMemoryVersion{}}, {ID: 2, CurrentVersion: &model.AgentMemoryVersion{Structured: map[string]any{"review_plan_id": float64(4), "review_overrides_memory_id": float64(1)}}}}
	a, b := MemoriesForPlan(m, 4), MemoriesForPlan(m, 5)
	if len(a) != 1 || a[0].ID != 2 || len(b) != 1 || b[0].ID != 1 {
		t.Fatal(a, b)
	}
}
