package decisionexecutor

import (
	"context"
	"errors"
	"fmt"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"log"
	"strings"
	"time"
)

type interactionFactSnapshotStore interface {
	GetLiveDerivedSafety(context.Context, int64, int64, string) (model.LiveDerivedSafetyAsset, error)
}

type contentStrategyStore interface {
	GetLatestPlanContentStrategy(context.Context, int64, int64, string, time.Time) (string, error)
}

type currentInteractionFactsStore interface {
	CurrentInteractionFacts(context.Context, int64, int64, int64) (string, error)
}

type currentInteractionScopeStore interface {
	CurrentInteractionScopeVersion(context.Context, int64, int64, int64, string) (model.LiveAgentPlanVersion, error)
}
type expressionConstraintStore interface {
	CurrentSpeechExpressionConstraints(context.Context, int64, int64) (contentpools.Sources, error)
}

func (w *Worker) interactionFactSnapshot(ctx context.Context, session model.LiveRuntimeSession, planID int64, options ...any) (string, error) {
	var item *decisionItem
	skipStrategy := false
	for _, option := range options {
		switch value := option.(type) {
		case *decisionItem:
			item = value
		case bool:
			skipStrategy = value
		}
	}
	if item != nil && item.BusinessCoreOnly {
		if source, ok := w.store.(currentInteractionFactsStore); ok {
			facts, err := source.CurrentInteractionFacts(ctx, session.TenantID, session.RoomID, planID)
			if item != nil {
				item.FullFacts = facts
			}
			if err != nil {
				return "", err
			}
			scope := interactionFactScope{FullFacts: facts}
			if source, ok := w.store.(currentInteractionScopeStore); ok && isViewerQuestionAnswer(*item) && !skipStrategy {
				version, e := source.CurrentInteractionScopeVersion(ctx, session.TenantID, session.RoomID, planID, facts)
				if e != nil {
					return "", e
				}
				if version.PlanID != planID {
					return "", fmt.Errorf("互动测试不能混用另一方案的商品范围")
				}
				facts, scope = narrowInteractionFacts(version.GenerationContext, facts, strings.Join(item.SampleQuestions, "\n"))
			}
			return w.interactionFactsGuidance(ctx, session, planID, facts, item, skipStrategy, scope)
		}
	}
	published, facts, err := w.interactionApprovedFacts(ctx, session, planID)
	if item != nil {
		item.FullFacts = facts
	}
	if err != nil || facts == "" {
		return "", err
	}
	scope := interactionFactScope{FullFacts: facts}
	if item != nil && isViewerQuestionAnswer(*item) && !skipStrategy {
		facts, scope = narrowInteractionFacts(published.GenerationContext, facts, strings.Join(item.SampleQuestions, "\n"))
	}
	return w.interactionFactsGuidance(ctx, session, planID, facts, item, skipStrategy, scope)
}

// Use the immutable approved package for both answer generation and fact checks.
// An absent reference is legacy data; an invalid reference must not fall back.
func (w *Worker) interactionApprovedFacts(ctx context.Context, session model.LiveRuntimeSession, planID int64) (model.LiveAgentPlanVersion, string, error) {
	source, ok := w.store.(interactionFactSnapshotStore)
	if !ok {
		return model.LiveAgentPlanVersion{}, "", nil
	}
	published, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return published, "", fmt.Errorf("读取本场正式事实版本失败: %w", err)
	}
	if published.PlanID != planID {
		return published, "", fmt.Errorf("本场方案已改变，不能混用商品事实")
	}
	value, hasSnapshot := published.GenerationContext["derived_safety"]
	if !hasSnapshot {
		return published, "", nil
	}
	ref, ok := value.(map[string]any)
	if !ok {
		return published, "", fmt.Errorf("本场正式事实引用无效")
	}
	hash, _ := ref["source_sha256"].(string)
	if strings.TrimSpace(hash) == "" {
		return published, "", fmt.Errorf("本场正式事实指纹缺失")
	}
	asset, err := source.GetLiveDerivedSafety(ctx, session.TenantID, planID, hash)
	if err != nil || strings.TrimSpace(asset.SafeFacts) == "" {
		return published, "", fmt.Errorf("本场审核事实不可用，停止生成商品回答")
	}
	return published, asset.SafeFacts, nil
}

func (w *Worker) interactionFactsGuidance(ctx context.Context, session model.LiveRuntimeSession, planID int64, facts string, item *decisionItem, skipStrategy bool, scopes ...interactionFactScope) (string, error) {
	fullFacts := facts
	var scopeLinks []string
	if len(scopes) > 0 {
		fullFacts, scopeLinks = scopes[0].FullFacts, scopes[0].Links
	}
	if source, ok := w.store.(expressionConstraintStore); ok {
		constraints, err := source.CurrentSpeechExpressionConstraints(ctx, session.TenantID, planID)
		if err != nil {
			return "", fmt.Errorf("读取用户采用的表达约束失败：%w", err)
		}
		fullFacts += contentpools.RenderCorrections(constraints)
		if len(scopeLinks) > 0 {
			selected := []contentpools.Product{}
			for _, product := range constraints.Products {
				for _, link := range scopeLinks {
					if product.Link == link || (interactionProductNumber(link) != "" && interactionProductNumber(product.Link) == interactionProductNumber(link)) {
						selected = append(selected, product)
						break
					}
				}
			}
			constraints.Products = selected
		}
		facts += contentpools.RenderCorrections(constraints)
	}
	raw, err := interactionpolicy.Read("interaction-facts-boundary.md")
	if err != nil {
		return "", err
	}
	result := "\n" + strings.ReplaceAll(string(raw), "{{FACTS}}", facts)
	if skipStrategy {
		return result, nil
	}
	if strategyStore, ok := w.store.(contentStrategyStore); ok {
		if control, err := strategyStore.GetLatestPlanContentStrategy(ctx, session.TenantID, planID, "", session.StartedAt); err == nil && contentstrategy.Disabled(control) {
			return result, nil
		}
		strategyRaw, strategyErr := strategyStore.GetLatestPlanContentStrategy(ctx, session.TenantID, planID, contentstrategy.SHA(fullFacts), session.StartedAt)
		if errors.Is(strategyErr, appdb.ErrPlanAnalysisAssetNotFound) {
			return result, nil
		}
		if strategyErr == nil {
			var snapshot *contentstrategy.Snapshot
			snapshot, strategyErr = contentstrategy.Parse(strategyRaw, fullFacts)
			if strategyErr == nil {
				var guidance string
				if item != nil && item.PurposeID != "" {
					recent := []string{}
					for _, played := range w.playedInteractionContent(ctx, session) {
						recent = append(recent, played.ContentAngle)
					}
					active := contentstrategy.ActiveBenefitFacts(snapshot.Facts, time.Now().UTC())
					var selected contentstrategy.Selection
					guidance, selected, strategyErr = snapshot.RenderSelection(contentstrategy.SelectionRequest{Mode: "互动", AllowedFacts: facts, Question: primaryQuestion(*item), PurposeID: item.PurposeID, Recent: recent, ActiveBenefit: len(active) > 0, ActiveFactIDs: active, IntentAngles: item.IntentAngles})
					if guidance != "" {
						item.SelectedContentPlan = &selected
					}
				} else {
					guidance, strategyErr = snapshot.Render("互动", facts)
				}
				if strategyErr == nil {
					return result + "\n\n" + guidance, nil
				}
			}
		}
		log.Printf("interaction content strategy fallback tenant=%d room=%d session=%d: %v", session.TenantID, session.RoomID, session.ID, strategyErr)
	}
	return result, nil
}
