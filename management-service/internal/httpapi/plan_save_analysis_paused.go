package httpapi

import (
	"context"
	"errors"
	"strings"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// Saving a plan currently stores prepared inputs only. Do not relocate the
// paused model work to a cache miss, first playback, or a background save job.
type planSaveCacheOnlyKey struct{}

func planSaveCacheOnly(ctx context.Context) bool {
	value, _ := ctx.Value(planSaveCacheOnlyKey{}).(bool)
	return value
}

const planSaveAnalysisPausedField = "save_analysis_paused"

func planSaveAnalysisPaused(data map[string]any) bool {
	value, _ := data[planSaveAnalysisPausedField].(bool)
	return value
}

func resetPausedPlanSaveAnalysis(input *model.CreateLiveAgentPlanVersionInput) {
	if input.GenerationContext == nil {
		input.GenerationContext = map[string]any{}
	}
	// These are server-owned results. Caller-provided or inherited answers must
	// not be mistaken for a fresh analysis of this save's facts.
	for _, key := range []string{"derived_safety", "atomic_facts", "domain_card"} {
		delete(input.GenerationContext, key)
	}
	input.GenerationContext[planSaveAnalysisPausedField] = true
}

// existingSafetyForPlanSave validates the identity of an already prepared
// snapshot using read-only source/review reads. It never syncs reviews, rewrites
// facts, calls a model, or creates a new derived-safety asset.
func (s *Server) existingSafetyForPlanSave(ctx context.Context, tenant, plan int64) (model.LiveDerivedSafetyAsset, error) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	_, source, err := loadReplaceablePromptAsset("ANCHOR_STYLE_DERIVED_SAFETY_PROMPT_PATH", "configs/prompts/anchor-style/compliance/derived-assets-safe-rewrite.md", "派生卖点安全改写母版")
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	d, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	reviews, err := s.store.ListLiveRiskReviews(ctx, tenant, plan)
	if err != nil {
		return model.LiveDerivedSafetyAsset{}, err
	}
	for _, review := range reviews {
		d.Statuses[review.ClaimSHA256] = review
	}
	asset, err := s.store.GetLiveDerivedSafety(ctx, tenant, plan, d.fingerprint(source))
	if errors.Is(err, appdb.ErrDerivedSafetyNotFound) {
		return asset, errors.New("保存时的资料分析已暂停，当前资料没有可复用的初始化快照；实时生成模式请先完成初始化，再保存方案")
	}
	if err != nil {
		return asset, err
	}
	if asset.TenantID != tenant || asset.PlanID != plan || asset.ID <= 0 || strings.TrimSpace(asset.SafeFacts) == "" || strings.TrimSpace(asset.SafeWheelJSON) == "" {
		return asset, errors.New("已准备的实时方案资料不完整，请先完成初始化，再保存方案")
	}
	return asset, nil
}

func (s *Server) capturePlanSaveWithoutAnalysis(ctx context.Context, tenant, plan int64, input *model.CreateLiveAgentPlanVersionInput) error {
	resetPausedPlanSaveAnalysis(input)
	if !isRealtimeScheme(input.GenerationContext) {
		// Original recording, script-first TTS and prepared audio do not require
		// an AI-built fact snapshot to save or play their existing mainline.
		if model.LiveContentModeFromGenerationContext(input.GenerationContext) != model.LiveContentUserAudio {
			if set, ok, err := s.latestSavedContentPoolSet(ctx, tenant, plan); err == nil && ok {
				input.GenerationContext["content_pools"] = contentPoolRef(set)
			}
		}
		return nil
	}

	safety, err := s.existingSafetyForPlanSave(ctx, tenant, plan)
	if err != nil {
		return err
	}
	input.GenerationContext["derived_safety"] = derivedSafetyRef(safety)
	// Product ownership can be read from prepared cache, but a missing entry
	// must not start the old implicit classification model from the Save button.
	ctx = context.WithValue(ctx, planSaveCacheOnlyKey{}, true)
	return s.captureRealtimeProductFocus(ctx, tenant, plan, input, safety)
}
