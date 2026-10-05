package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"livecompanion/management/internal/model"
)

var (
	ErrLiveAgentPlanVersionNotFound = errors.New("live agent plan version not found")
	ErrLiveAgentPlanRoomNotBound    = errors.New("live agent plan room not bound")
)

const liveAgentPlanVersionSelect = `
	SELECT live_agent_plan_versions.id,
	       live_agent_plan_versions.tenant_id,
	       live_agent_plan_versions.plan_id,
	       live_agent_plan_versions.room_id,
	       live_agent_plan_versions.version_no,
	       live_agent_plan_versions.lifecycle_status,
	       live_agent_plan_versions.duration_minutes,
	       live_agent_plan_versions.round_minutes,
	       CAST(live_agent_plan_versions.voice_identity_json AS CHAR),
	       CAST(live_agent_plan_versions.variants_json AS CHAR),
	       CAST(live_agent_plan_versions.generation_context_json AS CHAR),
	       live_agent_plan_versions.created_by_user_id,
	       live_agent_plan_versions.published_by_user_id,
	       live_agent_plan_versions.published_at,
	       live_agent_plan_versions.created_at,
	       live_agent_plan_versions.updated_at
	FROM live_agent_plan_versions
`

type liveAgentPlanVersionScanner interface {
	Scan(dest ...any) error
}

func scanLiveAgentPlanVersion(row liveAgentPlanVersionScanner) (model.LiveAgentPlanVersion, error) {
	var (
		item         model.LiveAgentPlanVersion
		voiceJSON    string
		variantsJSON string
		contextJSON  string
		createdBy    sql.NullInt64
		publishedBy  sql.NullInt64
		publishedAt  sql.NullTime
	)
	if err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.PlanID,
		&item.RoomID,
		&item.VersionNo,
		&item.LifecycleStatus,
		&item.DurationMinutes,
		&item.RoundMinutes,
		&voiceJSON,
		&variantsJSON,
		&contextJSON,
		&createdBy,
		&publishedBy,
		&publishedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(voiceJSON), &item.VoiceIdentity); err != nil {
		return item, fmt.Errorf("decode voice identity: %w", err)
	}
	if err := json.Unmarshal([]byte(variantsJSON), &item.Variants); err != nil {
		return item, fmt.Errorf("decode plan variants: %w", err)
	}
	formalCount := 0
	for index := range item.Variants {
		if item.Variants[index].IsFormal {
			formalCount++
		}
	}
	// Versions saved before workspace restoration stored only formal variants
	// and did not have an is_formal field. Treat all variants in those legacy
	// snapshots as formal so they remain publishable/restorable.
	if formalCount == 0 && len(item.Variants) > 0 {
		for index := range item.Variants {
			item.Variants[index].IsFormal = true
		}
	}
	if contextJSON != "" && contextJSON != "null" {
		if err := json.Unmarshal([]byte(contextJSON), &item.GenerationContext); err != nil {
			return item, fmt.Errorf("decode generation context: %w", err)
		}
	}
	if item.GenerationContext == nil {
		item.GenerationContext = map[string]any{}
	}
	if createdBy.Valid {
		v := createdBy.Int64
		item.CreatedByUserID = &v
	}
	if publishedBy.Valid {
		v := publishedBy.Int64
		item.PublishedByUserID = &v
	}
	if publishedAt.Valid {
		v := publishedAt.Time
		item.PublishedAt = &v
	}
	return item, nil
}

