package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

var errSupplementProductNotFound = errors.New("supplement product not found")

type supplementProductTarget struct {
	Link string `json:"link"`
	Name string `json:"product_name"`
}

type supplementFactPackage struct {
	Facts  string
	Target *supplementProductTarget
}

// Match persisted ownership and benefit bindings; display text is never an ID.
func selectSupplementProduct(tenant, plan, product int64, links []model.LiveAgentPlanProductLink, benefits []model.LiveAgentPlanBenefit, scopes map[int64]appdb.BenefitScope) ([]model.LiveAgentPlanProductLink, []model.LiveAgentPlanBenefit, error) {
	var selected *model.LiveAgentPlanProductLink
	for i := range links {
		p := &links[i]
		if p.ID == product && p.TenantID == tenant && p.PlanID == plan && p.Status == "active" {
			selected = p
			break
		}
	}
	if selected == nil {
		return nil, nil, errSupplementProductNotFound
	}
	applicable := []model.LiveAgentPlanBenefit{}
	for _, b := range benefits {
		if b.TenantID == tenant && b.PlanID == plan && contentpools.Contains(benefitScopeProducts(b, links, scopes), product) {
			applicable = append(applicable, b)
		}
	}
	chosen := []model.LiveAgentPlanProductLink{*selected}
	return chosen, applyContentBenefitScopes(applicable, chosen, scopes), nil
}

func (s *Server) loadSupplementFactPackage(ctx context.Context, tenant, plan, product int64, p0, p1, p2 string) (supplementFactPackage, error) {
	if product == 0 {
		source, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
		if err != nil {
			return supplementFactPackage{}, err
		}
		return supplementFactPackage{Facts: source.Facts}, nil
	}
	planData, err := s.store.GetLiveAgentPlan(ctx, tenant, plan)
	if err != nil {
		return supplementFactPackage{}, err
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		return supplementFactPackage{}, err
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(ctx, tenant, plan, time.Now().UTC())
	if err != nil {
		return supplementFactPackage{}, err
	}
	scopes, err := s.store.ListBenefitScopes(ctx, tenant, plan)
	if err != nil {
		return supplementFactPackage{}, err
	}
	links, benefits, err = selectSupplementProduct(tenant, plan, product, links, benefits, scopes)
	if err != nil {
		return supplementFactPackage{}, err
	}
	facts, err := s.store.ListLiveAgentPlanFacts(ctx, tenant, plan)
	if err != nil {
		return supplementFactPackage{}, err
	}
	text := renderAnchorStyleWorkbenchFacts(planData, facts, benefits, links)
	corrections, err := s.store.ListContentCorrections(ctx, tenant, plan)
	if err != nil {
		return supplementFactPackage{}, err
	}
	text += contentpools.RenderCorrections(contentpools.Sources{Corrections: corrections, Products: []contentpools.Product{{ID: product, Link: links[0].LinkKey}}})
	// Retain the same risk-rule parsing and fact boundaries as the whole-plan path.
	source, err := buildDerivedInput(text, "", p0, p1, p2)
	if err != nil {
		return supplementFactPackage{}, err
	}
	return supplementFactPackage{Facts: source.Facts, Target: &supplementProductTarget{Link: links[0].LinkKey, Name: links[0].ProductName}}, nil
}

func supplementScopeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSupplementProductNotFound) {
		writeError(w, http.StatusNotFound, "所选商品链接不存在、已停用或不属于当前方案")
		return
	}
	writeError(w, http.StatusInternalServerError, "读取正式事实包失败")
}
