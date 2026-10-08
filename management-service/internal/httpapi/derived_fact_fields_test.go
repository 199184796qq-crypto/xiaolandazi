package httpapi

import (
	"strings"
	"testing"
)

func TestSafetyRewriteDoesNotDropUnrelatedProductNumbersAndLink(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	original := "# 事实包\n- [商品卡] 链接=1号链接；商品=样品油；规格=5L；价格=129.9；卖点=零胆固醇、高油酸"
	d, err := buildDerivedInput(original, "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyDerivedReplacements(d, replacementsFor(d, "查看商品说明"), "1号链接 5L 129.9", p0, p1, p2); err != nil {
		t.Fatal(err)
	}
	safe := strings.Join(d.lines, "\n")
	for _, value := range []string{"链接=1号链接", "商品=样品油", "规格=5L", "价格=129.9"} {
		if !strings.Contains(safe, value) {
			t.Fatal("lost unrelated fact", value, safe)
		}
	}
	if d.Facts != original {
		t.Fatal("changed original")
	}
}
