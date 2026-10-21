package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestFactSpeechPreviewKeepsConditionsAndOnlySelectedMaterial(t *testing.T) {
	f := model.LiveAgentPlanFact{ID: 321987, TenantID: 14, PlanID: 4, VersionNo: 7, Status: "active", Key: "退货条件", Value: "一次性包装未破损时，可申请7天无理由退货", ForbiddenWording: "无条件退货", SafeRewrite: "符合条件可申请"}
	task := factPreviewTask(f, []map[string]any{{"表达限制": "不能省略包装条件"}})
	state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{MaxChars: 220})
	if err != nil {
		t.Fatal(err)
	}
	state.Surface, state.Phase, state.BaselineJSON = factPreviewSurface, "body", textTestBaseline()
	state.Facts = task.FactScope.Facts[0].Data
	if err = installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	messages, err := renderCycleTextMessages(&state)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(messages)
	for _, want := range []string{f.Value, f.ForbiddenWording, f.SafeRewrite, "不能省略包装条件", "不提红线词"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing boundary %q", want)
		}
	}
	for _, forbidden := range []string{"321987", "fact_id", "version_no", "tenant_id", "一次性包装已拆开也能退", "二级鸭肉资料"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("leaked unrelated material: %s", forbidden)
		}
	}
	if state.Config.TTS != nil || state.Delivery != nil {
		t.Fatal("preview must not attach live audio")
	}
	if state.Task().ProductMode == "lecture" {
		t.Fatal("preview generation must remain normal mainline")
	}
}

func TestSelectedPreviewFactRejectsScopeVersionAndInactive(t *testing.T) {
	f := model.LiveAgentPlanFact{ID: 5, TenantID: 14, PlanID: 4, VersionNo: 2, Status: "active"}
	for _, scope := range [][5]int64{{14, 4, 5, 2, 1}, {15, 4, 5, 2, 0}, {14, 3, 5, 2, 0}, {14, 4, 6, 2, 0}, {14, 4, 5, 1, 0}} {
		_, ok := selectedPreviewFact([]model.LiveAgentPlanFact{f}, scope[0], scope[1], scope[2], scope[3])
		if ok != (scope[4] == 1) {
			t.Fatal("scope/version mismatch", scope)
		}
	}
	f.Status = "deleted"
	if _, ok := selectedPreviewFact([]model.LiveAgentPlanFact{f}, 14, 4, 5, 2); ok {
		t.Fatal("deleted fact accepted")
	}
}

func TestFactSpeechPreviewNeverPersistsAngleAssets(t *testing.T) {
	state := anchorcycle.State{TenantID: 14, PlanID: 4, Surface: factPreviewSurface}
	if cycleAnglePlanPersistent(&state) {
		t.Fatal("preview changes live direction library")
	}
	state.Surface = "formal_live"
	if !cycleAnglePlanPersistent(&state) {
		t.Fatal("normal live direction persistence regressed")
	}
}
