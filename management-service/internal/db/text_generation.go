package db

import (
	"context"
	"livecompanion/management/internal/agentgateway"
)

func (s *Store) ResolveTextRole(ctx context.Context, stage string) (string, error) {
	// Interaction answers can be pinned to the operator's own role. Resolve them
	// before the room model pool collapses every role onto the leased 口播 model;
	// the matching model call then bypasses the pool through
	// ResolveOperatorSpeechModel (see agentgateway OperatorModelResolver).
	if agentgateway.UsesOperatorPinnedModel(stage) {
		models, err := s.operatorSpeechModels(ctx)
		if err != nil {
			return "", err
		}
		if models.ReviewDefaultID != "" {
			return models.ReviewDefaultID, nil
		}
	}
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
	if stage != "mainline_text" && stage != "speech_generation" && stage != "speech_review" {
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
