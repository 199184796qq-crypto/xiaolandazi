package db

import (
	"context"
	"database/sql"
)

type UserUIPreferences struct {
	UserID                 int64  `json:"user_id"`
	SelectedLiveRoomID     *int64 `json:"selected_live_room_id,omitempty"`
	SidebarCollapsed       bool   `json:"sidebar_collapsed"`
	LivePlanPanelCollapsed bool   `json:"live_plan_panel_collapsed"`
	AgentDrawerCollapsed   bool   `json:"agent_drawer_collapsed"`
}

type UserUIPreferencesPatch struct {
	SelectedLiveRoomID     *int64
	SidebarCollapsed       *bool
	LivePlanPanelCollapsed *bool
	AgentDrawerCollapsed   *bool
}

func defaultUserUIPreferences(userID int64) UserUIPreferences {
	return UserUIPreferences{
		UserID:                 userID,
		SidebarCollapsed:       true,
		LivePlanPanelCollapsed: true,
		AgentDrawerCollapsed:   true,
	}
}

func (s *Store) GetUserUIPreferences(ctx context.Context, userID int64) (UserUIPreferences, error) {
	item := defaultUserUIPreferences(userID)
	var roomID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT selected_live_room_id, sidebar_collapsed, live_plan_panel_collapsed, agent_drawer_collapsed
		FROM mgmt_user_ui_preferences
		WHERE user_id=?
	`, userID).Scan(
		&roomID,
		&item.SidebarCollapsed,
		&item.LivePlanPanelCollapsed,
		&item.AgentDrawerCollapsed,
	)
	if err == sql.ErrNoRows {
		return item, nil
	}
	if err != nil {
		return UserUIPreferences{}, err
	}
	if roomID.Valid {
		value := roomID.Int64
		item.SelectedLiveRoomID = &value
	}
	return item, nil
}

func (s *Store) UpdateUserUIPreferences(
	ctx context.Context,
	userID int64,
	patch UserUIPreferencesPatch,
) (UserUIPreferences, error) {
	item, err := s.GetUserUIPreferences(ctx, userID)
	if err != nil {
		return UserUIPreferences{}, err
	}
	if patch.SelectedLiveRoomID != nil {
		if *patch.SelectedLiveRoomID > 0 {
			value := *patch.SelectedLiveRoomID
			item.SelectedLiveRoomID = &value
		} else {
			item.SelectedLiveRoomID = nil
		}
	}
	if patch.SidebarCollapsed != nil {
		item.SidebarCollapsed = *patch.SidebarCollapsed
	}
	if patch.LivePlanPanelCollapsed != nil {
		item.LivePlanPanelCollapsed = *patch.LivePlanPanelCollapsed
	}
	if patch.AgentDrawerCollapsed != nil {
		item.AgentDrawerCollapsed = *patch.AgentDrawerCollapsed
	}

	var roomValue any
	if item.SelectedLiveRoomID != nil {
		roomValue = *item.SelectedLiveRoomID
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO mgmt_user_ui_preferences (
			user_id, selected_live_room_id, sidebar_collapsed, live_plan_panel_collapsed, agent_drawer_collapsed
		) VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			selected_live_room_id=VALUES(selected_live_room_id),
			sidebar_collapsed=VALUES(sidebar_collapsed),
			live_plan_panel_collapsed=VALUES(live_plan_panel_collapsed),
			agent_drawer_collapsed=VALUES(agent_drawer_collapsed),
			updated_at=CURRENT_TIMESTAMP(3)
	`, userID, roomValue, item.SidebarCollapsed, item.LivePlanPanelCollapsed, item.AgentDrawerCollapsed)
	if err != nil {
		return UserUIPreferences{}, err
	}
	return item, nil
}
