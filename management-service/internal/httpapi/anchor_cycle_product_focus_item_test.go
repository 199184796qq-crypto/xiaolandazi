package httpapi

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/anchorcycle"
)

func TestParseCardLinks(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		{"商品卡单链接", "- [商品卡] 链接=1；商品=测试；规格=900g", []string{"1"}},
		{"福利多链接顿号", "- [有效福利] 福利=1:x；链接=1、2、3；赠品=免邮费", []string{"1", "2", "3"}},
		{"福利多链接逗号", "- [有效福利] 福利=1:x；链接=1,2，3；活动=包邮", []string{"1", "2", "3"}},
		{"无链接字段", "- [brand/x] 品牌历史", nil},
	}
	for _, c := range cases {
		got := parseCardLinks(c.line)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v want %v", c.name, got, c.want)
			}
		}
	}
}

func TestApplyProductScopeCache(t *testing.T) {
	modelFacts := []cycleScopeFactInput{
		{ID: 1, Text: "- [flavor/口味] 麻辣鲜香"},
		{ID: 2, Text: "- [price/价格] 日常价87.6"},
	}
	tasks := []anchorcycle.Task{
		{SellingPoint: "核心工艺", Angle: "先卤后炸", Key: "核心工艺×先卤后炸"},
		{SellingPoint: "价格算账", Angle: "对比算账", Key: "价格算账×对比算账"},
	}
	cache := productScopeCache{
		Facts: []productScopeCacheFact{
			{ID: 1, Links: []string{"1", "2"}, Shared: boolPtr(false)},
			{ID: 2, Links: []string{}, Shared: boolPtr(true)},
		},
		Tasks: []productScopeCacheTask{
			{Key: "核心工艺×先卤后炸", Links: []string{}, Shared: boolPtr(true), Comparison: boolPtr(false)},
			{Key: "价格算账×对比算账", Links: []string{"1", "2"}, Shared: boolPtr(false), Comparison: boolPtr(true)},
		},
	}
	raw, _ := json.Marshal(cache)
	scope := &anchorcycle.ProductFocus{}
	if err := applyProductScopeCache(string(raw), modelFacts, tasks, scope); err != nil {
		t.Fatal(err)
	}
	if len(scope.Facts) != 2 {
		t.Fatalf("facts 应 2 条，got %d", len(scope.Facts))
	}
	if scope.Facts[0].Shared || len(scope.Facts[0].Links) != 2 {
		t.Fatalf("fact0 归属错误: %+v", scope.Facts[0])
	}
	if !scope.Facts[1].Shared {
		t.Fatalf("fact1 应为 shared: %+v", scope.Facts[1])
	}
	st, ok := scope.Tasks["价格算账×对比算账"]
	if !ok || !st.Comparison || st.Shared {
		t.Fatalf("task 归属错误: %+v", scope.Tasks)
	}
}

func boolPtr(v bool) *bool { return &v }
