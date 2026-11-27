package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	auditlog "livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

func TestCustomerDeletionGrantHTTPMySQL(t *testing.T) {
	port := os.Getenv("XIAOLAN_CUSTOMER_DELETION_TEST_DB_PORT")
	if port == "" {
		t.Skip("explicit isolated localhost MySQL required")
	}
	if value, err := strconv.Atoi(port); err != nil || value < 1024 || value > 65535 {
		t.Fatal("invalid fixture port")
	}
	cfg := config.Load()
	cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword = "127.0.0.1", port, "root", ""
	dsn := mysql.NewConfig()
	dsn.User, dsn.Net, dsn.Addr, dsn.ParseTime = cfg.DBUser, "tcp", "127.0.0.1:"+port, true
	admin, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("customer_delete_grant_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE " + schema); err != nil {
			t.Error(err)
		}
	}()
	cfg.DBName, dsn.DBName = schema, schema
	conn, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE mgmt_users(id BIGINT PRIMARY KEY,tenant_id BIGINT,username VARCHAR(64),password_hash VARCHAR(255) DEFAULT '',display_name VARCHAR(64) DEFAULT '',avatar_url TEXT,role VARCHAR(32),must_change_password BOOLEAN DEFAULT false,phone VARCHAR(32) DEFAULT '',province VARCHAR(32) DEFAULT '',city VARCHAR(32) DEFAULT '',district VARCHAR(32) DEFAULT '',status VARCHAR(32) DEFAULT 'active',created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_sessions(user_id BIGINT,token_hash VARCHAR(64),expires_at DATETIME(3),last_seen_at DATETIME(3))`)
	exec(`CREATE TABLE staff_groups(id BIGINT PRIMARY KEY,code VARCHAR(64),name VARCHAR(64),status VARCHAR(32))`)
	exec(`CREATE TABLE staff_employees(id BIGINT PRIMARY KEY,user_id BIGINT,primary_group_id BIGINT,employment_status VARCHAR(32))`)
	exec(`CREATE TABLE staff_roles(id BIGINT PRIMARY KEY,code VARCHAR(64),group_id BIGINT,is_group_manager BOOLEAN,status VARCHAR(32))`)
	exec(`CREATE TABLE staff_employee_roles(employee_id BIGINT,role_id BIGINT,scope_type VARCHAR(32))`)
	exec(`CREATE TABLE staff_group_managers(employee_id BIGINT,group_id BIGINT)`)
	exec(`CREATE TABLE staff_permissions(id BIGINT PRIMARY KEY,code VARCHAR(64))`)
	exec(`CREATE TABLE staff_role_permissions(role_id BIGINT,permission_id BIGINT)`)
	exec(`CREATE TABLE sys_feature_records(feature_key VARCHAR(64),record_key VARCHAR(128),status VARCHAR(32))`)
	exec(`INSERT INTO mgmt_users(id,username,role,avatar_url) VALUES (81,'operator','staff',''),(82,'peer','staff',''),(83,'root-admin','platform_admin',''),(84,'customer','customer','')`)
	exec(`INSERT INTO staff_groups VALUES(1,'live_operations','营销运维部','active')`)
	exec(`INSERT INTO staff_employees VALUES(11,81,1,'active'),(12,82,1,'active')`)
	exec(`INSERT INTO staff_roles VALUES(1,'live_operations_manager',1,true,'active')`)
	exec(`INSERT INTO staff_permissions VALUES(1,'liveops.room_quota.view')`)
	exec(`INSERT INTO staff_role_permissions VALUES(1,1)`)
	exec(`INSERT INTO staff_employee_roles VALUES(11,1,'group'),(12,1,'group')`)
	for id := 81; id <= 84; id++ {
		token := fmt.Sprintf("fixture-delete-grant-%d", id)
		exec(`INSERT INTO mgmt_sessions(user_id,token_hash,expires_at) VALUES(?,?,DATE_ADD(NOW(),INTERVAL 1 DAY))`,
			id, fmt.Sprintf("%x", sha256.Sum256([]byte(token))))
	}

	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateCustomerDeletions(t.Context()); err != nil {
		t.Fatal(err)
	}
	audit := auditlog.New("127.0.0.1:1", "", 0, 10, &auditBodyLedger{})
	defer audit.Close()
	server := &Server{store: store, auth: auth.NewResolver("test", store), audit: audit}
	handler := server.Handler()
	call := func(method, path, body string, id, want int) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if id > 0 {
			req.AddCookie(&http.Cookie{Name: "lc_session", Value: fmt.Sprintf("fixture-delete-grant-%d", id)})
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Errorf("%s %s from %d got %d want %d: %s", method, path, id, res.Code, want, res.Body.String())
		}
	}
	checkDenied := func(id, want int) {
		t.Helper()
		call(http.MethodDelete, "/api/v1/admin/customers/999", `{"username":"missing"}`, id, want)
		call(http.MethodGet, "/api/v1/admin/customers/999/deletion", "", id, want)
	}
	checkDenied(0, http.StatusUnauthorized)
	checkDenied(81, http.StatusForbidden)
	exec(`INSERT INTO sys_feature_records VALUES ('staff-customer-delete-grants','11:82','active'),('staff-customer-delete-grants','12:82','inactive')`)
	checkDenied(81, http.StatusForbidden)
	checkDenied(82, http.StatusForbidden)
	exec(`INSERT INTO sys_feature_records VALUES ('staff-customer-delete-grants','11:81','active')`)
	checkDenied(82, http.StatusForbidden)
	checkDenied(84, http.StatusForbidden)
	call(http.MethodDelete, "/api/v1/admin/customers/999", `{}`, 81, http.StatusBadRequest)
	checkDenied(81, http.StatusNotFound)
	checkDenied(83, http.StatusNotFound)
	exec(`UPDATE mgmt_users SET must_change_password=true WHERE id=81`)
	checkDenied(81, http.StatusForbidden)
	exec(`UPDATE mgmt_users SET must_change_password=false WHERE id=81`)
	exec(`UPDATE staff_employees SET employment_status='inactive' WHERE id=11`)
	checkDenied(81, http.StatusForbidden)
	exec(`UPDATE staff_employees SET employment_status='active' WHERE id=11`)
	exec(`UPDATE sys_feature_records SET status='inactive' WHERE record_key='11:81'`)
	checkDenied(81, http.StatusForbidden)
}
