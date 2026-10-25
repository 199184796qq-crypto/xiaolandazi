package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"livecompanion/management/internal/livecost"
)

// This independent request ledger deliberately does not consume the historical
// live_cost_ledger duration snapshots: those are not supplier request receipts.
func (s *Store) MigrateLiveCostRequests(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS live_cost_room_locks (tenant_id BIGINT NOT NULL, room_id BIGINT NOT NULL, PRIMARY KEY(tenant_id,room_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_cost_requests (
		 tenant_id BIGINT NOT NULL,room_id BIGINT NOT NULL,request_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		 kind VARCHAR(16) NOT NULL, provider VARCHAR(96) COLLATE utf8mb4_bin NOT NULL, provider_request_id VARCHAR(192) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
		 priority VARCHAR(16) NOT NULL,status VARCHAR(16) NOT NULL,request_json TEXT NOT NULL,receipt_json TEXT NULL,
		 estimated_micros BIGINT NOT NULL,reserved_micros BIGINT NOT NULL,charged_micros BIGINT NOT NULL DEFAULT 0,
		 usage_source VARCHAR(32) NOT NULL DEFAULT '',over_budget TINYINT NOT NULL DEFAULT 0,
		 requested_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
		 PRIMARY KEY(tenant_id,room_id,request_key),KEY idx_live_cost_room_window(tenant_id,room_id,requested_at),
		 KEY idx_live_cost_supplier(provider,provider_request_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("创建请求费用账本: %w", err)
		}
	}
	return nil
}

func lockLiveCostRoom(ctx context.Context, tx *sql.Tx, scope livecost.Scope) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO live_cost_room_locks(tenant_id,room_id) VALUES(?,?) ON DUPLICATE KEY UPDATE room_id=VALUES(room_id)`, scope.TenantID, scope.RoomID); err != nil {
		return err
	}
	var room int64
	return tx.QueryRowContext(ctx, `SELECT room_id FROM live_cost_room_locks WHERE tenant_id=? AND room_id=? FOR UPDATE`, scope.TenantID, scope.RoomID).Scan(&room)
}
func (s *Store) ReserveLiveCost(ctx context.Context, r livecost.Request, policy livecost.Policy, at time.Time) (livecost.Reservation, error) {
	var out livecost.Reservation
	r, err := livecost.NormalizeRequest(r)
	if err != nil {
		return out, err
	}
	if s == nil || s.db == nil {
		return out, livecost.ErrAccountingUnavailable
	}
	scope := livecost.Scope{TenantID: r.TenantID, RoomID: r.RoomID}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = lockLiveCostRoom(ctx, tx, scope); err != nil {
		return out, err
	}
	existing, over, err := readLiveCostEntry(ctx, tx, scope, r.Key)
	if err == nil {
		if !reflect.DeepEqual(existing.Request, r) {
			return out, livecost.ErrRequestConflict
		}
		out = livecost.Reservation{Key: r.Key, Existing: true, Status: existing.Status, OverBudget: over, EstimatedMicros: r.EstimatedMicros}
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	summary, err := liveCostSummary(ctx, tx, scope, policy, at)
	if err != nil {
		return out, err
	}
	// Subtraction avoids integer overflow while enforcing a shared model+TTS cap.
	limit := policy.OptionalLimitMicros
	if limit <= 0 {
		limit = livecost.DefaultPolicy().OptionalLimitMicros
	}
	optionalOver := summary.TotalMicros >= limit || r.EstimatedMicros > limit-summary.TotalMicros
	target := policy.TotalTargetMicros
	if target <= 0 {
		target = livecost.DefaultPolicy().TotalTargetMicros
	}
	over = summary.TotalMicros >= target || r.EstimatedMicros > target-summary.TotalMicros
	status := livecost.StatusReserved
	reserved := r.EstimatedMicros
	if r.Priority == livecost.PriorityOptional && optionalOver {
		status = livecost.StatusDeclined
		reserved = 0
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_cost_requests(tenant_id,room_id,request_key,kind,provider,priority,status,request_json,estimated_micros,reserved_micros,over_budget,requested_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.TenantID, r.RoomID, r.Key, r.Kind, r.Provider, r.Priority, status, string(encoded), r.EstimatedMicros, reserved, over, at.UTC(), at.UTC())
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return livecost.Reservation{Key: r.Key, Allowed: status == livecost.StatusReserved, Status: status, OverBudget: over, EstimatedMicros: r.EstimatedMicros}, nil
}

type costQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readLiveCostEntry(ctx context.Context, q costQuery, scope livecost.Scope, key string) (livecost.Entry, bool, error) {
	var entry livecost.Entry
	var request string
	var receipt sql.NullString
	var over bool
	err := q.QueryRowContext(ctx, `SELECT request_json,receipt_json,status,reserved_micros,charged_micros,usage_source,over_budget FROM live_cost_requests WHERE tenant_id=? AND room_id=? AND request_key=? FOR UPDATE`, scope.TenantID, scope.RoomID, key).Scan(&request, &receipt, &entry.Status, &entry.ReservedMicros, &entry.ChargedMicros, &entry.UsageSource, &over)
	if err != nil {
		return entry, over, err
	}
	if err = json.Unmarshal([]byte(request), &entry.Request); err != nil {
		return entry, over, err
	}
	if receipt.Valid && receipt.String != "" {
		err = json.Unmarshal([]byte(receipt.String), &entry.Receipt)
	}
	return entry, over, err
}
func (s *Store) FinishLiveCost(ctx context.Context, scope livecost.Scope, key string, receipt livecost.Receipt, at time.Time) error {
	if s == nil || s.db == nil {
		return livecost.ErrAccountingUnavailable
	}
	if scope.TenantID <= 0 || scope.RoomID <= 0 || key == "" || receipt.TenantID != scope.TenantID || receipt.RoomID != scope.RoomID {
		return livecost.ErrRequestConflict
	}
	if err := livecost.ValidateReceipt(receipt); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockLiveCostRoom(ctx, tx, scope); err != nil {
		return err
	}
	entry, _, err := readLiveCostEntry(ctx, tx, scope, key)
	if errors.Is(err, sql.ErrNoRows) {
		return livecost.ErrRequestNotFound
	}
	if err != nil {
		return err
	}
	next, err := livecost.ApplyReceipt(entry, receipt)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(entry, next) {
		return tx.Commit()
	}
	encoded, err := json.Marshal(next.Receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_cost_requests SET status=?,reserved_micros=?,charged_micros=?,usage_source=?,provider_request_id=?,receipt_json=?,updated_at=? WHERE tenant_id=? AND room_id=? AND request_key=?`, next.Status, next.ReservedMicros, next.ChargedMicros, next.UsageSource, next.Receipt.ProviderRequestID, string(encoded), at.UTC(), scope.TenantID, scope.RoomID, key)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) LiveCostSummary(ctx context.Context, scope livecost.Scope, policy livecost.Policy, at time.Time) (livecost.Summary, error) {
	if s == nil || s.db == nil {
		return livecost.Summary{}, livecost.ErrAccountingUnavailable
	}
	if scope.TenantID <= 0 || scope.RoomID <= 0 {
		return livecost.Summary{}, livecost.ErrRequestConflict
	}
	return liveCostSummary(ctx, s.db, scope, policy, at)
}
func liveCostSummary(ctx context.Context, q costQuery, scope livecost.Scope, policy livecost.Policy, at time.Time) (livecost.Summary, error) {
	window := policy.Window
	if window <= 0 {
		window = time.Hour
	}
	out := livecost.Summary{Scope: scope, WindowStart: at.Add(-window).UTC(), WindowEnd: at.UTC()}
	// Unresolved reservations deliberately have NO age cutoff. A missing response
	// is not proof of no charge, including after a process restart or a new live.
	err := q.QueryRowContext(ctx, `SELECT
	 COALESCE(SUM(CASE WHEN status='settled' AND kind='model' THEN charged_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status='settled' AND kind='tts' THEN charged_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status='settled' AND usage_source='sent_text_estimate' THEN charged_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status='settled' AND usage_source='supplier_usage' THEN charged_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status='settled' AND usage_source='supplier_invoice' THEN charged_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status IN ('reserved','unknown') THEN reserved_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status='unknown' THEN reserved_micros ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status IN ('reserved','unknown') THEN 1 ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN status='settled' THEN 1 ELSE 0 END),0)
	 FROM live_cost_requests WHERE tenant_id=? AND room_id=? AND ((requested_at>=? AND requested_at<=?) OR status IN ('reserved','unknown'))`, scope.TenantID, scope.RoomID, out.WindowStart, out.WindowEnd).Scan(&out.ModelMicros, &out.TTSMicros, &out.EstimatedMicros, &out.SupplierUsageMicros, &out.SupplierInvoiceMicros, &out.ReservedMicros, &out.UnknownMicros, &out.UnresolvedRequests, &out.SettledRequests)
	if err != nil {
		return out, err
	}
	if out.ModelMicros < 0 || out.TTSMicros < 0 || out.ReservedMicros < 0 || out.ModelMicros > math.MaxInt64-out.TTSMicros || out.ModelMicros+out.TTSMicros > math.MaxInt64-out.ReservedMicros {
		return out, errors.New("请求费用合计越界")
	}
	out.TotalMicros = out.ModelMicros + out.TTSMicros + out.ReservedMicros
	return out, nil
}

