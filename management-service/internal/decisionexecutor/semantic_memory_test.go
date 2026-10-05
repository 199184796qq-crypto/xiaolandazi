package decisionexecutor

import (
	"context"
	"testing"

	"livecompanion/management/internal/model"
)

type fakeMemoryEmbedder struct {
	vectors [][]float32
	err     error
}

func (f fakeMemoryEmbedder) Enabled() bool { return true }
func (f fakeMemoryEmbedder) Model() string { return "fake" }
func (f fakeMemoryEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return f.vectors, f.err
}

func memoryItem(id int64, memoryType, content string) model.AgentMemoryItem {
	return model.AgentMemoryItem{
		ID:         id,
		MemoryType: memoryType,
		Target:     content,
		CurrentVersion: &model.AgentMemoryVersion{
			ContentText: content,
		},
	}
}

func TestSelectRelevantMemoriesAlwaysKeepsFactsAndRanksOtherMemory(t *testing.T) {
	memories := []model.AgentMemoryItem{
		memoryItem(1, model.AgentMemoryTypeFact, "默认发顺丰"),
		memoryItem(2, model.AgentMemoryTypeSemantic, "物流问题先确认事实"),
		memoryItem(3, model.AgentMemoryTypeWording, "不要说喝"),
		memoryItem(4, model.AgentMemoryTypeStyle, "多用短句"),
		memoryItem(5, model.AgentMemoryTypeSemantic, "新人欢迎"),
		memoryItem(6, model.AgentMemoryTypeSemantic, "点赞感谢"),
		memoryItem(7, model.AgentMemoryTypeSemantic, "库存问题"),
		memoryItem(8, model.AgentMemoryTypeSemantic, "售后问题"),
		memoryItem(9, model.AgentMemoryTypeSemantic, "价格问题"),
		memoryItem(10, model.AgentMemoryTypeSemantic, "产地问题"),
		memoryItem(11, model.AgentMemoryTypeSemantic, "规格问题"),
	}
	// query vector + ten non-fact vectors. IDs 2/9/7 are strongest.
	vectors := [][]float32{{1, 0}}
	for _, score := range []float32{0.99, 0.1, 0.2, 0.3, 0.4, 0.80, 0.5, 0.95, 0.6, 0.7} {
		vectors = append(vectors, []float32{score, 1 - score})
	}
	got, err := selectRelevantMemories(context.Background(), fakeMemoryEmbedder{vectors: vectors}, memories, "多少钱，有库存吗，发什么快递")
	if err != nil {
		t.Fatal(err)
	}
	foundFact := false
	foundStrong := false
	for _, item := range got {
		if item.ID == 1 {
			foundFact = true
		}
		if item.ID == 2 || item.ID == 9 {
			foundStrong = true
		}
	}
	if !foundFact {
		t.Fatal("formal fact memory must always be retained")
	}
	if !foundStrong {
		t.Fatalf("expected semantically strong memories, got=%v", got)
	}
	if len(got) >= len(memories) {
		t.Fatalf("semantic retrieval should reduce non-fact prompt context: got=%d all=%d", len(got), len(memories))
	}
}

func TestSelectRelevantMemoriesFallsBackWhenDisabledBySmallSet(t *testing.T) {
	memories := []model.AgentMemoryItem{
		memoryItem(1, model.AgentMemoryTypeFact, "默认发顺丰"),
		memoryItem(2, model.AgentMemoryTypeSemantic, "物流回答"),
	}
	got, err := selectRelevantMemories(context.Background(), nil, memories, "快递")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(memories) {
		t.Fatalf("disabled semantic retrieval must preserve all memories")
	}
}
