package decisionexecutor

import (
	"strings"
	"testing"
)

func TestArbitrateAnswerClaimsDropsUnsupportedPersonRecommendation(t *testing.T) {
	answer := "小王老乡，两个人吃选450g半只装就行。2人啊，这个我还真不敢拍胸脯。"
	facts := "F056 规格=900g(一整只)\nF079 规格=450g(半只)"
	if got := arbitrateAnswerClaims(answer, "2人吃几号", facts); got != "" {
		t.Fatalf("unsupported recommendation should be dropped, got %q", got)
	}
}

func TestArbitrateAnswerClaimsKeepsSupportedRegionAndDropsDenial(t *testing.T) {
	answer := "小王老乡，上海包邮，不用补空运费。上海这个范围我还没确认。"
	facts := "F091 活动=包邮\nSYS-REGION 上海不属于事实里的偏远地区名单；当前活动包邮，因此上海包邮，不需要补空运费。"
	got := arbitrateAnswerClaims(answer, "上海包邮吗", facts)
	if strings.Contains(got, "没确认") || !strings.Contains(got, "上海包邮") {
		t.Fatalf("supported region should win, got %q", got)
	}
}
