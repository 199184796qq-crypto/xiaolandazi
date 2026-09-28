package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var (
	ErrLiveRuntimeAlreadyRunning = errors.New("live runtime already running")
	ErrLiveRuntimeNotRunning     = errors.New("live runtime not running")
	ErrLiveQuotaExhausted        = errors.New("live quota exhausted")
	ErrLiveDeviceNotBound        = errors.New("live device not bound")
	ErrLiveDeviceOffline         = errors.New("live device offline")
	ErrLiveBindingBusy           = errors.New("live device or room is running")
)

const liveDeviceHeartbeatTimeout = 20 * time.Second

type quotaBucketLock struct {
	ID        int64
	Remaining uint64
	Reserved  uint64
	ExpiresAt time.Time
}

func (b quotaBucketLock) Available() uint64 {
	if b.Remaining <= b.Reserved {
		return 0
	}
	return b.Remaining - b.Reserved
}

func newLiveReference(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UTC().UnixMilli(), hex.EncodeToString(raw[:])), nil
}

func defaultLiveAgentSettings(tenantID int64) model.LiveAgentSettings {
	return model.LiveAgentSettings{
		TenantID:         tenantID,
		DisplayName:      "小伴直播教练",
		RoleName:         "直播策略与场控 Agent",
		SelfIntroduction: "我是小伴直播教练，是你的直播策略与场控 Agent。",
		Mission:          "我负责直播策略调教、主播训练、固定话术、声音配置和现场场控协作。终端用户的调整只写入当前直播间第3层策略，不修改系统层和行业层。",
		Greeting:         "你好，我是小伴直播教练。你可以直接告诉我想调整主播表达、固定话术、声音或直播策略。",
	}
}

func (s *Store) GetLiveAgentSettings(ctx context.Context, tenantID int64) (model.LiveAgentSettings, error) {
	item := defaultLiveAgentSettings(tenantID)
	var updatedBy sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, display_name, role_name, self_introduction,
		       mission, greeting, updated_by_user_id, updated_at
		FROM live_agent_settings
		WHERE tenant_id=?
	`, tenantID).Scan(
		&item.TenantID,
		&item.DisplayName,
		&item.RoleName,
		&item.SelfIntroduction,
		&item.Mission,
		&item.Greeting,
		&updatedBy,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return model.LiveAgentSettings{}, err
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	return item, nil
}

func (s *Store) UpsertLiveAgentSettings(
	ctx context.Context,
	tenantID, userID int64,
	input model.LiveAgentSettingsInput,
) (model.LiveAgentSettings, error) {
	defaults := defaultLiveAgentSettings(tenantID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.RoleName = strings.TrimSpace(input.RoleName)
	input.SelfIntroduction = strings.TrimSpace(input.SelfIntroduction)
	input.Mission = strings.TrimSpace(input.Mission)
	input.Greeting = strings.TrimSpace(input.Greeting)
	if input.DisplayName == "" {
		input.DisplayName = defaults.DisplayName
	}
	if input.RoleName == "" {
		input.RoleName = defaults.RoleName
	}
	if input.SelfIntroduction == "" {
		input.SelfIntroduction = defaults.SelfIntroduction
	}
	if input.Mission == "" {
		input.Mission = defaults.Mission
	}
	if input.Greeting == "" {
		input.Greeting = defaults.Greeting
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentSettings{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_settings (
			tenant_id, display_name, role_name, self_introduction,
			mission, greeting, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			display_name=VALUES(display_name),
			role_name=VALUES(role_name),
			self_introduction=VALUES(self_introduction),
			mission=VALUES(mission),
			greeting=VALUES(greeting),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`,
		tenantID,
		input.DisplayName,
		input.RoleName,
		input.SelfIntroduction,
		input.Mission,
		input.Greeting,
		userID,
	)
	if err != nil {
		return model.LiveAgentSettings{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_profiles (
			tenant_id, name, status, created_by_user_id, updated_by_user_id
		) VALUES (?, ?, 'active', ?, ?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			status='active',
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, input.DisplayName, userID, userID); err != nil {
		return model.LiveAgentSettings{}, err
	}

	var agentID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM live_agent_profiles
		WHERE tenant_id=?
		LIMIT 1
	`, tenantID).Scan(&agentID); err != nil {
		return model.LiveAgentSettings{}, err
	}

	var nextVersion uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM live_agent_config_versions
		WHERE agent_id=?
	`, agentID).Scan(&nextVersion); err != nil {
		return model.LiveAgentSettings{}, err
	}

	base := model.AgentConfigInput{
		Layer1:       map[string]any{},
		Layer2:       map[string]any{},
		Layer3:       map[string]any{},
		ModelConfig:  map[string]any{},
		SpeechConfig: map[string]any{},
		StyleProfile: map[string]any{},
		SafetyConfig: map[string]any{},
	}
	var layer1Raw, layer2Raw, layer3Raw string
	var modelRaw, speechRaw, styleRaw, safetyRaw string
	err = tx.QueryRowContext(ctx, `
		SELECT
			CAST(v.layer1_json AS CHAR),
			CAST(v.layer2_json AS CHAR),
			CAST(v.layer3_json AS CHAR),
			CAST(v.model_config_json AS CHAR),
			CAST(v.speech_config_json AS CHAR),
			CAST(v.style_profile_json AS CHAR),
			CAST(v.safety_config_json AS CHAR)
		FROM live_agent_profiles p
		INNER JOIN live_agent_config_versions v ON v.id=p.current_version_id
		WHERE p.id=?
		LIMIT 1
	`, agentID).Scan(
		&layer1Raw,
		&layer2Raw,
		&layer3Raw,
		&modelRaw,
		&speechRaw,
		&styleRaw,
		&safetyRaw,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentSettings{}, err
	}
	if err == nil {
		_ = json.Unmarshal([]byte(layer1Raw), &base.Layer1)
		_ = json.Unmarshal([]byte(layer2Raw), &base.Layer2)
		_ = json.Unmarshal([]byte(layer3Raw), &base.Layer3)
		_ = json.Unmarshal([]byte(modelRaw), &base.ModelConfig)
		_ = json.Unmarshal([]byte(speechRaw), &base.SpeechConfig)
		_ = json.Unmarshal([]byte(styleRaw), &base.StyleProfile)
		_ = json.Unmarshal([]byte(safetyRaw), &base.SafetyConfig)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_config_versions
		SET lifecycle_status='archived'
		WHERE agent_id=? AND lifecycle_status IN ('active', 'draft')
	`, agentID); err != nil {
		return model.LiveAgentSettings{}, err
	}

	base.Persona = map[string]any{
		"display_name":      input.DisplayName,
		"role_name":         input.RoleName,
		"self_introduction": input.SelfIntroduction,
		"mission":           input.Mission,
		"greeting":          input.Greeting,
	}
	jsonValues, err := marshalAgentConfigInput(base)
	if err != nil {
		return model.LiveAgentSettings{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_config_versions (
			agent_id, version_no,
			layer1_json, layer2_json, layer3_json,
			persona_json, model_config_json, speech_config_json,
			style_profile_json, safety_config_json,
			lifecycle_status, created_by_user_id, published_by_user_id, published_at
		) VALUES (
			?, ?,
			CAST(? AS JSON), CAST(? AS JSON), CAST(? AS JSON),
			CAST(? AS JSON), CAST(? AS JSON), CAST(? AS JSON),
			CAST(? AS JSON), CAST(? AS JSON),
			'active', ?, ?, CURRENT_TIMESTAMP(3)
		)
	`,
		agentID,
		nextVersion,
		jsonValues[0],
		jsonValues[1],
		jsonValues[2],
		jsonValues[3],
		jsonValues[4],
		jsonValues[5],
		jsonValues[6],
		jsonValues[7],
		userID,
		userID,
	)
	if err != nil {
		return model.LiveAgentSettings{}, err
	}
	versionID, err := result.LastInsertId()
	if err != nil {
		return model.LiveAgentSettings{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_profiles
		SET current_version_id=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, versionID, userID, agentID); err != nil {
		return model.LiveAgentSettings{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveAgentSettings{}, err
	}
	return s.GetLiveAgentSettings(ctx, tenantID)
}

func (s *Store) ListLiveAgentConfigVersions(
	ctx context.Context,
	tenantID int64,
) ([]model.AgentConfigVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			v.id, v.agent_id, v.version_no,
			CAST(v.layer1_json AS CHAR),
			CAST(v.layer2_json AS CHAR),
			CAST(v.layer3_json AS CHAR),
			CAST(v.persona_json AS CHAR),
			CAST(v.model_config_json AS CHAR),
			CAST(v.speech_config_json AS CHAR),
			CAST(v.style_profile_json AS CHAR),
			CAST(v.safety_config_json AS CHAR),
			v.lifecycle_status, v.created_at, v.published_at
		FROM live_agent_config_versions v
		INNER JOIN live_agent_profiles p ON p.id=v.agent_id
		WHERE p.tenant_id=?
		ORDER BY v.version_no DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AgentConfigVersion, 0)
	for rows.Next() {
		var item model.AgentConfigVersion
		var layer1Raw, layer2Raw, layer3Raw string
		var personaRaw, modelRaw, speechRaw, styleRaw, safetyRaw string
		var publishedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.AgentID,
			&item.VersionNo,
			&layer1Raw,
			&layer2Raw,
			&layer3Raw,
			&personaRaw,
			&modelRaw,
			&speechRaw,
			&styleRaw,
			&safetyRaw,
			&item.LifecycleStatus,
			&item.CreatedAt,
			&publishedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(layer1Raw), &item.Layer1)
		_ = json.Unmarshal([]byte(layer2Raw), &item.Layer2)
		_ = json.Unmarshal([]byte(layer3Raw), &item.Layer3)
		_ = json.Unmarshal([]byte(personaRaw), &item.Persona)
		_ = json.Unmarshal([]byte(modelRaw), &item.ModelConfig)
		_ = json.Unmarshal([]byte(speechRaw), &item.SpeechConfig)
		_ = json.Unmarshal([]byte(styleRaw), &item.StyleProfile)
		_ = json.Unmarshal([]byte(safetyRaw), &item.SafetyConfig)
		if publishedAt.Valid {
			value := publishedAt.Time
			item.PublishedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) PauseLiveRuntimeSession(
	ctx context.Context,
	tenantID, roomID, userID int64,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockRunningSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveRuntimeSession{}, ErrLiveRuntimeNotRunning
		}
		return model.LiveRuntimeSession{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='paused', stop_reason='', last_billed_at=?,
		    version=version+1, updated_at=?
		WHERE id=? AND status='running'
	`, now, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title, occurred_at
		) VALUES (?, ?, ?, ?, 'user', ?, 'AI_RUNTIME_PAUSED', 'AI直播伴播已暂停', ?)
	`, tenantID, roomID, session.DeviceID, session.ID, userID, now); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
}

func (s *Store) ResumeLiveRuntimeSession(
	ctx context.Context,
	tenantID, roomID, userID int64,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockPausedSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveRuntimeSession{}, ErrLiveRuntimeNotRunning
		}
		return model.LiveRuntimeSession{}, err
	}
	buckets, err := ensureLiveQuotaAvailableTx(ctx, tx, tenantID, 1, now)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	var quota uint64
	for _, bucket := range buckets {
		quota += bucket.Available()
	}
	if quota == 0 {
		return model.LiveRuntimeSession{}, ErrLiveQuotaExhausted
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='running', stop_reason='', last_billed_at=?,
		    version=version+1, updated_at=?
		WHERE id=? AND status='paused'
	`, now, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title, occurred_at
		) VALUES (?, ?, ?, ?, 'user', ?, 'AI_RUNTIME_RESUMED', 'AI直播伴播已继续', ?)
	`, tenantID, roomID, session.DeviceID, session.ID, userID, now); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
}

