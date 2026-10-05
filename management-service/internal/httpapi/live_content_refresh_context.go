package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

func (s *Server) contentRefreshGenerationContext(ctx context.Context, version model.LiveAgentPlanVersion) (model.LiveAgentFullShowGenerationContext, string, error) {
	var saved model.LiveAgentFullShowGenerationContext
	raw, err := json.Marshal(version.GenerationContext)
	if err != nil {
		return saved, "", err
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return saved, "", err
	}
	plan, err := s.store.GetLiveAgentPlan(ctx, version.TenantID, version.PlanID)
	if err != nil {
		return saved, "", err
	}
	facts, err := s.store.ListLiveAgentPlanFacts(ctx, version.TenantID, version.PlanID)
	if err != nil {
		return saved, "", err
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(ctx, version.TenantID, version.PlanID, time.Now())
	if err != nil {
		return saved, "", err
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, version.TenantID, version.PlanID)
	if err != nil {
		return saved, "", err
	}
	industry, l1, l2, l3, err := s.store.LoadLivePolicyLayers(ctx, version.TenantID, version.RoomID)
	if err != nil {
		return saved, "", err
	}
	effective := policy.BuildEffective(industry, l1, l2, l3)
	input := model.LiveAgentFullShowPreviewInput{
		RoomID: version.RoomID, DurationMinutes: version.DurationMinutes, RoundMinutes: version.RoundMinutes,
		VariantCount: len(version.Variants), UseAnchorStyle: saved.UseAnchorStyle, UseDynamicFacts: true,
		GenerateTTSHints: saved.GenerateTTSHints, AvoidRecent: true, RhythmNodes: saved.RhythmNodes, AnchorStyle: saved.AnchorStyle,
	}
	result := compileFullShowContext(plan, facts, benefits, links, nil, input)
	if err := s.attachPlanStyleOverlay(ctx, version.TenantID, version.PlanID, &result); err != nil {
		return result, "", err
	}
	result.IndustryCode = industry
	result.PolicyRuleCount = len(effective.Rules)
	if len(result.FormalFacts)+len(result.Benefits)+len(result.ProductLinks) == 0 {
		return result, "", errors.New("没有正式事实可用于自动更新")
	}
	// Audit the retained 75% too. Never publish a partly refreshed program
	// containing numeric claims that have been removed from the current facts.
	auditContext := result
	auditContext.AvoidRecent = false
	for _, variant := range version.Variants {
		if !variant.IsFormal {
			continue
		}
		audited := auditFullShowVariants(auditContext, []model.LiveAgentFullShowVariant{{Text: variant.Text}}, nil)
		if !audited[0].Audit.Passed {
			return result, "", errors.New("已发布主线与当前事实不一致，请先确认并重新发布成品")
		}
	}
	return result, effective.PromptText, nil
}

func (s *Server) contentRefreshProgramTracks(ctx context.Context, version model.LiveAgentPlanVersion) ([]coreclient.ProgramRefreshTrack, error) {
	var tracks []coreclient.ProgramRefreshTrack
	for _, v := range version.Variants {
		if !v.IsFormal {
			continue
		}
		asset, err := s.store.GetMediaAsset(ctx, version.TenantID, v.AudioAssetID)
		if err != nil {
			return nil, err
		}
		assetStore, err := s.assetStorage.For(asset.StorageDriver)
		if err != nil {
			return nil, err
		}
		expiry := max(12*time.Hour, s.assetURLTTL)
		var audioURL string
		if signer, ok := assetStore.(interface {
			InternalSignedURL(context.Context, string, time.Duration) (string, error)
		}); ok {
			audioURL, err = signer.InternalSignedURL(ctx, asset.ObjectKey, expiry)
		} else {
			audioURL, err = assetStore.SignedURL(ctx, asset.ObjectKey, expiry)
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(audioURL) == "" {
			return nil, errors.New("更新成品没有有效声音地址")
		}
		var timeline []coreclient.ProgramRefreshTimelineSegment
		var safePoints []coreclient.ProgramRefreshSafePoint
		raw, _ := json.Marshal(v.Timeline)
		if err := json.Unmarshal(raw, &timeline); err != nil {
			return nil, err
		}
		raw, _ = json.Marshal(v.SafePoints)
		if err := json.Unmarshal(raw, &safePoints); err != nil {
			return nil, err
		}
		tracks = append(tracks, coreclient.ProgramRefreshTrack{ID: v.VariantKey, Label: v.Title, Text: v.Text, AudioURL: audioURL, DurationMS: int(v.AudioDurationMS), Timeline: timeline, SafePoints: safePoints})
	}
	if len(tracks) == 0 {
		return nil, errors.New("更新成品没有正式主线")
	}
	return tracks, nil
}
