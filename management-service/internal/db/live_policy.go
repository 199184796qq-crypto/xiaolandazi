package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLivePolicyScopeInvalid = errors.New("invalid live policy scope")

type livePolicyScanner interface {
	Scan(dest ...any) error
}

func normalizeLivePolicyVersionCollections(item *model.LivePolicyVersion) {
	if item.Rules == nil {
		item.Rules = []model.LivePolicyRule{}
	}
	if item.Overrides == nil {
		item.Overrides = []model.LivePolicyOverride{}
	}
	if item.Conflicts == nil {
		item.Conflicts = []model.LivePolicyConflict{}
	}
}

func scanLivePolicyScope(scanner livePolicyScanner) (model.LivePolicyScope, error) {
	var item model.LivePolicyScope
	var tenantID, roomID, currentVersionID sql.NullInt64
	if err := scanner.Scan(
		&item.ID, &item.Layer, &item.ScopeKey, &item.IndustryCode,
		&tenantID, &roomID, &item.Name, &item.Status, &currentVersionID,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return model.LivePolicyScope{}, err
	}
	if tenantID.Valid {
		value := tenantID.Int64
		item.TenantID = &value
	}
	if roomID.Valid {
		value := roomID.Int64
		item.RoomID = &value
	}
	if currentVersionID.Valid {
		value := currentVersionID.Int64
		item.CurrentVersionID = &value
	}
	return item, nil
}

func policyScopeKey(layer, industryCode string, tenantID, roomID int64) (string, error) {
	layer = strings.ToUpper(strings.TrimSpace(layer))
	switch layer {
	case model.LivePolicyLayerL1:
		return "L1:global", nil
	case model.LivePolicyLayerL2:
		industryCode = strings.ToLower(strings.TrimSpace(industryCode))
		if industryCode == "" {
			return "", ErrLivePolicyScopeInvalid
		}
		return "L2:" + industryCode, nil
	case model.LivePolicyLayerL3:
		if tenantID <= 0 || roomID <= 0 {
			return "", ErrLivePolicyScopeInvalid
		}
		return fmt.Sprintf("L3:%d:%d", tenantID, roomID), nil
	default:
		return "", ErrLivePolicyScopeInvalid
	}
}

