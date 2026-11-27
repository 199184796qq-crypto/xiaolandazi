package auth

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"

	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestPasswordLoginIdentifiersMySQL(t *testing.T) {
	port := os.Getenv("XIAOLAN_PASSWORD_LOGIN_TEST_DB_PORT")
	if port == "" {
		t.Skip("isolated localhost MySQL required")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1024 || number > 65535 || number == 3306 {
		t.Fatal("invalid isolated MySQL port")
	}
	ctx := t.Context()
	schema := "password_login_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr = "root", "tcp", net.JoinHostPort("127.0.0.1", port)
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE " + schema); err != nil {
			t.Error(err)
		}
	}()
	dsn.DBName = schema
	fixture, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := fixture.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE mgmt_users (
		id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, tenant_id BIGINT UNSIGNED, username VARCHAR(64) NOT NULL,
		password_hash VARCHAR(255) NOT NULL, must_change_password BOOLEAN NOT NULL DEFAULT false,
		display_name VARCHAR(64) NOT NULL, avatar_url VARCHAR(255) NOT NULL DEFAULT '',
		role VARCHAR(32) NOT NULL DEFAULT 'customer', status VARCHAR(32) NOT NULL DEFAULT 'active',
		phone VARCHAR(128) NOT NULL DEFAULT '',
		phone_unique_key VARCHAR(128) GENERATED ALWAYS AS (NULLIF(TRIM(phone), '')) STORED,
		UNIQUE KEY uk_mgmt_users_phone_unique (phone_unique_key),
		UNIQUE KEY uk_mgmt_users_username (username),
		created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3)
	)`)
	cfg := config.Config{DBHost: "127.0.0.1", DBPort: port, DBUser: "root", DBName: schema}
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const fixturePassword, numericPassword, staffPassword = "fixture-customer-1", "fixture-numeric-2", "fixture-staff-3"
	hash := func(password string) string {
		t.Helper()
		result, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		return string(result)
	}
	exec(`INSERT INTO mgmt_users(id, username, password_hash, display_name, phone, status) VALUES
		(1, 'fixture-account', ?, 'fixture-nickname', '13800000001', 'active'),
		(2, '13900000002', ?, 'fixture-numeric', '', 'active'),
		(3, 'fixture-phone-owner', ?, 'fixture-other', '13900000002', 'active'),
		(4, 'fixture-disabled', ?, 'fixture-disabled', '13800000004', 'disabled')`,
		hash(fixturePassword), hash(numericPassword), hash(fixturePassword), hash(fixturePassword))
	exec(`INSERT INTO mgmt_users(id, tenant_id, username, password_hash, display_name, phone, role, must_change_password) VALUES
		(5, NULL, 'fixture-admin', ?, 'fixture-admin', '13800000005', 'platform_admin', 0),
		(6, NULL, 'fixture-employee', ?, 'fixture-employee', '13800000006', 'staff', 1),
		(7, NULL, 'fixture-sales', ?, 'fixture-sales', '', 'sales_staff', 0),
		(8, 18, 'fixture-agent', ?, 'fixture-agent', '', 'agent_admin', 0)`,
		hash(staffPassword), hash(fixturePassword), hash(fixturePassword), hash(fixturePassword))
	exec(`CREATE TABLE mgmt_sessions (
		user_id BIGINT UNSIGNED NOT NULL, token_hash CHAR(64) PRIMARY KEY, expires_at DATETIME(3) NOT NULL,
		last_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), client_ip VARCHAR(64), user_agent VARCHAR(512)
	)`)
	const existingToken = "fixture-session-from-before-split"
	exec(`INSERT INTO mgmt_sessions (user_id, token_hash, expires_at, last_seen_at)
		VALUES (6, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 1 HOUR), UTC_TIMESTAMP())`, hashToken(existingToken))
	if err := store.MigrateAuth(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("backfill preserves account types and existing session", func(t *testing.T) {
		for _, table := range []string{"mgmt_customer_accounts", "mgmt_staff_accounts"} {
			var count int
			if err := fixture.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 4 {
				t.Fatalf("%s: got count %d, error %v; want 4", table, count, err)
			}
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/workspace/bootstrap", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: existingToken})
		actor, err := NewResolver("production", store).Resolve(req)
		if err != nil || actor.UserID != 6 || actor.Role != "staff" || !actor.MustChangePassword {
			t.Fatalf("pre-migration employee session: got actor %+v, error %v", actor, err)
		}
		agent, err := store.GetUserByID(ctx, 8)
		if err != nil || agent.Role != "agent_admin" || agent.TenantID == nil || *agent.TenantID != 18 {
			t.Fatalf("agent account lost its role or organization: %v", err)
		}
	})

	tryScoped := func(t *testing.T, resolver *Resolver, scope appdb.AccountScope, identifier, password string, validCaptcha bool, userID int64, wantErr error) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		rec := httptest.NewRecorder()
		if validCaptcha {
			resolver.captchas["test-challenge"] = captchaChallenge{Code: "12345", ExpiresAt: time.Now().Add(time.Minute)}
			req.AddCookie(&http.Cookie{Name: captchaCookieName, Value: "test-challenge"})
		}
		actor, err := resolver.Authenticate(ctx, rec, req, LoginInput{
			Method: LoginMethodPassword, Identifier: identifier, Credential: password, Captcha: "12345", Scope: scope,
		})
		if !errors.Is(err, wantErr) || actor.UserID != userID {
			t.Fatalf("login with %q: got user %d, error %v; want user %d, error %v", identifier, actor.UserID, err, userID, wantErr)
		}
		cookies := rec.Result().Cookies()
		if wantErr != nil {
			if len(cookies) != 0 {
				t.Fatal("failed login issued a session")
			}
			return
		}
		if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly {
			t.Fatal("successful login did not issue the expected session")
		}
		req.AddCookie(cookies[0])
		sessionActor, err := resolver.Resolve(req)
		if err != nil || sessionActor.UserID != userID {
			t.Fatalf("resolve session: got user %d, error %v; want user %d", sessionActor.UserID, err, userID)
		}
	}
	try := func(t *testing.T, resolver *Resolver, identifier, password string, validCaptcha bool, userID int64, wantErr error) {
		t.Helper()
		tryScoped(t, resolver, appdb.AccountAny, identifier, password, validCaptcha, userID, wantErr)
	}
	resolver := NewResolver("production", store)
	try(t, resolver, "fixture-account", fixturePassword, true, 1, nil)
	try(t, resolver, " 13800000001 ", fixturePassword, true, 1, nil)
	try(t, resolver, "+86 13800000001", fixturePassword, true, 1, nil)
	try(t, resolver, "fixture-nickname", fixturePassword, true, 0, ErrInvalidCredentials)
	try(t, resolver, "other13800000001", fixturePassword, true, 0, ErrInvalidCredentials)
	try(t, resolver, "13800000004", fixturePassword, true, 0, ErrInvalidCredentials)
	try(t, resolver, "13800000001", fixturePassword, false, 0, ErrCaptchaInvalid)
	try(t, resolver, "13900000002", numericPassword, true, 2, nil)
	try(t, resolver, "13900000002", fixturePassword, true, 0, ErrInvalidCredentials)
	try(t, resolver, "+8613900000002", fixturePassword, true, 3, nil)
	try(t, resolver, "fixture-admin", staffPassword, true, 5, nil)
	try(t, resolver, "13800000006", fixturePassword, true, 6, nil)
	try(t, resolver, "fixture-sales", fixturePassword, true, 7, nil)
	try(t, resolver, "fixture-agent", fixturePassword, true, 8, nil)

	t.Run("customer SMS login", func(t *testing.T) {
		const code = "654321"
		exec(`INSERT INTO mgmt_sms_login_codes (user_id, phone, code_hash, expires_at)
			VALUES (1, '13800000001', ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 5 MINUTE))`, hash(code))
		resolver := NewResolver("production", store)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		rec := httptest.NewRecorder()
		input := LoginInput{Method: LoginMethodSMS, Identifier: "+86 13800000001", Credential: code}
		actor, err := resolver.Authenticate(ctx, rec, req, input)
		if err != nil || actor.UserID != 1 || actor.Role != "customer" || len(rec.Result().Cookies()) != 1 {
			t.Fatalf("SMS login returned user %d, role %q and error %v", actor.UserID, actor.Role, err)
		}
		rec = httptest.NewRecorder()
		if _, err := resolver.Authenticate(ctx, rec, req, input); !errors.Is(err, ErrSMSCodeInvalid) || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("used SMS challenge was accepted: %v", err)
		}
	})

	t.Run("shared phone preserves identity and permissions", func(t *testing.T) {
		sharedPhone := "13800000005"
		defer exec("UPDATE mgmt_users SET phone = '13800000001' WHERE id = 1")
		update := model.AccountProfileChanges{Phone: &sharedPhone}
		if _, err := store.UpdateAccountProfile(t.Context(), 1, update); err == nil {
			t.Fatal("preparation allowed a shared phone before the release cutover")
		}
		exec("DROP INDEX uk_mgmt_users_phone_unique ON mgmt_users")
		profile, err := store.UpdateAccountProfile(t.Context(), 1, update)
		if err != nil || profile.Phone != sharedPhone {
			t.Fatalf("saving customer profile with a staff phone: %v", err)
		}
		for _, duplicateID := range []int64{3, 6} {
			_, err := store.UpdateAccountProfile(t.Context(), duplicateID, update)
			var duplicate *mysql.MySQLError
			if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
				t.Fatalf("same-type duplicate phone was not rejected: %v", err)
			}
		}
		if err := store.MigrateAuth(t.Context()); err != nil {
			t.Fatalf("restart with shared phone: %v", err)
		}
		resolver := NewResolver("production", store)
		tryScoped(t, resolver, appdb.AccountCustomer, "+86 "+sharedPhone, fixturePassword, true, 1, nil)
		tryScoped(t, resolver, appdb.AccountStaff, sharedPhone, staffPassword, true, 5, nil)
		tryScoped(t, resolver, appdb.AccountStaff, sharedPhone, fixturePassword, true, 0, ErrInvalidCredentials)
		tryScoped(t, resolver, appdb.AccountCustomer, sharedPhone, staffPassword, true, 0, ErrInvalidCredentials)
		tryScoped(t, resolver, appdb.AccountCustomer, "fixture-admin", staffPassword, true, 0, ErrInvalidCredentials)
		tryScoped(t, resolver, appdb.AccountStaff, "fixture-account", fixturePassword, true, 0, ErrInvalidCredentials)
		try(t, resolver, sharedPhone, fixturePassword, true, 0, ErrInvalidCredentials)
		try(t, resolver, sharedPhone, staffPassword, true, 0, ErrInvalidCredentials)
		try(t, resolver, "fixture-account", fixturePassword, true, 1, nil)
		try(t, resolver, "fixture-admin", staffPassword, true, 5, nil)

		const customerCode, staffCode = "123456", "345678"
		exec(`INSERT INTO mgmt_sms_login_codes (user_id, phone, code_hash, expires_at)
			VALUES (1, ?, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 5 MINUTE)),
			       (5, ?, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 5 MINUTE))`, sharedPhone, hash(customerCode), sharedPhone, hash(staffCode))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/customer/login", nil)
		rec := httptest.NewRecorder()
		input := LoginInput{Method: LoginMethodSMS, Identifier: sharedPhone, Credential: staffCode, Scope: appdb.AccountCustomer}
		if _, err := resolver.Authenticate(ctx, rec, req, input); !errors.Is(err, ErrSMSCodeInvalid) || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("customer login accepted an employee SMS code: %v", err)
		}
		input.Credential = customerCode
		rec = httptest.NewRecorder()
		actor, err := resolver.Authenticate(ctx, rec, req, input)
		if err != nil || actor.UserID != 1 || actor.Role != "customer" || len(rec.Result().Cookies()) != 1 {
			t.Fatalf("scoped SMS login failed for customer: %v", err)
		}
	})

	t.Run("shared usernames stay scoped after reuse and restart", func(t *testing.T) {
		shared := "fixture-account"
		update := model.AccountProfileChanges{Username: &shared}
		if _, err := store.UpdateAccountProfile(ctx, 5, update); err == nil {
			t.Fatal("expected the old global index to remain until cutover")
		}
		exec("ALTER TABLE mgmt_users DROP INDEX uk_mgmt_users_username")
		defer exec("UPDATE mgmt_users SET username='fixture-admin',display_name='fixture-admin' WHERE id=5")
		displayName := "测试昵称"
		update.DisplayName = &displayName
		profile, err := store.UpdateAccountProfile(ctx, 5, update)
		if err != nil || profile.Username != shared || profile.DisplayName != displayName {
			t.Fatalf("cross-type username or nickname did not save: %+v %v", profile, err)
		}
		for _, id := range []int64{3, 6} {
			_, err := store.UpdateAccountProfile(ctx, id, update)
			var duplicate *mysql.MySQLError
			if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
				t.Fatalf("same-type duplicate username accepted: %v", err)
			}
		}
		if err := store.MigrateAuth(ctx); err != nil {
			t.Fatal(err)
		}
		if err := store.EnsureDefaultAdmin(ctx, "fixture-admin", "旧默认昵称", hash(staffPassword)); err != nil {
			t.Fatal(err)
		}
		resolver := NewResolver("production", store)
		tryScoped(t, resolver, appdb.AccountCustomer, shared, fixturePassword, true, 1, nil)
		tryScoped(t, resolver, appdb.AccountStaff, shared, staffPassword, true, 5, nil)
		tryScoped(t, resolver, appdb.AccountStaff, shared, fixturePassword, true, 0, ErrInvalidCredentials)
		try(t, resolver, shared, staffPassword, true, 0, ErrInvalidCredentials)
		try(t, resolver, "fixture-admin", staffPassword, true, 0, ErrInvalidCredentials)
		profile, err = store.GetAccountProfile(ctx, 5)
		if err != nil || profile.DisplayName != displayName {
			t.Fatalf("restart reset the administrator nickname: %+v %v", profile, err)
		}
	})

	t.Run("legacy writes and rollback keep the same account", func(t *testing.T) {
		legacy, err := appdb.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer legacy.Close()

		exec(`INSERT INTO mgmt_users(id, username, password_hash, display_name, phone)
			VALUES (9, 'fixture-new', ?, 'fixture-new', '13800000009')`, hash(fixturePassword))
		try(t, NewResolver("production", store), "13800000009", fixturePassword, true, 9, nil)
		exec(`UPDATE mgmt_users SET role = 'staff' WHERE id = 9`)
		var customerCount int
		if err := fixture.QueryRowContext(ctx, "SELECT COUNT(*) FROM mgmt_customer_accounts WHERE user_id = 9").Scan(&customerCount); err != nil || customerCount != 0 {
			t.Fatalf("former customer record count %d, error %v", customerCount, err)
		}
		if err := legacy.UpdatePassword(ctx, 9, hash(numericPassword)); err != nil {
			t.Fatal(err)
		}
		try(t, NewResolver("production", legacy), "fixture-new", numericPassword, true, 9, nil)
		try(t, NewResolver("production", store), "fixture-new", numericPassword, true, 9, nil)
		try(t, NewResolver("production", store), "fixture-new", fixturePassword, true, 0, ErrInvalidCredentials)

		tx, err := fixture.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(t.Context(), "UPDATE mgmt_users SET role = 'customer' WHERE id = 9"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		user, err := store.GetUserByID(ctx, 9)
		if err != nil || user.Role != "staff" {
			t.Fatalf("rolled-back update changed account type: role %q, error %v", user.Role, err)
		}

		exec(`UPDATE mgmt_users SET status = 'disabled' WHERE id = 6`)
		if _, err := store.ResolveSession(ctx, hashToken(existingToken), time.Now()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("disabled employee session resolved: %v", err)
		}
		exec(`DELETE FROM mgmt_users WHERE id = 9`)
		var staffCount int
		if err := fixture.QueryRowContext(ctx, "SELECT COUNT(*) FROM mgmt_staff_accounts WHERE user_id = 9").Scan(&staffCount); err != nil || staffCount != 0 {
			t.Fatalf("deleted employee record count %d, error %v", staffCount, err)
		}
	})

	t.Run("restart recovers an interrupted split", func(t *testing.T) {
		exec(`DROP TRIGGER mgmt_users_split_accounts_au`)
		exec(`UPDATE mgmt_users SET role = 'customer', tenant_id = 27 WHERE id = 8`)
		if _, err := store.GetUserByID(ctx, 8); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("unreconciled account resolved: %v", err)
		}
		if err := store.MigrateAuth(ctx); err != nil {
			t.Fatal(err)
		}
		user, err := store.GetUserByID(ctx, 8)
		if err != nil || user.Role != "customer" || user.TenantID == nil || *user.TenantID != 27 {
			t.Fatalf("resumed account split lost role or tenant: %v", err)
		}
		var employeeCount int
		if err := fixture.QueryRowContext(ctx, "SELECT COUNT(*) FROM mgmt_staff_accounts WHERE user_id = 8").Scan(&employeeCount); err != nil || employeeCount != 0 {
			t.Fatalf("former staff record count %d, error %v", employeeCount, err)
		}
	})

	t.Run("access denied before issuing session", func(t *testing.T) {
		resolver := NewResolver("production", store)
		resolver.captchas["staff-challenge"] = captchaChallenge{Code: "12345", ExpiresAt: time.Now().Add(time.Minute)}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/workspace/login", nil)
		req.AddCookie(&http.Cookie{Name: captchaCookieName, Value: "staff-challenge"})
		rec := httptest.NewRecorder()
		denied := errors.New("staff access denied")
		actor, err := resolver.AuthenticateWithAccess(ctx, rec, req, LoginInput{
			Method: LoginMethodPassword, Identifier: "fixture-account", Credential: fixturePassword, Captcha: "12345",
		}, func(actor model.Actor) error {
			if actor.UserID != 1 || actor.Role != "customer" {
				t.Fatalf("unexpected authenticated actor: %+v", actor)
			}
			return denied
		})
		if !errors.Is(err, denied) || actor.UserID != 0 || len(rec.Result().Cookies()) != 0 {
			t.Fatal("denied account received a session")
		}
	})

	t.Run("shared failure limit", func(t *testing.T) {
		resolver := NewResolver("production", store)
		for i := 0; i < maxLoginFailures-1; i++ {
			try(t, resolver, "fixture-account", "wrong", true, 0, ErrInvalidCredentials)
		}
		try(t, resolver, "13800000001", "wrong", true, 0, ErrInvalidCredentials)
		try(t, resolver, "+86 13800000001", fixturePassword, true, 0, ErrRateLimited)
		try(t, resolver, "fixture-account", fixturePassword, true, 0, ErrRateLimited)
	})

	t.Run("registration phone availability", func(t *testing.T) {
		exec(`CREATE TABLE IF NOT EXISTS iam_invite_codes (
			code VARCHAR(32) PRIMARY KEY, owner_user_id BIGINT NOT NULL,
			owner_role VARCHAR(32) NOT NULL, max_uses BIGINT DEFAULT 0, used_count BIGINT DEFAULT 0,
			expires_at DATETIME NULL, status VARCHAR(16) NOT NULL
		)`)
		exec(`INSERT INTO iam_invite_codes(code,owner_user_id,owner_role,status) VALUES ('CHECK-TEST',1,'customer','active')`)
		resolver := NewResolver("production", store)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register/phone-availability", nil)
		check := func(phone, invite string, wantAvailable bool, wantErr error) {
			t.Helper()
			available, err := resolver.CheckRegistrationPhone(t.Context(), req, phone, invite)
			if available != wantAvailable || !errors.Is(err, wantErr) {
				t.Fatalf("got availability %t, error %v; want %t, %v", available, err, wantAvailable, wantErr)
			}
		}
		check("13800000001", "CHECK-TEST", false, ErrCaptchaInvalid)
		resolver.captchas["test-challenge"] = captchaChallenge{Code: "12345", ExpiresAt: time.Now().Add(time.Minute)}
		req.AddCookie(&http.Cookie{Name: captchaCookieName, Value: "test-challenge"})
		check("bad", "CHECK-TEST", false, ErrInvalidPhone)
		check("13800000001", "MISSING", false, ErrInvalidInviteCode)
		check("+86 13800000001", "check-test", false, nil)
		check("13800000004", "CHECK-TEST", false, nil)
		check("13800000009", "CHECK-TEST", true, nil)
		check("13800000005", "CHECK-TEST", true, nil)
		for i := 5; i < registrationPhoneChecksPerWindow; i++ {
			check("13800000009", "CHECK-TEST", true, nil)
		}
		check("13800000009", "CHECK-TEST", false, ErrRateLimited)
		if !resolver.verifyCaptcha(req, "12345") {
			t.Fatal("availability checks consumed the registration captcha")
		}
	})
}
