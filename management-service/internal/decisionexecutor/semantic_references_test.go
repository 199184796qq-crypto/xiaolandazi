package decisionexecutor

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestSelectRelevantScriptReferencesUsesSemanticRanking(t *testing.T) {
	references := []model.LiveAgentPlanScriptReference{
		{ID: 1, Status: "active", Title: "价格", ContentText: "价格问题先直接回应"},
		{ID: 2, Status: "active", Title: "物流", ContentText: "物流问题按正式事实回答"},
		{ID: 3, Status: "active", Title: "欢迎", ContentText: "欢迎新朋友"},
		{ID: 4, Status: "active", Title: "点赞", ContentText: "感谢点赞"},
		{ID: 5, Status: "active", Title: "库存", ContentText: "库存以实时信息为准"},
		{ID: 6, Status: "active", Title: "售后", ContentText: "售后问题"},
	}
	vectors := [][]float32{
		{1, 0},
		{0.99, 0.01},
		{0.1, 0.9},
		{0.2, 0.8},
		{0.3, 0.7},
		{0.95, 0.05},
		{0.4, 0.6},
	}
	got, err := selectRelevantScriptReferences(context.Background(), fakeMemoryEmbedder{vectors: vectors}, references, "多少钱，有没有库存")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].ID != 1 {
		t.Fatalf("expected price reference first, got=%v", got)
	}
}

func TestScriptReferencePromptMarksReferencesAsNonFact(t *testing.T) {
	prompt := scriptReferencePrompt([]model.LiveAgentPlanScriptReference{{
		Status: "active", Title: "示例", ContentText: "今天69.9",
	}})
	if !strings.Contains(prompt, "必须以当前正式事实为准") {
		t.Fatalf("reference prompt must preserve fact boundary: %s", prompt)
	}
}
