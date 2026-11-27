package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type WorkspaceTimeCard struct {
	ID                          int64     `json:"id"`
	Code                        string    `json:"code"`
	Name                        string    `json:"name"`
	Description                 string    `json:"description"`
	Status                      string    `json:"status"`
	SortOrder                   int       `json:"sort_order"`
	PriceCents                  uint64    `json:"price_cents"`
	DurationSeconds             uint64    `json:"duration_seconds"`
	ValidityDays                uint32    `json:"validity_days"`
	ActivationDeadlineDays      uint32    `json:"activation_deadline_days"`
	ParticipatesReferral        bool      `json:"participates_referral"`
	ParticipatesSalesCommission bool      `json:"participates_sales_commission"`
	ParticipatesAgentSettlement bool      `json:"participates_agent_settlement"`
	SaleCount                   int64     `json:"sale_count"`
	NetCents                    int64     `json:"net_cents"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

func (s *Store) ListWorkspaceTimeCards(ctx context.Context, start, end time.Time) ([]WorkspaceTimeCard, error) {
	periodClause := ""
	var args []any
	if !start.IsZero() {
		periodClause = "WHERE a.created_at>=? AND a.created_at<?"
		args = []any{start, end}
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT p.id,p.code,p.name,p.description,p.status,p.sort_order,
            COALESCE(v.price_cents,0),COALESCE(v.duration_seconds,0),COALESCE(v.validity_days,7),
            COALESCE(NULLIF(v.activation_deadline_days,0),20),COALESCE(v.participates_referral,1),
            COALESCE(v.participates_sales_commission,1),COALESCE(v.participates_agent_settlement,1),
            COALESCE(total.sale_count,0),COALESCE(total.net,0),p.updated_at
        FROM catalog_time_card_products p
        LEFT JOIN catalog_time_card_versions v ON v.id=(
            SELECT cv.id FROM catalog_time_card_versions cv WHERE cv.product_id=p.id
            ORDER BY (cv.lifecycle_status='published') DESC,(cv.lifecycle_status='draft') DESC,cv.version_no DESC LIMIT 1
        )
        LEFT JOIN (
            SELECT a.product_id,COUNT(*) AS sale_count,SUM(CAST(`+timeCardPaidCentsSQL+` AS SIGNED)-COALESCE(refunds.refunded,0)) AS net
            FROM biz_time_card_assets a
            JOIN biz_orders o ON o.id=a.source_order_id
            JOIN biz_order_items i ON i.id=a.source_order_item_id
            `+timeCardRefundTotalsJoin+` `+periodClause+`
            GROUP BY a.product_id
        ) total ON total.product_id=p.id
        ORDER BY p.sort_order,p.id
    `, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkspaceTimeCard{}
	for rows.Next() {
		var v WorkspaceTimeCard
		if err := rows.Scan(&v.ID, &v.Code, &v.Name, &v.Description, &v.Status, &v.SortOrder,
			&v.PriceCents, &v.DurationSeconds, &v.ValidityDays, &v.ActivationDeadlineDays, &v.ParticipatesReferral,
			&v.ParticipatesSalesCommission, &v.ParticipatesAgentSettlement, &v.SaleCount, &v.NetCents, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

type TimeCardSalesQuery struct {
	Start, End                              *time.Time
	Username, Reference, Membership, Status string
	Page, PageSize                          int
}

type TimeCardSaleRefund struct {
	AssetID     int64      `json:"asset_id"`
	SaleNo      string     `json:"sale_no"`
	RefundNo    string     `json:"refund_no"`
	AmountCents uint64     `json:"amount_cents"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason"`
	Supervisor  string     `json:"supervisor"`
	Source      string     `json:"source"`
	RequestedAt time.Time  `json:"requested_at"`
	ProcessedAt *time.Time `json:"processed_at"`
}

type TimeCardSale struct {
	AssetID            int64                `json:"asset_id"`
	SaleNo             string               `json:"sale_no"`
	OrderNo            string               `json:"order_no"`
	PurchasedAt        time.Time            `json:"purchased_at"`
	Username           string               `json:"username"`
	DisplayName        string               `json:"display_name"`
	Membership         string               `json:"membership"`
	Channel            string               `json:"channel"`
	Status             string               `json:"status"`
	ActivationDeadline *time.Time           `json:"activation_deadline,omitempty"`
	ActivatedAt        *time.Time           `json:"activated_at,omitempty"`
	ExpiresAt          *time.Time           `json:"expires_at,omitempty"`
	OriginalSeconds    uint64               `json:"original_seconds"`
	RemainingSeconds   uint64               `json:"remaining_seconds"`
	FaceCents          uint64               `json:"face_cents"`
	PaidCents          uint64               `json:"paid_cents"`
	RefundedCents      uint64               `json:"refunded_cents"`
	RefundableCents    uint64               `json:"refundable_cents"`
	ManualRefundStatus string               `json:"manual_refund_status"`
	RefundCount        uint64               `json:"refund_count"`
	Refunds            []TimeCardSaleRefund `json:"refunds"`
}

type TimeCardSalesPage struct {
	Items                       []TimeCardSale `json:"items"`
	Memberships                 []string       `json:"memberships"`
	Count                       int64          `json:"count"`
	PaidCount                   int64          `json:"paid_count"`
	RefundCount                 int64          `json:"refund_count"`
	FaceCents                   int64          `json:"face_cents"`
	PaidCents                   int64          `json:"paid_cents"`
	RefundedCents               int64          `json:"refunded_cents"`
	HasUnallocatedLegacyRefunds bool           `json:"has_unallocated_legacy_refunds"`
}

const timeCardRefundTotalsJoin = `
    LEFT JOIN (
        SELECT sr.asset_id, SUM(sr.amount_cents) AS refunded, COUNT(*) AS refund_count
        FROM biz_time_card_sale_refunds sr JOIN fin_refund_orders r ON r.id=sr.refund_id
        WHERE r.status IN ('success','completed') GROUP BY sr.asset_id
    ) refunds ON refunds.asset_id=a.id`

const timeCardMembershipSQL = `CASE WHEN o.membership_plan_version_id_snapshot IS NULL THEN '非会员'
    ELSE COALESCE(NULLIF(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(o.pricing_snapshot_json,'$.membership_name')),'null'),''),p.name,'历史会员') END`

const timeCardSalesFrom = `
    FROM biz_time_card_sales sale
    JOIN biz_time_card_assets a ON a.id=sale.asset_id
    JOIN biz_orders o ON o.id=a.source_order_id
    JOIN biz_order_items i ON i.id=a.source_order_item_id
    LEFT JOIN catalog_membership_plan_versions v ON v.id=o.membership_plan_version_id_snapshot
    LEFT JOIN catalog_membership_plans p ON p.id=v.plan_id
    LEFT JOIN mgmt_users u ON u.id=COALESCE(sale.buyer_user_id,(
        SELECT MIN(u2.id) FROM mgmt_users u2 WHERE u2.role='customer' AND u2.tenant_id=a.tenant_id
    ))
    ` + timeCardRefundTotalsJoin

func (s *Store) ListTimeCardSales(ctx context.Context, productID int64, f TimeCardSalesQuery) (TimeCardSalesPage, error) {
	result := TimeCardSalesPage{Items: []TimeCardSale{}, Memberships: []string{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM catalog_time_card_products WHERE id=?)", productID).Scan(&exists); err != nil {
		return result, err
	}
	if !exists {
		return result, sql.ErrNoRows
	}
	from := timeCardSalesFrom + " WHERE sale.product_id=?"
	args := []any{productID}
	add := func(where string, arg ...any) { from += " AND " + where; args = append(args, arg...) }
	if f.Start != nil {
		add("a.created_at>=?", *f.Start)
	}
	if f.End != nil {
		add("a.created_at<?", *f.End)
	}
	if f.Username != "" {
		add("u.username=?", f.Username)
	}
	if f.Reference != "" {
		add("(sale.sale_no=? OR o.order_no=?)", f.Reference, f.Reference)
	}
	if f.Membership != "" {
		add("("+timeCardMembershipSQL+")=?", f.Membership)
	}
	if f.Status == "refunded" {
		add("COALESCE(refunds.refund_count,0)>0")
	} else if f.Status != "" {
		add("a.status=?", f.Status)
	}
	err = tx.QueryRowContext(ctx, `
        SELECT COUNT(*),COALESCE(SUM(`+timeCardPaidCentsSQL+`>0),0),
               COALESCE(SUM(COALESCE(refunds.refund_count,0)>0),0),COALESCE(SUM(i.unit_list_price_cents),0),
               COALESCE(SUM(`+timeCardPaidCentsSQL+`),0),COALESCE(SUM(refunds.refunded),0)
    `+from, args...).Scan(&result.Count, &result.PaidCount, &result.RefundCount, &result.FaceCents, &result.PaidCents, &result.RefundedCents)
	if err != nil {
		return result, err
	}
	if err := tx.QueryRowContext(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM biz_time_card_assets a
            JOIN fin_refund_orders r ON r.source_id=a.source_order_id AND r.source_type='order'
            WHERE a.product_id=? AND r.status IN ('success','completed')
              AND r.refund_amount_cents>(SELECT COALESCE(SUM(sr.amount_cents),0) FROM biz_time_card_sale_refunds sr WHERE sr.refund_id=r.id)
        )
    `, productID).Scan(&result.HasUnallocatedLegacyRefunds); err != nil {
		return result, err
	}

	rows, err := tx.QueryContext(ctx, `
        SELECT DISTINCT `+timeCardMembershipSQL+` AS membership
        FROM biz_time_card_sales sale JOIN biz_time_card_assets a ON a.id=sale.asset_id
        JOIN biz_orders o ON o.id=a.source_order_id
        LEFT JOIN catalog_membership_plan_versions v ON v.id=o.membership_plan_version_id_snapshot
        LEFT JOIN catalog_membership_plans p ON p.id=v.plan_id
        WHERE sale.product_id=? ORDER BY membership
    `, productID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return result, err
		}
		result.Memberships = append(result.Memberships, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, `
        SELECT a.id,sale.sale_no,o.order_no,a.created_at,COALESCE(u.username,''),
            COALESCE(NULLIF(u.display_name,''),u.username,''),`+timeCardMembershipSQL+`,
            COALESCE(NULLIF(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(o.pricing_snapshot_json,'$.marketing_campaign_name')),'null'),''),
                (SELECT c.campaign_name FROM mkt_campaign_order_snapshots c WHERE c.order_id=o.id AND c.target_type='time_card' ORDER BY c.id LIMIT 1),
                '商城'),a.status,a.activation_deadline_at,a.activated_at,a.expires_at,a.original_seconds,
            CASE WHEN a.status='expired' THEN COALESCE((
                SELECT MAX(q.remaining_before_seconds) FROM quota_ledger q WHERE q.business_type='time_card_expired'
                    AND q.business_id=a.id AND q.tenant_id=a.tenant_id), 0)
            ELSE a.remaining_seconds END,
            i.unit_list_price_cents,`+timeCardPaidCentsSQL+`,COALESCE(refunds.refunded,0),COALESCE(refunds.refund_count,0)
    `+from+` ORDER BY a.created_at DESC,a.id DESC LIMIT ? OFFSET ?`, append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return result, err
	}
	indices := map[int64]int{}
	refundArgs := []any{}
	for rows.Next() {
		v := TimeCardSale{Refunds: []TimeCardSaleRefund{}}
		if err := rows.Scan(&v.AssetID, &v.SaleNo, &v.OrderNo, &v.PurchasedAt, &v.Username, &v.DisplayName, &v.Membership, &v.Channel, &v.Status,
			&v.ActivationDeadline, &v.ActivatedAt, &v.ExpiresAt, &v.OriginalSeconds, &v.RemainingSeconds, &v.FaceCents, &v.PaidCents, &v.RefundedCents, &v.RefundCount); err != nil {
			rows.Close()
			return result, err
		}
		if v.Status == "expired" && v.ActivatedAt != nil {
			v.RefundableCents = proportionalTimeCardCents(v.FaceCents, v.RemainingSeconds, v.OriginalSeconds)
		}
		indices[v.AssetID] = len(result.Items)
		result.Items = append(result.Items, v)
		refundArgs = append(refundArgs, v.AssetID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	if len(refundArgs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(refundArgs)), ",")
		rows, err = tx.QueryContext(ctx, `
            SELECT sr.asset_id,r.refund_no,sr.amount_cents,r.status,r.reason,sr.supervisor,r.source_type,r.created_at,r.processed_at
            FROM biz_time_card_sale_refunds sr JOIN fin_refund_orders r ON r.id=sr.refund_id
            WHERE sr.asset_id IN (`+placeholders+`) ORDER BY COALESCE(r.processed_at,r.created_at) DESC,r.id DESC
        `, refundArgs...)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		for rows.Next() {
			var r TimeCardSaleRefund
			if err := rows.Scan(&r.AssetID, &r.RefundNo, &r.AmountCents, &r.Status, &r.Reason, &r.Supervisor, &r.Source, &r.RequestedAt, &r.ProcessedAt); err != nil {
				return result, err
			}
			v := &result.Items[indices[r.AssetID]]
			r.SaleNo = v.SaleNo
			if r.Source == timeCardResidualRefund && v.ManualRefundStatus == "" {
				v.ManualRefundStatus = r.Status
			}
			v.Refunds = append(v.Refunds, r)
		}
		if err := rows.Err(); err != nil {
			return result, err
		}
		rows.Close()
	}
	return result, tx.Commit()
}

func (s *Store) BackfillSingleTimeCardRefunds(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT IGNORE INTO biz_time_card_sale_refunds (asset_id,refund_id,amount_cents)
        SELECT a.id,r.id,r.refund_amount_cents
        FROM fin_refund_orders r JOIN biz_time_card_assets a ON a.source_order_id=r.source_id
        WHERE r.source_type='order' AND r.status IN ('success','completed') AND NOT EXISTS (
            SELECT 1 FROM biz_time_card_assets other WHERE other.source_order_id=a.source_order_id AND other.id<>a.id
        )
    `)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backfill single-card refunds: %w", err)
	}
	return nil
}
