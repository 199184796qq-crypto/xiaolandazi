package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/factbound"
	"livecompanion/management/internal/fallbackline"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechbase"
)

// modelWelcomeStrategy 是「欢迎」策略的模型实现。
//
// 欢迎策略分两块（用户 2026-10-20 明确）：
//  1. 欢迎：称呼 + 欢迎语，让新人感到被看见、被接住。
//  2. 介绍活动/优惠：给一个「留下来的理由」（钩子）。
//
// 活动/优惠的取数（用户 2026-10-20 拍板）：
//   - 主路径：把方案「全量事实」（安全事实快照 SafeFacts，含 [商品卡]...原价、[有效福利]）
//     喂给模型，让模型以 JSON 返回识别出的活动/福利。
//   - 兜底：模型识别为空或失败时，程序在「活动福利 + 事实依据」里筛一次福利相关。
type modelWelcomeStrategy struct {
	worker *Worker
}

// Respond 调模型生成欢迎回应（欢迎 + 活动钩子一次出）。
// 无福利、模型失败、或 worker 未就绪时，降级到 welcomeStrategy（纯程序欢迎）。
func (m modelWelcomeStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if m.worker == nil || m.worker.agent == nil {
		return (welcomeStrategy{}).Respond(ctx, session, item)
	}
	text := m.worker.generateWelcomeText(ctx, session, item)
	if strings.TrimSpace(text) == "" {
		return (welcomeStrategy{}).Respond(ctx, session, item)
	}
	return text
}

type welcomeProfile struct {
	Name  string
	Parts []string // greeting | intro | hook | highlight
}

var welcomeProfiles = []welcomeProfile{
	{Name: "极短", Parts: []string{"greeting"}},
	{Name: "短-福利", Parts: []string{"greeting", "hook"}},
	{Name: "短-介绍", Parts: []string{"greeting", "intro"}},
	{Name: "标准", Parts: []string{"greeting", "intro", "hook"}},
	{Name: "丰富", Parts: []string{"greeting", "intro", "hook", "highlight"}},
}

var welcomeProfileWeights = []int{20, 25, 15, 28, 12}

// pickWelcomeProfile rotates the welcome length and focus so every newcomer gets
// a welcome, but not always the same shape, and never the same as last time.
func (w *Worker) pickWelcomeProfile(roomID int64) welcomeProfile {
	w.addressMu.Lock()
	last := ""
	if w.recentWelcomeProfile != nil {
		last = w.recentWelcomeProfile[roomID]
	}
	total := 0
	for _, weight := range welcomeProfileWeights {
		total += weight
	}
	pick := rand.IntN(total)
	chosen := welcomeProfiles[0]
	for i, profile := range welcomeProfiles {
		if pick < welcomeProfileWeights[i] {
			chosen = profile
			break
		}
		pick -= welcomeProfileWeights[i]
	}
	if chosen.Name == last && len(welcomeProfiles) > 1 {
		chosen = welcomeProfiles[(indexOfProfile(chosen)+1)%len(welcomeProfiles)]
	}
	if w.recentWelcomeProfile == nil {
		w.recentWelcomeProfile = map[int64]string{}
	}
	w.recentWelcomeProfile[roomID] = chosen.Name
	w.addressMu.Unlock()
	return chosen
}

func indexOfProfile(profile welcomeProfile) int {
	for i, item := range welcomeProfiles {
		if item.Name == profile.Name {
			return i
		}
	}
	return 0
}

func profileNeeds(profile welcomeProfile, part string) bool {
	for _, item := range profile.Parts {
		if item == part {
			return true
		}
	}
	return false
}

type welcomeBenefit struct {
	Type    string `json:"type"`    // 优惠类型：团购价/原价对比、优惠券、赠品、包邮、价格对比等
	Content string `json:"content"` // 可播出的优惠内容（已按事实审核，不含星号隐藏部分）
}

