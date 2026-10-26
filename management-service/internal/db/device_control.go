package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

// Browser controls express an audio preference. Only the gateway's current
// connection lease may establish transport presence or advance its heartbeat.
const liveDeviceConnectionStatusSQL = `CASE
	WHEN JSON_UNQUOTE(JSON_EXTRACT(rs.metadata_json, '$.transport'))='xiaozhi_websocket_proven'
	 AND LENGTH(JSON_UNQUOTE(JSON_EXTRACT(rs.metadata_json, '$.gateway_connection_id'))) BETWEEN 8 AND 96
	 AND JSON_UNQUOTE(JSON_EXTRACT(rs.metadata_json, '$.last_transport_phase')) IN ('connect','heartbeat','disconnect','shutdown')
	THEN COALESCE(rs.connection_status, 'offline') ELSE 'offline' END`

func deviceAudioControl(metadata map[string]any) string {
	switch metadata["audio_control"] {
	case "playing":
		return "playing"
	case "paused":
		return "paused"
	case "disconnected":
		return "disconnected"
	}
	// Earlier browser controls persisted only control_action. Carry that
	// preference forward without treating their claimed handshake as proof.
	switch metadata["control_action"] {
	case "pause":
		return "paused"
	case "disconnect":
		return "disconnected"
	default:
		return "playing"
	}
}

func applyLiveDeviceAudioControl(item *model.LiveDevice, metadata map[string]any) {
	item.WorkStatus = deviceAudioWorkStatus(item.WorkStatus, metadata)
}

func deviceAudioWorkStatus(previous string, metadata map[string]any) string {
	switch deviceAudioControl(metadata) {
	case "paused":
		return "paused"
	case "disconnected":
		return "disconnected"
	case "playing":
		if (previous == "paused" || previous == "disconnected") && (metadata["audio_control"] == "playing" || metadata["control_action"] == "connect" || metadata["control_action"] == "resume") {
			return "idle"
		}
	}
	return previous
}

func deviceConnectionMetadata(raw sql.NullString) map[string]any {
	metadata := map[string]any{}
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return metadata
}

func deviceHasFreshConnection(connection string, heartbeat sql.NullTime, metadata map[string]any, now time.Time) bool {
	if connection != "online" || !heartbeat.Valid || heartbeat.Time.After(now.Add(time.Second)) || now.Sub(heartbeat.Time) > liveDeviceHeartbeatSuspectAfter {
		return false
	}
	if metadata["transport"] != "xiaozhi_websocket_proven" {
		return false
	}
	connectionID, ok := metadata["gateway_connection_id"].(string)
	if !ok || len(strings.TrimSpace(connectionID)) < 8 || len(connectionID) > 96 {
		return false
	}
	return metadata["last_transport_phase"] == "connect" || metadata["last_transport_phase"] == "heartbeat"
}

func requireLiveDeviceConnectionTx(ctx context.Context, tx *sql.Tx, tenantID, roomID, deviceID int64) error {
	var found int64
	var connection string
	var heartbeat sql.NullTime
	var metadata sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT b.device_id, COALESCE(rs.connection_status, 'offline'), rs.last_heartbeat_at, rs.metadata_json
		FROM live_device_room_bindings b
		INNER JOIN inv_devices d ON d.id=b.device_id
		LEFT JOIN live_device_runtime_state rs ON rs.device_id=b.device_id AND rs.tenant_id=b.tenant_id
		WHERE b.tenant_id=? AND b.room_id=? AND b.device_id=?
		  AND b.status='active' AND b.binding_role='primary' AND d.current_customer_id=?
		LIMIT 1 FOR UPDATE
	`, tenantID, roomID, deviceID, tenantID).Scan(&found, &connection, &heartbeat, &metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveDeviceNotBound
	}
	if err != nil {
		return err
	}
	// Assess liveness after acquiring the row lock. A request timestamp can be
	// stale after waiting on another transaction and must not age a new Pong.
	if !deviceHasFreshConnection(connection, heartbeat, deviceConnectionMetadata(metadata), time.Now().UTC()) {
		return ErrLiveDeviceOffline
	}
	return nil
}
