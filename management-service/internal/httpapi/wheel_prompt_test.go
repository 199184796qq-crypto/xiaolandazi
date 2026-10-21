package httpapi

import (
	"strings"
	"testing"
)

// 轮盘提示词必须按四层组织：必说/可说/不提/改写。P0 词表要落在「不提」下，
// 且「改写」给出禁说怎么改；否则角度里会写出极限词（实测「极致性价比」）。
func TestWheelPromptHasFourLayers(t *testing.T) {
	out, err := renderWheelFourLayerPrompt("母版正文", `- 极限词：最好、最佳、极致`, "行业红线：不承诺无隐形消费", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# 必说（本次任务）", "母版正文", "# 可说（事实包）", "fact_package",
		"# 不提（P0 红线", "极限词", "行业红线：不承诺无隐形消费", "# 改写（禁说 → 改说）"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in wheel prompt", want)
		}
	}
	// 「不提」必须排在「改写」之前，语义顺序不能反。
	if strings.Index(out, "# 不提") > strings.Index(out, "# 改写") {
		t.Fatal("P0 red lines must come before rewrite guidance")
	}
	if _, err = renderWheelFourLayerPrompt("  ", "", "", ""); err == nil {
		t.Fatal("empty mother must be rejected")
	}
}