// generateWelcomeText 是欢迎策略的模型调用核心，拆两次模型调用（用户 2026-10-20 拍板）：
//   第一次「欢迎称呼」：接住人——欢迎前缀（「欢迎 XX」或「XX 欢迎」随机）+ 点名 +（多人时）群称带过，不带福利。
//   第二次「活动钩子」：给留下来的理由——纯福利，不带任何称呼/群称（群称只在第一句多人时带）。
// 两次结果合并成一段（换行分隔），TTS 读出两个气口，避免「称呼 + 福利」挤在一句里称呼密度过高。
//
// 仍走话术基座（与主线一致）：内容四层（必说/可说/不提/改写），P0 与 pUser 拆进四层。
func (w *Worker) generateWelcomeText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if w == nil || w.agent == nil {
		return ""
	}

	// 活动/福利选材（先取，取不到福利时无钩子可讲，走纯程序欢迎降级）。
	benefits := w.welcomeBenefits(ctx, session)
	if len(benefits) == 0 {
		return ""
	}
	// 模型可能识别出一堆福利（链接商品多时尤甚）。程序先挑 1-2 个最有钩子力的，
	// 再喂给钩子模型，避免福利罗列一堆、话术没重点。
	benefits = pickTopBenefits(benefits)
	benefitText := renderBenefitText(benefits)

	// 每个进来的观众都要欢迎，但长短和重点轮换：极短、只讲福利、只讲介绍、
	// 标准三句、丰富四句，避免每次都念同一段。
	profile := w.pickWelcomeProfile(session.RoomID)

	greeting := w.generateWelcomeGreeting(ctx, session, item)
	intro := ""
	if profileNeeds(profile, "intro") {
		if introFacts := w.roomIntroFacts(ctx, session); introFacts != "" {
			intro = w.generateWelcomeIntro(ctx, session, introFacts)
		}
	}
	hook := ""
	if profileNeeds(profile, "hook") {
		hook = w.generateWelcomeHook(ctx, session, benefitText)
	}
	highlight := ""
	if profileNeeds(profile, "highlight") {
		if introFacts := w.roomIntroFacts(ctx, session); introFacts != "" {
			highlight = w.generateWelcomeHighlight(ctx, session, introFacts)
		}
	}

	// 任一成功就播；全失败返回空触发降级。
	greeting = strings.TrimSpace(greeting)
	intro = strings.TrimSpace(intro)
	hook = strings.TrimSpace(hook)
	highlight = strings.TrimSpace(highlight)
	if greeting == "" && hook == "" && intro == "" && highlight == "" {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, part := range []string{greeting, intro, hook, highlight} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	text := strings.Join(parts, "\n")
	// 两句以上才做整段口语化；极短欢迎保持原样，避免被改写拉长。
	if len(parts) >= 2 {
		if system, user := fallbackline.HumanizePrompt(text); system != "" {
			if polished := strings.TrimSpace(w.completeWelcome(ctx, session, "interaction_welcome_humanize", system, user, 240)); polished != "" {
				if fallbackline.ValidateHumanized(text, polished) == nil {
					return polished
				}
			}
		}
	}
	return text
}

// generateWelcomeHighlight adds one product highlight (craft, taste, how to eat)
// for the richest welcome shape.
func (w *Worker) generateWelcomeHighlight(ctx context.Context, session model.LiveRuntimeSession, facts string) string {
	if w == nil || w.agent == nil || strings.TrimSpace(facts) == "" {
		return ""
	}
	system := "你是直播间的真人主播。从下面事实里挑一个产品亮点（工艺、口感、吃法、保质期任选其一），" +
		"用一句话讲给刚进来的朋友，25到45字。只能用事实，不编造，不讲价格和福利，不要称呼，不要客服腔。只输出这句话。"
	user := "已确认事实：\n" + facts
	return strings.TrimSpace(w.completeWelcome(ctx, session, "interaction_welcome_highlight", system, user, 100))
}

