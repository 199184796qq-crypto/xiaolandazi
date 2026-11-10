package danmakutest

import (
	"livecompanion/management/internal/model"
	"testing"
)

func TestFocusPriceExcludesUnaskedPropertiesWithoutHidingRecall(t *testing.T) {
	links := []Link{{ID: 11, Name: "1"}, {ID: 22, Name: "2号链接"}}
	group := Group{Ask: Ask{Text: "1号多少钱", Subjects: []string{"1"}, Attribute: "价格"}, Hits: []Hit{
		{ID: 1, Attribute: "1号链接价格", Content: "87.6", Original: "价格：87.6"},
		{ID: 2, Attribute: "1号链接门店价", Content: "95.6元"},
		{ID: 3, Attribute: "门店价", Content: "一只加上运费后一百多"},
		{ID: 4, Attribute: "1号链接礼品包装费", Content: "额外加5元", Conditions: "要礼品包装"},
		{ID: 5, Attribute: "1号链接规格", Content: "900g"},
		{ID: 6, Attribute: "1号链接适用口味", Content: "麻辣和五香"},
		{ID: 7, Attribute: "默认规格", Content: "默认整只"},
		{ID: 8, Attribute: "1号链接商品名", Content: "杨鸭子"},
	}}
	selected, resolved := FocusFacts(group, links, nil)
	if !resolved || len(selected) != 1 || selected[0].ID != 1 || len(group.Hits) != 8 {
		t.Fatal(selected, resolved, group)
	}
	group.Ask.Attribute = "门店价"
	selected, resolved = FocusFacts(group, links, nil)
	if !resolved || len(selected) != 2 || selected[0].ID != 2 || selected[1].ID != 3 {
		t.Fatal("explicit store price lost", selected, resolved)
	}
	group.Ask.Attribute = "礼品包装费"
	selected, resolved = FocusFacts(group, links, nil)
	if !resolved || len(selected) != 1 || selected[0].ID != 4 || selected[0].Conditions != "要礼品包装" {
		t.Fatal("explicit fee/condition lost", selected, resolved)
	}
}

func TestFocusRetainsConditionsComparisonAndUnknownVocabulary(t *testing.T) {
	links := []Link{{ID: 11, Name: "1"}, {ID: 22, Name: "2"}}
	group := Group{Ask: Ask{Text: "1号2号价格有什么区别", Attribute: "价格", Subjects: []string{"1", "2"}}, Hits: []Hit{
		{ID: 1, Attribute: "1号链接价格", Content: "87.6元", Scope: "1", Conditions: "满两只", Exceptions: "礼盒除外", ConditionStatus: "待核对"},
		{ID: 2, Attribute: "2号链接价格", Content: "98元", Scope: "2"},
		{ID: 3, Attribute: "1号链接门店价", Content: "95.6元"},
	}}
	selected, resolved := FocusFacts(group, links, nil)
	if !resolved || len(selected) != 2 || selected[0].Conditions != "满两只" || selected[0].Exceptions != "礼盒除外" || selected[1].Scope != "2" {
		t.Fatal(selected, resolved)
	}
	group.Ask.Attribute = "到手总价"
	selected, resolved = FocusFacts(group, links, nil)
	if resolved || len(selected) != 3 {
		t.Fatal("unmapped semantic candidates silently discarded", selected, resolved)
	}
}

func TestFocusCanonicalNamesDoNotConflateOtherProperties(t *testing.T) {
	links := []Link{{ID: 11, Name: "1"}, {ID: 22, Name: "2号链接"}}
	stores := []model.MaterialStoreObject{{Name: "王家店"}}
	for _, tc := range [][2]string{{"1号链接价格", "价格"}, {"第2号链接：价格", "价格"}, {"王家店·门店价", "门店价"}, {"1号链接门店价", "门店价"}, {"1斤规格", "1斤规格"}, {"3号链接价格", "3号链接价格"}} {
		if got := AttributeName(tc[0], links, stores); got != tc[1] {
			t.Fatal(tc, got)
		}
	}
}
