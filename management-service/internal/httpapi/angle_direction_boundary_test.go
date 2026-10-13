package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
)

func TestFilterAngleDirectionBoundaryViolations(t *testing.T) {
	evidence := "老卤汤慢炖4小时，出锅裹上秘制红油高温锁汁定型。原料为八个月土麻鸭。支持不好吃随时退。"

	cases := []struct {
		name    string
		title   string
		action  string
		wantDrop bool
	}{
		{"竞品贬低-优于", "肉质对比", "说明肉质紧实度优于普通速成鸭", true},
		{"竞品贬低-秒杀", "口感", "这口感秒杀其他品牌", true},
		{"技术原理-锁水", "工艺", "解释老卤慢炖如何锁住水分", true},
		{"技术原理-机理", "工艺", "讲作用机理", true},
		{"正常-锁汁", "工艺", "说明出锅裹红油锁汁定型", false},
		{"正常-客观描述", "原料", "说明选用八个月土麻鸭", false},
		{"正常-售后", "售后", "支持不好吃随时退", false},
	}

	for _, tc := range cases {
		cards := []anchorcycle.AngleHook{{Title: tc.title, Action: tc.action}}
		kept, removed := filterAngleDirectionBoundaryViolations(cards, evidence)
		if tc.wantDrop {
			if len(kept) != 0 || len(removed) != 1 {
				t.Errorf("%s: 期望被过滤，实际 kept=%d removed=%d", tc.name, len(kept), len(removed))
			}
		} else {
			if len(kept) != 1 || len(removed) != 0 {
				t.Errorf("%s: 期望保留，实际 kept=%d removed=%v", tc.name, len(kept), removed)
			}
		}
	}
}

func TestUnsupportedDirectionPhrasesRespectsEvidence(t *testing.T) {
	// 事实正文里已出现"秒杀"（极端场景），方向再用"秒杀"不应被判违规。
	evidence := "本店限时秒杀活动"
	if phrases := unsupportedDirectionPhrases("说明秒杀力度", evidence); len(phrases) != 0 {
		t.Errorf("事实已有秒杀时不应命中，实际 %v", phrases)
	}
	// 事实正文没有"优于"，方向用"优于"应命中。
	if phrases := unsupportedDirectionPhrases("肉质优于普通鸭", "肉质紧实"); len(phrases) == 0 || !strings.Contains(strings.Join(phrases, ","), "优于") {
		t.Errorf("事实无优于时应命中，实际 %v", phrases)
	}
}