// roomIntroFacts returns the whole approved fact package: the intro summary has
// to be condensed by the model from everything, not from three pre-picked lines.
func (w *Worker) roomIntroFacts(ctx context.Context, session model.LiveRuntimeSession) string {
	if w == nil || w.store == nil {
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
	return factbound.SpeakableFacts(facts)
}

// generateWelcomeIntro tells a newcomer what the room is about, using only the
// approved facts, then the assembled welcome gets the human rewrite.
func (w *Worker) generateWelcomeIntro(ctx context.Context, session model.LiveRuntimeSession, introFacts string) string {
	if w == nil || w.agent == nil || strings.TrimSpace(introFacts) == "" {
		return ""
	}
	base := "你是直播间的真人主播。根据下面全部已确认事实，用一句话高度概括这个直播间是干什么的：" +
		"卖什么、给谁、有什么特点。要求：不超过45字；口语；只能使用事实，不编造；不要罗列商品和价格；不要称呼；不要客服腔。" +
		"\n必须说成“卖XX（产品名）+ 品类”，例如“卖四川特色美食绸都杨鸭子，卤味熟食”；" +
		"原料（土麻鸭、鸭坯）只能作为工艺补充放在后面，绝不能当商品说；禁止“活鸭、活禽、卖鸭”。" +
		"\n只输出这句话本身。"
	user := "已确认事实：\n" + introFacts
	for attempt := 0; attempt < 2; attempt++ {
		system := base
		if attempt > 0 {
			system += "\n上一次没有说清品类；这次必须出现本行业的品类词，明确是在卖做好的产品。"
		}
		text := strings.TrimSpace(w.completeWelcome(ctx, session, "interaction_welcome_intro", system, user, 100))
		if factbound.ValidateIntroFor(text, introFacts) == nil {
			return text
		}
	}
	return ""
}

// welcomeIntroProblem rejects an intro that sounds like selling live animals or
// never says which category of product the room sells. The word lists live in
// fact-bound.json, not in code.
func welcomeIntroProblem(text string) error { return factbound.ValidateIntro(text) }

// welcomeBaseConstraints 拼装欢迎话术公用的「不提/改写」层（P0 + pUser 拆进四层）。
func (w *Worker) welcomeBaseConstraints(ctx context.Context, session model.LiveRuntimeSession) (notMention, rewrite string) {
	notMention = speechbase.FourLayerNotMentionText
	if p0Forbidden := renderP0Forbidden(); p0Forbidden != "" {
		notMention += "\n\n红线词（本段一律不出现）：\n" + p0Forbidden
	}
	if puserForbidden := w.renderPUserForbidden(ctx, session); puserForbidden != "" {
		notMention += "\n\n用户确认的表达限制（本段一律不出现）：\n" + puserForbidden
	}

	if p0Rewrite := renderP0Rewrite(); p0Rewrite != "" {
		rewrite += p0Rewrite
	}
	if puserRewrite := w.renderPUserRewrite(ctx, session); puserRewrite != "" {
		if rewrite != "" {
			rewrite += "\n\n"
		}
		rewrite += "用户确认的展开指导：\n" + puserRewrite
	}
	return notMention, rewrite
}

// welcomeVoiceCard 取当前直播间的主播腔调卡（动态，每个直播间不同、可能没配）。
// 有主播风格 → RenderAnchorVoiceCard(StyleMD, BaselineJSON) 产出一张「风格+量化基准+禁用表达」卡；
// 没配风格 → 返回空字符串，让模型自由发挥（RenderAnchorVoiceCard 空兜底已处理）。
// 返回 (voiceCard string, ok bool)，ok=false 表示本场无腔调卡。
func (w *Worker) welcomeVoiceCard(ctx context.Context, session model.LiveRuntimeSession) (string, bool) {
	if w == nil || w.store == nil {
		return "", false
	}
	source, ok := w.store.(liveAnchorStyleSource)
	if !ok {
		return "", false
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil || plan.ID <= 0 {
		return "", false
	}
	style, err := source.GetLiveAnchorStyleForPlan(ctx, session.TenantID, plan.ID)
	if err != nil || style.Profile.Delivery == nil {
		return "", false
	}
	styleMD := style.Profile.Delivery.MetaPromptMD
	baselineJSON := style.Profile.Delivery.ScoringBaselineJSON
	card, err := speechbase.RenderAnchorVoiceCard(styleMD, baselineJSON)
	if err != nil {
		return "", false
	}
	// RenderAnchorVoiceCard 空兜底返回 ""（没配风格）；有风格返回 map。序列化成紧凑文本喂给模型。
	text := voiceCardText(card)
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// voiceCardText 把 RenderAnchorVoiceCard 的返回值（any：空字符串或 map）转成可喂给模型的紧凑文本。
func voiceCardText(card any) string {
	switch v := card.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		var b strings.Builder
		if style, _ := v["风格"].(string); strings.TrimSpace(style) != "" {
			b.WriteString("说话风格：" + strings.TrimSpace(style))
		}
		if quant, _ := v["量化基准"].(string); strings.TrimSpace(quant) != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("量化基准：" + strings.TrimSpace(quant))
		}
		if forbid, _ := v["禁用表达"].(string); strings.TrimSpace(forbid) != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("禁用表达：" + strings.TrimSpace(forbid))
		}
		return b.String()
	default:
		return ""
	}
}

