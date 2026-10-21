package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/speechbase"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func (s *Server) installCycleTextModel(ctx context.Context, state *anchorcycle.State) error {
	// New runs take the operator's current role; saved business/voice assets stay frozen.
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return err
	}
	state.Config.AngleRefreshAfterPlays = settings.Cycle.AngleRefreshAfterPlays
	if settings.Cycle.TextProfileID != "" {
		state.Config.TextProfileID = settings.Cycle.TextProfileID
		state.Config.FeedbackID = settings.Cycle.FeedbackID
	}
	// P0 语境复核已下线；此 stage 名仅作为「review 模型默认角色」的入口保留，
	// ResolveTextRole 会因 UsesReviewModel 直接返回 ReviewDefaultID，用于装 ReviewModel
	// 供最终文字审核（mainline_final_content_review）、角度方向审核等仍存在的环节复用。
	reviewID, err := s.store.ResolveTextRole(ctx, "mainline_p0_context")
	if err != nil {
		return err
	}
	if reviewID != "" {
		reviewer, e := s.store.ResolveSpeechModel(ctx, reviewID)
		if e != nil || reviewer == nil || !reviewer.Enabled || reviewer.APIKey == "" {
			return fmt.Errorf("文字审核模型不可用")
		}
		snapshot := *reviewer
		snapshot.APIKey = ""
		state.ReviewModel = &snapshot
	}
	if state.Config.TextProfileID == "" {
		state.Config.FeedbackID = settings.Cycle.FeedbackID
		return nil
	} // Explicit legacy Agent configuration.
	state.Config.FeedbackID = state.Config.TextProfileID
	profile, err := s.store.ResolveSpeechModel(ctx, state.Config.TextProfileID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		return fmt.Errorf("主线文字模型不可用")
	}
	_, hash, _, err := agentgateway.TextPromptContract(profile.Model)
	if err != nil {
		return err
	}
	snapshot := *profile
	snapshot.APIKey = ""
	state.TextModel = &snapshot
	state.TextPromptSHA = hash
	state.SessionID = "text-" + state.RunID // Run identifier only; never an upstream session.
	return nil
}

// Compile expression data only. Legacy sample products, price examples and
// filled hook skeletons remain in their original assets, never in today's facts.
func cycleExpressionView(state *anchorcycle.State) (any, error) {
	return renderAnchorExpressionView(state.BaselineJSON)
}

// renderAnchorExpressionView 委托共享包 speechbase 渲染主播表达标尺。
// 主线/开场/互动三类话术共用同一份实现，避免主播腔调三套各写各的。
func renderAnchorExpressionView(baselineJSON string) (any, error) {
	return speechbase.RenderAnchorExpressionView(baselineJSON)
}

// cycleVoiceCard 委托共享包 speechbase 把主播风格母版（StyleMD）与量化基准（Quant）
// 合并成一张「主播腔调卡」，主线话术的 stable 前缀只喂这一份，消除「风格母版 vs
// 表达标尺」两份重叠。
func cycleVoiceCard(state *anchorcycle.State) (any, error) {
	return speechbase.RenderAnchorVoiceCard(state.StyleMD, state.BaselineJSON)
}

// fourLayerNotMentionText 引用共享包的四层「不提」统一文案。
const fourLayerNotMentionText = speechbase.FourLayerNotMentionText

func textControlLeak(text string) bool {
	for _, marker := range []string{"{{", "}}", "本次任务：", "表达展开建议：", "开头模式：", "后续表达加强建议：", "\"本次任务\":", "\"reply\":", "\"needs_review\":", "## 本场有效事实", "## 主播表达标尺"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
func tailText(value string, limit int) string {
	r := []rune(value)
	if len(r) > limit {
		r = r[len(r)-limit:]
	}
	return string(r)
}
func cycleExpressionMethod(state *anchorcycle.State) string {
	raw, err := interactionpolicy.Read("text-expression-variations.json")
	if err != nil {
		return ""
	}
	var methods []string
	if json.Unmarshal(raw, &methods) != nil || len(methods) == 0 {
		return ""
	}
	// 开场段：轮换种子用 RunID 哈希，让每场开场从 12 条变体里轮换取不同的切入方式。
	// 否则开场时 TotalAccepted/CycleNumber/Attempts 全为 0，种子恒 0，永远取第 0 条「日常场景切入」，
	// 导致开场正文句式骨架固定复读。
	if state.Phase == "opening" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(state.RunID + ":expression-method"))
		return methods[int(h.Sum32()%uint32(len(methods)))]
	}
	return methods[(state.TotalAccepted()+state.CycleNumber+len(state.Attempts))%len(methods)]
}

