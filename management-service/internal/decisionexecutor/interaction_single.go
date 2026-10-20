package decisionexecutor

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

// singleInteractionStrategy 是「单一互动」的独立策略（骨架阶段）。
//
// 承接旧链路「问答三兄弟」：
//   - reply_question 回答问题（真提问，查事实组织答案）
//   - complaint       投诉与负面反馈（安抚 + 澄清 + 给交代）
//   - reply_chat      回复弹幕（性质混杂，先判真提问/夸赞闲聊/无意义刷屏）
//
// 与欢迎/关注点赞/下单是同一层级的第五个独立互动策略（用户 2026-10-20 定），
// 把这三类「事实应答 + 聊天兜底」收进新轻量互动策略机，走统一的四层话术基座。
//
// 话术基座与欢迎完全一致（用户 2026-10-20 确认）：
//   内容四层（必说/可说/不提/改写）+ 主播腔调卡 + 两段式（接住句 + 答案句两个气口）。
//
// 当前为骨架阶段：按 mission_kind 分三种占位回应，不调模型；
// 模型实现见下方 modelSingleInteractionStrategy（两段式，答案事实取数暂留 TODO）。
type singleInteractionStrategy struct{}

// Respond 生成单一互动回应（骨架：模板占位）。
func (singleInteractionStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	address := resolvedAddress(item)
	question := strings.TrimSpace(primaryQuestion(item))
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))

	switch kind {
	case "complaint":
		// 投诉：先安抚情绪，再给交代（骨架阶段先接住，不带具体解决）。
		if address != "" && address != "家人们" {
			return address + "别着急，您说的这个问题我记下了，马上帮您处理。"
		}
		return "别着急，您说的问题我记下了，马上帮您处理。"

	case "reply_chat":
		// 弹幕：性质混杂，骨架阶段按有无内容给两种兜底。
		if question == "" {
			// 无意义刷屏/无内容：极简回应或不回显。
			return "收到。"
		}
		// 有内容：暂按真提问处理（后续接模型细分「真提问/夸赞闲聊/无意义」）。
		if address != "" && address != "家人们" {
			return address + "您说的这个问题我帮您看看：" + question
		}
		return "您说的这个问题我帮您看看：" + question

	default:
		// reply_question 及兜底：真提问，查事实组织答案（骨架先回显确认）。
		if question != "" {
			if address != "" && address != "家人们" {
				return address + "您问的这个问题我帮您确认一下：" + question
			}
			return "您问的这个问题我帮您确认一下：" + question
		}
		return "收到，我帮您确认一下。"
	}
}

// modelSingleInteractionStrategy 是「单一互动」策略的模型实现。
//
// 话术基座与欢迎完全同构（用户 2026-10-20 确认）：
//   - 复用 welcomeBaseConstraints（四层「不提/改写」）、welcomeVoiceCard（主播腔调卡）、
//     completeWelcome（带 token 记账的模型调用）；
//   - 两段式：第一句「接住」+ 第二句「给答案」，换行分隔、TTS 读两个气口。
//
// 与欢迎唯一不同：第二句喂给模型的事实不同——欢迎是「福利事实」，单一互动是「答案事实」。
// 答案事实的取数逻辑尚未定，暂留 TODO（singleAnswerFacts 返回空，第二句降级为空）。
type modelSingleInteractionStrategy struct {
	worker *Worker
}

// Respond 调模型生成单一互动回应（接住 + 答案两段式）。
// 无 agent、模型失败、或 worker 未就绪时，降级到 singleInteractionStrategy（纯程序骨架）。
func (m modelSingleInteractionStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if m.worker == nil || m.worker.agent == nil {
		return (singleInteractionStrategy{}).Respond(ctx, session, item)
	}
	text := m.worker.generateSingleInteractionText(ctx, session, item)
	if strings.TrimSpace(text) == "" {
		return (singleInteractionStrategy{}).Respond(ctx, session, item)
	}
	return text
}