// generateWelcomeGreeting 第一次调用：只出「接住人」的欢迎称呼句，不带任何福利。
// 欢迎前缀随机（「欢迎 XX」/「XX 欢迎」），点名按单/多人名单，群称只在多人时带。
// 带主播腔调卡（有风格才带、没风格自由发挥）。
func (w *Worker) generateWelcomeGreeting(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	addressingGuidance := item.addressingGuidance

	notMention, rewrite := w.welcomeBaseConstraints(ctx, session)

	voiceCard, _ := w.welcomeVoiceCard(ctx, session)

	system := "你是直播口播合成器。根据欢迎事件生成一句自然、简短、可直接播出的欢迎话术，只负责「接住刚进直播间的人」，不介绍任何活动、福利、价格。" +
		"\n\n欢迎句式要随机，二选一：" +
		"\n· 前置式：「欢迎」+ 称呼（如「欢迎暖栀」）；" +
		"\n· 后置式：先称呼、后说欢迎（如「暖栀来啦，欢迎」）。"

	if voiceCard != "" {
		system += "\n\n【主播腔调】按下面的说话风格、语气词、口头禅自然表达，让人一听就是这位主播：" +
			"\n" + voiceCard
	}

	system += "\n\n按四层约束生成：" +
		"\n【必说】开头必须带一个合规称呼，不得裸奔；欢迎语气自然热情。" + addressingGuidance +
		"\n【不提】" + notMention +
		"\n【改写】" + rewrite +
		"\n\n最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。只输出主播真正会说的那一句。"

	// 点名名单单独在 user 里给出，让模型明确知道「叫谁」；性别未知的硬规则已由
	// addressingGuidance（称呼策略约束）统一承载，不在此重复。
	user := "事件类型：" + kind
	names := item.resolvedAddressingPlan.SelectedNames
	if len(names) >= 2 {
		user += "\n本次要称呼的对象（逐个点到，可自然带过）：" + strings.Join(names, "、")
	} else if len(names) == 1 {
		user += "\n本次要称呼的对象：" + names[0]
	}
	user += "\n\n请直接输出一句自然、简短、可播出的欢迎话术（只欢迎，不介绍活动）。"

	return w.completeWelcome(ctx, session, "interaction_welcome_greeting", system, user, 80)
}

// generateWelcomeHook 第二次调用：只出「给留下来的理由」的活动钩子句，纯福利，不带任何称呼/群称。
func (w *Worker) generateWelcomeHook(ctx context.Context, session model.LiveRuntimeSession, benefitText string) string {
	notMention, rewrite := w.welcomeBaseConstraints(ctx, session)
	rewrite += "福利事实已按规则审核，直接用给定内容自然表达，不擅自改价格、赠品、有效期。"

	system := "你是直播口播合成器。根据当前有效福利生成一句自然、简短、可直接播出的活动钩子话术，给刚进直播间的观众一个「留下来」的理由。" +
		"\n\n按四层约束生成：" +
		"\n【必说】这是承接上一句欢迎的第二句，语气与上一句保持一致、自然衔接；不要再称呼观众、不要再点任何名字、不要用「哥哥姐姐们」「家人们」等群称（上一句已经招呼过了）。" +
		"\n· 先说一个自然的承接句过渡，再带出福利，不要一开口就干巴巴报价格。承接句要口语、自然、随机变化。" +
		"\n【可说】可自然带出当前活动/优惠的钩子（见下方「当前有效福利」），但不得编造福利里没有的信息。" +
		"\n【不提】" + notMention +
		"\n【改写】" + rewrite +
		"\n\n最终答案禁止输出任何内部标题、策略名称、概率、系统说明、字段名或分析过程。只输出主播真正会说的那一句。"

	user := "【当前有效福利（活动/优惠，可自然带出）】\n" + benefitText +
		"\n\n请直接输出一句自然、简短、可播出的活动钩子话术（先用承接句自然过渡，再介绍福利，不要再称呼观众）。"

	return w.completeWelcome(ctx, session, "interaction_welcome_hook", system, user, 100)
}

// completeWelcome 是欢迎两句共用的模型调用封装。
// 走 completeInteractionAnswer 落 live_model_exchanges（带 token 记账），
// 让新链路（欢迎）的模型成本与旧链路一样可统计，不再是无记录的黑盒。
func (w *Worker) completeWelcome(ctx context.Context, session model.LiveRuntimeSession, stage, system, user string, maxTokens int) string {
	answerCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	request := agentgateway.Request{
		Stage:          stage,
		Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:      maxTokens,
		ResponseFormat: agentgateway.ResponseText,
		EnableThinking: false,
		Timeout:        8 * time.Second,
	}
	answer, err := w.completeInteractionAnswer(answerCtx, session, request)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(answer.Text)
}

