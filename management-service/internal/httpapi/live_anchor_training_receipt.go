package httpapi

import (
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/styleoverlay"
)

// Snapshot feedback rules supplied to this generation, not proof that the
// model followed them. Never derive this list from later UI history.
type liveAnchorAppliedTraining struct {
	ID                  string `json:"id"`
	Saved               bool   `json:"saved"`
	Feedback            string `json:"feedback"`
	Label               string `json:"label"`
	Diagnosis           string `json:"diagnosis,omitempty"`
	MainlineInstruction string `json:"mainline_instruction"`
}

func appliedAnchorTrainingReceipts(items []model.LiveAnchorStyleOverlayItem, saved bool) []liveAnchorAppliedTraining {
	result := make([]liveAnchorAppliedTraining, 0)
	for _, item := range items {
		if !item.Enabled || item.LearningBasis != "human_feedback" {
			continue
		}
		rule, err := styleoverlay.NormalizeRule(item.Rule)
		if err != nil || strings.TrimSpace(styleoverlay.Render(model.LiveAgentPlanStyleOverlayProfile{Items: []model.LiveAnchorStyleOverlayItem{item}})) == "" {
			continue
		}
		result = append(result, liveAnchorAppliedTraining{
			ID: item.ID, Saved: saved, Feedback: item.SourceText, Label: rule.Label,
			Diagnosis: item.ExplanationText, MainlineInstruction: rule.MainlineInstruction,
		})
	}
	return result
}