func (s *Store) CreateLiveAgentConfigDraft(
	ctx context.Context,
	tenantID, userID int64,
	input model.AgentConfigInput,
) (model.AgentConfigVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentConfigVersion{}, err
	}
	defer tx.Rollback()

	settings := defaultLiveAgentSettings(tenantID)
	var updatedBy sql.NullInt64
	_ = tx.QueryRowContext(ctx, `
		SELECT tenant_id, display_name, role_name, self_introduction,
		       mission, greeting, updated_by_user_id, updated_at
		FROM live_agent_settings
		WHERE tenant_id=?
	`, tenantID).Scan(
		&settings.TenantID,
		&settings.DisplayName,
		&settings.RoleName,
		&settings.SelfIntroduction,
		&settings.Mission,
		&settings.Greeting,
		&updatedBy,
		&settings.UpdatedAt,
	)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_profiles (
			tenant_id, name, status, created_by_user_id, updated_by_user_id
		) VALUES (?, ?, 'active', ?, ?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			status='active',
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, settings.DisplayName, userID, userID); err != nil {
		return model.AgentConfigVersion{}, err
	}

	var agentID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM live_agent_profiles
		WHERE tenant_id=?
		LIMIT 1
	`, tenantID).Scan(&agentID); err != nil {
		return model.AgentConfigVersion{}, err
	}

	var nextVersion uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM live_agent_config_versions
		WHERE agent_id=?
	`, agentID).Scan(&nextVersion); err != nil {
		return model.AgentConfigVersion{}, err
	}

	base := model.AgentConfigInput{
		Layer1: map[string]any{},
		Layer2: map[string]any{},
		Layer3: map[string]any{},
		Persona: map[string]any{
			"display_name":      settings.DisplayName,
			"role_name":         settings.RoleName,
			"self_introduction": settings.SelfIntroduction,
			"mission":           settings.Mission,
			"greeting":          settings.Greeting,
		},
		ModelConfig:  map[string]any{},
		SpeechConfig: map[string]any{},
		StyleProfile: map[string]any{},
		SafetyConfig: map[string]any{},
	}
	var layer1Raw, layer2Raw, layer3Raw string
	var personaRaw, modelRaw, speechRaw, styleRaw, safetyRaw string
	err = tx.QueryRowContext(ctx, `
		SELECT
			CAST(layer1_json AS CHAR),
			CAST(layer2_json AS CHAR),
			CAST(layer3_json AS CHAR),
			CAST(persona_json AS CHAR),
			CAST(model_config_json AS CHAR),
			CAST(speech_config_json AS CHAR),
			CAST(style_profile_json AS CHAR),
			CAST(safety_config_json AS CHAR)
		FROM live_agent_config_versions
		WHERE agent_id=?
		  AND lifecycle_status IN ('draft', 'active')
		ORDER BY (lifecycle_status='draft') DESC, version_no DESC
		LIMIT 1
	`, agentID).Scan(
		&layer1Raw,
		&layer2Raw,
		&layer3Raw,
		&personaRaw,
		&modelRaw,
		&speechRaw,
		&styleRaw,
		&safetyRaw,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AgentConfigVersion{}, err
	}
	if err == nil {
		_ = json.Unmarshal([]byte(layer1Raw), &base.Layer1)
		_ = json.Unmarshal([]byte(layer2Raw), &base.Layer2)
		_ = json.Unmarshal([]byte(layer3Raw), &base.Layer3)
		_ = json.Unmarshal([]byte(personaRaw), &base.Persona)
		_ = json.Unmarshal([]byte(modelRaw), &base.ModelConfig)
		_ = json.Unmarshal([]byte(speechRaw), &base.SpeechConfig)
		_ = json.Unmarshal([]byte(styleRaw), &base.StyleProfile)
		_ = json.Unmarshal([]byte(safetyRaw), &base.SafetyConfig)
	}
	input = mergeAgentConfigInput(base, input)
	jsonValues, err := marshalAgentConfigInput(input)
	if err != nil {
		return model.AgentConfigVersion{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_config_versions (
			agent_id, version_no,
			layer1_json, layer2_json, layer3_json,
			persona_json, model_config_json, speech_config_json,
			style_profile_json, safety_config_json,
			lifecycle_status, created_by_user_id
		) VALUES (
			?, ?,
			CAST(? AS JSON), CAST(? AS JSON), CAST(? AS JSON),
			CAST(? AS JSON), CAST(? AS JSON), CAST(? AS JSON),
			CAST(? AS JSON), CAST(? AS JSON),
			'draft', ?
		)
	`,
		agentID,
		nextVersion,
		jsonValues[0],
		jsonValues[1],
		jsonValues[2],
		jsonValues[3],
		jsonValues[4],
		jsonValues[5],
		jsonValues[6],
		jsonValues[7],
		userID,
	)
	if err != nil {
		return model.AgentConfigVersion{}, err
	}
	versionID, err := result.LastInsertId()
	if err != nil {
		return model.AgentConfigVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AgentConfigVersion{}, err
	}
	return s.GetLiveAgentConfigVersion(ctx, tenantID, versionID)
}

func (s *Store) GetLiveAgentConfigVersion(
	ctx context.Context,
	tenantID, versionID int64,
) (model.AgentConfigVersion, error) {
	var item model.AgentConfigVersion
	var layer1Raw, layer2Raw, layer3Raw string
	var personaRaw, modelRaw, speechRaw, styleRaw, safetyRaw string
	var publishedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT
			v.id, v.agent_id, v.version_no,
			CAST(v.layer1_json AS CHAR),
			CAST(v.layer2_json AS CHAR),
			CAST(v.layer3_json AS CHAR),
			CAST(v.persona_json AS CHAR),
			CAST(v.model_config_json AS CHAR),
			CAST(v.speech_config_json AS CHAR),
			CAST(v.style_profile_json AS CHAR),
			CAST(v.safety_config_json AS CHAR),
			v.lifecycle_status, v.created_at, v.published_at
		FROM live_agent_config_versions v
		INNER JOIN live_agent_profiles p ON p.id=v.agent_id
		WHERE v.id=? AND p.tenant_id=?
		LIMIT 1
	`, versionID, tenantID).Scan(
		&item.ID,
		&item.AgentID,
		&item.VersionNo,
		&layer1Raw,
		&layer2Raw,
		&layer3Raw,
		&personaRaw,
		&modelRaw,
		&speechRaw,
		&styleRaw,
		&safetyRaw,
		&item.LifecycleStatus,
		&item.CreatedAt,
		&publishedAt,
	)
	if err != nil {
		return model.AgentConfigVersion{}, err
	}
	_ = json.Unmarshal([]byte(layer1Raw), &item.Layer1)
	_ = json.Unmarshal([]byte(layer2Raw), &item.Layer2)
	_ = json.Unmarshal([]byte(layer3Raw), &item.Layer3)
	_ = json.Unmarshal([]byte(personaRaw), &item.Persona)
	_ = json.Unmarshal([]byte(modelRaw), &item.ModelConfig)
	_ = json.Unmarshal([]byte(speechRaw), &item.SpeechConfig)
	_ = json.Unmarshal([]byte(styleRaw), &item.StyleProfile)
	_ = json.Unmarshal([]byte(safetyRaw), &item.SafetyConfig)
	if publishedAt.Valid {
		value := publishedAt.Time
		item.PublishedAt = &value
	}
	return item, nil
}

