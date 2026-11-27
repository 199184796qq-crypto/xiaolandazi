package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

var ErrWechatLoginConflict = errors.New("wechat login identity is already linked")
var ErrWechatPairingCode = errors.New("wechat pairing code is incorrect")
var ErrWechatOriginalRequired = errors.New("original linked WeChat is required")
var ErrWechatPasswordLoginRequired = errors.New("password change requires WeChat login")
var ErrWechatPasswordChanged = errors.New("current password changed")

type WechatLoginFlow struct {
	Purpose      string
	AppID        string
	UserID       int64
	BrowserHash  string
	Target       string
	IdentityHash string
	PairingHash  string
	Attempts     int
}

func (s *Store) MigrateWechatLogin(ctx context.Context) error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS mgmt_wechat_login_identities (
            app_id VARBINARY(64) NOT NULL,
            identity_hash VARBINARY(64) NOT NULL,
            user_id BIGINT NOT NULL,
            PRIMARY KEY (app_id, identity_hash),
            UNIQUE KEY one_wechat_per_account (app_id, user_id)
        ) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS mgmt_wechat_login_flows (
            token_hash VARBINARY(64) NOT NULL PRIMARY KEY,
            purpose VARCHAR(16) NOT NULL,
            app_id VARBINARY(64) NOT NULL,
            user_id BIGINT NOT NULL,
            browser_hash VARBINARY(64) NOT NULL,
            target VARCHAR(255) NOT NULL,
            identity_hash VARBINARY(64) NOT NULL DEFAULT '',
            pairing_hash VARBINARY(64) NOT NULL DEFAULT '',
            attempts INT NOT NULL DEFAULT 0,
            expires_at DATETIME(6) NOT NULL,
            KEY wechat_login_flow_expiration (expires_at)
        ) ENGINE=InnoDB`,
	} {
		if _, err := s.db.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	for _, column := range []string{"wechat_login_app_id", "wechat_login_identity_hash"} {
		exists, err := s.columnExists(ctx, "mgmt_sessions", column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE mgmt_sessions ADD COLUMN "+column+" VARBINARY(64) NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) CreateWechatLoginFlow(ctx context.Context, tokenHash string, flow WechatLoginFlow, expires time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mgmt_wechat_login_flows WHERE expires_at <= UTC_TIMESTAMP(6) LIMIT 50`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mgmt_wechat_login_flows
        (token_hash, purpose, app_id, user_id, browser_hash, target, expires_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, tokenHash, flow.Purpose, flow.AppID, flow.UserID,
		flow.BrowserHash, flow.Target, expires.UTC())
	return err
}

func scanWechatLoginFlow(row *sql.Row) (WechatLoginFlow, error) {
	var f WechatLoginFlow
	err := row.Scan(&f.Purpose, &f.AppID, &f.UserID, &f.BrowserHash, &f.Target,
		&f.IdentityHash, &f.PairingHash, &f.Attempts)
	return f, err
}

const wechatLoginFlowColumns = `purpose, app_id, user_id, browser_hash, target, identity_hash, pairing_hash, attempts`

func (s *Store) ConsumeWechatLoginFlow(ctx context.Context, tokenHash, browserHash string) (WechatLoginFlow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WechatLoginFlow{}, err
	}
	defer tx.Rollback()
	flow, err := scanWechatLoginFlow(tx.QueryRowContext(ctx, `SELECT `+wechatLoginFlowColumns+`
        FROM mgmt_wechat_login_flows WHERE token_hash = ? AND browser_hash = ?
        AND purpose IN ('login','mobile','mobile_unbind','scan') AND expires_at > UTC_TIMESTAMP(6) FOR UPDATE`, tokenHash, browserHash))
	if err != nil {
		return WechatLoginFlow{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mgmt_wechat_login_flows WHERE token_hash = ?`, tokenHash); err != nil {
		return WechatLoginFlow{}, err
	}
	return flow, tx.Commit()
}

func (s *Store) WechatDesktopFlowExists(ctx context.Context, tokenHash, appID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM mgmt_wechat_login_flows
        WHERE token_hash = ? AND purpose IN ('desktop','desktop_unbind') AND app_id = ?
        AND identity_hash = '' AND expires_at > UTC_TIMESTAMP(6))`, tokenHash, appID).Scan(&n)
	return n == 1, err
}

