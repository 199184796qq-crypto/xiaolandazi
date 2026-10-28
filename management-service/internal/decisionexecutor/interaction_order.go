package decisionexecutor

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

// modelOrderStrategy 是「下单」策略的模型实现。
//
// 下单策略分两块（照欢迎的模型版模式，用户 2026-10-20 定）：
//  1. 感谢下单：接住人——称呼 + 感谢下单，不带履约信息。
//  2. 承接：给一个「下单后怎么拿到货」的交代（发货 / 到店核销 / 自提等）。
//
// 履约方式（发货 vs 到店）不写死：不同直播间履约方式不同（电商发货、餐饮到店核销、
// 门店自提等），所以把方案事实包（SafeFacts）里的履约事实喂给模型，让模型按方案里
// 实际写的履约方式自然承接，不擅自编造。
type modelOrderStrategy struct {
	worker *Worker
}

// Respond 调模型生成下单回应（感谢下单 + 承接履约一次出）。
// 无 agent、模型失败、或 worker 未就绪时，降级到 orderStrategy（纯程序骨架）。
func (m modelOrderStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if m.worker == nil || m.worker.agent == nil {
		return (orderStrategy{}).Respond(ctx, session, item)
	}
	text := m.worker.generateOrderText(ctx, session, item)
	if strings.TrimSpace(text) == "" {
		return (orderStrategy{}).Respond(ctx, session, item)
	}
	return text
}

// generateOrderText 是下单策略的模型调用核心，拆两次模型调用（照欢迎两段式）：
//   第一次「感谢下单」：接住人——称呼 + 感谢下单，不带任何履约信息。
//   第二次「承接履约」：交代下单后怎么拿到货（发货/到店核销/自提），纯履约、不带称呼。
// 两次结果合并成一段（换行分隔），TTS 读出两个气口。
//
// 仍走话术基座（与主线、欢迎一致）：内容四层（必说/可说/不提/改写），P0 与 pUser 拆进四层。
func (w *Worker) generateOrderText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if w == nil || w.agent == nil {
		return ""
	}

	thanks := w.generateOrderThanks(ctx, session, item)
	fulfill := w.generateOrderFulfillment(ctx, session)

	// 两句都成功才拼；任一失败则尽量返回成功的那句，全失败返回空触发降级。
	thanks = strings.TrimSpace(thanks)
	fulfill = strings.TrimSpace(fulfill)
	if thanks == "" && fulfill == "" {
		return ""
	}
	if thanks == "" {
		return fulfill
	}
	if fulfill == "" {
		return thanks
	}
	return thanks + "\n" + fulfill
}

// generateOrderThanks 第一次调用：只出「接住下单」的感谢句，不带任何履约信息。
// 称呼前置（resolvedAddress / addressingGuidance），感谢语气自然，不提发货、不提到店。
func (w *Worker) generateOrderThanks(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	addressingGuidance := item.addressingGuidance

	notMention, rewrite := w.welcomeBaseConstraints(ctx, session)

	voiceCard, _ := w.welcomeVoiceCard(ctx, session)

	system := "你是直播口播合成器。根据下单事件生成一句自然、简短、可直接播出的感谢话术，只负责「接住刚下单的观众、表达感谢」，不介绍任何履约方式、发货、到店、时效。" +
		"\n\n感谢句式要自然随机，不要机械复读「感谢下单」四个字，可用「感谢支持」「谢谢信任」「收到安排」等自然变体。"

	if voiceCard != "" {
		system += "\n\n【主播腔调】按下面的说话风格、语气词、口头禅自然表达，让人一听就是这位主播：" +
			"\n" + voiceCard
	}

	system += "\n\n按四层约束生成：" +
		"\n【必说】开头必须带一个合规称呼，不得裸奔；感谢语气自然热情。" + addressingGuidance +
		"\n【不提】" + notMention +
		"\n【改写】" + rewrite +
		"\n\n最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。只输出主播真正会说的那一句。"

	// 点名名单单独在 user 里给出，让模型明确知道「叫谁」；性别未知的硬规则已由
	// addressingGuidance（称呼策略约束）统一承载，不在此重复。
	user := "事件类型：下单"
	if phrase := strings.TrimSpace(item.PromptAddressPhrase); phrase != "" {
		user += "\n开头称呼（原样使用）：" + phrase
	} else if names := item.resolvedAddressingPlan.SelectedNames; len(names) >= 2 {
		user += "\n本次要称呼的对象（逐个点到，可自然带过）：" + strings.Join(names, "、")
	} else if len(names) == 1 {
		user += "\n本次要称呼的对象：" + names[0]
	}
	user += "\n\n请直接输出一句自然、简短、可播出的感谢下单话术（只感谢，不提履约/发货/到店）。"

	return w.completeWelcome(ctx, session, "interaction_order_thanks", system, user, 80)
}

