package httpapi

import (
	"context"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/atomicfacts"
)

// buildAtomicFacts uses the review model to split facts by attribute. The
// deterministic splitter is the fallback; a model/validation failure never
// blocks plan initialization or saving.
func (s *Server) buildAtomicFacts(ctx context.Context, raw string) string {
	fallback, _ := atomicfacts.Build(raw)
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	roleID, err := s.store.ResolveTextRole(ctx, "interaction_purpose_resolution")
	if err != nil {
		return fallback
	}
	profile, err := s.store.ResolveOperatorSpeechModel(ctx, roleID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		return fallback
	}
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	text, _, err := atomicfacts.BuildWithModel(callCtx, raw, func(callCtx context.Context, prompt string) (string, error) {
		response, callErr := agentgateway.CompleteSpeechEndpoint(callCtx, *profile, agentgateway.Request{
			Stage:          "atomic_fact_split",
			Model:          profile.Model,
			Messages:       []agentgateway.Message{{Role: "system", Content: prompt}},
			MaxTokens:      6000,
			ResponseFormat: agentgateway.ResponseJSON,
		})
		if callErr != nil {
			return "", callErr
		}
		return response.Text, nil
	})
	if err != nil || strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}