// welcomeBenefits 是欢迎策略的活动/福利选材入口。
//
// 主路径：把方案全量事实（SafeFacts）喂给模型，让模型识别出活动/福利（JSON）。
// 兜底：模型识别为空或失败时，程序在「活动福利 + 事实依据」里筛福利相关。
//
// 缓存：结果常驻 Redis（48 小时时效兜底），仅在「删除/修改方案或房间」时由
// InvalidateBenefitCache 主动失效；未注入 Redis 时每次现算。
//
// 返回空切片表示「没有任何福利可介绍」，调用方据此降级为纯程序欢迎。
func (w *Worker) welcomeBenefits(ctx context.Context, session model.LiveRuntimeSession) []welcomeBenefit {
	if w == nil || w.store == nil {
		return nil
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return nil
	}
	planID := plan.ID

	// 先查 Redis 缓存。
	if cached, ok := w.benefitFromRedis(ctx, session.TenantID, planID); ok {
		return cached
	}

	var benefits []welcomeBenefit
	// 主路径：全量事实喂模型识别。
	if w.agent != nil {
		if b, err := w.modelRecognizeBenefits(ctx, session, planID); err == nil && len(b) > 0 {
			benefits = b
		}
	}
	// 模型识别为空或失败，落到程序兜底。
	if len(benefits) == 0 {
		benefits = w.programFallbackBenefits(ctx, session, planID)
	}

	w.benefitSetRedis(ctx, session.TenantID, planID, benefits)
	return benefits
}

// benefitCacheKey 是福利缓存的 Redis key 命名空间。
const benefitCacheKeyPrefix = "livecompanion:benefit:v1:"

// benefitCacheTTL 是福利缓存的时效兜底：常驻但 48 小时后自动过期，
// 防止方案长期不更新时永远用旧福利。主动失效走 InvalidateBenefitCache。
const benefitCacheTTL = 48 * time.Hour

func benefitCacheKey(tenantID, planID int64) string {
	return fmt.Sprintf("%s%d:%d", benefitCacheKeyPrefix, tenantID, planID)
}

// benefitFromRedis 从 Redis 读福利缓存。命中返回 (benefits, true)，未命中/出错返回 (nil, false)。
// 返回的 benefits 可能为空切片（表示「已确认无福利」），由 ok 区分「有缓存」与「无缓存」。
func (w *Worker) benefitFromRedis(ctx context.Context, tenantID, planID int64) ([]welcomeBenefit, bool) {
	if w == nil || w.benefitRedis == nil {
		return nil, false
	}
	raw, err := w.benefitRedis.Get(ctx, benefitCacheKey(tenantID, planID)).Bytes()
	if err != nil {
		// redis.Nil 或任何错误都当未命中，走现算。
		return nil, false
	}
	var benefits []welcomeBenefit
	if err := json.Unmarshal(raw, &benefits); err != nil {
		return nil, false
	}
	return benefits, true
}

// benefitSetRedis 把福利结果写入 Redis（48 小时时效兜底）。
func (w *Worker) benefitSetRedis(ctx context.Context, tenantID, planID int64, benefits []welcomeBenefit) {
	if w == nil || w.benefitRedis == nil {
		return
	}
	raw, err := json.Marshal(benefits)
	if err != nil {
		return
	}
	// 写失败不阻塞欢迎生成，下次仍会现算。
	_ = w.benefitRedis.Set(ctx, benefitCacheKey(tenantID, planID), raw, benefitCacheTTL).Err()
}

// InvalidateBenefitCache 主动失效某个方案的福利缓存。
// 在「删除/修改方案或房间」时调用，让福利重新识别，避免用旧数据。
// 幂等：key 不存在时 Del 返回 0 也不报错。
func (w *Worker) InvalidateBenefitCache(ctx context.Context, tenantID, planID int64) {
	if w == nil || w.benefitRedis == nil {
		return
	}
	_ = w.benefitRedis.Del(ctx, benefitCacheKey(tenantID, planID)).Err()
}

