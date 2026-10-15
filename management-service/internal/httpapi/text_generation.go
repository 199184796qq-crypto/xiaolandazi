package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"math/rand"
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
	b, err := anchorstylescore.Parse(state.BaselineJSON)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"语气词": b.Identity.Particles, "称呼": b.Identity.Audience, "自指": b.Identity.Self,
		"节奏":     map[string]any{"平均分句长度": b.Identity.Rhythm.Length, "长度区间": b.Identity.Rhythm.LengthRange, "问句频率": b.Identity.Rhythm.QuestionFrequency},
		"数字表达习惯": map[string]any{"价格表述方式": b.Identity.Numbers.Style, "自然帮观众算账": b.Identity.Numbers.LikesArithmetic},
		"量化基准":   b.Quant, "表达偏好": b.Forbidden,
	}, nil
}
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
	return methods[(state.TotalAccepted()+state.CycleNumber+len(state.Attempts))%len(methods)]
}
func renderCycleTextMessages(state *anchorcycle.State) ([]agentgateway.Message, error) {
	if state.Task().ProductMode == "lecture" {
		return renderLectureTaskMessages(state)
	}
	foundation, err := interactionpolicy.Read("mainline-text-task.md")
	if err != nil {
		return nil, err
	}
	// 开场环节追加专用开场母版（4层结构），让口播模型写完整开场：
	// 第一句钩子 → 身份背书 → 卖点×角度 → 轻呼告，而不是只报一句欢迎。
	if state.Phase == "opening" {
		opening, _, e := loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_OPENING_PROMPT_PATH", "configs/prompts/anchor-style/cycle-opening.md", "开场母版")
		if e != nil {
			return nil, e
		}
		foundation = append(foundation, []byte("\n\n"+opening)...)
	}
	view, err := cycleExpressionView(state)
	if err != nil {
		return nil, err
	}
	task := state.Task()
	hookMode := state.TextModel != nil && cycleAnglePlanEnabled()
	if hookMode {
		task.Angle = ""
		task.Key = derivedHash(task.Key)
	}
	// The reusable expression view is separate from this task's material.
	// 四块结构：必说（本段任务，见 packet）/ 可说（本场有效事实）/ 不提（未知项）/ 改写（禁说→安全改写）。
	facts, expressionGuidance := generationMaterialViews(state.HookFactsForSide())
	material := map[string]any{"可说": facts, "主播表达标尺": view, "有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	if len(expressionGuidance) > 0 {
		material["改写"] = expressionGuidance
	}
	material["不提"] = "事实未提供、未确认的未知项，正反都不说：既不说“能/有/支持”，也不说“不能/没有/不支持”；必要时只说“这点需要帮您确认”。"
	material["用户已确认的长期纠正规则"] = state.EffectiveCorrections()
	if task.FactScope != nil {
		boundary, guidance := generationMaterialViews(task.FactScope.ProductBoundary)
		material["商品必要事实与使用边界（用于约束，不要求逐条讲述）"] = boundary
		material["商品必要事实的表达约束（不是新增事实）"] = guidance
	}
	fixed, _ := json.Marshal(material)
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
	packet := map[string]any{"开头模式": opener, "本次任务": generationTaskView(task), "环节": state.Phase, "开场版本": state.OpeningVersion(), "本轮编号": state.CycleNumber, "中间段计划": state.BodyLimit(), "已完成中间段": state.BodyCount, "字数上限": maxChars, "近期已采用内容仅供避重": tailText(strings.Join(recent, "\n"), 2400), "后续表达加强建议": tailText(feedback, 1200)}
	if method := cycleExpressionMethod(state); method != "" {
		packet["表达展开建议"] = method
	}
	// 直播时切入侧面裂变：程序按已采纳段数轮换切入侧面，硬保证相邻段落从
	// 不同侧面切入，让同一事实不重复讲法。侧面池产品无关，展开内容由本场事实决定。
	if side := state.CurrentFissionSide(); side != "" {
		packet["本次切入侧面"] = side
	}
	links := []string{}
	for _, id := range state.HookProducts() {
		if id != "current" {
			links = append(links, id+"号链接")
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
	if hook := state.PendingOpeningHook; state.Phase == "opening" && hook != nil {
		packet["开头沟通意图"] = map[string]any{"类型": hook.Kind, "第一句钩子方向（参考，不照抄）": hook.FilledTemplate, "需要说明的事实条件": hook.HardValues, "关注对象与顾虑": hook.SoftValues}
	}
	if state.PriceHook != nil {
		packet["上一段待解释的问题"] = state.PriceHook.Promise
	}
	if strategy, err := state.ContentStrategy.Render("主线", state.GenerationFacts()); err != nil {
		return nil, err
	} else if strategy != "" && state.CurrentAnglePlan() == nil && !hookMode {
		packet["内容方向与采用条件"] = strategy
	}
	if state.P0PendingRepair != nil {
		repair, err := renderP0Repair(state)
		if err != nil {
			return nil, err
		}
		packet["本次明确违规修正"] = repair
	}
	dynamic, _ := json.Marshal(packet)
	if len(state.Accepted) > 0 && state.Accepted[len(state.Accepted)-1].Task.ProductMode == "lecture" && !state.Accepted[len(state.Accepted)-1].LectureSkipped {
		last := state.Accepted[len(state.Accepted)-1]
		packet["刚结束的讲解"] = map[string]any{"主题": last.Task.SellingPoint, "结尾": tailText(cycleVoiceText(last.Selected), 80)}
		dynamic, _ = json.Marshal(packet)
		transition, _, e := loadReplaceablePromptAsset("ANCHOR_LECTURE_RETURN_PROMPT_PATH", "configs/prompts/anchor-style/lecture-mainline-transition.md", "讲解回归母版")
		if e != nil {
			return nil, e
		}
		foundation = append(foundation, []byte("\n\n"+transition)...)
	}
	return []agentgateway.Message{{Role: "system", Content: string(foundation) + "\n\n" + string(fixed)}, {Role: "user", Content: string(dynamic)}}, nil
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
	messages, err := renderCycleTextMessages(state)
	if previous, ok := ctx.Value(factPreviewRepairKey{}).(map[string]any); ok {
		messages, err = factSpeechRepairMessages(state, previous)
	}
	if err != nil {
		return agentgateway.Response{}, nil, err
	}
	// 局部重写（P0PendingRepair != nil）要求模型返回 {"replacements":[...]} JSON，
	// 必须用 ResponseJSON 协议，否则 qwen 在纯文本协议下会返回整段正文导致 Apply 解析失败。
	stage := "mainline_text"
	responseFormat := agentgateway.ResponseText
	if state.P0PendingRepair != nil {
		stage = "mainline_text_repair"
		responseFormat = agentgateway.ResponseJSON
	}
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
	if err == nil && state.P0PendingRepair == nil && strings.TrimSpace(response.Text) != "" {
		if rewritten, rwErr := s.rewriteCycleText(ctx, state, response.Text); rwErr == nil && strings.TrimSpace(rewritten) != "" {
			response.Text = rewritten
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
