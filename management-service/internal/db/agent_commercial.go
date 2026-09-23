package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type AgentLevelInput struct {
	Code              string
	Name              string
	Status            string
	EntryFeeCents     uint64
	IncludedDevices   uint64
	DeviceDiscountBPS uint64
	ConsumerShareBPS  uint64
	ReserveBPS        uint64
	SettlementCycle   string
	HoldDays          uint64
	OEMEnabled        bool
	Note              string
}

type AgentContractInput struct {
	ContractNo          string
	ExternalContractNo  string
	AgentTenantID       int64
	ParentContractID    *int64
	ContractType        string
	LevelID             *int64
	StartsOn            time.Time
	EndsOn              *time.Time
	ContractAmountCents uint64
	Note                string
}

func (s *Store) ListAgentLevels(ctx context.Context) ([]model.AgentLevel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, code, name, status, entry_fee_cents, included_devices,
			device_discount_bps, consumer_share_bps, reserve_bps,
			settlement_cycle, hold_days, oem_enabled, note,
			created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM crm_agent_levels
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AgentLevel, 0)
	for rows.Next() {
		item, err := scanAgentLevel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type agentLevelScanner interface {
	Scan(...any) error
}

func scanAgentLevel(scanner agentLevelScanner) (model.AgentLevel, error) {
	var item model.AgentLevel
	var createdBy sql.NullInt64
	var updatedBy sql.NullInt64
	var oemEnabled bool
	if err := scanner.Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&item.Status,
		&item.EntryFeeCents,
		&item.IncludedDevices,
		&item.DeviceDiscountBPS,
		&item.ConsumerShareBPS,
		&item.ReserveBPS,
		&item.SettlementCycle,
		&item.HoldDays,
		&oemEnabled,
		&item.Note,
		&createdBy,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.AgentLevel{}, err
	}
	item.OEMEnabled = oemEnabled
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	return item, nil
}

func (s *Store) GetAgentLevel(ctx context.Context, levelID int64) (model.AgentLevel, error) {
	return scanAgentLevel(s.db.QueryRowContext(ctx, `
		SELECT
			id, code, name, status, entry_fee_cents, included_devices,
			device_discount_bps, consumer_share_bps, reserve_bps,
			settlement_cycle, hold_days, oem_enabled, note,
			created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM crm_agent_levels
		WHERE id=?
		LIMIT 1
	`, levelID))
}

func normalizeAgentLevelInput(input AgentLevelInput) AgentLevelInput {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Status = strings.TrimSpace(input.Status)
	input.SettlementCycle = strings.TrimSpace(input.SettlementCycle)
	input.Note = strings.TrimSpace(input.Note)
	if input.Status == "" {
		input.Status = "active"
	}
	if input.SettlementCycle == "" {
		input.SettlementCycle = "monthly"
	}
	return input
}

