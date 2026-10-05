package decisionexecutor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

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

func (w *Worker) semanticMaterialPrompt(
	ctx context.Context,
	tenantID, planID int64,
	scripts []model.LiveAgentPlanScript,
	item decisionItem,
) string {
	if w == nil || w.semanticService == nil || !w.semanticService.Enabled() || tenantID <= 0 || planID <= 0 {
		return ""
	}
	query := semanticMemoryQuery(item)
	if strings.TrimSpace(query) == "" {
		return ""
	}
	chunks := materialDocuments(tenantID, planID, scripts)
	if len(chunks) == 0 {
		return ""
	}

	documents := make([]semantic.Document, len(chunks))
	for index := range chunks {
		documents[index] = chunks[index].document
	}
	vectors, err := w.semanticService.Resolve(ctx, documents)
	if err != nil || len(vectors) != len(documents) {
		return ""
	}
	queryVectors, err := w.semanticService.EmbedTexts(ctx, []string{query})
	if err != nil || len(queryVectors) != 1 {
		return ""
	}

	ranked := make([]semanticMaterialCandidate, 0, len(chunks))
	for index := range chunks {
		ranked = append(ranked, semanticMaterialCandidate{
			index: index,
			score: semantic.CosineSimilarity(queryVectors[0], vectors[index]),
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].index < ranked[j].index
		}
		return ranked[i].score > ranked[j].score
	})

	var builder strings.Builder
	selected := 0
	totalRunes := 0
	for _, candidate := range ranked {
		if selected >= semanticMaterialTopK || candidate.score < semanticMaterialThreshold {
			break
		}
		chunk := chunks[candidate.index]
		text := strings.TrimSpace(chunk.document.Text)
		if text == "" {
			continue
		}
		runeCount := utf8.RuneCountInString(text)
		if totalRunes+runeCount > semanticMaterialMaxPromptRunes {
			remaining := semanticMaterialMaxPromptRunes - totalRunes
			if remaining <= 80 {
				break
			}
			text = trimContextRunes(text, remaining)
			runeCount = utf8.RuneCountInString(text)
		}
		if selected == 0 {
			builder.WriteString("\n【语义召回的直播素材】")
			builder.WriteString("\n以下内容只作为讲解路径、表达案例和背景素材；涉及价格、库存、活动、规格、物流、承诺等事实时，必须以当前正式事实为准，不得把素材中的历史表述自动当成当前事实。")
		}
		builder.WriteString("\n- ")
		if chunk.title != "" {
			builder.WriteString(chunk.title)
			builder.WriteString("：")
		}
		builder.WriteString(text)
		totalRunes += runeCount
		selected++
	}
	w.semanticService.RecordRetrieval(semantic.ContentTypeMaterialChunk, selected > 0)
	return builder.String()
}
