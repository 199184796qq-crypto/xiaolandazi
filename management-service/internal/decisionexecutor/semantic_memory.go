package decisionexecutor

import (
	"fmt"
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
