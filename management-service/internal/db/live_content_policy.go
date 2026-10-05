package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"time"
)

var ErrContentPolicyConflict = errors.New("更新策略已被其他人员修改，请刷新后重试")
var ErrContentPolicyConsent = errors.New("直播间授权已经撤回，不能保存更新策略")
var ErrContentModeNotAuthorized = errors.New("当前直播间未开通AI动态生成高级模式")

func (s *Store) migrateLiveContentPolicies(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_content_policies (
	 tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 0, room_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	 policy_json TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1,
	 updated_by_user_id BIGINT UNSIGNED NOT NULL, updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	 PRIMARY KEY (tenant_id,room_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_room_content_access (
	 tenant_id BIGINT UNSIGNED NOT NULL, room_id BIGINT UNSIGNED NOT NULL,
	 selected_mode VARCHAR(32) NOT NULL DEFAULT 'ai_pregenerated',
	 dynamic_authorized TINYINT(1) NOT NULL DEFAULT 0, authorization_source VARCHAR(32) NOT NULL DEFAULT 'default',
	 updated_by_user_id BIGINT UNSIGNED NOT NULL, updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	 PRIMARY KEY (tenant_id,room_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	return err
}

func (s *Store) GetLiveContentPolicy(ctx context.Context, tenantID, roomID int64) (model.LiveContentPolicyRecord, error) {
	result := model.LiveContentPolicyRecord{Policy: model.DefaultLiveContentPolicy()}
	load := func(tid, rid int64) (bool, error) {
		var raw string
		var revision int64
		var updated time.Time
		err := s.db.QueryRowContext(ctx, `SELECT policy_json,revision,updated_at FROM live_content_policies WHERE tenant_id=? AND room_id=?`, tid, rid).Scan(&raw, &revision, &updated)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var p model.LiveContentPolicy
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return false, err
		}
		if err = p.Validate(); err != nil {
			return false, err
		}
		result.Policy = p
		result.Revision = revision
		result.UpdatedAt = &updated
		return true, nil
	}
	if _, err := load(0, 0); err != nil {
		return result, err
	}
	result.SystemRevision = result.Revision
	if tenantID > 0 && roomID > 0 {
		result.Revision = 0
		found, err := load(tenantID, roomID)
		result.Overridden = found
		if err != nil {
			return result, err
		}
		mode, modeErr := s.readLiveContentAccess(ctx, tenantID, roomID, result.Policy.ContentMode)
		result.Policy.ContentMode = mode.ContentMode
		result.DynamicAuthorized = mode.DynamicAuthorized
		return result, modeErr
	}
	return result, nil
}

// SaveLiveContentPolicy uses compare-and-swap so an old operator tab cannot
// silently overwrite another operator's update. Audit is in the same transaction.
func (s *Store) SaveLiveContentPolicy(ctx context.Context, tenantID, roomID, userID, expected int64, p model.LiveContentPolicy, inherit bool, dynamic *bool, expectedMode string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if roomID == 0 && (p.ContentMode == model.LiveContentAIDynamic || (dynamic != nil && *dynamic)) {
		return ErrContentModeNotAuthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if roomID > 0 {
		if err := lockUsableRoomTx(ctx, tx, tenantID, roomID); err != nil {
			return err
		}
		var grantID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM live_support_authorizations WHERE tenant_id=? AND room_id=? AND staff_user_id=? AND capability='l3_policy' AND status='active' AND revoked_at IS NULL FOR UPDATE`, tenantID, roomID, userID).Scan(&grantID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrContentPolicyConsent
		}
		if err != nil {
			return err
		}
	}
	var previous string
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT policy_json,revision FROM live_content_policies WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&previous, &revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if revision != expected {
		return ErrContentPolicyConflict
	}
	if roomID > 0 {
		var allowed bool
		var selected string
		err = tx.QueryRowContext(ctx, `SELECT selected_mode,dynamic_authorized FROM live_room_content_access WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&selected, &allowed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// A customer mode switch does not change timing-policy revisions. Check
		// that preference separately so an old operator form cannot revert it.
		if err == nil && model.ResolveLiveContentMode(selected, allowed, "").ContentMode != expectedMode {
			return ErrContentPolicyConflict
		}
		if dynamic != nil {
			allowed = *dynamic
		}
		if p.ContentMode == model.LiveContentAIDynamic && !allowed {
			if dynamic != nil && !*dynamic {
				p.ContentMode = model.LiveContentAIPregenerated
			} else {
				return ErrContentModeNotAuthorized
			}
		}
		source := "default"
		if allowed {
			source = "operator"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO live_room_content_access (tenant_id,room_id,selected_mode,dynamic_authorized,authorization_source,updated_by_user_id) VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE selected_mode=VALUES(selected_mode),dynamic_authorized=VALUES(dynamic_authorized),authorization_source=VALUES(authorization_source),updated_by_user_id=VALUES(updated_by_user_id),updated_at=CURRENT_TIMESTAMP(3)`, tenantID, roomID, p.ContentMode, allowed, source, userID)
		if err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(p)
	if inherit {
		if roomID <= 0 {
			return errors.New("系统默认策略不能删除")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM live_content_policies WHERE tenant_id=? AND room_id=?`, tenantID, roomID)
	} else if revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO live_content_policies (tenant_id,room_id,policy_json,revision,updated_by_user_id) VALUES (?,?,?,1,?)`, tenantID, roomID, string(raw), userID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE live_content_policies SET policy_json=?,revision=revision+1,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=?`, string(raw), userID, tenantID, roomID)
	}
	if err != nil {
		return err
	}
	// Existing immutable support event journal also retains system changes (room 0).
	err = insertLiveSupportEventTx(ctx, tx, "content_policy.update", tenantID, roomID, userID, userID, "l3_policy", map[string]any{"before": previous, "after": p, "inherit": inherit, "dynamic_authorized": dynamic})
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) readLiveContentAccess(ctx context.Context, tenantID, roomID int64, defaultMode string) (model.LiveContentModeAccess, error) {
	mode, source := defaultMode, "default"
	var allowed bool
	err := s.db.QueryRowContext(ctx, `SELECT selected_mode,dynamic_authorized,authorization_source FROM live_room_content_access WHERE tenant_id=? AND room_id=?`, tenantID, roomID).Scan(&mode, &allowed, &source)
	if errors.Is(err, sql.ErrNoRows) {
		// Existing recording rooms predate mode preferences. Infer only from
		// registered media, never from a client-supplied variant key or grant.
		mode, err = s.legacyLiveContentMode(ctx, tenantID, roomID, defaultMode)
	}
	return model.ResolveLiveContentMode(mode, allowed, source), err
}

func (s *Store) legacyLiveContentMode(ctx context.Context, tenantID, roomID int64, fallback string) (string, error) {
	version, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenantID, roomID)
	if errors.Is(err, ErrLiveAgentPlanVersionNotFound) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	var formalAssets []model.MediaAsset
	for _, variant := range version.Variants {
		if !variant.IsFormal {
			continue
		}
		if variant.AudioAssetID <= 0 {
			return fallback, nil
		}
		asset, err := s.GetMediaAsset(ctx, tenantID, variant.AudioAssetID)
		if errors.Is(err, sql.ErrNoRows) {
			return fallback, nil
		}
		if err != nil {
			return "", err
		}
		formalAssets = append(formalAssets, asset)
	}
	return legacyLiveContentModeFromAssets(formalAssets, fallback), nil
}

func legacyLiveContentModeFromAssets(assets []model.MediaAsset, fallback string) string {
	if len(assets) == 0 {
		return fallback
	}
	for _, asset := range assets {
		purpose, _ := asset.Metadata["purpose"].(string)
		if strings.TrimSpace(purpose) != "live_agent_custom_mainline_audio" {
			return fallback
		}
	}
	return model.LiveContentUserAudio
}

func (s *Store) GetLiveContentMode(ctx context.Context, tenantID, roomID int64) (model.LiveContentModeAccess, error) {
	p, err := s.GetLiveContentPolicy(ctx, tenantID, roomID)
	if err != nil {
		return model.LiveContentModeAccess{}, err
	}
	return s.readLiveContentAccess(ctx, tenantID, roomID, p.Policy.ContentMode)
}

func (s *Store) SaveLiveContentMode(ctx context.Context, tenantID, roomID, userID int64, mode string, support bool) error {
	if mode != model.LiveContentAIPregenerated && mode != model.LiveContentUserAudio && mode != model.LiveContentAIDynamic {
		return errors.New("无效的内容模式")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockUsableRoomTx(ctx, tx, tenantID, roomID); err != nil {
		return err
	}
	if support {
		var grantID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM live_support_authorizations WHERE tenant_id=? AND room_id=? AND staff_user_id=? AND capability='l3_policy' AND status='active' AND revoked_at IS NULL FOR UPDATE`, tenantID, roomID, userID).Scan(&grantID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrContentPolicyConsent
		}
		if err != nil {
			return err
		}
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT dynamic_authorized FROM live_room_content_access WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&allowed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if mode == model.LiveContentAIDynamic && !allowed {
		return ErrContentModeNotAuthorized
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_room_content_access (tenant_id,room_id,selected_mode,updated_by_user_id) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE selected_mode=VALUES(selected_mode),updated_by_user_id=VALUES(updated_by_user_id),updated_at=CURRENT_TIMESTAMP(3)`, tenantID, roomID, mode, userID)
	if err != nil {
		return err
	}
	err = insertLiveSupportEventTx(ctx, tx, "content_mode.select", tenantID, roomID, 0, userID, "", map[string]any{"content_mode": mode})
	if err != nil {
		return err
	}
	return tx.Commit()
}
