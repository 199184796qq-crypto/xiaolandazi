package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestBuildFactConflictPromptIncludesAllContext(t *testing.T) {
	existing := []model.LiveAgentPlanFact{
		{Category: "audience", Key: "适合人数", Value: "适合3-4个人"},
		{Category: "location", Key: "地址信息", Value: "滨江中路368号"},
	}
	links := []model.LiveAgentPlanProductLink{
		{LinkKey: "1号链接", ProductName: "10人餐", Audience: "6人", DailyPrice: "337"},
	}
	candidate := model.LiveAgentPlanFactCandidate{
		Category: "audience", Key: "适合人数", Value: "适合6人",
	}

	prompt := buildFactConflictPrompt(existing, links, candidate)

	for _, want := range []string{
		"现有事实",
		"audience/适合人数",
		"适合3-4个人",
		"商品卡",
		"1号链接",
		"适用人数=6人",
		"新输入的事实",
		"适合6人",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt 缺少 %q；完整：\n%s", want, prompt)
		}
	}
}

func TestBuildFactConflictPromptEmptyLinks(t *testing.T) {
	existing := []model.LiveAgentPlanFact{
		{Category: "service", Key: "堂食", Value: "仅堂食"},
	}
	candidate := model.LiveAgentPlanFactCandidate{Category: "service", Key: "堂食", Value: "可外带"}

	prompt := buildFactConflictPrompt(existing, nil, candidate)
	if strings.Contains(prompt, "商品卡") {
		t.Errorf("无商品卡时不应输出商品卡段落")
	}
	if !strings.Contains(prompt, "仅堂食") || !strings.Contains(prompt, "可外带") {
		t.Errorf("现有事实和新事实都应包含")
	}
}
