package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"livecompanion/management/internal/model"
)

// Publish only the delegated room's speech configuration into the latest active
// tenant version. Never activate the support draft wholesale: it may have been
// based on an older version or on a customer's unrelated unpublished draft.
func (s *Store) ActivateLiveSupportSpeechConfig(ctx context.Context, tenantID, roomID, versionID, staffID int64) (int64, error) {
	return s.activateLiveSupportRoomConfig(ctx, tenantID, roomID, versionID, staffID, model.LiveSupportCapabilityL3Policy, "speech")
}

func (s *Store) ActivateLiveSupportTrainingConfig(ctx context.Context, tenantID, roomID, versionID, staffID int64) (int64, error) {
	return s.activateLiveSupportRoomConfig(ctx, tenantID, roomID, versionID, staffID, model.LiveSupportCapabilityAnchorTraining, "style")
}

func (s *Store) activateLiveSupportRoomConfig(ctx context.Context, tenantID, roomID, versionID, staffID int64, capability, field string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockUsableRoomTx(ctx, tx, tenantID, roomID); err != nil {
		return 0, err
	}
	var grantID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM live_support_authorizations
		WHERE tenant_id=? AND room_id=? AND staff_user_id=? AND capability=?
		AND status='active' AND revoked_at IS NULL FOR UPDATE`, tenantID, roomID, staffID, capability).Scan(&grantID); err != nil {
		return 0, err
	}
	var agentID int64
	var speechRaw, styleRaw string
	if err := tx.QueryRowContext(ctx, `
		SELECT v.agent_id, CAST(v.speech_config_json AS CHAR), CAST(v.style_profile_json AS CHAR)
		FROM live_agent_config_versions v
		INNER JOIN live_support_config_versions support ON support.version_id=v.id
		WHERE v.id=? AND support.tenant_id=? AND support.room_id=?
		AND support.staff_user_id=? AND support.capability=? AND v.lifecycle_status='draft'
		FOR UPDATE`, versionID, tenantID, roomID, staffID, capability).Scan(&agentID, &speechRaw, &styleRaw); err != nil {
		return 0, err
	}
	var speech map[string]any
	if field == "style" {
		speechRaw = styleRaw
	}
	if err := json.Unmarshal([]byte(speechRaw), &speech); err != nil {
		return 0, err
	}
	rooms, _ := speech["rooms"].(map[string]any)
	roomSpeech, ok := rooms[fmt.Sprint(roomID)].(map[string]any)
	if !ok {
		return 0, errors.New("missing support room speech config")
	}
	roomRaw, err := json.Marshal(roomSpeech)
	if err != nil {
		return 0, err
	}
	var currentVersionID int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(current_version_id,0) FROM live_agent_profiles
		WHERE id=? AND tenant_id=? FOR UPDATE`, agentID, tenantID).Scan(&currentVersionID); err != nil {
		return 0, err
	}
	var nextVersion uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_no),0)+1 FROM live_agent_config_versions WHERE agent_id=?`, agentID).Scan(&nextVersion); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_config_versions (agent_id, version_no, layer1_json, layer2_json, layer3_json,
		persona_json, model_config_json, speech_config_json, style_profile_json, safety_config_json,
		lifecycle_status, created_by_user_id, published_by_user_id, published_at)
		SELECT ?, ?, COALESCE(v.layer1_json, JSON_OBJECT()), COALESCE(v.layer2_json, JSON_OBJECT()),
		COALESCE(v.layer3_json, JSON_OBJECT()), COALESCE(v.persona_json, JSON_OBJECT()),
		COALESCE(v.model_config_json, JSON_OBJECT()),
		CASE WHEN ?='speech' THEN JSON_SET(COALESCE(v.speech_config_json, JSON_OBJECT()), '$.rooms',
		 JSON_SET(COALESCE(JSON_EXTRACT(v.speech_config_json, '$.rooms'), JSON_OBJECT()), ?, CAST(? AS JSON))) ELSE COALESCE(v.speech_config_json, JSON_OBJECT()) END,
		CASE WHEN ?='style' THEN JSON_SET(COALESCE(v.style_profile_json, JSON_OBJECT()), '$.rooms',
		 JSON_SET(COALESCE(JSON_EXTRACT(v.style_profile_json, '$.rooms'), JSON_OBJECT()), ?, CAST(? AS JSON))) ELSE COALESCE(v.style_profile_json, JSON_OBJECT()) END,
		COALESCE(v.safety_config_json, JSON_OBJECT()),
		'active', ?, ?, CURRENT_TIMESTAMP(3)
		FROM live_agent_profiles p LEFT JOIN live_agent_config_versions v ON v.id=? AND v.agent_id=p.id
		WHERE p.id=? AND p.tenant_id=?`, agentID, nextVersion, field, fmt.Sprintf(`$."%d"`, roomID), string(roomRaw), field, fmt.Sprintf(`$."%d"`, roomID), string(roomRaw), staffID, staffID, currentVersionID, agentID, tenantID)
	if err != nil {
		return 0, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_agent_config_versions SET lifecycle_status='archived'
		WHERE agent_id=? AND (id=? OR id=?) AND id<>?`, agentID, currentVersionID, versionID, newID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_agent_profiles SET current_version_id=?, updated_by_user_id=? WHERE id=?`, newID, staffID, agentID); err != nil {
		return 0, err
	}
	if err := insertLiveSupportEventTx(ctx, tx, "config.room_"+field+".publish", tenantID, roomID, staffID, staffID, capability, map[string]any{"draft_id": versionID, "active_version_id": newID}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newID, nil
}
