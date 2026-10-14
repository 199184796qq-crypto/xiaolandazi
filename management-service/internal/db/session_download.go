package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"time"
)

const sessionSpeechArchiveSchema = `CREATE TABLE IF NOT EXISTS live_session_speech_assets (
	 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
	 tenant_id BIGINT UNSIGNED NOT NULL, room_id BIGINT UNSIGNED NOT NULL, session_id BIGINT UNSIGNED NOT NULL,
	 source_ref VARCHAR(256) NOT NULL, kind VARCHAR(24) NOT NULL, text_content MEDIUMTEXT NOT NULL,
	 audio_url TEXT NOT NULL, duration_ms INT NOT NULL DEFAULT 0,
	 archive_object_key VARCHAR(512) NOT NULL DEFAULT '', archive_bucket VARCHAR(128) NOT NULL DEFAULT '', archive_ready BOOLEAN NOT NULL DEFAULT FALSE, archive_retry_at DATETIME(3) NULL,
	 played_at DATETIME(3) NULL, expires_at DATETIME(3) NULL,
	 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	 PRIMARY KEY(id), UNIQUE KEY uk_session_source(tenant_id,room_id,session_id,source_ref),
	 KEY idx_session_played(tenant_id,room_id,session_id,played_at,id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func (s *Store) MigrateSessionDownloads(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, sessionSpeechArchiveSchema)
	return err
}

func (s *Store) SaveSessionSpeechAsset(ctx context.Context, a model.SessionSpeechAsset) error {
	if a.PlayedAt != nil && !a.PlayedAt.After(time.Now().Add(-model.SessionDownloadRetention)) {
		return nil
	}
	if a.TenantID <= 0 || a.RoomID <= 0 || a.SessionID <= 0 || a.SourceRef == "" || strings.TrimSpace(a.Text) == "" {
		return errors.New("invalid session speech archive")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_session_speech_assets
	 (tenant_id,room_id,session_id,source_ref,kind,text_content,audio_url,duration_ms,played_at,expires_at)
	 SELECT ?,?,?,?,?,?,?,?,?,? FROM live_runtime_sessions WHERE id=? AND tenant_id=? AND room_id=?
	 ON DUPLICATE KEY UPDATE played_at=COALESCE(played_at,VALUES(played_at)),
	 expires_at=IF(audio_url='',VALUES(expires_at),expires_at),
	 audio_url=IF(audio_url='',VALUES(audio_url),audio_url), duration_ms=GREATEST(duration_ms,VALUES(duration_ms))`,
		a.TenantID, a.RoomID, a.SessionID, a.SourceRef, a.Kind, strings.TrimSpace(a.Text), a.AudioURL, a.DurationMS, a.PlayedAt, a.ExpiresAt, a.SessionID, a.TenantID, a.RoomID)
	return err
}

// Recording mode can replay the same track. Each confirmed run has its own
// start time; resumptions and repeated observer reads remain idempotent.
func (s *Store) RecordSessionRecordingPlayback(ctx context.Context, tenant, room, session int64, ref string, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	var latestRef string
	err := s.db.QueryRowContext(ctx, `SELECT source_ref FROM live_session_speech_assets WHERE tenant_id=? AND room_id=? AND session_id=? AND kind='mainline' AND played_at IS NOT NULL ORDER BY played_at DESC,id DESC LIMIT 1`, tenant, room, session).Scan(&latestRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if strings.HasPrefix(latestRef, ref+":") {
		return nil
	}
	a := model.SessionSpeechAsset{TenantID: tenant, RoomID: room, SessionID: session, SourceRef: ref + ":" + at.UTC().Format(time.RFC3339Nano), PlayedAt: &at}
	var expiry sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT kind,text_content,audio_url,duration_ms,expires_at FROM live_session_speech_assets WHERE tenant_id=? AND room_id=? AND session_id=? AND source_ref=?`, tenant, room, session, ref).Scan(&a.Kind, &a.Text, &a.AudioURL, &a.DurationMS, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if expiry.Valid {
		a.ExpiresAt = &expiry.Time
	}
	return s.SaveSessionSpeechAsset(ctx, a)
}

func (s *Store) MarkSessionSpeechPlayed(ctx context.Context, tenant, room, session int64, ref string, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE live_session_speech_assets SET played_at=?
	 WHERE tenant_id=? AND room_id=? AND session_id=? AND source_ref=? AND played_at IS NULL`, at.UTC(), tenant, room, session, ref)
	return err
}

func (s *Store) ListSessionSpeechAssets(ctx context.Context, tenant, room, session int64, from, to *time.Time) ([]model.SessionSpeechAsset, error) {
	query := `SELECT id,source_ref,kind,text_content,audio_url,duration_ms,played_at,expires_at,created_at,archive_object_key,archive_bucket,archive_ready FROM live_session_speech_assets
	 WHERE tenant_id=? AND room_id=? AND session_id=? AND played_at IS NOT NULL AND played_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`
	args := []any{tenant, room, session}
	if from != nil {
		query += " AND played_at>=?"
		args = append(args, from.UTC())
	}
	if to != nil {
		query += " AND played_at<?"
		args = append(args, to.UTC())
	}
	query += " ORDER BY played_at,id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.SessionSpeechAsset{}
	for rows.Next() {
		a := model.SessionSpeechAsset{TenantID: tenant, RoomID: room, SessionID: session}
		var at, expiry sql.NullTime
		if err = rows.Scan(&a.ID, &a.SourceRef, &a.Kind, &a.Text, &a.AudioURL, &a.DurationMS, &at, &expiry, &a.CreatedAt, &a.ArchiveObjectKey, &a.ArchiveBucket, &a.ArchiveReady); err != nil {
			return nil, err
		}
		if at.Valid {
			a.PlayedAt = &at.Time
		}
		if expiry.Valid {
			a.ExpiresAt = &expiry.Time
		}
		a.Available = a.AudioURL != "" && (!expiry.Valid || time.Now().Before(expiry.Time))
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Store) GetSessionSpeechAsset(ctx context.Context, tenant, room, session, id int64) (model.SessionSpeechAsset, error) {
	a := model.SessionSpeechAsset{TenantID: tenant, RoomID: room, SessionID: session}
	var at, expiry sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT id,source_ref,kind,text_content,audio_url,duration_ms,played_at,expires_at,created_at,archive_object_key,archive_bucket,archive_ready FROM live_session_speech_assets WHERE tenant_id=? AND room_id=? AND session_id=? AND id=? AND played_at IS NOT NULL AND played_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`, tenant, room, session, id).Scan(&a.ID, &a.SourceRef, &a.Kind, &a.Text, &a.AudioURL, &a.DurationMS, &at, &expiry, &a.CreatedAt, &a.ArchiveObjectKey, &a.ArchiveBucket, &a.ArchiveReady)
	if at.Valid {
		a.PlayedAt = &at.Time
	}
	if expiry.Valid {
		a.ExpiresAt = &expiry.Time
	}
	a.Available = a.AudioURL != "" && (!expiry.Valid || time.Now().Before(expiry.Time))
	return a, err
}
