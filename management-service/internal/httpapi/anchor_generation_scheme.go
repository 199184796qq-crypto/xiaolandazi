package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/model"
)

// A scheme is a database-versioned configuration, not a preview recording.
// IDs, credentials and policies are populated by the server on save. Only the
// validated customer scheduling whitelist can vary. Runs take Redis-only copies.
type liveAnchorScheme struct {
	PoolPolicy       *anchorcycle.ProductPolicy `json:"pool_policy,omitempty"`
	PoolPolicySource string                     `json:"pool_policy_source,omitempty"`
	ProductFocus     *anchorcycle.ProductFocus  `json:"product_focus,omitempty"`
	StyleMD          string                     `json:"style_md"`
	BaselineJSON     string                     `json:"baseline_json"`
	BufferSeconds    int                        `json:"buffer_seconds"`
	Version          int                        `json:"version"`
	StyleID          int64                      `json:"style_id"`
	ScorecardID      int64                      `json:"scorecard_id"`
	WheelID          int64                      `json:"wheel_id"`
	Config           anchorcycle.Config         `json:"config"`
}

func realtimeScheme(data map[string]any) (liveAnchorScheme, error) {
	var scheme liveAnchorScheme
	raw, err := json.Marshal(data["anchor_generation"])
	if err == nil {
		err = json.Unmarshal(raw, &scheme)
	}
	if err != nil || scheme.Version != 1 || scheme.StyleID <= 0 || scheme.ScorecardID <= 0 || scheme.WheelID <= 0 {
		return scheme, errors.New("实时声音方案尚未初始化或保存，请初始化直播智能体后保存发布")
	}
	return scheme, nil
}

func isRealtimeScheme(data map[string]any) bool {
	mode, _ := data["content_mode"].(string)
	_, ok := data["anchor_generation"]
	return mode == model.LiveContentAIDynamic && ok
}

func (s *Server) captureRealtimeScheme(ctx context.Context, tenant, plan int64, input *model.CreateLiveAgentPlanVersionInput) error {
	// Discard all caller-supplied internal snapshots, including on static modes.
	delete(input.GenerationContext, "anchor_generation")
	delete(input.GenerationContext, "content_pools")
	if input.GenerationContext["content_mode"] != model.LiveContentAIDynamic || len(input.Variants) > 0 {
		delete(input.GenerationContext, "anchor_generation_options")
		return nil
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return err
	}
	if err = settings.Validate(); err != nil {
		return err
	}
	options, err := realtimeOptionsFromContext(input.GenerationContext, realtimeOptionsDefaults(settings))
	if err != nil {
		return err
	}
	meta, wheel, _, err := s.confirmedAnchorStyleAgentStudioAssets(ctx, tenant, plan)
	if err != nil {
		return err
	}
	card, err := s.store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan)
	if err != nil || card.Status != "confirmed" || card.MetaPromptAssetID != meta.ID {
		return errors.New("请先初始化当前主播评分基准")
	}
	if _, err = anchorstylescore.Parse(card.EffectiveScorecardJSON()); err != nil {
		return err
	}
	config := applyRealtimeOptions(settings.Cycle, options)
	config.MaxCycles = 0
	config.Rolling = true
	input.GenerationContext["anchor_generation_options"] = options
	input.GenerationContext["anchor_generation"] = liveAnchorScheme{Version: 1, StyleID: meta.ID, ScorecardID: card.ID, WheelID: wheel.ID, Config: config, StyleMD: meta.EffectiveMetaPromptMD(), BaselineJSON: card.EffectiveScorecardJSON(), BufferSeconds: options.BufferSeconds}
	return nil
}

func (s *Server) validateRealtimeScheme(ctx context.Context, tenant, plan int64, data map[string]any) error {
	scheme, err := realtimeScheme(data)
	if err != nil {
		return err
	}
	if scheme.PoolPolicy != nil {
		if err = scheme.PoolPolicy.Validate(); err != nil {
			return err
		}
	}
	if scheme.ProductFocus != nil {
		if err = scheme.ProductFocus.Policy.Validate(); err != nil {
			return err
		}
		if err = scheme.ProductFocus.ValidateProducts(); err != nil {
			return err
		}
	}
	if err = (realtimeGenerationOptions{scheme.BufferSeconds, scheme.Config.BodyMin, scheme.Config.BodyMax, scheme.Config.MaxChars}).validate(); err != nil {
		return err
	}
	meta, err := s.store.GetLiveAnchorStyleMetaPrompt(ctx, tenant, plan, scheme.StyleID)
	if err != nil || meta.EffectiveMetaPromptMD() == "" {
		return errors.New("已保存主播元提示词不存在")
	}
	card, err := s.store.GetLiveAnchorStyleScorecard(ctx, tenant, plan, scheme.ScorecardID)
	if err != nil || card.MetaPromptAssetID != meta.ID {
		return errors.New("已保存评分基准与主播风格不匹配")
	}
	if scheme.StyleMD == "" || scheme.BufferSeconds < 20 || scheme.BufferSeconds > 600 {
		return errors.New("已保存主播风格或缓存配置无效")
	}
	if _, err = anchorstylescore.Parse(scheme.BaselineJSON); err != nil {
		return err
	}
	if _, err = s.store.GetLiveSellingPointWheel(ctx, tenant, plan, scheme.WheelID); err != nil {
		return err
	}
	safetyRef := asSupportMap(data["derived_safety"])
	hash, _ := safetyRef["source_sha256"].(string)
	safety, err := s.store.GetLiveDerivedSafety(ctx, tenant, plan, hash)
	if err != nil || safety.SafeFacts == "" || safety.SafeWheelJSON == "" {
		return errors.New("已保存安全事实包和轮盘不存在")
	}
	_, err = s.prepareSavedAnchorTaskState(ctx, tenant, plan, data, scheme, safety)
	return err
}
