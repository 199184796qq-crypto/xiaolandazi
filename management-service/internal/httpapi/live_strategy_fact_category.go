package httpapi

import (
	"errors"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
)

type liveStrategyNewFactCategory struct {
	Label       string `json:"label"`
	Family      string `json:"family,omitempty"`
	Description string `json:"description,omitempty"`
}

func loadLiveStrategyIntentPrompt() (string, string, error) {
	return loadReplaceablePromptAsset("LIVE_STRATEGY_INTENT_PROMPT_PATH", "configs/prompts/anchor-style/live-strategy-intent.md", "直播策略意图母版")
}

func liveStrategyFactUpdateCandidate(current model.LiveAgentPlanFact, p liveStrategyExecutePayload) model.LiveAgentPlanFactCandidate {
	value := strings.TrimSpace(p.FactValue)
	if value == "" && (p.ForbiddenWording != nil || p.SafeRewrite != nil) {
		value = current.Value
	}
	c := model.LiveAgentPlanFactCandidate{Category: current.Category, Key: current.Key, Value: value, ForbiddenWording: current.ForbiddenWording, SafeRewrite: current.SafeRewrite}
	if p.ForbiddenWording != nil {
		c.ForbiddenWording = strings.TrimSpace(*p.ForbiddenWording)
	}
	if p.SafeRewrite != nil {
		c.SafeRewrite = strings.TrimSpace(*p.SafeRewrite)
	}
	return c
}

func proposedFactCategory(catalog factCategoryCatalog, input *liveStrategyNewFactCategory) (model.LiveFactCategory, bool, error) {
	if input == nil {
		return model.LiveFactCategory{}, false, errors.New("请提供新分类名称")
	}
	for _, c := range catalog.Categories {
		if strings.EqualFold(c.Label, strings.TrimSpace(input.Label)) {
			if input.Family != "" && input.Family != c.Family {
				return c, false, errors.New("同名分类已有不同的所属类型，请使用原分类或新名称")
			}
			return c, false, nil
		}
	}
	c, err := model.NewLiveFactCategory(input.Label, input.Family, input.Description)
	return c, true, err
}

func (s *Server) executeLiveStrategyAddFactCategory(r *http.Request, actor model.Actor, tenant int64, p liveStrategyExecutePayload) systemAgentChatOutput {
	if _, err := s.requireBoundLiveStrategyPlan(r, tenant, p.RoomID, p.PlanID); err != nil {
		return liveStrategyFailed("plan_scope_changed", "当前方案已经不再绑定这个直播间。", nil)
	}
	catalog, _, err := s.tenantFactCategoryCatalog(r.Context(), tenant)
	if err != nil {
		return liveStrategyFailed("category_lookup_failed", "读取事实分类失败。", nil)
	}
	category, isNew, err := proposedFactCategory(catalog, p.NewFactCategory)
	if err != nil {
		return liveStrategyFailed("invalid_category", err.Error(), nil)
	}
	if isNew {
		if err = s.store.CreateLiveFactCategory(r.Context(), tenant, actor.UserID, category); err != nil {
			return liveStrategyFailed("category_add_failed", "分类没有保存，请刷新后重试。", nil)
		}
	}
	return liveStrategySucceeded("fact_category_added", "分类“"+category.Label+"”已可选，仅适用于当前账号。", map[string]any{"module": "knowledge", "plan_id": p.PlanID, "category": category, "category_changed": isNew})
}
