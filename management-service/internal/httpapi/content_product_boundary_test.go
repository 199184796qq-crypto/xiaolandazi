package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	"strings"
	"testing"
)

func TestPrimaryProductUseBoundaryIsScopedAndInvalidatesDirections(t *testing.T) {
	set := taskFactPoolFixture()
	set.Sources.Rows = append(set.Sources.Rows, contentpools.SourceRow{ID: "use", Area: "knowledge", Fields: map[string]string{"分类": "usage"}}, contentpools.SourceRow{ID: "secondary", Area: "lecture", Fields: map[string]string{"分类": "usage"}})
	set.Pools[0].Facts = append(set.Pools[0].Facts, contentpools.Fact{ID: "use", Text: "成品开袋直接吃", Sources: []string{"use"}, Products: []int64{7}}, contentpools.Fact{ID: "secondary", Text: "原料可以炖汤", Sources: []string{"secondary"}, Products: []int64{7}})
	set.SourceSHA = set.Sources.SHA()
	tasks, _, err := contentPoolRuntime(set, anchorcycle.ProductPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(tasks[0].FactScope.ProductBoundary)
	if !strings.Contains(string(data), "开袋直接吃") || strings.Contains(string(data), "炖汤") || strings.Contains(string(data), "京东物流") {
		t.Fatal(string(data))
	}
	state := cycleTestState(t)
	state.Queue[0].FactScope = &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{}, ProductBoundary: tasks[0].FactScope.ProductBoundary}
	before := state.AnglePlanKey()
	state.Queue[0].FactScope.ProductBoundary = append(state.Queue[0].FactScope.ProductBoundary, anchorcycle.HookFact{ID: "current", Data: "按当前使用条件"})
	if state.AnglePlanKey() == before {
		t.Fatal("updated product-use facts reused stale directions")
	}
}
