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
	semanticReferenceTopK = 5
	// Short Chinese question -> richer answer references typically score around 0.5-0.7.
	semanticReferenceThreshold = 0.43
)

type scriptReferenceStore interface {
	ListLiveAgentPlanScriptReferences(context.Context, int64, int64) ([]model.LiveAgentPlanScriptReference, error)
}

type semanticReferenceCandidate struct {
	index int
	score float64
}

// ReferenceDocuments indexes only currently adopted reference answers.
func ReferenceDocuments(tenantID, planID int64, references []model.LiveAgentPlanScriptReference) []semantic.Document {
	documents := make([]semantic.Document, 0, len(references))
	for _, reference := range references {
		if !strings.EqualFold(strings.TrimSpace(reference.Status), "active") || strings.TrimSpace(reference.ContentText) == "" {
			continue
		}
		documents = append(documents, semantic.Document{
			TenantID: tenantID, PlanID: planID,
			ContentType:   semantic.ContentTypeReferenceAnswer,
			SourceID:      fmt.Sprintf("reference:%d", reference.ID),
			SourceVersion: reference.VersionNo,
			Text:          strings.Join([]string{strings.TrimSpace(reference.Title), strings.TrimSpace(reference.Goal), strings.TrimSpace(reference.ContentText)}, "\n"),
		})
	}
	return documents
}

func selectRelevantScriptReferencesWithService(
	ctx context.Context,
	service *semantic.Service,
	references []model.LiveAgentPlanScriptReference,
	query string,
	tenantID, planID int64,
) ([]model.LiveAgentPlanScriptReference, error) {
	query = strings.TrimSpace(query)
	if service == nil || !service.Enabled() || query == "" || len(references) == 0 {
		return nil, nil
	}
	active := make([]model.LiveAgentPlanScriptReference, 0, len(references))
	documents := make([]semantic.Document, 0, len(references))
	for _, reference := range references {
		if !strings.EqualFold(strings.TrimSpace(reference.Status), "active") {
			continue
		}
		content := strings.TrimSpace(reference.ContentText)
		if content == "" {
			continue
		}
		active = append(active, reference)
		documents = append(documents, semantic.Document{
			TenantID:      tenantID,
			PlanID:        planID,
			ContentType:   semantic.ContentTypeReferenceAnswer,
			SourceID:      fmt.Sprintf("reference:%d", reference.ID),
			SourceVersion: reference.VersionNo,
			Text: strings.Join([]string{
				strings.TrimSpace(reference.Title),
				strings.TrimSpace(reference.Goal),
				content,
			}, "\n"),
		})
	}
	if len(active) == 0 {
		service.RecordRetrieval(semantic.ContentTypeReferenceAnswer, false)
		return nil, nil
	}
	if len(active) <= semanticReferenceTopK {
		_, _ = service.Resolve(ctx, documents)
		service.RecordRetrieval(semantic.ContentTypeReferenceAnswer, true)
		return active, nil
	}

	vectors, err := service.Resolve(ctx, documents)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(documents) {
		return nil, fmt.Errorf("semantic reference vectors=%d want=%d", len(vectors), len(documents))
	}
	queryVectors, err := service.EmbedTexts(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(queryVectors) != 1 {
		return nil, fmt.Errorf("semantic reference query vectors=%d want=1", len(queryVectors))
	}

	ranked := make([]semanticReferenceCandidate, 0, len(active))
	for index := range active {
		ranked = append(ranked, semanticReferenceCandidate{
			index: index,
			score: semantic.CosineSimilarity(queryVectors[0], vectors[index]),
		})
	}
	selected := selectRankedScriptReferences(active, ranked)
	service.RecordRetrieval(semantic.ContentTypeReferenceAnswer, len(selected) > 0)
	return selected, nil
}

func selectRankedScriptReferences(active []model.LiveAgentPlanScriptReference, ranked []semanticReferenceCandidate) []model.LiveAgentPlanScriptReference {
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].index < ranked[j].index
		}
		return ranked[i].score > ranked[j].score
	})
	result := make([]model.LiveAgentPlanScriptReference, 0, semanticReferenceTopK)
	for _, candidate := range ranked {
		if len(result) >= semanticReferenceTopK {
			break
		}
		if candidate.score < semanticReferenceThreshold && len(result) >= 2 {
			break
		}
		result = append(result, active[candidate.index])
	}
	return result
}

