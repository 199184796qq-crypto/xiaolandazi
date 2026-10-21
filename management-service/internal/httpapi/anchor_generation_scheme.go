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
	PoolPolicy            *anchorcycle.ProductPolicy `json:"pool_policy,omitempty"`
	PoolPolicySource      string                     `json:"pool_policy_source,omitempty"`
	ProductFocus          *anchorcycle.ProductFocus  `json:"product_focus,omitempty"`
	SingleProductSchedule *anchorcycle.FocusProduct  `json:"single_product_schedule,omitempty"`
	StyleMD               string                     `json:"style_md"`
	BaselineJSON          string                     `json:"baseline_json"`
	BufferSeconds         int                        `json:"buffer_seconds"`
	Version               int                        `json:"version"`
	StyleID               int64                      `json:"style_id"`
	ScorecardID           int64                      `json:"scorecard_id"`
	WheelID               int64                      `json:"wheel_id"`
	Config                anchorcycle.Config         `json:"config"`
}

func realtimeScheme(data map[string]any) (liveAnchorScheme, error) {
	var scheme liveAnchorScheme
	raw, err := json.Marshal(data["anchor_generation"])
	if err == nil {
		err = json.Unmarshal(raw, &scheme)
	}
	// 只有卖点轮盘是必填项：它是本场任务来源。主播风格与评分基准都允许为空（自由发挥模式），
	// 这里不能因为它们为 0 就把整个方案判为「未初始化」，否则无风格方案永远开不了播。
	if err != nil || scheme.Version != 1 || scheme.WheelID <= 0 {
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
	// 风格与评分基准都可选：方案没绑风格时按自由发挥模式保存快照（StyleID/ScorecardID 为 0，
	// StyleMD/BaselineJSON 为空），开播后不打分。卖点轮盘仍必填，它是任务来源。
	meta, wheel, _, card, err := s.optionalAnchorStyleAssets(ctx, tenant, plan)
	if err != nil {
		return err
	}
	// 风格卡只有元提示词、没有样本评分基准；它仍然可以开播，只是本场不打分。
	cardStyle := false
	if bound, bindErr := s.store.GetLiveAnchorStyleForPlan(ctx, tenant, plan); bindErr == nil && bound.StyleSourceMode == "card" {
		cardStyle = true
	}
	if meta.ID != 0 && card.ID == 0 && !cardStyle {
		return errors.New("请先初始化当前主播评分基准")
	}
	config := applyRealtimeOptions(settings.Cycle, options)
	config.MaxCycles = 0
	config.Rolling = true
	input.GenerationContext["anchor_generation_options"] = options
	scheme := liveAnchorScheme{Version: 1, WheelID: wheel.ID, Config: config, BufferSeconds: options.BufferSeconds}
	if meta.ID != 0 {
		scheme.StyleID = meta.ID
		scheme.StyleMD = meta.EffectiveMetaPromptMD()
	}
	if card.ID != 0 {
		scheme.ScorecardID = card.ID
		scheme.BaselineJSON = card.EffectiveScorecardJSON()
	}
	input.GenerationContext["anchor_generation"] = scheme
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
	if err = (realtimeGenerationOptions{scheme.BufferSeconds, scheme.Config.BodyMin, scheme.Config.BodyMax, scheme.Config.MaxChars, scheme.Config.SelfReference}).validate(); err != nil {
		return err
	}
	// StyleID / ScorecardID 为 0 表示本方案未绑主播风格：允许，按自由发挥模式校验。
	if scheme.StyleID != 0 {
		meta, err := s.store.GetLiveAnchorStyleMetaPrompt(ctx, tenant, plan, scheme.StyleID)
		if err != nil || meta.EffectiveMetaPromptMD() == "" {
			return errors.New("已保存主播元提示词不存在")
		}
		if scheme.ScorecardID != 0 {
			card, err := s.store.GetLiveAnchorStyleScorecard(ctx, tenant, plan, scheme.ScorecardID)
			if err != nil || card.MetaPromptAssetID != meta.ID {
				return errors.New("已保存评分基准与主播风格不匹配")
			}
			if _, err = anchorstylescore.Parse(scheme.BaselineJSON); err != nil {
				return err
			}
		}
		if scheme.StyleMD == "" || scheme.BufferSeconds < 20 || scheme.BufferSeconds > 600 {
			return errors.New("已保存主播风格或缓存配置无效")
		}
	} else if scheme.BufferSeconds < 20 || scheme.BufferSeconds > 600 {
		return errors.New("已保存缓存配置无效")
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
