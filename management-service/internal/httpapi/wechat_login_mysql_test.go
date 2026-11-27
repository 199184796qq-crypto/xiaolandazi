package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type fixtureWechatOAuth struct{}

func (fixtureWechatOAuth) AppID() string { return "wx-test" }
func (fixtureWechatOAuth) OAuthURL(state string) string {
	return "https://oauth.test/authorize?state=" + state
}
func (fixtureWechatOAuth) ExchangeOAuthCode(_ context.Context, code string) (string, error) {
	if code == "" {
		return "", sql.ErrNoRows
	}
	return "openid-" + code, nil
}

func TestWechatLoginHTTPMySQL(t *testing.T) {
	port := os.Getenv("XIAOLAN_WECHAT_TEST_DB_PORT")
	if port == "" {
		t.Skip("isolated localhost MySQL required")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1024 || number > 65535 || number == 3306 {
		t.Fatal("invalid isolated MySQL port")
	}
	cfg := config.Load()
	cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword = "127.0.0.1", port, "root", ""
	cfg.DBName = "wechat_login_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr = cfg.DBUser, "tcp", net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	driver, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if _, err := driver.Exec("CREATE DATABASE " + cfg.DBName); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := driver.Exec("DROP DATABASE " + cfg.DBName); err != nil {
			t.Error(err)
		}
	}()
	dsn.DBName = cfg.DBName
	fixture, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	store, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := fixture.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE mgmt_users(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_tenants (id BIGINT UNSIGNED PRIMARY KEY, code VARCHAR(64), name VARCHAR(128),
		status VARCHAR(32) DEFAULT 'active', created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`INSERT INTO mgmt_tenants (id, code, name) VALUES (14, 'fixture-org', 'Fixture'), (15, 'fixture-org-two', 'Fixture Two')`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT NOT NULL,token_hash VARCHAR(64) PRIMARY KEY,expires_at DATETIME(3),last_seen_at DATETIME(3),client_ip VARCHAR(64),user_agent VARCHAR(512))`)
	password, err := auth.HashPassword("fixture-original-1")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,avatar_url,role,password_hash)
        VALUES (8,14,'fixture-one','','customer',?), (9,15,'fixture-two','','customer',?),
		       (10,NULL,'fixture-admin','','platform_admin',?)`, password, password, password)
	first := &http.Cookie{Name: "lc_session", Value: "fixture-first"}
	second := &http.Cookie{Name: "lc_session", Value: "fixture-second"}
	administrator := &http.Cookie{Name: "lc_session", Value: "fixture-admin"}
	for i, cookie := range []*http.Cookie{first, second, administrator} {
		exec(`INSERT INTO mgmt_sessions (user_id,token_hash,expires_at,last_seen_at) VALUES (?,?,DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 DAY),UTC_TIMESTAMP())`, 8+i, wechatHash(cookie.Value))
	}
	if err := store.MigrateAuth(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateWechatLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	const origin = "https://customer.example"
	resolver := auth.NewResolver("production", store)
	s := &Server{store: store, auth: resolver, publicWebURL: origin, wechatLogin: fixtureWechatOAuth{}}
	t.Run("customer auth route isolates shared phone", func(t *testing.T) {
		const phone, customerCode, employeeCode = "13800000008", "246810", "135790"
		exec("UPDATE mgmt_users SET phone = ? WHERE id IN (8, 10)", phone)
		customerHash, err := bcrypt.GenerateFromPassword([]byte(customerCode), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		employeeHash, err := bcrypt.GenerateFromPassword([]byte(employeeCode), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO mgmt_sms_login_codes (user_id, phone, code_hash, expires_at) VALUES
			(8, ?, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 5 MINUTE)),
			(10, ?, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 5 MINUTE))`, phone, string(customerHash), phone, string(employeeHash))
		handler := s.Handler()
		login := func(path, code string, wantStatus int) *httptest.ResponseRecorder {
			t.Helper()
			body := `{"method":"sms","identifier":"` + phone + `","credential":"` + code + `"}`
			r := httptest.NewRequest(http.MethodPost, origin+path, strings.NewReader(body))
			r.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != wantStatus {
				t.Fatalf("%s returned %d, want %d: %s", path, w.Code, wantStatus, w.Body)
			}
			if wantStatus != http.StatusOK && len(w.Result().Cookies()) != 0 {
				t.Fatal("failed login issued a session")
			}
			return w
		}
		login("/api/v1/auth/login", customerCode, http.StatusUnauthorized)
		login("/api/v1/auth/customer/login", employeeCode, http.StatusUnauthorized)
		w := login("/api/v1/auth/customer/login", customerCode, http.StatusOK)
		if !strings.Contains(w.Body.String(), `"role":"customer"`) || len(w.Result().Cookies()) != 1 {
			t.Fatalf("customer login did not issue the expected customer session: %s", w.Body)
		}
		login("/api/v1/admin/workspace/login", customerCode, http.StatusBadRequest)
	})
	t.Run("staff account center reads existing departments and profile", func(t *testing.T) {
		exec(`CREATE TABLE crm_sales_staff(user_id BIGINT UNSIGNED, employee_code VARCHAR(64), status VARCHAR(32))`)
		if err := store.MigrateStaff(t.Context()); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO mgmt_users(id,username,display_name,avatar_url,role,password_hash)
			VALUES (11,'fixture-staff','演示员工','','staff',?)`, password)
		exec(`INSERT INTO staff_employees (user_id,employee_no,primary_group_id)
			SELECT 11, 'TEST-11', id FROM staff_groups WHERE code='live_operations'`)
		exec(`INSERT INTO staff_employee_roles (employee_id,role_id)
			SELECT e.id, r.id FROM staff_employees e CROSS JOIN staff_roles r
			WHERE e.user_id=11 AND r.code IN ('live_operations_staff','live_operations_manager','warehouse_after_sales_staff')`)
		employee := &http.Cookie{Name: "lc_session", Value: "fixture-staff"}
		insertSession := `INSERT INTO mgmt_sessions (user_id,token_hash,expires_at,last_seen_at)
			VALUES (?,?,DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 DAY),UTC_TIMESTAMP())`
		exec(insertSession, 11, wechatHash(employee.Value))
		handler := http.NewServeMux()
		handler.Handle("/", s.Handler())
		// This fixture exercises the existing profile handler without the audit store.
		handler.HandleFunc("PATCH /api/v1/account/profile", s.accountUpdateProfile)
		handler.HandleFunc("PATCH /api/v1/admin/workspace/account", s.adminWorkspaceUpdateAccount)
		request := func(method, path, payload string, cookie *http.Cookie, wantStatus int) *httptest.ResponseRecorder {
			t.Helper()
			req := httptest.NewRequest(method, origin+"/api/v1"+path, strings.NewReader(payload))
			req.Header.Set("Origin", origin)
			req.AddCookie(cookie)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != wantStatus {
				t.Fatalf("%s %s = %d, want %d: %s", method, path, res.Code, wantStatus, res.Body)
			}
			return res
		}
		const accountPath = "/admin/workspace/account"
		const checkPath = accountPath + "/check-identifier"
		request(http.MethodGet, accountPath, "", first, http.StatusForbidden)
		request(http.MethodPatch, accountPath, `{"display_name":"denied"}`, first, http.StatusForbidden)
		request(http.MethodPost, checkPath, `{"field":"username","value":"fixture-one"}`, first, http.StatusForbidden)
		request(http.MethodGet, "/admin/workspace/shipping-regions", "", first, http.StatusForbidden)
		regionResponse := request(http.MethodGet, "/admin/workspace/shipping-regions", "", employee, http.StatusOK)
		if !json.Valid(regionResponse.Body.Bytes()) || !strings.Contains(regionResponse.Body.String(), "四川省") {
			t.Fatal("missing shipping region options")
		}
		readAccount := func(cookie *http.Cookie) (model.AccountProfile, []adminAccountDepartment, string) {
			t.Helper()
			var account struct {
				Profile     model.AccountProfile     `json:"profile"`
				Departments []adminAccountDepartment `json:"departments"`
				SystemTitle string                   `json:"system_title"`
			}
			res := request(http.MethodGet, accountPath+"?user_id=8", "", cookie, http.StatusOK)
			if err := json.Unmarshal(res.Body.Bytes(), &account); err != nil {
				t.Fatal(err)
			}
			return account.Profile, account.Departments, account.SystemTitle
		}
		profile, departments, title := readAccount(employee)
		if profile.UserID != 11 || profile.DisplayName != "演示员工" || title != "" || len(departments) != 2 ||
			departments[0].Name != "营销运维部" || !departments[0].IsPrimary || len(departments[0].Positions) != 2 ||
			departments[1].Name != "仓储售后部" || departments[1].IsPrimary || len(departments[1].Positions) != 1 {
			t.Fatalf("unexpected employee profile or organization: %+v %+v %q", profile, departments, title)
		}
		request(http.MethodPatch, "/account/profile", `{"email":"staff@example.invalid","display_name":"ignored"}`, employee, http.StatusOK)
		profile, _, _ = readAccount(employee)
		if profile.Email != "staff@example.invalid" || profile.DisplayName != "演示员工" {
			t.Fatalf("staff profile edits did not follow existing rules: %+v", profile)
		}
		request(http.MethodPost, checkPath, `{"field":"username","value":"fixture-staff"}`, employee, http.StatusNoContent)
		request(http.MethodPost, checkPath, `{"field":"username","value":"fixture-admin"}`, employee, http.StatusConflict)
		request(http.MethodPost, checkPath, `{"field":"username","value":"fixture-one"}`, employee, http.StatusNoContent)
		request(http.MethodPost, checkPath, `{"field":"phone","value":"+86 13800000008"}`, employee, http.StatusConflict)
		request(http.MethodPost, checkPath, `{"field":"phone","value":"13800000009"}`, employee, http.StatusNoContent)
		conflict := request(http.MethodPatch, accountPath, `{"username":"fixture-admin","display_name":"不得部分保存"}`, employee, http.StatusConflict)
		if !strings.Contains(conflict.Body.String(), "登录账号已被使用") {
			t.Fatal("username conflict did not identify the right field")
		}
		profile, _, _ = readAccount(employee)
		if profile.DisplayName != "演示员工" {
			t.Fatal("duplicate username partially updated the employee nickname")
		}
		request(http.MethodPatch, accountPath, `{"user_id":8,"display_name":"denied"}`, employee, http.StatusBadRequest)
		request(http.MethodPatch, accountPath, `{"username":" 小张 ","display_name":" 员工昵称 ","phone":"13800000009"}`, employee, http.StatusOK)
		profile, renamedDepartments, _ := readAccount(employee)
		if profile.Username != "小张" || profile.DisplayName != "员工昵称" || profile.Phone != "13800000009" || len(renamedDepartments) != 2 {
			t.Fatalf("renamed employee profile or department changed incorrectly: %+v %+v", profile, renamedDepartments)
		}
		boot := request(http.MethodGet, "/admin/workspace/bootstrap", "", employee, http.StatusOK)
		if !strings.Contains(boot.Body.String(), "员工昵称") {
			t.Fatal("existing session did not pick up the new employee nickname")
		}
		profile, departments, title = readAccount(administrator)
		if profile.UserID != 10 || departments == nil || len(departments) != 0 || title != "超级系统管理员" {
			t.Fatalf("unexpected administrator account response: %+v %+v %q", profile, departments, title)
		}
		exec("UPDATE mgmt_users SET must_change_password=TRUE WHERE id=11")
		request(http.MethodGet, accountPath, "", employee, http.StatusForbidden)
	})
	mux := http.NewServeMux()
	s.registerWechatLoginRoutes(mux)
	mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/v1/admin/workspace/bootstrap", s.adminWorkspaceBootstrap)
	mux.HandleFunc("POST /api/v1/auth/change-password", s.authChangePassword)
	request := func(method, path, body, site string, mobile bool, status int, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		if !strings.HasPrefix(path, "/api/") {
			path = wechatLoginBase + path
		}
		r := httptest.NewRequest(method, origin+path, strings.NewReader(body))
		r.Header.Set("Origin", site)
		if mobile {
			r.Header.Set("User-Agent", "Android MicroMessenger/8.0")
		}
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: status %d, want %d. %s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	for _, fixture := range []struct {
		path, responseText string
		cookie             *http.Cookie
		status             int
	}{
		{"/api/v1/bootstrap", "fixture-org", first, http.StatusOK},
		{"/api/v1/bootstrap", "super_system_admin", administrator, http.StatusOK},
		{"/api/v1/admin/workspace/bootstrap", "超级系统管理员", administrator, http.StatusOK},
		{"/api/v1/admin/workspace/bootstrap", "没有营销运维部", first, http.StatusForbidden},
	} {
		response := request(http.MethodGet, fixture.path, "", "", false, fixture.status, fixture.cookie)
		if !strings.Contains(response.Body.String(), fixture.responseText) {
			t.Fatalf("%s returned unexpected payload: %s", fixture.path, response.Body)
		}
	}
	fromURL := func(raw string) string {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil || !validWechatLoginToken(u.Query().Get("state")) {
			t.Fatal("invalid OAuth URL", raw)
		}
		return u.Query().Get("state")
	}
	cookieOf := func(w *httptest.ResponseRecorder, name string) *http.Cookie {
		t.Helper()
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == name && cookie.Value != "" {
				return cookie
			}
		}
		t.Fatal("missing cookie", name)
		return nil
	}
	start := func(path string, cookies ...*http.Cookie) (string, *http.Cookie) {
		w := request(http.MethodGet, path, "", "", true, http.StatusSeeOther, cookies...)
		return fromURL(w.Header().Get("Location")), cookieOf(w, wechatLoginCookie)
	}
	callback := func(state, code string, status int, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return request(http.MethodGet, "/callback?state="+state+"&code="+code, "", "", true, status, cookies...)
	}
	location := func(w *httptest.ResponseRecorder, want string) {
		t.Helper()
		if got := w.Header().Get("Location"); got != want {
			t.Fatalf("redirect %q, want %q", got, want)
		}
	}
	binding := func(mobile bool, owner *http.Cookie) (*httptest.ResponseRecorder, map[string]string) {
		t.Helper()
		w := request(http.MethodPost, "/bind", "", origin, mobile, http.StatusOK, owner)
		var info map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		return w, info
	}

	location(request(http.MethodGet, "/start", "", "", false, http.StatusSeeOther), "/login?wechat_login=manual")
	request(http.MethodPost, "/bind", "", "https://other.example", true, http.StatusForbidden, first)
	request(http.MethodPost, "/bind", "", origin, true, http.StatusUnauthorized)
	state, cookie := start("/start")
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("weak OAuth cookie")
	}
	location(callback(state, "first", http.StatusSeeOther), "/login?wechat_login=failed")
	location(callback(state, "first", http.StatusSeeOther, cookie), "/login?wechat_login=unbound")
	location(callback(state, "first", http.StatusSeeOther, cookie), "/login?wechat_login=failed")

	w, info := binding(true, first)
	state, cookie = fromURL(info["authorize_url"]), cookieOf(w, wechatLoginCookie)
	location(callback(state, "first", http.StatusSeeOther, second, cookie), "/me?wechat_login=failed")
	w, info = binding(true, first)
	location(callback(fromURL(info["authorize_url"]), "first", http.StatusSeeOther, first, cookieOf(w, wechatLoginCookie)), "/me?wechat_login=bound")

	state, cookie = start("/start?return=%2Fwallet%3Frecharge%3D12")
	w = callback(state, "first", http.StatusSeeOther, cookie)
	location(w, "/wallet?recharge=12")
	loginCookie := cookieOf(w, "lc_session")
	r := httptest.NewRequest(http.MethodGet, origin+"/", nil)
	r.AddCookie(loginCookie)
	actor, err := resolver.Resolve(r)
	if err != nil || actor.UserID != 8 || !loginCookie.Secure || !loginCookie.HttpOnly {
		t.Fatalf("wrong login: %d / %v", actor.UserID, err)
	}

	_, info = binding(false, second)
	ticket := info["ticket"]
	if !validWechatLoginToken(ticket) || !strings.HasPrefix(info["qr_code_data_url"], "data:image/png;base64,") {
		t.Fatal("missing desktop QR")
	}
	state, cookie = start("/scan?ticket=" + ticket)
	w = callback(state, "first", http.StatusOK, cookie)
	match := regexp.MustCompile(`<strong>([0-9]{6})</strong>`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("missing confirmation code")
	}
	confirm := func(code string, status int, owner *http.Cookie) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"ticket": ticket, "code": code})
		request(http.MethodPost, "/confirm", string(body), origin, false, status, owner)
	}
	confirm(match[1], http.StatusBadRequest, first)
	confirm(match[1], http.StatusConflict, second)

	_, info = binding(false, second)
	ticket = info["ticket"]
	state, cookie = start("/scan?ticket=" + ticket)
	w = callback(state, "second", http.StatusOK, cookie)
	match = regexp.MustCompile(`<strong>([0-9]{6})</strong>`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("missing second confirmation code")
	}
	wrong := "000000"
	if match[1] == wrong {
		wrong = "111111"
	}
	confirm(wrong, http.StatusBadRequest, second)
	confirm(match[1], http.StatusOK, second)
	confirm(match[1], http.StatusBadRequest, second)

	state, cookie = start("/start?return=https%3A%2F%2Fother.example")
	location(callback(state, "second", http.StatusSeeOther, cookie), "/")
	exec(`UPDATE mgmt_users SET status='disabled' WHERE id=9`)
	state, cookie = start("/start")
	location(callback(state, "second", http.StatusSeeOther, cookie), "/login?wechat_login=failed")
	exec(`UPDATE mgmt_users SET status='active' WHERE id=9`)
	// For an existing binding, account/password login and pre-upgrade sessions have no WeChat proof.
	passwordBody := func(current, next string) string {
		payload, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next, "confirm_password": next})
		return string(payload)
	}
	security := func(owner *http.Cookie, bound, allowed bool) {
		t.Helper()
		w := request(http.MethodGet, "/status", "", "", false, http.StatusOK, owner)
		var flags map[string]bool
		if err := json.Unmarshal(w.Body.Bytes(), &flags); err != nil || flags["bound"] != bound ||
			flags["password_change_allowed"] != allowed || !flags["enabled"] {
			t.Fatalf("unexpected account security state: %s / %v", w.Body, err)
		}
	}
	security(first, true, false)
	security(loginCookie, true, true)
	request(http.MethodPost, "/api/v1/auth/change-password", passwordBody("fixture-original-1", "fixture-replacement-2"),
		origin, false, http.StatusForbidden, first)
	request(http.MethodPost, "/bind", "", origin, true, http.StatusConflict, first)
	request(http.MethodPost, "/unbind", "", origin, true, http.StatusUnauthorized)
	request(http.MethodPost, "/unbind", "", "https://other.example", true, http.StatusForbidden, first)

	mobileUnbind := func(owner *http.Cookie, upstreamCode string) *httptest.ResponseRecorder {
		t.Helper()
		w := request(http.MethodPost, "/unbind", "", origin, true, http.StatusOK, owner)
		var result map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return callback(fromURL(result["authorize_url"]), upstreamCode, http.StatusSeeOther, owner, cookieOf(w, wechatLoginCookie))
	}
	location(mobileUnbind(first, "second"), "/me?wechat_login=original_required")
	security(first, true, false)

	w = request(http.MethodPost, "/api/v1/auth/change-password", passwordBody("fixture-original-1", "fixture-replacement-2"),
		origin, false, http.StatusOK, loginCookie)
	rotated := cookieOf(w, "lc_session")
	security(rotated, true, true)
	request(http.MethodPost, "/api/v1/auth/change-password", passwordBody("fixture-replacement-2", "old-session-denied"),
		origin, false, http.StatusUnauthorized, loginCookie)
	request(http.MethodPost, "/api/v1/auth/change-password", passwordBody("fixture-replacement-2", "old-session-denied"),
		origin, false, http.StatusUnauthorized, first)

	w = request(http.MethodPost, "/unbind", "", origin, false, http.StatusOK, rotated)
	var unlinkPairing map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &unlinkPairing); err != nil {
		t.Fatal(err)
	}
	ticket = unlinkPairing["ticket"]
	state, cookie = start("/scan?ticket=" + ticket)
	w = callback(state, "second", http.StatusOK, cookie)
	if regexp.MustCompile(`<strong>[0-9]{6}</strong>`).MatchString(w.Body.String()) {
		t.Fatal("wrong WeChat received an unlink code")
	}
	state, cookie = start("/scan?ticket=" + ticket)
	w = callback(state, "first", http.StatusOK, cookie)
	match = regexp.MustCompile(`<strong>([0-9]{6})</strong>`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("original WeChat did not receive unlink confirmation")
	}
	confirm(match[1], http.StatusBadRequest, second)
	confirm(match[1], http.StatusOK, rotated)
	request(http.MethodPost, "/unbind", "", origin, true, http.StatusUnauthorized, rotated)

	state, cookie = start("/start")
	location(callback(state, "first", http.StatusSeeOther, cookie), "/login?wechat_login=unbound")
	// Simulate a fresh account/password session after successful unlink.
	exec(`INSERT INTO mgmt_sessions (user_id,token_hash,expires_at,last_seen_at) VALUES (8,?,DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 DAY),UTC_TIMESTAMP())`,
		wechatHash(first.Value))
	security(first, false, true)
	w, info = binding(true, first)
	location(callback(fromURL(info["authorize_url"]), "replacement", http.StatusSeeOther, first, cookieOf(w, wechatLoginCookie)), "/me?wechat_login=bound")
	security(first, true, false)
	location(mobileUnbind(first, "replacement"), "/login?wechat_login=unbound_confirmed")
	state, cookie = start("/start")
	location(callback(state, "replacement", http.StatusSeeOther, cookie), "/login?wechat_login=unbound")
}
