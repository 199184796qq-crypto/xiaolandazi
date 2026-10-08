package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	appdb "livecompanion/management/internal/db"
)

func TestEmptyPoolMustUseCompleteGeneration(t *testing.T) {
	for _, raw := range []string{"", `{"卖点列表":[],"事实补充建议":[]}`, `{"卖点列表":[{"卖点名":"物流","角度":[]}]}`, `{"卖点列表":[{"卖点名":"物流","角度":["  "]}]}`} {
		if !emptySellingPointPool(raw) {
			t.Fatal("empty pool incorrectly reused", raw)
		}
	}
	for _, raw := range []string{`{"卖点列表":[{"卖点名":"物流","角度":["京东物流"]}]}`, `broken json`} {
		if emptySellingPointPool(raw) {
			t.Fatal("nonempty or corrupt pool considered empty", raw)
		}
	}
}
func TestIncrementalWheelPreservesUnrelatedUserEditsAndDoesNotRestoreRemovedFacts(t *testing.T) {
	var existing anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(`{"卖点列表":[{"卖点名":"价格","目标人群":"比较价格","痛点":"核对价格","角度":["原价87.6"]},{"卖点名":"人工主题","目标人群":"用户自定义人群","痛点":"用户原话","角度":["用户编辑的京东物流"]}],"事实补充建议":[]}`), &existing); err != nil {
		t.Fatal(err)
	}
	p := wheelPatch{Updates: []wheelPatchUpdate{{ID: 1, Point: nil}}, Additions: []anchorStyleSellingPointWheelItem{}}
	merged, err := mergeWheelPatch(existing, p, nil)
	if err != nil || len(merged.Items) != 1 || !reflect.DeepEqual(merged.Items[0], existing.Items[1]) || len(existing.Items) != 2 {
		t.Fatal("manual content mutated", merged, existing, err)
	}
	_, err = mergeWheelPatch(existing, wheelPatch{Updates: []wheelPatchUpdate{{ID: 2, Point: &existing.Items[0]}}, Additions: []anchorStyleSellingPointWheelItem{}}, map[int]bool{1: true})
	if err == nil {
		t.Fatal("review modified unaffected user point")
	}
	added, removed := wheelFactDelta("品牌70年\n价格87.6", "品牌70年\n价格79.9")
	if !reflect.DeepEqual(added, []string{"价格79.9"}) || !reflect.DeepEqual(removed, []string{"价格87.6"}) {
		t.Fatal(added, removed)
	}
}
func TestProductScopeAssetReuseAvoidsModelAndRevalidatesCachedOutput(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	generated, saved := 0, 0
	err := cachedProductScope(context.Background(), scope, facts, tasks, func() (string, error) { return validScopeResponse, nil }, func(string) error { saved++; return nil }, func() (string, error) { generated++; return validScopeResponse, nil })
	if err != nil || generated != 0 || saved != 0 {
		t.Fatal(err, generated, saved)
	}
	err = cachedProductScope(context.Background(), scope, facts, tasks, func() (string, error) { return `{ "facts": [] }`, nil }, func(string) error { return nil }, func() (string, error) { generated++; return validScopeResponse, nil })
	if err == nil || generated != 0 {
		t.Fatal("invalid stored result accepted or silently regenerated", err, generated)
	}
	err = cachedProductScope(context.Background(), scope, facts, tasks, func() (string, error) { return "", appdb.ErrPlanAnalysisAssetNotFound }, func(raw string) error {
		saved++
		if raw != validScopeResponse {
			t.Fatal("raw model response rewritten")
		}
		return nil
	}, func() (string, error) { generated++; return validScopeResponse, nil })
	if err != nil || generated != 1 || saved != 1 {
		t.Fatal(err, generated, saved)
	}
	err = cachedProductScope(context.Background(), scope, facts, tasks, func() (string, error) { return "", errors.New("database unavailable") }, func(string) error { return nil }, func() (string, error) { generated++; return validScopeResponse, nil })
	if err == nil || generated != 1 {
		t.Fatal("database error triggered blind model calls", err, generated)
	}
}
