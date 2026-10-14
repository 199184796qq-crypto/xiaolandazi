package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"math/rand/v2"
	"strings"
	"unicode/utf8"
)

// Only the paid read-only replay opts out of asset caching. This never changes
// P0, model roles, review or generation; real requests retain reusable examples.
type factExamplesEphemeralKey struct{}
type factExamplePool struct {
	Examples []string `json:"examples"`
}

func parseFactExamplePool(raw string) (factExamplePool, error) {
	var p factExamplePool
	if decodeLectureContent(raw, &p) != nil || len(p.Examples) < 3 || len(p.Examples) > 5 {
		return p, errors.New("正向举例结果不完整")
	}
	for _, text := range p.Examples {
		if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 800 {
			return p, errors.New("正向举例为空或过长")
		}
	}
	return p, nil
}
func shuffledFactExamples(examples []string) []string {
	ordered := append([]string(nil), examples...)
	rand.Shuffle(len(ordered), func(i, j int) { ordered[i], ordered[j] = ordered[j], ordered[i] })
	return ordered
}
func (s *Server) ensureFactExpressionExamples(ctx context.Context, state *anchorcycle.State) {
	task := state.Task()
	if task.FactScope == nil || len(task.FactScope.Facts) != 1 {
		return
	}
	if requireCycleP0(state) != nil {
		return
	}
	f := task.FactScope.Facts[0]
	facts, guidance := generationMaterialViews([]anchorcycle.HookFact{f})
	if len(guidance) == 0 {
		return
	}
	// Material views support both typed fields and the legacy three-field encoding.
	original := []map[string]string{}
	negative := []map[string]string{}
	view := generationFactView([]anchorcycle.HookFact{f})[0]
	all, _ := view["表达指导（不作为商品事实）"].([]map[string]string)
	for _, g := range all {
		label := g["要求"]
		if strings.Contains(label, "多个正向举例") {
			return
		}
		if strings.Contains(label, "改写") || strings.Contains(label, "举例") || strings.Contains(label, "参考") {
			original = append(original, g)
		} else {
			negative = append(negative, g)
		}
	}
	if len(original) == 0 {
		return
	}
	// Strip the single original example even if pool preparation is unavailable.
	// The original remains in the saved fact and exchange, never promoted to fact.
	f.ExpressionGuidance = append([]map[string]string(nil), negative...)
	f.Data, _ = view["事实内容"].(string)
	task.FactScope.Facts[0] = f
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_EXAMPLES_PATH", "configs/prompts/anchor-style/fact-expression-examples.md", "正向举例库母版")
	if err != nil {
		state.Warn("举例库暂不可用，按当前事实和目的生成")
		return
	}
	reviewMother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_EXAMPLES_REVIEW_PATH", "configs/prompts/anchor-style/fact-expression-examples-review.md", "正向举例复核母版")
	if err != nil {
		return
	}
	purpose := task.PainPoint
	var confirmed any = facts
	if task.ProductMode == "lecture" {
		purpose = task.LecturePurpose
		confirmed = task.LectureFacts
	}
	data := map[string]any{"confirmed_facts": confirmed, "negative_constraints": negative, "existing_example": original, "requested_purpose": purpose, "requested_angle": task.Angle, "current_rules": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	if task.LecturePlanJSON != "" {
		data["segment_plan"] = json.RawMessage(task.LecturePlanJSON)
	}
	data["user_confirmed_corrections"] = state.EffectiveCorrections()
	raw, _ := json.Marshal(data)
	fingerprint := derivedHash(string(raw) + f.ID + mother + reviewMother)
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	var pool factExamplePool
	cached, e := s.store.GetPlanAnalysisAsset(ctx, state.TenantID, state.PlanID, "fact_expression_examples", fingerprint)
	if e == nil {
		var asset struct {
			Examples []string `json:"examples"`
			InputSHA string   `json:"input_sha256"`
			Reviewed bool     `json:"reviewed"`
		}
		if json.Unmarshal([]byte(cached), &asset) != nil || !asset.Reviewed || asset.InputSHA != fingerprint {
			err = errors.New("举例资产版本不匹配")
		} else {
			wire, _ := json.Marshal(factExamplePool{asset.Examples})
			pool, err = parseFactExamplePool(string(wire))
		}
	} else if !errors.Is(e, appdb.ErrPlanAnalysisAssetNotFound) {
		state.Warn("举例库读取未完成，按事实生成")
		return
	} else {
		err = e
	}
	if err != nil {
		result, _, callErr := s.completeAnchorStyleWorkbench(ctx, "", "fact_expression_examples", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, 3200, false)
		if callErr != nil {
			return
		}
		pool, err = parseFactExamplePool(result.Text)
		if err != nil {
			return
		}
		data["examples"] = pool.Examples
		reviewData, _ := json.Marshal(data)
		review, _, callErr := s.completeAnchorStyleWorkbench(ctx, "", "fact_expression_examples_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: reviewMother}, {Role: "user", Content: string(reviewData)}}, 1500, false)
		if callErr != nil || normalizationReviewPassed(review.Text) != nil {
			state.Warn("举例未通过复核，仅采用事实和表达边界")
			return
		}
		if ephemeral, _ := ctx.Value(factExamplesEphemeralKey{}).(bool); !ephemeral {
			saved, _ := json.Marshal(map[string]any{"examples": pool.Examples, "fact_version_ref": f.ID, "input_sha256": fingerprint, "rule_sha256": derivedHash(state.P0PolicyMD + mother + reviewMother), "purpose": purpose, "angle": task.Angle, "reviewed": true, "review_response": review.Text})
			if e = s.store.SavePlanAnalysisAsset(ctx, state.TenantID, state.PlanID, "fact_expression_examples", fingerprint, string(saved)); e != nil {
				state.Warn("本次举例已复核，暂未保存到复用库")
			}
		}
	}
	for _, example := range pool.Examples {
		f.ExpressionGuidance = append(f.ExpressionGuidance, map[string]string{"要求": "多个正向举例（仅参考准确说法，按本次目的重组，不是事实）", "内容": example})
	}
	task.FactScope.Facts[0] = f
}
