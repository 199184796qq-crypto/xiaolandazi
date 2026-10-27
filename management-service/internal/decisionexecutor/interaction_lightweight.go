package decisionexecutor

import (
	"context"
	"log"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechmission"
)

// interactionTier 是三档划分的档位标识。
// 9 类互动按「回答需不需要动脑」收敛成三档：
//
//	第一档【事实应答】要查事实 + 组织答案：reply_question / complaint
//	第二档【礼仪回应】接住 + 给情绪 + 一句带过：reply_follow / reply_like /
//	                    welcome_named / welcome_batch / conversion_signal / warmup
//	第三档【聊天兜底】先判弹幕到底是啥：reply_chat（真提问 → 第一档；夸赞闲聊 → 第二档；无意义 → 不回应/极简）
type interactionTier int

const (
	// tierFactAnswer 第一档：事实应答，需要检索事实并组织答案。
	tierFactAnswer interactionTier = iota
	// tierAcknowledge 第二档：礼仪回应，接住情绪即可，不需要事实。
	tierAcknowledge
	// tierChat 第三档：聊天兜底，需要先判断弹幕性质。
	tierChat
)

// classifyInteraction 把一次互动事件归到三档之一。
// 这是新互动策略机的「分类」环节，纯程序逻辑，不调模型。
func classifyInteraction(item decisionItem) interactionTier {
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	switch kind {
	case "reply_question", "complaint":
		// 真提问 / 投诉：必须查事实组织答案。
		return tierFactAnswer
	case "reply_follow", "reply_like",
		"welcome_named", "welcome_batch",
		"conversion_signal", "warmup":
		// 礼仪性回应：接住 + 给情绪，不需要事实。
		return tierAcknowledge
	case "reply_chat":
		// 弹幕/闲聊：性质混杂，需要进一步判断（真提问 vs 夸赞闲聊 vs 无意义刷屏）。
		return tierChat
	default:
		// 空 kind 或未识别：旧链路里空 kind 走重链路（兜底），
		// 这里保守归入第一档，避免漏掉真实提问。
		return tierFactAnswer
	}
}

// classifyChat 进一步判断 reply_chat 弹幕的性质，决定它最终落哪一档。
// 当前为黑盒阶段的粗判：有内容且像提问的归第一档，其余归第二档。
// 真正的「真提问 / 夸赞闲聊 / 无意义刷屏」细分在接入模型时再细化。
func classifyChat(item decisionItem) interactionTier {
	question := strings.TrimSpace(primaryQuestion(item))
	if question == "" {
		// 无内容：无意义刷屏，第二档极简回应（黑盒阶段先不回显）。
		return tierAcknowledge
	}
	// 黑盒阶段：暂时把有内容的弹幕都当「真提问」归第一档，
	// 后续接入模型后，由模型判断「是提问还是夸赞」。
	// TODO(模型接入): 替换为模型/规则判定。
	return tierFactAnswer
}

// lightweightInteractionText 是新的最轻量互动策略机。
// 当前阶段是「骨架」：按互动类型分派到独立策略接口，各策略内部先用模板/占位，
// 不调用模型，用于验证「按类型分派」的结构是否正确。
//
// 输入是 core 已经判定好类型（item.MissionKind）的互动事件，
// 输出是最终可播的一段口播文字。后续 TTS、合规终审、下发流程全部复用。
//
// 分派原则（用户 2026-10-20 明确）：关注/点赞、欢迎、下单是三种本质不同的策略，
// 不能混在一个「礼仪回应」里，必须按互动类型分派到各自独立的策略接口。
func (w *Worker) lightweightInteractionText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	// 称呼前置（用户 2026-10-20 明确）：互动一进入（欢迎关注 / 提问答疑 / 下单等所有互动），
	// 就先把称呼策略跑一次，拿到「可播称呼 + 称呼约束 + 完整称呼计划」写回 item，
	// 供后面所有策略全局复用，不再由欢迎单独触发、也不再让其它策略走 promptNicknames 兜底。
	// 传指针，保证写回 item.resolvedAddress / addressingGuidance / resolvedAddressingPlan 生效。
	if w != nil {
		w.resolveInteractionAddressing(ctx, session, &item)
	}

	// 先定档，再按档位分派。
	// 第一档（事实应答 reply_question/complaint）与第三档（聊天兜底 reply_chat）
	// 都收进「单一互动」独立策略，由策略内部按 mission_kind 细分。
	// 第二档（礼仪回应 welcome/follow/like/order）走 dispatchLightInteraction。
	if strings.TrimSpace(item.MissionKind) == "" {
		log.Printf("interaction mission kind empty decision=%s origin=%s", item.ID, item.ManualOrigin)
	}
	tier := classifyInteraction(item)

	switch tier {
	case tierFactAnswer, tierChat:
		// 第一档【事实应答】+ 第三档【聊天兜底】：统一分派到「单一互动」策略。
		// 分派前先「筛事实」：复用 core 已解好的 purpose（item.PurposeID / item.PurposeKnowledge，
		// 通过队列落地），把 task_angles 提取成 item.IntentAngles，再走 interactionFactSnapshot
		// 筛出本次问题要用的几条事实，填 item.SelectedContentPlan 供答案句取用。
		// 解意图本身由 core 侧完成（打断调度 + 定 mission_kind 都在 core），management 不复解，
		// 避免同一条弹幕双倍 token。
		w.prepareSingleInteractionFacts(ctx, session, &item)
		return w.dispatchSingleInteraction(ctx, session, item)

	case tierAcknowledge:
		// 第二档【轻互动】按互动类型分派到独立策略接口。
		// 关注/点赞、欢迎、下单各自独立，不能混用。
		return w.dispatchLightInteraction(ctx, session, item)

	default:
		// 兜底（理论上走不到）：按事实应答处理。
		return w.dispatchSingleInteraction(ctx, session, item)
	}
}

