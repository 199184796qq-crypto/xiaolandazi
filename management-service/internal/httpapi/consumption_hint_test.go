package httpapi

import (
	"testing"

	"livecompanion/management/internal/contentguard"
)

func TestIsConsumptionHintHit(t *testing.T) {
	cases := []struct {
		name string
		text string
		hit  contentguard.Annotation
		want bool // true = 消费提示，放行
	}{
		{"保存期限-最好当天吃完", "最好当天吃完", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.absolute_claim", Text: "最好"}}, true},
		{"保存期限-建议尽快食用", "建议收到后尽快食用，口感更佳", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.absolute_claim", Text: "最佳"}}, true},
		{"主观口感-品尝最佳口感", "复热后品尝最佳口感", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.absolute_claim", Text: "最佳"}}, true},
		{"绝对化宣传-全网最好", "咱们家全网最好", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.absolute_claim", Text: "最好"}}, false},
		{"资质词-老字号", "几十年老字号历史", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.absolute_claim", Text: "老字号"}}, false},
		{"非绝对化规则-零风险", "试吃零风险", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.absolute_claim", Text: "零风险"}}, false},
		{"其它规则放行判断", "最好当天吃完", contentguard.Annotation{Hit: contentguard.Hit{RuleID: "l1.p0.unsupported_data_or_endorsement", Text: "最好"}}, false},
	}
	for _, c := range cases {
		got := isConsumptionHintHit(c.text, c.hit)
		if got != c.want {
			t.Errorf("%s: isConsumptionHintHit=%v, 期望=%v", c.name, got, c.want)
		}
	}
}
