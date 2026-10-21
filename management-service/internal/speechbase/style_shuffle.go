package speechbase

import (
	"math/rand"
	"strings"
)

// ShuffleStyleMDForPrompt 打乱风格母版里同类多选词的出现顺序。
// 目的：同一组称呼/自称/口头禅每次送模型时顺序不同，避免模型总挑第一个。
// 只改同一类词内部的顺序，不改词条、分类、说明和其他章节。
func ShuffleStyleMDForPrompt(styleMD string) string {
	if strings.TrimSpace(styleMD) == "" {
		return styleMD
	}
	lines := strings.Split(styleMD, "\n")
	groups := map[string][]int{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		tick := strings.Index(trimmed, "`")
		if tick < 0 {
			continue
		}
		// 稳定原词行形如：- 观众称呼 `朋友们`：...
		// 同类行归到同一组，只打乱这些行，不跨类别移动。
		key := trimmed[:tick]
		groups[key] = append(groups[key], i)
	}
	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		values := make([]string, len(indexes))
		for i, index := range indexes {
			values[i] = lines[index]
		}
		rand.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
		for i, index := range indexes {
			lines[index] = values[i]
		}
	}
	// 称呼覆盖行形如：- 观众/粉丝称呼：替换... `朋友们`、`老乡们`。
	// 一行内有多个反引号词时，词条顺序也打乱。
	for i, line := range lines {
		if strings.Count(line, "`") >= 4 {
			lines[i] = shuffleBacktickTerms(line)
		}
	}
	return strings.Join(lines, "\n")
}

func shuffleBacktickTerms(line string) string {
	parts := strings.Split(line, "`")
	terms := make([]string, 0, len(parts)/2)
	for i := 1; i < len(parts); i += 2 {
		terms = append(terms, parts[i])
	}
	if len(terms) < 2 {
		return line
	}
	rand.Shuffle(len(terms), func(i, j int) { terms[i], terms[j] = terms[j], terms[i] })
	for i := 1; i < len(parts); i += 2 {
		parts[i] = terms[(i-1)/2]
	}
	return strings.Join(parts, "`")
}
