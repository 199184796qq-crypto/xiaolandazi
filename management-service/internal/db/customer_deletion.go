package db

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrSharedCustomerTenant = errors.New("同一客户下还有其他账号，需先处理共享资料与账号归属")
	ErrCustomerConfirmation = errors.New("登录账号与当前用户不一致")
)

type CustomerDeletion struct {
	UserID   int64  `json:"user_id"`
	TenantID int64  `json:"-"`
	ActorID  int64  `json:"-"`
	Status   string `json:"status"`
	Retrying bool   `json:"retrying"`
}

func (s *Store) MigrateCustomerDeletions(ctx context.Context) error {
	if err := s.ensurePlanFactCards(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mgmt_customer_deletions (
		user_id BIGINT UNSIGNED PRIMARY KEY,
		tenant_id BIGINT UNSIGNED NOT NULL UNIQUE,
		actor_user_id BIGINT UNSIGNED NOT NULL,
		profile_json JSON NOT NULL,
		status VARCHAR(16) NOT NULL DEFAULT 'pending',
		attempts INT UNSIGNED NOT NULL DEFAULT 0,
		next_attempt_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		archived_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		completed_at DATETIME(3) NULL,
		KEY idx_customer_deletion_pending (status,next_attempt_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

func (s *Store) HasCustomerDeletionGrant(ctx context.Context, employeeID, userID int64) (bool, error) {
	var granted bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM sys_feature_records
		WHERE feature_key='staff-customer-delete-grants' AND record_key=CONCAT(?, ':', ?) AND status='active'
	)`, employeeID, userID).Scan(&granted)
	return granted, err
}

func (s *Store) GetCustomerDeletion(ctx context.Context, userID int64) (CustomerDeletion, error) {
	var job CustomerDeletion
	err := s.db.QueryRowContext(ctx, `SELECT user_id, tenant_id, actor_user_id, status, attempts>0 FROM mgmt_customer_deletions WHERE user_id=?`, userID).
		Scan(&job.UserID, &job.TenantID, &job.ActorID, &job.Status, &job.Retrying)
	return job, err
}

// QueueCustomerDeletion preserves stable user and tenant IDs for all financial,
// entitlement and support records. The committed archive also authorizes retries.
func (s *Store) QueueCustomerDeletion(ctx context.Context, userID, actorID int64, username string) (CustomerDeletion, error) {
	job := CustomerDeletion{UserID: userID, ActorID: actorID, Status: "pending"}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	var currentName, status string
	err = tx.QueryRowContext(ctx, `SELECT tenant_id, username, status FROM mgmt_users WHERE id=? AND role='customer' AND tenant_id IS NOT NULL FOR UPDATE`, userID).
		Scan(&job.TenantID, &currentName, &status)
	if err != nil {
		return job, err
	}
	if username != currentName {
		return job, ErrCustomerConfirmation
	}
	if status == "archived" {
		err = tx.QueryRowContext(ctx, `SELECT actor_user_id,status,attempts>0 FROM mgmt_customer_deletions WHERE user_id=?`, userID).
			Scan(&job.ActorID, &job.Status, &job.Retrying)
		return job, err
	}
	var tenantID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE`, job.TenantID).Scan(&tenantID); err != nil {
		return job, err
	}
	var shared bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mgmt_users WHERE tenant_id=? AND id<>?)
		OR EXISTS(SELECT 1 FROM mgmt_tenants WHERE parent_id=?)`, tenantID, userID, tenantID).Scan(&shared)
	if err != nil {
		return job, err
	}
	if shared {
		return job, ErrSharedCustomerTenant
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_customer_deletions (user_id,tenant_id,actor_user_id,profile_json)
		SELECT u.id,u.tenant_id,?, JSON_OBJECT('username',u.username,'display_name',u.display_name,
			'phone',u.phone,'email',u.email,'status',u.status,'created_at',u.created_at,
			'organization',t.name,'organization_code',t.code)
		FROM mgmt_users u INNER JOIN mgmt_tenants t ON t.id=u.tenant_id WHERE u.id=?`, actorID, userID)
	if err != nil {
		return job, err
	}
	for _, step := range []struct {
		query string
		arg   int64
	}{
		{`UPDATE mgmt_users SET status='archived',password_hash='' WHERE id=?`, userID},
		{`DELETE FROM mgmt_sessions WHERE user_id=?`, userID},
		{`UPDATE mgmt_tenants SET status='archived' WHERE id=?`, tenantID},
		{`UPDATE crm_customer_profiles SET cooperation_status='non_cooperating' WHERE tenant_id=?`, tenantID},
		{`UPDATE live_plan_support_requests SET status='revoked', decided_at=COALESCE(decided_at,CURRENT_TIMESTAMP(3))
		  WHERE tenant_id=? AND status IN ('pending','accepted')`, tenantID},
	} {
		if _, err = tx.ExecContext(ctx, step.query, step.arg); err != nil {
			return job, err
		}
	}
	return job, tx.Commit()
}

