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
func TestTTSRoomPoolsAuthenticatedMySQL(t *testing.T) {
	if os.Getenv("TTS_ROOM_POOLS_DB_TEST") != "1" {
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
	schema := fmt.Sprintf("tts_room_http_test_%d", time.Now().UnixNano())
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
	s.registerTTSRoomPoolRoutes(mux)
	mux.HandleFunc("GET /api/v1/live/official-voices", s.liveOfficialVoices)
	mux.HandleFunc("POST /api/v1/live/voice-profiles", func(w http.ResponseWriter, r *http.Request) { s.liveSaveVoiceProfile(w, r, 0) })
	mux.HandleFunc("GET /api/v1/live/voice-profiles", s.liveListVoiceProfiles)
	server := httptest.NewServer(mux)
	defer server.Close()
	call := func(user, room int, method, path string, input any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(input)
		r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
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
			t.Fatalf("%s %s room=%d status=%d want=%d: %s", method, path, room, resp.StatusCode, want, body)
		}
		return body
	}

	exec(`CREATE TABLE core_rooms(id BIGINT PRIMARY KEY,tenant_id BIGINT)`)
	exec(`INSERT INTO core_rooms VALUES(29,14),(30,15)`)
	path := "/api/v1/system/tts-room-pools?tenant_id=14&room_id=29"
	call(2, 0, "GET", path, nil, 403)
	call(1, 0, "GET", "/api/v1/system/tts-room-pools?tenant_id=15&room_id=29", nil, 404)
	var policy appdb.TTSRoomPoolPolicy
	if err = json.Unmarshal(call(1, 0, "GET", path, nil, 200), &policy); err != nil || policy.Revision != 0 || len(policy.AllowedPools) != 2 {
		t.Fatal("default room policy", err, policy)
	}
	policy.AllowedPools = []string{ttsscheduler.PoolHighFidelity}
	json.Unmarshal(call(1, 0, "PUT", "/api/v1/system/tts-room-pools", policy, 200), &policy)
	if policy.Revision != 1 || len(policy.AllowedPools) != 1 || policy.AllowedPools[0] != "high_fidelity" {
		t.Fatal("bad save receipt", policy)
	}
	stale := policy
	stale.Revision = 0
	call(1, 0, "PUT", "/api/v1/system/tts-room-pools", stale, 409)
	bad := policy
	bad.AllowedPools = nil
	call(1, 0, "PUT", "/api/v1/system/tts-room-pools", bad, 400)
	call(1, 0, "PUT", "/api/v1/system/tts-room-pools", map[string]any{"tenant_id": 0, "room_id": 0}, 400)
	input := model.VoiceProfileInput{Name: "高仿真主播", Provider: "volcengine", VoiceID: "S_existing", CloneStatus: "ready", Config: map[string]any{"target_model": ttsgateway.VolcengineICL2}}
	var profile model.VoiceProfile
	if err = json.Unmarshal(call(1, 29, "POST", "/api/v1/live/voice-profiles", input, 201), &profile); err != nil {
		t.Fatal(err)
	}
	identity := model.LiveAgentVoiceIdentity{TTSPool: "high_fidelity", Name: profile.Name, Source: "clone", Provider: "volcengine", Model: ttsgateway.VolcengineICL2, VoiceID: profile.VoiceID, ProfileID: profile.ID, Rate: 1.13}
	if err = s.validatePlanVoiceIdentity(t.Context(), 14, &identity); err != nil {
		t.Fatal("original imported voice cannot save", err)
	}
	wrong := identity
	wrong.TTSPool = "standard"
	if s.validatePlanVoiceIdentity(t.Context(), 14, &wrong) == nil {
		t.Fatal("pool mismatch allowed")
	}
	wrong = identity
	wrong.VoiceID = "S_another"
	if s.validatePlanVoiceIdentity(t.Context(), 14, &wrong) == nil {
		t.Fatal("forged voice allowed")
	}
	wrong = identity
	wrong.ProfileID = profile.ID + 5000
	if s.validatePlanVoiceIdentity(t.Context(), 14, &wrong) == nil {
		t.Fatal("forged profile allowed")
	}
	if s.validatePlanVoiceIdentity(t.Context(), 15, &identity) == nil {
		t.Fatal("cross tenant voice allowed")
	}
	exec(`INSERT INTO live_agent_plans(tenant_id,name,description,status,created_by_user_id) VALUES(14,'语音池回显','','active',2)`)
	var plan int64
	if err = db.QueryRow(`SELECT id FROM live_agent_plans WHERE tenant_id=14 LIMIT 1`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	saved, e := store.CreateLiveAgentPlanVersion(t.Context(), 14, plan, 2, model.CreateLiveAgentPlanVersionInput{VoiceIdentity: identity, DurationMinutes: 30, RoundMinutes: 10, GenerationContext: map[string]any{"content_mode": "user_audio"}})
	if e != nil {
		t.Fatal("save imported voice snapshot", e)
	}
	restored, e := store.GetLiveAgentPlanVersion(t.Context(), 14, plan, saved.ID)
	if e != nil || restored.VoiceIdentity.TTSPool != "high_fidelity" || restored.VoiceIdentity.ProfileID != profile.ID || restored.VoiceIdentity.VoiceID != "S_existing" || restored.VoiceIdentity.BindingID != 0 {
		t.Fatal("tier or original identity lost", e, restored.VoiceIdentity)
	}
	legacyAli := model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{Provider: "aliyun_qwen", Model: "qwen-audio-3.1-tts-flash"}}
	if err = s.validateLiveContentVersion(t.Context(), 14, 29, legacyAli); err != ttsscheduler.ErrPoolDenied {
		t.Fatal("legacy snapshot bypassed room policy", err)
	}
	fireCatalog := call(2, 0, "GET", "/api/v1/live/official-voices?pool=high_fidelity", nil, 200)
	if strings.Contains(string(fireCatalog), "S_existing") {
		t.Fatal("private voice leaked to official catalog")
	}
	call(2, 0, "GET", "/api/v1/live/official-voices?pool=bad", nil, 400)
}
