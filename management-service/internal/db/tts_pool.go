package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/ttsscheduler"
)

func (s *Store) MigrateTTSPool(ctx context.Context, apiKey string) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS mgmt_tts_pool_lock(id TINYINT PRIMARY KEY) ENGINE=InnoDB`,
		`INSERT IGNORE INTO mgmt_tts_pool_lock(id) VALUES(1)`,
		`CREATE TABLE IF NOT EXISTS mgmt_tts_pool_accounts (
   id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,name VARCHAR(80) NOT NULL,
   account_uid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
   provider VARCHAR(32) NOT NULL,credential_source VARCHAR(32) NOT NULL,
   sealed_key BLOB NULL,key_fingerprint CHAR(64) NOT NULL,voices_json JSON NOT NULL,
   configured BOOLEAN NOT NULL,enabled BOOLEAN NOT NULL DEFAULT TRUE,
   rpm INT NOT NULL DEFAULT 180,rps INT NOT NULL DEFAULT 3,revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
   updated_by_user_id BIGINT NOT NULL DEFAULT 0,updated_at DATETIME(6) NOT NULL,
   UNIQUE KEY uk_tts_account_uid(account_uid),UNIQUE KEY uk_tts_key(key_fingerprint)
  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS mgmt_tts_pool_jobs (
   seq BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
   account_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
   model VARCHAR(160) NOT NULL,provider VARCHAR(32) NOT NULL,voice_id VARCHAR(200) NOT NULL,
   tenant_id BIGINT NOT NULL DEFAULT 0,room_id BIGINT NOT NULL DEFAULT 0,session_id BIGINT NOT NULL DEFAULT 0,
   status VARCHAR(16) NOT NULL,queued_at DATETIME(6) NOT NULL,expires_at DATETIME(6) NOT NULL,
   heartbeat_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
   admitted_at DATETIME(6) NULL,finished_at DATETIME(6) NULL,
   UNIQUE KEY uk_tts_pool_job(id),KEY idx_tts_pool_fifo(status,seq),
   KEY idx_tts_pool_window(account_id,admitted_at),KEY idx_tts_pool_expiry(expires_at)
  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("创建TTS账户池: %w", err)
		}
	}
	if err := s.upgradeTTSPoolSchema(ctx); err != nil {
		return fmt.Errorf("升级TTS账户池: %w", err)
	}
	fingerprint := "current-environment"
	if key := strings.TrimSpace(apiKey); key != "" {
		fingerprint = modelPoolFingerprint(key)
	}
	// Restart preserves pauses, edited limits, and all current quota timestamps.
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO mgmt_tts_pool_accounts(id,name,account_uid,provider,credential_source,key_fingerprint,voices_json,configured,enabled,rpm,rps,updated_at)
 VALUES(?,'当前阿里TTS账号','current','aliyun','current',?,JSON_OBJECT(),?,TRUE,180,3,UTC_TIMESTAMP(6))
 `, ttsscheduler.CurrentAccountID, fingerprint, strings.TrimSpace(apiKey) != "")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_accounts SET configured=?,key_fingerprint=? WHERE id=?`, strings.TrimSpace(apiKey) != "", fingerprint, ttsscheduler.CurrentAccountID)
	return err
}

