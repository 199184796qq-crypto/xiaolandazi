package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"time"
)

func finalMainlineContentMessages(state *anchorcycle.State, c anchorcycle.Candidate, mother string) ([]agentgateway.Message, error) {
	if e := requireCycleP0(state); e != nil {
		return nil, e
	}
	facts, guidance := generationMaterialViews(state.HookFacts())
	packet := map[string]any{"当前任务": generationTaskView(state.Task()), "正文": cycleVoiceText(c), "当前相关事实": facts, "相关事实的表达约束及举例": guidance, "用户已确认要求": state.EffectiveCorrections(), "前次P0审核结果": priorP0ReviewView(state, c)}
	if scope := state.Task().FactScope; scope != nil {
		packet["商品必要事实与适用边界"] = generationFactView(scope.ProductBoundary)
	}
	if state.Task().ProductMode == "lecture" {
		packet["本次讲解资料"] = state.Task().LectureFacts
	}
	raw, e := json.Marshal(packet)
	if e != nil {
		return nil, e
	}
	p0, e := json.Marshal(map[string]any{"完整有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)})
	if e != nil {
		return nil, e
	}
	return []agentgateway.Message{{Role: "system", Content: string(p0)}, {Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, nil
}

func (s *Server) auditFinalMainlineContent(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
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
	s.reviewCycleP0Context(ctx, state, c)
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
		if c.ExpressionAudit == nil || !c.ExpressionAudit.Passed || c.ExpressionAudit.Error != "" {
			fail("当前明确表达要求未通过或核对未完成")
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
	contextSHA := anchorcycle.ProductDigest(struct{ Source, Model, Profile, Endpoint, Mother, RequirementModel, RequirementMother string }{a.ScopeSHA, profile.Model, profile.ID, profile.BaseURL, mother, requirementModel, requirementMother})
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
	request := agentgateway.Request{Stage: "mainline_final_content_review", Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, MaxTokens: 600, EnableThinking: false, Timeout: 10 * time.Second}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	work = withExchangeScope(work, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID, CandidateID: c.ID})
	exchange, err := s.beginModelExchange(work, request.Stage, profile.ID, request)
	if err != nil {
		fail("最终文字审核记录未完成")
		return
	}
	a.ExchangeID = exchange
	started := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(work, profile, request)
	response.Model, response.Provider = profile.Model, "anchor:"+profile.ID
	err = s.finishModelExchange(work, exchange, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
	a.Raw, a.InputTokens, a.OutputTokens, a.LatencyMS = response.Text, response.InputTokens, response.OutputTokens, time.Since(started).Milliseconds()
	if err != nil {
		fail("最终文字审核调用未完成")
		return
	}
	a.Decision, err = anchorcycle.ParseDirectionReview(response.Text)
	if err != nil {
		fail(err.Error())
	}
}
