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
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	auditlog "livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

func TestSelectedPlanPromptFillsKnownAndPreservesUnresolved(t *testing.T) {
	values := map[string]string{"直播间名": "荣昌卤鹅", "主播名": "小林", "主播身份": "店主本人", "直播赛道": "食品", "空值": " \t", "商品": "1号链接：\n商品名：卤鹅", "福利": "1. 优惠券"}
	for _, test := range []struct{ name, input, want string }{
		{"screenshot", "【身份】\n你是{{主播名}}，{{主播身份}}。\n直播间是{{直播间名}}，做的是{{直播赛道}}。", "【身份】\n你是小林，店主本人。\n直播间是荣昌卤鹅，做的是食品。"},
		{"missing and runtime", " \r\n{{未知}} {{ 空值 }} {{开场方式}} {{已有开场}} {{样本话术}} {{近期写作记录}}\r\n ", " \r\n{{未知}} {{ 空值 }} {{开场方式}} {{已有开场}} {{样本话术}} {{近期写作记录}}\r\n "},
		{"whitespace and repeated", "{{ 主播名 }}：{{主播名}}\n**正文**\n{{}}", "小林：小林\n**正文**\n{{}}"},
		{"known blocks", "前\n{{#商品}}旧商品{{/商品}}\n{{# 福利 }}旧福利{{/ 福利 }}\n后", "前\n1号链接：\n商品名：卤鹅\n1. 优惠券\n后"},
		{"unknown block unchanged", "{{#未知}}\n{{主播名}}\n{{/未知}} {{主播名}}", "{{#未知}}\n{{主播名}}\n{{/未知}} 小林"},
		{"empty known block unchanged", "{{#商品}}\n{{/商品}} {{#福利}}\n{{/福利}}", "1号链接：\n商品名：卤鹅 1. 优惠券"},
		{"unclosed unchanged", "{{#商品}}未完成", "{{#商品}}未完成"},
		{"malformed unchanged", "{{#商品}}未完成{{/福利}}", "{{#商品}}未完成{{/福利}}"},
		{"literal html", "<script>{{未知}}</script>", "<script>{{未知}}</script>"},
		{"empty", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := fillSelectedPlanPrompt(test.input, values)
			if err != nil || got != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
	original := "{{#商品}}\r\n{{主播名}}\r\n{{/商品}} {{福利}}"
	if got, err := fillSelectedPlanPrompt(original, map[string]string{"主播名": "小林"}); err != nil || got != original {
		t.Fatalf("missing block changed: %q %v", got, err)
	}
}

func TestSelectedPlanPromptReplacementIsLiteralNonrecursiveAndBounded(t *testing.T) {
	values := map[string]string{"主播名": "{{直播间名}} $1 \\ <b>", "直播间名": "不应出现", "商品": "{{主播名}}"}
	got, err := fillSelectedPlanPrompt("{{主播名}}\n{{#商品}}{{/商品}}", values)
	if err != nil || got != "{{直播间名}} $1 \\ <b>\n{{主播名}}" {
		t.Fatalf("recursive or transformed: %q %v", got, err)
	}
	for _, test := range []struct{ input, value string }{
		{"{{键}}", strings.Repeat("字", 20001)},
		{"{{键}}", strings.Repeat("😀", 15001)},
		{"{{键}}{{键}}", strings.Repeat("x", 10001)},
	} {
		if got, err := fillSelectedPlanPrompt(test.input, map[string]string{"键": test.value}); err == nil || got != "" {
			t.Fatalf("oversized preview accepted: len=%d err=%v", len(got), err)
		}
	}
	want := strings.Repeat("字", 20000)
	if got, err := fillSelectedPlanPrompt("{{键}}", map[string]string{"键": want}); err != nil || got != want {
		t.Fatal("exact limit rejected", err)
	}
}

// Opt-in only: a fresh loopback MySQL, never a production account or database.
func TestSelectedPlanPromptHTTPMySQL(t *testing.T) {
	if os.Getenv("PROMPT_SELECTION_DB_TEST") != "1" {
		t.Skip("explicit isolated loopback MySQL required")
	}
	cfg := config.Load()
	if cfg.DBHost != "127.0.0.1" {
		t.Fatal("local database required")
	}
	cfg.DBPassword = ""
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr, dsn.ParseTime = cfg.DBUser, "tcp", net.JoinHostPort(cfg.DBHost, cfg.DBPort), true
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("prompt_selection_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE `" + schema + "`"); err != nil {
			t.Error(err)
		}
	}()
	dsn.DBName = schema
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
	raw, err := os.ReadFile("../db/live_runtime_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"live_agent_plans": true, "live_agent_plan_room_bindings": true, "live_agent_plan_terms": true, "live_agent_plan_term_variants": true, "live_agent_plan_product_links": true, "live_agent_plan_product_attributes": true, "live_agent_plan_product_selling_points": true, "live_agent_plan_benefits": true, "live_agent_plan_facts": true}
	for _, query := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(query)
		if len(fields) > 5 && wanted[fields[5]] {
			exec(query)
		}
	}
	exec(`CREATE TABLE mgmt_users(id BIGINT PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`CREATE TABLE mgmt_system_settings(setting_key VARCHAR(128) PRIMARY KEY,value_text TEXT,setting_group VARCHAR(64),updated_by_user_id BIGINT NULL,updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,avatar_url,role) VALUES(8,14,'fixture','','customer')`)
	token := "local-prompt-selection-fixture"
	exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at) VALUES(8,?,DATE_ADD(NOW(),INTERVAL 1 DAY))`, fmt.Sprintf("%x", sha256.Sum256([]byte(token))))
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description,biz_domain,ecommerce_industry) VALUES(4,14,'荣昌卤鹅','','ecommerce','food'),(5,15,'禁止跨租户取值','','ecommerce','beauty')`)
	original := "你是{{主播名}}，{{主播身份}}。{{直播间名}} / {{直播赛道}}\n{{未知}} {{开场方式}}\n{{#商品}}\n{{/商品}}"
	exec(`INSERT INTO mgmt_system_settings(setting_key,value_text,setting_group) VALUES('plan_prompt_template_persona_md',?,'plan_prompt_templates'),('plan_prompt_draft_enabled','1','general')`, original)
	cfg.DBName = schema
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	audit := auditlog.New("127.0.0.1:1", "", 0, 10, factCategoryAuditFixture{})
	defer audit.Close()
	s := &Server{store: store, auth: auth.NewResolver("test", store), audit: audit}
	plan, err := store.GetLiveAgentPlan(t.Context(), 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.planPromptSelectionValues(t.Context(), 14, plan); err != nil {
		t.Fatal("fixture selection data:", err)
	}
	call := func(path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "lc_session", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}
	w := call("/api/v1/live-agent-plans/4/prompt/templates?page=persona_md", 200)
	var result struct {
		Templates []planPromptSelectedTemplate `json:"templates"`
		Revision  string                       `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Templates) != 1 {
		t.Fatal("selection result", err, w.Body.String())
	}
	item := result.Templates[0]
	if item.Content != original || item.RenderError != "" || !strings.Contains(item.RenderedContent, "小林") || !strings.Contains(item.RenderedContent, "荣昌卤鹅 / 食品") || !strings.Contains(item.RenderedContent, "{{未知}} {{开场方式}}\n{{#商品}}\n{{/商品}}") {
		t.Fatal("wrong values or unresolved text changed", item)
	}
	call("/api/v1/live-agent-plans/5/prompt/templates?page=persona_md", 404)
	call("/api/v1/live-agent-plans/4/prompt/templates?page=invalid", 400)
	var stored string
	if err := database.QueryRow(`SELECT value_text FROM mgmt_system_settings WHERE setting_key='plan_prompt_template_persona_md'`).Scan(&stored); err != nil || stored != original {
		t.Fatal("selection changed original", stored, err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group='plan_prompt_template_catalog'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("GET seeded catalog", count, err)
	}
	exec(`DROP TABLE live_agent_plan_benefits`)
	call("/api/v1/live-agent-plans/4/prompt/templates?page=persona_md", 500)
}