func (s *Store) ActivateLiveAgentConfigVersion(
	ctx context.Context,
	tenantID, versionID, userID int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var agentID int64
	var personaRaw string
	if err := tx.QueryRowContext(ctx, `
		SELECT v.agent_id, CAST(v.persona_json AS CHAR)
		FROM live_agent_config_versions v
		INNER JOIN live_agent_profiles p ON p.id=v.agent_id
		WHERE v.id=? AND p.tenant_id=?
		LIMIT 1
	`, versionID, tenantID).Scan(&agentID, &personaRaw); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_config_versions
		SET lifecycle_status='archived'
		WHERE agent_id=?
		  AND lifecycle_status IN ('active', 'draft')
		  AND id<>?
	`, agentID, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_config_versions
		SET lifecycle_status='active',
		    published_by_user_id=?,
		    published_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND agent_id=?
	`, userID, versionID, agentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_profiles
		SET current_version_id=?, updated_by_user_id=?
		WHERE id=?
	`, versionID, userID, agentID); err != nil {
		return err
	}

	var persona map[string]any
	if err := json.Unmarshal([]byte(personaRaw), &persona); err != nil {
		return err
	}
	defaults := defaultLiveAgentSettings(tenantID)
	displayName := stringValue(persona, "display_name", defaults.DisplayName)
	roleName := stringValue(persona, "role_name", defaults.RoleName)
	selfIntroduction := stringValue(persona, "self_introduction", defaults.SelfIntroduction)
	mission := stringValue(persona, "mission", defaults.Mission)
	greeting := stringValue(persona, "greeting", defaults.Greeting)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_settings (
			tenant_id, display_name, role_name, self_introduction,
			mission, greeting, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			display_name=VALUES(display_name),
			role_name=VALUES(role_name),
			self_introduction=VALUES(self_introduction),
			mission=VALUES(mission),
			greeting=VALUES(greeting),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`,
		tenantID,
		displayName,
		roleName,
		selfIntroduction,
		mission,
		greeting,
		userID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func marshalAgentConfigInput(input model.AgentConfigInput) ([8]string, error) {
	values := []map[string]any{
		input.Layer1,
		input.Layer2,
		input.Layer3,
		input.Persona,
		input.ModelConfig,
		input.SpeechConfig,
		input.StyleProfile,
		input.SafetyConfig,
	}
	var result [8]string
	for i, value := range values {
		if value == nil {
			value = map[string]any{}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return result, err
		}
		result[i] = string(raw)
	}
	return result, nil
}

func mergeAgentConfigInput(base, patch model.AgentConfigInput) model.AgentConfigInput {
	base.Layer1 = mergeJSONMap(base.Layer1, patch.Layer1)
	base.Layer2 = mergeJSONMap(base.Layer2, patch.Layer2)
	base.Layer3 = mergeJSONMap(base.Layer3, patch.Layer3)
	base.Persona = mergeJSONMap(base.Persona, patch.Persona)
	base.ModelConfig = mergeJSONMap(base.ModelConfig, patch.ModelConfig)
	base.SpeechConfig = mergeJSONMap(base.SpeechConfig, patch.SpeechConfig)
	base.StyleProfile = mergeJSONMap(base.StyleProfile, patch.StyleProfile)
	base.SafetyConfig = mergeJSONMap(base.SafetyConfig, patch.SafetyConfig)
	return base
}

func mergeJSONMap(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	if len(patch) == 0 {
		return base
	}
	result := make(map[string]any, len(base)+len(patch))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range patch {
		result[key] = value
	}
	return result
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (s *Store) GetQuotaRemainingSeconds(ctx context.Context, tenantID int64) (uint64, error) {
	var total uint64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(remaining_seconds), 0)
		FROM quota_buckets
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_at <= CURRENT_TIMESTAMP(3)
		  AND expires_at > CURRENT_TIMESTAMP(3)
		  AND remaining_seconds > 0
	`, tenantID).Scan(&total)
	return total, err
}

func (s *Store) ListLiveDevices(ctx context.Context, tenantID int64) ([]model.LiveDevice, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			d.id, d.sn, d.sku_code, d.lifecycle_status, d.current_customer_id,
			b.room_id, COALESCE(b.binding_role, ''),
			COALESCE(rs.connection_status, 'offline'),
			COALESCE(rs.work_status, 'idle'),
			COALESCE(rs.stop_reason, ''),
			rs.last_heartbeat_at
		FROM inv_devices d
		LEFT JOIN live_device_room_bindings b
		  ON b.device_id=d.id AND b.status='active'
		LEFT JOIN live_device_runtime_state rs
		  ON rs.device_id=d.id
		WHERE (?=0 OR d.current_customer_id=?)
		  AND d.current_customer_id IS NOT NULL
		  AND d.lifecycle_status <> 'SCRAPPED'
		ORDER BY d.current_customer_id ASC, d.id DESC
	`, tenantID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveDevice, 0)
	for rows.Next() {
		var item model.LiveDevice
		var roomID sql.NullInt64
		var tenant sql.NullInt64
		var heartbeat sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SN,
			&item.SKUCode,
			&item.LifecycleStatus,
			&tenant,
			&roomID,
			&item.BindingRole,
			&item.ConnectionStatus,
			&item.WorkStatus,
			&item.StopReason,
			&heartbeat,
		); err != nil {
			return nil, err
		}
		if tenant.Valid {
			item.TenantID = tenant.Int64
		}
		if roomID.Valid {
			value := roomID.Int64
			item.RoomID = &value
		}
		if heartbeat.Valid {
			value := heartbeat.Time
			item.LastHeartbeatAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLiveDevice(ctx context.Context, tenantID, deviceID int64) (model.LiveDevice, error) {
	var item model.LiveDevice
	var roomID sql.NullInt64
	var heartbeat sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT
			d.id, d.sn, d.sku_code, d.lifecycle_status, d.current_customer_id,
			b.room_id, COALESCE(b.binding_role, ''),
			COALESCE(rs.connection_status, 'offline'),
			COALESCE(rs.work_status, 'idle'),
			COALESCE(rs.stop_reason, ''),
			rs.last_heartbeat_at
		FROM inv_devices d
		LEFT JOIN live_device_room_bindings b
		  ON b.device_id=d.id AND b.status='active'
		LEFT JOIN live_device_runtime_state rs
		  ON rs.device_id=d.id
		WHERE d.id=? AND d.current_customer_id=?
		LIMIT 1
	`, deviceID, tenantID).Scan(
		&item.ID,
		&item.SN,
		&item.SKUCode,
		&item.LifecycleStatus,
		&item.TenantID,
		&roomID,
		&item.BindingRole,
		&item.ConnectionStatus,
		&item.WorkStatus,
		&item.StopReason,
		&heartbeat,
	)
	if err != nil {
		return model.LiveDevice{}, err
	}
	if roomID.Valid {
		value := roomID.Int64
		item.RoomID = &value
	}
	if heartbeat.Valid {
		value := heartbeat.Time
		item.LastHeartbeatAt = &value
	}
	return item, nil
}

func (s *Store) GetBoundLiveDeviceByRoom(ctx context.Context, tenantID, roomID int64) (model.LiveDevice, error) {
	var item model.LiveDevice
	var heartbeat sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT
			d.id, d.sn, d.sku_code, d.lifecycle_status, d.current_customer_id,
			b.room_id, b.binding_role,
			COALESCE(rs.connection_status, 'offline'),
			COALESCE(rs.work_status, 'idle'),
			COALESCE(rs.stop_reason, ''),
			rs.last_heartbeat_at
		FROM live_device_room_bindings b
		INNER JOIN inv_devices d ON d.id=b.device_id
		LEFT JOIN live_device_runtime_state rs ON rs.device_id=b.device_id
		WHERE b.tenant_id=? AND b.room_id=? AND b.status='active'
		  AND d.current_customer_id=?
		ORDER BY CASE WHEN b.binding_role='primary' THEN 0 ELSE 1 END, b.id DESC
		LIMIT 1
	`, tenantID, roomID, tenantID).Scan(
		&item.ID,
		&item.SN,
		&item.SKUCode,
		&item.LifecycleStatus,
		&item.TenantID,
		&item.RoomID,
		&item.BindingRole,
		&item.ConnectionStatus,
		&item.WorkStatus,
		&item.StopReason,
		&heartbeat,
	)
	if err != nil {
		return model.LiveDevice{}, err
	}
	if heartbeat.Valid {
		value := heartbeat.Time
		item.LastHeartbeatAt = &value
	}
	return item, nil
}

func (s *Store) BindLiveDevice(
	ctx context.Context,
	tenantID, userID, deviceID, roomID int64,
	role string,
) (model.LiveDevice, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "primary"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveDevice{}, err
	}
	defer tx.Rollback()

	// Serialize room/device binding changes inside one tenant so concurrent
	// operators cannot create two active primary bindings for the same room.
	var tenantLock int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE
	`, tenantID).Scan(&tenantLock); err != nil {
		return model.LiveDevice{}, err
	}

	var runningSessionID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM live_runtime_sessions
		WHERE tenant_id=? AND status='running'
		  AND (room_id=? OR device_id=?)
		LIMIT 1
		FOR UPDATE
	`, tenantID, roomID, deviceID).Scan(&runningSessionID)
	if err == nil {
		return model.LiveDevice{}, ErrLiveBindingBusy
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.LiveDevice{}, err
	}

	var customerID sql.NullInt64
	var lifecycle string
	if err := tx.QueryRowContext(ctx, `
		SELECT current_customer_id, lifecycle_status
		FROM inv_devices
		WHERE id=?
		FOR UPDATE
	`, deviceID).Scan(&customerID, &lifecycle); err != nil {
		return model.LiveDevice{}, err
	}
	if !customerID.Valid || customerID.Int64 != tenantID || lifecycle == "SCRAPPED" {
		return model.LiveDevice{}, sql.ErrNoRows
	}

	now := time.Now().UTC()

	// Remember every binding that will be displaced so its runtime state and
	// audit trail can be closed explicitly instead of leaving stale relations.
	displaced := make(map[int64]int64)
	rows, err := tx.QueryContext(ctx, `
		SELECT device_id, room_id
		FROM live_device_room_bindings
		WHERE tenant_id=? AND status='active'
		  AND (device_id=? OR (?='primary' AND room_id=? AND binding_role='primary'))
		FOR UPDATE
	`, tenantID, deviceID, role, roomID)
	if err != nil {
		return model.LiveDevice{}, err
	}
	for rows.Next() {
		var displacedDeviceID, displacedRoomID int64
		if err := rows.Scan(&displacedDeviceID, &displacedRoomID); err != nil {
			rows.Close()
			return model.LiveDevice{}, err
		}
		displaced[displacedDeviceID] = displacedRoomID
	}
	if err := rows.Close(); err != nil {
		return model.LiveDevice{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_device_room_bindings
		SET status='inactive', unbound_at=?, ended_by_user_id=?, updated_at=?
		WHERE tenant_id=? AND device_id=? AND status='active'
	`, now, userID, now, tenantID, deviceID); err != nil {
		return model.LiveDevice{}, err
	}
	if role == "primary" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_device_room_bindings
			SET status='inactive', unbound_at=?, ended_by_user_id=?, updated_at=?
			WHERE tenant_id=? AND room_id=? AND binding_role='primary' AND status='active'
		`, now, userID, now, tenantID, roomID); err != nil {
			return model.LiveDevice{}, err
		}
	}

	for displacedDeviceID, displacedRoomID := range displaced {
		if displacedDeviceID == deviceID && displacedRoomID == roomID {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_device_runtime_state
			SET current_room_id=NULL, work_status='idle', stop_reason='rebound', updated_at=?
			WHERE device_id=? AND tenant_id=?
		`, now, displacedDeviceID, tenantID); err != nil {
			return model.LiveDevice{}, err
		}
		detail, _ := json.Marshal(map[string]any{"room_id": displacedRoomID})
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_runtime_events (
				tenant_id, room_id, device_id, actor_type, actor_user_id,
				event_code, title, detail_json, occurred_at
			) VALUES (?, ?, ?, 'user', ?, 'DEVICE_UNBOUND', '设备已解除直播间绑定', ?, ?)
		`, tenantID, displacedRoomID, displacedDeviceID, userID, detail, now); err != nil {
			return model.LiveDevice{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_device_room_bindings (
			tenant_id, device_id, room_id, binding_role, status,
			bound_at, created_by_user_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?)
	`, tenantID, deviceID, roomID, role, now, userID, now, now); err != nil {
		return model.LiveDevice{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_device_runtime_state (
			device_id, tenant_id, current_room_id, connection_status,
			work_status, stop_reason, created_at, updated_at
		) VALUES (?, ?, ?, 'offline', 'idle', '', ?, ?)
		ON DUPLICATE KEY UPDATE
			tenant_id=VALUES(tenant_id),
			current_room_id=VALUES(current_room_id),
			work_status='idle',
			stop_reason='',
			updated_at=VALUES(updated_at)
	`, deviceID, tenantID, roomID, now, now); err != nil {
		return model.LiveDevice{}, err
	}

	detail, _ := json.Marshal(map[string]any{
		"room_id":      roomID,
		"binding_role": role,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, actor_type, actor_user_id,
			event_code, title, detail_json, occurred_at
		) VALUES (?, ?, ?, 'user', ?, 'DEVICE_BOUND', '设备已绑定直播间', ?, ?)
	`, tenantID, roomID, deviceID, userID, detail, now); err != nil {
		return model.LiveDevice{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveDevice{}, err
	}
	return s.GetLiveDevice(ctx, tenantID, deviceID)
}

func (s *Store) HeartbeatLiveDevice(
	ctx context.Context,
	tenantID, deviceID int64,
	roomID *int64,
	metadata map[string]any,
) (model.LiveDevice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveDevice{}, err
	}
	defer tx.Rollback()

	var customerID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT current_customer_id
		FROM inv_devices
		WHERE id=?
		FOR UPDATE
	`, deviceID).Scan(&customerID); err != nil {
		return model.LiveDevice{}, err
	}
	if !customerID.Valid || customerID.Int64 != tenantID {
		return model.LiveDevice{}, sql.ErrNoRows
	}

	var activeRoom sql.NullInt64
	_ = tx.QueryRowContext(ctx, `
		SELECT room_id
		FROM live_device_room_bindings
		WHERE tenant_id=? AND device_id=? AND status='active'
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, deviceID).Scan(&activeRoom)
	if roomID != nil {
		if !activeRoom.Valid || activeRoom.Int64 != *roomID {
			return model.LiveDevice{}, ErrLiveDeviceNotBound
		}
	} else if activeRoom.Valid {
		value := activeRoom.Int64
		roomID = &value
	}

	var previous string
	err = tx.QueryRowContext(ctx, `
		SELECT connection_status
		FROM live_device_runtime_state
		WHERE device_id=?
		FOR UPDATE
	`, deviceID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.LiveDevice{}, err
	}

	now := time.Now().UTC()
	metaJSON, _ := json.Marshal(metadata)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_device_runtime_state (
			device_id, tenant_id, current_room_id, connection_status,
			work_status, stop_reason, last_heartbeat_at, metadata_json,
			created_at, updated_at
		) VALUES (?, ?, ?, 'online', 'idle', '', ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			tenant_id=VALUES(tenant_id),
			current_room_id=COALESCE(VALUES(current_room_id), current_room_id),
			connection_status='online',
			last_heartbeat_at=VALUES(last_heartbeat_at),
			metadata_json=VALUES(metadata_json),
			updated_at=VALUES(updated_at)
	`, deviceID, tenantID, roomID, now, nullableJSON(metaJSON), now, now); err != nil {
		return model.LiveDevice{}, err
	}

	if previous != "online" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_runtime_events (
				tenant_id, room_id, device_id, actor_type,
				event_code, title, detail_json, occurred_at
			) VALUES (?, ?, ?, 'device', 'DEVICE_CONNECTED', '设备已上线', ?, ?)
		`, tenantID, roomID, deviceID, nullableJSON(metaJSON), now); err != nil {
			return model.LiveDevice{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.LiveDevice{}, err
	}
	return s.GetLiveDevice(ctx, tenantID, deviceID)
}

func (s *Store) ControlLiveDevice(
	ctx context.Context,
	tenantID, userID, deviceID, roomID int64,
	action string,
) (model.LiveDevice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveDevice{}, err
	}
	defer tx.Rollback()

	var boundDeviceID int64
	err = tx.QueryRowContext(ctx, `
		SELECT b.device_id
		FROM live_device_room_bindings b
		INNER JOIN inv_devices d ON d.id=b.device_id
		WHERE b.tenant_id=? AND b.room_id=? AND b.device_id=?
		  AND b.status='active' AND d.current_customer_id=?
		LIMIT 1
		FOR UPDATE
	`, tenantID, roomID, deviceID, tenantID).Scan(&boundDeviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveDevice{}, ErrLiveDeviceNotBound
		}
		return model.LiveDevice{}, err
	}

	previousConnection := "offline"
	previousWork := "idle"
	err = tx.QueryRowContext(ctx, `
		SELECT connection_status, work_status
		FROM live_device_runtime_state
		WHERE device_id=? AND tenant_id=?
		FOR UPDATE
	`, deviceID, tenantID).Scan(&previousConnection, &previousWork)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.LiveDevice{}, err
	}

	now := time.Now().UTC()
	connectionStatus := previousConnection
	workStatus := previousWork
	stopReason := ""
	var currentRoom any = roomID
	var heartbeatAt any = now
	eventCode := ""
	title := ""
	switch action {
	case "connect":
		connectionStatus = "online"
		workStatus = "working"
		eventCode = "DEVICE_CONNECTED"
		title = "设备握手成功"
	case "pause":
		if previousConnection != "online" {
			return model.LiveDevice{}, ErrLiveDeviceOffline
		}
		connectionStatus = "online"
		workStatus = "paused"
		eventCode = "DEVICE_PAUSED"
		title = "设备已暂停"
	case "resume":
		if previousConnection != "online" {
			return model.LiveDevice{}, ErrLiveDeviceOffline
		}
		connectionStatus = "online"
		workStatus = "working"
		eventCode = "DEVICE_RESUMED"
		title = "设备已继续工作"
	case "disconnect":
		connectionStatus = "offline"
		workStatus = "idle"
		stopReason = "manual_disconnect"
		currentRoom = nil
		heartbeatAt = nil
		eventCode = "DEVICE_DISCONNECTED"
		title = "设备已断开"
	default:
		return model.LiveDevice{}, fmt.Errorf("unsupported device control action %q", action)
	}

	metadata, _ := json.Marshal(map[string]any{
		"control_action": action,
		"transport":      "development_control",
		"handshake":      action == "connect",
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_device_runtime_state (
			device_id, tenant_id, current_room_id, connection_status,
			work_status, stop_reason, last_heartbeat_at, metadata_json,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			tenant_id=VALUES(tenant_id),
			current_room_id=VALUES(current_room_id),
			connection_status=VALUES(connection_status),
			work_status=VALUES(work_status),
			stop_reason=VALUES(stop_reason),
			last_heartbeat_at=VALUES(last_heartbeat_at),
			metadata_json=VALUES(metadata_json),
			updated_at=VALUES(updated_at)
	`, deviceID, tenantID, currentRoom, connectionStatus, workStatus, stopReason, heartbeatAt, nullableJSON(metadata), now, now); err != nil {
		return model.LiveDevice{}, err
	}

	detail, _ := json.Marshal(map[string]any{
		"action":              action,
		"previous_connection": previousConnection,
		"previous_work":       previousWork,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, actor_type, actor_user_id,
			event_code, title, detail_json, occurred_at
		) VALUES (?, ?, ?, 'user', ?, ?, ?, ?, ?)
	`, tenantID, roomID, deviceID, userID, eventCode, title, nullableJSON(detail), now); err != nil {
		return model.LiveDevice{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveDevice{}, err
	}
	return s.GetLiveDevice(ctx, tenantID, deviceID)
}

func (s *Store) StartLiveRuntimeSession(
	ctx context.Context,
	tenantID, roomID, userID int64,
	deviceID *int64,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	var existing int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM live_runtime_sessions
		WHERE tenant_id=? AND room_id=? AND status IN ('running','paused')
		ORDER BY id DESC
		LIMIT 1
		FOR UPDATE
	`, tenantID, roomID).Scan(&existing)
	if err == nil {
		return model.LiveRuntimeSession{}, ErrLiveRuntimeAlreadyRunning
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.LiveRuntimeSession{}, err
	}

	// Device is an optional audio-distribution endpoint. Core collection and the
	// paid intelligent-agent runtime can work without a device bound. If a device
	// is explicitly requested, only verify ownership/binding; device heartbeat is
	// not the switch for the AI layer.
	if deviceID != nil {
		var found int64
		err = tx.QueryRowContext(ctx, `
			SELECT b.device_id
			FROM live_device_room_bindings b
			INNER JOIN inv_devices d ON d.id=b.device_id
			WHERE b.tenant_id=? AND b.room_id=? AND b.device_id=?
			  AND b.status='active' AND d.current_customer_id=?
			LIMIT 1
			FOR UPDATE
		`, tenantID, roomID, *deviceID, tenantID).Scan(&found)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return model.LiveRuntimeSession{}, ErrLiveDeviceNotBound
			}
			return model.LiveRuntimeSession{}, err
		}
	}

	buckets, err := ensureLiveQuotaAvailableTx(ctx, tx, tenantID, 1, now)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	var quota uint64
	for _, bucket := range buckets {
		quota += bucket.Available()
	}
	if quota == 0 {
		return model.LiveRuntimeSession{}, ErrLiveQuotaExhausted
	}

	externalID, err := newLiveReference("LIVE")
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_sessions (
			external_id, tenant_id, room_id, device_id, status,
			started_by_user_id, started_at, last_billed_at,
			total_billed_seconds, version
		) VALUES (?, ?, ?, ?, 'running', ?, ?, ?, 0, 1)
	`, externalID, tenantID, roomID, deviceID, userID, now, now)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title, occurred_at
		) VALUES (?, ?, ?, ?, 'user', ?, 'AI_RUNTIME_STARTED', 'AI直播伴播已启动', ?)
	`, tenantID, roomID, deviceID, sessionID, userID, now); err != nil {
		return model.LiveRuntimeSession{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, sessionID)
}

