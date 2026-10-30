package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCustomMainlineUploadsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	for range 2 {
		if err := s.MigrateCustomMainlineUploads(ctx); err != nil {
			t.Fatal(err)
		}
	}
	job := model.CustomMainlineUpload{ID: "owned-random-upload", TenantID: 14, PlanID: 21, UserID: 8, Files: []model.CustomMainlineUploadFile{{Name: "原音.wav", Size: 2 << 30, Key: "source", SnapshotKey: "verified", ContentType: "audio/wav"}}, DurationMS: 1200}
	if err := s.CreateCustomMainlineUpload(ctx, job); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetCustomMainlineUpload(ctx, 14, 21, 8, job.ID)
	if err != nil || loaded.Files[0].Size != 2<<30 || loaded.Status != "uploading" {
		t.Fatal(loaded, err)
	}
	if age := time.Since(loaded.CreatedAt); age < -time.Second || age > 5*time.Second {
		t.Fatal("upload timestamps are not UTC", age)
	}
	for _, scope := range [][3]int64{{15, 21, 8}, {14, 22, 8}, {14, 21, 9}} {
		if _, err = s.GetCustomMainlineUpload(ctx, scope[0], scope[1], scope[2], job.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("scope leak", scope, err)
		}
	}
	for _, transition := range [][2]string{{"uploading", "queued"}, {"queued", "processing"}} {
		var winners atomic.Int32
		var wg sync.WaitGroup
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := s.TransitionCustomMainlineUpload(ctx, job, transition[0], transition[1], "", nil)
				if err != nil {
					t.Error(err)
				}
				if ok {
					winners.Add(1)
				}
			}()
		}
		wg.Wait()
		if winners.Load() != 1 {
			t.Fatal("duplicate paid processing admission", winners.Load())
		}
	}
	if err = s.HeartbeatCustomMainlineUpload(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	// Cancel wins over a late worker result; completed result is never revived.
	if ok, err := s.TransitionCustomMainlineUpload(ctx, job, "processing", "cancelled", "取消", nil); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := s.TransitionCustomMainlineUpload(ctx, job, "processing", "ready", "", json.RawMessage(`{"audio_asset_id":99}`)); ok || err != nil {
		t.Fatal("late worker overwrote cancellation", ok, err)
	}
	job.ID = "crashed-upload"
	if err = s.CreateCustomMainlineUpload(ctx, job); err != nil {
		t.Fatal(err)
	}
	_, _ = s.TransitionCustomMainlineUpload(ctx, job, "uploading", "queued", "", nil)
	_, _ = s.TransitionCustomMainlineUpload(ctx, job, "queued", "processing", "", nil)
	_, err = s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET updated_at=? WHERE id=?`, time.Now().UTC().Add(-4*time.Minute), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MaintainCustomMainlineUploads(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.GetCustomMainlineUpload(ctx, 14, 21, 8, job.ID)
	if err != nil || loaded.Status != "failed" {
		t.Fatal("crashed paid work replayed", loaded, err)
	}
	if _, err = s.NextCustomMainlineUpload(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed job requeued", err)
	}
	items, err := s.CustomMainlineUploadsToClean(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("deleted fresh staging object", items, err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE live_custom_mainline_uploads SET created_at=? WHERE id=?`, time.Now().UTC().Add(-25*time.Hour), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	items, err = s.CustomMainlineUploadsToClean(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if err = s.MarkCustomMainlineUploadCleaned(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	items, err = s.CustomMainlineUploadsToClean(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	loaded, err = s.GetCustomMainlineUpload(ctx, 14, 21, 8, job.ID)
	if err != nil || !loaded.Cleaned || loaded.Status != "failed" {
		t.Fatal("receipt lost", loaded, err)
	}
}
