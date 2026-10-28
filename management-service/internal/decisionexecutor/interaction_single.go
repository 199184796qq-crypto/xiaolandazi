package decisionexecutor

import (
	"context"
	"regexp"
	"strings"

	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/factbound"
	"livecompanion/management/internal/factguard"
	"livecompanion/management/internal/factselect"
	"livecompanion/management/internal/model"
)

// Identity questions are answered by the program, never improvised: a made-up
// anchor name is worse than an honest boundary.
var identityQuestionPattern = regexp.MustCompile(`名字|叫什么|你是谁|怎么称呼|贵姓|哪位`)

// locationQuestionPattern catches "在哪里/地址/位置" style questions so the
// fallback line talks about location instead of joking about memory.
var locationQuestionPattern = regexp.MustCompile(`在哪|哪里|哪儿|地址|位置|哪个城市|什么地方|哪座城市`)

// upsetQuestionPattern catches complaints and anxiety (sold out, missing, angry).
// These must never get a joke or a metaphor: it reads as disrespect.
var upsetQuestionPattern = regexp.MustCompile(`抢光|卖完|没了|没抢到|缺货|断货|怎么(没|已|都)|失望|生气|投诉|骗|坑|着急|等不及|错过`)

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
//
//	内容四层（必说/可说/不提/改写）+ 主播腔调卡 + 两段式（接住句 + 答案句两个气口）。
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
		// 投诉：先接住情绪，不承诺“马上处理”。
		if address != "" && address != "家人们" {
			return address + "别着急，您说的这个我记下了。"
		}
		return "别着急，您说的问题我记下了。"

	case "reply_chat":
		// 弹幕：不重复观众原话，也不承诺去查。
		if question == "" {
			return "收到。"
		}
		return "您说的这个我听着呢，咱们接着聊。"

	default:
		// 真提问但没生成出答案：如实说没核实，不承诺“帮您确认”。
		if question != "" {
			return "这个我还真没核实到，先不跟您说死。"
		}
		return "这个我暂时还说不好。"
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
		// A real question that failed to render still deserves an honest, human
		// boundary — never an echo of the question plus "我帮您确认一下".
		if strings.EqualFold(strings.TrimSpace(item.MissionKind), "reply_question") {
			if boundary := strings.TrimSpace(m.worker.generateNoFactBoundary(ctx, session, item)); boundary != "" {
				address := strings.TrimSpace(resolvedAddress(item))
				if address == "" || address == "家人们" {
					return boundary
				}
				return address + "，" + boundary
			}
		}
		return (singleInteractionStrategy{}).Respond(ctx, session, item)
	}
	return text
}

