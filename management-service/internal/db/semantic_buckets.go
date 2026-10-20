package db

import (
	"context"
	"strings"

	"livecompanion/management/internal/semantic"
)

// ListPurposeBuckets returns the active purpose buckets for one room, newest
// first. Rooms are fully isolated: buckets never cross room boundaries.
func (s *Store) ListPurposeBuckets(ctx context.Context, tenant, room, plan int64, limit int) ([]semantic.PurposeBucket, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, content_text, source_version
		FROM semantic_documents
		WHERE tenant_id=? AND room_id=? AND plan_id=? AND content_type='purpose_bucket'
		  AND status='active' AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP(3))
		ORDER BY updated_at DESC, id DESC LIMIT ?`,
		tenant, room, plan, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := make([]semantic.PurposeBucket, 0)
	for rows.Next() {
		var b semantic.PurposeBucket
		var conclusionVersion int64
		if err := rows.Scan(&b.SourceID, &b.Question, &conclusionVersion); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// UpsertPurposeBucket writes (or refreshes) a purpose bucket for one room. The
// conclusion JSON is persisted in a companion row so the question text stays the
// only payload the judge model needs to consume on the hot path.
func (s *Store) UpsertPurposeBucket(ctx context.Context, tenant, room, plan int64, sourceID, question, conclusion string) error {
	sourceID = strings.TrimSpace(sourceID)
	question = strings.TrimSpace(question)
	conclusion = strings.TrimSpace(conclusion)
	if sourceID == "" || question == "" {
		return nil
	}
	// Row 1: the question text (what the judge model reads).
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO semantic_documents (
			tenant_id, room_id, plan_id, content_type, source_id, source_version, status,
			content_text, text_hash, embedding_model, embedding_dimensions, embedding_blob, expires_at
		) VALUES (?, ?, ?, 'purpose_bucket', ?, 1, 'active', ?, ?, '', 0, NULL, NULL)
		ON DUPLICATE KEY UPDATE
			status='active',
			content_text=VALUES(content_text),
			text_hash=VALUES(text_hash),
			updated_at=CURRENT_TIMESTAMP(3)`,
		tenant, room, plan, sourceID, question, semantic.HashText(question)); err != nil {
		return err
	}
	// Row 2: the conclusion JSON (the reusable purpose result).
	if conclusion != "" {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO semantic_documents (
				tenant_id, room_id, plan_id, content_type, source_id, source_version, status,
				content_text, text_hash, embedding_model, embedding_dimensions, embedding_blob, expires_at
			) VALUES (?, ?, ?, 'purpose_bucket', ?, 2, 'active', ?, ?, '', 0, NULL, NULL)
			ON DUPLICATE KEY UPDATE
				status='active',
				content_text=VALUES(content_text),
				text_hash=VALUES(text_hash),
				updated_at=CURRENT_TIMESTAMP(3)`,
			tenant, room, plan, sourceID, conclusion, semantic.HashText(conclusion)); err != nil {
			return err
		}
	}
	return nil
}

// GetPurposeBucketConclusion returns the conclusion JSON for a matched bucket,
// so the judge-model hit can reuse the full purpose result without re-running
// the resolution model.
func (s *Store) GetPurposeBucketConclusion(ctx context.Context, tenant, room, plan int64, sourceID string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT content_text FROM semantic_documents
		WHERE tenant_id=? AND room_id=? AND plan_id=? AND content_type='purpose_bucket'
		  AND source_id=? AND source_version=2 AND status='active'
		  AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP(3))
		LIMIT 1`, tenant, room, plan, sourceID).Scan(&raw)
	return raw, err
}
