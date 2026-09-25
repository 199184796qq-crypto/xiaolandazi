package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanNotFound = errors.New("live agent plan not found")

func (s *Store) CreateLiveAgentPlan(
	ctx context.Context,
	tenantID int64,
	actorUserID int64,
	input model.CreateLiveAgentPlanInput,
) (model.LiveAgentPlan, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_agent_plans (
			tenant_id, name, description, status, created_by_user_id
		) VALUES (?, ?, ?, 'active', ?)
	`, tenantID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), actorUserID)
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	return s.GetLiveAgentPlan(ctx, tenantID, id)
}

func (s *Store) ListLiveAgentPlans(ctx context.Context, tenantID int64) ([]model.LiveAgentPlan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id, p.tenant_id, p.name, p.description, p.status,
			COUNT(DISTINCT CASE WHEN b.status='active' THEN b.room_id END) AS room_count,
			COUNT(DISTINCT CASE WHEN t.status='active' THEN t.id END) AS term_count,
			p.created_at, p.updated_at
		FROM live_agent_plans p
		LEFT JOIN live_agent_plan_room_bindings b ON b.plan_id=p.id
		LEFT JOIN live_agent_plan_terms t ON t.plan_id=p.id
		WHERE p.tenant_id=? AND p.status <> 'deleted'
		GROUP BY p.id, p.tenant_id, p.name, p.description, p.status, p.created_at, p.updated_at
		ORDER BY p.updated_at DESC, p.id DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveAgentPlan, 0)
	for rows.Next() {
		var item model.LiveAgentPlan
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.Name,
			&item.Description,
			&item.Status,
			&item.RoomCount,
			&item.TermCount,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLiveAgentPlan(ctx context.Context, tenantID, planID int64) (model.LiveAgentPlan, error) {
	var item model.LiveAgentPlan
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, description, status, created_at, updated_at
		FROM live_agent_plans
		WHERE id=? AND tenant_id=? AND status <> 'deleted'
	`, planID, tenantID).Scan(
		&item.ID,
		&item.TenantID,
		&item.Name,
		&item.Description,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlan{}, ErrLiveAgentPlanNotFound
	}
	if err != nil {
		return model.LiveAgentPlan{}, err
	}

	roomRows, err := s.db.QueryContext(ctx, `
		SELECT room_id
		FROM live_agent_plan_room_bindings
		WHERE plan_id=? AND tenant_id=? AND status='active'
		ORDER BY room_id
	`, planID, tenantID)
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	for roomRows.Next() {
		var roomID int64
		if err := roomRows.Scan(&roomID); err != nil {
			roomRows.Close()
			return model.LiveAgentPlan{}, err
		}
		item.RoomIDs = append(item.RoomIDs, roomID)
	}
	if err := roomRows.Err(); err != nil {
		roomRows.Close()
		return model.LiveAgentPlan{}, err
	}
	roomRows.Close()
	item.RoomCount = len(item.RoomIDs)

	terms, err := s.ListLiveAgentPlanTerms(ctx, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	item.Terms = terms
	item.TermCount = len(terms)
	return item, nil
}

func (s *Store) ArchiveLiveAgentPlan(ctx context.Context, tenantID, planID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plans
		SET status='archived', updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND status='active'
	`, planID, tenantID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrLiveAgentPlanNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_room_bindings
		SET status='archived', unbound_at=CURRENT_TIMESTAMP(3), updated_at=CURRENT_TIMESTAMP(3)
		WHERE plan_id=? AND tenant_id=? AND status='active'
	`, planID, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetLiveAgentPlanForRoom(ctx context.Context, tenantID, roomID int64) (model.LiveAgentPlan, error) {
	var planID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT b.plan_id
		FROM live_agent_plan_room_bindings b
		INNER JOIN live_agent_plans p ON p.id=b.plan_id AND p.tenant_id=b.tenant_id
		WHERE b.tenant_id=? AND b.room_id=? AND b.status='active' AND p.status='active'
		ORDER BY b.id DESC
		LIMIT 1
	`, tenantID, roomID).Scan(&planID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlan{}, ErrLiveAgentPlanNotFound
	}
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	return s.GetLiveAgentPlan(ctx, tenantID, planID)
}

func (s *Store) BindRoomToLiveAgentPlan(
	ctx context.Context,
	tenantID, planID, roomID, actorUserID int64,
) (model.LiveAgentPlan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlan{}, err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM live_agent_plans WHERE id=? AND tenant_id=? AND status <> 'deleted'
	`, planID, tenantID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlan{}, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return model.LiveAgentPlan{}, err
	} else if status != "active" {
		return model.LiveAgentPlan{}, ErrLiveAgentPlanNotFound
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_room_bindings
		SET status='replaced', unbound_at=CURRENT_TIMESTAMP(3), updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND room_id=? AND status='active' AND plan_id<>?
	`, tenantID, roomID, planID); err != nil {
		return model.LiveAgentPlan{}, err
	}

	var existingID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM live_agent_plan_room_bindings
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND status='active'
		LIMIT 1
	`, tenantID, planID, roomID).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_room_bindings (
				tenant_id, plan_id, room_id, status, bound_by_user_id
			) VALUES (?, ?, ?, 'active', ?)
		`, tenantID, planID, roomID, actorUserID)
		if err != nil {
			return model.LiveAgentPlan{}, err
		}
	case err != nil:
		return model.LiveAgentPlan{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlan{}, err
	}
	return s.GetLiveAgentPlan(ctx, tenantID, planID)
}

func (s *Store) UnbindRoomFromLiveAgentPlan(ctx context.Context, tenantID, planID, roomID int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_agent_plan_room_bindings
		SET status='unbound', unbound_at=CURRENT_TIMESTAMP(3), updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND plan_id=? AND room_id=? AND status='active'
	`, tenantID, planID, roomID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrLiveAgentPlanNotFound
	}
	return nil
}

