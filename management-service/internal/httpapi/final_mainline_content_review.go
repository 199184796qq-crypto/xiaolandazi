package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"strings"
	"time"
)

func finalMainlineContentMessages(state *anchorcycle.State, c anchorcycle.Candidate, mother string) ([]agentgateway.Message, error) {
	if e := requireCycleP0(state); e != nil {
		return nil, e
	}
	facts, guidance := generationMaterialViews(state.HookFacts())
	// 稳定事实包（本场直播不变）单独放一条 system，命中上下文缓存、省输入费。
	factsPacket := map[string]any{"当前相关事实": facts, "相关事实的表达约束及举例": guidance, "用户已确认要求": state.EffectiveCorrections()}
	if scope := state.Task().FactScope; scope != nil {
		boundary, boundaryGuidance := generationMaterialViews(scope.ProductBoundary)
		factsPacket["商品必要事实与适用边界"] = boundary
		factsPacket["商品必要事实的表达约束"] = boundaryGuidance
	}
	factsRaw, e := json.Marshal(factsPacket)
	if e != nil {
		return nil, e
	}

	packet := map[string]any{"当前任务": generationTaskView(state.Task()), "正文": cycleVoiceText(c), "前次P0审核结果": priorP0ReviewView(state, c)}
	raw, e := json.Marshal(packet)
	if e != nil {
		return nil, e
	}
	p0, e := json.Marshal(map[string]any{"完整有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)})
	if e != nil {
		return nil, e
	}
	messages := []agentgateway.Message{}
	if fourLayer, _, ferr := loadAuditFourLayerPrompt(); ferr == nil && strings.TrimSpace(fourLayer) != "" {
		messages = append(messages, agentgateway.Message{Role: "system", Content: fourLayer})
	}
	messages = append(messages,
		agentgateway.Message{Role: "system", Content: string(p0)},
		agentgateway.Message{Role: "system", Content: mother},
		agentgateway.Message{Role: "system", Content: string(factsRaw)},
		agentgateway.Message{Role: "user", Content: string(raw)},
	)
	return messages, nil
}

func (s *Server) auditFinalMainlineContent(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
	if state.Config.SkipReview {
		return
	}
	state.FinalContentReviewRequired = true
	text := cycleVoiceText(*c)
	a := &anchorcycle.FinalContentAudit{TextSHA: anchorcycle.ProductDigest(text), ScopeSHA: state.FinalContentSourceSHA()}
	fail := func(message string) { a.Error = message; c.FinalContentAudit = a }
	if text == "" {
		fail("最终正文为空")
		return
	}
	if e := requireCycleP0(state); e != nil {
		fail(e.Error())
		return
	}
	// A cleanup may have changed the approved body. Complete its P0 review first;
	// no second content model can override an unfinished or failed P0 receipt.
	if e := state.CheckP0Candidate(c); e != nil {
		fail(e.Error())
		return
	}
	text = cycleVoiceText(*c)
	a.TextSHA = anchorcycle.ProductDigest(text)
	if text == "" {
		fail("最终正文为空")
		return
	}
	if state.CheckApprovedText(*c, text) != nil {
		fail("当前P0尚未取得匹配的通过结果")
		return
	}
	if constraints := state.EffectiveExpressionConstraints(); len(constraints) > 0 {
		requirementMother, _, e := loadReplaceablePromptAsset("ANCHOR_SPEECH_CONSTRAINT_TEXT_REVIEW_PATH", "configs/prompts/anchor-style/speech-constraint-text-review.md", "用户约束文字审核母版")
		if e != nil {
			fail("表达要求审核母版不可用")
			return
		}
		previous := c.ExpressionAudit
		if previous == nil || previous.TextSHA != anchorcycle.ProductDigest(text) || previous.ScopeSHA != anchorcycle.ProductDigest(constraints) || previous.PromptSHA != derivedHash(requirementMother) {
			s.auditSpeechExpressionConstraints(ctx, state, c)
		}
		if c.ExpressionAudit == nil || c.ExpressionAudit.Error != "" {
			fail("当前明确表达要求核对未完成")
			return
		}
		if !c.ExpressionAudit.Passed {
			a.Decision, a.UpstreamReview, a.Reason = "有问题", "speech_constraint_text_review", c.ExpressionAudit.Reason
			c.FinalContentAudit = a
			return
		}
	}
	mother, _, err := loadReplaceablePromptAsset("MAINLINE_FINAL_CONTENT_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/mainline-final-content-review.md", "最终文字事实与要求审核母版")
	if err != nil {
		fail("最终文字审核母版不可用")
		return
	}
	id, err := s.store.ResolveTextRole(ctx, "mainline_final_content_review")
	if err != nil {
		fail("最终文字审核角色不可用")
		return
	}
	if state.ReviewModel != nil {
		id = state.ReviewModel.ID
	}
	current, err := s.store.ResolveSpeechModel(ctx, id)
	if err != nil || current == nil || !current.Enabled {
		fail("最终文字审核模型不可用")
		return
	}
	profile := *current
	if state.ReviewModel != nil {
		profile = *state.ReviewModel
		profile.APIKey = current.APIKey
	}
	requirementModel, requirementMother := "", ""
	if audit := c.ExpressionAudit; audit != nil {
		requirementModel, requirementMother = audit.Model, audit.PromptSHA
	}
	contextSHA := finalMainlineReviewContext(a.ScopeSHA, profile, mother, requirementModel, requirementMother)
	state.FinalContentReviewContextSHA = contextSHA
	if previous := c.FinalContentAudit; previous != nil && previous.ContextSHA == contextSHA && previous.TextSHA == a.TextSHA && previous.ScopeSHA == a.ScopeSHA && previous.Error == "" {
		return
	}
	a.ContextSHA, a.Model, a.ProfileID, a.PromptSHA = contextSHA, profile.Model, profile.ID, derivedHash(mother)
	c.FinalContentAudit = a
	messages, err := finalMainlineContentMessages(state, *c, mother)
	if err != nil {
		fail(err.Error())
		return
	}
	request := agentgateway.Request{Stage: "mainline_final_content_review", Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, MaxTokens: 600, EnableThinking: false, CacheSystemPrompt: true, Timeout: 10 * time.Second}
	err = runFinalMainlineReview(ctx, request.Timeout, a, func(work context.Context) (anchorcycle.FinalReviewAttempt, error) {
		work = withExchangeScope(work, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID, CandidateID: c.ID})
		result := anchorcycle.FinalReviewAttempt{}
		exchange, err := s.beginModelExchange(work, request.Stage, profile.ID, request)
		if err != nil {
			return result, err
		}
		result.ExchangeID = exchange
		started := time.Now()
		response, err := agentgateway.CompleteSpeechEndpoint(work, profile, request)
		response.Model, response.Provider = profile.Model, "anchor:"+profile.ID
		err = s.finishModelExchange(work, exchange, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
		result.Raw, result.InputTokens, result.OutputTokens = response.Text, response.InputTokens, response.OutputTokens
		return result, err
	})
	if err != nil {
		fail(err.Error())
	}
}
