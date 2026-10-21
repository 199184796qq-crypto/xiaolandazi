package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func taskPreparationFixture() (liveAnchorScheme, model.LiveDerivedSafetyAsset) {
	facts := "- [商品卡] 链接=1号链接；商品=十人餐\n- [商品卡] 链接=2号链接；商品=六人餐"
	focus := &anchorcycle.ProductFocus{Policy: anchorcycle.ProductPolicy{Version: "product-focus-v3"}, FactsSHA: derivedHash(facts),
		Products: []anchorcycle.FocusProduct{
			{LinkKey: "1号链接", Roles: []string{"ordinary"}, SellingPoints: []anchorcycle.FocusSellingPoint{{ID: 11, Name: "套餐", Angles: []anchorcycle.FocusSellingAngle{{Angle: "配置"}}}}},
			{LinkKey: "2号链接", Roles: []string{"main"}, SellingPoints: []anchorcycle.FocusSellingPoint{{ID: 12, Name: "套餐", Angles: []anchorcycle.FocusSellingAngle{{Angle: "配置"}}}}},
		}, Facts: []anchorcycle.ScopedFact{{Text: "只属于一号的事实", Links: []string{"1号链接"}}},
		Tasks: map[string]anchorcycle.ScopedTask{"old-internal-key": {Links: []string{"2号链接"}}}, CurrentLink: "2号链接", CurrentCycle: 50}
	return liveAnchorScheme{Config: anchorcycle.Config{Rolling: true, MaxChars: 300, PointGap: 2, CombinationGap: 8}, ProductFocus: focus},
		model.LiveDerivedSafetyAsset{SafeFacts: facts, SafeWheelJSON: `{"卖点列表":[{"卖点名":"旧轮盘任务","角度":["旧角度"]}]}`}
}

func TestSavedTaskPreparationRepairsOnlyProgramIndexesAndKeepsSnapshot(t *testing.T) {
	scheme, safety := taskPreparationFixture()
	before, _ := json.Marshal(scheme)
	server := &Server{}      // no database/model/Core dependencies on this path
	for i := 0; i < 2; i++ { // save preflight and startup execute the same path
		state, err := server.prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, scheme, safety)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status != "ready" || len(state.Queue) != 2 || len(state.ProductFocus.Tasks) != 2 {
			t.Fatalf("bad prepared state: %+v", state)
		}
		for _, task := range state.Queue {
			scope, ok := state.ProductFocus.Tasks[task.Key]
			if !ok || len(scope.Links) != 1 || scope.Links[0] != task.LinkKey || scope.Shared || scope.Comparison {
				t.Fatalf("wrong scope for %+v: %+v", task, scope)
			}
		}
		if state.ProductFocus.Facts[0].Text != "只属于一号的事实" || state.ProductFocus.Facts[0].Links[0] != "1号链接" {
			t.Fatal("fact scope changed")
		}
		if state.ProductFocus.CurrentCycle != 1 {
			t.Fatal("old run cursor reused")
		}
		state.ProductFocus.Facts[0].Text = "must not persist"
	}
	after, _ := json.Marshal(scheme)
	if string(before) != string(after) {
		t.Fatal("save/start preparation mutated the saved version")
	}
}

func TestSavedTaskPreparationChecksEveryProductWithActualConstructor(t *testing.T) {
	scheme, safety := taskPreparationFixture()
	scheme.ProductFocus.Products[1].SellingPoints[0].Angles = nil
	_, err := (&Server{}).prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, scheme, safety)
	if err == nil || !strings.Contains(err.Error(), "链接2号链接") {
		t.Fatalf("unrunnable second product was accepted: %v", err)
	}
}

func TestSavedTaskPreparationKeepsLegacySharedScopes(t *testing.T) {
	scheme, safety := taskPreparationFixture()
	for i := range scheme.ProductFocus.Products {
		scheme.ProductFocus.Products[i].SellingPoints = nil
	}
	scheme.ProductFocus.Tasks = map[string]anchorcycle.ScopedTask{"旧轮盘任务×旧角度": {Shared: true}}
	state, err := (&Server{}).prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, scheme, safety)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Queue) != 1 || state.Queue[0].Key != "旧轮盘任务×旧角度" || !state.ProductFocus.Tasks[state.Queue[0].Key].Shared {
		t.Fatal("legacy scope changed")
	}
	scheme.ProductFocus.Tasks["旧轮盘任务×旧角度"] = anchorcycle.ScopedTask{Links: []string{"1号链接"}}
	if _, err = (&Server{}).prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, scheme, safety); err == nil {
		t.Fatal("legacy unassigned second product was allowed to mix facts")
	}
}

func TestUnknownRuntimeFailureCannotClaimTTSError(t *testing.T) {
	for _, cause := range []error{nil, context.DeadlineExceeded} {
		failure := publicLiveFailure(10, cause)
		if strings.Contains(failure.Message, "声音生成失败") || failure.Stage == "声音合成" {
			t.Fatal(failure)
		}
		if cause == nil && !strings.Contains(failure.Message, "未提供具体错误详情") {
			t.Fatal("unknown failure must explicitly say details are missing", failure)
		}
		if cause != nil && (!strings.Contains(failure.Message, cause.Error()) || failure.Code != "request_timeout") {
			t.Fatal("actual cause must survive without inventing a TTS origin", failure)
		}
	}
}
