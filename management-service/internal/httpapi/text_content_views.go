package httpapi

import (
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	"strings"
)

// Enforced before every owned generation/planning call, including offline
// probes. An absent or invalid policy must never become an unconstrained call.
func requireCycleP0(state *anchorcycle.State) error {
	if strings.TrimSpace(state.P0PolicyMD) == "" {
		return fmt.Errorf("本次有效P0缺失，本次不发送模型请求")
	}
	if _, err := contentguard.Compile(state.P0PolicyMD); err != nil {
		return fmt.Errorf("本次有效P0无效，本次不发送模型请求: %w", err)
	}
	return nil
}

// Program provenance remains archived in Task.FactScope. The model receives
// only the content job and human-facing product labels, never its relation keys.
func generationTaskView(task anchorcycle.Task) map[string]any {
	return map[string]any{"卖点": task.SellingPoint, "角度": task.Angle, "观众顾虑": task.PainPoint, "参考人群": task.TargetAudience}
}

// Execution/logging documentation and rule identifiers belong to the program.
// Generation gets all applicable rule content, including contextual exceptions.
// Unrecognized user-authored policy text is preserved, never silently discarded.
func generationP0View(md, scope string) any {
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
		if r.Scope != "all" && r.Scope != scope {
			continue
		}
		rules = append(rules, map[string]any{"类别": r.Category, "适用范围": r.Scope, "适用条件": r.Condition, "处理方式": r.ReviewMode, "相关表述": r.Terms, "匹配表达": r.Patterns, "排除表述": r.ExcludedTerms, "所需依据": r.RequiredBasis, "规则说明": r.Note})
	}
	return map[string]any{"适用规则": rules, "说明": "按条件、范围和排除项理解规则，语境复核类词语不能在任意句子中一律禁用"}
}

// The existing fact renderer encodes operator guidance with reserved delimiters.
// Keep its exact fact text separate from guidance and its example numbers.
func generationFactView(facts []anchorcycle.HookFact) []map[string]any {
	result := []map[string]any{}
	for _, f := range facts {
		data := f.Data
		cut := len(data)
		guides := []map[string]string{}
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
