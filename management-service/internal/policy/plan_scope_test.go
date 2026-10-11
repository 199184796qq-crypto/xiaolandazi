package policy

import (
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestPlanPolicyKeepsP0AndDoesNotAdoptOldRoomBusiness(t *testing.T) {
	old := &model.LivePolicyVersion{ID: 19, Overrides: []model.LivePolicyOverride{
		{Key: "old-oil", Operation: "add", Text: "今年新菜籽", Metadata: nil},
		{Key: "other-plan", Operation: "add", Text: "其他方案", Metadata: map[string]any{"plan_id": float64(5)}},
		{Key: "current", Operation: "add", Text: "本方案规则", Metadata: map[string]any{"plan_id": int64(4)}},
	}}
	p0 := &model.LivePolicyVersion{ID: 1, Rules: []model.LivePolicyRule{{Key: "p0", Text: "有效P0", Enabled: true}}}
	effective := BuildEffective("food", p0, nil, ForPlan(old, 4))
	if !strings.Contains(effective.PromptText, "有效P0") || !strings.Contains(effective.PromptText, "本方案规则") || strings.Contains(effective.PromptText, "今年新菜籽") || strings.Contains(effective.PromptText, "其他方案") {
		t.Fatal(effective.PromptText)
	}
	if len(old.Overrides) != 3 || ForPlan(old, 0) != old {
		t.Fatal("legacy history changed")
	}
	// A routine save of the same old content must not assign it to current plan.
	next := BindPlanOverrides(old.Overrides, old, 4)
	if scopedPlanID(next[0].Metadata) != 0 || scopedPlanID(next[1].Metadata) != 5 {
		t.Fatal(next)
	}
	next[0].Text = "用户重新确认了这项内容"
	next = BindPlanOverrides(next, old, 4)
	if scopedPlanID(next[0].Metadata) != 4 {
		t.Fatal(next)
	}
}
