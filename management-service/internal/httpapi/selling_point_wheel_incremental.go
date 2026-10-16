package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type wheelPatchUpdate struct {
	ID    int                               `json:"id"`
	Point *anchorStyleSellingPointWheelItem `json:"point"`
}
type wheelPatch struct {
	Updates   []wheelPatchUpdate                 `json:"updates"`
	Additions []anchorStyleSellingPointWheelItem `json:"additions"`
	FactGaps  *[]anchorStyleFactGapSuggestion    `json:"fact_gap_suggestions"`
}

func emptySellingPointPool(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	var wheel anchorStyleSellingPointWheel
	if json.Unmarshal([]byte(raw), &wheel) != nil {
		return false
	}
	if len(wheel.Items) == 0 {
		return true
	}
	for _, item := range wheel.Items {
		for _, angle := range item.Angles {
			if strings.TrimSpace(angle) != "" {
				return false
			}
		}
	}
	return true
}
func wheelFactDelta(before, after string) (added, removed []string) {
	lines := func(text string) map[string]bool {
		m := map[string]bool{}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				m[line] = true
			}
		}
		return m
	}
	a, b := lines(before), lines(after)
	added, removed = []string{}, []string{}
	for _, line := range strings.Split(after, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !a[line] {
			added = append(added, line)
			a[line] = true
		}
	}
	for _, line := range strings.Split(before, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !b[line] {
			removed = append(removed, line)
			b[line] = true
		}
	}
	return
}
func mergeWheelPatch(existing anchorStyleSellingPointWheel, p wheelPatch, allowed map[int]bool) (anchorStyleSellingPointWheel, error) {
	if p.Updates == nil || p.Additions == nil {
		return existing, errors.New("增量结果缺少更新或新增数组")
	}
	updates := map[int]*anchorStyleSellingPointWheelItem{}
	for _, u := range p.Updates {
		if u.ID < 1 || u.ID > len(existing.Items) {
			return existing, errors.New("增量更新引用不存在的卖点")
		}
		if _, ok := updates[u.ID]; ok {
			return existing, errors.New("增量更新重复卖点ID")
		}
		if allowed != nil && !allowed[u.ID] {
			return existing, errors.New("增量复核试图修改补丁外的卖点")
		}
		updates[u.ID] = u.Point
	}
	merged := anchorStyleSellingPointWheel{Items: []anchorStyleSellingPointWheelItem{}, FactGaps: existing.FactGaps}
	for i, item := range existing.Items {
		if point, ok := updates[i+1]; ok {
			if point != nil {
				merged.Items = append(merged.Items, *point)
			}
		} else {
			merged.Items = append(merged.Items, item)
		}
	}
	merged.Items = append(merged.Items, p.Additions...)
	if p.FactGaps != nil {
		merged.FactGaps = *p.FactGaps
	}
	names := map[string]bool{}
	for _, item := range merged.Items {
		if names[item.Name] {
			return existing, errors.New("增量结果重复卖点主题")
		}
		names[item.Name] = true
	}
	return merged, nil
}
func wheelSourceFacts(asset model.LiveSellingPointWheelAsset) string {
	var source struct {
		Facts string `json:"fact_package"`
	}
	if json.Unmarshal([]byte(asset.RequestUser), &source) != nil || source.Facts == "" || derivedHash(source.Facts) != asset.FactPackageSHA256 {
		return ""
	}
	return source.Facts
}

