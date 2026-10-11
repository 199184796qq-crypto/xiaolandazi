package contentpools

import (
	"strings"
	"testing"
)

func TestProductContextCrossCategoryScopeUpdatesAndSecondaryIsolation(t *testing.T) {
	for _, category := range []string{"熟食", "服装", "家电", "护肤品", "课程", "咖啡豆"} {
		t.Run(category, func(t *testing.T) {
			set := Set{Sources: Sources{Products: []Product{{ID: 7, Link: "1", Name: category}, {ID: 8, Link: "2", Name: "另一商品"}}, Rows: []SourceRow{{ID: "primary", Area: "knowledge", Fields: map[string]string{"分类": "usage"}}, {ID: "secondary", Area: "lecture", Fields: map[string]string{"分类": "usage"}}, {ID: "delivery", Area: "knowledge", Fields: map[string]string{"分类": "fulfillment"}}, {ID: "benefit", Area: "benefits"}, {ID: "other", Area: "products"}}}, Pools: []Pool{{Facts: []Fact{{ID: "primary", Text: "本品使用限制", Sources: []string{"primary"}, Products: []int64{7}}, {ID: "secondary", Text: "二级哨兵", Sources: []string{"secondary"}, Products: []int64{7}}, {ID: "delivery", Text: "物流哨兵", Sources: []string{"delivery"}, Products: []int64{7}}, {ID: "benefit", Text: "福利哨兵", Sources: []string{"benefit"}, Products: []int64{7}}, {ID: "other", Text: "另一品用途", Sources: []string{"other"}, Products: []int64{8}}}}}}
			facts := PrimaryProductFacts(set, []int64{7})
			if len(facts) != 1 || facts[0].Text != "本品使用限制" {
				t.Fatal(facts)
			}
			text := ProductContext(set)
			if !strings.Contains(text, "1号链接："+category) || !strings.Contains(text, "2号链接：另一品用途") || strings.Contains(text, "哨兵") {
				t.Fatal(text)
			}
			set.Pools[0].Facts[0].Text = "更新后的本品限制"
			if text = ProductContext(set); !strings.Contains(text, "更新后的本品限制") || strings.Contains(text, "本品使用限制") {
				t.Fatal(text)
			}
		})
	}
}
