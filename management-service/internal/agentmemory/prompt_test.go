package agentmemory

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPromptIncludesWordingHardCheckTerms(t *testing.T) {
	items := []model.AgentMemoryItem{
		{
			MemoryType: model.AgentMemoryTypeWording,
			Target:     "菜籽油使用表达",
			CurrentVersion: &model.AgentMemoryVersion{
				ContentText: "菜籽油不要说喝，优先说炒菜、做饭。",
				Structured: map[string]any{
					"avoid":  []any{"喝", "喝起来"},
					"prefer": []any{"炒菜", "做饭"},
				},
			},
		},
	}
	prompt := Prompt(items)
	if !strings.Contains(prompt, "【用词硬校验词】喝｜喝起来") {
		t.Fatalf("prompt missing hard wording terms: %s", prompt)
	}
	if !strings.Contains(prompt, "菜籽油不要说喝") {
		t.Fatalf("prompt missing wording memory: %s", prompt)
	}
}

func TestPromptMarksAdoptedMemoryAsMandatory(t *testing.T) {
	items := []model.AgentMemoryItem{{
		MemoryType: model.AgentMemoryTypeFact,
		Target:     "所在地",
		CurrentVersion: &model.AgentMemoryVersion{
			ContentText: "所在地是绵阳山河。",
		},
	}}
	prompt := Prompt(items)
	if !strings.Contains(prompt, "已经采用并正在生效") || !strings.Contains(prompt, "不得遗漏") {
		t.Fatalf("prompt missing mandatory adopted-memory instruction: %s", prompt)
	}
}

func TestSemanticMemoryIsContextualEvidence(t *testing.T) {
	items := []model.AgentMemoryItem{
		{
			MemoryType: model.AgentMemoryTypeSemantic,
			Target:     "娃娃",
			CurrentVersion: &model.AgentMemoryVersion{
				ContentText: "娃娃可能指小孩，也可能指玩具；必须结合当前商品和上下文判断。",
			},
		},
	}
	prompt := Prompt(items)
	if !strings.Contains(prompt, "语义记忆是上下文证据，不是无条件字符串替换") {
		t.Fatalf("semantic contextual guard missing: %s", prompt)
	}
}
