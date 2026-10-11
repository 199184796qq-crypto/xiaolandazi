package db

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
)

// Permanent plan assets; session counters stay in the run's Redis state.
func (s *Store) MigrateAngleDirectionPools(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS live_angle_direction_generations (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,pool_key CHAR(64) NOT NULL,generation_id VARCHAR(64) NOT NULL,source_json JSON NOT NULL,plan_json JSON NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),PRIMARY KEY(tenant_id,plan_id,generation_id),KEY idx_angle_pool(tenant_id,plan_id,pool_key,created_at)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_angle_direction_plays (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,pool_key CHAR(64) NOT NULL,program_id VARCHAR(128) NOT NULL,track_id VARCHAR(128) NOT NULL,hook_id VARCHAR(128) NOT NULL,started_at VARCHAR(64) NOT NULL,PRIMARY KEY(tenant_id,plan_id,program_id,track_id),KEY idx_angle_use(tenant_id,plan_id,pool_key,hook_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_angle_direction_reviews (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,pool_key CHAR(64) NOT NULL,content_sha CHAR(64) NOT NULL,context_sha CHAR(64) NOT NULL,receipt_json JSON NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),PRIMARY KEY(tenant_id,plan_id,pool_key,content_sha,context_sha)) ENGINE=InnoDB`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadAngleDirectionReviews(ctx context.Context, tenant, plan int64, key, contextSHA string) ([]anchorcycle.DirectionReviewReceipt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT receipt_json FROM live_angle_direction_reviews WHERE tenant_id=? AND plan_id=? AND pool_key=? AND context_sha=?`, tenant, plan, key, contextSHA)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []anchorcycle.DirectionReviewReceipt{}
	for rows.Next() {
		var raw []byte
		var r anchorcycle.DirectionReviewReceipt
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SaveAngleDirectionReview(ctx context.Context, tenant, plan int64, key string, r anchorcycle.DirectionReviewReceipt) error {
	if !r.Completed() {
		return fmt.Errorf("未完成的方向审核不能保存为可复用判断")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO live_angle_direction_reviews(tenant_id,plan_id,pool_key,content_sha,context_sha,receipt_json) VALUES(?,?,?,?,?,?)`, tenant, plan, key, r.ContentSHA, r.ContextSHA, raw)
	return err
}
func (s *Store) LoadAngleDirections(ctx context.Context, tenant, plan int64, key string) (*anchorcycle.AnglePlan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT plan_json FROM live_angle_direction_generations WHERE tenant_id=? AND plan_id=? AND pool_key=? ORDER BY created_at,generation_id`, tenant, plan, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result *anchorcycle.AnglePlan
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p anchorcycle.AnglePlan
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if result == nil {
			result = &anchorcycle.AnglePlan{}
		}
		cards := anchorcycle.DistinctDirectionCards(result.Cards, p.Cards)
		candidates := anchorcycle.DistinctDirectionCards(result.ReviewCandidates, p.GeneratedCards, p.Cards)
		n := result.GenerationCount + 1
		*result = p
		result.Cards = cards
		result.ReviewCandidates = candidates
		result.GenerationCount = n
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	// A saved generation may only contain receipts for its newly added cards.
	// Reconcile the accumulated candidate history with the current review context
	// and the persistent receipt cache before selecting cards in a new run.
	result.ReviewContextSHA = ""
	counts, err := s.db.QueryContext(ctx, `SELECT hook_id,COUNT(*) FROM live_angle_direction_plays WHERE tenant_id=? AND plan_id=? AND pool_key=? GROUP BY hook_id`, tenant, plan, key)
	if err != nil {
		return nil, err
	}
	defer counts.Close()
	result.LifetimePlays = map[string]int{}
	for counts.Next() {
		var id string
		var n int
		if err = counts.Scan(&id, &n); err != nil {
			return nil, err
		}
		result.LifetimePlays[id] = n
	}
	return result, counts.Err()
}
func (s *Store) AppendAngleDirections(ctx context.Context, tenant, plan int64, key string, source any, p *anchorcycle.AnglePlan) error {
	src, err := json.Marshal(source)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_angle_direction_generations(tenant_id,plan_id,pool_key,generation_id,source_json,plan_json) VALUES(?,?,?,?,?,?)`, tenant, plan, key, p.GenerationID, src, raw)
	return err
}
func (s *Store) RecordAngleDirectionPlay(ctx context.Context, tenant, plan int64, program string, u anchorcycle.AnglePlayedUse) error {
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO live_angle_direction_plays(tenant_id,plan_id,pool_key,program_id,track_id,hook_id,started_at) VALUES(?,?,?,?,?,?,?)`, tenant, plan, u.PoolKey, program, u.TrackID, u.HookID, u.StartedAt)
	return err
}
