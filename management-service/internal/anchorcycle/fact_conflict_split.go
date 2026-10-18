package anchorcycle

import (
	"regexp"
	"strconv"
	"strings"
)

// 事实矛盾拆分：把「同一卖点×角度」喂给模型的一组事实，按「互不冲突」拆成若干组，
// 让模型一段只拿一组、其余留待后续段单独展开，避免把互相矛盾/不同维度的信息揉进
// 同一段话（例如「6人餐」与「适合3-4人」同时讲、价格与口味硬塞一段）。
//
// 纯规则、零模型成本，不依赖保存时的语义矛盾检测结果，也不阻塞生成。

// 数字区间：匹配「6人」「3-4人」「95.6元」「500g」等整数/小数及其后随单位汉字。
var factNumberRange = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?\s*(?:-[0-9]+(?:\.[0-9]+)?)?\s*[人元斤克公斤克gGmlML毫升片个盒袋包瓶]?`)

// extractNumbers 提取事实文本中的数值（含「3-4」这样的区间端点）。
// 只取与「人数/价格/份量/规格」相关的可比较数字，供同类别事实间判定是否对立。
func extractNumbers(text string) []float64 {
	// 剥离禁说/安全改写等表达引导，只对事实正文判数字。
	body := text
	for _, marker := range []string{"；禁说=", ";禁说=", "；安全改写=", ";安全改写="} {
		if at := strings.Index(body, marker); at >= 0 {
			body = body[:at]
		}
	}
	out := []float64{}
	for _, m := range factNumberRange.FindAllString(body, -1) {
		// 区间「3-4」拆成两个端点。
		seg := m
		seg = strings.TrimSpace(seg)
		// 去掉结尾单位汉字/字母，只留数字与可能的连字符。
		seg = strings.TrimRightFunc(seg, func(r rune) bool {
			return r > 127 || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		})
		seg = strings.TrimSpace(seg)
		if at := strings.Index(seg, "-"); at > 0 {
			for _, part := range strings.Split(seg, "-") {
				if v, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil {
					out = append(out, v)
				}
			}
			continue
		}
		if v, err := strconv.ParseFloat(seg, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// numbersConflict 判断两组数值是否「互相否定」。
// 区间包含/相邻（6 与 3、4）不算冲突；只有一组明确是一个集合、另一组落在
// 该集合之外且明显更小/更大（如 6 vs 3-4）才算对立，用于提示拆开讲。
func numbersConflict(a, b []float64) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	// 取各组「代表值」：区间取中点，单值取自身。
	rep := func(ns []float64) float64 {
		min, max := ns[0], ns[0]
		for _, v := range ns {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		return (min + max) / 2
	}
	ra, rb := rep(a), rep(b)
	if ra == 0 || rb == 0 {
		return false
	}
	// 一组是另一组的两倍以上或一半以下，视为明显对立（数量级差异）。
	// 6 vs 3-4（代表 3.5）→ 6/3.5 ≈ 1.7，不到 2，但「6」是固定值、「3-4」是区间，
	// 且区间上界 4 < 6，属于「规格对不上」，这里用「一方上界 < 另一方固定值」判定。
	maxA, minA := a[0], a[0]
	for _, v := range a {
		if v > maxA {
			maxA = v
		}
		if v < minA {
			minA = v
		}
	}
	maxB, minB := b[0], b[0]
	for _, v := range b {
		if v > maxB {
			maxB = v
		}
		if v < minB {
			minB = v
		}
	}
	// 完全分离：一组的区间完全在另一组之外。
	if maxA < minB || maxB < minA {
		return true
	}
	// 数量级悬殊。
	return ra >= rb*2 || rb >= ra*2
}

// SplitConflictingFacts 把一组事实按「互不冲突」拆成若干组。
//
// 规则：
//   1. 空或单条 → 不拆，直接返回。
//   2. 先按分类(category)聚合：不同分类分属不同维度（价格/口味/人数/工艺…），
//      一个角度绑了多个分类的事实，正是「多维度信息揉进一段」的来源，拆开各成一组。
//      无分类（通用事实）单独一组，不与任何分类混。
//   3. 同分类组内，若两条事实的数值互相否定（numbersConflict），再拆成不同组。
//
// 返回顺序稳定：按首次出现顺序，通用事实组放最后（它可伴随任意组）。
func SplitConflictingFacts(facts []HookFact) [][]HookFact {
	if len(facts) <= 1 {
		return [][]HookFact{append([]HookFact(nil), facts...)}
	}
	// 分类保序分组。
	type bucket struct {
		category string
		facts    []HookFact
	}
	var buckets []*bucket
	generic := []HookFact{}
	idx := map[string]*bucket{}
	for _, f := range facts {
		cat := strings.ToLower(strings.TrimSpace(CategoryFromFactText(f.Data)))
		if cat == "" {
			generic = append(generic, f)
			continue
		}
		b, ok := idx[cat]
		if !ok {
			b = &bucket{category: cat}
			idx[cat] = b
			buckets = append(buckets, b)
		}
		b.facts = append(b.facts, f)
	}
	// 组内数值对立再拆。
	var groups [][]HookFact
	for _, b := range buckets {
		sub := splitByNumberConflict(b.facts)
		groups = append(groups, sub...)
	}
	if len(generic) > 0 {
		groups = append(groups, generic)
	}
	if len(groups) == 0 {
		return [][]HookFact{append([]HookFact(nil), facts...)}
	}
	return groups
}

// FissionFactConflictHint 返回「当前侧面素材存在多维度/矛盾事实」时的生成约束提示。
//
// 当同一卖点×角度、当前切入侧面下的专属事实可分拆成多组（不同分类维度，或同分类
// 数值互相否定）时，本段若把这些信息一股脑揉进一段，容易自相矛盾（例如同时讲
// 「6人餐」与「适合3-4人」）。此提示告诉口播模型：本段只围绕其中一条主线维度展开，
// 其余维度（列出其分类）留待后续段单独讲，不要混在一段里互相打架。
//
// 无冲突（只一组）时返回空串，不改变现有生成行为。
func (s *State) FissionFactConflictHint() string {
	sideFacts := s.HookFactsForSide()
	groups := SplitConflictingFacts(sideFacts)
	if len(groups) <= 1 {
		return ""
	}
	// 第一组为本段主线，其余组的分类作为「留待后续」提示。
	var pending []string
	for _, g := range groups[1:] {
		for _, f := range g {
			cat := strings.ToLower(strings.TrimSpace(CategoryFromFactText(f.Data)))
			if cat != "" && !contains(pending, cat) {
				pending = append(pending, cat)
			}
		}
	}
	if len(pending) == 0 {
		return ""
	}
	return "本次「可说」事实分属多个不同维度，本段只围绕其中一条主线展开，其余维度（" +
		strings.Join(pending, "、") + "）留待后续段单独讲，不要混在一段里互相矛盾。"
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// splitByNumberConflict 在同分类事实内，把数值互相否定的拆到不同组。
// 无冲突或仅一条时原样返回。
func splitByNumberConflict(facts []HookFact) [][]HookFact {
	if len(facts) <= 1 {
		return [][]HookFact{append([]HookFact(nil), facts...)}
	}
	groups := [][]HookFact{}
	used := map[int]bool{}
	for i := 0; i < len(facts); i++ {
		if used[i] {
			continue
		}
		group := []HookFact{facts[i]}
		used[i] = true
		ni := extractNumbers(facts[i].Data)
		for j := i + 1; j < len(facts); j++ {
			if used[j] {
				continue
			}
			nj := extractNumbers(facts[j].Data)
			if numbersConflict(ni, nj) {
				continue // 数值对立，不进同组
			}
			group = append(group, facts[j])
			used[j] = true
			ni = append(ni, nj...)
		}
		groups = append(groups, group)
	}
	if len(groups) == 1 {
		return [][]HookFact{append([]HookFact(nil), facts...)}
	}
	return groups
}
