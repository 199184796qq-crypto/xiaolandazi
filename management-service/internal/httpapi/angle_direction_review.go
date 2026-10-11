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

func angleDirectionReviewContext(state *anchorcycle.State, profile agentgateway.SpeechModel, mother string) string {
	return anchorcycle.ProductDigest(struct{ Source, Scope, P0, Mother, Profile, Model, Endpoint string }{state.AnglePlanKey(), state.Config.ComplianceScope, state.P0PolicyMD, mother, profile.ID, profile.Model, profile.BaseURL})
}

func angleDirectionReviewMessages(state *anchorcycle.State, h anchorcycle.AngleHook, mother string) ([]agentgateway.Message, error) {
	if err := requireCycleP0(state); err != nil {
		return nil, err
	}
	input := map[string]any{"当前任务": generationTaskView(state.Task()), "候选方向": map[string]string{"讨论内容": h.Action, "标题": h.Title, "观众顾虑": h.Concern, "切入方法": h.Entry}, "当前相关事实": generationFactView(state.HookFacts()), "有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	input["用户已确认纠正规则"] = state.EffectiveCorrections()
	if scope := state.Task().FactScope; scope != nil {
		input["商品必要事实与使用边界"] = generationFactView(scope.ProductBoundary)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, nil
}

// Used by the production entrypoint and isolated provider tests. Each callback
// receives one content job; local IDs and bindings never depend on the model.
func filterAngleDirectionsWithReview(ctx context.Context, p *anchorcycle.AnglePlan, contextSHA string, cached []anchorcycle.DirectionReviewReceipt, review func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt) {
	if p.ReviewContextSHA == contextSHA && contextSHA != "" {
		p.Cards = anchorcycle.ReviewedDirectionCards(p.Cards, p.DirectionReviews, contextSHA)
		return
	}
	if len(p.ReviewCandidates) == 0 {
		p.ReviewCandidates = append([]anchorcycle.AngleHook(nil), p.Cards...)
	}
	receipts := append(append([]anchorcycle.DirectionReviewReceipt(nil), p.DirectionReviews...), cached...)
	current := []anchorcycle.DirectionReviewReceipt{}
	for _, h := range anchorcycle.DistinctDirectionCards(p.ReviewCandidates) {
		var found *anchorcycle.DirectionReviewReceipt
		for _, r := range receipts {
			if r.Matches(h, contextSHA) {
				copy := r
				copy.HookID = h.ID
				found = &copy
				break
			}
		}
		if found == nil {
			r := anchorcycle.DirectionReviewReceipt{HookID: h.ID, ContentSHA: anchorcycle.DirectionContentSHA(h), ContextSHA: contextSHA}
			if err := ctx.Err(); err != nil {
				r.Error = "本次方向审核预算已结束，未审核方向暂不选择"
			} else {
				r = review(ctx, h)
				r.HookID, r.ContentSHA, r.ContextSHA = h.ID, anchorcycle.DirectionContentSHA(h), contextSHA
			}
			found = &r
			receipts = append(receipts, r)
		}
		current = append(current, *found)
	}
	p.DirectionReviews = current
	p.ReviewContextSHA = contextSHA
	p.Cards = anchorcycle.ReviewedDirectionCards(p.ReviewCandidates, current, contextSHA)
}

func (s *Server) reviewCycleAngleDirections(ctx context.Context, state *anchorcycle.State, p *anchorcycle.AnglePlan) {
	if p == nil {
		return
	}
	if len(p.Cards) == 0 && len(p.ReviewCandidates) == 0 && len(p.Hooks) > 0 {
		lines := []string{}
		for _, h := range p.Hooks {
			lines = append(lines, "1. "+h)
		}
		legacy := anchorcycle.ParseDirectionContent(strings.Join(lines, "\n"), anchorcycle.ID(), state.HookFacts(), state.HookProducts(), p.Goal)
		p.Cards = legacy.Cards
	}
	if len(p.ReviewCandidates) == 0 {
		p.ReviewCandidates = append([]anchorcycle.AngleHook(nil), p.Cards...)
	}
	p.Hooks = nil // Legacy intent strings cannot bypass the reviewed card view.
	failed := func(message string) {
		p.Cards = nil
		p.ReviewContextSHA = ""
		state.Warn(message + "，使用当前任务继续供料")
	}
	if err := requireCycleP0(state); err != nil {
		failed(err.Error())
		return
	}
	mother, _, err := loadReplaceablePromptAsset("ANGLE_DIRECTION_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/angle-direction-review.md", "新方向事实与P0审核母版")
	if err != nil {
		failed("方向审核母版不可用")
		return
	}
	id, err := s.store.ResolveTextRole(ctx, "angle_direction_review")
	if err != nil {
		failed("方向审核模型配置不可用")
		return
	}
	if state.ReviewModel != nil {
		id = state.ReviewModel.ID
	}
	current, err := s.store.ResolveSpeechModel(ctx, id)
	if err != nil || current == nil || !current.Enabled {
		failed("方向审核模型不可用")
		return
	}
	profile := *current
	if state.ReviewModel != nil {
		profile = *state.ReviewModel
		profile.APIKey = current.APIKey
	}
	contextSHA := angleDirectionReviewContext(state, profile, mother)
	if p.ReviewContextSHA == contextSHA {
		p.Cards = anchorcycle.ReviewedDirectionCards(p.Cards, p.DirectionReviews, contextSHA)
		return
	}
	work, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cached := []anchorcycle.DirectionReviewReceipt{}
	if cycleAnglePlanPersistent(state) {
		cached, err = s.store.LoadAngleDirectionReviews(work, state.TenantID, state.PlanID, state.AnglePlanKey(), contextSHA)
		if err != nil {
			state.Warn("方向审核记录读取失败，本次按已有场内记录核对")
		}
	}
	filterAngleDirectionsWithReview(work, p, contextSHA, cached, func(c context.Context, h anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
		r := anchorcycle.DirectionReviewReceipt{Model: profile.Model, ProfileID: profile.ID, PromptSHA: derivedHash(mother)}
		messages, e := angleDirectionReviewMessages(state, h, mother)
		if e != nil {
			r.Error = e.Error()
			return r
		}
		request := agentgateway.Request{Stage: "angle_direction_review", Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, MaxTokens: 400, EnableThinking: false, Timeout: 6 * time.Second}
		c = withExchangeScope(c, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
		id, e := s.beginModelExchange(c, request.Stage, profile.ID, request)
		if e != nil {
			r.Error = "方向审核记录未完成"
			return r
		}
		r.ExchangeID = id
		started := time.Now()
		response, e := agentgateway.CompleteSpeechEndpoint(c, profile, request)
		response.Model, response.Provider = profile.Model, "anchor:"+profile.ID
		e = s.finishModelExchange(c, id, response, response.Text, started, response.InputTokens, response.OutputTokens, e)
		r.Raw, r.InputTokens, r.OutputTokens, r.LatencyMS = response.Text, response.InputTokens, response.OutputTokens, time.Since(started).Milliseconds()
		if e != nil {
			r.Error = "方向审核调用未完成"
			return r
		}
		r.Decision, e = anchorcycle.ParseDirectionReview(response.Text)
		if e != nil {
			r.Error = e.Error()
		}
		return r
	})
	saveCtx, stopSave := context.WithTimeout(ctx, 2*time.Second)
	defer stopSave()
	for _, r := range p.DirectionReviews {
		if r.Completed() && cycleAnglePlanPersistent(state) {
			if err := s.store.SaveAngleDirectionReview(saveCtx, state.TenantID, state.PlanID, state.AnglePlanKey(), r); err != nil {
				state.Warn("方向审核结果暂未保存，本场仍按已完成判断选择")
			}
		}
	}
	if len(p.Cards) == 0 {
		state.Warn("本次方向未取得可用审核结果，保留历史，按当前任务继续供料")
	}
}