// generateSingleInteractionText 是单一互动策略的模型调用核心，拆两次模型调用（照欢迎两段式）：
//   第一次「接住」：称呼 + 回应态度（问答=接住提问、投诉=安抚、chat=看到对方），不带答案。
//   第二次「给答案」：承接句过渡 + 答案事实，不带称呼。
// 两次结果合并成一段（换行分隔），TTS 读出两个气口。
//
// 仍走话术基座（与主线、欢迎、下单一致）：内容四层（必说/可说/不提/改写），P0 与 pUser 拆进四层。
func (w *Worker) generateSingleInteractionText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if w == nil || w.agent == nil {
		return ""
	}

	greet := w.generateSingleInteractionGreet(ctx, session, item)
	answer := w.generateSingleInteractionAnswer(ctx, session, item)

	// 两句都成功才拼；任一失败则尽量返回成功的那句，全失败返回空触发降级。
	greet = strings.TrimSpace(greet)
	answer = strings.TrimSpace(answer)
	if greet == "" && answer == "" {
		return ""
	}
	if greet == "" {
		return answer
	}
	if answer == "" {
		return greet
	}
	return greet + "\n" + answer
}

// generateSingleInteractionGreet 第一次调用：只出「接住人」的回应态度句，不带答案。
// 按 kind 区分态度：问答=接住提问、投诉=安抚情绪、chat=看到对方。
func (w *Worker) generateSingleInteractionGreet(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	addressingGuidance := item.addressingGuidance
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))

	notMention, rewrite := w.welcomeBaseConstraints(ctx, session)

	voiceCard, _ := w.welcomeVoiceCard(ctx, session)

	// 按互动类型给不同的「接住态度」描述。
	var attitude string
	switch kind {
	case "complaint":
		attitude = "先安抚情绪、让对方感到被重视，语气诚恳；不推卸、不争辩，先接住再给交代。"
	case "reply_chat":
		attitude = "先让对方感到被看见（夸赞就道谢、提问就说「我来看看」），语气轻快自然。"
	default: // reply_question 及兜底
		attitude = "先接住提问、让对方感到被认真对待，语气自然热情；把「我这就回答」的态度表达出来，让观众知道你马上就要给答案。禁止用「记下来」「回头说」「核实一下」「稍等」这类「延迟回答」的措辞——答案下一句就会跟上，接住句只负责「看见你 + 这就答」，不承担任何「我待会儿再回」的语义。"
	}

	system := "你是直播口播合成器。根据互动事件生成一句自然、简短、可直接播出的回应话术，只负责「接住刚发言的观众、表达回应态度」，不回答任何问题、不给出任何答案、不介绍任何商品/活动/价格。" +
		"\n\n" + attitude

	if voiceCard != "" {
		system += "\n\n【主播腔调】按下面的说话风格、语气词、口头禅自然表达，让人一听就是这位主播：" +
			"\n" + voiceCard
	}

	system += "\n\n按四层约束生成：" +
		"\n【必说】开头必须带一个合规称呼，不得裸奔；回应态度自然。" + addressingGuidance +
		"\n【不提】" + notMention +
		"\n【改写】" + rewrite +
		"\n\n最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。只输出主播真正会说的那一句。"

	// 称呼由 addressingGuidance（称呼策略算好的「本轮称呼谁 + 可简称可代称」）决定；
	// 点名名单仍单独在 user 里给出，让模型明确知道「叫谁」，但附加一条硬约束：
	// 性别未知时禁止单用「哥」或「姐」，改用「哥姐」「老乡」「老板」「朋友」等中性词。
	user := "事件类型：" + kind
	names := item.resolvedAddressingPlan.SelectedNames
	if len(names) >= 2 {
		user += "\n本次要称呼的对象（逐个点到，可自然带过）：" + strings.Join(names, "、")
	} else if len(names) == 1 {
		user += "\n本次要称呼的对象：" + names[0]
	}
	question := strings.TrimSpace(primaryQuestion(item))
	if question != "" {
		user += "\n对方刚说的内容：" + question
	}
	user += "\n\n请直接输出一句自然、简短、可播出的回应话术（只表达回应态度，不回答、不给答案）。"

	return w.completeWelcome(ctx, session, "interaction_single_greet", system, user, 80)
}

