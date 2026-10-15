package httpapi

import (
	"strings"
	"testing"
)

// 清单挖掘的行业必查层注入：有行业包时注入带行业名的维度块，无包时占位必须消干净。
func TestInjectIndustryFactChecklist(t *testing.T) {
	mother := "# 商品事实必查母版\n\n{{INDUSTRY_MUST_CHECK}}\n\n## 输出协议\n"

	withPack := injectIndustryFactChecklist(mother, "餐饮到店", "- 口味与招牌菜\n- 门店环境")
	if strings.Contains(withPack, "{{INDUSTRY_MUST_CHECK}}") {
		t.Fatalf("有行业包时占位应被替换: %s", withPack)
	}
	if !strings.Contains(withPack, "本方案行业已判定为「餐饮到店」") || !strings.Contains(withPack, "口味与招牌菜") {
		t.Fatalf("有行业包时应注入行业名与维度内容: %s", withPack)
	}
	if !strings.Contains(withPack, "## 输出协议") {
		t.Fatalf("注入不应破坏母版其余部分: %s", withPack)
	}

	withoutPack := injectIndustryFactChecklist(mother, "", "")
	if strings.Contains(withoutPack, "{{INDUSTRY_MUST_CHECK}}") {
		t.Fatalf("无行业包时占位也应被清掉: %s", withoutPack)
	}
	if !strings.Contains(withoutPack, "## 输出协议") {
		t.Fatalf("清占位不应破坏母版其余部分: %s", withoutPack)
	}

	// 占位不存在的母版（如旧版）应原样返回。
	untouched := injectIndustryFactChecklist("# 无占位母版", "餐饮到店", "- 口味")
	if untouched != "# 无占位母版" {
		t.Fatalf("无占位母版应原样返回: %s", untouched)
	}
}
