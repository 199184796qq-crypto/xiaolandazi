package coreclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"livecompanion/management/internal/model"
)

// Core receives runtime configuration, not the management database's audit
// record. Keep this boundary explicit: Core rejects unknown JSON fields.
type roomInteractionPreferencesPayload struct {
	model.RoomInteractionPreferencesInput
	TenantID  int64     `json:"tenant_id"`
	RoomID    int64     `json:"room_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c *Client) PutRoomInteractionPreferences(ctx context.Context, item model.RoomInteractionPreferences) (*http.Response, error) {
	if item.TenantID <= 0 || item.RoomID <= 0 {
		return nil, fmt.Errorf("interaction preferences require a tenant and room")
	}
	payload := roomInteractionPreferencesPayload{
		RoomInteractionPreferencesInput: model.RoomInteractionPreferencesInput{
			Schedule:             item.Schedule,
			OverallInteraction:   item.OverallInteraction,
			QuestionPreference:   item.QuestionPreference,
			WelcomePreference:    item.WelcomePreference,
			EngagementPreference: item.EngagementPreference,
			ChatPreference:       item.ChatPreference,
			ConversionPreference: item.ConversionPreference,
			AutoHeat:             item.AutoHeat,
		},
		TenantID: item.TenantID, RoomID: item.RoomID, UpdatedAt: item.UpdatedAt,
	}
	query := url.Values{"tenant_id": {strconv.FormatInt(item.TenantID, 10)}}
	return c.DoRoom(ctx, item.TenantID, item.RoomID, http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/interaction-preferences", item.RoomID), query, payload)
}
