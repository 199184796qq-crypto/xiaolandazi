package db

import "context"

func (s *Store) ResolveTextRole(ctx context.Context, stage string) (string, error) {
	settings, err := s.AnchorGenerationSettings(ctx)
	if err != nil {
		return "", err
	}
	if stage == "mainline_text" {
		return settings.Cycle.TextProfileID, nil
	}
	return settings.InteractionProfileID, nil
}
