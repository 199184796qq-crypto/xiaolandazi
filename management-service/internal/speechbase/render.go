// Package speechbase 是三类话术（开场/主线/互动）共享的「外围底座」渲染层。
//
// 这里只放纯函数：输入明确的数据（主播打分基线、P0 母版、事实），输出渲染结果，
// 不依赖任何具体的状态机或业务对象。主线/开场由 anchorcycle.State 提供数据，
// 互动由适配器提供数据，双方都调用这里的同一份实现，避免三套渲染各写各的。
package speechbase

import (
	"math/rand/v2"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/contentguard"
)

// FourLayerNotMentionText 是四层结构「不提」层的统一文案：事实未提供或未确认的
// 未知项正反都不说。主线/开场/互动共用同一句，避免三处同义措辞漂移。
const FourLayerNotMentionText = "事实未提供、未确认的未知项，正反都不说：既不说“能/有/支持”，也不说“不能/没有/不支持”；必要时只说“这点需要帮您确认”。"

// RenderAnchorExpressionView 从主播打分基线 JSON 渲染「主播表达标尺」。
// 主线/开场/互动三类话术共用同一份渲染，避免主播腔调（称呼/语气词/自指/数字习惯）三套各写各的。
func RenderAnchorExpressionView(baselineJSON string) (any, error) {
	b, err := anchorstylescore.Parse(baselineJSON)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"语气词": b.Identity.Particles, "称呼": b.Identity.Audience, "自指": b.Identity.Self,
		"节奏":     map[string]any{"平均分句长度": b.Identity.Rhythm.Length, "长度区间": b.Identity.Rhythm.LengthRange, "问句频率": b.Identity.Rhythm.QuestionFrequency},
		"数字表达习惯": map[string]any{"价格表述方式": b.Identity.Numbers.Style, "自然帮观众算账": b.Identity.Numbers.LikesArithmetic},
		"量化基准":   b.Quant, "表达偏好": b.Forbidden,
	}, nil
}

// Program provenance remains archived in Task.FactScope. The model receives
// only the content job and human-facing product labels, never its relation keys.
func GenerationTaskView(task anchorcycle.Task) map[string]any {
	return map[string]any{"卖点": task.SellingPoint, "角度": task.Angle, "观众顾虑": task.PainPoint, "参考人群": task.TargetAudience}
}

// Execution/logging documentation and rule identifiers belong to the program.
// Generation gets all applicable rule content, including contextual exceptions.
// Unrecognized user-authored policy text is preserved, never silently discarded.
func GenerationP0View(md, scope string) any {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	p, err := contentguard.Compile(md)
	if err != nil {
		return md
	}
	if scope == "" {
		scope = p.Data.Execution.DefaultScope
	}
	rules := []map[string]any{}
	for _, r := range p.Data.Rules {
		if scope != "*" && r.Scope != "all" && r.Scope != scope {
			continue
		}
		rules = append(rules, map[string]any{"类别": r.Category, "适用范围": r.Scope, "适用条件": r.Condition, "处理方式": r.ReviewMode, "相关表述": r.Terms, "匹配表达": r.Patterns, "排除表述": r.ExcludedTerms, "所需依据": r.RequiredBasis, "规则说明": r.Note})
	}
	return map[string]any{"适用规则": rules, "说明": "按条件、范围和排除项理解规则，语境复核类词语不能在任意句子中一律禁用"}
}

// The existing fact renderer encodes operator guidance with reserved delimiters.
// Keep its exact fact text separate from guidance and its example numbers.
func GenerationFactView(facts []anchorcycle.HookFact) []map[string]any {
	result := []map[string]any{}
	for _, f := range facts {
		data := f.Data
		cut := len(data)
		guides := append([]map[string]string{}, f.ExpressionGuidance...)
		for _, marker := range []string{"；禁说=", "；安全改写="} {
			at := strings.Index(data, marker)
			if at < 0 {
				continue
			}
			if at < cut {
				cut = at
			}
			text := data[at+len(marker):]
			end := len(text)
			for _, next := range []string{"；禁说=", "；安全改写="} {
				if i := strings.Index(text, next); i >= 0 && i < end {
					end = i
				}
			}
			guides = append(guides, map[string]string{"要求": strings.Trim(marker, "；="), "内容": text[:end]})
		}
		record := map[string]any{"事实内容": data[:cut], "适用商品链接": f.Products}
		if len(guides) > 0 {
			record["表达指导（不作为商品事实）"] = guides
		}
		result = append(result, record)
	}
	return result
}

// Facts and writing controls occupy separate model-input blocks. This also
// separates guidance from legacy delimiter-rendered facts without editing them.
func GenerationMaterialViews(facts []anchorcycle.HookFact) ([]map[string]any, []map[string]any) {
	content := GenerationFactView(facts)
	guidance := []map[string]any{}
	for _, record := range content {
		if rules, exists := record["表达指导（不作为商品事实）"]; exists {
			if typed, ok := rules.([]map[string]string); ok {
				copy := append([]map[string]string{}, typed...)
				positions := []int{}
				for i, g := range copy {
					if strings.Contains(g["要求"], "正向举例") {
						positions = append(positions, i)
					}
				}
				rand.Shuffle(len(positions), func(i, j int) { a, b := positions[i], positions[j]; copy[a], copy[b] = copy[b], copy[a] })
				rules = copy
			}
			guidance = append(guidance, map[string]any{"对应事实": record["事实内容"], "适用商品链接": record["适用商品链接"], "负向约束及正向举例（不作为新增事实，不直接照念指导）": rules})
			delete(record, "表达指导（不作为商品事实）")
		}
	}
	return content, guidance
}