// normalizeProductLinkLabel 把商品链接 id 规范成「X号链接」且幂等：
// 裸 id「2」→「2号链接」；已带后缀「2号链接」→ 原样返回，不重复追加「号链接」。
// 根因：生产数据源 link_key 存在「裸 id」与「带后缀」两种格式混用（DB 实测 2号链接/1号链接/3/2/1），
// 旧代码盲目 id+"号链接" 会把「2号链接」拼成「2号链接号链接」。
func normalizeProductLinkLabel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return id
	}
	if strings.HasSuffix(id, "号链接") || strings.HasSuffix(id, "号商品") {
		return id
	}
	// 纯数字或中文数字 → 补「号链接」。
	if n, err := strconv.Atoi(id); err == nil && n > 0 {
		return id + "号链接"
	}
	return id + "号链接"
}

// alignFactsToLink 把事实卡按指定商品链接二次过滤，让开场卖点与事实卡对齐。
// linkKey 是选材卖点的商品归属（裸 id，可能逗号连接多个，如 "1" 或 "1,2"）。
// 保留「Products 命中 linkKey」的商品专属事实 + 所有公共事实（Shared）。
// 过滤结果为空时返回原 facts 兜底，避免模型无事实可讲。
func alignFactsToLink(facts []anchorcycle.HookFact, linkKey string) []anchorcycle.HookFact {
	want := map[string]bool{}
	for _, k := range strings.Split(linkKey, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			want[k] = true
		}
	}
	if len(want) == 0 {
		return facts
	}
	out := make([]anchorcycle.HookFact, 0, len(facts))
	for _, f := range facts {
		if f.Shared {
			out = append(out, f)
			continue
		}
		hit := len(f.Products) == 0 // 无归属视为通用，保留
		for _, p := range f.Products {
			if want[p] {
				hit = true
				break
			}
		}
		if hit {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return facts
	}
	return out
}

// appendBrandingFacts 把公共「品牌背书」事实（地址/门店/停车/环境等）补进事实卡。
// 用于开场选材轮换到「公共卖点」（Common=true，如门店地址与停车）时：这类事实在商品段
// 被 ProductFacts 挡掉（只进公共段），导致公共卖点开场没有对应事实可讲（实测 25669）。
// 按 ID 去重，避免与已有事实重复。
func appendBrandingFacts(facts []anchorcycle.HookFact, branding []anchorcycle.HookFact) []anchorcycle.HookFact {
	if len(branding) == 0 {
		return facts
	}
	seen := map[string]bool{}
	for _, f := range facts {
		if f.ID != "" {
			seen[f.ID] = true
		}
	}
	for _, b := range branding {
		if b.ID != "" && seen[b.ID] {
			continue
		}
		if b.ID != "" {
			seen[b.ID] = true
		}
		facts = append(facts, b)
	}
	return facts
}

