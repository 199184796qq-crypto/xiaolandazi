package anchorstylescore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const CalibrationVersion = "qualitative-quantitative-v2"

func containsExact(values []string, word string) bool {
	for _, value := range values {
		if value == word {
			return true
		}
	}
	return false
}
func withoutBareLe(values []string) []string {
	result := []string{}
	for _, value := range clean(values) {
		if value != "了" {
			result = append(result, value)
		}
	}
	return result
}
func structuredSelfTerms(values []string) []string {
	result := []string{}
	for _, value := range clean(values) {
		if value != "我" && value != "我们" {
			result = append(result, value)
		}
	}
	return result
}
func ranked(values []Count) []Count {
	result := []Count{}
	for _, value := range values {
		if value.Count > 0 {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Term < result[j].Term
	})
	return result
}
func names(values []Count) []string {
	result := []string{}
	for _, value := range values {
		result = append(result, value.Term)
	}
	return result
}

// ModelFields accepts both the new qualitative-only response and legacy
// responses. Model-supplied numeric sections are discarded before parsing.
func ModelFields(raw string) (Baseline, map[string]map[string]any, map[string]any, error) {
	var b Baseline
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &root) != nil || root == nil {
		return b, nil, nil, fmt.Errorf("模型定性返回必须是JSON对象")
	}
	var identity map[string]map[string]any
	if json.Unmarshal(root["主播识别信息"], &identity) != nil || identity == nil {
		return b, nil, nil, fmt.Errorf("缺少主播识别信息对象")
	}
	var forbidden map[string]any
	if json.Unmarshal(root["禁用词（反推）"], &forbidden) != nil || forbidden == nil {
		return b, nil, nil, fmt.Errorf("缺少禁用词（反推）对象")
	}
	delete(root, "量化基准")
	zero := map[string]any{}
	for _, key := range []string{"每百字_主力语气词", "每百字_辅助语气词1", "每百字_辅助语气词2", "每百字_主力称呼", "每百字_自指词", "每百字_问号", "每百字_感叹号"} {
		zero[key] = 0
	}
	root["量化基准（供打分用）"], _ = json.Marshal(zero)
	// Old model-estimated count maps are not required and cannot prevent reuse.
	if audience := identity["称呼体系"]; audience != nil {
		delete(audience, "各称呼频次")
	}
	root["主播识别信息"], _ = json.Marshal(identity)
	encoded, _ := json.Marshal(root)
	var err error
	b, err = Parse(string(encoded))
	return b, identity, forbidden, err
}

// projectModelFields copies qualitative fields only. Unknown model estimates
// never leak into the final quantitative standard.
func projectModelFields(identity map[string]map[string]any) map[string]map[string]any {
	spec := map[string][]string{
		"语气词系统":  {"各词功能"},
		"称呼体系":   {"称呼使用场景"},
		"自指体系":   {"我们是否=我们家", "禁用自指"},
		"句式节奏":   {"平均分句长度区间", "长句短句搭配", "确认句库", "问句使用频率"},
		"数字比价习惯": {"价格表述方式", "算账句式", "是否爱帮观众算账", "比价锚点举例"},
		"方言口头禅":  {"方言词库", "口头禅"},
		"重复回环习惯": {"高频回环短语", "回环强度"},
		"情绪特征":   {"整体情绪", "情绪判断依据", "情绪高点出现在"},
	}
	result := map[string]map[string]any{}
	for section, fields := range spec {
		result[section] = map[string]any{}
		for _, field := range fields {
			if value, ok := identity[section][field]; ok {
				result[section][field] = value
			}
		}
	}
	if _, ok := result["自指体系"]["我们是否=我们家"]; !ok {
		if legacy, ok := identity["自指体系"]["是否用「我们」替代自指"]; ok {
			result["自指体系"]["我们是否=我们家"] = legacy
		}
	}
	if _, ok := result["句式节奏"]["平均分句长度区间"]; !ok {
		if legacy, ok := identity["句式节奏"]["平均分句长度"]; ok {
			result["句式节奏"]["平均分句长度区间"] = legacy
		}
	}
	return result
}

func candidateParticles(b Baseline, fields map[string]map[string]any) ([]string, error) {
	words := append([]string{}, b.Identity.Particles.Candidates...)
	words = append(words, b.Identity.Particles.Main)
	words = append(words, b.Identity.Particles.Other...)
	if raw, ok := fields["语气词系统"]["各词功能"]; ok {
		encoded, _ := json.Marshal(raw)
		var functions map[string]string
		if json.Unmarshal(encoded, &functions) != nil {
			return nil, fmt.Errorf("各词功能必须是词到说明文字的对象")
		}
		keys := []string{}
		for word := range functions {
			keys = append(keys, word)
		}
		sort.Strings(keys)
		words = append(words, keys...)
	}
	return clean(words), nil
}

// addressAliases merges explicit aliases plus contractions actually present
// outside the full address. Canonical ownership is longest first, then lexical.
func addressAliases(words []string, existing map[string][]string, sample string) ([]string, map[string][]string) {
	words = clean(words)
	sort.Slice(words, func(i, j int) bool {
		if len([]rune(words[i])) != len([]rune(words[j])) {
			return len([]rune(words[i])) > len([]rune(words[j]))
		}
		return words[i] < words[j]
	})
	result := map[string][]string{}
	owned := map[string]bool{}
	canonical := []string{}
	for _, word := range words {
		if owned[word] {
			continue
		}
		canonical = append(canonical, word)
		owned[word] = true
		proposals := append([]string{}, existing[word]...)
		runes := []rune(word)
		withoutFull := strings.ReplaceAll(sample, word, "")
		for i := 1; i < len(runes); i++ {
			if runes[i] == runes[i-1] {
				alias := string(runes[:i]) + string(runes[i+1:])
				if strings.Contains(withoutFull, alias) {
					proposals = append(proposals, alias)
				}
			}
		}
		proposals = clean(proposals)
		sort.Strings(proposals)
		for _, alias := range proposals {
			if !owned[alias] && strings.Contains(sample, alias) {
				result[word] = append(result[word], alias)
				owned[alias] = true
			}
		}
	}
	return canonical, result
}