func selectRelevantScriptReferences(
	ctx context.Context,
	embedder semantic.Embedder,
	references []model.LiveAgentPlanScriptReference,
	query string,
) ([]model.LiveAgentPlanScriptReference, error) {
	query = strings.TrimSpace(query)
	if embedder == nil || !embedder.Enabled() || query == "" || len(references) == 0 {
		return nil, nil
	}
	if len(references) <= semanticReferenceTopK {
		return references, nil
	}

	texts := make([]string, 1, len(references)+1)
	texts[0] = query
	active := make([]model.LiveAgentPlanScriptReference, 0, len(references))
	for _, reference := range references {
		if !strings.EqualFold(strings.TrimSpace(reference.Status), "active") {
			continue
		}
		content := strings.TrimSpace(reference.ContentText)
		if content == "" {
			continue
		}
		active = append(active, reference)
		texts = append(texts, strings.Join([]string{
			strings.TrimSpace(reference.Title),
			strings.TrimSpace(reference.Goal),
			content,
		}, "\n"))
	}
	if len(active) == 0 {
		return nil, nil
	}
	if len(active) <= semanticReferenceTopK {
		return active, nil
	}

	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("semantic reference vectors=%d want=%d", len(vectors), len(texts))
	}

	ranked := make([]semanticReferenceCandidate, 0, len(active))
	for index := range active {
		ranked = append(ranked, semanticReferenceCandidate{
			index: index,
			score: semantic.CosineSimilarity(vectors[0], vectors[index+1]),
		})
	}
	return selectRankedScriptReferences(active, ranked), nil
}

func scriptReferencePrompt(references []model.LiveAgentPlanScriptReference) string {
	if len(references) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("\n【语义召回的话术参考】")
	builder.WriteString("\n以下内容只提供表达方式、讲解路径和回答案例；其中涉及价格、库存、活动、物流、规格等事实时，必须以当前正式事实为准，不得把历史话术自动当成事实。")
	for index, reference := range references {
		if index >= semanticReferenceTopK {
			break
		}
		content := strings.TrimSpace(reference.ContentText)
		if content == "" {
			continue
		}
		builder.WriteString("\n- ")
		if title := strings.TrimSpace(reference.Title); title != "" {
			builder.WriteString(title)
			builder.WriteString("：")
		}
		builder.WriteString(trimContextRunes(content, 600))
	}
	return builder.String()
}

func (w *Worker) semanticReferencePrompt(ctx context.Context, tenantID, planID int64, item decisionItem) string {
	if w == nil || w.semanticEmbedder == nil || !w.semanticEmbedder.Enabled() || planID <= 0 {
		return ""
	}
	store, ok := w.store.(scriptReferenceStore)
	if !ok {
		return ""
	}
	references, err := store.ListLiveAgentPlanScriptReferences(ctx, tenantID, planID)
	if err != nil {
		return ""
	}
	var selected []model.LiveAgentPlanScriptReference
	if w.semanticService != nil && w.semanticService.Enabled() {
		selected, err = selectRelevantScriptReferencesWithService(
			ctx, w.semanticService, references, semanticMemoryQuery(item), tenantID, planID,
		)
	} else {
		selected, err = selectRelevantScriptReferences(ctx, w.semanticEmbedder, references, semanticMemoryQuery(item))
	}
	if err != nil {
		return ""
	}
	return scriptReferencePrompt(selected)
}
