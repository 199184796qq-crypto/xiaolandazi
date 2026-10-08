package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func parseFactRiskSemanticReview(raw string, pending map[string]bool) ([]model.LiveRiskSemanticDecision, error) {
	var output struct {
		Items []model.LiveRiskSemanticDecision `json:"items"`
	}
	decoder := json.NewDecoder(strings.NewReader(stripPolicyJSONFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("语义复核返回包含额外内容")
	}
	if len(output.Items) != len(pending) {
		return nil, errors.New("语义复核没有逐项返回全部风险候选")
	}
	seen := map[string]bool{}
	for i := range output.Items {
		item := &output.Items[i]
		item.ClaimSHA256 = strings.TrimSpace(item.ClaimSHA256)
		item.Decision = strings.TrimSpace(item.Decision)
		item.Reason = strings.TrimSpace(item.Reason)
		item.Suggestion = strings.TrimSpace(item.Suggestion)
		if !pending[item.ClaimSHA256] || seen[item.ClaimSHA256] {
			return nil, errors.New("语义复核返回未知或重复的风险标识")
		}
		if item.Decision != "pass" && item.Decision != "evidence_required" && item.Decision != "block" {
			return nil, errors.New("语义复核决定无效")
		}
		if item.Reason == "" || utf8.RuneCountInString(item.Reason) > 600 || utf8.RuneCountInString(item.Suggestion) > 4000 || strings.ContainsAny(item.Suggestion, "\r\n") {
			return nil, errors.New("语义复核理由或修改建议格式无效")
		}
		seen[item.ClaimSHA256] = true
	}
	return output.Items, nil
}

// These are sequence/time references, not comparative advertising claims.
// The semantic model still reviews the complete, unmodified original text.
var firstSequenceReference = regexp.MustCompile(`第一(?:次|步|天|晚|夜|周|个月|月|年|批|件|袋|瓶|盒|单|顿|口|轮|阶段|时间)`)

func requiresVerbatimGeneratedFact(text string) bool {
	for _, term := range []string{"唯一", "独家", "最便宜", "最低价", "最优惠", "最划算", "最先进", "最受欢迎", "最流行", "顶级", "极致", "世界领先", "遥遥领先", "绝无仅有", "独一无二", "100%", "百分百", "零风险"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	return strings.Contains(firstSequenceReference.ReplaceAllString(text, ""), "第一")
}

func enforceGeneratedWheelSemanticBoundaries(decisions []model.LiveRiskSemanticDecision, reviews map[string]model.LiveRiskReview, factPackage string) []model.LiveRiskSemanticDecision {
	facts := model.NormalizeLiveRiskReviewText(factPackage)
	for i := range decisions {
		decision := &decisions[i]
		review := reviews[decision.ClaimSHA256]
		if review.RuleID != "l1.p0.absolute_claim" || decision.Decision == "block" {
			continue
		}
		generatedWheelClaim := false
		supportedVerbatim := false
		for _, source := range review.Sources {
			if !strings.HasPrefix(source.SourceKey, "wheel/") {
				continue
			}
			claim := model.NormalizeLiveRiskReviewText(source.Text)
			if !requiresVerbatimGeneratedFact(claim) {
				continue
			}
			generatedWheelClaim = true
			if claim != "" && strings.Contains(facts, claim) {
				supportedVerbatim = true
			}
		}
		if generatedWheelClaim && !supportedVerbatim {
			decision.Decision = "block"
			decision.Reason = model.GeneratedWheelAbsoluteBoundaryReason
			decision.Suggestion = ""
		}
	}
	return decisions
}

func (s *Server) reviewPendingFactRisks(ctx context.Context, tenant, plan int64) (int, error) {
	prompt, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_FACT_RISK_SEMANTIC_PROMPT_PATH", "configs/prompts/anchor-style/compliance/fact-risk-semantic-review.md", "事实风险语义复核母版")
	if err != nil {
		return 0, err
	}
	promptSHA := derivedHash(prompt)
	items, err := s.store.ListLiveRiskReviews(ctx, tenant, plan)
	if err != nil {
		return 0, err
	}
	pending := map[string]bool{}
	reviews := map[string]model.LiveRiskReview{}
	requests := []map[string]any{}
	for _, item := range items {
		if item.Severity != "P0" || item.SemanticDecision != "pending" {
			continue
		}
		pending[item.ClaimSHA256] = true
		reviews[item.ClaimSHA256] = item
		requests = append(requests, map[string]any{
			"claim_sha256": item.ClaimSHA256, "text": item.Text, "rule_id": item.RuleID,
			"program_reason": item.Reason, "subject": item.Subject, "sources": item.Sources,
		})
	}
	if len(requests) == 0 {
		return 0, nil
	}
	if len(requests) > 40 {
		return 0, errors.New("待语义复核项目过多，请先修改或分批处理原始内容")
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return 0, err
	}
	source, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		return 0, errors.New("读取事实包失败：" + err.Error())
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return 0, errors.New("读取语义复核模型配置失败")
	}
	profile := strings.TrimSpace(settings.SafetyProfileID)
	if profile == "" {
		profile = strings.TrimSpace(settings.SellingPointProfileID)
	}
	if profile == "" {
		return 0, errors.New("后台尚未配置风险语义复核模型")
	}
	payload, _ := json.Marshal(map[string]any{"plan_id": plan, "fact_package": source.Facts, "items": requests})
	modelCtx := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	result, _, err := s.completeAnchorStyleWorkbench(modelCtx, profile, "fact_risk_semantic_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(payload)}}, 3200, false)
	if err != nil {
		return 0, errors.New("风险语义复核暂时失败：" + err.Error())
	}
	decisions, err := parseFactRiskSemanticReview(result.Text, pending)
	if err != nil {
		return 0, errors.New("风险语义复核结果格式错误：" + err.Error())
	}
	decisions = enforceGeneratedWheelSemanticBoundaries(decisions, reviews, source.Facts)
	if err = s.store.SaveLiveRiskSemanticDecisions(ctx, tenant, plan, promptSHA, result.Provider, result.Model, decisions); err != nil {
		return 0, err
	}
	return len(decisions), nil
}

func (s *Server) liveFactRiskSemanticReview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	if err := s.refreshPlanFactRiskRegistry(r.Context(), tenant, plan); err != nil {
		writeError(w, http.StatusServiceUnavailable, "刷新事实风险登记失败："+err.Error())
		return
	}
	reviewed, err := s.reviewPendingFactRisks(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	items, err := s.store.ListLiveRiskReviews(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 503, "读取复核结果失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "reviewed": reviewed})
}
