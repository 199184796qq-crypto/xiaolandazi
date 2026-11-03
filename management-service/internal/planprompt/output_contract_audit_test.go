package planprompt

import (
	"strings"
	"testing"
)

func TestAuditOutputContractSectionBoundaries(t *testing.T) {
	for _, heading := range []string{"【输出格式】", "【输出格式要求】", "【输出格式（系统保留，不可删除或修改）】", "## 输出格式", "输出格式："} {
		for _, next := range []string{"【业务要求】", "## 业务要求", "第二步：写作要求"} {
			got := StripOutputFormatSection("保留前文\n" + heading + "\n旧格式\n" + next + "\n保留后文")
			if strings.Contains(got, "旧格式") || !strings.Contains(got, "保留后文") {
				t.Errorf("wrong section boundary: %q", got)
			}
		}
	}
	inline := "【输出格式】只是商品上印的文字，不是标题。\n正常业务。"
	if got := StripOutputFormatSection(inline); got != inline {
		t.Fatal("inline fact removed", got)
	}
}
