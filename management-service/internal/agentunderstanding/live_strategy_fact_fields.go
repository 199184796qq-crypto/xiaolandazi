package agentunderstanding

import (
	"regexp"
	"strings"
)

var factWordingFieldPattern = regexp.MustCompile(`(?:不能这么说|不能这样说|不要这样说|禁止说法|禁说内容|可以这么说|改成这样说|建议说法|安全改写)\s*[:：]`)
var factCategoryAddPattern = regexp.MustCompile(`(?:新建|添加|新增)(?:一种分类|一个分类|一类|事实分类|分类)\s*[:：]?\s*[“「"']?([^”」"'，,；;。\n]{1,40})[”」"']?`)

// Structured wording fields must never be swallowed by the legacy fact-value
// parser. Ambiguous prose is handed to the configured model instead of guessed.
func programInterpretFactFields(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	hasBoundary := containsAnyProgram(compact, "不能这么说", "不能这样说", "不要这样说", "禁止说法", "禁说内容", "可以这么说", "改成这样说", "建议说法", "安全改写", "别说", "不要说")
	category := factCategoryAddPattern.FindStringSubmatch(message)
	if len(category) == 2 && !hasBoundary && !containsAnyProgram(compact, "事实名称", "事实内容") && !strings.Contains(message, "添加事实") {
		out := programIntent(KindCommand, "fact_category.add", 0.96)
		out.Changes["new_fact_category"] = map[string]any{"label": strings.TrimSpace(category[1])}
		// Type/description prose needs semantic interpretation, not a guessed label.
		if containsAnyProgram(compact, "商品维度", "履约", "类型", "属于") {
			out.Kind = KindClarify
			out.Confidence = 0.65
			out.Reply = "请确认新分类名称和所属类型。"
		}
		return out, true
	}
	if !hasBoundary && len(category) == 0 {
		return UnifiedIntent{}, false
	}
	unresolved := programIntent(KindClarify, "unknown", 0.6)
	unresolved.Reply = "请明确事实名称，以及要写入的事实内容、不能这么说或可以这么说。"
	indices := factWordingFieldPattern.FindAllStringIndex(message, -1)
	if len(indices) == 0 || len(category) > 0 {
		return unresolved, true
	}
	base := strings.TrimSpace(strings.TrimRight(message[:indices[0][0]], "；;，,\n "))
	action := mutationVerb(compact)
	if action == "" {
		return unresolved, true
	}
	cat, key, value := parseFactTriple(base)
	if key == "" && action == "update" {
		matches := []LiveStrategyFact{}
		for _, fact := range ctx.Facts {
			if strings.Contains(base, fact.Key) {
				matches = append(matches, fact)
			}
		}
		if len(matches) == 1 {
			cat, key = matches[0].Category, matches[0].Key
		}
	}
	if cat == "" || key == "" || (action == "add" && value == "") {
		return unresolved, true
	}
	out := programIntent(KindCommand, "fact."+action, 0.99)
	out.Target["fact_category"], out.Target["fact_key"] = cat, key
	if value != "" {
		out.Changes["fact_value"] = value
	}
	for i, index := range indices {
		end := len(message)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		label := message[index[0]:index[1]]
		field := "forbidden_wording"
		if containsAnyProgram(label, "可以这么说", "改成这样说", "建议说法", "安全改写") {
			field = "safe_rewrite"
		}
		if _, exists := out.Changes[field]; exists {
			return unresolved, true
		}
		content := strings.TrimSpace(strings.TrimRight(message[index[1]:end], "；;\n "))
		if content == "" {
			return unresolved, true
		}
		out.Changes[field] = content
	}
	return out, true
}
