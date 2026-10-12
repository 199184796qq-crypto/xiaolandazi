package httpapi

import (
	"testing"

	"livecompanion/management/internal/contentpools"
)

func TestFactsChangedAndSourceIDsChanged(t *testing.T) {
	changed := map[string]bool{"price": true}

	// nil 保守：全部视为变化（旧调用/测试行为不变）
	if !factsChanged([]contentpools.Fact{{ID: "price"}}, nil) {
		t.Error("nil changed 应保守返回 true")
	}
	if !sourceIDsChanged([]string{"price"}, nil) {
		t.Error("nil changed 应保守返回 true")
	}

	// 命中变化 fact
	if !factsChanged([]contentpools.Fact{{ID: "price"}}, changed) {
		t.Error("包含变化 fact 应返回 true")
	}
	if !sourceIDsChanged([]string{"taste", "price"}, changed) {
		t.Error("Sources 含变化 ID 应返回 true")
	}

	// 未命中变化 fact
	if factsChanged([]contentpools.Fact{{ID: "taste"}}, changed) {
		t.Error("不含变化 fact 应返回 false")
	}
	if sourceIDsChanged([]string{"taste"}, changed) {
		t.Error("Sources 不含变化 ID 应返回 false")
	}
}

func TestChangedFactDetection(t *testing.T) {
	// 模拟 buildContentPools 里 changed 的计算逻辑，验证三种情况都标记。
	prev := map[string]contentpools.Fact{
		"keep":   {ID: "keep", Text: "不变"},
		"edit":   {ID: "edit", Text: "旧内容"},
		"delete": {ID: "delete", Text: "要删除"},
	}
	facts := []contentpools.Fact{
		{ID: "keep", Text: "不变"},
		{ID: "edit", Text: "新内容"},
		{ID: "new", Text: "新增"},
	}
	factsByID := map[string]bool{}
	for _, f := range facts {
		factsByID[f.ID] = true
	}
	changed := map[string]bool{}
	for _, f := range facts {
		if old, ok := prev[f.ID]; !ok || old.Text != f.Text {
			changed[f.ID] = true
		}
	}
	for id := range prev {
		if !factsByID[id] {
			changed[id] = true
		}
	}
	// keep 不变不标记；edit(改) new(增) delete(删) 都标记
	if changed["keep"] {
		t.Error("未变化的 fact 不应标记")
	}
	if !changed["edit"] || !changed["new"] || !changed["delete"] {
		t.Errorf("变化/新增/删除都应标记，实际 %v", changed)
	}
}
