package decisionexecutor

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
)

const (
	semanticMemoryTopK = 8
	// Calibrated against live qwen3.7-text-embedding-flash Chinese retrieval samples.
	semanticMemoryThreshold = 0.42
	semanticMemoryMinKeep   = 3
)

type semanticMemoryCandidate struct {
	index int
	score float64
}

// CorrectionDocuments mirrors the active, non-fact memory sources used by
// runtime recall. It is shared with the offline index maintenance command.
func CorrectionDocuments(tenantID, roomID int64, memories []model.AgentMemoryItem) []semantic.Document {
	documents := make([]semantic.Document, 0, len(memories))
	for _, memory := range memories {
		if memory.MemoryType == model.AgentMemoryTypeFact || memory.CurrentVersion == nil ||
			!strings.EqualFold(strings.TrimSpace(memory.Status), "active") {
			continue
		}
		content := strings.TrimSpace(memory.CurrentVersion.ContentText)
		if content == "" {
			continue
		}
		documents = append(documents, semantic.Document{
			TenantID: tenantID, RoomID: roomID,
			ContentType:   semantic.ContentTypeCorrection,
			SourceID:      fmt.Sprintf("memory:%d", memory.ID),
			SourceVersion: int64(memory.CurrentVersion.VersionNo),
			Text:          strings.TrimSpace(memory.Target) + "\n" + content,
		})
	}
	return documents
}

func semanticMemoryQuery(item decisionItem) string {
	parts := make([]string, 0, len(item.SampleQuestions)+4)
	for _, question := range item.SampleQuestions {
		if value := strings.TrimSpace(question); value != "" {
			parts = append(parts, value)
		}
	}
	for _, value := range []string{item.Topic, item.Title, item.Summary, item.ReplyHint} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n")
}

func selectRelevantMemoriesWithService(
	ctx context.Context,
	service *semantic.Service,
	memories []model.AgentMemoryItem,
	query string,
	tenantID, roomID int64,
) ([]model.AgentMemoryItem, error) {
	query = strings.TrimSpace(query)
	if service == nil || !service.Enabled() || query == "" {
		return memories, nil
	}

	candidateIndexes := make([]int, 0, len(memories))
	documents := make([]semantic.Document, 0, len(memories))
	for index, memory := range memories {
		if memory.MemoryType == model.AgentMemoryTypeFact || memory.CurrentVersion == nil {
			continue
		}
		content := strings.TrimSpace(memory.CurrentVersion.ContentText)
		if content == "" {
			continue
		}
		candidateIndexes = append(candidateIndexes, index)
		documents = append(documents, semantic.Document{
			TenantID:      tenantID,
			RoomID:        roomID,
			ContentType:   semantic.ContentTypeCorrection,
			SourceID:      fmt.Sprintf("memory:%d", memory.ID),
			SourceVersion: int64(memory.CurrentVersion.VersionNo),
			Text:          strings.TrimSpace(memory.Target) + "\n" + content,
		})
	}
	if len(candidateIndexes) <= semanticMemoryTopK {
		if _, err := service.Resolve(ctx, documents); err != nil {
			return nil, err
		}
		service.RecordRetrieval(semantic.ContentTypeCorrection, len(candidateIndexes) > 0)
		return memories, nil
	}

	vectors, err := service.Resolve(ctx, documents)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(documents) {
		return nil, fmt.Errorf("semantic memory vectors=%d want=%d", len(vectors), len(documents))
	}
	queryVectors, err := service.EmbedTexts(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(queryVectors) != 1 {
		return nil, fmt.Errorf("semantic memory query vectors=%d want=1", len(queryVectors))
	}

	ranked := make([]semanticMemoryCandidate, 0, len(candidateIndexes))
	for vectorIndex, memoryIndex := range candidateIndexes {
		ranked = append(ranked, semanticMemoryCandidate{
			index: memoryIndex,
			score: semantic.CosineSimilarity(queryVectors[0], vectors[vectorIndex]),
		})
	}
	selected := selectRankedMemories(memories, ranked)
	service.RecordRetrieval(semantic.ContentTypeCorrection, len(selected) > 0)
	return selected, nil
}

func selectRankedMemories(memories []model.AgentMemoryItem, ranked []semanticMemoryCandidate) []model.AgentMemoryItem {
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].index < ranked[j].index
		}
		return ranked[i].score > ranked[j].score
	})

	selected := make(map[int]struct{}, semanticMemoryTopK)
	for _, candidate := range ranked {
		if len(selected) >= semanticMemoryTopK {
			break
		}
		if candidate.score >= semanticMemoryThreshold || len(selected) < semanticMemoryMinKeep {
			selected[candidate.index] = struct{}{}
		}
	}

	result := make([]model.AgentMemoryItem, 0, len(selected)+4)
	for index, memory := range memories {
		if memory.MemoryType == model.AgentMemoryTypeFact {
			result = append(result, memory)
			continue
		}
		if _, ok := selected[index]; ok {
			result = append(result, memory)
		}
	}
	if len(result) == 0 {
		return memories
	}
	return result
}

func selectRelevantMemories(
	ctx context.Context,
	embedder semantic.Embedder,
	memories []model.AgentMemoryItem,
	query string,
) ([]model.AgentMemoryItem, error) {
	query = strings.TrimSpace(query)
	if embedder == nil || !embedder.Enabled() || query == "" || len(memories) <= semanticMemoryTopK {
		return memories, nil
	}

	candidateIndexes := make([]int, 0, len(memories))
	texts := []string{query}
	for index, memory := range memories {
		if memory.MemoryType == model.AgentMemoryTypeFact {
			continue
		}
		if memory.CurrentVersion == nil {
			continue
		}
		content := strings.TrimSpace(memory.CurrentVersion.ContentText)
		if content == "" {
			continue
		}
		candidateIndexes = append(candidateIndexes, index)
		texts = append(texts, strings.TrimSpace(memory.Target)+"\n"+content)
	}
	if len(candidateIndexes) <= semanticMemoryTopK {
		return memories, nil
	}

	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("semantic memory vectors=%d want=%d", len(vectors), len(texts))
	}

	ranked := make([]semanticMemoryCandidate, 0, len(candidateIndexes))
	for vectorIndex, memoryIndex := range candidateIndexes {
		ranked = append(ranked, semanticMemoryCandidate{
			index: memoryIndex,
			score: semantic.CosineSimilarity(vectors[0], vectors[vectorIndex+1]),
		})
	}
	return selectRankedMemories(memories, ranked), nil
}
