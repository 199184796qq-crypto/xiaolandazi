package db

import "context"

// Exact repeats reuse persistent knowledge before any embedding request.
func (s *Store) GetInteractionPurpose(ctx context.Context, tenant, room, plan int64, sourceID string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT content_text FROM semantic_documents WHERE tenant_id=? AND room_id=? AND plan_id=? AND content_type='interaction_purpose' AND source_id=? AND source_version=1 AND status='active' AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP(3)) ORDER BY updated_at DESC,id DESC LIMIT 1`, tenant, room, plan, sourceID).Scan(&raw)
	return raw, err
}
