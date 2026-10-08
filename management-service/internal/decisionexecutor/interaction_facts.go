package decisionexecutor

import (
	"context"
	"fmt"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"strings"
)

type interactionFactSnapshotStore interface {
	GetLiveDerivedSafety(context.Context, int64, int64, string) (model.LiveDerivedSafetyAsset, error)
}

func (w *Worker) interactionFactSnapshot(ctx context.Context, session model.LiveRuntimeSession, planID int64) (string, error) {
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
	raw, err := interactionpolicy.Read("interaction-facts-boundary.md")
	if err != nil {
		return "", err
	}
	return "\n" + strings.ReplaceAll(string(raw), "{{FACTS}}", asset.SafeFacts), nil
}