func renderCycleTextMessages(state *anchorcycle.State) ([]agentgateway.Message, error) {
	if state.Task().ProductMode == "lecture" {
		return renderLectureTaskMessages(state)
	}
	// 开场母版（4层结构）随环节变化：开场段才讲，非开场段不讲。
	// 已不设 system 消息，开场母版作为「环节指令」并入下方 user 消息（见 packet 拼装）。
	var phaseDirective string
	if state.Phase == "opening" {
		opening, _, e := loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_OPENING_PROMPT_PATH", "configs/prompts/anchor-style/cycle-opening.md", "开场母版")
		if e != nil {
			return nil, e
		}
		phaseDirective = opening
	}
	voiceCard, err := cycleVoiceCard(state)
	if err != nil {
		return nil, err
	}
	task := state.Task()
	// 首场开场「两次调用」：第一次选材已选定卖点×角度 + 开场方式，覆盖本次任务，
	// 让第二次写话术只聚焦选中的主线，不按队列当前任务硬写。
	if state.Phase == "opening" && state.OpeningSelection != nil {
		sel := state.OpeningSelection
		if sel.SellingPoint != "" {
			task.SellingPoint = sel.SellingPoint
		}
		if sel.Angle != "" {
			task.Angle = sel.Angle
		}
	}
	hookMode := state.TextModel != nil && cycleAnglePlanEnabled()
	if hookMode {
		task.Angle = ""
		task.Key = derivedHash(task.Key)
	}
	// The reusable expression view is separate from this task's material.
	// 四块结构：必说（本段任务，见 packet）/ 可说（本场有效事实）/ 不提（未知项）/ 改写（禁说→安全改写）。
	sideFacts := state.HookFactsForSide()
	// 开场卖点↔事实卡对齐：选材定了卖点后，事实卡须按该卖点的类型分流。
	// - 商品专属卖点（LinkKey 非空）→ 按 LinkKey 二次过滤，只留命中商品事实 + 公共事实。
	// - 公共卖点（Common=true，门店地址/停车/环境等）→ 补进 branding 公共事实（地址/停车/环境），
	//   否则这些事实在商品段被 ProductFacts 挡掉，导致公共卖点「名不副实」（实测 25669）。
	if state.Phase == "opening" && state.OpeningSelection != nil {
		sel := state.OpeningSelection
		if strings.TrimSpace(sel.LinkKey) != "" {
			sideFacts = alignFactsToLink(sideFacts, sel.LinkKey)
		} else if sel.Common {
			sideFacts = appendBrandingFacts(sideFacts, state.BrandingCommonFacts())
		}
	}
	facts, expressionGuidance := generationMaterialViews(sideFacts)
	// 稳定前缀放 system：母版 + 主播腔调卡 + 不提 + 长期纠正规则，整场不变，命中上下文缓存。
	// 随侧面/任务变化的内容（可说事实、改写约束、商品边界）放 user（见下方 variable），
	// 避免 system 每段随侧面变导致前缀缓存失效。
	// P0 不再单独塞 system 一份完整母版：改为加载精简 JSON（红线+裸词表+专属改说），
	// 由提示词工厂把红线词表合并进四层「不提」、把改说合并进「改写」，省 token 且拦截力不变。
	p0Rules, err := loadGenerationP0()
	if err != nil {
		return nil, err
	}
	// 用户已确认的长期纠正规则（pUser）不再整块塞 stable：与 P0 一样拆进四层。
	// 「表达限制」并入「不提」，「展开指导」并入「改写」，「事实修正」仍由可说事实覆盖，
	// 不在 stable 里重复携带一份完整 JSON（省 token，且与入口1事实卡的字段流向对齐）。
	corrections := state.EffectiveCorrections()
	// 不再设 system 母版：全部约束并进 user，靠「内容四层 + 表达四层」驱动生成。
	// 内容四层 = 必说（本次任务）/可说（事实包）/不提（未知项+红线词）/改写（改说）。
	// 表达四层 = 主播腔调卡（纯风格 + 量化基准 + 禁用表达）+ 近期避重，整场人设。
	// 空风格（直播间未配主播人设）时，voiceCard 为空字符串，给出明确占位让模型自由发挥，
	// 而不是把空标签塞进 prompt。
	voiceSlot := voiceCard
	if s, ok := voiceCard.(string); ok && strings.TrimSpace(s) == "" {
		voiceSlot = "（本场未配置主播风格，请用自然、得体的口播语气自由表达，不套任何固定腔调）"
	}
	variable := map[string]any{
		"可说": facts,
		// 主播腔调卡（表达层）：风格、量化基准、禁用表达，整场不变的人设语气。
		"主播腔调": voiceSlot,
		// 四层「不提」统一文案：未知项正反都不说。
		"不提": fourLayerNotMentionText,
		// 红线词表合并进「不提」层：主动规避高频极限词/功效词/逼单词。
		"不提红线词": speechbase.RenderP0ForbiddenWordList(p0Rules),
		// 改说合并进「改写」层：每条红线配一句专属安全替换，被动降级"把话说满"的冲动。
		"改写": speechbase.RenderP0RewriteGuidance(p0Rules),
	}
	// pUser 表达限制并进「不提」：完整语义禁令按原句保留，不与 P0 裸词表混抽。
	if puserForbidden := speechbase.RenderPUserForbiddenList(corrections); puserForbidden != "" {
		variable["不提红线词"] = variable["不提红线词"].(string) + "\n\n用户确认的表达限制（本段一律不出现）：\n" + puserForbidden
	}
	// pUser 展开指导并进「改写」：用户明确给出的正向替换说法按原句保留。
	if puserRewrite := speechbase.RenderPUserRewriteGuidance(corrections); puserRewrite != "" {
		variable["改写"] = variable["改写"].(string) + "\n\n用户确认的展开指导：\n" + puserRewrite
	}
	if len(expressionGuidance) > 0 {
		if g, err := json.Marshal(expressionGuidance); err == nil {
			variable["改写"] = variable["改写"].(string) + "\n\n事实卡补充改说约束：" + string(g)
		}
	}
	if task.FactScope != nil {
		boundary, guidance := generationMaterialViews(task.FactScope.ProductBoundary)
		variable["商品必要事实与使用边界（用于约束，不要求逐条讲述）"] = boundary
		variable["商品必要事实的表达约束（不是新增事实）"] = guidance
	}
	recent := []string{}
	for _, part := range state.Accepted[max(0, len(state.Accepted)-8):] {
		if part.Task.ProductMode == "lecture" {
			continue
		}
		if task.FactScope != nil && part.Task.Key != task.Key {
			continue
		}
		recent = append(recent, cycleVoiceText(part.Selected))
	}
	feedback := state.PendingFeedback
	if len(state.Attempts) > 0 {
		feedback += "\n" + state.Attempts[len(state.Attempts)-1].RetryFeedback
	}
	maxChars := state.Config.MaxChars
	if task.ProductMaxChars > 0 && task.ProductMaxChars < maxChars {
		maxChars = task.ProductMaxChars
	}
	// Vary explanation methods within the selected purpose/angle, not the task itself.
	opener := "address_or_content"
	if state.TotalAccepted()%3 != 0 {
		opener = "content_first"
	}
	isOpening := state.Phase == "opening"
	packet := map[string]any{"开头模式": opener, "本次任务": generationTaskView(task), "环节": state.Phase, "字数上限": maxChars}
	// 开场段不注入「近期已采用原文」与「表达展开建议」：卖点/角度/开场方式已由程序随机，
	// 且首场开场本无历史正文，喂完整历史原文会让模型照抄，导致开场方式锁死、正文复读。
	// 主线中段保留这两者用于避重。
	if !isOpening {
		packet["近期已采用内容仅供避重"] = tailText(strings.Join(recent, "\n"), 2400)
	}
	// 「后续表达加强建议」保留注入（含重试反馈 + first-live-opening.md 的「防编造」纯指令），
	// 但历史开场原文已在 first_live_opening.go 停止注入，这里只剩纯指令，不是复读源。
	if strings.TrimSpace(feedback) != "" {
		packet["后续表达加强建议"] = tailText(feedback, 1200)
	}
	// 随侧面/任务变化的内容并入 user，保持 system 稳定前缀。
	for k, v := range variable {
		packet[k] = v
	}
	if method := cycleExpressionMethod(state); method != "" && !isOpening {
		packet["表达展开建议"] = method
	}
	// 直播时切入侧面裂变：程序按已采纳段数轮换切入侧面，硬保证相邻段落从
	// 不同侧面切入，让同一事实不重复讲法。侧面池产品无关，展开内容由本场事实决定。
	if side := state.CurrentFissionSide(); side != "" {
		packet["本次切入侧面"] = side
	}
	// 侧面素材存在多维度/矛盾事实时，提示模型本段只讲一条主线、其余留待后续段，
	// 避免把「6人餐」与「适合3-4人」这类互斥信息揉进同一段。
	if hint := state.FissionFactConflictHint(); hint != "" {
		packet["事实维度提示"] = hint
	}
	links := []string{}
	for _, id := range state.HookProducts() {
		if id != "current" {
			links = append(links, normalizeProductLinkLabel(id))
		}
	}
	packet["本段授权的商品称呼"] = links
	applyCycleAnglePlanPacket(state, packet)
	if state.Phase == "closing" {
		// Phase names are execution labels, not a selling point for the audience.
		delete(packet, "本次任务")
		delete(packet, "讲述目的")
		packet["本段沟通任务"] = "自然结束当前话题，按当前相关事实帮助观众理解选择；无需宣告环节名称。"
	}
	if hookMode || state.CurrentAnglePlan() != nil {
		// Adopted work records provide bounded continuity without turning the
		// last spoken paragraph into the next paragraph's reference example.
		memory := []map[string]any{}
		for _, part := range state.Accepted[max(0, len(state.Accepted)-8):] {
			if part.Task.ProductMode == "lecture" || (task.FactScope != nil && part.Task.Key != state.Task().Key) {
				continue
			}
			if h := part.Selected.AngleHook; h != nil {
				memory = append(memory, map[string]any{"卖点": part.Task.SellingPoint, "展开方向": h.Action})
			} else {
				memory = append(memory, map[string]any{"卖点": part.Task.SellingPoint, "环节": part.Phase})
			}
		}
		delete(packet, "近期已采用内容仅供避重")
		wordings := []string{}
		for _, part := range state.Accepted[max(0, len(state.Accepted)-8):] {
			if part.Task.Key == state.Task().Key && part.Task.ProductMode != "lecture" && state.CheckApprovedText(part.Selected, cycleVoiceText(part.Selected)) == nil {
				wordings = append(wordings, cycleVoiceText(part.Selected))
			}
		}
		if len(wordings) > 0 {
			packet["近期正文（仅供文字避重，不是事实或范文）"] = tailText(strings.Join(wordings[max(0, len(wordings)-2):], "\n"), 1000)
		}
		packet["近期已采用任务记录（不代表已播放）"] = memory
		// At most two same-direction paragraphs provide phrasing context only.
		// They never become an additional source of product facts or promises.
		if current := state.CurrentAngleHook(); current != nil {
			previous := []string{}
			for i := len(state.Accepted) - 1; i >= 0 && len(previous) < 2; i-- {
				part := state.Accepted[i]
				if part.Task.Key == state.Task().Key && part.Selected.AngleHook != nil && part.Selected.AngleHook.Action == current.Action && state.CheckApprovedText(part.Selected, cycleVoiceText(part.Selected)) == nil {
					previous = append(previous, cycleVoiceText(part.Selected))
				}
			}
			if len(previous) > 0 {
				packet["同方向近期原文（仅避重，不是事实依据或范文）"] = tailText(strings.Join(previous, "\n"), 1200)
			}
		}
	}
	// 旧「开头沟通意图」（PendingOpeningHook）已废弃：它把「第一句钩子方向」塞进 prompt，
	// 与新的「两次调用自选开场方式 + 欢迎+留人四步」抢第一句，导致欢迎被「别划走/怕没选明白」顶掉。
	// 开场第一句只由「欢迎+留人」四步 + 「本次开场方式」驱动，不再注入任何钩子方向。
	if state.PriceHook != nil {
		packet["上一段待解释的问题"] = state.PriceHook.Promise
	}
	// 内容策略表（内容方向与采用条件）已移除：话术方向就是「卖点×角度+侧面」，
	// 由「本次任务」和「本次切入侧面」直接驱动，策略表与任务高度重复，纯浪费 token。
	// 局部重写已移除，不再拼接「本次明确违规修正」。
	// 讲解回归母版同样是环节特有指令，并入 user 保持 system 前缀稳定。
	var transitionDirective string
	if len(state.Accepted) > 0 && state.Accepted[len(state.Accepted)-1].Task.ProductMode == "lecture" && !state.Accepted[len(state.Accepted)-1].LectureSkipped {
		last := state.Accepted[len(state.Accepted)-1]
		packet["刚结束的讲解"] = map[string]any{"主题": last.Task.SellingPoint, "结尾": tailText(cycleVoiceText(last.Selected), 80)}
		transition, _, e := loadReplaceablePromptAsset("ANCHOR_LECTURE_RETURN_PROMPT_PATH", "configs/prompts/anchor-style/lecture-mainline-transition.md", "讲解回归母版")
		if e != nil {
			return nil, e
		}
		transitionDirective = transition
	}
	if phaseDirective != "" || transitionDirective != "" {
		directive := strings.TrimSpace(strings.Join([]string{phaseDirective, transitionDirective}, "\n\n"))
		if directive != "" {
			packet["环节指令（仅本段生效，不作为商品事实）"] = directive
		}
	}
	// 首场开场选材结果注入「本次开场方式」：模型第二次写话术按这个方式走钩子。
	if state.Phase == "opening" && state.OpeningSelection != nil && state.OpeningSelection.OpeningMode != "" {
		packet["本次开场方式"] = state.OpeningSelection.OpeningMode
	}
	// 开头「欢迎 + 留人」样板：程序按 RunID 轮换选一条，模型照骨架组织措辞、
	// 把 {称呼} 换成主播自己的称呼风格，避免开场欢迎与留人话场场雷同。
	// 只影响开头欢迎两句，后面的 8 类开场方式钩子照旧。
	if state.Phase == "opening" {
		if tpl := openingWelcomeTemplateForRun(state.RunID); tpl != "" {
			packet["本次开场欢迎样板"] = tpl + "（照这个骨架组织欢迎和留人，{称呼} 换成主播自己的称呼风格，不要逐字照抄，留人话也可以换一种说法）"
		}
	}
	dynamic, _ := json.Marshal(packet)
	// 不设 system 消息：两个四层（内容 + 表达）全部并进 user，system 一个不留。
	return []agentgateway.Message{{Role: "user", Content: string(dynamic)}}, nil
}
func (s *Server) completeCycleText(ctx context.Context, state *anchorcycle.State) (agentgateway.Response, []agentgateway.Message, error) {
	if err := s.installCurrentExpressionConstraints(ctx, state); err != nil {
		return agentgateway.Response{}, nil, fmt.Errorf("读取用户采用的表达约束失败：%w", err)
	}
	if err := requireCycleP0(state); err != nil {
		return agentgateway.Response{}, nil, err
	}
	if err := s.ensureLectureSegmentPlan(ctx, state); err != nil {
		return agentgateway.Response{}, nil, fmt.Errorf("%w：%s", errLecturePlanning, err)
	}
	current, err := s.store.ResolveSpeechModel(ctx, state.Config.TextProfileID)
	if err != nil || current == nil || !current.Enabled || current.APIKey == "" || state.TextModel == nil {
		return agentgateway.Response{}, nil, fmt.Errorf("本场文字模型凭据不可用")
	}
	profile := *state.TextModel
	profile.APIKey = current.APIKey
	s.ensureFactExpressionExamples(ctx, state)
	if cycleAnglePlanEnabled() {
		s.ensureCycleAnglePlan(ctx, state, profile)
	}
	// 首场开场「两次调用」第一次：模型从候选池选「卖点×角度 + 开场方式」。
	// 选材结果写回 state.OpeningSelection，供 renderCycleTextMessages 聚焦本次任务。
	if state.Phase == "opening" && state.OpeningSelection == nil {
		if selErr := s.completeOpeningSelection(ctx, state, profile); selErr != nil {
			state.Warn("开场选材调用失败，退回原开场钩子流程：" + selErr.Error())
		}
	}
	messages, err := renderCycleTextMessages(state)
	if err != nil {
		return agentgateway.Response{}, nil, err
	}
	// 局部重写已移除，主线统一走纯文本正文生成（ResponseText）。
	stage := "mainline_text"
	responseFormat := agentgateway.ResponseText
	req := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: messages, ResponseFormat: responseFormat, MaxTokens: 1800, EnableThinking: false, CacheSystemPrompt: true, Timeout: time.Duration(profile.TimeoutMS) * time.Millisecond}
	if profile.SystemPrompt != "" {
		req.Messages = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, req.Messages...)
	}
	req, err = agentgateway.PrepareTextRequest(profile, req)
	if err != nil {
		return agentgateway.Response{}, messages, err
	}
	state.TextPromptSHA = req.TextPromptSHA
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(ctx, "mainline_text", profile.ID, req)
	if err != nil {
		return agentgateway.Response{}, req.Messages, err
	}
	started := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(ctx, profile, req)
	response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
	err = s.finishModelExchange(ctx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
	// 生成后同义改写：换「使用方式/动作」措辞、锁死数字与称呼，打破高频短语（如"叮1分钟"）复读。
	// 失败降级用原文，不阻塞。
	// 局部重写（P0PendingRepair）不参与同义改写与价格漂移检查：重写结果是 replacements JSON，
	// 同义改写会用纯文本协议把 JSON 改写成正文，破坏 patch.Apply 的解析。
	// 润色降级：同一卖点×角度近期复讲（侧面轮换一圈回到同侧面）才润色，首次讲不润色，
	// 省一次模型调用（正文 + 侧面筛事实已拉开结构）。
	if err == nil && strings.TrimSpace(response.Text) != "" {
		if state.RecentSameTask(8) {
			if rewritten, rwErr := s.rewriteCycleText(ctx, state, response.Text); rwErr == nil && strings.TrimSpace(rewritten) != "" {
				response.Text = rewritten
			}
		}
		// 润色后「商品名/价格/链接号」词语组核对：检测链接号→价格串换，串错则随机二选一纠正。
		if state.ProductFocus != nil {
			if drifts := anchorcycle.CheckPriceDrift(response.Text, state.ProductFocus.Products); len(drifts) > 0 {
				rng := rand.New(rand.NewSource(time.Now().UnixNano()))
				// 从后往前纠正，避免文本长度变化打乱后续偏移。
				for i := len(drifts) - 1; i >= 0; i-- {
					response.Text = anchorcycle.ApplyPriceCorrection(response.Text, drifts[i], rng)
				}
			}
		}
	}
	return response, req.Messages, err
}
