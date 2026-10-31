package httpapi

import (
	"bytes"
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
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
)

// Real handler/auth/storage path in a disposable localhost database. Core is
// read-only and mocked; import must never invoke any paid synthesis or training.
func TestVolcengineVoiceImportAuthenticatedMySQL(t *testing.T) {
	if os.Getenv("VOLCENGINE_VOICE_DB_TEST") != "1" {
		t.Skip("explicit isolated localhost MySQL required")
	}
	cfg := config.Load()
	if cfg.DBHost != "127.0.0.1" {
		t.Fatal("localhost required")
	}
	cfg.DBPassword = ""
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr, dsn.ParseTime = cfg.DBUser, "tcp", net.JoinHostPort(cfg.DBHost, cfg.DBPort), true
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("volc_voice_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE `" + schema + "`"); err != nil {
			t.Error(err)
		}
	}()
	cfg.DBName, dsn.DBName = schema, schema
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
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
	exec(`CREATE TABLE mgmt_users(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`CREATE TABLE mgmt_system_settings(setting_key VARCHAR(96) PRIMARY KEY,value_text TEXT)`)
	exec(`CREATE TABLE mgmt_tenants(id BIGINT PRIMARY KEY,status VARCHAR(32))`)
	exec(`INSERT INTO mgmt_tenants VALUES(14,'active')`)
	commercialDDL, err := os.ReadFile("../db/commercial_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(strings.ReplaceAll(string(commercialDDL), "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS quota_buckets (") {
			exec(statement)
		}
	}
	exec(`CREATE TABLE biz_memberships(id BIGINT,tenant_id BIGINT,plan_id BIGINT,status VARCHAR(32),cycle_start_at DATETIME,cycle_end_at DATETIME)`)
	exec(`CREATE TABLE catalog_membership_plans(id BIGINT,name VARCHAR(80))`)
	exec(`CREATE TABLE mgmt_membership_room_limits(plan_id BIGINT,room_limit BIGINT)`)
	exec(`CREATE TABLE org_resource_accounts(organization_id BIGINT,resource_type VARCHAR(40),status VARCHAR(32),balance BIGINT)`)
	for _, migrate := range []func() error{
		func() error { return store.MigrateLiveRuntime(t.Context()) },
		func() error { return store.MigrateRoomDeletions(t.Context()) },
		func() error { return store.MigrateTTSPool(t.Context(), "") },
	} {
		if err := migrate(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.GetEffectiveCustomerRoomLimit(t.Context(), 14); err != nil {
		t.Fatalf("isolated quota fixture incomplete: %v", err)
	}
	t.Setenv("MODEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("fixture-key-", 4))
	if err := store.SaveTTSAccount(t.Context(), ttsscheduler.AccountInput{Provider: "volcengine", Name: "本地火山", AccountUID: "fire1", NewAPIKey: "local-fixture-key", Enabled: true, RPM: 30, RPS: 1, MaxConcurrent: 1, Models: []string{ttsgateway.VolcengineICL2}, Voices: map[string]string{ttsgateway.VolcengineICL2 + "/S_existing": "S_existing"}}, 1); err != nil {
		t.Fatal(err)
	}
	for id, role := range map[int]string{1: "platform_admin", 2: "customer"} {
		exec(`INSERT INTO mgmt_users(id,tenant_id,username,display_name,avatar_url,role) VALUES(?,14,?,'测试','',?)`, id, fmt.Sprintf("user%d", id), role)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("local-token-%d", id))))
		exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at,last_seen_at) VALUES(?,?,DATE_ADD(NOW(),INTERVAL 1 DAY),NOW())`, id, hash)
	}
	exec(`INSERT INTO live_support_authorizations(tenant_id,room_id,staff_user_id,capability,granted_by_user_id) VALUES(14,29,1,'l3_policy',2)`)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/internal/v1/rooms/29" {
			t.Error("unexpected Core mutation/request")
			w.WriteHeader(500)
			return
		}
		writeJSON(w, 200, map[string]any{"id": 29, "tenant_id": 14})
	}))
	defer core.Close()
	s := &Server{store: store, auth: auth.NewResolver("test", store), core: coreclient.New(core.URL, "fixture")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/live/voice-profiles", func(w http.ResponseWriter, r *http.Request) { s.liveSaveVoiceProfile(w, r, 0) })
	mux.HandleFunc("GET /api/v1/live/voice-profiles", s.liveListVoiceProfiles)
	server := httptest.NewServer(mux)
	defer server.Close()
	call := func(user, room int, method, query string, input any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(input)
		r, _ := http.NewRequest(method, server.URL+"/api/v1/live/voice-profiles"+query, bytes.NewReader(raw))
		r.AddCookie(&http.Cookie{Name: "lc_session", Value: fmt.Sprintf("local-token-%d", user)})
		if room > 0 {
			r.Header.Set("X-Live-Support-Room-ID", fmt.Sprint(room))
		}
		r.Header.Set("Content-Type", "application/json")
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s room=%d status=%d want=%d: %s", method, query, room, resp.StatusCode, want, body)
		}
		return body
	}
	input := model.VoiceProfileInput{Name: "火山主播", Provider: "volcengine", VoiceID: "S_existing", CloneStatus: "ready", Config: map[string]any{"target_model": ttsgateway.VolcengineICL2, "support_room_id": 888, "api_key": "must-not-be-stored"}}
	call(1, 0, "POST", "?tenant_id=14", input, 403) // Admin is not customer consent.
	call(1, 30, "POST", "", input, 403)             // No grant for another room.
	call(2, 0, "POST", "", input, 403)              // Customers cannot claim global pool voices.
	call(1, 29, "POST", "?tenant_id=15", input, 403)
	bad := input
	bad.VoiceID = "S_unmapped"
	call(1, 29, "POST", "", bad, 409)
	var profile model.VoiceProfile
	if err := json.Unmarshal(call(1, 29, "POST", "", input, 201), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID <= 0 || profile.TenantID != 14 || supportVoiceProfileRoomID(profile) != 29 || profile.Config["api_key"] != nil || profile.SampleAssetID != nil {
		t.Fatal("voice import lost verified scope or retained untrusted data", profile)
	}
	for _, scope := range []struct{ user, room int }{{1, 29}, {2, 0}} {
		var profiles []model.VoiceProfile
		if err := json.Unmarshal(call(scope.user, scope.room, "GET", "", nil, 200), &profiles); err != nil || len(profiles) != 1 || profiles[0].ID != profile.ID {
			t.Fatal("imported voice not visible in real read-back", err, profiles)
		}
	}
	call(2, 0, "GET", "?tenant_id=15", nil, 403)
	if v, m, err := resolveSelectedCloneVoice(profile, fullShowVariantVoiceInput{Source: "clone", ProfileID: profile.ID, VoiceID: "S_existing", Model: ttsgateway.VolcengineICL2}, nil); err != nil || v != "S_existing" || m != ttsgateway.VolcengineICL2 {
		t.Fatal("original imported voice not selectable", err)
	}
	exec(`UPDATE live_support_authorizations SET status='revoked',revoked_at=NOW() WHERE room_id=29`)
	call(1, 29, "GET", "", nil, 403)
}
