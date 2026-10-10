package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestGenerationFactViewSeparatesInstructionExamples(t *testing.T) {
	data := "价格85.5元；安全改写=按份量解释；禁说=不要编成9元优惠"
	view := generationFactView([]anchorcycle.HookFact{{ID: "knowledge:26", Data: data, Products: []string{"1"}}})
	if view[0]["事实内容"] != "价格85.5元" || len(view[0]["表达指导（不作为商品事实）"].([]map[string]string)) != 2 {
		t.Fatal(view)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "knowledge:26") {
		t.Fatal("internal locator sent as content")
	}
	if got := generationP0View("用户自定义：不得解释内部任务", "ordinary_food"); got != "用户自定义：不得解释内部任务" {
		t.Fatal("custom rules lost")
	}
}