func (s *Store) CreateAgentLevel(
	ctx context.Context,
	userID int64,
	input AgentLevelInput,
) (model.AgentLevel, error) {
	input = normalizeAgentLevelInput(input)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_agent_levels (
			code, name, status, entry_fee_cents, included_devices,
			device_discount_bps, consumer_share_bps, reserve_bps,
			settlement_cycle, hold_days, oem_enabled, note,
			created_by_user_id, updated_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.Code,
		input.Name,
		input.Status,
		input.EntryFeeCents,
		input.IncludedDevices,
		input.DeviceDiscountBPS,
		input.ConsumerShareBPS,
		input.ReserveBPS,
		input.SettlementCycle,
		input.HoldDays,
		input.OEMEnabled,
		input.Note,
		userID,
		userID,
	)
	if err != nil {
		return model.AgentLevel{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.AgentLevel{}, err
	}
	return s.GetAgentLevel(ctx, id)
}

func (s *Store) UpdateAgentLevel(
	ctx context.Context,
	levelID int64,
	userID int64,
	input AgentLevelInput,
) (model.AgentLevel, error) {
	input = normalizeAgentLevelInput(input)
	result, err := s.db.ExecContext(ctx, `
		UPDATE crm_agent_levels
		SET
			code=?,
			name=?,
			status=?,
			entry_fee_cents=?,
			included_devices=?,
			device_discount_bps=?,
			consumer_share_bps=?,
			reserve_bps=?,
			settlement_cycle=?,
			hold_days=?,
			oem_enabled=?,
			note=?,
			updated_by_user_id=?
		WHERE id=?
	`,
		input.Code,
		input.Name,
		input.Status,
		input.EntryFeeCents,
		input.IncludedDevices,
		input.DeviceDiscountBPS,
		input.ConsumerShareBPS,
		input.ReserveBPS,
		input.SettlementCycle,
		input.HoldDays,
		input.OEMEnabled,
		input.Note,
		userID,
		levelID,
	)
	if err != nil {
		return model.AgentLevel{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.AgentLevel{}, sql.ErrNoRows
	}
	return s.GetAgentLevel(ctx, levelID)
}

func activateDueAgentLevelsTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, agent_tenant_id, effective_at
		FROM crm_agent_level_history
		WHERE status='scheduled'
		  AND effective_at <= UTC_TIMESTAMP(3)
		ORDER BY effective_at ASC, id ASC
		FOR UPDATE
	`)
	if err != nil {
		return err
	}

	type dueAssignment struct {
		id          int64
		agentID     int64
		effectiveAt time.Time
	}
	due := make([]dueAssignment, 0)
	for rows.Next() {
		var item dueAssignment
		if err := rows.Scan(&item.id, &item.agentID, &item.effectiveAt); err != nil {
			rows.Close()
			return err
		}
		due = append(due, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range due {
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_agent_level_history
			SET status='ended', ended_at=?
			WHERE agent_tenant_id=?
			  AND status='active'
			  AND id<>?
		`, item.effectiveAt, item.agentID, item.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_agent_level_history
			SET status='active'
			WHERE id=? AND status='scheduled'
		`, item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AssignAgentLevel(
	ctx context.Context,
	agentTenantID int64,
	levelID int64,
	userID int64,
	effectiveAt time.Time,
	reason string,
) (model.AgentLevelHistory, error) {
	reason = strings.TrimSpace(reason)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentLevelHistory{}, err
	}
	defer tx.Rollback()

	var agentName string
	if err := tx.QueryRowContext(ctx, `
		SELECT name
		FROM mgmt_tenants
		WHERE id=? AND org_type='agent' AND status='active'
		FOR UPDATE
	`, agentTenantID).Scan(&agentName); err != nil {
		return model.AgentLevelHistory{}, err
	}

	var levelStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM crm_agent_levels
		WHERE id=?
		LIMIT 1
	`, levelID).Scan(&levelStatus); err != nil {
		return model.AgentLevelHistory{}, err
	}
	if levelStatus != "active" {
		return model.AgentLevelHistory{}, fmt.Errorf("agent level is not active")
	}

	if err := activateDueAgentLevelsTx(ctx, tx); err != nil {
		return model.AgentLevelHistory{}, err
	}

	var previousLevel sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT level_id
		FROM crm_agent_level_history
		WHERE agent_tenant_id=? AND status='active'
		ORDER BY effective_at DESC, id DESC
		LIMIT 1
	`, agentTenantID).Scan(&previousLevel)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AgentLevelHistory{}, err
	}
	if previousLevel.Valid && previousLevel.Int64 == levelID && !effectiveAt.After(time.Now().UTC()) {
		return model.AgentLevelHistory{}, fmt.Errorf("agent already uses this level")
	}

	status := "scheduled"
	if !effectiveAt.After(time.Now().UTC()) {
		status = "active"
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_agent_level_history
			SET status='ended', ended_at=?
			WHERE agent_tenant_id=? AND status='active'
		`, effectiveAt, agentTenantID); err != nil {
			return model.AgentLevelHistory{}, err
		}
	}

	var previousValue any
	if previousLevel.Valid {
		previousValue = previousLevel.Int64
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO crm_agent_level_history (
			agent_tenant_id, level_id, previous_level_id, status,
			approved_by_user_id, approved_at, effective_at, reason
		)
		VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP(3), ?, ?)
	`,
		agentTenantID,
		levelID,
		previousValue,
		status,
		userID,
		effectiveAt.UTC(),
		reason,
	)
	if err != nil {
		return model.AgentLevelHistory{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.AgentLevelHistory{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AgentLevelHistory{}, err
	}
	return s.GetAgentLevelHistory(ctx, id)
}

func (s *Store) ListAgentLevelHistory(ctx context.Context) ([]model.AgentLevelHistory, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := activateDueAgentLevelsTx(ctx, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, agentLevelHistorySelect+`
		ORDER BY h.effective_at DESC, h.id DESC
		LIMIT 500
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AgentLevelHistory, 0)
	for rows.Next() {
		item, err := scanAgentLevelHistory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const agentLevelHistorySelect = `
	SELECT
		h.id,
		h.agent_tenant_id,
		agent.name,
		h.level_id,
		level.code,
		level.name,
		h.previous_level_id,
		COALESCE(previous.name,''),
		h.status,
		h.approved_by_user_id,
		h.approved_at,
		h.effective_at,
		h.ended_at,
		h.reason,
		h.created_at
	FROM crm_agent_level_history h
	INNER JOIN mgmt_tenants agent ON agent.id=h.agent_tenant_id
	INNER JOIN crm_agent_levels level ON level.id=h.level_id
	LEFT JOIN crm_agent_levels previous ON previous.id=h.previous_level_id
`

func scanAgentLevelHistory(scanner agentLevelScanner) (model.AgentLevelHistory, error) {
	var item model.AgentLevelHistory
	var previousLevelID sql.NullInt64
	var approvedBy sql.NullInt64
	var endedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.AgentTenantID,
		&item.AgentName,
		&item.LevelID,
		&item.LevelCode,
		&item.LevelName,
		&previousLevelID,
		&item.PreviousLevelName,
		&item.Status,
		&approvedBy,
		&item.ApprovedAt,
		&item.EffectiveAt,
		&endedAt,
		&item.Reason,
		&item.CreatedAt,
	); err != nil {
		return model.AgentLevelHistory{}, err
	}
	if previousLevelID.Valid {
		value := previousLevelID.Int64
		item.PreviousLevelID = &value
	}
	if approvedBy.Valid {
		value := approvedBy.Int64
		item.ApprovedByUserID = &value
	}
	if endedAt.Valid {
		value := endedAt.Time
		item.EndedAt = &value
	}
	return item, nil
}

