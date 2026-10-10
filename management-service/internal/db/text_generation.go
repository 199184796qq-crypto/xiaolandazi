package db

import (
	"context"
	"livecompanion/management/internal/agentgateway"
)

func (s *Store) ResolveTextRole(ctx context.Context, stage string) (string, error) {
	models, err := s.SpeechModels(ctx)
	if err != nil {
		return "", err
	}
	if agentgateway.UsesReviewModel(stage) && models.ReviewDefaultID != "" {
		return models.ReviewDefaultID, nil
	}
	if models.SystemDefaultID != "" {
		if agentgateway.UsesSpeechModel(stage) {
			return models.DefaultID, nil
		}
		return models.SystemDefaultID, nil
	}
	if stage != "mainline_text" && stage != "speech_generation" && stage != "interaction_agent_answer" && stage != "speech_review" {
		return "", nil
	}
	settings, err := s.AnchorGenerationSettings(ctx)
	if err != nil {
		return "", err
	}
	if stage == "mainline_text" {
		return settings.Cycle.TextProfileID, nil
	}
	return settings.InteractionProfileID, nil
}
