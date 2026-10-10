package decisionexecutor

import (
	"context"
	"errors"
	"fmt"
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
			if err != nil {
				return "", err
			}
			return w.interactionFactsGuidance(ctx, session, planID, facts, item, skipStrategy)
		}
	}
	source, ok := w.store.(interactionFactSnapshotStore)
	if !ok {
		return "", nil
	}
	published, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", fmt.Errorf("读取本场正式事实版本失败: %w", err)
	}
	if published.PlanID != planID {
		return "", fmt.Errorf("本场方案已改变，不能混用商品事实")
	}
	ref, hasSnapshot := published.GenerationContext["derived_safety"].(map[string]any)
	if !hasSnapshot {
		return "", nil
	}
	hash, _ := ref["source_sha256"].(string)
	if hash == "" {
		return "", fmt.Errorf("本场正式事实指纹缺失")
	}
	asset, err := source.GetLiveDerivedSafety(ctx, session.TenantID, planID, hash)
	if err != nil || strings.TrimSpace(asset.SafeFacts) == "" {
		return "", fmt.Errorf("本场审核事实不可用，停止生成商品回答")
	}
	return w.interactionFactsGuidance(ctx, session, planID, asset.SafeFacts, item, skipStrategy)
}

func (w *Worker) interactionFactsGuidance(ctx context.Context, session model.LiveRuntimeSession, planID int64, facts string, item *decisionItem, skipStrategy bool) (string, error) {
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
		strategyRaw, strategyErr := strategyStore.GetLatestPlanContentStrategy(ctx, session.TenantID, planID, contentstrategy.SHA(facts), session.StartedAt)
		if errors.Is(strategyErr, appdb.ErrPlanAnalysisAssetNotFound) {
			return result, nil
		}
		if strategyErr == nil {
			var snapshot *contentstrategy.Snapshot
			snapshot, strategyErr = contentstrategy.Parse(strategyRaw, facts)
			if strategyErr == nil {
				var guidance string
				if item != nil && item.PurposeID != "" {
					recent := []string{}
					for _, played := range w.playedInteractionContent(ctx, session) {
						recent = append(recent, played.ContentAngle)
					}
					active := contentstrategy.ActiveBenefitFacts(snapshot.Facts, time.Now().UTC())
					var selected contentstrategy.Selection
					guidance, selected, strategyErr = snapshot.RenderSelection(contentstrategy.SelectionRequest{Mode: "互动", AllowedFacts: facts, Question: primaryQuestion(*item), PurposeID: item.PurposeID, Recent: recent, ActiveBenefit: len(active) > 0, ActiveFactIDs: active})
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
