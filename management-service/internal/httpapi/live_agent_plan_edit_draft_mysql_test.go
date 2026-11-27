package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	auditlog "livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestPromptRequirementsEditingDraftHTTPMySQL(t *testing.T) {
	if os.Getenv("PLAN_EDIT_DRAFT_DB_TEST") != "1" {
		t.Skip("explicit isolated localhost MySQL required")
	}
	cfg := config.Load()
	if cfg.DBHost != "127.0.0.1" {
		t.Fatal("local database required")
	}
	cfg.DBPassword = ""
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr = cfg.DBUser, "tcp", net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg.DBName = fmt.Sprintf("plan_edit_draft_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + cfg.DBName + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE " + cfg.DBName); err != nil {
			t.Error(err)
		}
	}()
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dsn.DBName = cfg.DBName
	database, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := os.ReadFile("../db/live_runtime_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{
		"live_agent_plans": true, "live_agent_plan_edit_drafts": true,
		"live_agent_plan_versions": true, "live_agent_room_plan_selections": true, "live_runtime_sessions": true,
	}
	for _, statement := range strings.Split(strings.ReplaceAll(string(schema), "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(statement)
		if len(fields) > 5 && required[fields[5]] {
			exec(statement)
		}
	}
	managementSchema, err := os.ReadFile("../db/db.go")
	if err != nil {
		t.Fatal(err)
	}
	promptTable := regexp.MustCompile("(?s)`(CREATE TABLE IF NOT EXISTS live_agent_plan_prompts .*?)`").FindSubmatch(managementSchema)
	if len(promptTable) != 2 {
		t.Fatal("missing plan prompt DDL")
	}
	exec(string(promptTable[1]))
	exec(`CREATE TABLE mgmt_users(id BIGINT PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`CREATE TABLE core_rooms (id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL)`)
	exec(`CREATE TABLE mgmt_room_deletions (room_id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL)`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,display_name,avatar_url,role) VALUES (8,14,'fixture','测试','','customer')`)
	token := "local-plan-edit-draft-fixture"
	exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at,last_seen_at) VALUES (8,?,DATE_ADD(NOW(),INTERVAL 1 DAY),NOW())`, fmt.Sprintf("%x", sha256.Sum256([]byte(token))))
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES (9,14,'测试方案','')`)
	exec(`INSERT INTO live_agent_plan_prompts(tenant_id,plan_id,prompt_md,sections_json)
		VALUES (14,9,'',JSON_OBJECT('facts_md','原有事实','interaction_requirements_md','原有互动规则'))`)

	audit := auditlog.New("127.0.0.1:1", "", 0, 10, factCategoryAuditFixture{})
	defer audit.Close()
	server := (&Server{store: store, auth: auth.NewResolver("test", store), audit: audit}).Handler()
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/live-agent-plans/9/"+path+"?tenant_id=14", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "lc_session", Value: token})
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s editing draft: status %d, want %d, response %s", method, w.Code, status, w.Body)
		}
		return w
	}
	request(http.MethodPut, "editing-draft", `{"revision":"","draft":{"open_issues":[],"prompt":{"invalid_field":"value"}}}`, http.StatusBadRequest)
	speech := "用内容自然过渡，保留限制条件。\n第二段保留“引号”和  两个空格。\n"
	speech += strings.Repeat("补", 303-utf8.RuneCountInString(speech))
	// Use the literal browser field name so a backend tag regression fails here.
	encodedSpeech, err := json.Marshal(speech)
	if err != nil {
		t.Fatal(err)
	}
	interaction := "  优先回应实际问题。\n相关条件需要说清楚。  "
	encodedInteraction, err := json.Marshal(interaction)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"revision":"","draft":{"room_intro":"直播简介","open_issues":[],"prompt":{"persona_md":"主播人设","red_lines_md":"红线规则","speech_requirements_text":` + string(encodedSpeech) + `,"interaction_guidance_md":` + string(encodedInteraction) + `}}}`
	request(http.MethodPut, "editing-draft", body, http.StatusOK)
	var saved struct {
		Revision string `json:"revision"`
		Draft    struct {
			Prompt map[string]string `json:"prompt"`
		} `json:"draft"`
	}
	if err := json.Unmarshal(request(http.MethodGet, "editing-draft", "", http.StatusOK).Body.Bytes(), &saved); err != nil || saved.Revision == "" || saved.Draft.Prompt["speech_requirements_text"] != speech || saved.Draft.Prompt["interaction_guidance_md"] != interaction {
		t.Fatalf("draft reload did not return the original prompt requirements: %v", err)
	}
	if _, err := store.CreateLiveAgentPlanVersion(t.Context(), 14, 9, 8, model.CreateLiveAgentPlanVersionInput{
		DurationMinutes: 20, RoundMinutes: 4, EditDraftRevision: saved.Revision,
		GenerationContext: map[string]any{"content_mode": model.LiveContentUserAudio},
	}); err != nil {
		t.Fatal(err)
	}
	var doc appdb.LiveAgentPlanPromptDocument
	err = json.Unmarshal(request(http.MethodGet, "prompt", "", http.StatusOK).Body.Bytes(), &doc)
	if err != nil || doc.Sections.SpeechRequirementsMD != speech || doc.Sections.FactsMD != "原有事实" || doc.Sections.InteractionRequirementsMD != "原有互动规则" || doc.InteractionGuidanceMD != interaction {
		t.Fatalf("formal prompt did not preserve the original sections: %v", err)
	}
	prompt := renderChainRewritePrompt(doc.Sections.PersonaMD, "结构", doc.Sections.SpeechRequirementsMD, doc.Sections.RedLinesMD, "[]")
	if !strings.Contains(prompt, "\n【话术要求】\n"+speech+"\n\n【红线禁止】\n红线规则") {
		t.Fatal("rewrite request omitted or changed the saved speech requirements")
	}
}
