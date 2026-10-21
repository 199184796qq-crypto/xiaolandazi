package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/contentguard"
)

// 命中的角度只回喂这一条 + 事实 + 红线 + 改说，要求 2 个备选。
// 提示词必须短：这里只换一句话，塞全量母版会让模型重写整个轮盘。
func TestWheelRewritePromptIsShortAndFourLayer(t *testing.T) {
	messages := renderWheelRewriteMessages("价格算账与优惠", "对比两档套餐的绝对节省金额", []string{"绝对"}, "- 绝对词与极限词：最好、最佳、绝对", 0)
	if len(messages) != 2 {
		t.Fatalf("want system+user, got %d", len(messages))
	}
	system := messages[0].Content
	for _, want := range []string{"# 必说", "# 不提", "# 改写", "绝对", "{\"备选\""} {
		if !strings.Contains(system, want) {
			t.Fatalf("missing %q in rewrite system prompt", want)
		}
	}
	if len([]rune(system)) > 900 {
		t.Fatalf("rewrite prompt too long: %d chars", len([]rune(system)))
	}
	if !strings.Contains(messages[1].Content, "对比两档套餐的绝对节省金额") || !strings.Contains(messages[1].Content, "违规词") {
		t.Fatal("user message must carry the original angle and the hit terms")
	}
	// 改写时不注入事实：给了事实模型就抄事实，主题和商品都会跑偏。
	if strings.Contains(system, "团购价337") {
		t.Fatal("facts must not be injected into the rewrite prompt")
	}
}

func TestWheelRewriteCandidatePicking(t *testing.T) {
	policy, err := contentguard.Compile(wheelP0Fixture)
	if err != nil {
		t.Fatal(err)
	}
	raw := "```json\n{\"备选\":[\"这是最好的套餐\",\"核对套餐的实惠程度\"]}\n```"
	candidates := wheelRewriteCandidates(raw)
	if len(candidates) != 2 {
		t.Fatalf("candidates not parsed: %#v", candidates)
	}
	if picked := pickWheelRewriteCandidate(candidates, "这是最好的套餐", policy); picked != "核对套餐的实惠程度" {
		t.Fatalf("must skip the candidate that still hits P0, got %q", picked)
	}
	if picked := pickWheelRewriteCandidate([]string{"这是最好的套餐"}, "这是最好的套餐", policy); picked != "" {
		t.Fatalf("all-hit must return empty, got %q", picked)
	}
	if hits := p0PolicyHits("这是最好的套餐", policy); len(hits) == 0 {
		t.Fatal("P0 hit not detected")
	}
}
