package decisionexecutor

import (
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
)

const (
	semanticMaterialChunkRunes   = 700
	semanticMaterialChunkOverlap = 80
	semanticMaterialMaxChunks    = 120
	semanticMaterialTopK         = 6
	// Query-to-long-chunk similarity is naturally lower than sentence-to-sentence similarity.
	semanticMaterialThreshold      = 0.32
	semanticMaterialMaxPromptRunes = 3200
)

type semanticMaterialChunk struct {
	document semantic.Document
	title    string
}

type semanticMaterialCandidate struct {
	index int
	score float64
}

func splitMaterialText(text string, chunkRunes, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkRunes <= 0 {
		chunkRunes = semanticMaterialChunkRunes
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkRunes {
		overlap = chunkRunes / 5
	}

	runes := []rune(text)
	if len(runes) <= chunkRunes {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)/chunkRunes)+1)
	start := 0
	for start < len(runes) {
		end := start + chunkRunes
		if end > len(runes) {
			end = len(runes)
		} else {
			searchStart := start + chunkRunes/2
			best := -1
			for index := end - 1; index >= searchStart; index-- {
				switch runes[index] {
				case '\n', '。', '！', '？', ';', '；':
					best = index + 1
				}
				if best > 0 {
					break
				}
			}
			if best > start {
				end = best
			}
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func materialSemanticText(script model.LiveAgentPlanScript) string {
	text := strings.TrimSpace(script.ReadableText)
	if text == "" {
		text = strings.TrimSpace(script.RawText)
	}
	return text
}

func materialDocuments(tenantID, planID int64, scripts []model.LiveAgentPlanScript) []semanticMaterialChunk {
	chunks := make([]semanticMaterialChunk, 0)
	for _, script := range scripts {
		if !strings.EqualFold(strings.TrimSpace(script.Status), "active") {
			continue
		}
		text := materialSemanticText(script)
		if text == "" {
			continue
		}
		version := script.UpdatedAt.UTC().UnixMilli()
		if version <= 0 {
			version = 1
		}
		for index, chunk := range splitMaterialText(text, semanticMaterialChunkRunes, semanticMaterialChunkOverlap) {
			if len(chunks) >= semanticMaterialMaxChunks {
				return chunks
			}
			chunks = append(chunks, semanticMaterialChunk{
				title: strings.TrimSpace(script.Title),
				document: semantic.Document{
					TenantID:      tenantID,
					PlanID:        planID,
					ContentType:   semantic.ContentTypeMaterialChunk,
					SourceID:      fmt.Sprintf("script:%d:chunk:%d", script.ID, index),
					SourceVersion: version,
					Status:        "active",
					Text:          chunk,
				},
			})
		}
	}
	return chunks
}

// MaterialDocuments returns the complete current chunk set for one plan.
func MaterialDocuments(tenantID, planID int64, scripts []model.LiveAgentPlanScript) []semantic.Document {
	chunks := materialDocuments(tenantID, planID, scripts)
	documents := make([]semantic.Document, len(chunks))
	for index, chunk := range chunks {
		documents[index] = chunk.document
	}
	return documents
}