// generateSingleInteractionText 是单一互动策略的模型调用核心，拆两次模型调用（照欢迎两段式）：
//
//	第一次「接住」：称呼 + 回应态度（问答=接住提问、投诉=安抚、chat=看到对方），不带答案。
//	第二次「给答案」：承接句过渡 + 答案事实，不带称呼。
//
// 两次结果合并成一段（换行分隔），TTS 读出两个气口。
//
// 仍走话术基座（与主线、欢迎、下单一致）：内容四层（必说/可说/不提/改写），P0 与 pUser 拆进四层。
func (w *Worker) generateSingleInteractionText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if w == nil || w.agent == nil {
		return ""
	}

	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	if kind == "reply_question" {
		// 事实问答不再先调“接住句”：一句“称呼 + 答案”或“称呼 + 边界”，
		// 避免“问得好，我这就给你讲”这种只评价问题、不回答的客服腔。
		if direct := w.generateSingleInteractionDirectAnswer(ctx, session, item); strings.TrimSpace(direct) != "" {
			return direct
		}
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

// generateSingleInteractionDirectAnswer 是事实问答的一步式回答：
// 称呼 + 答案；没有事实时直接给边界，不再先生成“问得好/我这就讲”的接住句。
const interactionNoFactBoundary = "这个我还没确认，先不跟你说死。"

// generateNoFactBoundary 不再按行业词分类。
// 没有事实时，让模型只针对当前这句话说“哪一点还没确认”，程序负责兜底和拦截承诺。
func (w *Worker) generateNoFactBoundary(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	question := strings.TrimSpace(primaryQuestion(item))
	if question == "" {
		return interactionNoFactBoundary
	}
	class := "no_dimension"
	if identityQuestionPattern.MatchString(question) {
		// No facts can name the anchor, so the model must not invent one; the
		// identity class bans self-introduced names inside the prompt and in the
		// program-side validator.
		class = "identity"
	} else if upsetQuestionPattern.MatchString(question) {
		class = "complaint"
	} else if locationQuestionPattern.MatchString(question) {
		class = "location"
	}
	return w.boundaryLine(ctx, session, class, question, nil)
}

// retrievedAnswerFacts is the last-chance retrieval when the selected plan has no
// facts: pick the closest facts from the room package and let the boundary check
// decide. It never invents anything.
func (w *Worker) retrievedAnswerFacts(ctx context.Context, session model.LiveRuntimeSession, question string) string {
	if w == nil || w.store == nil || strings.TrimSpace(question) == "" {
		return ""
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil || plan.ID <= 0 {
		return ""
	}
	_, facts, err := w.interactionApprovedFacts(ctx, session, plan.ID)
	if err != nil || strings.TrimSpace(facts) == "" {
		return ""
	}
	all := make([]factselect.Fact, 0, 32)
	for _, fact := range contentstrategy.Facts(facts) {
		all = append(all, factselect.Fact{ID: fact.ID, Text: fact.Text})
	}
	picked := factselect.Select(question, nil, nil, all, 3)
	var b strings.Builder
	for _, fact := range picked {
		b.WriteString(fact.ID)
		b.WriteString(" ")
		b.WriteString(fact.Text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (w *Worker) generateSingleInteractionDirectAnswer(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if w == nil || w.agent == nil {
		return ""
	}
	address := strings.TrimSpace(resolvedAddress(item))
	if address == "" {
		address = "朋友"
	}
	question := strings.TrimSpace(primaryQuestion(item))
	answerText := w.singleAnswerFacts(item)
	answerText = w.preferRegionFacts(ctx, session, question, answerText)
	if strings.TrimSpace(answerText) == "" {
		// The selected plan may be empty even though the room has facts (a new
		// purpose, a stale snapshot). Try one retrieval pass before giving up.
		answerText = w.retrievedAnswerFacts(ctx, session, question)
	}
	if strings.TrimSpace(answerText) == "" {
		return address + "，" + w.generateNoFactBoundary(ctx, session, item)
	}
	// A sold-out question should not be answered with shipping times: narrow the
	// facts to the aspect the viewer actually asked about.
	contextName, questionContext := factbound.MatchContext(question)
	if contextName != "" {
		if filtered := strings.TrimSpace(factbound.FilterFactsForContext(answerText, questionContext)); filtered != "" {
			answerText = filtered
		}
	}
	// The program owns the three-state fact check; the model only phrases it.
	bound := factbound.Evaluate(question, answerText, true)
	// A yes/no question can presume something the facts never say ("是当天发货吗").
	// When nothing in the facts supports it, answer with the boundary instead of
	// letting the model agree.
	if assertion := factguard.Assertions(question, answerText); len(assertion.Missing) > 0 {
		return address + "，" + w.boundaryLine(ctx, session, "partial_value", question, assertion.Missing)
	}
	voiceCard, _ := w.welcomeVoiceCard(ctx, session)
	system := "你是直播口播合成器。根据答案事实直接回答观众，第一句必须给出答案，不要先评价问题，也不要宣布“我这就讲/确认”。"
	if questionContext.Prompt != "" {
		system += "\n" + questionContext.Prompt
	}
	if voiceCard != "" {
		system += "\n\n【主播腔调】按下面的说话风格、语气词、口头禅自然表达，让人一听就是这位主播：\n" + voiceCard
	}
	system += "\n\n【必说】开头用称呼“" + address + "”，随后直接回应用户实际问的点；不要重复称呼，不要先评价问题。" +
		"\n只选与问题直接相关的事实，不要罗列无关事实，不要输出事实编号。" +
		"\n如果给出的答案事实只覆盖一部分，必须明确说出哪一点还没核实，不能把相关事实说成已经确认。" +
		"\n禁止出现“问得好、问得特别好、问得特别关键、问得真细致、问到点子上了、我这就、马上、稍等”等评价问题或让观众等待的说法。" +
		"\n【可说】只使用下方答案事实，不编造事实里没有的信息；有直接相关且不造成确认的保存、工艺信息可以补，没有就停。" +
		"\n【不提】不要用“新鲜、品质有保障”等话绕开未知，不暗示已经核实，不假装查过，不承诺稍后回复；不要输出内部标题、策略名称、字段名或分析过程。" +
		"\n【改写】答案用口语自然表达，通常一两句；事实里的价格、规格、时效不得改动；回答到位就结束，不主动转商品、问需求、催下单。"
	uncoveredTexts := make([]string, 0, len(bound.Uncovered))
	for _, value := range bound.Uncovered {
		uncoveredTexts = append(uncoveredTexts, value.Text)
	}
	if instruction := factbound.BoundaryInstruction(bound.Uncovered); instruction != "" {
		// Only answer the covered part here; the unconfirmed part gets its own
		// separately generated line so the wording can vary.
		system += "\n【只答已确认】只回答答案事实里已确认的部分，不要提及、不要确认：" + strings.Join(uncoveredTexts, "、") + "。" +
			"\n禁止出现“没核实、没确认、说不好、不确定”这些词；没确认的部分下一句单独说。"
	}
	user := "观众问题：" + question + "\n【当前答案事实】\n" + factbound.SpeakableFacts(answerText) +
		"\n\n请直接输出一句自然、简短、可播出的回答（称呼 + 答案，不要再称呼第二次）。"
	text := w.completeWelcome(ctx, session, "interaction_single_direct_answer", system, user, 100)
	text = stripAwkwardAnswerLead(strings.TrimSpace(text))
	// Remove sentences that invent numbers, promises or timings.
	text, _ = factguard.Audit(text, answerText)
	if text == "" || text == address || text == address+"。" {
		return address + "，" + w.generateNoFactBoundary(ctx, session, item)
	}
	if bound.NeedsBoundary() {
		// The covered part must not confirm the missing scope; the separately
		// generated line below is the only place it is named.
		text = softenTerms(text, uncoveredTexts)
		text = stripHedges(text)
		if strings.TrimSpace(text) == "" {
			// The whole covered sentence was hedging; ask once for facts only.
			text = w.coveredFactsLine(ctx, session, answerText, uncoveredTexts)
		}
		line := w.boundaryLine(ctx, session, "partial_value", question, uncoveredTexts)
		if strings.TrimSpace(text) == "" {
			return address + "，" + line
		}
		text = strings.TrimRight(text, "。！？!?；; ") + "。" + line
	}
	if answerStartsAsQuestionFragment(text, question) {
		// 复述了观众原话的残片（如「个107的」），改用边界句，避免播半句。
		text = ""
	}
	if strings.TrimSpace(text) == "" {
		return address + "，" + w.generateNoFactBoundary(ctx, session, item)
	}
	if !strings.HasPrefix(text, address) {
		text = address + "，" + text
	}
	return text
}

// answerStartsAsQuestionFragment detects an answer opening that is a slice of
// the viewer's own question instead of a real answer.
func answerStartsAsQuestionFragment(answer, question string) bool {
	answer = strings.TrimSpace(answer)
	question = strings.TrimSpace(question)
	if answer == "" || question == "" {
		return false
	}
	runes := []rune(answer)
	switch runes[0] {
	case '个', '的', '了', '和', '就', '都', '还', '也':
		return true
	}
	if len(runes) < 3 {
		return false
	}
	prefix := string(runes[:3])
	return strings.Contains(question, prefix) && !strings.ContainsAny(string(runes[0]), "好这那我你他她它咱")
}

var awkwardAnswerLeadPhrases = []string{
	"这个问题问得", "你问得正好", "你问得真好", "问得特别好",
	"问得特别关键", "问得真细致", "问到点子上了",
	"我这就给你", "我这就帮你", "我马上帮你", "马上给你", "稍等",
}

// stripAwkwardAnswerLead 去掉“问得好/我这就讲”这类空转从句，保留称呼和答案。
func stripAwkwardAnswerLead(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lastPunct := ""
	if runes := []rune(text); len(runes) > 0 && strings.ContainsRune("。！？", runes[len(runes)-1]) {
		lastPunct = string(runes[len(runes)-1])
	}
	parts := strings.Split(text, "，")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		awkward := false
		for _, phrase := range awkwardAnswerLeadPhrases {
			if strings.Contains(part, phrase) {
				awkward = true
				break
			}
		}
		if !awkward {
			kept = append(kept, part)
		}
	}
	result := strings.TrimSpace(strings.Join(kept, "，"))
	if result != "" && lastPunct != "" && !strings.HasSuffix(result, lastPunct) {
		result += lastPunct
	}
	return result
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
	default: // 未知类型兜底
		attitude = "先自然回应当前观众，语气自然；不要评价问题，不要用「问得好」「我这就讲」「马上」「稍等」这类空转或让观众等待的说法。"
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
	if phrase := strings.TrimSpace(item.PromptAddressPhrase); phrase != "" {
		user += "\n开头称呼（原样使用）：" + phrase
	} else if names := item.resolvedAddressingPlan.SelectedNames; len(names) >= 2 {
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
		"\n· 只从「当前答案事实」里取材，事实里没有的信息一律不许现编；如果事实不足以正面回答，就把事实里有的先说出来，剩下的如实说这块还没核实——不要承诺去确认、不要说稍后回复、不要重复观众的问题当答案。" +
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
	// Only the facts selected for this question count as answer evidence. The
	// full package is a fact pool, never a fallback: handing it to the model is
	// exactly how an unconfirmed region becomes "包邮".
	if item.SelectedContentPlan == nil || len(item.SelectedContentPlan.Facts) == 0 {
		return ""
	}
	facts := make([]factselect.Fact, 0, len(item.SelectedContentPlan.Facts))
	for _, f := range item.SelectedContentPlan.Facts {
		facts = append(facts, factselect.Fact{ID: f.ID, Text: f.Text})
	}
	angleIDs := make(map[string]bool, len(facts))
	angleTexts := make([]string, 0, len(item.IntentAngles))
	for _, row := range item.IntentAngles {
		angleTexts = append(angleTexts, strings.TrimSpace(row.Angle+" "+row.Task))
		for _, id := range row.FactIDs {
			angleIDs[id] = true
		}
	}
	// Narrow the selection to what the question actually asks: angle facts and
	// lexical matches only, never a fact about another region.
	picked := factselect.Select(primaryQuestion(item), angleTexts, angleIDs, facts, 3)
	var b strings.Builder
	for _, f := range picked {
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

// regionTerms lists delivery regions mentioned by viewers.
var regionTerms = []string{"新疆", "西藏", "内蒙古", "海南", "黑龙江", "吉林", "辽宁", "青海", "甘肃", "宁夏", "上海", "北京", "天津", "重庆", "广东", "江苏", "浙江", "山东", "河南", "四川", "湖北", "湖南", "福建", "安徽", "陕西", "山西", "河北", "江西", "云南", "贵州", "广西", "辽宁"}

// remoteRegions need air freight plus an extra fee.
var remoteRegions = map[string]bool{"新疆": true, "西藏": true, "内蒙古": true, "海南": true, "黑龙江": true, "吉林": true, "辽宁": true, "青海": true, "甘肃": true, "宁夏": true}

// preferRegionFacts puts region-appropriate shipping facts in front.
func (w *Worker) preferRegionFacts(ctx context.Context, session model.LiveRuntimeSession, question, current string) string {
	regions := detectRegions(question)
	if len(regions) == 0 {
		return current
	}
	packing := strings.Contains(question, "真空") || strings.Contains(question, "抽真空")
	remote := make([]string, 0, 2)
	for _, term := range regions {
		if remoteRegions[term] {
			remote = append(remote, term)
		}
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil || plan.ID <= 0 {
		return current
	}
	_, facts, err := w.interactionApprovedFacts(ctx, session, plan.ID)
	if err != nil || strings.TrimSpace(facts) == "" {
		return current
	}
	picked := make([]string, 0, 2)
	if packing {
		local, hint := packingHint(regions)
		for _, line := range strings.Split(facts, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !strings.Contains(line, "真空") {
				continue
			}
			if local && strings.Contains(line, "不抽真空") {
				picked = append(picked, line)
			}
			if !local && strings.Contains(line, "外地") {
				picked = append(picked, line)
			}
		}
		if len(picked) > 0 && hint != "" {
			picked = append([]string{hint}, picked...)
		}
		if len(picked) > 0 {
			if strings.TrimSpace(current) == "" {
				return strings.Join(picked, "\n")
			}
			return strings.Join(picked, "\n") + "\n" + current
		}
		return current
	}
	if len(remote) > 0 {
		for _, line := range strings.Split(facts, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			for _, term := range remote {
				if strings.Contains(line, term) {
					picked = append(picked, line)
					break
				}
			}
			if len(picked) >= 2 {
				break
			}
		}
		if len(picked) > 0 {
			picked = append([]string{"偏远地区按空运并补运费处理："}, picked...)
		}
	} else {
		for _, line := range strings.Split(facts, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.Contains(line, "发货") || strings.Contains(line, "京东物流") || strings.Contains(line, "快递") {
				picked = append(picked, line)
			}
			if len(picked) >= 3 {
				break
			}
		}
		if len(picked) > 0 {
			picked = append([]string{regions[0] + "属于普通地区，按常规发货，不需要空运补运费："}, picked...)
		}
	}
	if len(picked) == 0 {
		return current
	}
	if strings.TrimSpace(current) == "" {
		return strings.Join(picked, "\n")
	}
	return strings.Join(picked, "\n") + "\n" + current
}

// detectRegions returns the delivery regions mentioned in the question.
func detectRegions(question string) []string {
	regions := make([]string, 0, 2)
	for _, term := range regionTerms {
		if strings.Contains(question, term) {
			regions = append(regions, term)
		}
	}
	return regions
}

// packingHint decides whether the region is local (Sichuan/Chongqing) and the
// wording that explains the vacuum packing rule for it.
func packingHint(regions []string) (bool, string) {
	for _, term := range regions {
		if term == "四川" || term == "重庆" {
			return true, "川渝本地发货不抽真空："
		}
	}
	if len(regions) > 0 {
		return false, regions[0] + "属于外地，发货采用真空包装："
	}
	return false, ""
}
