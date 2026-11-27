package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/model"
)

const staffAccountRoles = "('platform_admin', 'staff', 'sales_staff', 'agent_admin')"

type AccountScope string

const (
	AccountAny           AccountScope = ""
	AccountCustomer      AccountScope = "customer"
	AccountStaff         AccountScope = "staff"
	PhoneAccountIndex                 = "uk_mgmt_users_phone_by_type"
	UsernameAccountIndex              = "uk_mgmt_users_username_by_type"
	LegacyUsernameIndex               = "uk_mgmt_users_username"
)

func (s *Store) migrateUsernameAccountScope(ctx context.Context) error {
	exists, err := s.indexExists(ctx, "mgmt_users", UsernameAccountIndex)
	if err != nil || exists {
		return err
	}
	// The deployment removes the global index after the scoped login code is live.
	_, err = s.db.ExecContext(ctx, "CREATE UNIQUE INDEX "+UsernameAccountIndex+
		" ON mgmt_users (username, phone_account_type)")
	if err != nil {
		return fmt.Errorf("enforce username uniqueness within account type: %w", err)
	}
	return nil
}

func (s *Store) migratePhoneAccountScope(ctx context.Context) error {
	for _, column := range []struct{ name, definition string }{
		{"phone_unique_key", "VARCHAR(128) GENERATED ALWAYS AS (NULLIF(TRIM(phone), '')) STORED AFTER phone"},
		{"phone_account_type", "VARCHAR(8) GENERATED ALWAYS AS (IF(role = 'customer', 'customer', 'staff')) STORED"},
	} {
		exists, err := s.columnExists(ctx, "mgmt_users", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE mgmt_users ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return fmt.Errorf("add account phone column %s: %w", column.name, err)
			}
		}
	}
	const legacyIndex = "uk_mgmt_users_phone_unique"
	legacy, err := s.indexExists(ctx, "mgmt_users", legacyIndex)
	if err != nil {
		return err
	}
	scoped, err := s.indexExists(ctx, "mgmt_users", PhoneAccountIndex)
	if err != nil {
		return err
	}
	if !scoped {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE mgmt_users SET phone = TRIM(phone)
			WHERE CAST(phone AS BINARY) <> CAST(TRIM(phone) AS BINARY)
		`); err != nil {
			return fmt.Errorf("normalize account phones: %w", err)
		}
		if !legacy {
			// Pre-index installations already used a marker for duplicates. Keep
			// the marker behavior for same-type duplicates on that upgrade path.
			if _, err := s.db.ExecContext(ctx, `
				UPDATE mgmt_users u
				JOIN (
					SELECT phone_account_type, phone_unique_key, MIN(id) keep_id
					FROM mgmt_users
					WHERE phone_unique_key IS NOT NULL
					GROUP BY phone_account_type, phone_unique_key
					HAVING COUNT(*) > 1
				) dup ON dup.phone_account_type = u.phone_account_type
					AND dup.phone_unique_key = u.phone_unique_key AND u.id <> dup.keep_id
				SET u.phone = CONCAT('DUP-PHONE-', u.id)
			`); err != nil {
				return fmt.Errorf("deduplicate same-type account phones: %w", err)
			}
		}
		if _, err := s.db.ExecContext(ctx, "CREATE UNIQUE INDEX "+PhoneAccountIndex+
			" ON mgmt_users (phone_unique_key, phone_account_type)"); err != nil {
			return fmt.Errorf("enforce phone uniqueness within account type: %w", err)
		}
	}
	// On upgrades, the release removes the legacy index only after the new
	// service and clients pass smoke checks. A restart never recreates it.
	return nil
}

var accountTables = [...]struct {
	name      string
	condition string
}{
	{"mgmt_customer_accounts", "role = 'customer'"},
	{"mgmt_staff_accounts", "role IN " + staffAccountRoles},
}

func (s *Store) migrateAccountTables(ctx context.Context) error {
	for _, account := range accountTables {
		// Keep the identity row and its existing business references during rollout.
		statement := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			user_id BIGINT UNSIGNED NOT NULL,
			tenant_id BIGINT UNSIGNED NULL,
			username VARCHAR(128) NOT NULL,
			phone VARCHAR(128) NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			must_change_password TINYINT(1) NOT NULL,
			role VARCHAR(32) NOT NULL,
			status VARCHAR(32) NOT NULL,
			PRIMARY KEY (user_id),
			FOREIGN KEY (user_id) REFERENCES mgmt_users (id) ON DELETE CASCADE ON UPDATE CASCADE,
			CHECK (%s)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`, account.name, account.condition)
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create %s: %w", account.name, err)
		}
	}

	// Old account writers and the rollback binary still write mgmt_users.
	// Synchronize each mutation inside the writer's own transaction.
	syncAccount := fmt.Sprintf(`
		IF NEW.role = 'customer' THEN
			%s;
		ELSEIF NEW.role IN %s THEN
			%s;
		END IF;`,
		upsertAccount("mgmt_customer_accounts", "NEW", "VALUES ("+accountValues("NEW")+")"),
		staffAccountRoles,
		upsertAccount("mgmt_staff_accounts", "NEW", "VALUES ("+accountValues("NEW")+")"))
	if err := s.ensureAccountTrigger(ctx, "mgmt_users_split_accounts_ai", "INSERT", syncAccount); err != nil {
		return err
	}
	clearFormerAccount := `
		IF OLD.role = 'customer' AND NEW.role <> 'customer' THEN
			DELETE FROM mgmt_customer_accounts WHERE user_id = NEW.id;
		ELSEIF OLD.role IN ` + staffAccountRoles + ` AND NEW.role NOT IN ` + staffAccountRoles + ` THEN
			DELETE FROM mgmt_staff_accounts WHERE user_id = NEW.id;
		END IF;`
	if err := s.ensureAccountTrigger(ctx, "mgmt_users_split_accounts_au", "UPDATE", clearFormerAccount+syncAccount); err != nil {
		return err
	}

	matches, err := accountTablesMatch(ctx, s.db)
	if err != nil {
		return err
	}
	if !matches {
		if err := s.backfillAccountTables(ctx); err != nil {
			return err
		}
	}
	s.accountTablesReady.Store(true)
	return nil
}

