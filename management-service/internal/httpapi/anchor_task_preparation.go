package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

// Save validation and actual startup execute this same preparation path. It
// reads only the saved inputs; no model, TTS, runtime lease or playback is used.
func (s *Server) prepareSavedAnchorTaskState(ctx context.Context, tenant, plan int64, data map[string]any, scheme liveAnchorScheme, safety model.LiveDerivedSafetyAsset) (anchorcycle.State, error) {
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(safety.SafeWheelJSON), &wheel); err != nil {
		return anchorcycle.State{}, err
	}
	var tasks []anchorcycle.Task
	for _, item := range flattenAnchorStyleWheelTasks(wheel) {
		tasks = append(tasks, anchorcycle.Task{SellingPoint: item.SellingPoint, Angle: item.Angle, Key: item.Key, Tier: item.Tier, TargetAudience: item.TargetAudience, PainPoint: item.PainPoint})
	}
	set, err := s.savedContentPoolSet(ctx, tenant, plan, data)
	if err != nil {
		return anchorcycle.State{}, err
	}
	var focus *anchorcycle.ProductFocus
	if set != nil {
		if set.SafeFacts != safety.SafeFacts || scheme.PoolPolicy == nil {
			return anchorcycle.State{}, errors.New("已保存内容池与事实或调度配置不匹配")
		}
		tasks, focus, err = contentPoolRuntime(*set, *scheme.PoolPolicy)
		if err != nil {
			return anchorcycle.State{}, err
		}
	} else if scheme.ProductFocus != nil {
		focus, err = savedProductFocusForFacts(scheme.ProductFocus, safety.SafeFacts)
		if err != nil {
			return anchorcycle.State{}, err
		}
		if focus.MaterialVersion == productCommonMaterialVersion || hasProductLevelSellingPoints(focus.Products) {
			// These scopes are internal program relationships, not model output.
			// Rebuild the run-only index from the immutable per-product snapshot;
			// never inherit a stale key map or expand another product's facts.
			tasks = anchorcycle.ExpandProductSellingPoints(focus.Products, &focus.Policy)
			focus.Tasks = anchorcycle.ProductTaskScopes(tasks)
			if focus.MaterialVersion == productCommonMaterialVersion {
				if err = validateProductFactSnapshot(safety.SafeFacts, focus); err != nil {
					return anchorcycle.State{}, err
				}
				focus.RefreshProductBundles()
			}
		} else if savedTasks := savedLegacyQueue(wheel, focus); len(savedTasks) > 0 {
			tasks = savedTasks
		}
	} else if scheme.SingleProductSchedule != nil {
		policy := anchorcycle.ProductPolicy{}
		tasks = anchorcycle.ExpandProductSellingPoints([]anchorcycle.FocusProduct{*scheme.SingleProductSchedule}, &policy)
	}
	config := scheme.Config
	config.MaxCycles = 0
	state, err := anchorcycle.New(tasks, config)
	if err != nil {
		return state, err
	}
	state.Surface = "formal_live"
	if focus == nil {
		state.SelectProductTask()
		return state, nil
	}
	if err = focus.Policy.Validate(); err != nil {
		return state, err
	}
	if err = focus.ValidateProducts(); err != nil {
		return state, err
	}
	focus.ResetRun()
	state.ProductFocus = focus
	// Check all products through the real selector, not an uninitialized queue.
	// Shared/legacy scopes retain their existing meaning and IDs.
	for _, product := range focus.Products {
		probe, scoped := state, *focus
		scoped.CurrentLink = product.LinkKey
		scoped.CurrentSlot = scoped.ScheduleCursor
		probe.ProductFocus = &scoped
		probe.SelectProductTask()
		if probe.Status == "spacing_blocked" || probe.Status == "compliance_blocked" {
			return state, fmt.Errorf("商品任务准备失败：链接%s没有匹配的单品任务", product.LinkKey)
		}
	}
	state.SelectProductTask()
	return state, nil
}
