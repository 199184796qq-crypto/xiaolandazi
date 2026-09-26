package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) MigrateSpeechAnalysis(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS live_speech_analysis_tasks (
			id BIGINT NOT NULL AUTO_INCREMENT,
			tenant_id BIGINT NOT NULL,
			room_id BIGINT NOT NULL,
			recording_id VARCHAR(128) NOT NULL,
			recording_started_at DATETIME(3) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'queued',
			stage VARCHAR(64) NOT NULL DEFAULT '等待处理',
			progress INT NOT NULL DEFAULT 0,
			error_message TEXT NULL,
			asr_task_id VARCHAR(128) NOT NULL DEFAULT '',
			audio_object_key VARCHAR(1024) NOT NULL DEFAULT '',
			transcript_object_key VARCHAR(1024) NOT NULL DEFAULT '',
			report_object_key VARCHAR(1024) NOT NULL DEFAULT '',
			report_file_name VARCHAR(255) NOT NULL DEFAULT '',
			created_by_user_id BIGINT NOT NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			finished_at DATETIME(3) NULL,
			PRIMARY KEY (id),
			KEY idx_speech_analysis_room (tenant_id, room_id, id),
			KEY idx_speech_analysis_recording (tenant_id, room_id, recording_id),
			KEY idx_speech_analysis_status (status, updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return fmt.Errorf("create live_speech_analysis_tasks: %w", err)
	}
	return nil
}

func (s *Store) CreateSpeechAnalysisTask(
	ctx context.Context,
	tenantID, roomID, userID int64,
	recordingID string,
	recordingStartedAt time.Time,
	reportFileName string,
) (model.SpeechAnalysisTask, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_speech_analysis_tasks (
			tenant_id, room_id, recording_id, recording_started_at,
			status, stage, progress, report_file_name, created_by_user_id
		) VALUES (?, ?, ?, ?, 'queued', '等待处理', 0, ?, ?)
	`, tenantID, roomID, recordingID, recordingStartedAt.UTC(), reportFileName, userID)
	if err != nil {
		return model.SpeechAnalysisTask{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.SpeechAnalysisTask{}, err
	}
	return s.GetSpeechAnalysisTask(ctx, tenantID, roomID, id)
}

func (s *Store) GetLatestSpeechAnalysisTask(ctx context.Context, tenantID, roomID int64) (model.SpeechAnalysisTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, room_id, recording_id, recording_started_at,
		       status, stage, progress, COALESCE(error_message, ''), asr_task_id,
		       audio_object_key, transcript_object_key, report_object_key, report_file_name,
		       created_by_user_id, created_at, updated_at, finished_at
		FROM live_speech_analysis_tasks
		WHERE tenant_id=? AND room_id=?
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, roomID)
	return scanSpeechAnalysisTask(row)
}

func (s *Store) GetSpeechAnalysisTask(ctx context.Context, tenantID, roomID, taskID int64) (model.SpeechAnalysisTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, room_id, recording_id, recording_started_at,
		       status, stage, progress, COALESCE(error_message, ''), asr_task_id,
		       audio_object_key, transcript_object_key, report_object_key, report_file_name,
		       created_by_user_id, created_at, updated_at, finished_at
		FROM live_speech_analysis_tasks
		WHERE tenant_id=? AND room_id=? AND id=?
		LIMIT 1
	`, tenantID, roomID, taskID)
	return scanSpeechAnalysisTask(row)
}

func (s *Store) SaveSpeechAnalysisTask(ctx context.Context, task model.SpeechAnalysisTask) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE live_speech_analysis_tasks
		SET status=?, stage=?, progress=?, error_message=?, asr_task_id=?,
		    audio_object_key=?, transcript_object_key=?, report_object_key=?,
		    report_file_name=?, finished_at=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND room_id=?
	`,
		task.Status,
		task.Stage,
		task.Progress,
		task.ErrorMessage,
		task.ASRTaskID,
		task.AudioObjectKey,
		task.TranscriptObjectKey,
		task.ReportObjectKey,
		task.ReportFileName,
		task.FinishedAt,
		task.ID,
		task.TenantID,
		task.RoomID,
	)
	return err
}

func (s *Store) ListSpeechAnalysisTasksNeedingAudioCleanup(ctx context.Context, before time.Time, limit int) ([]model.SpeechAnalysisTask, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, room_id, recording_id, recording_started_at,
		       status, stage, progress, COALESCE(error_message, ''), asr_task_id,
		       audio_object_key, transcript_object_key, report_object_key, report_file_name,
		       created_by_user_id, created_at, updated_at, finished_at
		FROM live_speech_analysis_tasks
		WHERE audio_object_key <> ''
		  AND (transcript_object_key <> '' OR created_at < ?)
		ORDER BY created_at ASC
		LIMIT ?
	`, before.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SpeechAnalysisTask, 0)
	for rows.Next() {
		item, err := scanSpeechAnalysisTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ClearSpeechAnalysisAudioObjectKey(
	ctx context.Context,
	taskID, tenantID, roomID int64,
	expectedObjectKey string,
) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE live_speech_analysis_tasks
		SET audio_object_key='', updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND room_id=? AND audio_object_key=?
	`, taskID, tenantID, roomID, expectedObjectKey)
	return err
}

type speechTaskScanner interface {
	Scan(...any) error
}

func scanSpeechAnalysisTask(row speechTaskScanner) (model.SpeechAnalysisTask, error) {
	var item model.SpeechAnalysisTask
	var finishedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.RoomID,
		&item.RecordingID,
		&item.RecordingStartedAt,
		&item.Status,
		&item.Stage,
		&item.Progress,
		&item.ErrorMessage,
		&item.ASRTaskID,
		&item.AudioObjectKey,
		&item.TranscriptObjectKey,
		&item.ReportObjectKey,
		&item.ReportFileName,
		&item.CreatedByUserID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&finishedAt,
	)
	if err != nil {
		return model.SpeechAnalysisTask{}, err
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}

func (s *Store) LatestSpeechAnalysisForRecording(
	ctx context.Context,
	tenantID, roomID int64,
	recordingID string,
) (model.SpeechAnalysisTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, room_id, recording_id, recording_started_at,
		       status, stage, progress, COALESCE(error_message, ''), asr_task_id,
		       audio_object_key, transcript_object_key, report_object_key, report_file_name,
		       created_by_user_id, created_at, updated_at, finished_at
		FROM live_speech_analysis_tasks
		WHERE tenant_id=? AND room_id=? AND recording_id=?
		ORDER BY id DESC
		LIMIT 1
	`, tenantID, roomID, recordingID)
	return scanSpeechAnalysisTask(row)
}

func IsSpeechAnalysisMissing(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
