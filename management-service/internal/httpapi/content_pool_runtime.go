package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/model"
)

func contentPoolRuntime(set contentpools.Set, policy anchorcycle.ProductPolicy) ([]anchorcycle.Task, *anchorcycle.ProductFocus, error) {
	if err := contentpools.ValidateSet(set); err != nil {
		return nil, nil, err
	}
	queue := []anchorcycle.Task{}
	focus := &anchorcycle.ProductFocus{Policy: policy, FactsSHA: derivedHash(set.SafeFacts), Products: []anchorcycle.FocusProduct{}, Facts: []anchorcycle.ScopedFact{}, Tasks: map[string]anchorcycle.ScopedTask{}}
	links := map[int64]string{}
	identities := map[int64]anchorcycle.HookFact{}
	for _, p := range set.Sources.Products {
		links[p.ID] = p.Link
		if strings.TrimSpace(p.Name) != "" {
			identities[p.ID] = anchorcycle.HookFact{ID: fmt.Sprintf("products:%d:identity", p.ID), Data: "商品名称=" + p.Name, Products: []string{p.Link}, Benefits: []string{}}
		}
		focus.Products = append(focus.Products, anchorcycle.FocusProduct{LinkKey: p.Link, Name: p.Name, Roles: p.Roles})
	}
	for _, p := range set.Pools {
		scopeLinks := []string{}
		for _, id := range p.Products {
			scopeLinks = append(scopeLinks, links[id])
		}
		shared := len(p.Products) == len(set.Sources.Products)
		boundFacts := map[string]anchorcycle.HookFact{}
		for _, f := range p.Facts {
			factLinks := []string{}
			for _, id := range f.Products {
				factLinks = append(factLinks, links[id])
			}
			benefits := []string{}
			for _, source := range f.Sources {
				if strings.HasPrefix(source, "benefits:") {
					benefits = append(benefits, source)
				}
			}
			focus.Facts = append(focus.Facts, anchorcycle.ScopedFact{ID: f.ID, BenefitIDs: benefits, Text: f.Text, Links: factLinks, Shared: len(f.Products) == len(set.Sources.Products), Applicable: true})
			boundFacts[f.ID] = anchorcycle.HookFact{ID: f.ID, Data: f.Text, Products: factLinks, Benefits: benefits}
		}
		for _, t := range p.Tasks {
			if t.Disabled {
				continue
			}
			scope := &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{}}
			scope.Corrections = contentpools.CorrectionView(set.Sources, p.Products)
			// Identifying the item is necessary even for a narrowly scoped service
			// or storage task. This is a name only, never the entire product card.
			for _, productID := range p.Products {
				if identity, ok := identities[productID]; ok {
					scope.Facts = append(scope.Facts, identity)
				}
			}
			for _, id := range t.Sources {
				scope.Facts = append(scope.Facts, boundFacts[id])
			}
			allowedLinks, _ := json.Marshal(scopeLinks)
			queue = append(queue, anchorcycle.Task{FactScope: scope, AllowedLinksJSON: string(allowedLinks), ID: t.ID, Key: t.ID, SellingPoint: t.Point, Angle: t.Angle, TargetAudience: t.TargetAudience, PainPoint: t.PainPoint})
			focus.Tasks[t.ID] = anchorcycle.ScopedTask{Links: scopeLinks, Shared: shared}
		}
	}
	if len(set.Sources.Products) < 2 {
		return queue, nil, nil
	}
	if err := focus.Policy.Validate(); err != nil {
		return nil, nil, err
	}
	if err := focus.ValidateProducts(); err != nil {
		return nil, nil, err
	}
	return queue, focus, nil
}

// An old voice-only version can point to a new derived wheel while retaining
// its saved original wheel and scope. Restore the wheel selected in THAT
// version, using only tasks with a matching saved assignment. No model call,
// mutable wheel lookup, new assignment or published-version edit occurs here.
func savedLegacyQueue(wheel anchorStyleSellingPointWheel, focus *anchorcycle.ProductFocus) []anchorcycle.Task {
	queue := []anchorcycle.Task{}
	for _, t := range flattenAnchorStyleWheelTasks(wheel) {
		if focus != nil {
			if _, ok := focus.Tasks[t.Key]; !ok {
				continue
			}
		}
		queue = append(queue, anchorcycle.Task{Key: t.Key, SellingPoint: t.SellingPoint, Angle: t.Angle, TargetAudience: t.TargetAudience, PainPoint: t.PainPoint})
	}
	return queue
}

func (s *Server) captureContentPoolSnapshot(ctx context.Context, tenant, plan int64, input *model.CreateLiveAgentPlanVersionInput) (contentpools.Set, error) {
	set, err := s.readyContentPoolSet(ctx, tenant, plan)
	if err != nil {
		return set, err
	}
	input.GenerationContext["content_pools"] = contentPoolRef(set)
	input.GenerationContext["derived_safety"] = map[string]any{"asset_id": set.SafetyID, "source_sha256": set.SafetyHash, "notice": "商品专属池与公共池已对应保存，本场继续使用原版本，下次开播使用本次保存内容。"}
	return set, nil
}
func (s *Server) captureContentPoolScheme(ctx context.Context, tenant, plan int64, input *model.CreateLiveAgentPlanVersionInput, set contentpools.Set) error {
	if !isRealtimeScheme(input.GenerationContext) {
		return nil
	}
	scheme, err := realtimeScheme(input.GenerationContext)
	if err != nil {
		return err
	}
	policy, source, err := loadCycleProductPolicy()
	if err != nil {
		return err
	}
	if _, _, err = contentPoolRuntime(set, policy); err != nil {
		return err
	}
	scheme.ProductFocus = nil
	scheme.PoolPolicy = &policy
	scheme.PoolPolicySource = source
	input.GenerationContext["anchor_generation"] = scheme
	return nil
}
func (s *Server) savedContentPoolSet(ctx context.Context, tenant, plan int64, data map[string]any) (*contentpools.Set, error) {
	id := contentPoolSetID(data)
	if id == 0 {
		return nil, nil
	}
	set, err := s.store.GetContentPoolSet(ctx, tenant, plan, id)
	if err != nil {
		return nil, errors.New("已保存的内容池版本无法读取")
	}
	ref := asSupportMap(data["content_pools"])
	hash, _ := ref["source_sha"].(string)
	if hash != set.SourceSHA || strings.TrimSpace(hash) == "" {
		return nil, errors.New("已保存的内容池引用不匹配")
	}
	if err = contentpools.ValidateSet(set); err != nil {
		return nil, err
	}
	return &set, nil
}
