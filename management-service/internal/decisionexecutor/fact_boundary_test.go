package decisionexecutor

import (
	"strings"
	"testing"
)

func TestTranslateFactBoundary(t *testing.T) {
	// 速度类边界应翻译出时效禁词
	terms := translateFactBoundary("介绍物流时，不拿其他快递品牌作速度类比")
	found := false
	for _, term := range terms {
		if term == "时效快" {
			found = true
		}
	}
	if !found {
		t.Fatalf("速度类边界应翻译出'时效快'，得到 %v", terms)
	}
	// 到货时间类边界也应翻译出时效禁词
	terms2 := translateFactBoundary("不说到货时间承诺")
	found2 := false
	for _, term := range terms2 {
		if term == "今天下单明天到" {
			found2 = true
		}
	}
	if !found2 {
		t.Fatalf("到货时间类边界应翻译出'今天下单明天到'，得到 %v", terms2)
	}
	// 门店地域外推类边界应翻译出地域禁词
	terms3 := translateFactBoundary("不能说全国五十多家门店、门店遍布全国，门店都在南充")
	found3 := false
	for _, term := range terms3 {
		if term == "全国五十多家" {
			found3 = true
		}
	}
	if !found3 {
		t.Fatalf("地域外推类边界应翻译出'全国五十多家'，得到 %v", terms3)
	}
}

func TestFactBoundaryRiskTerms(t *testing.T) {
	ctx := "1号链接：使用京东物流；禁说=不拿速度类比，例如'顺丰级别的速度'；安全改写=介绍京东物流\n2号链接：别的"
	terms := factBoundaryRiskTerms(ctx)
	joined := strings.Join(terms, " ")
	// 引号内具体词
	if !strings.Contains(joined, "顺丰级别的速度") {
		t.Fatalf("应提取引号内禁说词，得到 %v", terms)
	}
	// 分类翻译出的时效禁词
	if !strings.Contains(joined, "时效快") {
		t.Fatalf("应翻译出时效禁词'时效快'，得到 %v", terms)
	}
	// 无禁说的文本应返回空
	if got := factBoundaryRiskTerms("普通文本"); len(got) != 0 {
		t.Fatalf("无禁说应返回空，得到 %v", got)
	}
}
