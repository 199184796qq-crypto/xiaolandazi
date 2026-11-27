package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/agentgateway"
)

// AcquirePlanModelLease uses the existing account catalog and a separate task-slot count. Its expiry
// recovers the slot after a process failure without changing any room lease.
func (s *Store) AcquirePlanModelLease(ctx context.Context, until time.Time) (agentgateway.SpeechModel, string, error) {
	var profile agentgateway.SpeechModel
	if !s.modelPoolReady || !until.After(time.Now()) {
		return profile, "", errors.New("文字模型池不可用")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return profile, "", err
	}
	defer tx.Rollback()
	if err = lockModelPoolTx(ctx, tx); err != nil {
		return profile, "", err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mgmt_model_pool_task_leases WHERE expires_at <= UTC_TIMESTAMP(3)"); err != nil {
		return profile, "", err
	}
	usage, err := planModelPoolUsageTx(ctx, tx)
	if err != nil {
		return profile, "", err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,max_rooms FROM mgmt_model_pool_entries WHERE enabled=1 AND max_rooms>0 AND LOWER(model) LIKE '%deepseek%' ORDER BY created_at,id FOR UPDATE")
	if err != nil {
		return profile, "", err
	}
	var best string
	configured := false
	bestCapacity := 1
	for rows.Next() {
		var entry string
		var capacity int
		if err = rows.Scan(&entry, &capacity); err != nil {
			break
		}
		configured = true
		if usage[entry] < capacity && (best == "" || usage[entry]*bestCapacity < usage[best]*capacity) {
			best, bestCapacity = entry, capacity
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return profile, "", err
	}
	if !configured {
		return profile, "", errors.New("请在模型池启用 DeepSeek 账号")
	}
	if best == "" {
		return profile, "", ErrModelPoolFull
	}
	id, err := modelPoolID()
	if err != nil {
		return profile, "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO mgmt_model_pool_task_leases(lease_id,entry_id,expires_at) VALUES(?,?,?)", id, best, until.UTC()); err != nil {
		return profile, "", err
	}
	if err = tx.Commit(); err != nil {
		return profile, "", err
	}
	loaded, err := s.ModelPoolCatalogProfile(ctx, best)
	if err != nil || loaded == nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.ReleasePlanModelLease(cleanup, id)
		return profile, "", fmt.Errorf("读取裂变模型配置失败: %v", err)
	}
	return *loaded, id, nil
}

func (s *Store) ReleasePlanModelLease(ctx context.Context, lease string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM mgmt_model_pool_task_leases WHERE lease_id=?", lease)
	return err
}

func planModelPoolUsageTx(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT entry_id FROM mgmt_model_pool_task_leases WHERE expires_at>UTC_TIMESTAMP(3) FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	usage := map[string]int{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		usage[id]++
	}
	return usage, rows.Err()
}

func planModelPoolEntryActiveTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	tasks, err := planModelPoolUsageTx(ctx, tx)
	return tasks[id] > 0, err
}
