package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

type livePlanWorkspaceCheck struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// A label names a fact; it does not change its category, claim, conditions or
// forbidden wording. Never normalize product cards or welfare link fields.
func savedFactIdentity(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- [") {
		return line
	}
	end := strings.Index(line, "]")
	if end < 0 {
		return line
	}
	header := line[3:end]
	category, _, labeled := strings.Cut(header, "/")
	if !labeled {
		return line
	}
	return "- [" + category + "]" + line[end+1:]
}

// Rebind a run-only copy to the saved safe facts, retaining the approved link
// assignments. Changed claims cannot inherit a scope by sharing a label. Facts
// with no unambiguous saved assignment remain outside product-specific prompts.
func savedProductFocusForFacts(saved *anchorcycle.ProductFocus, facts string) (*anchorcycle.ProductFocus, error) {
	if saved == nil {
		return nil, nil
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	var focus anchorcycle.ProductFocus
	if err = json.Unmarshal(raw, &focus); err != nil {
		return nil, err
	}
	if focus.FactsSHA == derivedHash(facts) {
		return &focus, nil
	}
	byIdentity := map[string][]string{}
	savedIdentities := map[string]int{}
	for _, fact := range focus.Facts {
		savedIdentities[savedFactIdentity(fact.Text)]++
	}
	exact := map[string]bool{}
	for _, line := range productFactBlocks(facts) {
		exact[line] = true
		key := savedFactIdentity(line)
		byIdentity[key] = append(byIdentity[key], line)
	}
	retained := make([]anchorcycle.ScopedFact, 0, len(focus.Facts))
	for _, fact := range focus.Facts {
		line := strings.TrimSpace(fact.Text)
		if exact[line] {
			fact.Text = line
			retained = append(retained, fact)
		} else if matches := byIdentity[savedFactIdentity(line)]; len(matches) == 1 && savedIdentities[savedFactIdentity(line)] == 1 {
			fact.Text = matches[0]
			retained = append(retained, fact)
		}
	}
	focus.Facts, focus.FactsSHA = retained, derivedHash(facts)
	if focus.MaterialVersion == productCommonMaterialVersion {
		focus.RefreshProductBundles()
	}
	return &focus, nil
}

func savedPlanDifferenceChecks(version *model.LiveAgentPlanVersion, safety model.LiveDerivedSafetyAsset, source *derivedInput) []livePlanWorkspaceCheck {
	checks := []livePlanWorkspaceCheck{}
	if source != nil && (source.Facts != safety.OriginalFacts || source.WheelJSON != safety.OriginalWheelJSON) {
		checks = append(checks, livePlanWorkspaceCheck{"plan_sources_changed", "当前事实、福利或卖点轮盘与已保存方案有差异。本场继续使用已保存版本；请核对后保存，让下次开播使用最新内容。"})
	}
	if isRealtimeScheme(version.GenerationContext) {
		scheme, err := realtimeScheme(version.GenerationContext)
		if err == nil && scheme.ProductFocus != nil && scheme.ProductFocus.FactsSHA != derivedHash(safety.SafeFacts) {
			focus, bindErr := savedProductFocusForFacts(scheme.ProductFocus, safety.SafeFacts)
			message := "商品范围与安全事实的指纹不同，兼容的事实名称变化会自动适配。"
			if bindErr != nil || len(focus.Facts) != len(scheme.ProductFocus.Facts) {
				message = "商品范围与安全事实存在内容差异，无法对应的事实暂不进入商品专属讲解。请核对后保存方案。"
			}
			checks = append(checks, livePlanWorkspaceCheck{"product_focus_snapshot_changed", message})
		}
	}
	return checks
}

// Read-only, nonblocking diagnostics belong to the strategy workspace. This
// path never invokes a model, updates reviews or changes the published version.
func (s *Server) liveAgentPlanWorkspaceChecks(ctx context.Context, tenant, plan int64, version *model.LiveAgentPlanVersion) []livePlanWorkspaceCheck {
	checks := []livePlanWorkspaceCheck{}
	ref := asSupportMap(version.GenerationContext["derived_safety"])
	hash, _ := ref["source_sha256"].(string)
	if hash == "" {
		return []livePlanWorkspaceCheck{{"saved_snapshot_missing", "该历史方案尚无安全事实快照，请核对并保存方案。"}}
	}
	safety, err := s.store.GetLiveDerivedSafety(ctx, tenant, plan, hash)
	if err != nil {
		return []livePlanWorkspaceCheck{{"saved_snapshot_unavailable", "暂时无法读取已保存事实快照，请稍后重新调阅方案。"}}
	}
	if isRealtimeScheme(version.GenerationContext) {
		if err = s.validateRealtimeScheme(ctx, tenant, plan, version.GenerationContext); err != nil {
			checks = append(checks, livePlanWorkspaceCheck{"saved_scheme_check", err.Error()})
		}
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	var source *derivedInput
	if err == nil {
		source, err = s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	}
	if err != nil {
		checks = append(checks, livePlanWorkspaceCheck{"source_check_unavailable", "暂时无法核对当前方案来源，已保存版本仍可调阅。"})
	}
	return append(checks, savedPlanDifferenceChecks(version, safety, source)...)
}
