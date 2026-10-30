package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	auditlog "livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type auditBodyLedger struct {
	mu               sync.Mutex
	begun, completed []model.AdminAuditLog
}

func (l *auditBodyLedger) BeginAdminAudit(_ context.Context, e model.AdminAuditLog) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.begun = append(l.begun, e)
	return "fixture", nil
}
func (l *auditBodyLedger) CompleteAdminAudit(_ context.Context, _ string, e model.AdminAuditLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.completed = append(l.completed, e)
	return nil
}
func (*auditBodyLedger) ListAdminAudits(context.Context, int64) ([]model.AdminAuditLog, error) {
	return nil, nil
}

// Explicit localhost-only database. Real authentication, audit middleware,
// transport and route parser run; no customer save or model/TTS is performed.
func TestAuditBodyAuthenticatedHTTPMySQL(t *testing.T) {
	if os.Getenv("AUDIT_BODY_DB_TEST") != "1" {
		t.Skip("explicit isolated localhost MySQL required")
	}
	cfg := config.Load()
	if cfg.DBHost != "127.0.0.1" {
		t.Fatal("localhost required")
	}
	cfg.DBPassword = ""
	dsn := mysql.NewConfig()
	dsn.User = cfg.DBUser
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	dsn.ParseTime = true
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("audit_body_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE `" + schema + "`"); err != nil {
			t.Error(err)
		}
	}()
	dsn.DBName = schema
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE mgmt_users(id BIGINT PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,status VARCHAR(32))`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,avatar_url,role) VALUES(8,14,'fixture','','customer')`)
	token := "local-audit-body-fixture"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at) VALUES(8,?,DATE_ADD(NOW(),INTERVAL 1 DAY))`, hash)
	cfg.DBName = schema
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ledger := &auditBodyLedger{}
	audit := auditlog.New("127.0.0.1:1", "", 0, 10, ledger)
	defer audit.Close()
	s := &Server{store: store, auth: auth.NewResolver("test", store), audit: audit}
	raw, err := os.ReadFile(os.Getenv("RECORDING_SAVE_TEST_PAYLOAD"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 64<<10 || len(raw) >= 1<<20 {
		t.Fatal("fixture must cover actual long-recording request")
	}
	var expected model.CreateLiveAgentPlanVersionInput
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	call := func(handler http.Handler, chunked, authenticated bool, want int) {
		t.Helper()
		server := httptest.NewServer(handler)
		defer server.Close()
		var reader io.Reader = bytes.NewReader(raw)
		if chunked {
			reader = struct{ io.Reader }{reader}
		}
		r, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/live-agent-plans/7/versions", reader)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "text/event-stream")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "lc_session", Value: token})
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		response, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("request status=%d want=%d response=%s", resp.StatusCode, want, response)
		}
	}
	// The complete router reaches the deliberately missing plan lookup (404),
	// after decoding the real payload. Old audit code fails earlier with 400.
	for _, chunked := range []bool{false, true} {
		call(s.Handler(), chunked, true, http.StatusNotFound)
	}
	call(s.Handler(), false, false, http.StatusUnauthorized)
	// Continue through the exact endpoint validators with the real middleware.
	decoder := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input model.CreateLiveAgentPlanVersionInput
		if err := readJSON(w, r, &input); err != nil {
			http.Error(w, "decode", 400)
			return
		}
		if len(input.Variants) != len(expected.Variants) || input.Variants[0].Text != expected.Variants[0].Text || input.Variants[0].SRT != expected.Variants[0].SRT || len(input.Variants[0].Timeline) != len(expected.Variants[0].Timeline) || len(input.Variants[0].SafePoints) != len(expected.Variants[0].SafePoints) {
			http.Error(w, "recording changed", 400)
			return
		}
		if err := normalizeRecordingScript(input.GenerationContext); err != nil {
			http.Error(w, "script", 400)
			return
		}
		if err := validateLiveAgentPlanVersionInput(&input); err != nil {
			http.Error(w, "validate", 400)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, chunked := range []bool{false, true} {
		call(s.adminAuditMiddleware(decoder), chunked, true, http.StatusNoContent)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.begun) != 4 || len(ledger.completed) != 4 {
		t.Fatalf("audit missing: begin=%d complete=%d", len(ledger.begun), len(ledger.completed))
	}
	for _, entry := range ledger.completed {
		if entry.ActorUserID != 8 || entry.ObjectType != "agent_plan" || entry.ObjectID != "7" || entry.DetailJSON != "" || !strings.HasPrefix(entry.Result, "http_") {
			t.Fatal("audit identity/result or bounded metadata changed")
		}
	}
}
