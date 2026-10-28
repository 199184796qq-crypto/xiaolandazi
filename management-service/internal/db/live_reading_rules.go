package db

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

// GetRoomReadingRules returns the room's reading rules text (one rule per line).
func (s *Store) GetRoomReadingRules(ctx context.Context, tenantID, roomID int64) (model.RoomReadingRules, error) {
	version, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenantID, roomID)
	if err != nil {
		return model.RoomReadingRules{}, err
	}
	if text, ok := version.GenerationContext["reading_rules"].(string); ok {
		return model.RoomReadingRules{TenantID: tenantID, PlanID: version.PlanID, RoomID: roomID, RulesText: text}, nil
	}
	return s.GetPlanReadingRules(ctx, tenantID, version.PlanID)
}

// UpsertRoomReadingRules saves the room's reading rules, capped to a sane size.
func (s *Store) UpsertRoomReadingRules(ctx context.Context, tenantID, roomID, userID int64, input model.RoomReadingRulesInput) (model.RoomReadingRules, error) {
	text := strings.TrimSpace(input.RulesText)
	if len([]rune(text)) > 4000 {
		text = string([]rune(text)[:4000])
	}
	plan, err := s.GetLiveAgentPlanForRoom(ctx, tenantID, roomID)
	if err != nil {
		return model.RoomReadingRules{}, err
	}
	return s.SetPlanReadingRules(ctx, tenantID, plan.ID, userID, text)
}