func (s *Store) GetLiveRuntimeSession(
	ctx context.Context,
	tenantID, sessionID int64,
) (model.LiveRuntimeSession, error) {
	return scanLiveRuntimeSession(s.db.QueryRowContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.id=? AND s.tenant_id=?
	`, sessionID, tenantID))
}

func (s *Store) GetLiveRuntimeByRoom(
	ctx context.Context,
	tenantID, roomID int64,
) (model.LiveRuntimeSession, error) {
	return scanLiveRuntimeSession(s.db.QueryRowContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.tenant_id=? AND s.room_id=?
		ORDER BY CASE s.status WHEN 'running' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END, s.id DESC
		LIMIT 1
	`, tenantID, roomID))
}

func scanLiveRuntimeSession(scanner interface{ Scan(...any) error }) (model.LiveRuntimeSession, error) {
	var item model.LiveRuntimeSession
	var deviceID, startedBy, stoppedBy sql.NullInt64
	var endedAt sql.NullTime
	err := scanner.Scan(
		&item.ID,
		&item.ExternalID,
		&item.TenantID,
		&item.RoomID,
		&deviceID,
		&item.DeviceSN,
		&item.Status,
		&item.StopReason,
		&startedBy,
		&stoppedBy,
		&item.StartedAt,
		&item.LastBilledAt,
		&endedAt,
		&item.TotalBilledSeconds,
		&item.Version,
	)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if deviceID.Valid {
		value := deviceID.Int64
		item.DeviceID = &value
	}
	if startedBy.Valid {
		value := startedBy.Int64
		item.StartedByUserID = &value
	}
	if stoppedBy.Valid {
		value := stoppedBy.Int64
		item.StoppedByUserID = &value
	}
	if endedAt.Valid {
		value := endedAt.Time
		item.EndedAt = &value
	}
	return item, nil
}