var _ livecost.Store = (*Store)(nil)

// ReconcileLiveCostInvoice upgrades a request's estimate/usage to explicit
// supplier invoice evidence. Reimporting the same invoice does not add a charge.
func (s *Store) ReconcileLiveCostInvoice(ctx context.Context, invoice livecost.Invoice, at time.Time) error {
	if err := invoice.Validate(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return livecost.ErrAccountingUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request_key FROM live_cost_requests WHERE tenant_id=? AND room_id=? AND provider=? AND provider_request_id=? LIMIT 2`, invoice.TenantID, invoice.RoomID, invoice.Provider, invoice.ProviderRequestID)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return livecost.ErrRequestNotFound
	}
	if len(keys) != 1 {
		return fmt.Errorf("%w: 供应商请求关联不唯一", livecost.ErrReceiptConflict)
	}
	return s.FinishLiveCost(ctx, livecost.Scope{TenantID: invoice.TenantID, RoomID: invoice.RoomID}, keys[0], livecost.Receipt{TenantID: invoice.TenantID, RoomID: invoice.RoomID, Status: livecost.ReceiptSuccess, ActualKnown: true, ActualMicros: invoice.ActualMicros, ProviderRequestID: invoice.ProviderRequestID, UsageSource: livecost.SourceSupplierInvoice}, at)
}