// generateSingleInteractionAnswer 第二次调用：只出「给答案」的承接句，纯答案、不带称呼。
//
// 答案事实从 singleAnswerFacts 取。取数逻辑：从 item.SelectedContentPlan.Facts
// （①②环节已由 interactionFactsGuidance 筛好的事实，含【必说】/【可说】安全改写层）
// 拼成「F编号 + 正文」喂给模型，让模型照「必说」念、用「可说」润色，天然防编造。
// 无事实时返回空 → generateSingleInteractionText 只出第一句（夸赞闲聊/无事实的情形）。
func (w *Worker) generateSingleInteractionAnswer(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	answerText := w.singleAnswerFacts(item)
	if strings.TrimSpace(answerText) == "" {
		return ""
	}

	notMention, rewrite := w.welcomeBaseConstraints(ctx, session)
	rewrite += "答案事实已按规则审核，直接用给定内容自然表达，不擅自改事实、价格、规格、时效。"

	voiceCard, _ := w.welcomeVoiceCard(ctx, session)

	system := "你是直播口播合成器。根据互动答案事实生成一句自然、简短、可直接播出的回答话术，给刚提问/反馈的观众一个交代。"

	if voiceCard != "" {
		system += "\n\n【主播腔调】按下面的说话风格、语气词、口头禅自然表达，让人一听就是这位主播：" +
			"\n" + voiceCard
	}

	system += "\n\n按四层约束生成：" +
		"\n【必说】这是承接上一句回应的第二句，语气与上一句保持一致、自然衔接；不要再称呼观众、不要再点任何名字、不要用「哥哥姐姐们」「家人们」等群称（上一句已经招呼过了）。" +
		"\n· 先说一个自然的承接句过渡，再带出答案，不要一开口就干巴巴报答案。承接句要口语、自然、随机变化。" +
		"\n· 答案必须严格按下方「当前答案事实」里的写，不得编造、不得改口。事实里【必说】的内容必须原样保留语义，【可说】的内容可用你的语气自然润色。" +
		"\n· 只从「当前答案事实」里取材，事实里没有的信息一律不许现编；如果事实不足以正面回答，就把事实里有的先说出来、并自然引导观众（如「这个我再帮您确认下」），绝不编造答案。" +
		"\n【可说】可自然带出下方「当前答案事实」，但不得编造事实里没有的信息。" +
		"\n【不提】" + notMention +
		"\n【改写】" + rewrite +
		"\n\n最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。只输出主播真正会说的那一句。"

	user := "【当前答案事实（可直接用来回答）】\n" + answerText +
		"\n\n请直接输出一句自然、简短、可播出的回答话术（先用承接句自然过渡，再交代答案，不要再称呼观众）。"

	return w.completeWelcome(ctx, session, "interaction_single_answer", system, user, 100)
}

// singleAnswerFacts 是单一互动「给答案」的答案事实取数入口。
//
// 取数来源：item.SelectedContentPlan.Facts（①②环节已筛好，见 interactionFactsGuidance）。
// 拼成「F编号 + 正文」喂模型（带编号不带角度标签，编号供程序核对、正文干净）。
// 无事实返回空（对应「相关但缺事实」与「夸赞闲聊无答案」两种情形，
// 由 generateSingleInteractionAnswer 返回空 → 只出接住句）。
func (w *Worker) singleAnswerFacts(item decisionItem) string {
	if item.SelectedContentPlan == nil || len(item.SelectedContentPlan.Facts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range item.SelectedContentPlan.Facts {
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Text) == "" {
			continue
		}
		b.WriteString(f.ID)
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(f.Text))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
