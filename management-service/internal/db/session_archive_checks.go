package db

import "context"

func (s *Store) HasSessionSpeechAudio(ctx context.Context, tenant, room, session int64, ref string) bool {
	var id int64
	return s.db.QueryRowContext(ctx, `SELECT id FROM live_session_speech_assets WHERE tenant_id=? AND room_id=? AND session_id=? AND source_ref=? AND audio_url<>''`, tenant, room, session, ref).Scan(&id) == nil
}

func (s *Store) SessionHasGeneratedDecision(ctx context.Context, tenant, room, session int64, decision string) bool {
	var id int64
	return s.db.QueryRowContext(ctx, `SELECT id FROM live_generated_speech_history WHERE tenant_id=? AND room_id=? AND runtime_session_id=? AND decision_id=? LIMIT 1`, tenant, room, session, decision).Scan(&id) == nil
}
