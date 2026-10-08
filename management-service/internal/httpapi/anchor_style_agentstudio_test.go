package httpapi

import (
	"strings"
	"testing"
)

func TestRenderAnchorStyleWheelContinuationPromptWithFeedback(t *testing.T) {
	task := anchorStyleWheelTask{SellingPoint: "健康品质", Angle: "老人家庭日常用油"}
	prompt := renderAnchorStyleWheelContinuationPrompt(task, 300, "称呼偏少，下一段自然增加哥哥姐姐们，不要连续堆叠。")
	for _, want := range []string{"健康品质 × 老人家庭日常用油", "最多300字", "# 本轮回灌提示词", "称呼偏少"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
	for _, unwanted := range []string{"继续。", "下一段讲", "只输出主播"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("prompt contains obsolete wrapper %q: %s", unwanted, prompt)
		}
	}
}

func TestAppendAnchorStyleFeedbackPromptEmptyIsNoop(t *testing.T) {
	if got := appendAnchorStyleFeedbackPrompt("基础提示", "  "); got != "基础提示" {
		t.Fatalf("empty feedback changed prompt: %q", got)
	}
}

func TestUnlimitedContinuationOmitsCharacterLimit(t *testing.T) {
	task := anchorStyleWheelTask{SellingPoint: "价格算账", Angle: "家庭用量"}
	prompt := renderAnchorStyleWheelContinuationPrompt(task, 0, "")
	if prompt != "价格算账 × 家庭用量" {
		t.Fatalf("unlimited continuation must be the exact wheel cell, got: %q", prompt)
	}
	if strings.Contains(prompt, "最多") || strings.Contains(prompt, "字数") {
		t.Fatalf("unlimited prompt contains a character limit: %s", prompt)
	}
}