func (s *Store) ListLivePolicyIndustries(ctx context.Context) ([]model.LivePolicyIndustry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, name, parent_code, status, sort_order, created_at, updated_at
		FROM live_policy_industries
		WHERE status='active'
		ORDER BY sort_order, name, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LivePolicyIndustry, 0)
	for rows.Next() {
		var item model.LivePolicyIndustry
		if err := rows.Scan(
			&item.Code, &item.Name, &item.ParentCode, &item.Status, &item.SortOrder,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLivePolicyIndustry(ctx context.Context, code string) (model.LivePolicyIndustry, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	var item model.LivePolicyIndustry
	err := s.db.QueryRowContext(ctx, `
		SELECT code, name, parent_code, status, sort_order, created_at, updated_at
		FROM live_policy_industries
		WHERE code=?
		LIMIT 1
	`, code).Scan(
		&item.Code, &item.Name, &item.ParentCode, &item.Status, &item.SortOrder,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *Store) UpsertLivePolicyIndustry(
	ctx context.Context,
	userID int64,
	item model.LivePolicyIndustry,
) (model.LivePolicyIndustry, error) {
	item.Code = strings.ToLower(strings.TrimSpace(item.Code))
	item.Name = strings.TrimSpace(item.Name)
	item.ParentCode = strings.ToLower(strings.TrimSpace(item.ParentCode))
	if item.Code == "" || item.Name == "" {
		return model.LivePolicyIndustry{}, ErrLivePolicyScopeInvalid
	}
	if item.Status == "" {
		item.Status = "active"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_policy_industries (
			code, name, parent_code, status, sort_order,
			created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			parent_code=VALUES(parent_code),
			status=VALUES(status),
			sort_order=VALUES(sort_order),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, item.Code, item.Name, item.ParentCode, item.Status, item.SortOrder, userID, userID)
	if err != nil {
		return model.LivePolicyIndustry{}, err
	}
	return s.GetLivePolicyIndustry(ctx, item.Code)
}

func (s *Store) GetTenantLivePolicyIndustry(ctx context.Context, tenantID int64) (string, error) {
	var code string
	err := s.db.QueryRowContext(ctx, `
		SELECT industry_code
		FROM live_policy_tenant_industries
		WHERE tenant_id=?
		LIMIT 1
	`, tenantID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "general", nil
	}
	if err != nil {
		return "", err
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		code = "general"
	}
	return code, nil
}

func (s *Store) BindTenantLivePolicyIndustry(
	ctx context.Context,
	tenantID int64,
	industryCode string,
	userID int64,
) error {
	if tenantID <= 0 {
		return ErrLivePolicyScopeInvalid
	}
	industryCode = strings.ToLower(strings.TrimSpace(industryCode))
	if industryCode == "" {
		industryCode = "general"
	}
	industry, err := s.GetLivePolicyIndustry(ctx, industryCode)
	if err != nil {
		return err
	}
	if industry.Status != "active" {
		return ErrLivePolicyScopeInvalid
	}
	previousIndustry, err := s.GetTenantLivePolicyIndustry(ctx, tenantID)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO live_policy_tenant_industries (
			tenant_id, industry_code, bound_by_user_id
		) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE
			industry_code=VALUES(industry_code),
			bound_by_user_id=VALUES(bound_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, industryCode, userID); err != nil {
		return err
	}

	detailRaw, err := json.Marshal(map[string]any{
		"previous_industry_code": previousIndustry,
		"industry_code":          industryCode,
		"industry_name":          industry.Name,
	})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO live_policy_audit_logs (
			actor_user_id, action, layer, tenant_id, industry_code, detail_json
		) VALUES (?, 'tenant.industry.bind', 'L2', ?, ?, CAST(? AS JSON))
	`, userID, tenantID, industryCode, string(detailRaw)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetLivePolicyScope(
	ctx context.Context,
	layer, industryCode string,
	tenantID, roomID int64,
) (model.LivePolicyScope, error) {
	key, err := policyScopeKey(layer, industryCode, tenantID, roomID)
	if err != nil {
		return model.LivePolicyScope{}, err
	}
	return scanLivePolicyScope(s.db.QueryRowContext(ctx, `
		SELECT id, layer, scope_key, industry_code, tenant_id, room_id,
		       name, status, current_version_id, created_at, updated_at
		FROM live_policy_scopes
		WHERE scope_key=?
		LIMIT 1
	`, key))
}

func (s *Store) GetLivePolicyScopeByID(ctx context.Context, policyID int64) (model.LivePolicyScope, error) {
	return scanLivePolicyScope(s.db.QueryRowContext(ctx, `
		SELECT id, layer, scope_key, industry_code, tenant_id, room_id,
		       name, status, current_version_id, created_at, updated_at
		FROM live_policy_scopes
		WHERE id=?
		LIMIT 1
	`, policyID))
}

func (s *Store) EnsureLivePolicyScope(
	ctx context.Context,
	layer, industryCode string,
	tenantID, roomID, userID int64,
	name string,
) (model.LivePolicyScope, error) {
	layer = strings.ToUpper(strings.TrimSpace(layer))
	industryCode = strings.ToLower(strings.TrimSpace(industryCode))
	key, err := policyScopeKey(layer, industryCode, tenantID, roomID)
	if err != nil {
		return model.LivePolicyScope{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		switch layer {
		case model.LivePolicyLayerL1:
			name = "系统全局规则"
		case model.LivePolicyLayerL2:
			industry, getErr := s.GetLivePolicyIndustry(ctx, industryCode)
			if getErr != nil {
				return model.LivePolicyScope{}, getErr
			}
			name = industry.Name + "行业默认规则"
		case model.LivePolicyLayerL3:
			name = fmt.Sprintf("终端%d直播间%d差异规则", tenantID, roomID)
		}
	}
	var tenantValue, roomValue any
	if layer == model.LivePolicyLayerL3 {
		tenantValue = tenantID
		roomValue = roomID
	}
	if layer != model.LivePolicyLayerL2 {
		industryCode = ""
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO live_policy_scopes (
			layer, scope_key, industry_code, tenant_id, room_id, name,
			status, created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, layer, key, industryCode, tenantValue, roomValue, name, userID, userID)
	if err != nil {
		return model.LivePolicyScope{}, err
	}
	return s.GetLivePolicyScope(ctx, layer, industryCode, tenantID, roomID)
}

func scanLivePolicyVersion(scanner livePolicyScanner) (model.LivePolicyVersion, error) {
	var item model.LivePolicyVersion
	var rulesRaw, overridesRaw, conflictsRaw string
	var sourceVersionID, createdBy, publishedBy sql.NullInt64
	var publishedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.PolicyID, &item.VersionNo, &item.LifecycleStatus, &item.SourceText,
		&rulesRaw, &overridesRaw, &conflictsRaw, &item.Note,
		&sourceVersionID, &createdBy, &publishedBy, &item.CreatedAt, &publishedAt,
	); err != nil {
		return model.LivePolicyVersion{}, err
	}
	item.Rules = []model.LivePolicyRule{}
	item.Overrides = []model.LivePolicyOverride{}
	item.Conflicts = []model.LivePolicyConflict{}
	_ = json.Unmarshal([]byte(rulesRaw), &item.Rules)
	_ = json.Unmarshal([]byte(overridesRaw), &item.Overrides)
	_ = json.Unmarshal([]byte(conflictsRaw), &item.Conflicts)
	normalizeLivePolicyVersionCollections(&item)
	if sourceVersionID.Valid {
		value := sourceVersionID.Int64
		item.SourceVersionID = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if publishedBy.Valid {
		value := publishedBy.Int64
		item.PublishedByUserID = &value
	}
	if publishedAt.Valid {
		value := publishedAt.Time
		item.PublishedAt = &value
	}
	return item, nil
}

func (s *Store) GetLivePolicyVersion(ctx context.Context, versionID int64) (model.LivePolicyVersion, error) {
	return scanLivePolicyVersion(s.db.QueryRowContext(ctx, `
		SELECT id, policy_id, version_no, lifecycle_status, source_text,
		       CAST(rules_json AS CHAR), CAST(overrides_json AS CHAR), CAST(conflicts_json AS CHAR),
		       note, source_version_id, created_by_user_id, published_by_user_id,
		       created_at, published_at
		FROM live_policy_versions
		WHERE id=?
		LIMIT 1
	`, versionID))
}

func (s *Store) ListLivePolicyVersions(ctx context.Context, policyID int64) ([]model.LivePolicyVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, policy_id, version_no, lifecycle_status, source_text,
		       CAST(rules_json AS CHAR), CAST(overrides_json AS CHAR), CAST(conflicts_json AS CHAR),
		       note, source_version_id, created_by_user_id, published_by_user_id,
		       created_at, published_at
		FROM live_policy_versions
		WHERE policy_id=?
		ORDER BY version_no DESC
	`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LivePolicyVersion, 0)
	for rows.Next() {
		item, err := scanLivePolicyVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetActiveLivePolicyVersion(
	ctx context.Context,
	layer, industryCode string,
	tenantID, roomID int64,
) (*model.LivePolicyVersion, error) {
	scope, err := s.GetLivePolicyScope(ctx, layer, industryCode, tenantID, roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if scope.CurrentVersionID == nil {
		return nil, nil
	}
	item, err := s.GetLivePolicyVersion(ctx, *scope.CurrentVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateLivePolicyDraft(
	ctx context.Context,
	userID int64,
	input model.CreateLivePolicyDraftInput,
) (model.LivePolicyVersion, error) {
	scope, err := s.EnsureLivePolicyScope(
		ctx, input.Layer, input.IndustryCode, input.TenantID, input.RoomID,
		userID, input.Name,
	)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	rulesRaw, err := json.Marshal(input.Rules)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	overridesRaw, err := json.Marshal(input.Overrides)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	conflictsRaw, err := json.Marshal(input.Conflicts)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	defer tx.Rollback()

	var lockedScopeID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM live_policy_scopes
		WHERE id=?
		FOR UPDATE
	`, scope.ID).Scan(&lockedScopeID); err != nil {
		return model.LivePolicyVersion{}, err
	}

	var nextVersion uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM live_policy_versions
		WHERE policy_id=?
	`, scope.ID).Scan(&nextVersion); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_policy_versions
		SET lifecycle_status='archived'
		WHERE policy_id=? AND lifecycle_status='draft'
	`, scope.ID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_policy_versions (
			policy_id, version_no, lifecycle_status, source_text,
			rules_json, overrides_json, conflicts_json, note,
			created_by_user_id
		) VALUES (?, ?, 'draft', ?, CAST(? AS JSON), CAST(? AS JSON), CAST(? AS JSON), ?, ?)
	`, scope.ID, nextVersion, input.SourceText,
		string(rulesRaw), string(overridesRaw), string(conflictsRaw), input.Note, userID)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	versionID, err := result.LastInsertId()
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	if err := insertLivePolicyAuditTx(
		ctx, tx, userID, "draft.create", scope, &versionID,
		map[string]any{"version_no": nextVersion},
	); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LivePolicyVersion{}, err
	}
	return s.GetLivePolicyVersion(ctx, versionID)
}

func (s *Store) PublishLivePolicyVersion(ctx context.Context, versionID, userID int64) (model.LivePolicyVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	defer tx.Rollback()

	var policyID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT policy_id FROM live_policy_versions WHERE id=? FOR UPDATE
	`, versionID).Scan(&policyID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	scope, err := scanLivePolicyScope(tx.QueryRowContext(ctx, `
		SELECT id, layer, scope_key, industry_code, tenant_id, room_id,
		       name, status, current_version_id, created_at, updated_at
		FROM live_policy_scopes WHERE id=? FOR UPDATE
	`, policyID))
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_policy_versions
		SET lifecycle_status='archived'
		WHERE policy_id=? AND lifecycle_status='active' AND id<>?
	`, policyID, versionID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_policy_versions
		SET lifecycle_status='active',
		    published_by_user_id=?,
		    published_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND policy_id=?
	`, userID, versionID, policyID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_policy_scopes
		SET current_version_id=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, versionID, userID, policyID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if err := insertLivePolicyAuditTx(ctx, tx, userID, "version.publish", scope, &versionID, nil); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LivePolicyVersion{}, err
	}
	return s.GetLivePolicyVersion(ctx, versionID)
}

func (s *Store) RollbackLivePolicyVersion(ctx context.Context, targetVersionID, userID int64) (model.LivePolicyVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	defer tx.Rollback()

	var policyID int64
	var sourceText, rulesRaw, overridesRaw, conflictsRaw, note string
	if err := tx.QueryRowContext(ctx, `
		SELECT policy_id, source_text,
		       CAST(rules_json AS CHAR), CAST(overrides_json AS CHAR), CAST(conflicts_json AS CHAR), note
		FROM live_policy_versions
		WHERE id=?
		LIMIT 1
	`, targetVersionID).Scan(
		&policyID, &sourceText, &rulesRaw, &overridesRaw, &conflictsRaw, &note,
	); err != nil {
		return model.LivePolicyVersion{}, err
	}
	scope, err := scanLivePolicyScope(tx.QueryRowContext(ctx, `
		SELECT id, layer, scope_key, industry_code, tenant_id, room_id,
		       name, status, current_version_id, created_at, updated_at
		FROM live_policy_scopes WHERE id=? FOR UPDATE
	`, policyID))
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	var nextVersion uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM live_policy_versions
		WHERE policy_id=?
	`, policyID).Scan(&nextVersion); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_policy_versions
		SET lifecycle_status='archived'
		WHERE policy_id=? AND lifecycle_status='active'
	`, policyID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_policy_versions (
			policy_id, version_no, lifecycle_status, source_text,
			rules_json, overrides_json, conflicts_json, note,
			source_version_id, created_by_user_id, published_by_user_id, published_at
		) VALUES (
			?, ?, 'active', ?,
			CAST(? AS JSON), CAST(? AS JSON), CAST(? AS JSON), ?,
			?, ?, ?, CURRENT_TIMESTAMP(3)
		)
	`, policyID, nextVersion, sourceText, rulesRaw, overridesRaw, conflictsRaw,
		"回滚自版本 "+fmt.Sprint(targetVersionID)+"；"+note,
		targetVersionID, userID, userID)
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	newVersionID, err := result.LastInsertId()
	if err != nil {
		return model.LivePolicyVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_policy_scopes
		SET current_version_id=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, newVersionID, userID, policyID); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if err := insertLivePolicyAuditTx(
		ctx, tx, userID, "version.rollback", scope, &newVersionID,
		map[string]any{"source_version_id": targetVersionID},
	); err != nil {
		return model.LivePolicyVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LivePolicyVersion{}, err
	}
	return s.GetLivePolicyVersion(ctx, newVersionID)
}

func (s *Store) GetLivePolicyContext(
	ctx context.Context,
	layer, industryCode string,
	tenantID, roomID int64,
) (model.LivePolicyContext, error) {
	scope, err := s.GetLivePolicyScope(ctx, layer, industryCode, tenantID, roomID)
	if err != nil {
		return model.LivePolicyContext{}, err
	}
	versions, err := s.ListLivePolicyVersions(ctx, scope.ID)
	if err != nil {
		return model.LivePolicyContext{}, err
	}
	result := model.LivePolicyContext{Scope: scope, Versions: versions}
	if scope.CurrentVersionID != nil {
		for i := range versions {
			if versions[i].ID == *scope.CurrentVersionID {
				active := versions[i]
				result.Active = &active
				break
			}
		}
	}
	if strings.EqualFold(layer, model.LivePolicyLayerL2) {
		industry, err := s.GetLivePolicyIndustry(ctx, industryCode)
		if err == nil {
			result.Industry = &industry
		}
	}
	return result, nil
}

func (s *Store) LoadLivePolicyLayers(
	ctx context.Context,
	tenantID, roomID int64,
) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error) {
	industryCode, err := s.GetTenantLivePolicyIndustry(ctx, tenantID)
	if err != nil {
		return "", nil, nil, nil, err
	}
	l1, err := s.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL1, "", 0, 0)
	if err != nil {
		return "", nil, nil, nil, err
	}
	l2, err := s.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL2, industryCode, 0, 0)
	if err != nil {
		return "", nil, nil, nil, err
	}
	if l2 == nil && industryCode != "general" {
		l2, err = s.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL2, "general", 0, 0)
		if err != nil {
			return "", nil, nil, nil, err
		}
	}
	l3, err := s.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL3, "", tenantID, roomID)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return industryCode, l1, l2, l3, nil
}