// Reuse the actual confirmed pool, including user edits; never overwrite it
// merely because the initialization button was clicked again.
func (s *Server) reuseOrUpdateWheel(ctx context.Context, tenant, plan, actor int64, facts, contract, promptSnapshot, profile string) (bool, int, int, error) {
	asset, err := s.store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
	if errors.Is(err, appdb.ErrLiveSellingPointWheelNotFound) {
		return false, 0, 0, nil
	}
	if err != nil {
		return true, 0, 0, err
	}
	if asset.Status != "confirmed" {
		return false, 0, 0, nil
	}
	if emptySellingPointPool(asset.EffectiveWheelJSON()) {
		reportAnchorAnalysis(ctx, 35, "尚未有讲解要点，开始完整梳理与核对")
		return false, 0, 0, nil
	}
	canonical, points, angles, err := normalizeAnchorStyleSellingPointWheelJSON(asset.EffectiveWheelJSON())
	if err != nil {
		return true, 0, 0, err
	}
	if err = validateSellingPointPromptContract(canonical, contract); err != nil {
		return true, 0, 0, err
	}
	if asset.ExtractionPromptMD != promptSnapshot {
		reportAnchorAnalysis(ctx, 35, "讲解要点生成规则已更新，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	if asset.FactPackageSHA256 == derivedHash(facts) && len(sellingPointFactBoundaryViolations(canonical, facts)) == 0 {
		reportAnchorAnalysis(ctx, 94, "讲解要点没有变化，直接使用已确认内容")
		return true, points, angles, nil
	}
	before := wheelSourceFacts(asset)
	if before == "" {
		reportAnchorAnalysis(ctx, 35, "旧讲解要点缺少原始依据，需要完整核对一次")
		return false, 0, 0, nil
	}
	mother, source, err := loadReplaceablePromptAsset("ANCHOR_WHEEL_INCREMENTAL_PROMPT_PATH", "configs/prompts/anchor-style/selling-point-wheel-incremental.md", "卖点池增量更新母版")
	if err != nil {
		return true, 0, 0, err
	}
	var existing anchorStyleSellingPointWheel
	if err = json.Unmarshal([]byte(canonical), &existing); err != nil {
		return true, 0, 0, err
	}
	added, removed := wheelFactDelta(before, facts)
	if len(added) == 0 && len(removed) == 0 && len(sellingPointFactBoundaryViolations(canonical, facts)) == 0 {
		return true, points, angles, nil
	}
	rows := []map[string]any{}
	for i, item := range existing.Items {
		rows = append(rows, map[string]any{"id": i + 1, "point": item})
	}
	data := map[string]any{"phase": "update", "fact_package": facts, "added_facts": added, "removed_facts": removed, "existing_points": rows, "existing_fact_gap_suggestions": existing.FactGaps}
	scoped := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	request, _ := json.Marshal(data)
	reportAnchorAnalysis(ctx, 45, "正在更新新增资料和受影响的讲解要点，其余内容保留")
	result, _, err := s.completeAnchorStyleWorkbench(scoped, profile, "selling_point_incremental_update", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(request)}}, 5000, false)
	if err != nil {
		return true, 0, 0, err
	}
	var patch wheelPatch
	if err = decodeProductJSON(result.Text, &patch); err != nil {
		reportAnchorAnalysis(ctx, 35, "讲解要点更新结果无效，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	if _, err = mergeWheelPatch(existing, patch, nil); err != nil {
		reportAnchorAnalysis(ctx, 35, "讲解要点更新合并失败，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	allowed := map[int]bool{}
	for _, u := range patch.Updates {
		allowed[u.ID] = true
	}
	data["phase"], data["proposed_patch"] = "review", patch
	request, _ = json.Marshal(data)
	reportAnchorAnalysis(ctx, 75, "正在校对本次变化内容，不重新梳理全部讲解要点")
	auditRules := strings.Split(contract, "## 五、输出协议")[0]
	review, _, err := s.completeAnchorStyleWorkbench(scoped, profile, "selling_point_incremental_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: auditRules + "\n\n" + mother}, {Role: "user", Content: string(request)}}, 5000, false)
	if err != nil {
		return true, 0, 0, err
	}
	var checked wheelPatch
	if err = decodeProductJSON(review.Text, &checked); err != nil {
		reportAnchorAnalysis(ctx, 35, "讲解要点校对结果无效，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	merged, err := mergeWheelPatch(existing, checked, allowed)
	if err != nil {
		reportAnchorAnalysis(ctx, 35, "讲解要点校对合并失败，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	// A review must explicitly account for each affected old point. Omission
	// must not silently restore obsolete prices or claims from the old pool.
	if len(checked.Updates) != len(patch.Updates) || len(checked.Additions) > len(patch.Additions) {
		reportAnchorAnalysis(ctx, 35, "校对遗漏了受影响的讲解要点，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	raw, _ := json.Marshal(merged)
	final, points, angles, err := normalizeAnchorStyleSellingPointWheelJSON(string(raw))
	if err != nil {
		reportAnchorAnalysis(ctx, 35, "更新结果不合法，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	if err = validateSellingPointPromptContract(final, contract); err != nil {
		reportAnchorAnalysis(ctx, 35, "更新结果未通过校验，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	if violations := sellingPointFactBoundaryViolations(final, facts); len(violations) > 0 {
		reportAnchorAnalysis(ctx, 35, "更新结果存在资料不符，重新完整梳理讲解要点")
		return false, 0, 0, nil
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		return true, 0, 0, err
	}
	latest, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil || latest.Facts != facts {
		return true, 0, 0, errors.New("增量更新期间事实已变化，原池未修改")
	}
	current, err := s.store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
	if err != nil || current.ID != asset.ID || derivedHash(current.EffectiveWheelJSON()) != derivedHash(asset.EffectiveWheelJSON()) {
		return true, 0, 0, errors.New("增量更新期间卖点池已被修改，未覆盖用户编辑")
	}
	requestSource, _ := json.Marshal(map[string]string{"fact_package": facts})
	responses, _ := json.Marshal(map[string]any{"incremental_update_raw": result.Text, "incremental_review_raw": review.Text, "source_wheel_id": asset.ID, "added_facts": added, "removed_facts": removed})
	created, err := s.store.SaveLiveSellingPointWheel(ctx, tenant, plan, actor, model.SaveLiveSellingPointWheelInput{Status: "draft", ExtractionPromptMD: promptSnapshot + "\n\n" + mother, PromptSource: source, FactPackageSHA256: derivedHash(facts), OriginalWheelJSON: final, RequestSystem: mother, RequestUser: string(requestSource), RawResponse: string(responses), Provider: review.Provider, Model: review.Model, LatencyMS: result.LatencyMS + review.LatencyMS, InputTokens: result.InputTokens + review.InputTokens, OutputTokens: result.OutputTokens + review.OutputTokens})
	if err != nil {
		return true, 0, 0, err
	}
	_, err = s.store.ConfirmLiveSellingPointWheel(ctx, tenant, plan, created.ID, actor, final)
	return true, points, angles, err
}
