package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"path/filepath"
)

type scriptSharedAssetKey struct{}
type scriptSharedAssetOwner struct {
	Room int64
	Plan int64
}

func scriptFirstCacheDir(tenant, room int64, v model.LiveAgentPlanVersion) (string, error) {
	if tenant <= 0 || room <= 0 || v.PlanID <= 0 || (v.TenantID > 0 && v.TenantID != tenant) {
		return "", errors.New("方案声音缓存归属无效")
	}
	if hasGroupedRecordingScript(v) {
		plan, ok := planCacheDir(tenant, v.PlanID, v.VoiceIdentity.Model)
		if !ok {
			return "", errors.New("方案声音缓存目录不可用")
		}
		return filepath.Join(plan, "paragraphs"), nil
	}
	scope, ok := recordingCacheScopeFor(tenant, room, v.PlanID)
	if !ok {
		return "", errors.New("房间声音缓存目录不可用")
	}
	return scope.CacheDir(scriptFirstNamespace(v.VoiceIdentity.Model), "paragraphs"), nil
}

func withScriptSharedAsset(ctx context.Context, v model.LiveAgentPlanVersion, room int64) context.Context {
	if !hasGroupedRecordingScript(v) {
		return ctx
	}
	return context.WithValue(ctx, scriptSharedAssetKey{}, scriptSharedAssetOwner{Room: room, Plan: v.PlanID})
}
func applyScriptSharedAssetInfo(ctx context.Context, metadata map[string]any) {
	if owner, ok := ctx.Value(scriptSharedAssetKey{}).(scriptSharedAssetOwner); ok {
		metadata["cache_scope"] = "plan_shared"
		metadata["plan_id"] = owner.Plan
		metadata["room_id"] = int64(0)
		metadata["generated_by_room_id"] = owner.Room
	}
}
func isSharedScriptMedia(asset model.MediaAsset) bool {
	if asset.AssetType != "generated_voice" {
		return false
	}
	if asset.Metadata["cache_scope"] == "plan_shared" {
		return true
	}
	// Old plan paragraph archives were labelled with the room that first paid
	// for them. They are still referenced by other rooms' immutable pool entries.
	if asset.Metadata["purpose"] != "live_agent_formal_voice_timeline" {
		return false
	}
	_, paragraph := asset.Metadata["paragraph_key"]
	_, native := asset.Metadata["volcengine_tempo_percent"]
	return paragraph || native
}
