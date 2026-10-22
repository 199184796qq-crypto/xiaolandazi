package db

import (
	"context"
	"fmt"
)

// mediaIndexSchema 是「话术优先」音频单元的索引表。
// 目录是资产真相，这张表负责批量删除、孤儿对账和容量统计。
const mediaIndexSchema = `CREATE TABLE IF NOT EXISTS live_media_index (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
	tenant_id BIGINT UNSIGNED NOT NULL,
	room_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	plan_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	kind VARCHAR(32) NOT NULL DEFAULT 'recording_unit',
	voice_key VARCHAR(64) NOT NULL DEFAULT '',
	script_hash VARCHAR(64) NOT NULL DEFAULT '',
	sentence_hash VARCHAR(64) NOT NULL DEFAULT '',
	object_key VARCHAR(512) NOT NULL,
	asset_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	size_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
	status VARCHAR(16) NOT NULL DEFAULT 'active',
	created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	last_used_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	PRIMARY KEY(id),
	UNIQUE KEY uq_media_index_key(tenant_id, object_key),
	KEY idx_media_index_plan(tenant_id, plan_id, status),
	KEY idx_media_index_room(tenant_id, room_id, status),
	KEY idx_media_index_script(tenant_id, plan_id, voice_key, script_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

// MediaIndexEntry 是索引表的一行。
type MediaIndexEntry struct {
	ID           int64
	TenantID     int64
	RoomID       int64
	PlanID       int64
	Kind         string
	VoiceKey     string
	ScriptHash   string
	SentenceHash string
	ObjectKey    string
	AssetID      int64
	SizeBytes    int64
	Status       string
}

// EnsureMediaIndexSchema 建表，启动时调用一次即可重复执行。
func (s *Store) EnsureMediaIndexSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, mediaIndexSchema); err != nil {
		return fmt.Errorf("创建媒体索引表: %w", err)
	}
	return nil
}

// UpsertMediaIndex 登记一个资产；同一租户同一 object_key 只保留一行。
func (s *Store) UpsertMediaIndex(ctx context.Context, entry MediaIndexEntry) error {
	kind := entry.Kind
	if kind == "" {
		kind = "recording_unit"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_media_index
			(tenant_id, room_id, plan_id, kind, voice_key, script_hash, sentence_hash, object_key, asset_id, size_bytes, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active')
		ON DUPLICATE KEY UPDATE
			room_id = VALUES(room_id), plan_id = VALUES(plan_id), kind = VALUES(kind),
			voice_key = VALUES(voice_key), script_hash = VALUES(script_hash),
			sentence_hash = VALUES(sentence_hash), asset_id = VALUES(asset_id),
			size_bytes = VALUES(size_bytes), status = 'active', last_used_at = CURRENT_TIMESTAMP(3)`,
		entry.TenantID, entry.RoomID, entry.PlanID, kind, entry.VoiceKey, entry.ScriptHash,
		entry.SentenceHash, entry.ObjectKey, entry.AssetID, entry.SizeBytes)
	if err != nil {
		return fmt.Errorf("登记媒体索引: %w", err)
	}
	return nil
}

// ListMediaIndexByScope 列出某个范围内的资产，scope 为空表示不限。
// planID、roomID 任一为 0 时不参与过滤；两者都为 0 表示整个租户。
func (s *Store) ListMediaIndexByScope(ctx context.Context, tenantID, roomID, planID int64, voiceKey, scriptHash string) ([]MediaIndexEntry, error) {
	query := `SELECT id, tenant_id, room_id, plan_id, kind, voice_key, script_hash, sentence_hash, object_key, asset_id, size_bytes, status
		FROM live_media_index WHERE tenant_id = ? AND status = 'active'`
	args := []any{tenantID}
	if roomID > 0 {
		query += " AND room_id = ?"
		args = append(args, roomID)
	}
	if planID > 0 {
		query += " AND plan_id = ?"
		args = append(args, planID)
	}
	if voiceKey != "" {
		query += " AND voice_key = ?"
		args = append(args, voiceKey)
	}
	if scriptHash != "" {
		query += " AND script_hash = ?"
		args = append(args, scriptHash)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询媒体索引: %w", err)
	}
	defer rows.Close()

	var entries []MediaIndexEntry
	for rows.Next() {
		var entry MediaIndexEntry
		if err := rows.Scan(&entry.ID, &entry.TenantID, &entry.RoomID, &entry.PlanID, &entry.Kind,
			&entry.VoiceKey, &entry.ScriptHash, &entry.SentenceHash, &entry.ObjectKey,
			&entry.AssetID, &entry.SizeBytes, &entry.Status); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// MarkMediaIndexDeleted 软删一批索引行。文件删除成功后再调用，失败可重跑。
func (s *Store) MarkMediaIndexDeleted(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE live_media_index SET status = 'deleted' WHERE id = ?`, id); err != nil {
			return fmt.Errorf("标记媒体索引删除: %w", err)
		}
	}
	return nil
}