func (s *Store) SetWechatDesktopCandidate(ctx context.Context, tokenHash, appID, identityHash, pairingHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mgmt_wechat_login_flows AS f SET identity_hash = ?, pairing_hash = ?
        WHERE f.token_hash = ? AND f.app_id = ?
        AND f.identity_hash = '' AND f.expires_at > UTC_TIMESTAMP(6)
        AND (f.purpose = 'desktop' OR (f.purpose = 'desktop_unbind' AND EXISTS (
            SELECT 1 FROM mgmt_wechat_login_identities i
            WHERE i.app_id = f.app_id AND i.user_id = f.user_id AND i.identity_hash = ?)))`, identityHash, pairingHash, tokenHash, appID, identityHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) ConfirmWechatDesktopFlow(ctx context.Context, tokenHash, appID, sessionHash, pairingHash string, userID int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := lockWechatBindingSession(ctx, tx, sessionHash, userID); err != nil {
		return false, err
	}
	flow, err := scanWechatLoginFlow(tx.QueryRowContext(ctx, `SELECT `+wechatLoginFlowColumns+`
        FROM mgmt_wechat_login_flows WHERE token_hash = ? AND purpose IN ('desktop','desktop_unbind') AND app_id = ?
        AND user_id = ? AND browser_hash = ? AND identity_hash <> '' AND attempts < 5
        AND expires_at > UTC_TIMESTAMP(6) FOR UPDATE`, tokenHash, appID, userID, sessionHash))
	if err != nil {
		return false, err
	}
	if flow.PairingHash != pairingHash {
		if _, err := tx.ExecContext(ctx, `UPDATE mgmt_wechat_login_flows SET attempts = attempts + 1 WHERE token_hash = ?`, tokenHash); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, ErrWechatPairingCode
	}
	bound := flow.Purpose == "desktop"
	if err := changeWechatBinding(ctx, tx, appID, flow.IdentityHash, userID, !bound); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mgmt_wechat_login_flows WHERE token_hash = ?`, tokenHash); err != nil {
		return false, err
	}
	return bound, tx.Commit()
}

func (s *Store) UpdateWechatBinding(ctx context.Context, appID, identityHash, sessionHash string, userID int64, unlink bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockWechatBindingSession(ctx, tx, sessionHash, userID); err != nil {
		return err
	}
	if err := changeWechatBinding(ctx, tx, appID, identityHash, userID, unlink); err != nil {
		return err
	}
	return tx.Commit()
}

func lockWechatBindingSession(ctx context.Context, tx *sql.Tx, sessionHash string, userID int64) error {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_users WHERE id = ?
        AND role = 'customer' AND tenant_id IS NOT NULL AND status = 'active' AND must_change_password = 0 FOR UPDATE`,
		userID).Scan(&id); err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, `SELECT user_id FROM mgmt_sessions WHERE token_hash = ?
        AND user_id = ? AND expires_at > UTC_TIMESTAMP(6) FOR UPDATE`, sessionHash, userID).Scan(&id)
}

func changeWechatBinding(ctx context.Context, tx *sql.Tx, appID, identityHash string, userID int64, unlink bool) error {
	if unlink {
		result, err := tx.ExecContext(ctx, `DELETE FROM mgmt_wechat_login_identities
            WHERE app_id = ? AND identity_hash = ? AND user_id = ?`, appID, identityHash, userID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrWechatOriginalRequired
		}
		// Older sessions have no reliable login-method marker.
		_, err = tx.ExecContext(ctx, `DELETE FROM mgmt_sessions WHERE user_id = ?`, userID)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO mgmt_wechat_login_identities
        (app_id, identity_hash, user_id) VALUES (?, ?, ?)`, appID, identityHash, userID)
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		return err
	}
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM mgmt_wechat_login_identities
        WHERE app_id = ? AND identity_hash = ? FOR UPDATE`, appID, identityHash).Scan(&existing)
	if err == nil && existing == userID {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return ErrWechatLoginConflict
}

// WechatAccountSecurity returns whether the account is bound and whether the
// current session came from an authorization for the current binding.
func (s *Store) WechatAccountSecurity(ctx context.Context, appID, sessionHash string, userID int64) (bound, verified bool, err error) {
	var matches bool
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(s.wechat_login_identity_hash = i.identity_hash,0)
        FROM mgmt_wechat_login_identities i
        LEFT JOIN mgmt_sessions s ON s.user_id = i.user_id AND s.token_hash = ?
            AND s.wechat_login_app_id = i.app_id AND s.expires_at > UTC_TIMESTAMP(6)
        WHERE i.app_id = ? AND i.user_id = ?`, sessionHash, appID, userID).Scan(&matches)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, matches, err
}

