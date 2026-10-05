// Package factexpansion compiles user-selected creative freedom into a stable,
// vendor-neutral generation contract. It does not decide legal or platform
// policy; those higher-priority rules are supplied separately.
package factexpansion

import "livecompanion/management/internal/model"

const DefaultFreedom = 65

func Compile(freedom int) model.LiveFactExpansionPolicy {
	if freedom < 0 {
		freedom = 0
	}
	if freedom > 100 {
		freedom = 100
	}
	policy := model.LiveFactExpansionPolicy{
		Version:              model.LiveFactExpansionPolicyVersion,
		UserAuthorized:       true,
		Freedom:              freedom,
		BoundaryRewriteFirst: true,
		Allowed: []string{
			"改写、拆句、换序、问后自答、回顾和非连续重复",
		},
		AlwaysLocked: []string{
			"法律、平台规则及L1/L2中标记为绝对禁止的内容",
			"用户明确标记不可修改的事实、原词、数字、链接和承诺",
			"伪造具体库存、销量、评价、资质、检测、人物证言或实时房间状态",
		},
	}
	switch {
	case freedom <= 25:
		policy.Level = "conservative"
		policy.Allowed = append(policy.Allowed,
			"围绕已给事实做中性提问和表达层变化，不主动补充事实语义",
		)
	case freedom <= 55:
		policy.Level = "balanced"
		policy.Allowed = append(policy.Allowed,
			"加入不改变产品结论的日常场景、类比和观众自我判断问题",
			"做低风险常识性承接，但不用确定口吻制造新功效或新承诺",
		)
	case freedom <= 80:
		policy.Level = "open"
		policy.Allowed = append(policy.Allowed,
			"使用更丰富的场景、类比、故事框架、情绪价值和常识性合理推导",
			"使用非特指的人群观察和购买考虑，不伪造具体顾客、评价或成交事实",
			"允许更强的促单和紧迫感表达，但具体数字和实时状态必须有来源",
		)
	default:
		policy.Level = "edge_compliant"
		policy.Allowed = append(policy.Allowed,
			"在不违法、不违反平台规则且不触碰锁定项的前提下最大化场景化、故事化、类比、情绪和促单表达",
			"允许贴近审核边界的表达；如果原说法可能违规，保留表达目的并改写成不违规说法，而不是直接沉默或删除整段",
			"允许一般性、非精确的生活观察和合理推演，不得冒充已证实的功效、数据、背书或真实用户事件",
		)
	}
	return policy
}