// generateOrderFulfillment 第二次调用：只出「下单后怎么拿到货」的承接句，纯履约、不带称呼。
// 履约事实从方案 SafeFacts 里读，让模型按方案实际写的履约方式（发货/到店核销/自提）自然承接。
// 取不到履约事实时返回空，让调用方降级到骨架（不编造履约方式）。
func (w *Worker) generateOrderFulfillment(ctx context.Context, session model.LiveRuntimeSession) string {
	fulfillText := w.orderFulfillmentFacts(ctx, session)
	if strings.TrimSpace(fulfillText) == "" {
		return ""
	}

	notMention, rewrite := w.welcomeBaseConstraints(ctx, session)
	rewrite += "履约事实已按规则审核，直接用给定内容自然表达，不擅自改履约方式、时效、地点。"

	system := "你是直播口播合成器。根据下单后的履约事实生成一句自然、简短、可直接播出的承接话术，给刚下单的观众一个「怎么拿到货」的交代。" +
		"\n\n按四层约束生成：" +
		"\n【必说】这是承接上一句感谢的第二句，语气与上一句保持一致、自然衔接；不要再称呼观众、不要再点任何名字、不要用「哥哥姐姐们」「家人们」等群称（上一句已经招呼过了）。" +
		"\n· 先说一个自然的承接句过渡，再带出履约方式，不要一开口就干巴巴报「发货」「到店」。承接句要口语、自然、随机变化。" +
		"\n· 履约方式（发货 / 到店核销 / 自提等）必须严格按下方「当前履约事实」里的写，不得编造、不得改口。" +
		"\n【可说】可自然带出下方「当前履约事实」，但不得编造事实里没有的信息。" +
		"\n【不提】" + notMention +
		"\n【改写】" + rewrite +
		"\n\n最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。只输出主播真正会说的那一句。"

	user := "【当前履约事实（下单后怎么拿到货）】\n" + fulfillText +
		"\n\n请直接输出一句自然、简短、可播出的承接话术（先用承接句自然过渡，再交代履约方式，不要再称呼观众）。"

	return w.completeWelcome(ctx, session, "interaction_order_fulfillment", system, user, 100)
}

// orderFulfillmentFacts 从方案事实包（SafeFacts）里筛出「履约方式」相关的事实原文，
// 供承接句生成使用。履约类事实类别：发货/物流/快递、核销、到店/自提、预约、使用条件等。
//
// 复刻欢迎的 welcomeSafeFacts 直读路径（探针已验证），只读 SafeFacts 全文返回，
// 由模型在第二句 prompt 里自行挑履约相关内容，避免程序硬编码履约类别导致误判。
func (w *Worker) orderFulfillmentFacts(ctx context.Context, session model.LiveRuntimeSession) string {
	if w == nil || w.store == nil {
		return ""
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return ""
	}
	facts, err := w.welcomeSafeFacts(ctx, session, plan.ID)
	if err != nil || strings.TrimSpace(facts) == "" {
		return ""
	}
	return facts
}