// prepareSingleInteractionFacts 是单一互动分派前的「筛事实」前置。
//
// 解意图（判断是否提问/相关/purpose_id）由 core 侧完成，结果经队列落到
// item.PurposeID + item.PurposeKnowledge（JSON tag purpose_id / purpose_knowledge）。
// 本方法只做两件 management 侧的事，不复解意图：
//
//  1. 把 PurposeKnowledge 里的 task_angles 提取成 item.IntentAngles
//     （复用旧链路 intentTaskAngles，供 RenderSelection 按角度精准筛事实）；
//  2. 调 interactionFactSnapshot 筛出本次问题要用的几条事实，填 item.SelectedContentPlan，
//     供答案句 singleAnswerFacts 取用。
//
// 复用 core 的 purpose 而非 management 自解，避免同一条弹幕双倍 token。
func (w *Worker) prepareSingleInteractionFacts(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem) {
	if w == nil || item == nil {
		return
	}
	// ① 提取 task_angles → IntentAngles。
	item.IntentAngles = intentTaskAngles(item.PurposeKnowledge)

	// ② 筛事实 → SelectedContentPlan。需 planID。
	// store 为 nil（测试/未初始化 &Worker{}）时降级返回，不筛事实，
	// 答案句会因 SelectedContentPlan 为空而只出接住句，符合降级语义。
	if w.store == nil {
		return
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil || plan.ID <= 0 {
		return
	}
	// interactionFactSnapshot 内部：item.PurposeID 非空时走 RenderSelection 精准筛，
	// 并把 selected 写回 item.SelectedContentPlan；返回值是拼好的事实 prompt 字符串，
	// 新链路只取 SelectedContentPlan，忽略返回值。skipStrategy=false 保留策略母版注入。
	_, _ = w.interactionFactSnapshot(ctx, session, plan.ID, item, false)
}

// dispatchLightInteraction 按 mission_kind 把轻互动分派到对应的独立策略接口。
// 每个互动类型一个策略：关注/点赞 → likeFollowStrategy，欢迎 → welcomeStrategy，下单 → orderStrategy。
func (w *Worker) dispatchLightInteraction(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	var strategy InteractionStrategy
	switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
	case "conversion_signal":
		// 下单：独立策略（感谢下单 + 承接履约）。
		// 优先走模型实现（modelOrderStrategy），模型失败/无 agent 时内部降级到骨架。
		if w.agent != nil {
			strategy = modelOrderStrategy{worker: w}
		} else {
			strategy = w.orderStrategy
		}
	case "welcome_named", "welcome_batch", "warmup":
		// 欢迎：独立策略（称呼 + 介绍当前活动/优惠）。
		// 优先走模型实现（modelWelcomeStrategy），无福利/模型失败时内部降级到骨架。
		if w.agent != nil {
			strategy = modelWelcomeStrategy{worker: w}
		} else {
			strategy = w.welcomeStrategy
		}
	case "reply_follow", "reply_like":
		// 关注/点赞：轻量策略（称呼 + 一句感谢，不带信息）。
		strategy = w.likeFollowStrategy
	default:
		// 兜底：未知轻互动事件，走关注/点赞的轻量策略。
		strategy = w.likeFollowStrategy
	}

	// nil 兜底：测试或未初始化时（&Worker{}），回退到对应策略的零值骨架实现。
	if strategy == nil {
		switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
		case "conversion_signal":
			strategy = orderStrategy{}
		case "welcome_named", "welcome_batch", "warmup":
			strategy = welcomeStrategy{}
		default:
			strategy = likeFollowStrategy{}
		}
	}
	return strategy.Respond(ctx, session, item)
}

