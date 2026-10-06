package httpapi

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/model"
)

func trainingReceiptFixture(id, feedback string) model.LiveAnchorStyleOverlayItem {
	return model.LiveAnchorStyleOverlayItem{
		ID: id, SourceText: feedback, ExplanationText: "口语节奏需要更自然",
		Enabled: true, LearningBasis: "human_feedback", Rule: validStyleOverlayRuleFixture(),
	}
}

func TestAppliedTrainingReceiptsExcludeDisabledInvalidAndNonTrainingRules(t *testing.T) {
	active := trainingReceiptFixture("active", "偶尔轻松接一句")
	disabled := trainingReceiptFixture("disabled", "已经停用")
	disabled.Enabled = false
	invalid := trainingReceiptFixture("invalid", "无效规则")
	invalid.Rule.MainlineInstruction = ""
	plugin := trainingReceiptFixture("plugin", "个性插件不是训练反馈")
	plugin.LearningBasis = "plugin_manifest"
	items := appliedAnchorTrainingReceipts([]model.LiveAnchorStyleOverlayItem{disabled, active, invalid, plugin}, true)
	if len(items) != 1 || items[0].ID != "active" || !items[0].Saved || items[0].Feedback != active.SourceText {
		t.Fatalf("unexpected training receipt: %+v", items)
	}
}

func TestAppliedTrainingReceiptsListAllRoundsAndSnapshotGenerationState(t *testing.T) {
	saved := []model.LiveAnchorStyleOverlayItem{trainingReceiptFixture("one", "第一条反馈"), trainingReceiptFixture("two", "第二条反馈")}
	transient := []model.LiveAnchorStyleOverlayItem{trainingReceiptFixture("three", "第三条反馈")}
	items := append(appliedAnchorTrainingReceipts(saved, true), appliedAnchorTrainingReceipts(transient, false)...)
	if len(items) != 3 || items[0].ID != "one" || items[1].ID != "two" || items[2].ID != "three" || items[2].Saved {
		t.Fatalf("missing rounds or wrong saved state: %+v", items)
	}
	saved[0].SourceText = "生成以后修改反馈"
	transient[0].Enabled = false
	if items[0].Feedback != "第一条反馈" || items[2].Feedback != "第三条反馈" {
		t.Fatal("receipt changed with current settings")
	}
	if items[0].MainlineInstruction != saved[0].Rule.MainlineInstruction || items[0].Diagnosis != saved[0].ExplanationText {
		t.Fatal("compiled training details absent")
	}
}

func TestAppliedTrainingReceiptsReturnExplicitEmptyArray(t *testing.T) {
	data, err := json.Marshal(appliedAnchorTrainingReceipts(nil, true))
	if err != nil || string(data) != "[]" {
		t.Fatalf("empty receipt must not mean unavailable: %s %v", data, err)
	}
}