func (s *Store) ensureAccountTrigger(ctx context.Context, name, operation, body string) error {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.TRIGGERS
			WHERE TRIGGER_SCHEMA = DATABASE() AND TRIGGER_NAME = ?
		)
	`, name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check account trigger %s: %w", name, err)
	}
	if exists {
		return nil
	}
	statement := fmt.Sprintf("CREATE TRIGGER %s AFTER %s ON mgmt_users FOR EACH ROW BEGIN %s END", name, operation, body)
	_, err = s.db.ExecContext(ctx, statement)
	var mysqlErr *mysql.MySQLError
	if err != nil && !(errors.As(err, &mysqlErr) && mysqlErr.Number == 1359) {
		return fmt.Errorf("create account trigger %s: %w", name, err)
	}
	return nil
}

func (s *Store) backfillAccountTables(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock identities before changing split rows, in the same order as
	// regular account writes. The range lock also covers concurrent inserts.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM mgmt_users FORCE INDEX (PRIMARY) ORDER BY id FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("lock account identities for backfill: %w", err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read locked account identities: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, account := range accountTables {
		// Recover stale memberships if startup previously stopped during DDL.
		statement := fmt.Sprintf(`
			DELETE account FROM %[1]s account
			JOIN mgmt_users u ON u.id = account.user_id
			WHERE NOT (u.%[2]s)
		`, account.name, account.condition)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("reconcile %s membership: %w", account.name, err)
		}
		sourceRows := "SELECT " + accountValues("u") + " FROM mgmt_users u WHERE u." + account.condition
		if _, err := tx.ExecContext(ctx, upsertAccount(account.name, "u", sourceRows)); err != nil {
			return fmt.Errorf("backfill %s: %w", account.name, err)
		}
	}
	matches, err := accountTablesMatch(ctx, tx)
	if err != nil {
		return err
	}
	if !matches {
		return errors.New("account split verification failed; check legacy account roles and split records")
	}
	return tx.Commit()
}

func accountValues(source string) string {
	return fmt.Sprintf("%[1]s.id, %[1]s.tenant_id, %[1]s.username, %[1]s.phone, %[1]s.password_hash, %[1]s.must_change_password, %[1]s.role, %[1]s.status", source)
}

func upsertAccount(table, source, rows string) string {
	return fmt.Sprintf(`INSERT INTO %[1]s (user_id, tenant_id, username, phone, password_hash, must_change_password, role, status)
		%[3]s
		ON DUPLICATE KEY UPDATE
			tenant_id = %[2]s.tenant_id,
			username = %[2]s.username,
			phone = %[2]s.phone,
			password_hash = %[2]s.password_hash,
			must_change_password = %[2]s.must_change_password,
			role = %[2]s.role,
			status = %[2]s.status`, table, source, rows)
}

type accountRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const splitAccountJoins = `
	LEFT JOIN mgmt_customer_accounts c ON c.user_id = u.id
	LEFT JOIN mgmt_staff_accounts e ON e.user_id = u.id