// dispatchSingleInteraction 把「单一互动」（问答三兄弟）分派到独立策略接口。
// 承接旧链路的 reply_question / complaint / reply_chat，三者合为一个「单一互动」策略。
//
// 当前为骨架阶段：优先走模型外壳（modelSingleInteractionStrategy，内部暂时降级到骨架），
// 无 agent 时走纯程序骨架（singleInteractionStrategy）。模型实现后续逐步接入。
func (w *Worker) dispatchSingleInteraction(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	var strategy InteractionStrategy
	if w.agent != nil {
		strategy = modelSingleInteractionStrategy{worker: w}
	} else {
		strategy = singleInteractionStrategy{}
	}

	// nil 兜底：测试或未初始化时（&Worker{}），回退到骨架实现。
	if strategy == nil {
		strategy = singleInteractionStrategy{}
	}
	return strategy.Respond(ctx, session, item)
}

// InteractionStrategy 是所有「轻互动」策略的统一接口。
//
// 每个互动类型一个独立策略实现，调用方（dispatchLightInteraction）按 mission_kind 分派，
// 只认接口、不关心底下是模板还是模型。
//
// 用户 2026-10-20 明确：关注/点赞、欢迎、下单是三种本质不同的策略，不能混在一个
// 「礼仪回应」里。三者差异：
//   - 关注/点赞：称呼 + 一句感谢，不带任何信息；
//   - 欢迎：称呼 + 介绍当前活动/优惠（要带直播间活动信息）；
//   - 下单：独立策略，感谢下单 + 承接发货。
type InteractionStrategy interface {
	Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string
}

// likeFollowStrategy 是「关注/点赞」的轻量策略。
// 本质：称呼 + 一句感谢，点到即止，不带产品、不带活动信息。
// 当前为骨架阶段（模板占位，不调模型）；后续接模型时替换为一次轻量调用。
type likeFollowStrategy struct{}

// Respond 生成关注/点赞的回应（骨架：模板占位）。
func (likeFollowStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	address := resolvedAddress(item)
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	switch kind {
	case "reply_like":
		if address != "" && address != "家人们" {
			return "谢谢" + address + "的点赞。"
		}
		return "感谢大家的点赞。"
	default: // reply_follow 及兜底
		if address != "" && address != "家人们" {
			return "谢谢" + address + "的关注，以后常来。"
		}
		return "感谢关注，以后常来啊。"
	}
}

// welcomeStrategy 是「欢迎」的独立策略。
// 本质：称呼 + 介绍当前活动/优惠（钩子式欢迎）。
// 活动/优惠信息来源暂空（用户 2026-10-20 定：等下单独做），当前骨架先只做「称呼 + 欢迎」，
// 活动介绍留待后续接入事实快照里的活动类事实。
type welcomeStrategy struct{}

// Respond 生成欢迎回应（骨架：模板占位，活动信息暂空）。
func (welcomeStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	address := resolvedAddress(item)
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	switch kind {
	case "welcome_named":
		if address != "" && address != "家人们" {
			return address + "来了啊，欢迎欢迎。"
		}
		return "欢迎来到直播间。"
	case "welcome_batch", "warmup":
		if address != "" {
			return address + "欢迎来到直播间。"
		}
		return "欢迎新进来的家人们。"
	default:
		return "欢迎来到直播间。"
	}
	// TODO(活动信息): 后续在这里接入「介绍当前活动/优惠」——从正式事实包筛活动类事实喂模型，
	// 让欢迎变成「称呼 + 欢迎 + 一句活动钩子」。
}

// orderStrategy 是「下单」的独立策略。
// 本质：感谢下单 + 承接发货/安排，促进转化，与关注/点赞、欢迎完全独立。
// 当前为骨架阶段（模板占位，不调模型）；后续接模型时替换为独立的下单回应调用。
type orderStrategy struct{}

