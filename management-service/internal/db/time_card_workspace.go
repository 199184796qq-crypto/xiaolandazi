package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"livecompanion/management/internal/model"
)

var ErrTimeCardCatalogChanged = errors.New("时长卡列表已更新，请刷新后重试")
var ErrTimeCardArchived = errors.New("时长卡已归档")
var chinaTime = time.FixedZone("Asia/Shanghai", 8*3600)

func nextInternalSequence(ctx context.Context, tx *sql.Tx, namespace string, limit int64) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO internal_code_sequences (namespace) VALUES (?)
		ON DUPLICATE KEY UPDATE sequence_value=sequence_value
	`, namespace); err != nil {
		return 0, err
	}
	var last int64
	if err := tx.QueryRowContext(ctx, "SELECT sequence_value FROM internal_code_sequences WHERE namespace=? FOR UPDATE", namespace).Scan(&last); err != nil {
		return 0, err
	}
	if last >= limit {
		return 0, fmt.Errorf("internal sequence exhausted: %s", namespace)
	}
	next := last + 1
	_, err := tx.ExecContext(ctx, "UPDATE internal_code_sequences SET sequence_value=? WHERE namespace=?", next, namespace)
	return next, err
}

func newInternalCode(ctx context.Context, tx *sql.Tx, feature string, now time.Time) (string, error) {
	date := now.In(chinaTime).Format("20060102")
	sequence, err := nextInternalSequence(ctx, tx, "code:"+feature+":"+date, 999999)
	if err != nil {
		return "", err
	}
	nonce, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ai%s%s%04d%06d", feature, date, nonce.Int64(), sequence), nil
}

func (s *Store) SaveWorkspaceTimeCard(ctx context.Context, userID, productID int64, input model.CommercialTimeCardInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if productID == 0 {
		code, err := newInternalCode(ctx, tx, "tc", now)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO catalog_time_card_products (code, name, description, status, sort_order)
            SELECT ?, ?, ?, 'inactive', COALESCE(MAX(sort_order), 0) + 1
            FROM catalog_time_card_products
        `, code, input.Name, input.Description)
		if err != nil {
			return err
		}
		productID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM catalog_time_card_products WHERE id=? FOR UPDATE", productID).Scan(&status); err != nil {
			return err
		}
		if status == "archived" {
			return ErrTimeCardArchived
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE catalog_time_card_products SET name=?, description=?,
                status=IF(status='draft', 'inactive', status) WHERE id=?
        `, input.Name, input.Description, productID); err != nil {
			return err
		}
	}

	var active model.CommercialTimeCardVersion
	err = tx.QueryRowContext(ctx, `
        SELECT id, price_cents, duration_seconds, validity_days, activation_deadline_days,
               participates_referral, participates_sales_commission, participates_agent_settlement
        FROM catalog_time_card_versions
        WHERE product_id=? AND lifecycle_status='published'
        ORDER BY version_no DESC LIMIT 1
    `, productID).Scan(&active.ID, &active.PriceCents, &active.DurationSeconds, &active.ValidityDays, &active.ActivationDeadlineDays,
		&active.ParticipatesReferral, &active.ParticipatesSalesCommission, &active.ParticipatesAgentSettlement)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// The current terms replace the old terms in the catalogue. Version IDs
	// referenced by orders and membership discounts remain historical snapshots.
	if active.ID == 0 || active.PriceCents != input.PriceCents || active.DurationSeconds != input.DurationSeconds ||
		active.ValidityDays != input.ValidityDays || active.ActivationDeadlineDays != input.ActivationDeadlineDays ||
		active.ParticipatesReferral != input.ParticipatesReferral || active.ParticipatesSalesCommission != input.ParticipatesSalesCommission ||
		active.ParticipatesAgentSettlement != input.ParticipatesAgentSettlement {
		if _, err := tx.ExecContext(ctx, `
            UPDATE catalog_time_card_versions SET lifecycle_status='retired', effective_to=?
            WHERE product_id=? AND lifecycle_status IN ('published','draft')
        `, now, productID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO catalog_time_card_versions
                (product_id, version_no, lifecycle_status, price_cents, duration_seconds, validity_days,
                 activation_deadline_days, participates_referral, participates_sales_commission,
                 participates_agent_settlement, effective_from, created_by_user_id, published_by_user_id, published_at)
            SELECT ?, COALESCE(MAX(version_no),0)+1, 'published', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
            FROM catalog_time_card_versions WHERE product_id=?
        `, productID, input.PriceCents, input.DurationSeconds, input.ValidityDays, input.ActivationDeadlineDays,
			input.ParticipatesReferral, input.ParticipatesSalesCommission, input.ParticipatesAgentSettlement,
			now, userID, userID, now, productID)
		if err != nil {
			return err
		}
		nextID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if active.ID > 0 {
			if _, err := tx.ExecContext(ctx, `
                INSERT INTO catalog_membership_card_discounts (plan_version_id, card_version_id, discount_bps)
                SELECT plan_version_id, ?, discount_bps FROM catalog_membership_card_discounts WHERE card_version_id=?
            `, nextID, active.ID); err != nil {
				return err
			}
		}
	}
	if err := insertCommercialAuditTx(ctx, tx, userID, "time_card.product.save", "time_card_product",
		fmt.Sprint(productID), nil, map[string]any{"name": input.Name, "price_cents": input.PriceCents, "duration_seconds": input.DurationSeconds,
			"validity_days": input.ValidityDays, "activation_deadline_days": input.ActivationDeadlineDays}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReorderWorkspaceTimeCards(ctx context.Context, userID int64, ids []int64, previous []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
        SELECT id FROM catalog_time_card_products WHERE status<>'archived' ORDER BY sort_order, id FOR UPDATE
    `)
	if err != nil {
		return err
	}
	current := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		current = append(current, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !slices.Equal(previous, current) || len(current) != len(ids) {
		return ErrTimeCardCatalogChanged
	}
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	slices.Sort(current)
	if !slices.Equal(sorted, current) {
		return ErrTimeCardCatalogChanged
	}
	for rank, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE catalog_time_card_products SET sort_order=? WHERE id=?", rank+1, id); err != nil {
			return err
		}
	}
	if err := insertCommercialAuditTx(ctx, tx, userID, "time_card.product.reorder", "time_card_product", "catalog", previous, ids); err != nil {
		return err
	}
	return tx.Commit()
}

func recordTimeCardSaleTx(ctx context.Context, tx *sql.Tx, productID, assetID, userID int64, autoRefund bool) error {
	var code string
	if err := tx.QueryRowContext(ctx, "SELECT code FROM catalog_time_card_products WHERE id=?", productID).Scan(&code); err != nil {
		return err
	}
	sequence, err := nextInternalSequence(ctx, tx, fmt.Sprintf("tc-sale:%d", productID), 99999999)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
        INSERT INTO biz_time_card_sales (asset_id, product_id, sale_no, buyer_user_id, auto_refund)
        SELECT a.id, ?, ?, u.id, ?
        FROM biz_time_card_assets a
        LEFT JOIN mgmt_users u ON u.id=? AND u.tenant_id=a.tenant_id AND u.role='customer'
        WHERE a.id=?
    `, productID, fmt.Sprintf("%s%08d", code, sequence), autoRefund, userID, assetID)
	return err
}

func (s *Store) BackfillTimeCardSales(ctx context.Context) error {
	for {
		rows, err := s.db.QueryContext(ctx, `
            SELECT a.id, a.product_id FROM biz_time_card_assets a
            LEFT JOIN biz_time_card_sales sale ON sale.asset_id=a.id
            WHERE sale.asset_id IS NULL ORDER BY a.id LIMIT 250
        `)
		if err != nil {
			return err
		}
		var assets [][2]int64
		for rows.Next() {
			var item [2]int64
			if err := rows.Scan(&item[0], &item[1]); err != nil {
				rows.Close()
				return err
			}
			assets = append(assets, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(assets) == 0 {
			return nil
		}
		for _, asset := range assets {
			if err := s.backfillTimeCardSale(ctx, asset[0], asset[1]); err != nil {
				return err
			}
		}
	}
}

func (s *Store) backfillTimeCardSale(ctx context.Context, assetID, productID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var asset int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM biz_time_card_assets WHERE id=? FOR UPDATE", assetID).Scan(&asset); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM biz_time_card_sales WHERE asset_id=?)", assetID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := recordTimeCardSaleTx(ctx, tx, productID, assetID, 0, false); err != nil {
		return err
	}
	return tx.Commit()
}
