package db

import "context"

func (s *Store) HasSessionSpeechAsset(ctx context.Context, tenant, room, session int64, ref string) bool {
	var id int64
	return s.db.QueryRowContext(ctx, `SELECT id FROM live_session_speech_assets WHERE tenant_id=? AND room_id=? AND session_id=? AND source_ref=?`, tenant, room, session, ref).Scan(&id) == nil
}

func (s *Store) SessionHasGeneratedDecision(ctx context.Context, tenant, room, session int64, decision string) bool {
	var id int64
	return s.db.QueryRowContext(ctx, `SELECT id FROM live_generated_speech_history WHERE tenant_id=? AND room_id=? AND runtime_session_id=? AND decision_id=? LIMIT 1`, tenant, room, session, decision).Scan(&id) == nil
}

func (s *Store) ArchiveCandidateText(ctx context.Context, tenant, room int64, candidate string) string {
	var text string
	_ = s.db.QueryRowContext(ctx, `SELECT candidate_text FROM live_p0_audit_events WHERE tenant_id=? AND room_id=? AND candidate_id=? ORDER BY id DESC LIMIT 1`, tenant, room, candidate).Scan(&text)
	return text
}