type NewLoginSession struct {
	TokenHash string
	ExpiresAt time.Time
	ClientIP  string
	UserAgent string
}

func storeWechatSession(ctx context.Context, tx *sql.Tx, userID int64, session NewLoginSession, appID, identityHash string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO mgmt_sessions
        (user_id, token_hash, expires_at, client_ip, user_agent, wechat_login_app_id, wechat_login_identity_hash)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, session.TokenHash, session.ExpiresAt.UTC(), session.ClientIP, session.UserAgent, appID, identityHash)
	return err
}

func (s *Store) CreateWechatSession(ctx context.Context, userID int64, appID, identityHash string, session NewLoginSession) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_users WHERE id = ?
        AND role = 'customer' AND tenant_id IS NOT NULL AND status = 'active' AND must_change_password = 0 FOR UPDATE`,
		userID).Scan(&id); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM mgmt_wechat_login_identities
        WHERE app_id = ? AND identity_hash = ? AND user_id = ? FOR UPDATE`, appID, identityHash, userID).Scan(&id); err != nil {
		return err
	}
	if err := storeWechatSession(ctx, tx, userID, session, appID, identityHash); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateWechatProtectedPassword serializes protection checks with linking and unlinking.
func (s *Store) UpdateWechatProtectedPassword(ctx context.Context, userID int64, appID, currentSession, oldHash, newHash string, replacement NewLoginSession) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var passwordHash string
	if err := tx.QueryRowContext(ctx, `SELECT password_hash FROM mgmt_users
        WHERE id = ? AND status = 'active' FOR UPDATE`, userID).Scan(&passwordHash); err != nil {
		return err
	}
	if oldHash != passwordHash {
		return ErrWechatPasswordChanged
	}

	var currentApp, currentIdentity string
	if err := tx.QueryRowContext(ctx, `SELECT wechat_login_app_id, wechat_login_identity_hash FROM mgmt_sessions
        WHERE user_id = ? AND token_hash = ? AND expires_at > UTC_TIMESTAMP(6) FOR UPDATE`,
		userID, currentSession).Scan(&currentApp, &currentIdentity); err != nil {
		return err
	}

	var linkedIdentity string
	err = tx.QueryRowContext(ctx, `SELECT identity_hash FROM mgmt_wechat_login_identities
        WHERE app_id = ? AND user_id = ? FOR UPDATE`, appID, userID).Scan(&linkedIdentity)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if linkedIdentity != "" && (currentApp != appID || currentIdentity != linkedIdentity) {
		return ErrWechatPasswordLoginRequired
	}

	if _, err := tx.ExecContext(ctx, `UPDATE mgmt_users SET password_hash = ?, must_change_password = 0 WHERE id = ?`, newHash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mgmt_sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if linkedIdentity == "" {
		appID = ""
	}
	if err := storeWechatSession(ctx, tx, userID, replacement, appID, linkedIdentity); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) HasWechatLogin(ctx context.Context, appID string, userID int64) (bool, error) {
	var bound int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM mgmt_wechat_login_identities
        WHERE app_id = ? AND user_id = ?)`, appID, userID).Scan(&bound)
	return bound == 1, err
}

func (s *Store) WechatLoginUser(ctx context.Context, appID, identityHash string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM mgmt_wechat_login_identities
        WHERE app_id = ? AND identity_hash = ?`, appID, identityHash).Scan(&userID)
	return userID, err
}