`

func accountTablesMatch(ctx context.Context, q accountRowQuerier) (bool, error) {
	var invalid bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM mgmt_users u
	`+splitAccountJoins+`
			WHERE NOT (
				(u.role = 'customer' AND c.user_id IS NOT NULL AND e.user_id IS NULL)
				OR (u.role IN `+staffAccountRoles+` AND c.user_id IS NULL AND e.user_id IS NOT NULL)
			)
			OR NOT (COALESCE(c.tenant_id, e.tenant_id) <=> u.tenant_id)
			OR CAST(COALESCE(c.username, e.username) AS BINARY) <> CAST(u.username AS BINARY)
			OR CAST(COALESCE(c.phone, e.phone) AS BINARY) <> CAST(u.phone AS BINARY)
			OR CAST(COALESCE(c.password_hash, e.password_hash) AS BINARY) <> CAST(u.password_hash AS BINARY)
			OR COALESCE(c.must_change_password, e.must_change_password) <> u.must_change_password
			OR CAST(COALESCE(c.role, e.role) AS BINARY) <> CAST(u.role AS BINARY)
			OR CAST(COALESCE(c.status, e.status) AS BINARY) <> CAST(u.status AS BINARY)
		)
	`).Scan(&invalid)
	if err != nil {
		return false, fmt.Errorf("verify account split: %w", err)
	}
	return !invalid, nil
}

func (s *Store) queryAccount(ctx context.Context, predicate string, activeOnly bool, args ...any) (model.User, error) {
	columns := "u.id, u.tenant_id, u.username, u.password_hash, u.display_name, u.avatar_url, u.role, u.must_change_password, u.phone, u.province, u.city, u.district, u.status, u.created_at"
	source := "FROM mgmt_users u"
	status := "u.status"
	if s.accountTablesReady.Load() {
		columns = `u.id, COALESCE(c.tenant_id, e.tenant_id), COALESCE(c.username, e.username),
			COALESCE(c.password_hash, e.password_hash), u.display_name, u.avatar_url, u.role,
			COALESCE(c.must_change_password, e.must_change_password), COALESCE(c.phone, e.phone),
			u.province, u.city, u.district, COALESCE(c.status, e.status), u.created_at`
		source += splitAccountJoins
		predicate += ` AND (
			(u.role = c.role AND e.user_id IS NULL) OR (u.role = e.role AND c.user_id IS NULL)
		)`
		status = "COALESCE(c.status, e.status)"
	}
	if activeOnly {
		predicate += " AND " + status + " = 'active'"
	}
	return s.queryUser(ctx, "SELECT "+columns+" "+source+" WHERE "+predicate+" LIMIT 1", args...)
}

func (s *Store) GetUserByScopedUsername(ctx context.Context, username string, scope AccountScope) (model.User, error) {
	if scope == AccountAny {
		return s.GetUserByUsername(ctx, username)
	}
	return s.queryScopedAccount(ctx, scope, "u.username = ?", strings.TrimSpace(username))
}

func (s *Store) queryScopedAccount(ctx context.Context, scope AccountScope, predicate string, identifier string) (model.User, error) {
	var table string
	switch scope {
	case AccountCustomer:
		table = "mgmt_customer_accounts"
	case AccountStaff:
		table = "mgmt_staff_accounts"
	default:
		return model.User{}, fmt.Errorf("unsupported account scope %q", scope)
	}
	return s.queryUser(ctx, `
		SELECT a.user_id, a.tenant_id, a.username, a.password_hash, u.display_name, u.avatar_url,
			a.role, a.must_change_password, a.phone, u.province, u.city, u.district, a.status, u.created_at
		FROM mgmt_users u
		JOIN `+table+` a ON a.user_id = u.id AND a.role = u.role
		WHERE `+predicate+` LIMIT 1
	`, identifier)
}
