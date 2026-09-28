package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

const LiveQuotaLeaseSeconds uint64 = 60

type LiveQuotaLeaseRequest struct {
	RoomID                  int64
	RuntimeSessionID        int64
	CoreBootID              string
	CoreWorkingStartSeconds uint64
	RequestedSeconds        uint64
}

type LiveQuotaLeaseGrant struct {
	ID                      int64
	ExternalID              string
	TenantID                int64
	RoomID                  int64
	RuntimeSessionID        int64
	CoreBootID              string
	CoreWorkingStartSeconds uint64
	AllocatedSeconds        uint64
	ConsumedSeconds         uint64
	Status                  string
	GrantedAt               time.Time
	ExpiresAt               time.Time
}

type leaseAllocation struct {
	ID       int64
	BucketID int64
	Reserved uint64
	Consumed uint64
}

func fairLeaseAllocation(total uint64, requests []LiveQuotaLeaseRequest) map[int64]uint64 {
	result := make(map[int64]uint64, len(requests))
	pending := make([]LiveQuotaLeaseRequest, 0, len(requests))
	for _, request := range requests {
		if request.RuntimeSessionID <= 0 || request.RequestedSeconds == 0 {
			continue
		}
		pending = append(pending, request)
		result[request.RuntimeSessionID] = 0
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].RoomID == pending[j].RoomID {
			return pending[i].RuntimeSessionID < pending[j].RuntimeSessionID
		}
		return pending[i].RoomID < pending[j].RoomID
	})

	for total > 0 && len(pending) > 0 {
		share := total / uint64(len(pending))
		if share == 0 {
			share = 1
		}
		next := pending[:0]
		progressed := false
		for _, request := range pending {
			if total == 0 {
				next = append(next, request)
				continue
			}
			have := result[request.RuntimeSessionID]
			if have >= request.RequestedSeconds {
				continue
			}
			need := request.RequestedSeconds - have
			give := share
			if give > need {
				give = need
			}
			if give > total {
				give = total
			}
			if give > 0 {
				result[request.RuntimeSessionID] += give
				total -= give
				progressed = true
			}
			if result[request.RuntimeSessionID] < request.RequestedSeconds {
				next = append(next, request)
			}
		}
		pending = next
		if !progressed {
			break
		}
	}
	return result
}