// welcomeSafeFacts 直接读方案的正式事实快照 SafeFacts（探针 benefit-probe 已验证路径）。
//
// 与 interactionApprovedFacts 的区别：那条链路要经过「本场方案未改变」的 PlanID 严格校验、
// 事实边界复核（interaction-facts-boundary.md）、内容策略选材等一串处理，任何一环失败都返回空。
// 福利识别只要「事实里有哪些优惠」，不需要那一整套，所以这里直读：
//   1. GetPublishedLiveAgentPlanVersionForRoom 拿 published 版本的 generation_context；
//   2. 取 derived_safety.source_sha256；
//   3. GetLiveDerivedSafety 拿 SafeFacts 正文。
// 任何一步失败返回错误，调用方（welcomeBenefits）据此走程序兜底，不会卡死欢迎。
func (w *Worker) welcomeSafeFacts(ctx context.Context, session model.LiveRuntimeSession, planID int64) (string, error) {
	if w == nil || w.store == nil {
		return "", fmt.Errorf("欢迎福利取数未就绪")
	}
	source, ok := w.store.(interactionFactSnapshotStore)
	if !ok {
		return "", fmt.Errorf("store 不支持直接读安全事实快照")
	}

	published, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", fmt.Errorf("读取本场正式事实版本失败: %w", err)
	}
	value, hasSnapshot := published.GenerationContext["derived_safety"]
	if !hasSnapshot {
		return "", fmt.Errorf("本场正式事实快照缺失")
	}
	ref, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("本场正式事实引用无效")
	}
	hash, _ := ref["source_sha256"].(string)
	if strings.TrimSpace(hash) == "" {
		return "", fmt.Errorf("本场正式事实指纹缺失")
	}
	asset, err := source.GetLiveDerivedSafety(ctx, session.TenantID, planID, hash)
	if err != nil || strings.TrimSpace(asset.SafeFacts) == "" {
		return "", fmt.Errorf("本场审核事实不可用: %w", err)
	}
	return asset.SafeFacts, nil
}

// modelRecognizeBenefits 把方案全量事实（SafeFacts）喂给模型，让模型以 JSON 返回识别出的活动/福利。
//
// 全量事实来源 = welcomeSafeFacts（直接读正式事实快照 SafeFacts，探针 benefit-probe 已验证路径），
// 含 [商品卡]...团购价/原价 与 [有效福利] 等完整内容。这里不走 interactionApprovedFacts 的
// 「事实边界复核」链路，因为福利识别只需要「有什么优惠」，不需要主线那份带策略选材的完整事实包，
// 独立直读更稳、更快，避免福利识别被主线事实链路的任何校验卡住导致欢迎降级到骨架。
// 模型识别为空（benefits=[]）不算错误，返回空切片让调用方走程序兜底。
func (w *Worker) modelRecognizeBenefits(ctx context.Context, session model.LiveRuntimeSession, planID int64) ([]welcomeBenefit, error) {
	facts, err := w.welcomeSafeFacts(ctx, session, planID)
	if err != nil || strings.TrimSpace(facts) == "" {
		return nil, err
	}

	system := "你是直播间的活动/福利识别器。下面给你一个直播方案的完整事实清单。" +
		"你的任务：从中识别出「活动」「优惠」「团购价/原价对比」「赠品」「包邮」「限时」「优惠券」这类与促销优惠相关的内容。" +
		"规则：" +
		"\n1. 只挑与价格优惠、折扣、团购价、原价对比、优惠券、赠品、包邮、限时活动相关的事实，其余（地址、口味、工艺、预约、停车、包装等）一律不要。" +
		"\n2. 若事实里同时出现团购价和原价，把「团购价 / 原价 / 省多少」完整保留进 content。" +
		"\n3. 不得编造事实清单里没有的价格、赠品、期限、库存。" +
		"\n4. 输出严格 JSON，不要代码围栏、不要解释。" +
		"\n输出格式：{\"benefits\":[{\"type\":\"优惠类型\",\"content\":\"优惠内容\"}]}" +
		"\n若没有任何优惠，返回 {\"benefits\":[]}。"

	user := "以下是该方案的完整事实清单：\n" + facts

	recognizeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	request := agentgateway.Request{
		Stage:          "interaction_welcome_benefit_recognize",
		Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		ResponseFormat: agentgateway.ResponseJSON,
		MaxTokens:      500,
		EnableThinking: false,
		Timeout:        8 * time.Second,
	}

	answer, err := w.completeInteractionAnswer(recognizeCtx, session, request)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Benefits []welcomeBenefit `json:"benefits"`
	}
	if err := json.Unmarshal([]byte(answer.Text), &parsed); err != nil {
		return nil, err
	}
	// 过滤空条目。
	result := make([]welcomeBenefit, 0, len(parsed.Benefits))
	for _, b := range parsed.Benefits {
		if strings.TrimSpace(b.Content) != "" {
			result = append(result, b)
		}
	}
	return result, nil
}

