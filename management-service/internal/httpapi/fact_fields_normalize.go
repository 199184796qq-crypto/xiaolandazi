package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type normalizedFactFields struct {
	Fact       *string `json:"fact_value"`
	MustSay    *string `json:"must_say"`
	CanSay     *string `json:"can_say"`
	Rewrite    *string `json:"rewrite"`
	NotMention *string `json:"not_mention"`
	Question   *string `json:"question"`
}

// fourLayerSection 把模型返回的四层结构组装成存储字段：
// 正向两层（必说/可说）进 safe_rewrite，负向两层（改写/不提）进 forbidden_wording，
// 每层带【标题】对齐四层结构，前端展开详情按标题分块展示。
func fourLayerSection(parts ...[2]string) string {
	sections := make([]string, 0, len(parts))
	for _, p := range parts {
		if content := strings.TrimSpace(p[1]); content != "" {
			sections = append(sections, "【"+p[0]+"】"+content)
		}
	}
	return strings.Join(sections, "\n")
}

func parseNormalizedFactFields(raw string, original model.LiveAgentPlanFactCandidate) (model.LiveAgentPlanFactCandidate, error) {
	var fields normalizedFactFields
	if err := decodeStrictJSON(raw, &fields); err != nil || fields.Fact == nil || fields.MustSay == nil || fields.CanSay == nil || fields.Rewrite == nil || fields.NotMention == nil || fields.Question == nil {
		return original, errors.New("四层整理结果不完整，未保存，请重试")
	}
	if question := strings.TrimSpace(*fields.Question); question != "" {
		return original, fmt.Errorf("请补充或修改后保存：%s", question)
	}
	next := original // The program retains names, categories, provenance and relations.
	next.Value = strings.TrimSpace(*fields.Fact)
	next.SafeRewrite = fourLayerSection([2]string{"必说", *fields.MustSay}, [2]string{"可说", *fields.CanSay})
	next.ForbiddenWording = fourLayerSection([2]string{"改写", *fields.Rewrite}, [2]string{"不提", *fields.NotMention})
	if next.Value == "" || utf8.RuneCountInString(next.Value) > 4000 || utf8.RuneCountInString(next.ForbiddenWording) > 4000 || utf8.RuneCountInString(next.SafeRewrite) > 4000 {
		return original, errors.New("整理后的事实为空或超过字段长度，未保存")
	}
	return next, nil
}

func factFieldsContent(candidate model.LiveAgentPlanFactCandidate) map[string]string {
	return map[string]string{"事实名称": candidate.Key, "fact_value": candidate.Value, "forbidden_wording": candidate.ForbiddenWording, "safe_rewrite": candidate.SafeRewrite}
}

func normalizationReviewPassed(raw string) error {
	var review struct {
		Passed *bool  `json:"passed"`
		Reason string `json:"reason"`
	}
	if decodeStrictJSON(raw, &review) != nil || review.Passed == nil {
		return errors.New("四层整理复核未完成，未保存，请重试")
	}
	if !*review.Passed {
		if strings.TrimSpace(review.Reason) == "" {
			return errors.New("四层整理未通过复核，未保存")
		}
		return fmt.Errorf("四层整理未保存：%s", review.Reason)
	}
	return nil
}

