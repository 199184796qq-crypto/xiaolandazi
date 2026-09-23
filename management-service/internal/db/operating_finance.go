package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

//go:embed operating_finance_schema.sql
var operatingFinanceSchema string

func (s *Store) MigrateOperatingFinance(ctx context.Context) error {
	schema := strings.ReplaceAll(operatingFinanceSchema, "\r\n", "\n")
	for _, raw := range strings.Split(schema, "\n-- +statement\n") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply operating finance schema: %w", err)
		}
	}
	return nil
}

func insertOperatingEntryTx(
	ctx context.Context,
	tx *sql.Tx,
	direction string,
	category string,
	amountCents uint64,
	businessType string,
	businessID *int64,
	businessNo string,
	sourceKey string,
	counterpartyName string,
	paymentMethod string,
	description string,
	operatorUserID int64,
	occurredAt time.Time,
) error {
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO fin_operating_entries (
			entry_no, direction, category, amount_cents, currency,
			business_type, business_id, business_no, source_key,
			counterparty_name, payment_method, description,
			operator_user_id, occurred_at
		)
		VALUES (?, ?, ?, ?, 'CNY', ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE source_key=VALUES(source_key)
	`,
		nextInventoryNo("OPE"),
		strings.TrimSpace(direction),
		strings.TrimSpace(category),
		amountCents,
		strings.TrimSpace(businessType),
		businessID,
		strings.TrimSpace(businessNo),
		strings.TrimSpace(sourceKey),
		strings.TrimSpace(counterpartyName),
		strings.TrimSpace(paymentMethod),
		strings.TrimSpace(description),
		operatorUserID,
		occurredAt.UTC(),
	)
	return err
}

func (s *Store) CreateTokenPurchase(
	ctx context.Context,
	userID int64,
	input model.TokenPurchaseInput,
) (model.TokenPurchase, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.TokenPurchase{}, err
	}
	defer tx.Rollback()

	purchasedAt := time.Now().UTC()
	if input.PurchasedAt != nil && !input.PurchasedAt.IsZero() {
		purchasedAt = input.PurchasedAt.UTC()
	}
	purchaseNo := nextInventoryNo("TOK")
	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_token_purchases (
			purchase_no, provider_name, model_scope, token_quantity,
			amount_cents, payment_method, invoice_no, purchased_at,
			note, operator_user_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		purchaseNo,
		strings.TrimSpace(input.ProviderName),
		strings.TrimSpace(input.ModelScope),
		input.TokenQuantity,
		input.AmountCents,
		strings.TrimSpace(input.PaymentMethod),
		strings.TrimSpace(input.InvoiceNo),
		purchasedAt,
		strings.TrimSpace(input.Note),
		userID,
	)
	if err != nil {
		return model.TokenPurchase{}, err
	}
	purchaseID, err := result.LastInsertId()
	if err != nil {
		return model.TokenPurchase{}, err
	}
	if err := insertOperatingEntryTx(
		ctx,
		tx,
		"expense",
		"token_purchase",
		input.AmountCents,
		"token_purchase",
		&purchaseID,
		purchaseNo,
		"token_purchase:"+fmt.Sprint(purchaseID),
		input.ProviderName,
		input.PaymentMethod,
		"Token 采购 · "+strings.TrimSpace(input.ModelScope),
		userID,
		purchasedAt,
	); err != nil {
		return model.TokenPurchase{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.TokenPurchase{}, err
	}
	return s.getTokenPurchase(ctx, purchaseID)
}

func (s *Store) getTokenPurchase(ctx context.Context, purchaseID int64) (model.TokenPurchase, error) {
	var item model.TokenPurchase
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.purchase_no, p.provider_name, p.model_scope,
		       p.token_quantity, p.amount_cents, p.payment_method,
		       p.invoice_no, p.purchased_at, p.note, p.operator_user_id,
		       COALESCE(u.display_name, u.username, ''), p.created_at
		FROM fin_token_purchases p
		LEFT JOIN mgmt_users u ON u.id=p.operator_user_id
		WHERE p.id=?
	`, purchaseID).Scan(
		&item.ID,
		&item.PurchaseNo,
		&item.ProviderName,
		&item.ModelScope,
		&item.TokenQuantity,
		&item.AmountCents,
		&item.PaymentMethod,
		&item.InvoiceNo,
		&item.PurchasedAt,
		&item.Note,
		&item.OperatorUserID,
		&item.OperatorName,
		&item.CreatedAt,
	)
	return item, err
}