func (s *Store) CreateLiveAgentPlanVersion(
	ctx context.Context,
	tenantID, planID, actorUserID int64,
	input model.CreateLiveAgentPlanVersionInput,
) (model.LiveAgentPlanVersion, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM live_agent_plans
		WHERE tenant_id=? AND id=? AND status='active'
	`, tenantID, planID).Scan(&exists); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if exists == 0 {
		return model.LiveAgentPlanVersion{}, ErrLiveAgentPlanNotFound
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM live_agent_plan_room_bindings
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND status='active'
	`, tenantID, planID, input.RoomID).Scan(&exists); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if exists == 0 {
		return model.LiveAgentPlanVersion{}, ErrLiveAgentPlanRoomNotBound
	}

	voiceJSON, err := json.Marshal(input.VoiceIdentity)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	variantsJSON, err := json.Marshal(input.Variants)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if input.GenerationContext == nil {
		input.GenerationContext = map[string]any{}
	}
	contextJSON, err := json.Marshal(input.GenerationContext)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	defer tx.Rollback()

	var nextVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM live_agent_plan_versions
		WHERE tenant_id=? AND plan_id=? AND room_id=?
	`, tenantID, planID, input.RoomID).Scan(&nextVersion); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_versions
		SET lifecycle_status='superseded'
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND lifecycle_status='draft'
	`, tenantID, planID, input.RoomID); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_versions (
			tenant_id, plan_id, room_id, version_no, lifecycle_status,
			duration_minutes, round_minutes,
			voice_identity_json, variants_json, generation_context_json,
			created_by_user_id
		) VALUES (?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?)
	`,
		tenantID,
		planID,
		input.RoomID,
		nextVersion,
		input.DurationMinutes,
		input.RoundMinutes,
		string(voiceJSON),
		string(variantsJSON),
		string(contextJSON),
		actorUserID,
	)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	versionID, err := result.LastInsertId()
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	return s.GetLiveAgentPlanVersion(ctx, tenantID, planID, versionID)
}

func (s *Store) GetLiveAgentPlanVersion(
	ctx context.Context,
	tenantID, planID, versionID int64,
) (model.LiveAgentPlanVersion, error) {
	item, err := scanLiveAgentPlanVersion(s.db.QueryRowContext(
		ctx,
		liveAgentPlanVersionSelect+` WHERE tenant_id=? AND plan_id=? AND id=?`,
		tenantID,
		planID,
		versionID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanVersion{}, ErrLiveAgentPlanVersionNotFound
	}
	return item, err
}

func (s *Store) ListLiveAgentPlanVersions(
	ctx context.Context,
	tenantID, planID, roomID int64,
) ([]model.LiveAgentPlanVersion, error) {
	rows, err := s.db.QueryContext(
		ctx,
		liveAgentPlanVersionSelect+`
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND lifecycle_status NOT IN ('prepared','abandoned')
		ORDER BY version_no DESC
	`,
		tenantID,
		planID,
		roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanVersion, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListLiveAgentPlanVersionsForPlan(
	ctx context.Context,
	tenantID, planID int64,
) ([]model.LiveAgentPlanVersion, error) {
	rows, err := s.db.QueryContext(
		ctx,
		liveAgentPlanVersionSelect+`
		WHERE tenant_id=? AND plan_id=? AND lifecycle_status NOT IN ('prepared','abandoned')
		ORDER BY updated_at DESC, id DESC
	`,
		tenantID,
		planID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanVersion, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) PublishLiveAgentPlanVersion(
	ctx context.Context,
	tenantID, planID, roomID, versionID, actorUserID int64,
) (model.LiveAgentPlanVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	defer tx.Rollback()

	_, err = scanLiveAgentPlanVersion(tx.QueryRowContext(
		ctx,
		liveAgentPlanVersionSelect+` WHERE tenant_id=? AND plan_id=? AND room_id=? AND id=? AND lifecycle_status NOT IN ('prepared','abandoned') FOR UPDATE`,
		tenantID,
		planID,
		roomID,
		versionID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanVersion{}, ErrLiveAgentPlanVersionNotFound
	}
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_versions
		SET lifecycle_status='superseded'
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND lifecycle_status='published' AND id<>?
	`, tenantID, planID, roomID, versionID); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_versions
		SET lifecycle_status='published',
		    published_by_user_id=?,
		    published_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND id=?
	`, actorUserID, tenantID, planID, roomID, versionID); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_room_plan_publications (
			tenant_id, room_id, plan_id, version_id, published_by_user_id, published_at
		) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP(3))
		ON DUPLICATE KEY UPDATE
			plan_id=VALUES(plan_id),
			version_id=VALUES(version_id),
			published_by_user_id=VALUES(published_by_user_id),
			published_at=VALUES(published_at)
	`, tenantID, roomID, planID, versionID, actorUserID); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	return s.GetLiveAgentPlanVersion(ctx, tenantID, planID, versionID)
}

func (s *Store) GetPublishedLiveAgentPlanVersionForRoom(
	ctx context.Context,
	tenantID, roomID int64,
) (model.LiveAgentPlanVersion, error) {
	item, err := scanLiveAgentPlanVersion(s.db.QueryRowContext(
		ctx,
		liveAgentPlanVersionSelect+`
		INNER JOIN live_agent_room_plan_selections selection
		        ON selection.tenant_id=live_agent_plan_versions.tenant_id
		       AND selection.room_id=live_agent_plan_versions.room_id
		       AND selection.plan_id=live_agent_plan_versions.plan_id
		WHERE selection.tenant_id=? AND selection.room_id=?
		  AND live_agent_plan_versions.lifecycle_status='published'
		ORDER BY live_agent_plan_versions.version_no DESC, live_agent_plan_versions.id DESC
		LIMIT 1
	`,
		tenantID,
		roomID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		// Backward compatibility for rooms published before explicit plan
		// selection was introduced.
		item, err = scanLiveAgentPlanVersion(s.db.QueryRowContext(
			ctx,
			liveAgentPlanVersionSelect+`
			INNER JOIN live_agent_room_plan_publications pub
			        ON pub.version_id=live_agent_plan_versions.id
			INNER JOIN live_agent_plan_room_bindings binding
			        ON binding.tenant_id=live_agent_plan_versions.tenant_id
			       AND binding.room_id=live_agent_plan_versions.room_id
			       AND binding.plan_id=live_agent_plan_versions.plan_id
			WHERE pub.tenant_id=? AND pub.room_id=?
			  AND binding.status='active'
			  AND live_agent_plan_versions.lifecycle_status='published'
			LIMIT 1
		`,
			tenantID,
			roomID,
		))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanVersion{}, ErrLiveAgentPlanVersionNotFound
	}
	return item, err
}