// Pure model transformation and independent fidelity/role/P0 review. The paid
// probe uses this method without writing facts, revisions or analysis assets.
func (s *Server) normalizePlanFactFields(ctx context.Context, tenant, plan int64, candidate model.LiveAgentPlanFactCandidate) (model.LiveAgentPlanFactCandidate, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	state := anchorcycle.State{TenantID: tenant, PlanID: plan}
	if err := installP0Mother(&state); err != nil {
		return candidate, "", err
	}
	if err := s.installPlanP0Allowances(ctx, &state); err != nil {
		return candidate, "", errors.New("读取已确认规则失败，未保存")
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_FIELDS_NORMALIZE_PROMPT_PATH", "configs/prompts/anchor-style/fact-fields-normalize.md", "事实三栏规范母版")
	if err != nil {
		return candidate, "", err
	}
	reviewMother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_FIELDS_NORMALIZE_REVIEW_PATH", "configs/prompts/anchor-style/fact-fields-normalize-review.md", "事实三栏复核母版")
	if err != nil {
		return candidate, "", err
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return candidate, "", errors.New("读取事实整理模型失败，未保存")
	}
	input := candidate
	input.Value, input.ForbiddenWording, input.SafeRewrite = html.UnescapeString(input.Value), html.UnescapeString(input.ForbiddenWording), html.UnescapeString(input.SafeRewrite)
	allowances := []map[string]string{}
	for _, allowance := range state.P0Allowances {
		allowances = append(allowances, map[string]string{"已确认的相关表述": allowance.MatchedText, "确认判断": allowance.Decision})
	}
	packet := map[string]any{"original": factFieldsContent(input), "effective_p0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope), "已确认规则判断（仅在原有事实范围内适用）": allowances}
	for attempt := 0; attempt < 2; attempt++ {
		raw, _ := json.Marshal(packet)
		result, _, callErr := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "fact_fields_normalize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, 6500, false)
		if callErr != nil {
			return candidate, "", errors.New("事实整理模型暂时不可用，原输入未保存，请重试")
		}
		next, parseErr := parseNormalizedFactFields(result.Text, candidate)
		if parseErr != nil {
			return candidate, "", parseErr
		}
		packet["normalized"] = factFieldsContent(next)
		raw, _ = json.Marshal(packet)
		review, _, callErr := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "fact_fields_normalize_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: reviewMother}, {Role: "user", Content: string(raw)}}, 1300, false)
		if callErr != nil {
			return candidate, "", errors.New("四层整理复核暂时不可用，未保存，请重试")
		}
		err = normalizationReviewPassed(review.Text)
		if err == nil {
			return next, derivedHash(mother + reviewMother + state.P0PolicyMD), nil
		}
		if attempt == 1 {
			return candidate, "", err
		}
		packet["previous_normalization"], packet["previous_review_rejection"] = factFieldsContent(next), review.Text
		delete(packet, "normalized")
	}
	return candidate, "", err
}

// Archive the original request BEFORE a fact write. A failed or conflicted
// write leaves an attempted normalization record, never a falsely saved fact.
// The record is stable plan data; source_ref links successful fact revisions.
func (s *Server) normalizeFactForSave(ctx context.Context, tenant, plan, actor, fact, version int64, original model.LiveAgentPlanFactCandidate, originalSource string) (model.LiveAgentPlanFactCandidate, string, error) {
	next, policySHA, err := s.normalizePlanFactFields(ctx, tenant, plan, original)
	if err != nil {
		return original, originalSource, err
	}
	id := anchorcycle.ID()
	archive, _ := json.Marshal(map[string]any{"normalization_id": id, "original": original, "normalized": next, "requested_fact_id": fact, "expected_version_no": version, "original_source_ref": originalSource, "actor_user_id": actor, "policy_sha256": policySHA, "operation": "normalize_before_save", "created_at": time.Now().UTC().Format(time.RFC3339Nano)})
	if err = s.store.SavePlanAnalysisAsset(ctx, tenant, plan, "fact_normalization", derivedHash(id), string(archive)); err != nil {
		return original, originalSource, errors.New("保存原始输入记录失败，本次事实未保存")
	}
	prefix := "fact-normalization:"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(originalSource)), "manual:") {
		prefix = "manual:fact-normalization:"
	}
	return next, prefix + id, nil
}

// normalizeAllPlanFacts 保存方案前，用审核模型复核方案全部事实的四层表达，
// 把"容易踩坑的"（禁词原词污染、自然语言禁说）翻译成标准表达并就地更新。
// 单条归一化失败或结果未变化时跳过，不阻断保存方案。
func (s *Server) normalizeAllPlanFacts(ctx context.Context, tenant, plan, actor int64) {
	items, err := s.store.ListLiveAgentPlanFacts(ctx, tenant, plan)
	if err != nil {
		log.Printf("batch normalize list facts tenant=%d plan=%d: %v", tenant, plan, err)
		return
	}
	changed := 0
	for _, item := range items {
		candidate := model.LiveAgentPlanFactCandidate{
			Category:         item.Category,
			Key:              item.Key,
			Value:            item.Value,
			ForbiddenWording: item.ForbiddenWording,
			SafeRewrite:      item.SafeRewrite,
		}
		normalized, _, normalizeErr := s.normalizeFactForSave(ctx, tenant, plan, actor, item.ID, item.VersionNo, candidate, "manual:save-plan-normalize")
		if normalizeErr != nil {
			continue
		}
		if normalized.Value == item.Value && normalized.ForbiddenWording == item.ForbiddenWording && normalized.SafeRewrite == item.SafeRewrite {
			continue
		}
		if _, err := s.store.UpdateLiveAgentPlanFact(ctx, tenant, plan, item.ID, actor, normalized.Category, normalized.Key, normalized.Value, normalized.ForbiddenWording, normalized.SafeRewrite); err != nil {
			log.Printf("batch normalize update fact tenant=%d plan=%d fact=%d: %v", tenant, plan, item.ID, err)
			continue
		}
		changed++
	}
	if changed > 0 {
		log.Printf("batch normalize facts tenant=%d plan=%d changed=%d", tenant, plan, changed)
	}
}