func (s *Store) ListTenantLiveBillingRooms(ctx context.Context, tenantID int64) ([]model.LiveBillingRoomSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.room_id,
		       COALESCE(NULLIF(r.name, ''), CONCAT('直播间 ', s.room_id)),
		       s.id, s.total_billed_seconds, s.started_at
		FROM live_runtime_sessions s
		LEFT JOIN core_rooms r ON r.id=s.room_id AND r.tenant_id=s.tenant_id
		WHERE s.tenant_id=? AND s.status='running'
		ORDER BY s.started_at ASC, s.id ASC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveBillingRoomSummary, 0)
	for rows.Next() {
		var item model.LiveBillingRoomSummary
		if err := rows.Scan(
			&item.RoomID,
			&item.RoomName,
			&item.SessionID,
			&item.BilledSeconds,
			&item.StartedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListRunningLiveRuntimeSessions(ctx context.Context) ([]model.LiveRuntimeSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.status IN ('running','paused')
		ORDER BY s.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveRuntimeSession, 0)
	for rows.Next() {
		item, err := scanLiveRuntimeSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) StopLiveRuntimeSession(
	ctx context.Context,
	tenantID, roomID, userID int64,
	reason string,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockRunningSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveRuntimeSession{}, ErrLiveRuntimeNotRunning
		}
		return model.LiveRuntimeSession{}, err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "manual_stop"
	}
	stoppedBy := userID
	session, err = settleLiveRuntimeTx(ctx, tx, session, true, now, reason, &stoppedBy)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
}