func (s *Store) GetAgentLevelHistory(ctx context.Context, historyID int64) (model.AgentLevelHistory, error) {
	return scanAgentLevelHistory(s.db.QueryRowContext(
		ctx,
		agentLevelHistorySelect+` WHERE h.id=? LIMIT 1`,
		historyID,
	))
}

func (s *Store) ListAgentContracts(ctx context.Context) ([]model.AgentContract, error) {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE crm_agent_contracts
		SET status='expired'
		WHERE status='active'
		  AND ends_on IS NOT NULL
		  AND ends_on < CURRENT_DATE()
	`); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, agentContractSelect+`
		ORDER BY c.created_at DESC, c.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AgentContract, 0)
	for rows.Next() {
		item, err := scanAgentContract(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	for i := range items {
		attachments, err := s.ListAgentContractAttachments(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Attachments = attachments
	}
	return items, nil
}

const agentContractSelect = `
	SELECT
		c.id,
		c.contract_no,
		c.agent_tenant_id,
		agent.name,
		c.parent_contract_id,
		c.contract_type,
		c.status,
		c.level_id,
		COALESCE(level.name,''),
		COALESCE(CAST(c.level_snapshot_json AS CHAR),'{}'),
		c.starts_on,
		c.ends_on,
		c.contract_amount_cents,
		c.note,
		c.signed_at,
		c.created_by_user_id,
		c.updated_by_user_id,
		c.created_at,
		c.updated_at
	FROM crm_agent_contracts c
	INNER JOIN mgmt_tenants agent ON agent.id=c.agent_tenant_id
	LEFT JOIN crm_agent_levels level ON level.id=c.level_id
`

func scanAgentContract(scanner agentLevelScanner) (model.AgentContract, error) {
	var item model.AgentContract
	var parentID sql.NullInt64
	var levelID sql.NullInt64
	var endsOn sql.NullTime
	var signedAt sql.NullTime
	var createdBy sql.NullInt64
	var updatedBy sql.NullInt64
	if err := scanner.Scan(
		&item.ID,
		&item.ContractNo,
		&item.AgentTenantID,
		&item.AgentName,
		&parentID,
		&item.ContractType,
		&item.Status,
		&levelID,
		&item.LevelName,
		&item.LevelSnapshotJSON,
		&item.StartsOn,
		&endsOn,
		&item.ContractAmountCents,
		&item.Note,
		&signedAt,
		&createdBy,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.AgentContract{}, err
	}
	if parentID.Valid {
		value := parentID.Int64
		item.ParentContractID = &value
	}
	if levelID.Valid {
		value := levelID.Int64
		item.LevelID = &value
	}
	if endsOn.Valid {
		value := endsOn.Time
		item.EndsOn = &value
	}
	if signedAt.Valid {
		value := signedAt.Time
		item.SignedAt = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	return item, nil
}

func (s *Store) GetAgentContract(ctx context.Context, contractID int64) (model.AgentContract, error) {
	item, err := scanAgentContract(s.db.QueryRowContext(
		ctx,
		agentContractSelect+` WHERE c.id=? LIMIT 1`,
		contractID,
	))
	if err != nil {
		return model.AgentContract{}, err
	}
	attachments, err := s.ListAgentContractAttachments(ctx, contractID)
	if err != nil {
		return model.AgentContract{}, err
	}
	item.Attachments = attachments
	return item, nil
}

func (s *Store) ListAgentContractAttachments(ctx context.Context, contractID int64) ([]model.AgentContractAttachment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, contract_id, file_name, file_url, content_type, size_bytes, page_order, created_by_user_id, created_at
		FROM crm_agent_contract_attachments
		WHERE contract_id=?
		ORDER BY page_order ASC, id ASC
	`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentContractAttachment, 0)
	for rows.Next() {
		var item model.AgentContractAttachment
		var createdBy sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.ContractID, &item.FileName, &item.FileURL, &item.ContentType,
			&item.SizeBytes, &item.PageOrder, &createdBy, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			value := createdBy.Int64
			item.CreatedByUserID = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateAgentContractAttachment(
	ctx context.Context,
	contractID int64,
	userID int64,
	fileName string,
	fileURL string,
	contentType string,
	sizeBytes uint64,
) (model.AgentContractAttachment, error) {
	var pageOrder int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(page_order), 0) + 1
		FROM crm_agent_contract_attachments
		WHERE contract_id=?
	`, contractID).Scan(&pageOrder); err != nil {
		return model.AgentContractAttachment{}, err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_agent_contract_attachments (
			contract_id, file_name, file_url, content_type, size_bytes, page_order, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, contractID, fileName, fileURL, contentType, sizeBytes, pageOrder, userID)
	if err != nil {
		return model.AgentContractAttachment{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.AgentContractAttachment{}, err
	}
	return s.GetAgentContractAttachment(ctx, id)
}

func (s *Store) GetAgentContractAttachment(ctx context.Context, attachmentID int64) (model.AgentContractAttachment, error) {
	var item model.AgentContractAttachment
	var createdBy sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, contract_id, file_name, file_url, content_type, size_bytes, page_order, created_by_user_id, created_at
		FROM crm_agent_contract_attachments
		WHERE id=?
		LIMIT 1
	`, attachmentID).Scan(
		&item.ID, &item.ContractID, &item.FileName, &item.FileURL, &item.ContentType,
		&item.SizeBytes, &item.PageOrder, &createdBy, &item.CreatedAt,
	)
	if err != nil {
		return model.AgentContractAttachment{}, err
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	return item, nil
}

func (s *Store) DeleteAgentContractAttachment(ctx context.Context, contractID, attachmentID int64) (model.AgentContractAttachment, error) {
	item, err := s.GetAgentContractAttachment(ctx, attachmentID)
	if err != nil {
		return model.AgentContractAttachment{}, err
	}
	if item.ContractID != contractID {
		return model.AgentContractAttachment{}, sql.ErrNoRows
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE a FROM crm_agent_contract_attachments a
		INNER JOIN crm_agent_contracts c ON c.id=a.contract_id
		WHERE a.id=? AND a.contract_id=? AND c.status IN ('draft','pending_signature')
	`, attachmentID, contractID)
	if err != nil {
		return model.AgentContractAttachment{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.AgentContractAttachment{}, fmt.Errorf("contract attachments are locked")
	}
	return item, nil
}

func (s *Store) CreateAgentContract(
	ctx context.Context,
	userID int64,
	input AgentContractInput,
) (model.AgentContract, error) {
	input.ExternalContractNo = strings.TrimSpace(input.ExternalContractNo)
	input.ContractType = strings.TrimSpace(input.ContractType)
	input.Note = strings.TrimSpace(input.Note)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentContract{}, err
	}
	defer tx.Rollback()

	var agentStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM mgmt_tenants
		WHERE id=? AND org_type='agent'
		LIMIT 1
	`, input.AgentTenantID).Scan(&agentStatus); err != nil {
		return model.AgentContract{}, err
	}
	if agentStatus != "active" {
		return model.AgentContract{}, fmt.Errorf("agent is not active")
	}
	if input.LevelID != nil {
		var levelStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM crm_agent_levels WHERE id=? LIMIT 1
		`, *input.LevelID).Scan(&levelStatus); err != nil {
			return model.AgentContract{}, err
		}
		if levelStatus != "active" {
			return model.AgentContract{}, fmt.Errorf("agent level is not active")
		}
	}
	if input.ParentContractID != nil {
		var parentAgentID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT agent_tenant_id FROM crm_agent_contracts WHERE id=? LIMIT 1
		`, *input.ParentContractID).Scan(&parentAgentID); err != nil {
			return model.AgentContract{}, err
		}
		if parentAgentID != input.AgentTenantID {
			return model.AgentContract{}, fmt.Errorf("parent contract belongs to another agent")
		}
	}

	var parentValue any
	if input.ParentContractID != nil {
		parentValue = *input.ParentContractID
	}
	var levelValue any
	if input.LevelID != nil {
		levelValue = *input.LevelID
	}
	var endsValue any
	if input.EndsOn != nil {
		endsValue = input.EndsOn.UTC()
	}
	temporaryContractNo := fmt.Sprintf("TMP-%d-%d", userID, time.Now().UnixNano())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO crm_agent_contracts (
			contract_no, external_contract_no, agent_tenant_id, parent_contract_id,
			contract_type, status, level_id, level_snapshot_json,
			starts_on, ends_on, contract_amount_cents, note,
			created_by_user_id, updated_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, 'draft', ?, CAST('{}' AS JSON), ?, ?, ?, ?, ?, ?)
	`,
		temporaryContractNo,
		input.ExternalContractNo,
		input.AgentTenantID,
		parentValue,
		input.ContractType,
		levelValue,
		input.StartsOn.UTC(),
		endsValue,
		input.ContractAmountCents,
		input.Note,
		userID,
		userID,
	)
	if err != nil {
		return model.AgentContract{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.AgentContract{}, err
	}
	systemContractNo := fmt.Sprintf("CON-%s-%06d", time.Now().UTC().Format("20060102"), id)
	if _, err := tx.ExecContext(ctx, `
		UPDATE crm_agent_contracts SET contract_no=? WHERE id=?
	`, systemContractNo, id); err != nil {
		return model.AgentContract{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AgentContract{}, err
	}
	return s.GetAgentContract(ctx, id)
}

func (s *Store) UpdateAgentContractDraft(
	ctx context.Context,
	contractID int64,
	userID int64,
	input AgentContractInput,
) (model.AgentContract, error) {
	input.ExternalContractNo = strings.TrimSpace(input.ExternalContractNo)
	input.ContractType = strings.TrimSpace(input.ContractType)
	input.Note = strings.TrimSpace(input.Note)

	var status string
	if err := s.db.QueryRowContext(ctx, `
		SELECT status FROM crm_agent_contracts WHERE id=? LIMIT 1
	`, contractID).Scan(&status); err != nil {
		return model.AgentContract{}, err
	}
	if status != "draft" && status != "pending_signature" {
		return model.AgentContract{}, fmt.Errorf("signed contract cannot be edited")
	}

	var parentValue any
	if input.ParentContractID != nil {
		parentValue = *input.ParentContractID
	}
	var levelValue any
	if input.LevelID != nil {
		levelValue = *input.LevelID
	}
	var endsValue any
	if input.EndsOn != nil {
		endsValue = input.EndsOn.UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE crm_agent_contracts
		SET
			external_contract_no=?,
			agent_tenant_id=?,
			parent_contract_id=?,
			contract_type=?,
			level_id=?,
			starts_on=?,
			ends_on=?,
			contract_amount_cents=?,
			note=?,
			updated_by_user_id=?
		WHERE id=?
		  AND status IN ('draft','pending_signature')
	`,
		input.ExternalContractNo,
		input.AgentTenantID,
		parentValue,
		input.ContractType,
		levelValue,
		input.StartsOn.UTC(),
		endsValue,
		input.ContractAmountCents,
		input.Note,
		userID,
		contractID,
	)
	if err != nil {
		return model.AgentContract{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.AgentContract{}, sql.ErrNoRows
	}
	return s.GetAgentContract(ctx, contractID)
}

func contractTransitionAllowed(current, target string) bool {
	switch current {
	case "draft":
		return target == "pending_signature" || target == "void"
	case "pending_signature":
		return target == "signed" || target == "void" || target == "draft"
	case "signed":
		return target == "active" || target == "terminated"
	case "active":
		return target == "expired" || target == "terminated"
	default:
		return false
	}
}

func (s *Store) TransitionAgentContract(
	ctx context.Context,
	contractID int64,
	userID int64,
	targetStatus string,
) (model.AgentContract, error) {
	targetStatus = strings.TrimSpace(targetStatus)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentContract{}, err
	}
	defer tx.Rollback()

	var currentStatus string
	var levelID sql.NullInt64
	var snapshot string
	if err := tx.QueryRowContext(ctx, `
		SELECT status, level_id, COALESCE(CAST(level_snapshot_json AS CHAR),'{}')
		FROM crm_agent_contracts
		WHERE id=?
		FOR UPDATE
	`, contractID).Scan(&currentStatus, &levelID, &snapshot); err != nil {
		return model.AgentContract{}, err
	}
	if !contractTransitionAllowed(currentStatus, targetStatus) {
		return model.AgentContract{}, fmt.Errorf("invalid contract status transition")
	}
	if targetStatus == "signed" {
		var attachmentCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM crm_agent_contract_attachments WHERE contract_id=?
		`, contractID).Scan(&attachmentCount); err != nil {
			return model.AgentContract{}, err
		}
		if attachmentCount == 0 {
			return model.AgentContract{}, fmt.Errorf("contract attachment required")
		}
	}

	if (targetStatus == "signed" || targetStatus == "active") &&
		(snapshot == "" || snapshot == "{}" || snapshot == "null") &&
		levelID.Valid {
		level, err := scanAgentLevel(tx.QueryRowContext(ctx, `
			SELECT
				id, code, name, status, entry_fee_cents, included_devices,
				device_discount_bps, consumer_share_bps, reserve_bps,
				settlement_cycle, hold_days, oem_enabled, note,
				created_by_user_id, updated_by_user_id, created_at, updated_at
			FROM crm_agent_levels
			WHERE id=?
			LIMIT 1
		`, levelID.Int64))
		if err != nil {
			return model.AgentContract{}, err
		}
		raw, err := json.Marshal(level)
		if err != nil {
			return model.AgentContract{}, err
		}
		snapshot = string(raw)
	}

	if targetStatus == "signed" {
		_, err = tx.ExecContext(ctx, `
			UPDATE crm_agent_contracts
			SET
				status=?,
				level_snapshot_json=CAST(? AS JSON),
				signed_at=UTC_TIMESTAMP(3),
				updated_by_user_id=?
			WHERE id=?
		`, targetStatus, snapshot, userID, contractID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE crm_agent_contracts
			SET
				status=?,
				level_snapshot_json=CAST(? AS JSON),
				updated_by_user_id=?
			WHERE id=?
		`, targetStatus, snapshot, userID, contractID)
	}
	if err != nil {
		return model.AgentContract{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AgentContract{}, err
	}
	return s.GetAgentContract(ctx, contractID)
}
