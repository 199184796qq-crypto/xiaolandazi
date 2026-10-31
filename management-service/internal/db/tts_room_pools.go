package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"livecompanion/management/internal/ttsscheduler"
)

type TTSRoomPoolPolicy struct {
	TenantID     int64     `json:"tenant_id"`
	RoomID       int64     `json:"room_id"`
	AllowedPools []string  `json:"allowed_pools"`
	Revision     uint64    `json:"revision"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Store) migrateTTSRoomPools(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mgmt_tts_room_pool_policies (
	 tenant_id BIGINT NOT NULL,room_id BIGINT NOT NULL,allowed_pools_json JSON NOT NULL,
	 revision BIGINT UNSIGNED NOT NULL DEFAULT 1,updated_by_user_id BIGINT NOT NULL,
	 updated_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,room_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

type ttsPolicyQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ttsRoomPoolPolicy(ctx context.Context, q ttsPolicyQuery, tenant, room int64) (TTSRoomPoolPolicy, error) {
	out := TTSRoomPoolPolicy{TenantID: tenant, RoomID: room, AllowedPools: []string{ttsscheduler.PoolStandard, ttsscheduler.PoolHighFidelity}}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT allowed_pools_json,revision,updated_at FROM mgmt_tts_room_pool_policies WHERE tenant_id=? AND room_id=?`, tenant, room).Scan(&raw, &out.Revision, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out.AllowedPools); err != nil {
		return out, err
	}
	if !validTTSPools(out.AllowedPools) {
		return out, ttsscheduler.ErrConfig
	}
	return out, nil
}

func validTTSPools(pools []string) bool {
	if len(pools) < 1 || len(pools) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, pool := range pools {
		if ttsscheduler.PoolLabel(pool) == "" || seen[pool] {
			return false
		}
		seen[pool] = true
	}
	return true
}

func checkTTSRoomPool(ctx context.Context, q ttsPolicyQuery, tenant, room int64, provider string) error {
	pool := ttsscheduler.PoolForProvider(provider)
	if room <= 0 || pool == "" {
		return nil
	}
	if tenant <= 0 {
		return ttsscheduler.ErrPoolDenied
	}
	policy, err := ttsRoomPoolPolicy(ctx, q, tenant, room)
	if err != nil {
		return err
	}
	for _, allowed := range policy.AllowedPools {
		if allowed == pool {
			return nil
		}
	}
	return ttsscheduler.ErrPoolDenied
}

func (s *Store) CheckTTSRoomPool(ctx context.Context, tenant, room int64, provider string) error {
	return checkTTSRoomPool(ctx, s.db, tenant, room, provider)
}
func (s *Store) GetTTSRoomPoolPolicy(ctx context.Context, tenant, room int64) (TTSRoomPoolPolicy, error) {
	return ttsRoomPoolPolicy(ctx, s.db, tenant, room)
}

func (s *Store) SaveTTSRoomPoolPolicy(ctx context.Context, input TTSRoomPoolPolicy, actor int64) (TTSRoomPoolPolicy, error) {
	if input.TenantID <= 0 || input.RoomID <= 0 || !validTTSPools(input.AllowedPools) {
		return input, fmt.Errorf("%w：请选择直播间及至少一个语音池", ttsscheduler.ErrConfig)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return input, err
	}
	defer tx.Rollback()
	now, err := lockTTSPool(ctx, tx)
	if err != nil {
		return input, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM core_rooms WHERE tenant_id=? AND id=?`, input.TenantID, input.RoomID).Scan(&exists); err != nil {
		return input, err
	}
	if exists != 1 {
		return input, fmt.Errorf("%w：直播间不属于所选账号", ttsscheduler.ErrConfig)
	}
	previous, err := ttsRoomPoolPolicy(ctx, tx, input.TenantID, input.RoomID)
	if err != nil {
		return input, err
	}
	if previous.Revision != input.Revision {
		return input, ttsscheduler.ErrRevision
	}
	pools := append([]string(nil), input.AllowedPools...)
	sort.Strings(pools)
	raw, _ := json.Marshal(pools)
	_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_tts_room_pool_policies(tenant_id,room_id,allowed_pools_json,revision,updated_by_user_id,updated_at) VALUES(?,?,?,1,?,?) ON DUPLICATE KEY UPDATE allowed_pools_json=VALUES(allowed_pools_json),revision=revision+1,updated_by_user_id=VALUES(updated_by_user_id),updated_at=VALUES(updated_at)`, input.TenantID, input.RoomID, string(raw), actor, now)
	if err != nil {
		return input, err
	}
	// New policy affects pending admissions, never an already sent synthesis.
	for _, provider := range []string{"qwen_audio", "cosyvoice", "volcengine", "aliyun", "aliyun_qwen", "aliyun_qwen_clone", "aliyun_cosyvoice", "qwen", "dashscope", "doubao", "volcano", ""} {
		check := checkTTSRoomPool(ctx, tx, input.TenantID, input.RoomID, provider)
		if check != nil && !errors.Is(check, ttsscheduler.ErrPoolDenied) {
			return input, check
		}
		if errors.Is(check, ttsscheduler.ErrPoolDenied) {
			_, err = tx.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET status='cancelled',finished_at=? WHERE tenant_id=? AND room_id=? AND status='queued' AND provider=?`, now, input.TenantID, input.RoomID, provider)
			if err != nil {
				return input, err
			}
		}
	}
	result, err := ttsRoomPoolPolicy(ctx, tx, input.TenantID, input.RoomID)
	if err != nil {
		return input, err
	}
	if err = tx.Commit(); err != nil {
		return input, err
	}
	return result, nil
}
