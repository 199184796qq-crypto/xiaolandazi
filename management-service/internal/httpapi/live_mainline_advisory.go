package httpapi

import (
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechexpander"
)

// liveMainlineAdvisory is a backstage result. It must never be concatenated to
// speech text or sent to TTS/Core. Hard policy and fact failures are repaired
// before this stage; these items are reviewable quality/boundary suggestions.
type liveMainlineAdvisory struct {
	Code           string `json:"code"`
	Level          string `json:"level"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Suggestion     string `json:"suggestion"`
	UserDecidable  bool   `json:"user_decidable"`
	DecisionTarget string `json:"decision_target,omitempty"`
	HardBoundary   bool   `json:"hard_boundary"`
}

func liveMainlineAdviceOverrides(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = true
		}
	}
	return result
}

func buildLiveMainlineAdvisories(generation model.LiveAgentFullShowGenerationContext, strategy speechexpander.ResolvedContentStrategy, text string, audit model.LiveAgentFullShowAudit, overrides []string) []liveMainlineAdvisory {
	ignored := liveMainlineAdviceOverrides(overrides)
	items := make([]liveMainlineAdvisory, 0, 6)
	add := func(item liveMainlineAdvisory) {
		if !ignored[item.Code] {
			items = append(items, item)
		}
	}
	if len(generation.AuthorizedFacts) > 0 && len(generation.AuthorizedFacts) < 4 && utf8.RuneCountInString(text) >= 450 {
		add(liveMainlineAdvisory{
			Code: "narrow_material_pool", Level: "建议", Title: "可用材料较少",
			Message:       "当前长文本主要依靠少量事实反复展开，继续拉长后容易显得干瘪或重复。",
			Suggestion:    "可以补充真实的材质、规格、使用场景、选择方法或来源证据；也可以接受当前材料范围继续生成。",
			UserDecidable: true, DecisionTarget: "test_guidance",
		})
	}
	fulfillmentMentions := 0
	for _, word := range []string{"快递", "发货", "物流", "配送", "中通", "极兔", "申通", "邮政"} {
		fulfillmentMentions += strings.Count(text, word)
	}
	if fulfillmentMentions >= 6 {
		add(liveMainlineAdvisory{
			Code: "fulfillment_dominance", Level: "建议", Title: "物流内容占比偏高",
			Message:       "正文多次回到快递或发货，可能挤占商品价值、场景和选择内容。",
			Suggestion:    "下次测试降低物流事实的复用频率，把物流保留在选择或行动节点；如果你的直播间确实重视发货，也可以允许当前尺度。",
			UserDecidable: true, DecisionTarget: "test_guidance",
		})
	}
	hasBenefit := false
	for _, fact := range generation.AuthorizedFacts {
		if fact.SourceKind == "benefit" {
			hasBenefit = true
			break
		}
	}
	if strategy.ConversionIntensity >= 70 && !hasBenefit {
		add(liveMainlineAdvisory{
			Code: "strong_conversion_without_offer", Level: "提醒", Title: "成交推进较强但没有有效活动",
			Message:       "当前可以做选择引导和查看链接，但缺少活动、库存或限时事实，强逼单容易显得空。",
			Suggestion:    "降低成交推进力度，或补充经过确认且仍有效的活动事实。",
			UserDecidable: true, DecisionTarget: "conversion_intensity",
		})
	}
	for _, fact := range generation.FormalFacts {
		if strings.TrimSpace(fact.ForbiddenWording) != "" && strings.TrimSpace(fact.SafeRewrite) == "" {
			add(liveMainlineAdvisory{
				Code: "boundary_without_rewrite:" + fact.Key, Level: "提醒", Title: "禁止表达缺少替代说法",
				Message:       "事实“" + fact.Key + "”配置了不要这样说，但没有告诉系统相同意图可以怎么表达。",
				Suggestion:    "在事实依据里补充“改成这样说”；若你确认该表达不违法、不违反平台或 L1/L2，也可以修改或移除这条用户边界。",
				UserDecidable: true, DecisionTarget: "fact_boundary",
			})
		}
	}
	for _, issue := range audit.Issues {
		if issue.Severity != "warning" {
			continue
		}
		add(liveMainlineAdvisory{
			Code: "audit_warning:" + issue.Code, Level: "观察", Title: "本次校对提示",
			Message: issue.Message, Suggestion: "可以继续训练或调整方案；该提示不会进入口播。",
			UserDecidable: true, DecisionTarget: "test_guidance",
		})
	}
	return items
}