// programFallbackBenefits 是程序兜底：在「活动福利 + 事实依据」里筛福利相关（不含商品卡）。
//
// 两个来源（用户 2026-10-20 定）：
//  1. 活动福利（LiveAgentPlanBenefit）：活动价 / 赠品 / 活动规则，带有效期过滤。
//  2. 事实依据（LiveAgentPlanFact）：Category 为 promotion/price/redemption 等促销类事实。
//
// 命中 1-2 条即止。两个来源都筛不出时返回空切片。
func (w *Worker) programFallbackBenefits(ctx context.Context, session model.LiveRuntimeSession, planID int64) []welcomeBenefit {
	var result []welcomeBenefit

	// ① 活动福利（最高优先级）。
	if src, ok := w.store.(interface {
		ListLiveAgentPlanBenefits(context.Context, int64, int64) ([]model.LiveAgentPlanBenefit, error)
	}); ok {
		if benefits, err := src.ListLiveAgentPlanBenefits(ctx, session.TenantID, planID); err == nil {
			now := w.now()
			for _, b := range benefits {
				if !benefitActive(b, now) {
					continue
				}
				line := strings.TrimSpace(strings.Join([]string{
					strings.TrimSpace(b.ProductName),
					strings.TrimSpace(b.Activity),
					strings.TrimSpace(b.ActivityPrice),
					strings.TrimSpace(b.Gift),
				}, " "))
				if line != "" {
					result = append(result, welcomeBenefit{Type: "活动福利", Content: line})
					if len(result) >= 2 {
						break
					}
				}
			}
		}
	}

	// ② 事实依据（促销类）。
	if len(result) < 2 {
		if facts, err := w.store.ListLiveAgentPlanFacts(ctx, session.TenantID, planID); err == nil {
			for _, f := range facts {
				if !isPromotionFactCategory(f.Category) {
					continue
				}
				line := strings.TrimSpace(strings.Join([]string{
					strings.TrimSpace(f.Key),
					strings.TrimSpace(f.Value),
				}, " "))
				if line != "" {
					result = append(result, welcomeBenefit{Type: "活动优惠", Content: line})
					if len(result) >= 2 {
						break
					}
				}
			}
		}
	}

	return result
}

// benefitActive 判断活动福利是否当前有效（StartsAt 未到或 EndsAt 已过则无效）。
func benefitActive(b model.LiveAgentPlanBenefit, now time.Time) bool {
	if b.StartsAt != nil && now.Before(*b.StartsAt) {
		return false
	}
	if b.EndsAt != nil && !now.Before(*b.EndsAt) {
		return false
	}
	return true
}

// pickTopBenefits 从模型识别出的一堆福利里挑 1-2 个最有钩子力的喂给生成模型。
//
// 背景：模型识别福利可能返回很多条（链接商品多时尤甚），若全塞给钩子模型，
// 容易罗列一堆福利、话术没重点。程序先按「钩子力」挑 1-2 个，宁少勿滥。
//
// 优先级（确定性、可解释）：
//  1. 含「团购价/原价/省」价格对比的福利（价格冲击力最强，直播最有效的钩子）；
//  2. 含「券/直降/减/折扣/赠」等优惠关键词的福利；
//  3. 其余福利（兜底）。
//
// 桶内随机取样（用户 2026-10-20）：同一档多个候选时不固定取第一个，随机挑，
// 避免同一直播间反复欢迎永远推同一个商品/同一条福利；兜底也随机，不按固定顺序。
// 最多返回 2 条。
func pickTopBenefits(benefits []welcomeBenefit) []welcomeBenefit {
	if len(benefits) <= 2 {
		return benefits
	}

	// 分桶：价格对比 > 优惠关键词 > 其余。
	var price []welcomeBenefit
	var promo []welcomeBenefit
	var rest []welcomeBenefit

	for _, b := range benefits {
		c := b.Content
		switch {
		case benefitHasPriceContrast(c):
			price = append(price, b)
		case benefitHasPromoKeyword(c):
			promo = append(promo, b)
		default:
			rest = append(rest, b)
		}
	}

	// 桶内随机打乱，避免固定取第一个。
	rand.Shuffle(len(price), func(i, j int) { price[i], price[j] = price[j], price[i] })
	rand.Shuffle(len(promo), func(i, j int) { promo[i], promo[j] = promo[j], promo[i] })
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })

	// 按优先级跨桶轮换拼接，取前 2。
	// 价格桶最多取 1 条，把第 2 个名额留给优惠券/其余桶，让价格款与优惠券款轮换登场，
	// 避免价格桶一次占满 2 个名额、优惠券永远排不上（同一直播间反复欢迎永远推同一商品）。
	picked := make([]welcomeBenefit, 0, 2)
	appendUpTo := func(list []welcomeBenefit, limit int) {
		for _, b := range list {
			if len(picked) >= limit {
				return
			}
			picked = append(picked, b)
		}
	}
	// 价格桶最多 1 条，为后续桶留名额。
	if len(price) > 0 {
		picked = append(picked, price[0])
	}
	appendUpTo(promo, 2)
	appendUpTo(rest, 2)
	return picked
}

