package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/model"
)

type interactionProductContextStore interface {
	CurrentInteractionProductContext(context.Context, int64, int64, int64) (string, error)
}
type interactionProductPoolStore interface {
	GetContentPoolSet(context.Context, int64, int64, int64) (contentpools.Set, error)
}

func (w *Worker) interactionProductContext(ctx context.Context, s model.LiveRuntimeSession, plan int64, current bool) (string, error) {
	if current {
		if source, ok := w.store.(interactionProductContextStore); ok {
			return source.CurrentInteractionProductContext(ctx, s.TenantID, s.RoomID, plan)
		}
	}
	source, ok := w.store.(interactionProductPoolStore)
	if !ok {
		return "", nil
	}
	v, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, s.TenantID, s.RoomID)
	if err != nil {
		return "", err
	}
	if v.PlanID != plan {
		return "", fmt.Errorf("商品上下文不能混用另一方案")
	}
	var ref struct {
		ID  int64  `json:"set_id"`
		SHA string `json:"source_sha"`
	}
	raw, err := json.Marshal(v.GenerationContext["content_pools"])
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(raw, &ref); err != nil {
		return "", err
	}
	if ref.ID == 0 {
		return "", nil
	}
	set, err := source.GetContentPoolSet(ctx, s.TenantID, plan, ref.ID)
	if err != nil {
		return "", err
	}
	if set.ID != ref.ID || set.SourceSHA != ref.SHA {
		return "", fmt.Errorf("商品上下文版本不一致")
	}
	return contentpools.ProductContext(set), nil
}