// Respond 生成下单回应（骨架：模板占位）。
func (orderStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	address := resolvedAddress(item)
	if address != "" && address != "家人们" {
		return "谢谢" + address + "的支持，这就给您安排。"
	}
	return "收到，感谢支持，这就安排。"
}

// resolvedAddress 从已前置解析好的称呼计划里取一个「可播称呼」。
// 称呼由 resolveInteractionAddressing 在前置环节算好并存进 item，这里只读结果；
// 若前置环节未执行（如测试直接构造策略），降级到 item.promptNicknames() 兜底。
func resolvedAddress(item decisionItem) string {
	if strings.TrimSpace(item.resolvedAddress) != "" {
		return item.resolvedAddress
	}
	names := item.promptNicknames()
	if len(names) == 0 {
		return "家人们"
	}
	if len(names) == 1 {
		return names[0]
	}
	return "家人们"
}

// resolveInteractionAddressing 是「称呼前置、全局复用」的统一入口。
//
// 用户 2026-10-20 明确：系统触发互动后，反正都要有称呼，就先跑一次称呼策略拿到称呼，
// 供后续所有策略（欢迎/关注/点赞/下单/事实应答）复用，不再每个策略各算一次。
//
// 它复用系统成熟的称呼机制（与旧链路 applyAddressingStrategyStage 同源）：
//  1. planNicknameAddresses 规划昵称简称（带 30 分钟缓存，同一昵称只调一次模型）；
//  2. 查房间称呼偏好 + 主播主力称呼；
//  3. buildAddressingPlan 判定单/多人模式（SINGLE/GROUP/MIXED/SAMPLE/NONE）并渲染称呼约束。
//
// 返回：
//   - address：一个「可播称呼」字符串（playableAddressFromPlan 产物，单个场景=单人名、群场景=群称词），
//     仅供骨架策略直接拼进话术；它始终是「单个」的便捷值，不代表完整点名名单。
//   - guidance：称呼约束文本（addressingPlanGuidance 产物），供模型策略喂进 prompt 的【必说】层，
//     其中完整点名名单（多个人时）已用顿号串好。
//
// 结果同时写回 item.resolvedAddress / item.addressingGuidance / item.resolvedAddressingPlan，
// 其中 resolvedAddressingPlan 是完整称呼计划（与旧链路 mission.Addressing 同源）：
// 单个就是单个（SelectedNames 长度 1）、多个就是多个（长度 2~3），名单不丢、不压平。
// 各分支策略要「单人或多人名单」时读 plan.SelectedNames，要群称读 plan.GroupLabel，各取所需。
func (w *Worker) resolveInteractionAddressing(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem) (address string, guidance string) {
	if item == nil {
		return "", ""
	}
	if strings.TrimSpace(item.resolvedAddress) != "" || item.addressingGuidance != "" {
		return item.resolvedAddress, item.addressingGuidance
	}
	// ① 昵称简称规划（填 PromptNicknames，带缓存）。
	w.planNicknameAddresses(ctx, session, item)

	// ② 房间称呼偏好 + 主播主力称呼。
	preferences := model.RoomAddressingPreferences{NamingPreference: "natural"}
	if w != nil && w.store != nil {
		if p, err := w.store.GetRoomAddressingPreferences(ctx, session.TenantID, session.RoomID); err == nil {
			preferences = p
		}
	}
	var anchorTerms []string
	if w != nil {
		anchorTerms = w.fetchAnchorAddressTerms(ctx, session)
	}

	// ③ 单/多人模式判定 + 渲染。
	plan := w.buildAddressingPlan(session.RoomID, *item, "room_preference", "", preferences, anchorTerms)
	guidance = addressingPlanGuidance(plan)

	// 提取可播称呼：优先点名名单，其次群称词，最后主力称呼/「家人们」兜底。
	address = playableAddressFromPlan(plan)

	item.resolvedAddress = address
	item.addressingGuidance = guidance
	item.resolvedAddressingPlan = plan
	return address, guidance
}

// playableAddressFromPlan 从称呼计划里取一个可直接拼进话术的称呼字符串。
func playableAddressFromPlan(plan speechmission.AddressingPlan) string {
	if len(plan.SelectedNames) > 0 {
		return plan.SelectedNames[0]
	}
	if g := strings.TrimSpace(plan.GroupLabel); g != "" {
		return g
	}
	if strings.TrimSpace(plan.Candidate) != "" {
		return strings.TrimSpace(plan.Candidate)
	}
	return "家人们"
}