func (s *Store) AllocateLiveQuotaLeases(
	ctx context.Context,
	tenantID int64,
	requests []LiveQuotaLeaseRequest,
	now time.Time,
) ([]LiveQuotaLeaseGrant, error) {
	if tenantID <= 0 || len(requests) == 0 {
		return []LiveQuotaLeaseGrant{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	clean := make([]LiveQuotaLeaseRequest, 0, len(requests))
	seen := map[int64]struct{}{}
	for _, request := range requests {
		request.CoreBootID = strings.TrimSpace(request.CoreBootID)
		if request.RoomID <= 0 || request.RuntimeSessionID <= 0 || request.CoreBootID == "" {
			continue
		}
		if request.RequestedSeconds == 0 {
			request.RequestedSeconds = LiveQuotaLeaseSeconds
		}
		if request.RequestedSeconds > LiveQuotaLeaseSeconds {
			request.RequestedSeconds = LiveQuotaLeaseSeconds
		}
		if _, ok := seen[request.RuntimeSessionID]; ok {
			continue
		}
		seen[request.RuntimeSessionID] = struct{}{}
		clean = append(clean, request)
	}
	if len(clean) == 0 {
		return []LiveQuotaLeaseGrant{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	if _, err := expireTimeCardAssetsTx(ctx, tx, tenantID, now); err != nil {
		return nil, err
	}

	filtered := clean[:0]
	for _, request := range clean {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM live_quota_leases
				WHERE tenant_id=? AND runtime_session_id=?
				  AND core_boot_id=? AND status IN ('reserved','active')
				  AND core_working_start_seconds + allocated_seconds > ?
			)
		`, tenantID, request.RuntimeSessionID, request.CoreBootID, request.CoreWorkingStartSeconds).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			filtered = append(filtered, request)
		}
	}
	clean = filtered
	if len(clean) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return []LiveQuotaLeaseGrant{}, nil
	}

	buckets, err := loadActiveQuotaBucketsTx(ctx, tx, tenantID, now)
	if err != nil {
		return nil, err
	}
	var totalAvailable uint64
	for _, bucket := range buckets {
		totalAvailable += bucket.Available()
	}
	distribution := fairLeaseAllocation(totalAvailable, clean)
	grants := make([]LiveQuotaLeaseGrant, 0, len(clean))
	var totalReserved uint64

	for _, request := range clean {
		allocated := distribution[request.RuntimeSessionID]
		if allocated == 0 {
			continue
		}
		externalID, err := newLiveReference("LQL")
		if err != nil {
			return nil, err
		}
		expiresAt := now.Add(time.Duration(allocated) * time.Second)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO live_quota_leases (
				external_id, tenant_id, room_id, runtime_session_id, core_boot_id,
				core_working_start_seconds, allocated_seconds, consumed_seconds,
				status, granted_at, expires_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'active', ?, ?)
		`, externalID, tenantID, request.RoomID, request.RuntimeSessionID, request.CoreBootID,
			request.CoreWorkingStartSeconds, allocated, now, expiresAt)
		if err != nil {
			return nil, err
		}
		leaseID, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}

		need := allocated
		for i := range buckets {
			if need == 0 {
				break
			}
			available := buckets[i].Available()
			if available == 0 {
				continue
			}
			reserve := available
			if reserve > need {
				reserve = need
			}
			update, err := tx.ExecContext(ctx, `
				UPDATE quota_buckets
				SET reserved_seconds=reserved_seconds+?, updated_at=CURRENT_TIMESTAMP(3)
				WHERE id=? AND reserved_seconds+?<=remaining_seconds
			`, reserve, buckets[i].ID, reserve)
			if err != nil {
				return nil, err
			}
			if affected, _ := update.RowsAffected(); affected != 1 {
				return nil, fmt.Errorf("quota reservation conflict bucket=%d", buckets[i].ID)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO live_quota_lease_allocations (
					lease_id, bucket_id, reserved_seconds, consumed_seconds
				) VALUES (?, ?, ?, 0)
			`, leaseID, buckets[i].ID, reserve); err != nil {
				return nil, err
			}
			buckets[i].Reserved += reserve
			need -= reserve
		}
		if need != 0 {
			return nil, fmt.Errorf("quota reservation invariant failed: lease=%d missing=%d", leaseID, need)
		}
		totalReserved += allocated
		grants = append(grants, LiveQuotaLeaseGrant{
			ID: leaseID, ExternalID: externalID, TenantID: tenantID,
			RoomID: request.RoomID, RuntimeSessionID: request.RuntimeSessionID,
			CoreBootID:              request.CoreBootID,
			CoreWorkingStartSeconds: request.CoreWorkingStartSeconds,
			AllocatedSeconds:        allocated, Status: "active", GrantedAt: now, ExpiresAt: expiresAt,
		})
	}
	if totalReserved > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE org_resource_accounts
			SET reserved=reserved+?
			WHERE organization_id=? AND resource_type='ai_seconds'
		`, totalReserved, tenantID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return grants, nil
}

func (s *Store) LiveQuotaLeaseRunway(
	ctx context.Context,
	tenantID, sessionID int64,
	coreBootID string,
	coreWorkingSeconds uint64,
) (uint64, error) {
	var end sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(core_working_start_seconds + allocated_seconds)
		FROM live_quota_leases
		WHERE tenant_id=? AND runtime_session_id=? AND core_boot_id=?
		  AND status IN ('reserved','active')
	`, tenantID, sessionID, strings.TrimSpace(coreBootID)).Scan(&end)
	if err != nil {
		return 0, err
	}
	if !end.Valid || end.Int64 <= 0 || uint64(end.Int64) <= coreWorkingSeconds {
		return 0, nil
	}
	return uint64(end.Int64) - coreWorkingSeconds, nil
}

func (s *Store) ReconcileLiveQuotaLeases(
	ctx context.Context,
	tenantID, sessionID int64,
	coreBootID string,
	coreWorkingSeconds uint64,
	finalize bool,
	normalCompletion bool,
	now time.Time,
) (uint64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	session, err := lockLiveRuntimeSession(ctx, tx, sessionID)
	if err != nil {
		return 0, err
	}
	if session.TenantID != tenantID {
		return 0, errors.New("runtime session tenant mismatch")
	}
	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, external_id, room_id, runtime_session_id, core_boot_id,
		       core_working_start_seconds, allocated_seconds, consumed_seconds,
		       status, granted_at, expires_at
		FROM live_quota_leases
		WHERE tenant_id=? AND runtime_session_id=? AND status IN ('reserved','active')
		ORDER BY core_working_start_seconds ASC, id ASC
		FOR UPDATE
	`, tenantID, sessionID)
	if err != nil {
		return 0, err
	}
	leases := make([]LiveQuotaLeaseGrant, 0)
	for rows.Next() {
		var lease LiveQuotaLeaseGrant
		if err := rows.Scan(&lease.ID, &lease.ExternalID, &lease.RoomID, &lease.RuntimeSessionID,
			&lease.CoreBootID, &lease.CoreWorkingStartSeconds, &lease.AllocatedSeconds,
			&lease.ConsumedSeconds, &lease.Status, &lease.GrantedAt, &lease.ExpiresAt); err != nil {
			rows.Close()
			return 0, err
		}
		lease.TenantID = tenantID
		leases = append(leases, lease)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	var totalConsumed uint64
	for _, lease := range leases {
		sameBoot := strings.TrimSpace(coreBootID) != "" && lease.CoreBootID == strings.TrimSpace(coreBootID)
		consume := uint64(0)
		shouldClose := false
		cancelled := false
		if !sameBoot {
			shouldClose = true
			cancelled = true
		} else if normalCompletion {
			if coreWorkingSeconds > lease.CoreWorkingStartSeconds {
				consume = coreWorkingSeconds - lease.CoreWorkingStartSeconds
				if consume > lease.AllocatedSeconds {
					consume = lease.AllocatedSeconds
				}
			}
			if consume >= lease.AllocatedSeconds || finalize {
				shouldClose = true
			}
		} else if finalize {
			shouldClose = true
			cancelled = true
		}
		if !shouldClose {
			continue
		}
		consumed, err := settleLiveQuotaLeaseTx(ctx, tx, session, lease, consume, cancelled, now)
		if err != nil {
			return 0, err
		}
		totalConsumed += consumed
	}
	if totalConsumed > 0 {
		session.TotalBilledSeconds += totalConsumed
		session.LastBilledAt = now
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_runtime_sessions
			SET total_billed_seconds=?, last_billed_at=?, version=version+1, updated_at=?
			WHERE id=?
		`, session.TotalBilledSeconds, now, now, session.ID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return totalConsumed, nil
}

func settleLiveQuotaLeaseTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.LiveRuntimeSession,
	lease LiveQuotaLeaseGrant,
	consume uint64,
	cancelled bool,
	now time.Time,
) (uint64, error) {
	if consume > lease.AllocatedSeconds {
		consume = lease.AllocatedSeconds
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, bucket_id, reserved_seconds, consumed_seconds
		FROM live_quota_lease_allocations
		WHERE lease_id=?
		ORDER BY id ASC
		FOR UPDATE
	`, lease.ID)
	if err != nil {
		return 0, err
	}
	allocations := make([]leaseAllocation, 0)
	for rows.Next() {
		var item leaseAllocation
		if err := rows.Scan(&item.ID, &item.BucketID, &item.Reserved, &item.Consumed); err != nil {
			rows.Close()
			return 0, err
		}
		allocations = append(allocations, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	consumeLeft := consume
	var totalReleased uint64
	var totalConsumed uint64
	for _, allocation := range allocations {
		use := uint64(0)
		if consumeLeft > 0 {
			use = allocation.Reserved
			if use > consumeLeft {
				use = consumeLeft
			}
		}
		var before, reservedBefore uint64
		if err := tx.QueryRowContext(ctx, `
			SELECT remaining_seconds, reserved_seconds
			FROM quota_buckets WHERE id=? FOR UPDATE
		`, allocation.BucketID).Scan(&before, &reservedBefore); err != nil {
			return 0, err
		}
		if reservedBefore < allocation.Reserved || before < use {
			return 0, fmt.Errorf("quota lease settlement invariant failed bucket=%d", allocation.BucketID)
		}
		after := before - use
		reservedAfter := reservedBefore - allocation.Reserved
		status := "active"
		if after == 0 && reservedAfter == 0 {
			status = "exhausted"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE quota_buckets
			SET remaining_seconds=?, reserved_seconds=?, status=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, after, reservedAfter, status, allocation.BucketID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_time_card_assets
			SET remaining_seconds=?,
			    status=CASE WHEN ?=0 AND ?=0 THEN 'exhausted' ELSE status END,
			    updated_at=CURRENT_TIMESTAMP(3)
			WHERE quota_bucket_id=? AND status='active'
		`, after, after, reservedAfter, allocation.BucketID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_quota_lease_allocations
			SET consumed_seconds=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, use, allocation.ID); err != nil {
			return 0, err
		}
		if use > 0 {
			externalID, err := newLiveReference("QLED")
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO quota_ledger (
					external_id, tenant_id, bucket_id, change_seconds,
					remaining_before_seconds, remaining_after_seconds,
					business_type, business_id, operator_user_id,
					reason, idempotency_key, occurred_at
				) VALUES (?, ?, ?, ?, ?, ?, 'live_ai_usage', ?, NULL, ?, ?, ?)
			`, externalID, session.TenantID, allocation.BucketID, -int64(use), before, after,
				session.ID, fmt.Sprintf("直播间 %d AI租约时长消费", session.RoomID),
				fmt.Sprintf("live-lease-%d-bucket-%d", lease.ID, allocation.BucketID), now); err != nil {
				return 0, err
			}
		}
		consumeLeft -= use
		totalConsumed += use
		totalReleased += allocation.Reserved
	}
	if consumeLeft != 0 {
		return 0, fmt.Errorf("quota lease settlement missing reserved seconds lease=%d missing=%d", lease.ID, consumeLeft)
	}
	if totalReleased > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE org_resource_accounts
			SET reserved=GREATEST(0, reserved-?)
			WHERE organization_id=? AND resource_type='ai_seconds'
		`, totalReleased, session.TenantID); err != nil {
			return 0, err
		}
	}
	status := "settled"
	if cancelled {
		status = "cancelled"
		totalConsumed = 0
	}
	endSeconds := lease.CoreWorkingStartSeconds + totalConsumed
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_quota_leases
		SET consumed_seconds=?, core_working_end_seconds=?, status=?, settled_at=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND status IN ('reserved','active')
	`, totalConsumed, endSeconds, status, now, lease.ID); err != nil {
		return 0, err
	}
	if totalConsumed > 0 {
		externalID, err := newLiveReference("QUSE")
		if err != nil {
			return 0, err
		}
		startedAt := lease.GrantedAt
		endedAt := startedAt.Add(time.Duration(totalConsumed) * time.Second)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quota_usage_events (
				external_id, tenant_id, room_id, billable_seconds,
				started_at, ended_at, status, idempotency_key
			) VALUES (?, ?, ?, ?, ?, ?, 'posted', ?)
		`, externalID, session.TenantID, session.RoomID, totalConsumed,
			startedAt, endedAt, fmt.Sprintf("live-lease-use-%d", lease.ID)); err != nil {
			return 0, err
		}
		if err := syncCustomerAIResourceAfterQuotaChargeTx(ctx, tx, session.TenantID,
			int64(totalConsumed), session.RoomID, endedAt); err != nil {
			return 0, err
		}
	}
	return totalConsumed, nil
}
