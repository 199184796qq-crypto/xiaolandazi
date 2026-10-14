package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in; temporary tables shadow business tables on one connection.
// Closing that connection drops fixtures without touching persistent records.
func TestSessionDownloadsMySQLScopeAndPlayback(t *testing.T) {
	raw := os.Getenv("SESSION_DOWNLOAD_TEST_CONFIG")
	if raw == "" {
		t.Skip("requires explicit temporary-table MySQL test configuration")
	}
	var cfg config.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal("invalid test configuration")
	}
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	s.db.SetMaxOpenConns(1)
	s.db.SetMaxIdleConns(1)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := s.db.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TEMPORARY TABLE live_runtime_sessions(id BIGINT PRIMARY KEY,tenant_id BIGINT,room_id BIGINT)`)
	exec(strings.Replace(sessionSpeechArchiveSchema, "CREATE TABLE IF NOT EXISTS", "CREATE TEMPORARY TABLE", 1))
	exec(`INSERT INTO live_runtime_sessions VALUES(3,1,2),(4,1,2),(5,8,9)`)
	at := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	expiry := at.Add(-time.Hour)
	base := model.SessionSpeechAsset{TenantID: 1, RoomID: 2, SessionID: 3, SourceRef: "first", Kind: "mainline", Text: "第一段", DurationMS: 1200}
	save := func(a model.SessionSpeechAsset) {
		t.Helper()
		if e := s.SaveSessionSpeechAsset(ctx, a); e != nil {
			t.Fatal(e)
		}
	}
	list := func(tenant, room, session int64, from, to *time.Time) []model.SessionSpeechAsset {
		t.Helper()
		v, e := s.ListSessionSpeechAssets(ctx, tenant, room, session, from, to)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	save(base)
	if len(list(1, 2, 3, nil, nil)) != 0 {
		t.Fatal("staged audio included")
	}
	base.AudioURL = "https://bucket.oss-cn-hangzhou.aliyuncs.com/a.wav"
	base.ExpiresAt = &expiry
	save(base)
	if err = s.MarkSessionSpeechPlayed(ctx, 1, 2, 3, "first", at); err != nil {
		t.Fatal(err)
	}
	base.Text = "不能覆盖实际口播"
	base.PlayedAt = &at
	save(base)
	items := list(1, 2, 3, nil, nil)
	if len(items) != 1 || items[0].Text != "第一段" || items[0].Available || items[0].ExpiresAt == nil {
		t.Fatal("history/expiry update incorrect", items)
	}
	for _, scope := range [][3]int64{{8, 2, 3}, {1, 9, 3}, {1, 2, 4}} {
		if len(list(scope[0], scope[1], scope[2], nil, nil)) != 0 {
			t.Fatal("cross-scope list")
		}
		if _, e := s.GetSessionSpeechAsset(ctx, scope[0], scope[1], scope[2], items[0].ID); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("cross-scope audio", e)
		}
	}
	wrong := base
	wrong.SessionID = 5
	wrong.SourceRef = "wrong"
	save(wrong)
	if len(list(1, 2, 5, nil, nil)) != 0 {
		t.Fatal("mismatched session inserted")
	}
	for i, kind := range []string{"interaction", "bridge"} {
		a := base
		a.SourceRef = kind
		a.Kind = kind
		a.Text = kind
		v := at.Add(time.Duration(i+1) * time.Second)
		a.PlayedAt = &v
		save(a)
	}
	end := at.Add(time.Second)
	if len(list(1, 2, 3, &at, &end)) != 1 {
		t.Fatal("half-open range incorrect")
	}
	if len(list(1, 2, 3, nil, nil)) != 3 {
		t.Fatal("missing speech kinds")
	}
	for i := 0; i < 2; i++ {
		if e := s.RecordSessionRecordingPlayback(ctx, 1, 2, 3, "first", at.Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
	}
	if len(list(1, 2, 3, nil, nil)) != 4 {
		t.Fatal("recording receipt not idempotent")
	}
	// Retention applies at read time before cleanup, including audio-by-ID.
	exec(`INSERT INTO live_session_speech_assets(tenant_id,room_id,session_id,source_ref,kind,text_content,audio_url,played_at) VALUES(1,2,3,'expired','mainline','到期原稿','',UTC_TIMESTAMP(3)-INTERVAL 25 HOUR)`)
	var oldID int64
	if e := s.db.QueryRowContext(ctx, `SELECT id FROM live_session_speech_assets WHERE source_ref='expired'`).Scan(&oldID); e != nil {
		t.Fatal(e)
	}
	if len(list(1, 2, 3, nil, nil)) != 4 {
		t.Fatal("expired transcript returned")
	}
	if _, e := s.GetSessionSpeechAsset(ctx, 1, 2, 3, oldID); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("expired audio returned", e)
	}
	expired, e := s.SessionDownloadMaintenanceAssets(ctx, true, 100)
	if e != nil || len(expired) != 1 {
		t.Fatal("expiry cleanup list", expired, e)
	}
	if e = s.DeleteExpiredSessionDownloadAsset(ctx, expired[0]); e != nil {
		t.Fatal(e)
	}
	// A saved, adopted strategy is independent of its expiring raw transcript.
	exec(`CREATE TEMPORARY TABLE live_generated_speech_history(id BIGINT PRIMARY KEY,created_at DATETIME(3),generated_text TEXT)`)
	exec(`CREATE TEMPORARY TABLE agent_memory_versions(id BIGINT PRIMARY KEY,content_text TEXT)`)
	exec(`INSERT INTO live_generated_speech_history VALUES(1,UTC_TIMESTAMP(3)-INTERVAL 25 HOUR,'旧原稿'),(2,UTC_TIMESTAMP(3),'新原稿')`)
	exec(`INSERT INTO agent_memory_versions VALUES(7,'已经采纳的回答策略')`)
	if e = s.PurgeExpiredGeneratedSpeechHistory(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	var strategy string
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_generated_speech_history`).Scan(&count); e != nil || count != 1 {
		t.Fatal("raw history retention", count, e)
	}
	if e = s.db.QueryRowContext(ctx, `SELECT content_text FROM agent_memory_versions WHERE id=7`).Scan(&strategy); e != nil || strategy != "已经采纳的回答策略" {
		t.Fatal("adopted strategy removed", strategy, e)
	}
}