func (s *Store) ClaimCustomerDeletion(ctx context.Context) (CustomerDeletion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CustomerDeletion{}, err
	}
	defer tx.Rollback()
	var job CustomerDeletion
	err = tx.QueryRowContext(ctx, `SELECT user_id,tenant_id,actor_user_id,status,attempts>0 FROM mgmt_customer_deletions
		WHERE status='pending' AND next_attempt_at<=CURRENT_TIMESTAMP(3)
		ORDER BY next_attempt_at,user_id LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&job.UserID, &job.TenantID, &job.ActorID, &job.Status, &job.Retrying)
	if err != nil {
		return job, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_customer_deletions
		SET next_attempt_at=TIMESTAMPADD(MINUTE,10,CURRENT_TIMESTAMP(3)) WHERE user_id=?`, job.UserID); err != nil {
		return job, err
	}
	return job, tx.Commit()
}

func (s *Store) RetryCustomerDeletion(ctx context.Context, job CustomerDeletion) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mgmt_customer_deletions SET attempts=attempts+1,
		next_attempt_at=TIMESTAMPADD(MINUTE,1,CURRENT_TIMESTAMP(3)) WHERE user_id=? AND status='pending'`, job.UserID)
	return err
}

type CustomerVoiceClone struct {
	Provider, Model, VoiceID string
}

func (s *Store) CustomerVoiceClones(ctx context.Context, tenantID int64) ([]CustomerVoiceClone, error) {
	// Source markers are written by the clone handlers. Historical versions
	// can retain a clone after its profile was edited or unbound.
	rows, err := s.db.QueryContext(ctx, `WITH candidates AS (
		SELECT provider, COALESCE(config_json->>'$.target_model','') model, voice_id
		FROM voice_profiles WHERE tenant_id=? AND config_json->>'$.source'
			IN ('customer_voice_clone','custom_mainline_auto_clone','customer_voice_design')
		UNION
		SELECT provider, model, voice_id FROM voice_model_bindings WHERE tenant_id=? AND config_json->>'$.source'
			IN ('customer_voice_clone','custom_mainline_auto_clone','customer_voice_design')
		UNION
		SELECT voice_identity_json->>'$.provider', voice_identity_json->>'$.model', voice_identity_json->>'$.voice_id'
		FROM live_agent_plan_versions WHERE tenant_id=? AND voice_identity_json->>'$.source'='clone'
		UNION
		SELECT '', metadata_json->>'$.tts_model', metadata_json->>'$.voice_id'
		FROM media_assets WHERE tenant_id=? AND metadata_json->>'$.source'='clone'
	)
	SELECT COALESCE(c.provider,''),COALESCE(c.model,''),c.voice_id FROM candidates c
	WHERE c.voice_id IS NOT NULL AND c.voice_id<>'' AND c.voice_id<>'null'
		AND NOT EXISTS(SELECT 1 FROM voice_profiles other WHERE other.tenant_id<>? AND other.voice_id=c.voice_id)
		AND NOT EXISTS(SELECT 1 FROM voice_model_bindings other WHERE other.tenant_id<>? AND other.voice_id=c.voice_id)
		AND NOT EXISTS(SELECT 1 FROM live_agent_plan_versions other WHERE other.tenant_id<>? AND other.voice_identity_json->>'$.voice_id'=c.voice_id)
	ORDER BY c.voice_id,c.model`, tenantID, tenantID, tenantID, tenantID, tenantID, tenantID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CustomerVoiceClone
	for rows.Next() {
		var voice CustomerVoiceClone
		if err = rows.Scan(&voice.Provider, &voice.Model, &voice.VoiceID); err != nil {
			return nil, err
		}
		result = append(result, voice)
	}
	return result, rows.Err()
}

// CustomerDeletionIDs reads the authoritative SQL ownership before files are
// removed. Keep historical room IDs so an interrupted cleanup is repeatable.
func (s *Store) CustomerDeletionIDs(ctx context.Context, tenantID int64) (plans, rooms []int64, err error) {
	for _, scope := range []struct {
		query string
		args  []any
		ids   *[]int64
	}{
		{`SELECT id FROM live_agent_plans WHERE tenant_id=?`, []any{tenantID}, &plans},
		{`SELECT id FROM core_rooms WHERE tenant_id=? UNION SELECT room_id FROM mgmt_room_deletions WHERE tenant_id=?`, []any{tenantID, tenantID}, &rooms},
	} {
		rows, err := s.db.QueryContext(ctx, scope.query, scope.args...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, nil, err
			}
			*scope.ids = append(*scope.ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	return plans, rooms, nil
}

func (s *Store) VerifyCustomerDeletionOwner(ctx context.Context, job CustomerDeletion) error {
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mgmt_users u INNER JOIN mgmt_tenants t ON t.id=u.tenant_id
		WHERE u.id=? AND t.id=? AND u.role='customer' AND u.status='archived' AND t.status='archived'
		  AND NOT EXISTS(SELECT 1 FROM mgmt_users other WHERE other.tenant_id=t.id AND other.id<>u.id)
		  AND NOT EXISTS(SELECT 1 FROM mgmt_tenants child WHERE child.parent_id=t.id))`, job.UserID, job.TenantID).Scan(&valid)
	if err == nil && !valid {
		return ErrSharedCustomerTenant
	}
	return err
}

