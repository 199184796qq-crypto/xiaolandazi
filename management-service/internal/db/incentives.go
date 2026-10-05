package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) ListIncentivePrograms(
	ctx context.Context,
	programType string,
) ([]model.IncentiveProgram, error) {
	query := `
		SELECT id, code, name, program_type, status, description, created_at, updated_at
		FROM inc_programs
	`
	args := make([]any, 0, 1)
	if strings.TrimSpace(programType) != "" {
		query += " WHERE program_type=?"
		args = append(args, strings.TrimSpace(programType))
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.IncentiveProgram, 0)
	for rows.Next() {
		var item model.IncentiveProgram
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.ProgramType,
			&item.Status,
			&item.Description,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		versions, err := s.listIncentiveProgramVersions(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		for j := range versions {
			version := versions[j]
			if items[i].LatestVersion == nil || version.VersionNo > items[i].LatestVersion.VersionNo {
				copy := version
				items[i].LatestVersion = &copy
			}
			switch version.LifecycleStatus {
			case "published":
				copy := version
				items[i].ActiveVersion = &copy
			case "draft":
				copy := version
				items[i].DraftVersion = &copy
			}
		}
	}
	return items, nil
}

func (s *Store) GetIncentiveProgram(
	ctx context.Context,
	programID int64,
) (model.IncentiveProgram, error) {
	items, err := s.ListIncentivePrograms(ctx, "")
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	for _, item := range items {
		if item.ID == programID {
			return item, nil
		}
	}
	return model.IncentiveProgram{}, sql.ErrNoRows
}

func (s *Store) listIncentiveProgramVersions(
	ctx context.Context,
	programID int64,
) ([]model.IncentiveProgramVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, program_id, version_no, lifecycle_status, pending_days,
		       effective_from, effective_to, published_at, created_at
		FROM inc_program_versions
		WHERE program_id=?
		ORDER BY version_no ASC
	`, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.IncentiveProgramVersion, 0)
	for rows.Next() {
		var item model.IncentiveProgramVersion
		if err := rows.Scan(
			&item.ID,
			&item.ProgramID,
			&item.VersionNo,
			&item.LifecycleStatus,
			&item.PendingDays,
			&item.EffectiveFrom,
			&item.EffectiveTo,
			&item.PublishedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		rules, err := s.listIncentiveRules(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.Rules = rules
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listIncentiveRules(
	ctx context.Context,
	versionID int64,
) ([]model.IncentiveRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, program_version_id, priority, event_type,
		       COALESCE(CAST(conditions_json AS CHAR), '{}'),
		       action_type, CAST(action_config_json AS CHAR),
		       enabled, created_at, updated_at
		FROM inc_rules
		WHERE program_version_id=?
		ORDER BY priority ASC, id ASC
	`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.IncentiveRule, 0)
	for rows.Next() {
		var item model.IncentiveRule
		if err := rows.Scan(
			&item.ID,
			&item.ProgramVersionID,
			&item.Priority,
			&item.EventType,
			&item.ConditionsJSON,
			&item.ActionType,
			&item.ActionConfigJSON,
			&item.Enabled,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateIncentiveProgram(
	ctx context.Context,
	userID int64,
	input model.IncentiveProgramInput,
) (model.IncentiveProgram, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO inc_programs (code, name, program_type, status, description)
		VALUES (?, ?, ?, 'draft', ?)
	`, input.Code, input.Name, input.ProgramType, input.Description)
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	programID, err := result.LastInsertId()
	if err != nil {
		return model.IncentiveProgram{}, err
	}

	versionID, err := insertIncentiveVersionTx(
		ctx,
		tx,
		programID,
		1,
		input.PendingDays,
		userID,
		input.Rules,
	)
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	_ = versionID

	if err := tx.Commit(); err != nil {
		return model.IncentiveProgram{}, err
	}
	return s.GetIncentiveProgram(ctx, programID)
}

func (s *Store) SaveIncentiveProgramDraft(
	ctx context.Context,
	userID int64,
	programID int64,
	input model.IncentiveProgramInput,
) (model.IncentiveProgram, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE inc_programs
		SET code=?, name=?, program_type=?, description=?
		WHERE id=?
	`, input.Code, input.Name, input.ProgramType, input.Description, programID)
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	if affected == 0 {
		return model.IncentiveProgram{}, sql.ErrNoRows
	}

	var draftID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM inc_program_versions
		WHERE program_id=? AND lifecycle_status='draft'
		ORDER BY version_no DESC
		LIMIT 1
		FOR UPDATE
	`, programID).Scan(&draftID)

	if err == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inc_program_versions
			SET pending_days=?, created_by_user_id=?
			WHERE id=?
		`, input.PendingDays, userID, draftID); err != nil {
			return model.IncentiveProgram{}, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM inc_rules WHERE program_version_id=?", draftID); err != nil {
			return model.IncentiveProgram{}, err
		}
		if err := insertIncentiveRulesTx(ctx, tx, draftID, input.Rules); err != nil {
			return model.IncentiveProgram{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		var versionNo uint32
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(version_no),0)+1
			FROM inc_program_versions
			WHERE program_id=?
		`, programID).Scan(&versionNo); err != nil {
			return model.IncentiveProgram{}, err
		}
		if _, err := insertIncentiveVersionTx(
			ctx,
			tx,
			programID,
			versionNo,
			input.PendingDays,
			userID,
			input.Rules,
		); err != nil {
			return model.IncentiveProgram{}, err
		}
	} else {
		return model.IncentiveProgram{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.IncentiveProgram{}, err
	}
	return s.GetIncentiveProgram(ctx, programID)
}

func (s *Store) PublishIncentiveProgram(
	ctx context.Context,
	userID int64,
	programID int64,
) (model.IncentiveProgram, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.IncentiveProgram{}, err
	}
	defer tx.Rollback()

	var draftID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM inc_program_versions
		WHERE program_id=? AND lifecycle_status='draft'
		ORDER BY version_no DESC
		LIMIT 1
		FOR UPDATE
	`, programID).Scan(&draftID); err != nil {
		return model.IncentiveProgram{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE inc_program_versions
		SET lifecycle_status='retired', effective_to=COALESCE(effective_to, ?)
		WHERE program_id=? AND lifecycle_status='published'
	`, now, programID); err != nil {
		return model.IncentiveProgram{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inc_program_versions
		SET lifecycle_status='published', effective_from=?, effective_to=NULL,
		    published_by_user_id=?, published_at=?
		WHERE id=?
	`, now, userID, now, draftID); err != nil {
		return model.IncentiveProgram{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inc_programs SET status='active' WHERE id=?
	`, programID); err != nil {
		return model.IncentiveProgram{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.IncentiveProgram{}, err
	}
	return s.GetIncentiveProgram(ctx, programID)
}

func insertIncentiveVersionTx(
	ctx context.Context,
	tx *sql.Tx,
	programID int64,
	versionNo uint32,
	pendingDays uint32,
	userID int64,
	rules []model.IncentiveRuleInput,
) (int64, error) {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inc_program_versions (
			program_id, version_no, lifecycle_status, pending_days, created_by_user_id
		)
		VALUES (?, ?, 'draft', ?, ?)
	`, programID, versionNo, pendingDays, userID)
	if err != nil {
		return 0, err
	}
	versionID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := insertIncentiveRulesTx(ctx, tx, versionID, rules); err != nil {
		return 0, err
	}
	return versionID, nil
}

func insertIncentiveRulesTx(
	ctx context.Context,
	tx *sql.Tx,
	versionID int64,
	rules []model.IncentiveRuleInput,
) error {
	for _, rule := range rules {
		conditions := strings.TrimSpace(rule.ConditionsJSON)
		if conditions == "" {
			conditions = "{}"
		}
		actionConfig := strings.TrimSpace(rule.ActionConfigJSON)
		if actionConfig == "" {
			actionConfig = "{}"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inc_rules (
				program_version_id, priority, event_type, conditions_json,
				action_type, action_config_json, enabled
			)
			VALUES (?, ?, ?, CAST(? AS JSON), ?, CAST(? AS JSON), ?)
		`,
			versionID,
			rule.Priority,
			rule.EventType,
			conditions,
			rule.ActionType,
			actionConfig,
			rule.Enabled,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListIncentiveEarnings(ctx context.Context) ([]model.IncentiveEarning, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, external_id, beneficiary_type, beneficiary_id, earning_type,
		       source_order_id, source_refund_id, program_version_id, rule_id,
		       currency, amount_cents, quota_seconds, status, available_at,
		       created_at, updated_at
		FROM inc_earnings
		ORDER BY id DESC
		LIMIT 5000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.IncentiveEarning, 0)
	for rows.Next() {
		var item model.IncentiveEarning
		if err := rows.Scan(
			&item.ID,
			&item.ExternalID,
			&item.BeneficiaryType,
			&item.BeneficiaryID,
			&item.EarningType,
			&item.SourceOrderID,
			&item.SourceRefundID,
			&item.ProgramVersionID,
			&item.RuleID,
			&item.Currency,
			&item.AmountCents,
			&item.QuotaSeconds,
			&item.Status,
			&item.AvailableAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListSettlementBatches(ctx context.Context) ([]model.SettlementBatch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.batch_no, b.beneficiary_type, b.beneficiary_id,
		       b.currency, b.period_start_at, b.period_end_at,
		       b.gross_amount_cents, b.adjustment_amount_cents,
		       b.settlement_amount_cents, b.status, b.created_by_user_id, b.approved_by_user_id,
		       b.approved_at, b.paid_at, b.created_at, b.updated_at,
		       COUNT(i.id)
		FROM inc_settlement_batches b
		LEFT JOIN inc_settlement_items i ON i.settlement_batch_id=b.id
		GROUP BY b.id
		ORDER BY b.id DESC
		LIMIT 2000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SettlementBatch, 0)
	for rows.Next() {
		var item model.SettlementBatch
		if err := rows.Scan(
			&item.ID,
			&item.BatchNo,
			&item.BeneficiaryType,
			&item.BeneficiaryID,
			&item.Currency,
			&item.PeriodStartAt,
			&item.PeriodEndAt,
			&item.GrossAmountCents,
			&item.AdjustmentAmountCents,
			&item.SettlementAmountCents,
			&item.Status,
			&item.CreatedByUserID,
			&item.ApprovedByUserID,
			&item.ApprovedAt,
			&item.PaidAt,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.ItemCount,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateSettlementBatch(
	ctx context.Context,
	userID int64,
	input model.CreateSettlementBatchInput,
) (model.SettlementBatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SettlementBatch{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, amount_cents
		FROM inc_earnings
		WHERE beneficiary_type=?
		  AND beneficiary_id=?
		  AND status='available'
		  AND LEFT(earning_type,9)<>'commerce_'
		  AND created_at >= ?
		  AND created_at < ?
		ORDER BY id ASC
		FOR UPDATE
	`,
		input.BeneficiaryType,
		input.BeneficiaryID,
		input.PeriodStartAt,
		input.PeriodEndAt,
	)
	if err != nil {
		return model.SettlementBatch{}, err
	}

	type earningRow struct {
		id     int64
		amount int64
	}
	earnings := make([]earningRow, 0)
	var gross int64
	for rows.Next() {
		var row earningRow
		if err := rows.Scan(&row.id, &row.amount); err != nil {
			rows.Close()
			return model.SettlementBatch{}, err
		}
		earnings = append(earnings, row)
		gross += row.amount
	}
	if err := rows.Close(); err != nil {
		return model.SettlementBatch{}, err
	}
	if len(earnings) == 0 {
		return model.SettlementBatch{}, sql.ErrNoRows
	}

	batchNo := fmt.Sprintf("SET-%d", time.Now().UTC().UnixNano())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inc_settlement_batches (
			batch_no, beneficiary_type, beneficiary_id, currency,
			period_start_at, period_end_at, gross_amount_cents,
			adjustment_amount_cents, settlement_amount_cents, status, created_by_user_id
		)
		VALUES (?, ?, ?, 'CNY', ?, ?, ?, 0, ?, 'reviewing', ?)
	`,
		batchNo,
		input.BeneficiaryType,
		input.BeneficiaryID,
		input.PeriodStartAt,
		input.PeriodEndAt,
		gross,
		gross,
		userID,
	)
	if err != nil {
		return model.SettlementBatch{}, err
	}
	batchID, err := result.LastInsertId()
	if err != nil {
		return model.SettlementBatch{}, err
	}

	for _, earning := range earnings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inc_settlement_items (settlement_batch_id, earning_id, amount_cents)
			VALUES (?, ?, ?)
		`, batchID, earning.id, earning.amount); err != nil {
			return model.SettlementBatch{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE inc_earnings SET status='settling' WHERE id=?
		`, earning.id); err != nil {
			return model.SettlementBatch{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.SettlementBatch{}, err
	}
	return s.getSettlementBatch(ctx, batchID)
}

func (s *Store) ApproveSettlementBatch(
	ctx context.Context,
	userID int64,
	batchID int64,
) (model.SettlementBatch, error) {
	var status string
	var createdBy sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT status, created_by_user_id
		FROM inc_settlement_batches
		WHERE id=?
	`, batchID).Scan(&status, &createdBy); err != nil {
		return model.SettlementBatch{}, err
	}
	if status != "reviewing" {
		return model.SettlementBatch{}, sql.ErrNoRows
	}
	if createdBy.Valid && createdBy.Int64 == userID {
		return model.SettlementBatch{}, fmt.Errorf("maker_checker_conflict")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE inc_settlement_batches
		SET status='approved', approved_by_user_id=?, approved_at=?
		WHERE id=? AND status='reviewing'
	`, userID, now, batchID)
	if err != nil {
		return model.SettlementBatch{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.SettlementBatch{}, err
	}
	if affected == 0 {
		return model.SettlementBatch{}, sql.ErrNoRows
	}
	return s.getSettlementBatch(ctx, batchID)
}

func (s *Store) PaySettlementBatch(
	ctx context.Context,
	batchID int64,
) (model.SettlementBatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SettlementBatch{}, err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM inc_settlement_batches WHERE id=? FOR UPDATE
	`, batchID).Scan(&status); err != nil {
		return model.SettlementBatch{}, err
	}
	if status != "approved" {
		return model.SettlementBatch{}, fmt.Errorf("batch not approved")
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE inc_settlement_batches SET status='paid', paid_at=? WHERE id=?
	`, now, batchID); err != nil {
		return model.SettlementBatch{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inc_earnings e
		INNER JOIN inc_settlement_items i ON i.earning_id=e.id
		SET e.status='paid'
		WHERE i.settlement_batch_id=?
	`, batchID); err != nil {
		return model.SettlementBatch{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.SettlementBatch{}, err
	}
	return s.getSettlementBatch(ctx, batchID)
}

func (s *Store) RejectSettlementBatch(
	ctx context.Context,
	batchID int64,
) (model.SettlementBatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SettlementBatch{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE inc_settlement_batches
		SET status='rejected'
		WHERE id=? AND status='reviewing'
	`, batchID)
	if err != nil {
		return model.SettlementBatch{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.SettlementBatch{}, err
	}
	if affected == 0 {
		return model.SettlementBatch{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inc_earnings e
		INNER JOIN inc_settlement_items i ON i.earning_id=e.id
		SET e.status='available'
		WHERE i.settlement_batch_id=? AND e.status='settling'
	`, batchID); err != nil {
		return model.SettlementBatch{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.SettlementBatch{}, err
	}
	return s.getSettlementBatch(ctx, batchID)
}

func (s *Store) getSettlementBatch(
	ctx context.Context,
	batchID int64,
) (model.SettlementBatch, error) {
	var item model.SettlementBatch
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id, b.batch_no, b.beneficiary_type, b.beneficiary_id,
		       b.currency, b.period_start_at, b.period_end_at,
		       b.gross_amount_cents, b.adjustment_amount_cents,
		       b.settlement_amount_cents, b.status, b.created_by_user_id, b.approved_by_user_id,
		       b.approved_at, b.paid_at, b.created_at, b.updated_at,
		       COUNT(i.id)
		FROM inc_settlement_batches b
		LEFT JOIN inc_settlement_items i ON i.settlement_batch_id=b.id
		WHERE b.id=?
		GROUP BY b.id
	`, batchID).Scan(
		&item.ID,
		&item.BatchNo,
		&item.BeneficiaryType,
		&item.BeneficiaryID,
		&item.Currency,
		&item.PeriodStartAt,
		&item.PeriodEndAt,
		&item.GrossAmountCents,
		&item.AdjustmentAmountCents,
		&item.SettlementAmountCents,
		&item.Status,
		&item.ApprovedByUserID,
		&item.ApprovedAt,
		&item.PaidAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.ItemCount,
	)
	return item, err
}