func (s *Store) SaveLiveRuntimePolicySnapshot(
	ctx context.Context,
	snapshot model.LiveRuntimePolicySnapshot,
) error {
	raw, err := json.Marshal(snapshot.Effective)
	if err != nil {
		return err
	}
	revisionKey := fmt.Sprintf(
		"%s:%d:%d:%d",
		strings.TrimSpace(snapshot.IndustryCode),
		livePolicyVersionIDValue(snapshot.L1VersionID),
		livePolicyVersionIDValue(snapshot.L2VersionID),
		livePolicyVersionIDValue(snapshot.L3VersionID),
	)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO live_runtime_policy_snapshots (
			session_id, tenant_id, room_id, industry_code,
			l1_version_id, l2_version_id, l3_version_id, effective_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON))
		ON DUPLICATE KEY UPDATE
			industry_code=VALUES(industry_code),
			l1_version_id=VALUES(l1_version_id),
			l2_version_id=VALUES(l2_version_id),
			l3_version_id=VALUES(l3_version_id),
			effective_json=VALUES(effective_json)
	`, snapshot.SessionID, snapshot.TenantID, snapshot.RoomID, snapshot.IndustryCode,
		nullableLivePolicyInt64(snapshot.L1VersionID),
		nullableLivePolicyInt64(snapshot.L2VersionID),
		nullableLivePolicyInt64(snapshot.L3VersionID),
		string(raw)); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO live_runtime_policy_revisions (
			session_id, tenant_id, room_id, industry_code,
			l1_version_id, l2_version_id, l3_version_id,
			revision_key, effective_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON))
	`, snapshot.SessionID, snapshot.TenantID, snapshot.RoomID, snapshot.IndustryCode,
		nullableLivePolicyInt64(snapshot.L1VersionID),
		nullableLivePolicyInt64(snapshot.L2VersionID),
		nullableLivePolicyInt64(snapshot.L3VersionID),
		revisionKey,
		string(raw))
	if err != nil {
		return err
	}

	if rows, _ := result.RowsAffected(); rows > 0 {
		detailRaw, marshalErr := json.Marshal(map[string]any{
			"industry_code": snapshot.IndustryCode,
			"l1_version_id": livePolicyVersionIDValue(snapshot.L1VersionID),
			"l2_version_id": livePolicyVersionIDValue(snapshot.L2VersionID),
			"l3_version_id": livePolicyVersionIDValue(snapshot.L3VersionID),
			"revision_key":  revisionKey,
		})
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO live_runtime_events (
				tenant_id, room_id, session_id, actor_type,
				event_code, title, detail_json, occurred_at
			) VALUES (?, ?, ?, 'system', 'POLICY_REVISION_APPLIED',
			          '直播策略版本已切换', CAST(? AS JSON), CURRENT_TIMESTAMP(3))
		`, snapshot.TenantID, snapshot.RoomID, snapshot.SessionID, string(detailRaw)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func livePolicyVersionIDValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func insertLivePolicyAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	action string,
	scope model.LivePolicyScope,
	versionID *int64,
	detail map[string]any,
) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_policy_audit_logs (
			actor_user_id, action, layer, policy_id, version_id,
			tenant_id, room_id, industry_code, detail_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON))
	`, userID, action, scope.Layer, scope.ID, nullableLivePolicyInt64(versionID),
		nullableLivePolicyInt64(scope.TenantID),
		nullableLivePolicyInt64(scope.RoomID),
		scope.IndustryCode, string(raw))
	return err
}

func nullableLivePolicyInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
