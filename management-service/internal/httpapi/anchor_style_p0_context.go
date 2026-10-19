package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"time"
)

func cycleP0ContextMessages(state *anchorcycle.State, candidate anchorcycle.Candidate, mother string) ([]agentgateway.Message, error) {
	if candidate.P0Report == nil {
		return nil, errors.New("文字扫描回执缺失")
	}
	policy, err := contentguard.Compile(state.P0PolicyMD)
	if err != nil {
		return nil, err
	}
	items, err := policy.ContextContent(*candidate.P0Report)
	if err != nil {
		return nil, err
	}
	// 稳定内容（范围/确认事实/有效P0 整场不变）挪进 system 作缓存前缀；
	// 变化内容（正文/命中规则）留在 user。配合 CacheSystemPrompt 让 DashScope 前缀缓存 system。
	stable, err := json.Marshal(map[string]any{"范围": state.Config.ComplianceScope, "确认事实": state.ProductFacts(state.Task()), "有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)})
	if err != nil {
		return nil, err
	}
	packet, err := json.Marshal(map[string]any{"正文": cycleVoiceText(candidate), "待核对规则内容": items})
	if err != nil {
		return nil, err
	}
	return []agentgateway.Message{{Role: "system", Content: mother + "\n\n" + string(stable)}, {Role: "user", Content: string(packet)}}, nil
}

// This is the sole final-text review, only for rules explicitly marked semantic.
// It does not assess sales quality, arithmetic phrasing, or rewrite the prose.
func (s *Server) reviewCycleP0Context(ctx context.Context, state *anchorcycle.State, candidate *anchorcycle.Candidate) {
	if state.Config.SkipReview {
		return
	}
	s.reviewCycleP0ContextOnce(ctx, state, candidate)
	if candidate.P0Report == nil || candidate.P0Report.ContextReview == nil || candidate.P0Report.ContextReview.Output.InstructionLeak == nil || !*candidate.P0Report.ContextReview.Output.InstructionLeak {
		return
	}
	previous := candidate.P0Report.ContextReview
	if !cleanCycleOpeningInstruction(state, candidate, previous.Output.InstructionQuote) {
		return
	}
	candidate.OpeningInstructionEdits[len(candidate.OpeningInstructionEdits)-1].Review = previous
	if err := state.CheckP0Candidate(candidate); err != nil {
		candidate.P0Report.Passed, candidate.P0Report.Error = false, err.Error()
		candidate.P0ApprovedTextSHA = ""
		return
	}
	// Bounded final review of the edited body. Never recursively delete or retry.
	s.reviewCycleP0ContextOnce(ctx, state, candidate)
}

func cycleInstructionSources(state *anchorcycle.State) []string {
	sources := []string{state.Task().SellingPoint, state.Task().Angle, state.PendingFeedback, cycleExpressionMethod(state)}
	if plan := state.CurrentAnglePlan(); plan != nil {
		sources = append(sources, plan.Goal, plan.Action)
		sources = append(sources, plan.Hooks...)
		if h := state.CurrentAngleHook(); h != nil {
			sources = append(sources, h.Concern, h.Entry, h.Action)
		}
	}
	return sources
}

func cleanCycleOpeningInstruction(state *anchorcycle.State, candidate *anchorcycle.Candidate, quote string) bool {
	labels := []string{}
	if state.Phase == "closing" && state.Task().SellingPoint == "本轮收口" {
		labels = []string{"本轮收口", "这轮收口", "这一轮收口", "本段收口", "这段收口", "本次收口", "这次收口"}
	}
	changed := false
	if quote == "" {
		if state.TotalAccepted()%3 != 0 {
			labels := []string{}
			for _, link := range state.HookProducts() {
				if link != "current" {
					labels = append(labels, link+"号链接")
				}
			}
			text, edit := contentguard.StripLeadingProductControl(cycleVoiceText(*candidate), "直接说", labels)
			if edit != nil {
				candidate.OutputText = text
				candidate.OpeningInstructionEdits = append(candidate.OpeningInstructionEdits, *edit)
				changed = true
			}
		}
		text, edits, e := contentguard.StripRequestInstructions(cycleVoiceText(*candidate), cycleInstructionSources(state), labels)
		if e != nil {
			candidate.P0PatchError = e.Error()
			return false
		}
		if len(edits) > 0 {
			candidate.OutputText = text
			candidate.OpeningInstructionEdits = append(candidate.OpeningInstructionEdits, edits...)
			changed = true
		}
	}
	cleaned, edit, err := contentguard.StripOpeningInstruction(cycleVoiceText(*candidate), cycleInstructionSources(state), quote)
	if err != nil {
		candidate.P0PatchError = err.Error()
		return false
	}
	if edit == nil {
		return changed
	}
	candidate.OutputText = cleaned
	candidate.OpeningInstructionEdits = append(candidate.OpeningInstructionEdits, *edit)
	return true
}

func (s *Server) reviewCycleP0ContextOnce(ctx context.Context, state *anchorcycle.State, candidate *anchorcycle.Candidate) {
	if candidate.P0Report == nil || !contentguard.NeedsContextReview(*candidate.P0Report) {
		return
	}
	started := time.Now()
	failed := func(message string) {
		candidate.P0Report.Passed = false
		candidate.P0Report.Error = message
		candidate.P0Report.ContextReview = &contentguard.ContextReview{LexicalHits: append([]contentguard.Hit(nil), candidate.P0Report.Hits...), Error: message, LatencyMS: time.Since(started).Milliseconds()}
		candidate.P0ApprovedTextSHA = ""
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_P0_CONTEXT_PROMPT_PATH", "configs/prompts/anchor-style/compliance/L1/p0-context-review.md", "最终文字P0语境复核母版")
	if err != nil {
		failed("读取P0语境复核母版失败")
		return
	}
	messages, err := cycleP0ContextMessages(state, *candidate, mother)
	if err != nil {
		failed(err.Error())
		return
	}
	id := state.Config.TextProfileID
	if state.ReviewModel != nil {
		id = state.ReviewModel.ID
	}
	if id == "" {
		id = state.Config.FeedbackID
	}
	if id == "" {
		settings, e := s.store.AnchorGenerationSettings(ctx)
		if e != nil {
			failed("读取文字审核模型失败")
			return
		}
		id = settings.Cycle.FeedbackID
	}
	current, err := s.store.ResolveSpeechModel(ctx, id)
	if err != nil || current == nil || !current.Enabled || current.APIKey == "" {
		failed("文字审核模型不可用")
		return
	}
	profile := *current
	if state.ReviewModel != nil {
		profile = *state.ReviewModel
		profile.APIKey = current.APIKey
	} else if state.TextModel != nil {
		profile = *state.TextModel
		profile.APIKey = current.APIKey
	}
	reviewCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	reviewCtx = withExchangeScope(reviewCtx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID, CandidateID: candidate.ID})
	request := agentgateway.Request{Stage: "mainline_p0_context", Model: profile.Model, Messages: messages, MaxTokens: min(1800, profile.MaxTokens), ResponseFormat: agentgateway.ResponseJSON, EnableThinking: false, CacheSystemPrompt: true, Timeout: 15 * time.Second}
	exchange, err := s.beginModelExchange(reviewCtx, request.Stage, profile.ID, request)
	if err != nil {
		failed("P0语境复核请求记录失败")
		return
	}
	result, callErr := agentgateway.CompleteSpeechEndpoint(reviewCtx, profile, request)
	result.Provider, result.Model = "anchor:"+profile.ID, profile.Model
	callErr = s.finishModelExchange(reviewCtx, exchange, result, result.Text, started, result.InputTokens, result.OutputTokens, callErr)
	if callErr != nil {
		failed("P0语境复核未完成，本段跳过继续供料")
		return
	}
	review := contentguard.ContextReview{PromptSHA: derivedHash(mother), Provider: result.Provider, Model: result.Model, Raw: result.Text, LatencyMS: time.Since(started).Milliseconds()}
	if err = state.ApplyP0ContextContent(candidate, review); err != nil {
		failed("P0语境复核未完成：" + err.Error())
		candidate.P0Report.ContextReview.Raw = result.Text
		candidate.P0Report.ContextReview.PromptSHA = review.PromptSHA
		candidate.P0Report.ContextReview.Provider = review.Provider
		candidate.P0Report.ContextReview.Model = review.Model
	}
}
