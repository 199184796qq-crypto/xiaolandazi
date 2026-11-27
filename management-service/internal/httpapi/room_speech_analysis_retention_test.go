package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	assetstorage "livecompanion/management/internal/storage"
)

func TestSpeechAnalysisRetentionBoundary(t *testing.T) {
	finished := time.Now().UTC().Add(-3 * time.Hour)
	task := model.SpeechAnalysisTask{ID: 17, CreatedAt: finished.Add(-time.Hour), FinishedAt: &finished}
	deadline := finished.Add(24 * time.Hour)
	var response speechAnalysisStatusResponse
	response.setTask(task, deadline.Add(-time.Millisecond))
	if response.Task == nil || !response.ExpiresAt.Equal(deadline) {
		t.Fatalf("available result lost its expiry: %+v", response)
	}
	response = speechAnalysisStatusResponse{}
	response.setTask(task, deadline)
	if response.Task != nil || response.ExpiresAt != nil {
		t.Fatalf("expired result returned: %+v", response)
	}
	task.FinishedAt = nil
	if got := speechAnalysisExpiresAt(task); !got.Equal(task.CreatedAt.Add(24 * time.Hour)) {
		t.Fatalf("unfinished result deadline = %v", got)
	}
	for _, test := range []struct {
		name, query string
		age         time.Duration
		code        int
	}{
		{"current task", "?task_id=17", time.Hour, http.StatusOK},
		{"older task link", "?task_id=16", time.Hour, http.StatusNotFound},
		{"expired result link", "?task_id=17", 25 * time.Hour, http.StatusGone},
		{"expired old client", "", 25 * time.Hour, http.StatusGone},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := task
			task.CreatedAt = time.Now().Add(-test.age)
			r := httptest.NewRequest(http.MethodGet, "/speech-analysis/transcript"+test.query, nil)
			w := httptest.NewRecorder()
			if got := speechAnalysisResultAvailable(w, r, task); got != (test.code == http.StatusOK) || w.Code != test.code {
				t.Fatalf("available=%v, status=%d want %d", got, w.Code, test.code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("result response can be cached")
			}
		})
	}
}

type speechAnalysisCleanupStore struct {
	assetstorage.Store
	deleted map[string]int
	failure string
}

func (s *speechAnalysisCleanupStore) Delete(_ context.Context, key string) error {
	if key == s.failure {
		return errors.New("temporary failure")
	}
	s.deleted[key]++
	return nil
}

func TestSpeechAnalysisCleanupMySQL(t *testing.T) {
	port := os.Getenv("XIAOLAN_CAPTURE_TEST_DB_PORT")
	if port == "" {
		t.Skip("isolated localhost MySQL required")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1024 || number > 65535 || number == 3306 {
		t.Fatal("invalid isolated MySQL port")
	}
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr = "root", "tcp", net.JoinHostPort("127.0.0.1", port)
	connection, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx := context.Background()
	schema := "speech_analysis_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := connection.ExecContext(ctx, "CREATE DATABASE "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := connection.ExecContext(ctx, "DROP DATABASE "+schema); err != nil {
			t.Error(err)
		}
	}()
	store, err := appdb.Open(config.Config{DBHost: "127.0.0.1", DBPort: port, DBUser: "root", DBName: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateSpeechAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	save := func(recording string, age time.Duration, legacy bool) model.SpeechAnalysisTask {
		t.Helper()
		task, err := store.CreateSpeechAnalysisTask(ctx, 3, 17, 7, recording, time.Now().UTC(), "report.md")
		if err != nil {
			t.Fatal(err)
		}
		finished := time.Now().UTC().Add(-age)
		task.FinishedAt, task.Status = &finished, model.SpeechAnalysisReady
		prefix := speechAnalysisTaskPrefix(task)
		if legacy {
			prefix = speechAnalysisRecordingPrefix(task)
		}
		task.TranscriptObjectKey, task.ReportObjectKey = path.Join(prefix, "transcript.txt"), path.Join(prefix, "analysis.md")
		if err := store.SaveSpeechAnalysisTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	old := save("shared", 26*time.Hour, true)
	recent := save("shared", time.Hour, true)
	expired := save("upload-sample", 25*time.Hour, false)
	mismatch := save("other", 25*time.Hour, false)
	mismatch.ReportObjectKey = "unrelated-storage/analysis.md"
	if err := store.SaveSpeechAnalysisTask(ctx, mismatch); err != nil {
		t.Fatal(err)
	}
	cloud := &speechAnalysisCleanupStore{deleted: make(map[string]int), failure: expired.ReportObjectKey}
	server := Server{store: store}
	server.cleanupSpeechAnalysisResults(ctx, cloud)
	get := func(task model.SpeechAnalysisTask) model.SpeechAnalysisTask {
		t.Helper()
		row, err := store.GetSpeechAnalysisTask(ctx, task.TenantID, task.RoomID, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	if get(old).TranscriptObjectKey == "" || get(recent).ReportObjectKey == "" ||
		cloud.deleted[old.TranscriptObjectKey] != 0 || cloud.deleted[old.ReportObjectKey] != 0 {
		t.Fatal("a legacy object with an unexpired reference was deleted")
	}
	if got := get(expired); got.TranscriptObjectKey != "" || got.ReportObjectKey != expired.ReportObjectKey ||
		cloud.deleted[expired.TranscriptObjectKey] != 1 {
		t.Fatalf("partial cleanup did not retain the failed object: %+v", got)
	}
	if cloud.deleted[mismatch.ReportObjectKey] != 0 || get(mismatch).ReportObjectKey != mismatch.ReportObjectKey {
		t.Fatal("an object outside the task namespace was deleted")
	}
	cloud.failure = ""
	server.cleanupSpeechAnalysisResults(ctx, cloud)
	if got := get(expired); got.ReportObjectKey != "" || cloud.deleted[expired.ReportObjectKey] != 1 ||
		cloud.deleted[expired.TranscriptObjectKey] != 1 {
		t.Fatalf("retry did not finish just the remaining object: %+v", got)
	}
}
