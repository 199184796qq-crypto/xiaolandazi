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
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	auditlog "livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// Actual authenticated save, derived safety, version transaction and workspace
// read-back in an isolated database. No live room, external provider or customer
// data is used. The long original recording remains independent of its old clone.
func TestPlanVoiceFullSaveMySQL(t *testing.T) {
	if os.Getenv("PLAN_VOICE_DB_TEST") != "1" {
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
	schema := fmt.Sprintf("plan_voice_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE `" + schema + "`"); err != nil {
			t.Error(err)
		}
	}()
	cfg.DBName = schema
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
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
	exec(`CREATE TABLE mgmt_users(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`CREATE TABLE mgmt_system_settings(setting_key VARCHAR(96) PRIMARY KEY,value_text TEXT)`)
	for _, migrate := range []func(context.Context) error{store.Migrate, store.MigrateLiveRuntime, store.MigrateContentPools, store.MigrateContentCorrections} {
		if err := migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,password_hash,display_name,avatar_url,role,status) VALUES(800014,14,'voice-fixture','','测试','','customer','active')`)
	token := "local-plan-voice-fixture"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at,last_seen_at) VALUES(800014,?,DATE_ADD(NOW(),INTERVAL 1 DAY),NOW())`, hash)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description,voice_rate) VALUES(7,14,'测试方案','',1.2)`)
	exec(`INSERT INTO live_agent_plan_asset_settings(tenant_id,plan_id,content_mode,reading_rules) VALUES(14,7,'user_audio','')`)
	exec(`INSERT INTO mgmt_system_settings(setting_key,value_text) VALUES('voice_model_config','{"custom_model":"qwen-audio-3.1-tts-flash","official_library":"tencent-premium","revision":2}') ON DUPLICATE KEY UPDATE value_text=VALUES(value_text)`)
	raw, err := os.ReadFile(os.Getenv("RECORDING_SAVE_TEST_PAYLOAD"))
	if err != nil {
		t.Fatal(err)
	}
	var input model.CreateLiveAgentPlanVersionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Variants) != 1 || len(raw) <= 64<<10 {
		t.Fatal("long original recording fixture required")
	}
	v := input.Variants[0]
	exec(`INSERT INTO media_assets(id,tenant_id,asset_type,original_name,storage_driver,object_key,mime_type,size_bytes,duration_ms,checksum_sha256,status,metadata_json) VALUES(?,14,'audio','fixture.wav','local','fixture/original.wav','audio/wav',100,?,'fixture','active','{"purpose":"live_agent_custom_mainline_audio"}')`, v.AudioAssetID, v.AudioDurationMS)
	exec(`INSERT INTO voice_model_bindings(id,tenant_id,profile_id,sample_asset_id,provider,model,voice_id,status,config_json) VALUES(21,14,13,1,'qwen_audio','qwen-audio-3.1-tts-flash','fixture-clone-21','ready','{}'),(20,14,12,1,'qwen_audio','qwen-audio-3.1-tts-flash','fixture-clone-20','disabled','{}')`)
	t.Setenv("TENCENT_TTS_APP_ID", "1")
	t.Setenv("TENCENT_TTS_SECRET_ID", "fixture")
	t.Setenv("TENCENT_TTS_SECRET_KEY", "fixture")
	t.Setenv("DASHSCOPE_API_KEY", "fixture")
	ledger := &auditBodyLedger{}
	audit := auditlog.New("127.0.0.1:1", "", 0, 10, ledger)
	defer audit.Close()
	s := &Server{store: store, auth: auth.NewResolver("test", store), audit: audit}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	call := func(method, path string, body []byte, want int) []byte {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: "lc_session", Value: token})
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s status=%d want=%d error=%s", path, resp.StatusCode, want, out)
		}
		return out
	}
	official := model.LiveAgentVoiceIdentity{Name: "智柯", Version: "V1", Source: "official", Provider: "tencent_tts", VoiceID: "101030", Model: "tencent-premium", Rate: 1.2}
	input.VoiceIdentity = official
	encode := func() []byte {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var saved model.LiveAgentPlanVersion
	if err := json.Unmarshal(call("POST", "/api/v1/live-agent-plans/7/versions", encode(), 201), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ID <= 0 || saved.VersionNo != 1 || saved.RoomID != 0 || saved.VoiceIdentity.VoiceID != "101030" || saved.VoiceIdentity.Rate != 1.2 || saved.Variants[0].AudioAssetID != v.AudioAssetID || saved.Variants[0].Text != v.Text || saved.Variants[0].SRT != v.SRT || len(saved.Variants[0].Timeline) != 251 {
		t.Fatal("full saved recording/voice receipt changed")
	}
	var workspace struct {
		Version *model.LiveAgentPlanVersion `json:"version"`
	}
	if err := json.Unmarshal(call("GET", "/api/v1/live-agent-plans/7/workspace?tenant_id=14&content_mode=user_audio", nil, 200), &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.Version == nil || workspace.Version.ID != saved.ID || workspace.Version.VoiceIdentity.VoiceID != "101030" {
		t.Fatal("saved default voice not restored")
	}
	// The original upload may reference a disabled old clone in its archive;
	// explicitly selecting that clone for interaction must still be refused.
	input.VoiceIdentity = model.LiveAgentVoiceIdentity{Name: "失效声音", Version: "V1", Source: "clone", Provider: "qwen_audio", VoiceID: "fixture-clone-20", Model: "qwen-audio-3.1-tts-flash", BindingID: 20, ProfileID: 12, Rate: 1.2}
	call("POST", "/api/v1/live-agent-plans/7/versions", encode(), 409)
	input.VoiceIdentity = model.LiveAgentVoiceIdentity{Name: "新互动声音", Version: "V1", Source: "clone", Provider: "qwen_audio", VoiceID: "fixture-clone-21", Model: "qwen-audio-3.1-tts-flash", BindingID: 21, ProfileID: 13, Rate: 1.2}
	if err := json.Unmarshal(call("POST", "/api/v1/live-agent-plans/7/versions", encode(), 201), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.VersionNo != 2 || saved.VoiceIdentity.BindingID != 21 || saved.Variants[0].AudioAssetID != v.AudioAssetID {
		t.Fatal("explicit plan voice did not persist independently of original audio")
	}
	// Aliyun's legacy catalogue omits provider, while callers use its canonical
	// provider name; exact model and configured library are still mandatory.
	exec(`UPDATE mgmt_system_settings SET value_text='{"custom_model":"qwen-audio-3.1-tts-flash","official_library":"qwen_audio","revision":3}' WHERE setting_key='voice_model_config'`)
	qwen := model.LiveAgentVoiceIdentity{Name: "龙安灵心", Version: "V1", Source: "official", Provider: "aliyun_qwen", VoiceID: "longanlingxin", Model: "qwen-audio-3.0-tts-plus", Rate: 1.2}
	if err := s.validatePlanVoiceIdentity(context.Background(), 14, &qwen); err != nil {
		t.Fatal(err)
	}
	qwen.Model = "wrong-model"
	if err := s.validatePlanVoiceIdentity(context.Background(), 14, &qwen); err == nil {
		t.Fatal("wrong official model accepted")
	}
	if err := s.validatePlanVoiceIdentity(context.Background(), 14, &official); err == nil {
		t.Fatal("foreign official library accepted")
	}
	var rooms, bindings int
	_ = db.QueryRow(`SELECT COUNT(*) FROM live_agent_plan_room_bindings WHERE tenant_id=14 AND plan_id=7`).Scan(&rooms)
	_ = db.QueryRow(`SELECT COUNT(*) FROM voice_model_bindings WHERE id=20 AND status='disabled'`).Scan(&bindings)
	if rooms != 0 || bindings != 1 {
		t.Fatal("save changed room binding or re-enabled a disabled voice")
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.begun) != 3 || len(ledger.completed) != 3 {
		t.Fatal("save audits missing")
	}
	if !strings.HasPrefix(ledger.completed[0].Result, "http_201") {
		t.Fatal("save audit not successful")
	}
}
