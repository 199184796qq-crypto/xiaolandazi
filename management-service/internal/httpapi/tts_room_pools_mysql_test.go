package httpapi

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
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
	exec(`CREATE TABLE mgmt_system_settings(setting_key VARCHAR(96) PRIMARY KEY,setting_group VARCHAR(64) NOT NULL DEFAULT 'general',label VARCHAR(160) DEFAULT '',value_text TEXT NOT NULL,input_type VARCHAR(32) DEFAULT 'text',sort_order INT DEFAULT 0,updated_by_user_id BIGINT)`)
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
	mux.HandleFunc("POST /api/v1/live/official-voices/{voiceID}/preview", s.liveOfficialVoicePreview)
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
	var catalog struct {
		Items      []struct{ ID, Name, Provider, Model, Gender string }
		Configured bool
	}
	if err := json.Unmarshal(fireCatalog, &catalog); err != nil || !catalog.Configured || len(catalog.Items) != 1 || catalog.Items[0].ID != "S_existing" {
		t.Fatal("configured pool mapping is not directly selectable", err, string(fireCatalog))
	}
	for _, private := range []string{"local-fixture-key", "fire1", "sealed_key", "account_uid"} {
		if strings.Contains(string(fireCatalog), private) {
			t.Fatal("catalog leaked account details", private)
		}
	}
	call(0, 0, "GET", "/api/v1/live/official-voices?pool=high_fidelity", nil, 401)
	// The shared pool does not allocate a voice profile or consume private slots.
	var profilesBefore, profilesAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM voice_profiles`).Scan(&profilesBefore); err != nil {
		t.Fatal(err)
	}
	// Display updates are admin-only and independently versioned; no paid provider
	// endpoint is called. Default metadata does not guess gender or use the ID.
	displayPath := "/api/v1/system/tts-voice-display"
	call(0, 0, "GET", displayPath, nil, 401)
	call(2, 0, "GET", displayPath, nil, 403)
	display := appdb.TTSVoiceDisplay{VoiceID: "S_existing", Name: "晚晴", Gender: "女声"}
	call(2, 0, "PUT", displayPath, display, 403)
	var initial struct {
		Items []appdb.TTSVoiceDisplay `json:"items"`
	}
	if err := json.Unmarshal(call(1, 0, "GET", displayPath, nil, 200), &initial); err != nil || len(initial.Items) != 1 || initial.Items[0].Name != "" || initial.Items[0].Gender != "" || initial.Items[0].Revision != 0 {
		t.Fatal("display default must be unknown", err, initial)
	}
	if err := json.Unmarshal(call(1, 0, "PUT", displayPath, display, 200), &display); err != nil || display.Revision != 1 {
		t.Fatal("display save failed", err, display)
	}
	staleDisplay := display
	staleDisplay.Revision = 0
	call(1, 0, "PUT", displayPath, staleDisplay, 409)
	invalidDisplay := display
	invalidDisplay.Gender = "女" // explicit supported values only
	call(1, 0, "PUT", displayPath, invalidDisplay, 400)
	invalidDisplay = display
	invalidDisplay.VoiceID = "S_unavailable"
	call(1, 0, "PUT", displayPath, invalidDisplay, 400)
	invalidDisplay = display
	invalidDisplay.Name = strings.Repeat("声", 81)
	call(1, 0, "PUT", displayPath, invalidDisplay, 400)
	invalidDisplay.Name = " \n "
	call(1, 0, "PUT", displayPath, invalidDisplay, 400)
	if err := json.Unmarshal(call(2, 0, "GET", "/api/v1/live/official-voices?pool=high_fidelity", nil, 200), &catalog); err != nil || catalog.Items[0].Name != "晚晴" || catalog.Items[0].Gender != "女声" {
		t.Fatal("customer catalogue lost admin display metadata", err, catalog)
	}
	if err := json.Unmarshal(call(1, 0, "GET", displayPath, nil, 200), &initial); err != nil || initial.Items[0] != display {
		t.Fatal("display metadata did not persist", err, initial)
	}
	var displayKey string
	if err := db.QueryRow(`SELECT setting_key FROM mgmt_system_settings WHERE setting_group='tts_voice_display' LIMIT 1`).Scan(&displayKey); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSystemSettings(t.Context(), []model.SystemSettingUpdate{{Key: displayKey, Value: "{}"}}, 1); err == nil {
		t.Fatal("generic settings update bypassed metadata validation/CAS")
	}
	// Concurrent first saves must reject one writer, including the missing-row case.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"甲", "乙"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, err := store.SaveTTSVoiceDisplay(t.Context(), appdb.TTSVoiceDisplay{VoiceID: "S_concurrent", Name: name, Gender: "男声"}, 1)
			results <- err
		}(name)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ttsscheduler.ErrRevision) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("first save lost CAS protection", success, conflict)
	}
	pooled := model.LiveAgentVoiceIdentity{Name: "高仿真音色 existing", Version: "V1", TTSPool: "high_fidelity", Source: "official", Provider: "volcengine", Model: ttsgateway.VolcengineICL2, VoiceID: "S_existing", Rate: 1.13}
	if err := s.validatePlanVoiceIdentity(t.Context(), 14, &pooled); err != nil {
		t.Fatal("pool identity needs no private slot", err)
	}
	if pooled.Name != "晚晴" || pooled.VoiceID != "S_existing" || pooled.Rate != 1.13 {
		t.Fatal("new save must refresh name without replacing voice/rate", pooled)
	}
	if voice, modelName, err := s.resolveFormalVoice(t.Context(), 14, fullShowVariantVoiceInput{Source: "official", VoiceID: "S_existing", Model: ttsgateway.VolcengineICL2}); err != nil || voice != "S_existing" || modelName != ttsgateway.VolcengineICL2 {
		t.Fatal("live mainline cannot resolve pool voice", err)
	}
	request := httptest.NewRequest("POST", "/", nil)
	request.Header.Set("X-Live-Support-Room-ID", "29")
	if !s.requireLiveSupportSelectedVoice(httptest.NewRecorder(), request, model.Actor{UserID: 1, Role: "platform_admin"}, 14, map[string]any{"voice_id": "S_existing"}) {
		t.Fatal("authorized staff cannot select pool catalogue voice")
	}
	poolSaved, err := store.CreateLiveAgentPlanVersion(t.Context(), 14, plan, 2, model.CreateLiveAgentPlanVersionInput{VoiceIdentity: pooled, DurationMinutes: 30, RoundMinutes: 10, GenerationContext: map[string]any{"content_mode": "user_audio"}})
	if err != nil {
		t.Fatal("save pool identity", err)
	}
	poolRestored, err := store.GetLiveAgentPlanVersion(t.Context(), 14, plan, poolSaved.ID)
	if err != nil || poolRestored.VoiceIdentity.Source != "official" || poolRestored.VoiceIdentity.VoiceID != "S_existing" || poolRestored.VoiceIdentity.TTSPool != "high_fidelity" || poolRestored.VoiceIdentity.ProfileID != 0 || poolRestored.VoiceIdentity.BindingID != 0 {
		t.Fatal("pool identity lost during restore", err, poolRestored.VoiceIdentity)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM voice_profiles`).Scan(&profilesAfter); err != nil || profilesAfter != profilesBefore {
		t.Fatal("pool operation allocated private slots", err)
	}
	for _, alter := range []func(*model.LiveAgentVoiceIdentity){func(v *model.LiveAgentVoiceIdentity) { v.VoiceID = "S_unknown" }, func(v *model.LiveAgentVoiceIdentity) { v.TTSPool = "standard" }, func(v *model.LiveAgentVoiceIdentity) { v.Provider = "aliyun_qwen" }, func(v *model.LiveAgentVoiceIdentity) { v.ProfileID = profile.ID }, func(v *model.LiveAgentVoiceIdentity) { v.BindingID = 1 }} {
		invalid := pooled
		alter(&invalid)
		if s.validatePlanVoiceIdentity(t.Context(), 14, &invalid) == nil {
			t.Fatal("forged pool identity accepted", invalid)
		}
	}
	call(2, 0, "POST", "/api/v1/live/official-voices/S_unknown/preview", map[string]any{"text": "no paid call"}, 404)
	exec(`UPDATE mgmt_tts_pool_accounts SET enabled=FALSE WHERE provider='volcengine'`)
	call(1, 0, "PUT", displayPath, display, 400)
	disabledCatalog := call(2, 0, "GET", "/api/v1/live/official-voices?pool=high_fidelity", nil, 200)
	if err := json.Unmarshal(disabledCatalog, &catalog); err != nil || catalog.Configured || len(catalog.Items) != 0 {
		t.Fatal("disabled account catalogue remained visible", err, string(disabledCatalog))
	}
	if s.validatePlanVoiceIdentity(t.Context(), 14, &pooled) == nil {
		t.Fatal("disabled account voice can still save")
	}
	if _, _, err := s.resolveFormalVoice(t.Context(), 14, fullShowVariantVoiceInput{Source: "official", VoiceID: "S_existing", Model: ttsgateway.VolcengineICL2}); err == nil {
		t.Fatal("disabled account still resolves for synthesis")
	}
	if s.requireLiveSupportSelectedVoice(httptest.NewRecorder(), request, model.Actor{UserID: 1, Role: "platform_admin"}, 14, map[string]any{"voice_id": "S_existing"}) {
		t.Fatal("disabled pool bypassed staff selection gate")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM voice_profiles`).Scan(&profilesAfter); err != nil || profilesAfter != profilesBefore {
		t.Fatal("private voices changed", err)
	}
	if err := s.validatePlanVoiceIdentity(t.Context(), 14, &identity); err != nil {
		t.Fatal("legacy imported profile compatibility changed", err)
	}
	call(2, 0, "GET", "/api/v1/live/official-voices?pool=bad", nil, 400)
}