// benefitHasPriceContrast 判断福利内容是否含「团购价/原价/省」这类价格对比信息。
func benefitHasPriceContrast(content string) bool {
	c := content
	return strings.Contains(c, "原价") ||
		strings.Contains(c, "团购价") ||
		strings.Contains(c, "省")
}

// benefitHasPromoKeyword 判断福利内容是否含优惠券/直降/折扣/赠品等优惠关键词。
func benefitHasPromoKeyword(content string) bool {
	c := content
	for _, kw := range []string{"券", "直降", "减", "折扣", "赠", "优惠", "特价", "限时"} {
		if strings.Contains(c, kw) {
			return true
		}
	}
	return false
}

// renderBenefitText 把筛出的活动/福利拼成一段简短描述喂给欢迎模型。
func renderBenefitText(benefits []welcomeBenefit) string {
	if len(benefits) == 0 {
		return ""
	}
	lines := make([]string, 0, len(benefits))
	for _, b := range benefits {
		line := strings.TrimSpace(b.Content)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "；")
}

// —— P0 / pUser 渲染（对齐主线：P0 拆「不提/改写」，pUser 拆「表达限制/展开指导」）——

// loadWelcomeP0Rules 读 P0 生成侧精简 JSON（compliance/L1/p0-generation.json），
// 返回 []speechbase.P0Rule。与主线 loadGenerationP0 同源，读同一个文件。
func loadWelcomeP0Rules() ([]speechbase.P0Rule, error) {
	raw, err := interactionpolicy.Read("compliance/L1/p0-generation.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Rules []speechbase.P0Rule `json:"rules"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Rules) == 0 {
		return nil, errWelcomeP0Empty
	}
	return doc.Rules, nil
}

var errWelcomeP0Empty = &welcomeP0EmptyError{}

type welcomeP0EmptyError struct{}

func (*welcomeP0EmptyError) Error() string { return "P0生成侧JSON为空或缺少rules" }

// renderP0Forbidden 把 P0 红线词表渲染成「不提」层用的一段裸词清单。
func renderP0Forbidden() string {
	rules, err := loadWelcomeP0Rules()
	if err != nil {
		return ""
	}
	return speechbase.RenderP0ForbiddenWordList(rules)
}

// renderP0Rewrite 把 P0 改说渲染成「改写」层用的一段映射清单。
func renderP0Rewrite() string {
	rules, err := loadWelcomeP0Rules()
	if err != nil {
		return ""
	}
	return speechbase.RenderP0RewriteGuidance(rules)
}

// welcomeCorrections 取用户已确认的长期纠正规则（pUser），与主线 EffectiveCorrections 同源：
// 经 contentpools.CorrectionView 转成带「表达限制/展开指导」的 []map[string]any。
func (w *Worker) welcomeCorrections(ctx context.Context, session model.LiveRuntimeSession) []map[string]any {
	if w == nil || w.store == nil {
		return nil
	}
	source, ok := w.store.(expressionConstraintStore)
	if !ok {
		return nil
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return nil
	}
	constraints, err := source.CurrentSpeechExpressionConstraints(ctx, session.TenantID, plan.ID)
	if err != nil {
		return nil
	}
	all := make([]int64, 0, len(constraints.Products))
	for _, p := range constraints.Products {
		all = append(all, p.ID)
	}
	return contentpools.CorrectionView(constraints, all)
}

// renderPUserForbidden 把 pUser「表达限制」渲染成「不提」层用的一段。
func (w *Worker) renderPUserForbidden(ctx context.Context, session model.LiveRuntimeSession) string {
	return speechbase.RenderPUserForbiddenList(w.welcomeCorrections(ctx, session))
}

// renderPUserRewrite 把 pUser「展开指导」渲染成「改写」层用的一段。
func (w *Worker) renderPUserRewrite(ctx context.Context, session model.LiveRuntimeSession) string {
	return speechbase.RenderPUserRewriteGuidance(w.welcomeCorrections(ctx, session))
}

// isPromotionFactCategory 判断事实分类是否属于「活动/优惠/促销」类，可作为欢迎钩子。
func isPromotionFactCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "promotion", "price", "redemption", "activity", "discount", "campaign", "benefit":
		return true
	default:
		return false
	}
}