func (s *Store) ListOperatingFinance(ctx context.Context, limit int) (model.OperatingFinanceOverview, error) {
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	var out model.OperatingFinanceOverview
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN direction='income' THEN amount_cents ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN direction='expense' THEN amount_cents ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN direction='income' AND YEAR(occurred_at)=YEAR(CURRENT_DATE()) AND MONTH(occurred_at)=MONTH(CURRENT_DATE()) THEN amount_cents ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN direction='expense' AND YEAR(occurred_at)=YEAR(CURRENT_DATE()) AND MONTH(occurred_at)=MONTH(CURRENT_DATE()) THEN amount_cents ELSE 0 END), 0)
		FROM fin_operating_entries
	`).Scan(
		&out.TotalIncomeCents,
		&out.TotalExpenseCents,
		&out.MonthIncomeCents,
		&out.MonthExpenseCents,
	); err != nil {
		return model.OperatingFinanceOverview{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.entry_no, e.direction, e.category, e.amount_cents,
		       e.currency, e.business_type, e.business_id, e.business_no,
		       e.counterparty_name, e.payment_method, e.description,
		       e.operator_user_id, COALESCE(u.display_name, u.username, ''),
		       e.occurred_at, e.created_at
		FROM fin_operating_entries e
		LEFT JOIN mgmt_users u ON u.id=e.operator_user_id
		ORDER BY e.occurred_at DESC, e.id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return model.OperatingFinanceOverview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.OperatingFinanceEntry
		if err := rows.Scan(
			&item.ID,
			&item.EntryNo,
			&item.Direction,
			&item.Category,
			&item.AmountCents,
			&item.Currency,
			&item.BusinessType,
			&item.BusinessID,
			&item.BusinessNo,
			&item.CounterpartyName,
			&item.PaymentMethod,
			&item.Description,
			&item.OperatorUserID,
			&item.OperatorName,
			&item.OccurredAt,
			&item.CreatedAt,
		); err != nil {
			return model.OperatingFinanceOverview{}, err
		}
		out.Entries = append(out.Entries, item)
	}
	if err := rows.Err(); err != nil {
		return model.OperatingFinanceOverview{}, err
	}

	tokenRows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.purchase_no, p.provider_name, p.model_scope,
		       p.token_quantity, p.amount_cents, p.payment_method,
		       p.invoice_no, p.purchased_at, p.note, p.operator_user_id,
		       COALESCE(u.display_name, u.username, ''), p.created_at
		FROM fin_token_purchases p
		LEFT JOIN mgmt_users u ON u.id=p.operator_user_id
		ORDER BY p.purchased_at DESC, p.id DESC
		LIMIT 200
	`)
	if err != nil {
		return model.OperatingFinanceOverview{}, err
	}
	defer tokenRows.Close()
	for tokenRows.Next() {
		var item model.TokenPurchase
		if err := tokenRows.Scan(
			&item.ID,
			&item.PurchaseNo,
			&item.ProviderName,
			&item.ModelScope,
			&item.TokenQuantity,
			&item.AmountCents,
			&item.PaymentMethod,
			&item.InvoiceNo,
			&item.PurchasedAt,
			&item.Note,
			&item.OperatorUserID,
			&item.OperatorName,
			&item.CreatedAt,
		); err != nil {
			return model.OperatingFinanceOverview{}, err
		}
		out.TokenPurchases = append(out.TokenPurchases, item)
	}
	return out, tokenRows.Err()
}