func (s *Store) CustomerDeletionUploadsBusy(ctx context.Context, tenantID int64) (bool, error) {
	var busy bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM live_custom_mainline_uploads
		WHERE tenant_id=? AND (status IN ('queued','processing') OR (status='uploading' AND created_at>TIMESTAMPADD(HOUR,-2,CURRENT_TIMESTAMP(3)))))
		OR EXISTS(SELECT 1 FROM live_speech_analysis_tasks WHERE tenant_id=? AND status NOT IN ('ready','failed'))`, tenantID, tenantID).Scan(&busy)
	return busy, err
}

type CustomerMediaLocation struct {
	ID                  int64
	Driver, Bucket, Key string
}

func (s *Store) CustomerMediaPrefixShared(ctx context.Context, tenantID int64, prefix string) (bool, error) {
	var shared bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE tenant_id<>? AND object_key LIKE ?)`,
		tenantID, prefix+"%").Scan(&shared)
	return shared, err
}

func (s *Store) CustomerMediaLocations(ctx context.Context, tenantID int64) ([]CustomerMediaLocation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,storage_driver,storage_bucket,object_key FROM media_assets WHERE tenant_id=?`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []CustomerMediaLocation
	for rows.Next() {
		var asset CustomerMediaLocation
		if err := rows.Scan(&asset.ID, &asset.Driver, &asset.Bucket, &asset.Key); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (s *Store) CompleteCustomerDeletion(ctx context.Context, job CustomerDeletion, plans []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, plan := range plans {
		if err := deleteLiveAgentPlanData(ctx, tx, job.TenantID, plan); err != nil {
			return err
		}
	}
	// Billing and support are deliberately absent from the deletion set.
	for _, table := range []string{
		"live_agent_plan_edit_drafts", "live_agent_plan_prompts", "live_agent_plan_material_objects",
		"live_agent_plan_material_claims", "live_agent_plan_material_sources", "live_agent_plan_fact_cards",
		"live_agent_plan_fact_edit_receipts",
		"live_agent_plan_context_words", "live_agent_plan_asset_settings", "live_custom_mainline_uploads",
		"live_anchor_style_samples", "live_anchor_style_analysis_versions", "live_anchor_style_analysis_activations", "live_anchor_styles",
		"live_speech_analysis_tasks", "voice_model_bindings", "voice_profiles", "media_assets", "semantic_documents",
	} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE tenant_id=?", job.TenantID); err != nil {
			return fmt.Errorf("purge customer data %s: %w", table, err)
		}
	}
	// Retain a minimal plan identity for the FK-backed support history.
	if _, err = tx.ExecContext(ctx, `DELETE plan FROM live_agent_plans plan WHERE plan.tenant_id=?
		AND NOT EXISTS(SELECT 1 FROM live_plan_support_requests support WHERE support.plan_id=plan.id)`, job.TenantID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plans SET status='archived',description='' WHERE tenant_id=?`, job.TenantID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_users SET avatar_url='' WHERE id=? AND tenant_id=? AND status='archived'`, job.UserID, job.TenantID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_customer_deletions SET status='completed',
		completed_at=CURRENT_TIMESTAMP(3) WHERE user_id=? AND tenant_id=? AND status='pending'`, job.UserID, job.TenantID); err != nil {
		return err
	}
	return tx.Commit()
}
