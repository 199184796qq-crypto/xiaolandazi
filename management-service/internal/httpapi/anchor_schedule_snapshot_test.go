package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"livecompanion/management/internal/model"
)

func TestSingleProductScheduleUsesFrozenWeightsWithoutChangingFactPath(t *testing.T) {
	links := []model.LiveAgentPlanProductLink{{LinkKey: "1", Status: "active", SellingPoints: []model.LiveAgentPlanProductSellingPoint{
		{ID: 11, Status: "active", SellingPoint: "口感", PointWeight: 9, Angles: []model.LiveAgentPlanProductSellingAngle{{Angle: "层次", Weight: 2}}},
		{ID: 12, Status: "active", SellingPoint: "工艺", PointWeight: 1, Angles: []model.LiveAgentPlanProductSellingAngle{{Angle: "步骤", Weight: 1}}},
	}}}
	scheme, safety := taskPreparationFixture()
	scheme.ProductFocus = nil
	scheme.SingleProductSchedule = singleProductSchedule(links)
	if scheme.SingleProductSchedule == nil {
		t.Fatal("single product preferences were not captured")
	}
	raw, _ := json.Marshal(scheme)
	links[0].SellingPoints[0].PointWeight = 1 // live edits cannot rewrite a saved preference
	state, err := (&Server{}).prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, scheme, safety)
	if err != nil || state.ProductFocus != nil || len(state.Queue) != 2 || state.Status != "ready" {
		t.Fatal("single scheduling should not create a fact-scope object", err)
	}
	weights := map[string]int{}
	for _, task := range state.Queue {
		weights[task.SellingPoint] = task.PointWeight
		if task.FactScope != nil || task.LinkKey != "1" {
			t.Fatal("scheduling changed the existing fact path", task)
		}
	}
	if weights["口感"] != 9 || weights["工艺"] != 1 {
		t.Fatal("saved point weights were lost", weights)
	}
	after, _ := json.Marshal(scheme)
	if string(raw) != string(after) {
		t.Fatal("runtime preparation mutated the immutable saved input")
	}
	links = append(links, model.LiveAgentPlanProductLink{LinkKey: "2", Status: "active"})
	if singleProductSchedule(links) != nil {
		t.Fatal("multi-product snapshot was mistaken for a single product")
	}
}