// Older pool previews stored only the current environment account. Add fields
// in place: pauses, quota history, revisions and existing job receipts survive.
func (s *Store) upgradeTTSPoolSchema(ctx context.Context) error {
	for _, field := range []struct{ table, name, definition string }{
		{"mgmt_tts_pool_accounts", "account_uid", "VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT ''"},
		{"mgmt_tts_pool_accounts", "sealed_key", "BLOB NULL"},
		{"mgmt_tts_pool_accounts", "key_fingerprint", "CHAR(64) NOT NULL DEFAULT ''"},
		{"mgmt_tts_pool_accounts", "voices_json", "JSON NULL"},
		{"mgmt_tts_pool_jobs", "provider", "VARCHAR(32) NOT NULL DEFAULT 'aliyun'"},
		{"mgmt_tts_pool_jobs", "voice_id", "VARCHAR(200) NOT NULL DEFAULT ''"},
		{"mgmt_tts_pool_jobs", "heartbeat_at", "DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)"},
	} {
		exists, err := s.columnExists(ctx, field.table, field.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+field.table+" ADD COLUMN "+field.name+" "+field.definition); err != nil {
				return err
			}
		}
	}
	for _, statement := range []string{
		`UPDATE mgmt_tts_pool_accounts SET account_uid=CASE WHEN id='current-aliyun-tts' THEN 'current' ELSE CONCAT('legacy:',id) END WHERE account_uid=''`,
		`UPDATE mgmt_tts_pool_accounts SET key_fingerprint=SHA2(CONCAT('legacy:',id),256) WHERE key_fingerprint=''`,
		`UPDATE mgmt_tts_pool_accounts SET voices_json=JSON_OBJECT() WHERE voices_json IS NULL`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var nullable string
	if err := s.db.QueryRowContext(ctx, `SELECT IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='mgmt_tts_pool_accounts' AND COLUMN_NAME='voices_json'`).Scan(&nullable); err != nil {
		return err
	}
	if nullable == "YES" {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE mgmt_tts_pool_accounts MODIFY COLUMN voices_json JSON NOT NULL`); err != nil {
			return err
		}
	}
	for _, index := range []struct{ name, definition string }{
		{"uk_tts_account_uid", "UNIQUE KEY uk_tts_account_uid(account_uid)"},
		{"uk_tts_key", "UNIQUE KEY uk_tts_key(key_fingerprint)"},
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='mgmt_tts_pool_accounts' AND INDEX_NAME=?`, index.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE mgmt_tts_pool_accounts ADD "+index.definition); err != nil {
				return err
			}
		}
	}
	return nil
}
func lockTTSPool(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var id int
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tts_pool_lock WHERE id=1 FOR UPDATE`).Scan(&id); err != nil {
		return now, err
	}
	err := tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&now)
	return now, err
}
func validateTTSAccount(a *ttsscheduler.AccountInput) error {
	a.Name = strings.TrimSpace(a.Name)
	a.NewAPIKey = strings.TrimSpace(a.NewAPIKey)
	a.AccountUID = strings.TrimSpace(a.AccountUID)
	if a.Name == "" || utf8.RuneCountInString(a.Name) > 80 || strings.IndexFunc(a.Name, unicode.IsControl) >= 0 {
		return errors.New("账户名称须为1—80个字")
	}
	if a.RPM < 1 || a.RPM > 180 || a.RPS < 1 || a.RPS > 3 {
		return errors.New("每个账号每分钟须为1—180次、每秒须为1—3次")
	}
	if len(a.AccountUID) < 1 || len(a.AccountUID) > 80 || strings.IndexFunc(a.AccountUID, func(r rune) bool { return r > 127 || unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("请填写阿里云主账号UID，相同主账号只能登记一次")
	}
	if a.ID != "" && (a.Revision == 0 || len(a.ID) > 64) {
		return ttsscheduler.ErrRevision
	}
	if len(a.NewAPIKey) > 4096 || strings.IndexFunc(a.NewAPIKey, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("API Key格式错误")
	}
	if a.ID == ttsscheduler.CurrentAccountID && a.NewAPIKey != "" {
		return errors.New("当前账号凭据由服务器环境管理")
	}
	if a.ID == "" && a.NewAPIKey == "" {
		return errors.New("新账号需要API Key")
	}
	if len(a.Voices) > 200 {
		return errors.New("音色对应配置过多")
	}
	for source, target := range a.Voices {
		if !ttsVoiceToken(source) || !ttsVoiceToken(target) || !strings.Contains(source, "/") {
			return errors.New("音色编号格式错误")
		}
	}
	if a.ID != ttsscheduler.CurrentAccountID && a.Enabled && len(a.Voices) == 0 {
		return errors.New("备用账号启用前需要配置目标模型及音色对应关系")
	}
	return nil
}

// Mapping keys include the exact model: cloned voices cannot move across models.
func ttsVoiceToken(s string) bool {
	return len(s) > 0 && len(s) <= 400 && strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
func (s *Store) SaveTTSAccount(ctx context.Context, a ttsscheduler.AccountInput, actor int64) error {
	if err := validateTTSAccount(&a); err != nil {
		if errors.Is(err, ttsscheduler.ErrRevision) {
			return err
		}
		return fmt.Errorf("%w：%v", ttsscheduler.ErrConfig, err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, err := lockTTSPool(ctx, tx)
	if err != nil {
		return err
	}
	var sealed []byte
	fingerprint := ""
	if a.ID == "" {
		var currentUID string
		if err = tx.QueryRowContext(ctx, `SELECT account_uid FROM mgmt_tts_pool_accounts WHERE id=?`, ttsscheduler.CurrentAccountID).Scan(&currentUID); err != nil {
			return err
		}
		if currentUID == "current" {
			return fmt.Errorf("%w：添加备用账号前，请先补充当前账号的阿里云主账号UID", ttsscheduler.ErrConfig)
		}
		var raw [16]byte
		if _, err = rand.Read(raw[:]); err != nil {
			return err
		}
		a.ID = "tts-account-" + hex.EncodeToString(raw[:])
	} else {
		var revision uint64
		var oldUID string
		if err = tx.QueryRowContext(ctx, `SELECT sealed_key,key_fingerprint,revision,account_uid FROM mgmt_tts_pool_accounts WHERE id=?`, a.ID).Scan(&sealed, &fingerprint, &revision, &oldUID); err != nil {
			return ttsscheduler.ErrRevision
		}
		if revision != a.Revision {
			return ttsscheduler.ErrRevision
		}
		if oldUID != "current" && oldUID != a.AccountUID {
			return fmt.Errorf("%w：已登记的主账号UID不能更换，请停用后添加其他独立账号", ttsscheduler.ErrConfig)
		}
	}
	if a.NewAPIKey != "" {
		sealed, err = agentgateway.SealSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), "tts-pool:"+a.ID, a.NewAPIKey)
		if err != nil {
			return fmt.Errorf("%w：服务器凭据加密配置不可用，请联系系统管理员", ttsscheduler.ErrConfig)
		}
		fingerprint = modelPoolFingerprint(a.NewAPIKey)
	}
	var duplicate int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_accounts WHERE id<>? AND (account_uid=? OR key_fingerprint=?)`, a.ID, a.AccountUID, fingerprint).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return fmt.Errorf("%w：该主账号或API Key已在账户池中，不能重复增加额度", ttsscheduler.ErrConfig)
	}
	if a.Voices == nil {
		a.Voices = map[string]string{}
	}
	voices, _ := json.Marshal(a.Voices)
	if a.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_tts_pool_accounts(id,name,account_uid,provider,credential_source,sealed_key,key_fingerprint,voices_json,configured,enabled,rpm,rps,updated_by_user_id,updated_at) VALUES(?,?,?,'aliyun','encrypted',?,?,?,TRUE,?,?,?,?,?)`, a.ID, a.Name, a.AccountUID, sealed, fingerprint, voices, a.Enabled, a.RPM, a.RPS, actor, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE mgmt_tts_pool_accounts SET name=?,account_uid=?,sealed_key=?,key_fingerprint=?,voices_json=?,enabled=?,rpm=?,rps=?,revision=revision+1,updated_by_user_id=?,updated_at=? WHERE id=? AND revision=?`, a.Name, a.AccountUID, sealed, fingerprint, voices, a.Enabled, a.RPM, a.RPS, actor, now, a.ID, a.Revision)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func expireTTSJobs(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET status='cancelled',finished_at=? WHERE (status IN ('queued','running') AND expires_at<=?) OR (status='queued' AND heartbeat_at<?)`, now, now, now.Add(-10*time.Second))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs j LEFT JOIN live_runtime_sessions r ON r.id=j.session_id
	 SET j.status='cancelled',j.finished_at=? WHERE j.status='queued' AND j.session_id>0
	 AND (r.id IS NULL OR r.tenant_id<>j.tenant_id OR r.room_id<>j.room_id OR r.status NOT IN ('running','paused'))`, now)
	return err
}
func (s *Store) EnqueueTTS(ctx context.Context, w ttsscheduler.Work) error {
	if w.TTL <= 0 || w.ID == "" || w.Model == "" || len(w.ID) > 64 || len(w.Model) > 160 || len(w.VoiceID) > 200 || len(w.Provider) > 32 {
		return ttsscheduler.ErrExpired
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, err := lockTTSPool(ctx, tx)
	if err != nil {
		return err
	}
	if err = expireTTSJobs(ctx, tx, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mgmt_tts_pool_jobs WHERE status NOT IN ('queued','running') AND queued_at<? AND (admitted_at IS NULL OR admitted_at<?) LIMIT 1000`, now.Add(-10*time.Minute), now.Add(-time.Minute)); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_jobs WHERE status='queued'`).Scan(&count); err != nil {
		return err
	}
	if count >= ttsscheduler.MaxQueue {
		return ttsscheduler.ErrQueueFull
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_tts_pool_jobs(id,model,provider,voice_id,tenant_id,room_id,session_id,status,queued_at,expires_at,heartbeat_at) VALUES(?,?,?,?,?,?,?,'queued',?,?,?)`, w.ID, w.Model, w.Provider, w.VoiceID, w.TenantID, w.RoomID, w.SessionID, now, now.Add(min(w.TTL, 5*time.Minute)), now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type ttsPoolAccount struct {
	ttsscheduler.Account
	sealed []byte
}

func ttsAccountsTx(ctx context.Context, tx *sql.Tx) (out []ttsPoolAccount, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,name,account_uid,provider,credential_source,configured,enabled,rpm,rps,revision,voices_json,sealed_key FROM mgmt_tts_pool_accounts ORDER BY (id='current-aliyun-tts') DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a ttsPoolAccount
		var voices []byte
		if err = rows.Scan(&a.ID, &a.Name, &a.AccountUID, &a.Provider, &a.CredentialSource, &a.Configured, &a.Enabled, &a.RPM, &a.RPS, &a.Revision, &voices, &a.sealed); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(voices, &a.Voices); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func ttsMappedVoice(a ttsPoolAccount, model, voice string) (string, bool) {
	if !a.Enabled || !a.Configured {
		return "", false
	}
	if a.ID == ttsscheduler.CurrentAccountID {
		return voice, true
	}
	v, ok := a.Voices[model+"/"+voice]
	return v, ok && v != ""
}
func (s *Store) ClaimTTS(ctx context.Context, id string) (out ttsscheduler.Claim, err error) {
	out.Wait = 250 * time.Millisecond
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return
	}
	defer tx.Rollback()
	now, err := lockTTSPool(ctx, tx)
	if err != nil {
		return out, err
	}
	if err = expireTTSJobs(ctx, tx, now); err != nil {
		return out, err
	}
	var status, model, voice string
	var seq uint64
	if err = tx.QueryRowContext(ctx, `SELECT status,model,voice_id,seq FROM mgmt_tts_pool_jobs WHERE id=?`, id).Scan(&status, &model, &voice, &seq); err != nil {
		return out, err
	}
	if status != "queued" {
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return out, ttsscheduler.ErrExpired
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET heartbeat_at=? WHERE id=? AND status='queued'`, now, id); err != nil {
		return out, err
	}
	var paused int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_jobs j JOIN live_runtime_sessions r ON r.id=j.session_id WHERE j.id=? AND r.status='paused'`, id).Scan(&paused); err != nil {
		return out, err
	}
	if paused > 0 {
		return out, tx.Commit()
	}
	accounts, err := ttsAccountsTx(ctx, tx)
	if err != nil {
		return out, err
	}
	// FIFO per compatible account. An unavailable voice cannot block other rooms.
	type earlier struct{ model, voice string }
	var prior []earlier
	rows, err := tx.QueryContext(ctx, `SELECT j.model,j.voice_id FROM mgmt_tts_pool_jobs j LEFT JOIN live_runtime_sessions r ON r.id=j.session_id WHERE j.status='queued' AND j.seq<? AND (j.session_id=0 OR r.status='running') ORDER BY j.seq`, seq)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p earlier
		if err = rows.Scan(&p.model, &p.voice); err != nil {
			rows.Close()
			return out, err
		}
		prior = append(prior, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, a := range accounts {
		mapped, ok := ttsMappedVoice(a, model, voice)
		if !ok {
			continue
		}
		blocked := false
		for _, p := range prior {
			if _, ok := ttsMappedVoice(a, p.model, p.voice); ok {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		rows, err = tx.QueryContext(ctx, `SELECT admitted_at FROM mgmt_tts_pool_jobs WHERE account_id=? AND admitted_at>? ORDER BY admitted_at`, a.ID, now.Add(-time.Minute))
		if err != nil {
			return out, err
		}
		var admitted []time.Time
		for rows.Next() {
			var at time.Time
			if err = rows.Scan(&at); err != nil {
				rows.Close()
				return out, err
			}
			admitted = append(admitted, at)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if wait := ttsscheduler.WaitForQuota(now, admitted, a.RPM, a.RPS); wait > 0 {
			out.Wait = min(out.Wait, wait)
			continue
		}
		credential := ttsscheduler.Credential{AccountID: a.ID, VoiceID: mapped}
		if a.CredentialSource != "current" {
			credential.APIKey, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), "tts-pool:"+a.ID, a.sealed)
			if err != nil {
				continue
			}
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		result, e := tx.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET status='running',account_id=?,admitted_at=?,expires_at=? WHERE id=? AND status='queued'`, a.ID, now, now.Add(2*time.Minute), id)
		if e != nil {
			return out, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return out, e
		}
		if n != 1 {
			return out, ttsscheduler.ErrExpired
		}
		if err = tx.Commit(); err != nil {
			return out, err
		}
		out.Granted = true
		out.Credential = credential
		return out, nil
	}
	return out, tx.Commit()
}
func (s *Store) FinishTTS(ctx context.Context, id, status string) error {
	if status != "completed" && status != "failed" && status != "cancelled" {
		return errors.New("TTS请求状态无效")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET status=?,finished_at=UTC_TIMESTAMP(6) WHERE id=? AND status IN ('queued','running')`, status, id)
	return err
}
func (s *Store) TTSPoolSnapshot(ctx context.Context) (out ttsscheduler.Snapshot, err error) {
	out.Accounts = []ttsscheduler.Account{}
	out.Requests = []ttsscheduler.Queued{}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&out.ObservedAt); err != nil {
		return
	}
	accounts, err := ttsAccountsTx(ctx, tx)
	if err != nil {
		return out, err
	}
	for _, a := range accounts {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(admitted_at>?),0),COALESCE(SUM(admitted_at>?),0),COALESCE(SUM(status='running' AND expires_at>?),0) FROM mgmt_tts_pool_jobs WHERE account_id=?`, out.ObservedAt.Add(-time.Minute), out.ObservedAt.Add(-time.Second), out.ObservedAt, a.ID).Scan(&a.UsedMinute, &a.UsedSecond, &a.InFlight); err != nil {
			return out, err
		}
		out.Accounts = append(out.Accounts, a.Account)
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_jobs WHERE status='queued' AND expires_at>?`, out.ObservedAt).Scan(&out.Queued); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,model,provider,voice_id,tenant_id,room_id,session_id,queued_at,expires_at FROM mgmt_tts_pool_jobs WHERE status='queued' AND expires_at>? ORDER BY seq LIMIT 50`, out.ObservedAt)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var q ttsscheduler.Queued
		if err = rows.Scan(&q.ID, &q.Model, &q.Provider, &q.VoiceID, &q.TenantID, &q.RoomID, &q.SessionID, &q.QueuedAt, &q.ExpiresAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Requests = append(out.Requests, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
