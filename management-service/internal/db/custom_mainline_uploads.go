package db

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/model"
	"time"
)

func (s *Store) MigrateCustomMainlineUploads(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_custom_mainline_uploads (
		id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
		tenant_id BIGINT NOT NULL, plan_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
		files_json LONGTEXT NOT NULL, duration_ms BIGINT NOT NULL DEFAULT 0,
		status VARCHAR(32) NOT NULL, error_message TEXT NOT NULL, result_json LONGTEXT NULL,
		cleaned TINYINT NOT NULL DEFAULT 0,
		created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		KEY idx_custom_upload_scope (tenant_id, plan_id, user_id),
		KEY idx_custom_upload_queue (status, created_at),
		KEY idx_custom_upload_cleanup (cleaned, created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

const customUploadColumns = `id,tenant_id,plan_id,user_id,files_json,duration_ms,status,error_message,COALESCE(result_json,''),created_at,updated_at,cleaned`

func scanCustomUpload(row interface{ Scan(...any) error }) (model.CustomMainlineUpload, error) {
	var item model.CustomMainlineUpload
	var files, result string
	err := row.Scan(&item.ID, &item.TenantID, &item.PlanID, &item.UserID, &files, &item.DurationMS, &item.Status, &item.Error, &result, &item.CreatedAt, &item.UpdatedAt, &item.Cleaned)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(files), &item.Files); err != nil {
		return item, err
	}
	if result != "" {
		item.Result = json.RawMessage(result)
	}
	return item, nil
}

func (s *Store) CreateCustomMainlineUpload(ctx context.Context, item model.CustomMainlineUpload) error {
	files, err := json.Marshal(item.Files)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_custom_mainline_uploads (id,tenant_id,plan_id,user_id,files_json,duration_ms,status,error_message,created_at,updated_at) VALUES (?,?,?,?,?,?,'uploading','',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, item.ID, item.TenantID, item.PlanID, item.UserID, files, item.DurationMS)
	return err
}

func (s *Store) GetCustomMainlineUpload(ctx context.Context, tenantID, planID, userID int64, id string) (model.CustomMainlineUpload, error) {
	return scanCustomUpload(s.db.QueryRowContext(ctx, `SELECT `+customUploadColumns+` FROM live_custom_mainline_uploads WHERE tenant_id=? AND plan_id=? AND user_id=? AND id=?`, tenantID, planID, userID, id))
}

// Ready receipts belong to their uploading user and plan. Workspace recovery is
// read-only; it never requeues ASR/cloning or changes a saved version.
func (s *Store) ListReadyCustomMainlineUploads(ctx context.Context, tenantID, planID, userID int64) ([]model.CustomMainlineUpload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+customUploadColumns+` FROM live_custom_mainline_uploads WHERE tenant_id=? AND plan_id=? AND user_id=? AND status='ready' AND result_json IS NOT NULL ORDER BY created_at DESC,id DESC LIMIT 20`, tenantID, planID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.CustomMainlineUpload, 0)
	for rows.Next() {
		item, err := scanCustomUpload(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Present the latest bounded set in upload order.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func (s *Store) TransitionCustomMainlineUpload(ctx context.Context, item model.CustomMainlineUpload, from, to, message string, result json.RawMessage) (bool, error) {
	var resultValue any
	if len(result) > 0 {
		if !json.Valid(result) {
			return false, fmt.Errorf("invalid upload result")
		}
		resultValue = string(result)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET status=?,error_message=?,result_json=?,updated_at=UTC_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND user_id=? AND id=? AND status=?`, to, message, resultValue, item.TenantID, item.PlanID, item.UserID, item.ID, from)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

func (s *Store) NextCustomMainlineUpload(ctx context.Context) (model.CustomMainlineUpload, error) {
	return scanCustomUpload(s.db.QueryRowContext(ctx, `SELECT `+customUploadColumns+` FROM live_custom_mainline_uploads WHERE status='queued' ORDER BY created_at,id LIMIT 1`))
}

func (s *Store) HeartbeatCustomMainlineUpload(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET updated_at=UTC_TIMESTAMP(3) WHERE id=? AND status='processing'`, id)
	return err
}

func (s *Store) MaintainCustomMainlineUploads(ctx context.Context) error {
	// A crashed paid operation is not replayed automatically. The user can retry explicitly.
	_, err := s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET status='failed',error_message='后台处理被中断，请重新上传；不会自动重复识别或声音复刻',updated_at=UTC_TIMESTAMP(3) WHERE status='processing' AND updated_at<?`, time.Now().UTC().Add(-3*time.Minute))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET status='expired',error_message='上传授权已过期，请重新选择文件',updated_at=UTC_TIMESTAMP(3) WHERE status='uploading' AND created_at<?`, time.Now().UTC().Add(-2*time.Hour))
	return err
}

func (s *Store) CustomMainlineUploadsToClean(ctx context.Context) ([]model.CustomMainlineUpload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+customUploadColumns+` FROM live_custom_mainline_uploads WHERE cleaned=0 AND status NOT IN ('queued','processing') AND created_at<? ORDER BY created_at LIMIT 50`, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.CustomMainlineUpload{}
	for rows.Next() {
		item, err := scanCustomUpload(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkCustomMainlineUploadCleaned(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET cleaned=1 WHERE id=? AND status NOT IN ('queued','processing')`, id)
	return err
}
