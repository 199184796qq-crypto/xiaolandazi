package db

import (
	"context"
	"livecompanion/management/internal/model"
	"time"
)

func (s *Store) SessionDownloadMaintenanceAssets(ctx context.Context, expired bool, limit int) ([]model.SessionSpeechAsset, error) {
	condition := `played_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR AND audio_url<>'' AND archive_ready=FALSE AND (archive_retry_at IS NULL OR archive_retry_at<=UTC_TIMESTAMP(3))`
	if expired {
		condition = `COALESCE(played_at,created_at)<=UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,tenant_id,room_id,session_id,source_ref,kind,text_content,audio_url,duration_ms,played_at,created_at,archive_object_key,archive_bucket,archive_ready FROM live_session_speech_assets WHERE `+condition+` ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.SessionSpeechAsset{}
	for rows.Next() {
		var a model.SessionSpeechAsset
		if err = rows.Scan(&a.ID, &a.TenantID, &a.RoomID, &a.SessionID, &a.SourceRef, &a.Kind, &a.Text, &a.AudioURL, &a.DurationMS, &a.PlayedAt, &a.CreatedAt, &a.ArchiveObjectKey, &a.ArchiveBucket, &a.ArchiveReady); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func (s *Store) ReserveSessionDownloadAudio(ctx context.Context, a model.SessionSpeechAsset, key, bucket string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_session_speech_assets SET archive_object_key=?,archive_bucket=? WHERE id=? AND tenant_id=? AND room_id=? AND session_id=? AND archive_object_key='' AND played_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`, key, bucket, a.ID, a.TenantID, a.RoomID, a.SessionID)
	return err
}

func (s *Store) DelaySessionDownloadAudio(ctx context.Context, a model.SessionSpeechAsset) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_session_speech_assets SET archive_retry_at=UTC_TIMESTAMP(3)+INTERVAL 5 MINUTE WHERE id=? AND tenant_id=? AND room_id=? AND session_id=? AND archive_ready=FALSE`, a.ID, a.TenantID, a.RoomID, a.SessionID)
	return err
}
func (s *Store) CompleteSessionDownloadAudio(ctx context.Context, a model.SessionSpeechAsset, key, address string, expiry time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_session_speech_assets SET audio_url=?,expires_at=?,archive_ready=TRUE WHERE id=? AND tenant_id=? AND room_id=? AND session_id=? AND archive_object_key=? AND played_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`, address, expiry.UTC(), a.ID, a.TenantID, a.RoomID, a.SessionID, key)
	return err
}
func (s *Store) DeleteExpiredSessionDownloadAsset(ctx context.Context, a model.SessionSpeechAsset) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM live_session_speech_assets WHERE id=? AND tenant_id=? AND room_id=? AND session_id=? AND COALESCE(played_at,created_at)<=UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`, a.ID, a.TenantID, a.RoomID, a.SessionID)
	return err
}
func (s *Store) PurgeExpiredGeneratedSpeechHistory(ctx context.Context) error {
	// Adopted policies live in agent_memory_items/versions and learning evidence.
	// Never delete those assets when removing raw session speech.
	_, err := s.db.ExecContext(ctx, `DELETE FROM live_generated_speech_history WHERE created_at<=UTC_TIMESTAMP(3)-INTERVAL 24 HOUR LIMIT 1000`)
	return err
}