func Calibrate(raw, sample string, includeLe bool) (string, error) {
	b, modelFields, forbidden, err := ModelFields(raw)
	if err != nil {
		return "", err
	}
	particles, err := candidateParticles(b, modelFields)
	if err != nil {
		return "", err
	}
	if includeLe {
		particles = clean(append(particles, "了"))
	} else {
		particles = withoutBareLe(particles)
	}
	b.Identity.Particles.Main = ""
	b.Identity.Particles.Other = particles
	b.Quant.IncludeLe = includeLe
	addresses := append([]string{}, b.Identity.Audience.Candidates...)
	addresses = append(addresses, b.Identity.Audience.Main)
	addresses = append(addresses, b.Identity.Audience.Other...)
	addresses, b.Identity.Audience.Aliases = addressAliases(addresses, b.Identity.Audience.Aliases, sample)
	b.Identity.Audience.Main = ""
	b.Identity.Audience.Other = addresses
	b.Identity.Self.Terms = structuredSelfTerms(b.Identity.Self.Terms)
	first := Measure(sample, b)
	if first.Chars == 0 {
		return "", fmt.Errorf("原话术全文不能为空")
	}
	particleCounts := ranked(first.Particles)
	addressCounts := ranked(first.AudienceGroups)
	b.Identity.Particles.Main = ""
	b.Identity.Particles.Other = []string{}
	if len(particleCounts) > 0 {
		b.Identity.Particles.Main = particleCounts[0].Term
		b.Identity.Particles.Other = names(particleCounts[1:])
	}
	b.Identity.Audience.Main = ""
	b.Identity.Audience.Other = []string{}
	if len(addressCounts) > 0 {
		b.Identity.Audience.Main = addressCounts[0].Term
		b.Identity.Audience.Other = names(addressCounts[1:])
	}
	// Only self-reference words present in the full sample enter the final ruler.
	selfCounts := ranked(first.Self)
	b.Identity.Self.Terms = names(selfCounts)
	s := Measure(sample, b)
	identity := projectModelFields(modelFields)
	identity["语气词系统"]["主力语气词"] = b.Identity.Particles.Main
	identity["语气词系统"]["辅助语气词"] = b.Identity.Particles.Other
	identity["语气词系统"]["候选语气词"] = names(particleCounts)
	identity["称呼体系"]["主力称呼"] = b.Identity.Audience.Main
	identity["称呼体系"]["辅助称呼"] = b.Identity.Audience.Other
	identity["称呼体系"]["候选称呼"] = names(addressCounts)
	identity["称呼体系"]["称呼别名"] = b.Identity.Audience.Aliases
	identity["自指体系"]["自指词"] = b.Identity.Self.Terms
	quant := map[string]any{
		"样本总字数_N":   s.Chars,
		"每百字_主力语气词": 0.0, "每百字_辅助语气词": 0.0,
		"每百字_辅助语气词1": 0.0, "每百字_辅助语气词2": 0.0,
		"每百字_主力称呼": round(density(s.MainAudienceCount, s.Chars)),
		"每百字_自指词":  round(density(s.SelfCount, s.Chars)),
		"每百字_问号":   round(density(s.QuestionMarks, s.Chars)),
		"每百字_问句":   round(density(s.Questions, s.Chars)),
		"每百字_感叹号":  round(density(s.Exclamations, s.Chars)),
		"语气词逐词实测":  particleCounts, "称呼逐组实测": addressCounts,
		"自指逐词实测": s.Self, "自指排除项实测": s.SelfExcluded,
		"主次排序": names(particleCounts), "称呼主次排序": names(addressCounts),
		"了_单列次数": s.LeCount, "了_计入语气词": includeLe,
		"平均分句长度实测": s.AverageClauseLength,
	}
	ratioNames, ratioCounts := []string{}, []string{}
	others := 0
	for i, v := range particleCounts {
		ratioNames = append(ratioNames, v.Term)
		ratioCounts = append(ratioCounts, fmt.Sprint(v.Count))
		if i == 0 {
			quant["每百字_主力语气词"] = v.Per100
		} else {
			others += v.Count
		}
		if i == 1 {
			quant["每百字_辅助语气词1"] = v.Per100
		}
		if i == 2 {
			quant["每百字_辅助语气词2"] = v.Per100
		}
	}
	quant["每百字_辅助语气词"] = round(density(others, s.Chars))
	ratio := ""
	if len(particleCounts) > 0 {
		ratio = strings.Join(ratioNames, ":") + " = " + strings.Join(ratioCounts, ":")
	}
	quant["语气词配比"] = ratio
	root := map[string]any{
		"主播识别信息": identity, "量化基准": quant, "禁用词（反推）": forbidden,
		"样本统计校验": map[string]any{"统计口径": CountingMethod, "实测": s, "校对版本": CalibrationVersion, "量化来源": "程序对原话术全文实测", "主次排序来源": "实测次数降序；并列按词典序", "模型估数处理": "不采纳模型的次数、频次、比例和主次判断"},
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	if _, err = Parse(string(encoded)); err != nil {
		return "", err
	}
	return string(encoded), nil
}
