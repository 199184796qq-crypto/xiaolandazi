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
