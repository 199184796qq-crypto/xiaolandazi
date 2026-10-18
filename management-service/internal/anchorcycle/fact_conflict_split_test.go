package anchorcycle

import (
	"reflect"
	"testing"
)

func TestSplitConflictingFacts_SingleOrEmpty(t *testing.T) {
	if got := SplitConflictingFacts(nil); len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("nil 应返回 1 个空组，got %#v", got)
	}
	one := []HookFact{{Data: "- [flavor/口味] 麻辣五香。"}}
	got := SplitConflictingFacts(one)
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("单条不应拆，got %#v", got)
	}
}

func TestSplitConflictingFacts_ByCategory(t *testing.T) {
	facts := []HookFact{
		{Data: "- [price/线下价] 线下门店价95.6元。"},
		{Data: "- [flavor/口味] 麻辣五香。"},
		{Data: "- [audience/人数] 适合6人。"},
		{Data: "- [craft/工艺] 老卤慢炖4小时。"},
	}
	groups := SplitConflictingFacts(facts)
	// 4 个不同分类，应拆成 4 组。
	if len(groups) != 4 {
		t.Fatalf("4 个不同分类应拆 4 组，got %d 组 %#v", len(groups), groups)
	}
	// 每组恰一条，且分类保序。
	for i, g := range groups {
		if len(g) != 1 {
			t.Fatalf("组 %d 应恰 1 条，got %d", i, len(g))
		}
	}
	if CategoryFromFactText(groups[0][0].Data) != "price" ||
		CategoryFromFactText(groups[3][0].Data) != "craft" {
		t.Fatalf("分类保序失败 %#v", groups)
	}
}

func TestSplitConflictingFacts_NumberConflictWithinCategory(t *testing.T) {
	facts := []HookFact{
		{Data: "- [audience/适用] 适合6人。"},
		{Data: "- [audience/适用] 适合3到4人。"},
	}
	groups := SplitConflictingFacts(facts)
	if len(groups) != 2 {
		t.Fatalf("6人 vs 3-4人 数值对立，应拆 2 组，got %d 组 %#v", len(groups), groups)
	}
}

func TestSplitConflictingFacts_SameCategoryNoConflict(t *testing.T) {
	facts := []HookFact{
		{Data: "- [audience/适用] 适合6人。"},
		{Data: "- [audience/适用] 上限6人，3到4人也合适。"}, // 区间包含关系，不算对立
	}
	groups := SplitConflictingFacts(facts)
	if len(groups) != 1 {
		t.Fatalf("区间包含不应拆，got %d 组 %#v", len(groups), groups)
	}
}

func TestSplitConflictingFacts_GenericLast(t *testing.T) {
	facts := []HookFact{
		{Data: "- [flavor/口味] 麻辣。"},
		{Data: "开袋即可食用。"}, // 无分类，通用事实
		{Data: "- [price/价格] 95.6元。"},
	}
	groups := SplitConflictingFacts(facts)
	// flavor、price、generic 三组，generic 放最后。
	if len(groups) != 3 {
		t.Fatalf("应拆 3 组，got %d", len(groups))
	}
	last := groups[len(groups)-1]
	if len(last) != 1 || CategoryFromFactText(last[0].Data) != "" {
		t.Fatalf("通用事实应独占最后一组，got %#v", last)
	}
}

func TestExtractNumbers(t *testing.T) {
	cases := []struct {
		text string
		want []float64
	}{
		{"适合6人。", []float64{6}},
		{"适合3到4人。", []float64{3, 4}},
		{"价格95.6元。", []float64{95.6}},
		{"老卤慢炖4小时。", []float64{4}},
		{"适合3-4人。", []float64{3, 4}},
	}
	for _, c := range cases {
		got := extractNumbers(c.text)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("extractNumbers(%q)=%v want %v", c.text, got, c.want)
		}
	}
}
