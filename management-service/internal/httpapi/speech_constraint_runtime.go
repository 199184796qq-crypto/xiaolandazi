package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
)

func (s *Server) installCurrentExpressionConstraints(ctx context.Context, state *anchorcycle.State) error {
	if state.TenantID <= 0 || state.PlanID <= 0 || s.store == nil {
		return nil
	}
	source, e := s.store.CurrentSpeechExpressionConstraints(ctx, state.TenantID, state.PlanID)
	if e != nil {
		return e
	}
	ids := []int64{}
	links := state.HookProducts()
	for _, p := range source.Products {
		for _, link := range links {
			if link == p.Link || link == "current" {
				ids = append(ids, p.ID)
				break
			}
		}
	}
	state.LatestExpressionConstraints = contentpools.CorrectionView(source, ids)
	return nil
}

func (s *Server) auditSpeechExpressionConstraints(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
	if len(state.LatestExpressionConstraints) == 0 {
		return
	}
	text := cycleVoiceText(*c)
	audit := &anchorcycle.LectureAudit{TextSHA: anchorcycle.ProductDigest(text), ScopeSHA: anchorcycle.ProductDigest(state.LatestExpressionConstraints)}
	c.ExpressionAudit = audit
	if err := requireCycleP0(state); err != nil {
		audit.Error = "有效P0读取未完成"
		return
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_SPEECH_CONSTRAINT_TEXT_REVIEW_PATH", "configs/prompts/anchor-style/speech-constraint-text-review.md", "用户约束文字审核母版")
	if err != nil {
		audit.Error = "表达约束审核母版不可用"
		return
	}
	packet := map[string]any{"text": text, "adopted_constraints": state.LatestExpressionConstraints, "effective_p0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	out, err := boundedExpressionConstraintAudit(text, func(previous string) (string, error) {
		if previous != "" {
			packet["previous_review_format_error"] = previous
		}
		raw, _ := json.Marshal(packet)
		res, _, e := s.completeAnchorStyleWorkbench(ctx, state.Config.FeedbackID, "speech_constraint_text_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, 900, false)
		return res.Text, e
	})
	if err != nil {
		audit.Error = "表达约束审核未完成"
		return
	}
	audit.Passed, audit.Quote, audit.Reason = out.Passed, out.Quote, out.Reason
}

func boundedExpressionConstraintAudit(text string, call func(string) (string, error)) (anchorcycle.LectureAudit, error) {
	previous := ""
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := call(previous)
		if err != nil {
			return anchorcycle.LectureAudit{}, err
		}
		out, err := agentgateway.ParseExpressionReview(raw, text)
		if err == nil {
			return anchorcycle.LectureAudit{Passed: *out.Passed, Quote: out.Quote, Reason: out.Reason}, nil
		}
		if !errors.Is(err, agentgateway.ErrExpressionReviewQuote) || attempt == 1 {
			return anchorcycle.LectureAudit{}, err
		}
		previous = raw
	}
	return anchorcycle.LectureAudit{}, errors.New("表达约束审核未完成")
}
