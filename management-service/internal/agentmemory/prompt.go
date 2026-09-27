package agentmemory

import (
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

var orderedTypes = []string{
	model.AgentMemoryTypeSemantic,
	model.AgentMemoryTypeFact,
	model.AgentMemoryTypeWording,
	model.AgentMemoryTypeStyle,
}

func TypeLabel(memoryType string) string {
	switch memoryType {
	case model.AgentMemoryTypeSemantic:
		return "语义记忆"
	case model.AgentMemoryTypeFact:
		return "事实记忆"
	case model.AgentMemoryTypeWording:
		return "用词规范"
	case model.AgentMemoryTypeStyle:
		return "主播风格"
	default:
		return memoryType
	}
}

func stringList(value any) []string {
	result := make([]string, 0)
	switch typed := value.(type) {
	case []string:
		result = append(result, typed...)
	case []any:
		for _, item := range typed {
			result = append(result, fmt.Sprint(item))
		}
	case string:
		result = append(result, strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '|'
		})...)
	}
	cleaned := make([]string, 0, len(result))
	seen := map[string]struct{}{}
	for _, item := range result {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		cleaned = append(cleaned, item)
	}
	return cleaned
}

func WordingAvoidTerms(items []model.AgentMemoryItem) []string {
	result := make([]string, 0)
	for _, item := range items {
		if item.MemoryType != model.AgentMemoryTypeWording || item.CurrentVersion == nil {
			continue
		}
		for _, key := range []string{"avoid", "forbidden", "avoid_terms", "blocked_terms"} {
			result = append(result, stringList(item.CurrentVersion.Structured[key])...)
		}
	}
	return stringList(result)
}

func Prompt(items []model.AgentMemoryItem) string {
	if len(items) == 0 {
		return ""
	}
	byType := make(map[string][]model.AgentMemoryItem, 4)
	for _, item := range items {
		if item.CurrentVersion == nil || strings.TrimSpace(item.CurrentVersion.ContentText) == "" {
			continue
		}
		byType[item.MemoryType] = append(byType[item.MemoryType], item)
	}
	var builder strings.Builder
	builder.WriteString("【当前直播间智能体记忆】\n")
	builder.WriteString("以下均为当前直播间已经采用并正在生效的记忆。与当前问题相关的条目必须执行，不得遗漏；与当前问题无关的条目不要生拉硬套。必须按以下顺序使用：先结合上下文理解语义，再使用确认事实，再遵守用词规范，再经过合规约束，最后应用主播风格。事实内容不得被风格改写成不同事实；语义记忆是上下文证据，不是无条件字符串替换。\n")
	if avoidTerms := WordingAvoidTerms(items); len(avoidTerms) > 0 {
		builder.WriteString("【用词硬校验词】" + strings.Join(avoidTerms, "｜") + "\n")
	}
	index := 1
	for _, memoryType := range orderedTypes {
		group := byType[memoryType]
		if len(group) == 0 {
			continue
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n", index, TypeLabel(memoryType)))
		for _, item := range group {
			builder.WriteString("- ")
			if target := strings.TrimSpace(item.Target); target != "" {
				builder.WriteString(target)
				builder.WriteString("：")
			}
			builder.WriteString(strings.TrimSpace(item.CurrentVersion.ContentText))
			builder.WriteString("\n")
		}
		index++
	}
	return strings.TrimSpace(builder.String())
}
