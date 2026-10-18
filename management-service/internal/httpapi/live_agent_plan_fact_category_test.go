package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
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

type factCategoryAuditFixture struct{}

func (factCategoryAuditFixture) BeginAdminAudit(context.Context, model.AdminAuditLog) (string, error) {
	return "fixture", nil
}
func (factCategoryAuditFixture) CompleteAdminAudit(context.Context, string, model.AdminAuditLog) error {
	return nil
}
func (factCategoryAuditFixture) ListAdminAudits(context.Context, int64) ([]model.AdminAuditLog, error) {
	return nil, nil
}

func TestFactCategoryRoutesRequireAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, route := range []struct{ method, path string }{{"PUT", "/api/v1/live-agent-plans/4/facts/7/category"}, {"POST", "/api/v1/live-agent-plans/4/facts/suggest-types"}} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
}

func TestFactTypeSuggestParseAndPrompt(t *testing.T) {
	for _, tc := range []struct {
		raw string
		low bool
	}{{`{"category":"appointment","confidence":0.95,"reason":"预约条件"}`, false}, {"```json\n{\"category\":\"appointment\",\"confidence\":0.4}\n```", true}} {
		got, err := parseFactTypeSuggest(tc.raw)
		if err != nil || got.Category != "appointment" || got.LowConf != tc.low {
			t.Fatal(got, err)
		}
	}
	for _, raw := range []string{"broken", `{}`, `{"category":""}`} {
		if _, err := parseFactTypeSuggest(raw); err == nil {
			t.Fatal("invalid output accepted", raw)
		}
	}
	pack, err := loadIndustryPack("local_service", "restaurant")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildFactTypeSuggestSystemPrompt(factCategoryCatalog{Categories: pack.FactCategories})
	if !strings.Contains(prompt, "appointment") || !strings.Contains(prompt, "预约") {
		t.Fatal("industry categories absent")
	}
}

// Explicit local-only opt-in. Creates/drops a random schema and never uses a
// production login, fact, model account, or text-to-speech provider.
func TestFactCategoryHTTPMySQLManualSaveAndCatalog(t *testing.T) {
	if os.Getenv("FACT_CATEGORY_DB_TEST") != "1" {
		t.Skip("explicit local isolated MySQL required")
	}
	cfg := config.Load()
	if cfg.DBHost != "127.0.0.1" {
		t.Fatal("local database required")
	}
	// The explicitly started local fixture uses an empty password; config.Load
	// otherwise substitutes the development default for an empty environment.
	cfg.DBPassword = ""
	dsn := mysql.NewConfig()
	dsn.User = cfg.DBUser
	dsn.Passwd = cfg.DBPassword
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	dsn.ParseTime = true
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("fact_category_test_%d", time.Now().UnixNano())
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
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	raw, err := os.ReadFile("../db/live_runtime_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n-- +statement\n") {
		f := strings.Fields(q)
		if len(f) > 5 && (f[5] == "live_agent_plans" || f[5] == "live_agent_plan_facts" || f[5] == "live_agent_plan_fact_revisions" || f[5] == "live_fact_categories" || f[5] == "live_agent_plan_room_bindings" || f[5] == "live_agent_plan_terms" || f[5] == "live_agent_plan_term_variants") {
			exec(q)
		}
	}
	exec(`CREATE TABLE mgmt_users(id BIGINT PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,avatar_url,role) VALUES(8,14,'fixture','','customer')`)
	token := "local-fixture-only"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at) VALUES(8,?,DATE_ADD(NOW(),INTERVAL 1 DAY))`, hash)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description,biz_domain,local_service_industry) VALUES(4,14,'测试餐饮','','local_service','restaurant'),(5,14,'另一个方案','','ecommerce','general')`)
	exec(`INSERT INTO live_agent_plan_facts(id,tenant_id,plan_id,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,scope_json) VALUES(7,14,4,'other','预约','无需预约','不可承诺免排队','高峰期排队','原资料',JSON_OBJECT('kind','common'))`)
	cfg.DBName = schema
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	audit := auditlog.New("127.0.0.1:1", "", 0, 10, factCategoryAuditFixture{})
	defer audit.Close()
	s := &Server{store: store, auth: auth.NewResolver("test", store), audit: audit}
	handler := s.Handler()
	call := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "lc_session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	cat, err := model.NewLiveFactCategory("门店补充", "other", "租户自定义")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateLiveFactCategory(context.Background(), 14, 8, cat); err != nil {
		t.Fatal(err)
	}
	w := call("GET", "/api/v1/live-agent-fact-categories?plan_id=4", "", 200)
	var catalog factCategoryCatalog
	if json.Unmarshal(w.Body.Bytes(), &catalog) != nil || !catalog.contains("appointment") || !catalog.contains("fulfillment") || !catalog.contains(cat.Value) {
		t.Fatal(w.Body.String())
	}
	for _, c := range catalog.Categories {
		if err := s.validatePlanFactCategory(context.Background(), 14, 4, c.Value); err != nil {
			t.Fatal("displayed option rejected", c, err)
		}
	}
	w = call("PUT", "/api/v1/live-agent-plans/4/facts/7/category", `{"tenant_id":14,"expected_version_no":1,"category":"appointment"}`, 200)
	var got model.LiveAgentPlanFact
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.VersionNo != 2 || got.Category != "appointment" || got.Value != "无需预约" || got.ForbiddenWording != "不可承诺免排队" || got.SafeRewrite != "高峰期排队" || got.Scope.Kind != "common" {
		t.Fatal(w.Body.String())
	}
	call("PUT", "/api/v1/live-agent-plans/4/facts/7/category", `{"tenant_id":14,"expected_version_no":1,"category":"other"}`, 409)
	call("PUT", "/api/v1/live-agent-plans/4/facts/7/category", `{"tenant_id":15,"expected_version_no":2,"category":"other"}`, 403)
	call("PUT", "/api/v1/live-agent-plans/5/facts/7/category", `{"tenant_id":14,"expected_version_no":2,"category":"other"}`, 404)
	call("PUT", "/api/v1/live-agent-plans/4/facts/7/category", `{"tenant_id":14,"expected_version_no":2,"category":"not_a_category"}`, 400)
	call("PUT", "/api/v1/live-agent-plans/4/facts/7/category", fmt.Sprintf(`{"expected_version_no":2,"category":%q}`, cat.Value), 200)
	call("POST", "/api/v1/live-agent-plans/4/facts/suggest-types", `{"facts":[]}`, 400)
	var revisions int
	if err = db.QueryRow(`SELECT COUNT(*) FROM live_agent_plan_fact_revisions WHERE action='category' AND fact_id=7`).Scan(&revisions); err != nil || revisions != 2 {
		t.Fatal(revisions, err)
	}
}