func (s *Store) UpsertLiveAgentPlanTerm(
	ctx context.Context,
	tenantID, planID, actorUserID int64,
	input model.UpsertLiveAgentPlanTermInput,
) (model.LiveAgentPlanTerm, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanTerm{}, err
	}
	defer tx.Rollback()

	var planExists int
	if err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM live_agent_plans
		WHERE id=? AND tenant_id=? AND status='active'
	`, planID, tenantID).Scan(&planExists); errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanTerm{}, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return model.LiveAgentPlanTerm{}, err
	}

	canonical := strings.TrimSpace(input.CanonicalText)
	termType := strings.TrimSpace(input.TermType)
	if termType == "" {
		termType = "proper_noun"
	}
	var termID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM live_agent_plan_terms
		WHERE tenant_id=? AND plan_id=? AND canonical_text=? AND status='active'
		LIMIT 1
	`, tenantID, planID, canonical).Scan(&termID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result, insertErr := tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_terms (
				tenant_id, plan_id, canonical_text, term_type, note, status, created_by_user_id
			) VALUES (?, ?, ?, ?, ?, 'active', ?)
		`, tenantID, planID, canonical, termType, strings.TrimSpace(input.Note), actorUserID)
		if insertErr != nil {
			return model.LiveAgentPlanTerm{}, insertErr
		}
		termID, err = result.LastInsertId()
		if err != nil {
			return model.LiveAgentPlanTerm{}, err
		}
	case err != nil:
		return model.LiveAgentPlanTerm{}, err
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_agent_plan_terms
			SET term_type=?, note=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, termType, strings.TrimSpace(input.Note), termID); err != nil {
			return model.LiveAgentPlanTerm{}, err
		}
	}

	observed := strings.TrimSpace(input.ObservedText)
	if observed != "" && observed != canonical {
		source := strings.TrimSpace(input.Source)
		if source == "" {
			source = "manual_correction"
		}
		now := time.Now().UTC()
		result, err := tx.ExecContext(ctx, `
			UPDATE live_agent_plan_term_variants
			SET confirmation_count=confirmation_count+1,
				last_confirmed_at=?,
				source=?,
				updated_at=CURRENT_TIMESTAMP(3)
			WHERE term_id=? AND variant_text=?
		`, now, source, termID, observed)
		if err != nil {
			return model.LiveAgentPlanTerm{}, err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO live_agent_plan_term_variants (
					term_id, variant_text, source, confirmation_count,
					last_confirmed_at, created_by_user_id
				) VALUES (?, ?, ?, 1, ?, ?)
			`, termID, observed, source, now, actorUserID); err != nil {
				return model.LiveAgentPlanTerm{}, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanTerm{}, err
	}
	terms, err := s.ListLiveAgentPlanTerms(ctx, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanTerm{}, err
	}
	for _, term := range terms {
		if term.ID == termID {
			return term, nil
		}
	}
	return model.LiveAgentPlanTerm{}, ErrLiveAgentPlanNotFound
}

func (s *Store) ListLiveAgentPlanTerms(ctx context.Context, tenantID, planID int64) ([]model.LiveAgentPlanTerm, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.id, t.plan_id, t.canonical_text, t.term_type, t.note, t.status,
			t.created_at, t.updated_at,
			v.id, v.variant_text, v.source, v.confirmation_count, v.last_confirmed_at
		FROM live_agent_plan_terms t
		LEFT JOIN live_agent_plan_term_variants v ON v.term_id=t.id
		WHERE t.tenant_id=? AND t.plan_id=? AND t.status='active'
		ORDER BY t.updated_at DESC, t.id DESC, v.confirmation_count DESC, v.id DESC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveAgentPlanTerm, 0)
	index := map[int64]int{}
	for rows.Next() {
		var (
			term                model.LiveAgentPlanTerm
			variantID           sql.NullInt64
			variantText, source sql.NullString
			confirmationCount   sql.NullInt64
			lastConfirmedAt     sql.NullTime
		)
		if err := rows.Scan(
			&term.ID,
			&term.PlanID,
			&term.CanonicalText,
			&term.TermType,
			&term.Note,
			&term.Status,
			&term.CreatedAt,
			&term.UpdatedAt,
			&variantID,
			&variantText,
			&source,
			&confirmationCount,
			&lastConfirmedAt,
		); err != nil {
			return nil, err
		}
		pos, ok := index[term.ID]
		if !ok {
			term.Variants = []model.LiveAgentPlanTermVariant{}
			items = append(items, term)
			pos = len(items) - 1
			index[term.ID] = pos
		}
		if variantID.Valid {
			item := model.LiveAgentPlanTermVariant{
				ID:                variantID.Int64,
				VariantText:       variantText.String,
				Source:            source.String,
				ConfirmationCount: int(confirmationCount.Int64),
			}
			if lastConfirmedAt.Valid {
				item.LastConfirmedAt = lastConfirmedAt.Time
			}
			items[pos].Variants = append(items[pos].Variants, item)
		}
	}
	return items, rows.Err()
}