func (s *Store) StopLiveRuntimeSessionWithoutMeter(
	ctx context.Context,
	tenantID, roomID, userID int64,
	reason string,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "manual_stop"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()
	session, err := lockActiveSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveRuntimeSession{}, ErrLiveRuntimeNotRunning
		}
		return model.LiveRuntimeSession{}, err
	}
	stoppedBy := userID
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='stopped', stop_reason=?, stopped_by_user_id=?, ended_at=?, last_billed_at=?,
		    version=version+1, updated_at=?
		WHERE id=? AND status IN ('running','paused')
	`, reason, stoppedBy, now, now, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title, detail_json, occurred_at
		) VALUES (?, ?, ?, ?, 'user', ?, 'AI_RUNTIME_STOPPED', 'AI直播伴播已停止', ?, ?)
	`, tenantID, roomID, session.DeviceID, session.ID, userID,
		mustJSON(map[string]any{"stop_reason": reason, "total_billed_seconds": session.TotalBilledSeconds}), now); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
}

func (s *Store) AbortLiveRuntimeSession(
	ctx context.Context,
	sessionID int64,
	reason string,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	if sessionID <= 0 {
		return model.LiveRuntimeSession{}, errors.New("invalid runtime session")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "core_runtime_reset"
	}
	abortTitle := "付费AI运行已中止"
	switch strings.TrimSpace(reason) {
	case "core_restart", "core_runtime_reset":
		abortTitle = "Core重启，付费AI未自动恢复"
	case "room_missing":
		abortTitle = "直播间已不存在，付费AI已停止"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()
	session, err := lockLiveRuntimeSession(ctx, tx, sessionID)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if session.Status == "running" || session.Status == "paused" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_runtime_sessions
			SET status='stopped', stop_reason=?, ended_at=?, last_billed_at=?,
			    version=version+1, updated_at=?
			WHERE id=? AND status IN ('running','paused')
		`, reason, now, now, now, session.ID); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_runtime_events (
				tenant_id, room_id, device_id, session_id,
				actor_type, event_code, title, detail_json, occurred_at
			) VALUES (?, ?, ?, ?, 'system', 'AI_RUNTIME_ABORTED', ?, ?, ?)
		`, session.TenantID, session.RoomID, session.DeviceID, session.ID,
			abortTitle, mustJSON(map[string]any{"stop_reason": reason, "total_billed_seconds": session.TotalBilledSeconds}), now); err != nil {
			return model.LiveRuntimeSession{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	// A Core reset invalidates every unfinished lease from the old Core lifecycle.
	// Cancelling releases reserved quota and deliberately charges zero seconds.
	if _, err := s.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, "", 0, true, false, now); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, session.TenantID, session.ID)
}

func (s *Store) ReconcileLiveRuntimeSession(
	ctx context.Context,
	sessionID int64,
	roomLive bool,
	roomStoppedAt *time.Time,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockLiveRuntimeSession(ctx, tx, sessionID)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if session.Status != "running" {
		if err := tx.Commit(); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		return session, nil
	}

	stopReason := ""
	settleAt := now
	if !roomLive {
		stopReason = "room_offline"
		if roomStoppedAt != nil && roomStoppedAt.Before(settleAt) {
			settleAt = *roomStoppedAt
		}
	} else if session.DeviceID != nil {
		var connection string
		var heartbeat sql.NullTime
		err := tx.QueryRowContext(ctx, `
			SELECT connection_status, last_heartbeat_at
			FROM live_device_runtime_state
			WHERE device_id=? AND tenant_id=?
			FOR UPDATE
		`, *session.DeviceID, session.TenantID).Scan(&connection, &heartbeat)
		if err != nil || connection != "online" || !heartbeat.Valid ||
			heartbeat.Time.Before(now.Add(-liveDeviceHeartbeatTimeout)) {
			stopReason = "device_offline"
			if heartbeat.Valid {
				deviceStopAt := heartbeat.Time.Add(liveDeviceHeartbeatTimeout)
				if deviceStopAt.Before(settleAt) {
					settleAt = deviceStopAt
				}
			}
		}
	}

	session, err = settleLiveRuntimeTx(ctx, tx, session, stopReason == "", settleAt, stopReason, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, session.TenantID, session.ID)
}

func (s *Store) ReconcileLiveRuntimeMeter(
	ctx context.Context,
	sessionID int64,
	roomLive bool,
	coreWorkingSeconds uint64,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockLiveRuntimeSession(ctx, tx, sessionID)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if session.Status == "paused" {
		if roomLive {
			if err := tx.Commit(); err != nil {
				return model.LiveRuntimeSession{}, err
			}
			return session, nil
		}
		session, err = stopPausedLiveRuntimeTx(ctx, tx, session, now, "room_offline", nil)
		if err != nil {
			return model.LiveRuntimeSession{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		return s.GetLiveRuntimeSession(ctx, session.TenantID, session.ID)
	}
	if session.Status != "running" {
		if err := tx.Commit(); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		return session, nil
	}

	stopReason := ""
	if !roomLive {
		stopReason = "room_offline"
	}
	session, err = settleLiveRuntimeMeterTx(
		ctx,
		tx,
		session,
		coreWorkingSeconds,
		roomLive,
		now,
		stopReason,
		nil,
	)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, session.TenantID, session.ID)
}

func (s *Store) PauseLiveRuntimeSessionMeter(
	ctx context.Context,
	tenantID, roomID, userID int64,
	coreWorkingSeconds uint64,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockRunningSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveRuntimeSession{}, ErrLiveRuntimeNotRunning
		}
		return model.LiveRuntimeSession{}, err
	}
	session, err = settleLiveRuntimeMeterTx(
		ctx, tx, session, coreWorkingSeconds, true, now, "", nil,
	)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if session.Status != "running" {
		if err := tx.Commit(); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='paused', stop_reason='', last_billed_at=?,
		    version=version+1, updated_at=?
		WHERE id=? AND status='running'
	`, now, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title, occurred_at
		) VALUES (?, ?, ?, ?, 'user', ?, 'AI_RUNTIME_PAUSED', 'AI直播伴播已暂停', ?)
	`, tenantID, roomID, session.DeviceID, session.ID, userID, now); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
}

func (s *Store) StopLiveRuntimeSessionMeter(
	ctx context.Context,
	tenantID, roomID, userID int64,
	reason string,
	coreWorkingSeconds uint64,
	now time.Time,
) (model.LiveRuntimeSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	defer tx.Rollback()

	session, err := lockActiveSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveRuntimeSession{}, ErrLiveRuntimeNotRunning
		}
		return model.LiveRuntimeSession{}, err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "manual_stop"
	}
	stoppedBy := userID
	if session.Status == "paused" {
		session, err = stopPausedLiveRuntimeTx(ctx, tx, session, now, reason, &stoppedBy)
	} else {
		session, err = settleLiveRuntimeMeterTx(
			ctx, tx, session, coreWorkingSeconds, false, now, reason, &stoppedBy,
		)
	}
	if err != nil {
		return model.LiveRuntimeSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return s.GetLiveRuntimeSession(ctx, tenantID, session.ID)
}

func stopPausedLiveRuntimeTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.LiveRuntimeSession,
	now time.Time,
	stopReason string,
	stoppedBy *int64,
) (model.LiveRuntimeSession, error) {
	if stopReason == "" {
		stopReason = "stopped"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='stopped', stop_reason=?, stopped_by_user_id=?, ended_at=?,
		    last_billed_at=?, version=version+1, updated_at=?
		WHERE id=? AND status='paused'
	`, stopReason, stoppedBy, now, now, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	session.Status = "stopped"
	session.StopReason = stopReason
	session.EndedAt = &now
	session.StoppedByUserID = stoppedBy
	session.LastBilledAt = now
	session.Version++

	eventCode := "AI_RUNTIME_STOPPED"
	title := "AI直播伴播已停止"
	if stopReason == "room_offline" {
		eventCode = "ROOM_OFFLINE_STOP"
		title = "直播间掉线，AI已停止"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title,
			detail_json, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.TenantID,
		session.RoomID,
		session.DeviceID,
		session.ID,
		func() string {
			if stoppedBy != nil {
				return "user"
			}
			return "system"
		}(),
		stoppedBy,
		eventCode,
		title,
		mustJSON(map[string]any{
			"stop_reason":          stopReason,
			"total_billed_seconds": session.TotalBilledSeconds,
		}),
		now,
	); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return session, nil
}

func settleLiveRuntimeMeterTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.LiveRuntimeSession,
	coreWorkingSeconds uint64,
	canContinue bool,
	now time.Time,
	stopReason string,
	stoppedBy *int64,
) (model.LiveRuntimeSession, error) {
	if now.Before(session.LastBilledAt) {
		now = session.LastBilledAt
	}

	requested := uint64(0)
	if coreWorkingSeconds > session.TotalBilledSeconds {
		requested = coreWorkingSeconds - session.TotalBilledSeconds
	}
	if requested > 0 {
		usageStartedAt := now.Add(-time.Duration(requested) * time.Second)
		charged, quotaRemaining, err := chargeQuotaTx(
			ctx, tx, session, requested, usageStartedAt,
		)
		if err != nil {
			return model.LiveRuntimeSession{}, err
		}
		session.TotalBilledSeconds += charged
		if charged < requested || !quotaRemaining {
			canContinue = false
			stopReason = "quota_exhausted"
		}
	}

	session.LastBilledAt = now
	if canContinue {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_runtime_sessions
			SET last_billed_at=?, total_billed_seconds=?, version=version+1, updated_at=?
			WHERE id=? AND status='running'
		`, now, session.TotalBilledSeconds, now, session.ID); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		session.Version++
		return session, nil
	}

	if stopReason == "" {
		stopReason = "stopped"
	}
	endedAt := now
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='stopped', stop_reason=?, stopped_by_user_id=?,
		    last_billed_at=?, ended_at=?, total_billed_seconds=?,
		    version=version+1, updated_at=?
		WHERE id=? AND status='running'
	`, stopReason, stoppedBy, now, endedAt, session.TotalBilledSeconds, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	session.Status = "stopped"
	session.StopReason = stopReason
	session.EndedAt = &endedAt
	session.StoppedByUserID = stoppedBy
	session.Version++

	eventCode := "AI_RUNTIME_STOPPED"
	title := "AI直播伴播已停止"
	switch stopReason {
	case "room_offline":
		eventCode = "ROOM_OFFLINE_STOP"
		title = "直播间掉线，AI已停止"
	case "quota_exhausted":
		eventCode = "AI_QUOTA_EXHAUSTED"
		title = "AI时长已用完，直播伴播已停止"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title,
			detail_json, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.TenantID,
		session.RoomID,
		session.DeviceID,
		session.ID,
		func() string {
			if stoppedBy != nil {
				return "user"
			}
			return "system"
		}(),
		stoppedBy,
		eventCode,
		title,
		mustJSON(map[string]any{
			"stop_reason":          stopReason,
			"total_billed_seconds": session.TotalBilledSeconds,
			"core_working_seconds": coreWorkingSeconds,
		}),
		endedAt,
	); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	return session, nil
}

func settleLiveRuntimeTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.LiveRuntimeSession,
	canContinue bool,
	now time.Time,
	stopReason string,
	stoppedBy *int64,
) (model.LiveRuntimeSession, error) {
	if now.Before(session.LastBilledAt) {
		now = session.LastBilledAt
	}
	requested := uint64(now.Sub(session.LastBilledAt) / time.Second)
	charged := uint64(0)
	if requested > 0 {
		var err error
		var quotaRemaining bool
		charged, quotaRemaining, err = chargeQuotaTx(ctx, tx, session, requested, session.LastBilledAt)
		if err != nil {
			return model.LiveRuntimeSession{}, err
		}
		session.TotalBilledSeconds += charged
		session.LastBilledAt = session.LastBilledAt.Add(time.Duration(charged) * time.Second)
		if charged < requested || !quotaRemaining {
			canContinue = false
			stopReason = "quota_exhausted"
		}
	}

	if canContinue {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_runtime_sessions
			SET last_billed_at=?, total_billed_seconds=?, version=version+1, updated_at=?
			WHERE id=? AND status='running'
		`, session.LastBilledAt, session.TotalBilledSeconds, now, session.ID); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		session.Version++
		return session, nil
	}

	if stopReason == "" {
		stopReason = "stopped"
	}
	endedAt := now
	if stopReason == "quota_exhausted" && session.LastBilledAt.Before(now) {
		endedAt = session.LastBilledAt
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_runtime_sessions
		SET status='stopped', stop_reason=?, stopped_by_user_id=?,
		    last_billed_at=?, ended_at=?, total_billed_seconds=?,
		    version=version+1, updated_at=?
		WHERE id=? AND status='running'
	`, stopReason, stoppedBy, session.LastBilledAt, endedAt, session.TotalBilledSeconds, now, session.ID); err != nil {
		return model.LiveRuntimeSession{}, err
	}
	session.Status = "stopped"
	session.StopReason = stopReason
	session.EndedAt = &endedAt
	session.StoppedByUserID = stoppedBy
	session.Version++

	if session.DeviceID != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_device_runtime_state
			SET connection_status=CASE WHEN ?='device_offline' THEN 'offline' ELSE connection_status END,
			    work_status='stopped', stop_reason=?, updated_at=?
			WHERE device_id=? AND tenant_id=?
		`, stopReason, stopReason, now, *session.DeviceID, session.TenantID); err != nil {
			return model.LiveRuntimeSession{}, err
		}
		if stopReason == "device_offline" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO live_runtime_events (
					tenant_id, room_id, device_id, session_id,
					actor_type, event_code, title, occurred_at
				) VALUES (?, ?, ?, ?, 'device', 'DEVICE_DISCONNECTED', '设备已离线', ?)
			`, session.TenantID, session.RoomID, *session.DeviceID, session.ID, now); err != nil {
				return model.LiveRuntimeSession{}, err
			}
		}
	}

	eventCode := "AI_RUNTIME_STOPPED"
	title := "AI直播伴播已停止"
	switch stopReason {
	case "room_offline":
		eventCode = "ROOM_OFFLINE_STOP"
		title = "直播间掉线，AI已停止"
	case "device_offline":
		eventCode = "DEVICE_OFFLINE_STOP"
		title = "设备离线，AI已停止"
	case "quota_exhausted":
		eventCode = "AI_QUOTA_EXHAUSTED"
		title = "AI时长已用完，直播伴播已停止"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title,
			detail_json, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.TenantID,
		session.RoomID,
		session.DeviceID,
		session.ID,
		func() string {
			if stoppedBy != nil {
				return "user"
			}
			return "system"
		}(),
		stoppedBy,
		eventCode,
		title,
		mustJSON(map[string]any{
			"stop_reason":          stopReason,
			"total_billed_seconds": session.TotalBilledSeconds,
		}),
		endedAt,
	); err != nil {
		return model.LiveRuntimeSession{}, err
	}

	return session, nil
}

func chargeQuotaTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.LiveRuntimeSession,
	requested uint64,
	startedAt time.Time,
) (uint64, bool, error) {
	remaining := requested
	charged := uint64(0)
	for remaining > 0 {
		usageAt := startedAt.Add(time.Duration(charged) * time.Second)
		if err := lockTenantAIResourceTx(ctx, tx, session.TenantID); err != nil {
			return 0, false, err
		}
		if _, err := expireTimeCardAssetsTx(ctx, tx, session.TenantID, usageAt); err != nil {
			return 0, false, err
		}
		buckets, err := loadActiveQuotaBucketsTx(ctx, tx, session.TenantID, usageAt)
		if err != nil {
			return 0, false, err
		}
		if len(buckets) == 0 {
			break
		}

		progressed := false
		for _, bucket := range buckets {
			if remaining == 0 {
				break
			}
			available := bucket.Available()
			if available == 0 {
				continue
			}
			bucketUsageAt := startedAt.Add(time.Duration(charged) * time.Second)
			validFor := bucket.ExpiresAt.Sub(bucketUsageAt)
			if validFor <= 0 {
				continue
			}
			use := available
			// Billing is whole-second based. Never consume more seconds from a
			// bucket than can fit before its expiry boundary; the next loop
			// will expire/switch the source and, if necessary, activate one
			// reserve card.
			maxBeforeExpiryDuration := validFor / time.Second
			if validFor%time.Second != 0 {
				maxBeforeExpiryDuration++
			}
			maxBeforeExpiry := uint64(maxBeforeExpiryDuration)
			if use > maxBeforeExpiry {
				use = maxBeforeExpiry
			}
			if use > remaining {
				use = remaining
			}
			after := bucket.Remaining - use
			status := "active"
			if after == 0 && bucket.Reserved == 0 {
				status = "exhausted"
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE quota_buckets
				SET remaining_seconds=?, status=?, updated_at=CURRENT_TIMESTAMP(3)
				WHERE id=?
			`, after, status, bucket.ID); err != nil {
				return 0, false, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE biz_time_card_assets
				SET remaining_seconds=?,
				    status=CASE WHEN ?=0 THEN 'exhausted' ELSE status END,
				    updated_at=CURRENT_TIMESTAMP(3)
				WHERE quota_bucket_id=? AND status='active'
			`, after, after, bucket.ID); err != nil {
				return 0, false, err
			}

			externalID, err := newLiveReference("QLED")
			if err != nil {
				return 0, false, err
			}
			idempotency := fmt.Sprintf(
				"live-ai-%d-%d-%d",
				session.ID,
				startedAt.UnixNano(),
				bucket.ID,
			)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO quota_ledger (
					external_id, tenant_id, bucket_id, change_seconds,
					remaining_before_seconds, remaining_after_seconds,
					business_type, business_id, operator_user_id,
					reason, idempotency_key, occurred_at
				) VALUES (?, ?, ?, ?, ?, ?, 'live_ai_usage', ?, NULL, ?, ?, ?)
			`,
				externalID,
				session.TenantID,
				bucket.ID,
				-int64(use),
				bucket.Remaining,
				after,
				session.ID,
				fmt.Sprintf("直播间 %d AI运行时长消费", session.RoomID),
				idempotency,
				startedAt.Add(time.Duration(charged+use)*time.Second),
			); err != nil {
				return 0, false, err
			}

			remaining -= use
			charged += use
			progressed = true
		}
		if !progressed {
			break
		}
	}

	if charged > 0 {
		externalID, err := newLiveReference("QUSE")
		if err != nil {
			return 0, false, err
		}
		endedAt := startedAt.Add(time.Duration(charged) * time.Second)
		idempotency := fmt.Sprintf("live-usage-%d-%d", session.ID, startedAt.Unix())
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quota_usage_events (
				external_id, tenant_id, room_id, billable_seconds,
				started_at, ended_at, status, idempotency_key
			) VALUES (?, ?, ?, ?, ?, ?, 'posted', ?)
		`,
			externalID,
			session.TenantID,
			session.RoomID,
			charged,
			startedAt,
			endedAt,
			idempotency,
		); err != nil {
			return 0, false, err
		}
		if err := syncCustomerAIResourceAfterQuotaChargeTx(
			ctx,
			tx,
			session.TenantID,
			int64(charged),
			session.RoomID,
			endedAt,
		); err != nil {
			return 0, false, err
		}
	}

	if charged < requested {
		return charged, false, nil
	}
	afterAt := startedAt.Add(time.Duration(charged) * time.Second)
	var hasQuotaAfter bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM quota_buckets
			WHERE tenant_id=?
			  AND status='active'
			  AND effective_at<=?
			  AND expires_at>?
			  AND remaining_seconds>reserved_seconds
		)
	`, session.TenantID, afterAt, afterAt).Scan(&hasQuotaAfter); err != nil {
		return 0, false, err
	}
	return charged, hasQuotaAfter, nil
}

func loadActiveQuotaBucketsTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	now time.Time,
) ([]quotaBucketLock, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, remaining_seconds, reserved_seconds, expires_at
		FROM quota_buckets
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_at <= ?
		  AND expires_at > ?
		  AND remaining_seconds > reserved_seconds
		ORDER BY expires_at ASC, priority ASC, id ASC
		FOR UPDATE
	`, tenantID, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]quotaBucketLock, 0)
	for rows.Next() {
		var item quotaBucketLock
		if err := rows.Scan(&item.ID, &item.Remaining, &item.Reserved, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func lockRunningSessionByRoom(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, roomID int64,
) (model.LiveRuntimeSession, error) {
	return scanLiveRuntimeSession(tx.QueryRowContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.tenant_id=? AND s.room_id=? AND s.status='running'
		ORDER BY s.id DESC
		LIMIT 1
		FOR UPDATE
	`, tenantID, roomID))
}

func lockPausedSessionByRoom(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, roomID int64,
) (model.LiveRuntimeSession, error) {
	return scanLiveRuntimeSession(tx.QueryRowContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.tenant_id=? AND s.room_id=? AND s.status='paused'
		ORDER BY s.id DESC
		LIMIT 1
		FOR UPDATE
	`, tenantID, roomID))
}

func lockActiveSessionByRoom(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, roomID int64,
) (model.LiveRuntimeSession, error) {
	return scanLiveRuntimeSession(tx.QueryRowContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.tenant_id=? AND s.room_id=? AND s.status IN ('running','paused')
		ORDER BY s.id DESC
		LIMIT 1
		FOR UPDATE
	`, tenantID, roomID))
}

func lockLiveRuntimeSession(
	ctx context.Context,
	tx *sql.Tx,
	sessionID int64,
) (model.LiveRuntimeSession, error) {
	return scanLiveRuntimeSession(tx.QueryRowContext(ctx, `
		SELECT
			s.id, s.external_id, s.tenant_id, s.room_id, s.device_id,
			COALESCE(d.sn, ''), s.status, s.stop_reason,
			s.started_by_user_id, s.stopped_by_user_id,
			s.started_at, s.last_billed_at, s.ended_at,
			s.total_billed_seconds, s.version
		FROM live_runtime_sessions s
		LEFT JOIN inv_devices d ON d.id=s.device_id
		WHERE s.id=?
		FOR UPDATE
	`, sessionID))
}

func (s *Store) ListLiveRuntimeEvents(
	ctx context.Context,
	tenantID, roomID int64,
	limit int,
) ([]model.LiveRuntimeEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, tenant_id, room_id, device_id, session_id,
			actor_type, actor_user_id, event_code, title,
			detail_json, occurred_at
		FROM live_runtime_events
		WHERE tenant_id=? AND room_id=?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?
	`, tenantID, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveRuntimeEvent, 0)
	for rows.Next() {
		var item model.LiveRuntimeEvent
		var rid, did, sid, uid sql.NullInt64
		var detail []byte
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&rid,
			&did,
			&sid,
			&item.ActorType,
			&uid,
			&item.EventCode,
			&item.Title,
			&detail,
			&item.OccurredAt,
		); err != nil {
			return nil, err
		}
		if rid.Valid {
			value := rid.Int64
			item.RoomID = &value
		}
		if did.Valid {
			value := did.Int64
			item.DeviceID = &value
		}
		if sid.Valid {
			value := sid.Int64
			item.SessionID = &value
		}
		if uid.Valid {
			value := uid.Int64
			item.ActorUserID = &value
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &item.Detail)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordLiveRuntimeEvent(
	ctx context.Context,
	tenantID, roomID, userID int64,
	input model.RecordLiveRuntimeEventInput,
) (model.LiveRuntimeEvent, error) {
	code := strings.ToUpper(strings.TrimSpace(input.EventCode))
	allowed := map[string]string{
		"QUESTION_ADDED":           "问题已加入待处理",
		"QUESTIONS_MENTIONED":      "待处理问题已@到场控Agent",
		"AGENT_COMMAND_SUBMITTED":  "用户向场控Agent发送指令",
		"AGENT_DRAWER_OPENED":      "用户打开场控Agent协作面板",
		"AGENT_DRAWER_AUTO_OPENED": "场控Agent协作面板自动打开",
		"DEVICE_SIM_STARTED":       "模拟设备开始工作",
		"DEVICE_SIM_PAUSED":        "模拟设备已连接但暂停工作",
		"DEVICE_SIM_CLOSED":        "模拟设备已关闭并离线",
	}
	defaultTitle, ok := allowed[code]
	if !ok {
		return model.LiveRuntimeEvent{}, fmt.Errorf("unsupported live runtime event code")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = defaultTitle
	}
	if len(title) > 160 {
		title = title[:160]
	}
	detail := mustJSON(input.Detail)
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_runtime_events (
			tenant_id, room_id, actor_type, actor_user_id,
			event_code, title, detail_json, occurred_at
		) VALUES (?, ?, 'user', ?, ?, ?, ?, ?)
	`, tenantID, roomID, userID, code, title, nullableJSON(detail), now)
	if err != nil {
		return model.LiveRuntimeEvent{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveRuntimeEvent{}, err
	}
	room := roomID
	user := userID
	return model.LiveRuntimeEvent{
		ID:          id,
		TenantID:    tenantID,
		RoomID:      &room,
		ActorType:   "user",
		ActorUserID: &user,
		EventCode:   code,
		Title:       title,
		Detail:      input.Detail,
		OccurredAt:  now,
	}, nil
}

func nullableJSON(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}
