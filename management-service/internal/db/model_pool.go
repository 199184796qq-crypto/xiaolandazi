package db

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
)

var (
	ErrModelPoolFull         = errors.New("暂无可用模型名额，请联系管理员配置模型池或等待其他直播间停止AI")
	ErrModelPoolLeaseMissing = errors.New("当前AI的模型分配已失效，请停止AI后重新开始")
	ErrModelPoolRevision     = errors.New("模型池配置已被修改，请刷新后重试")
	ErrModelPoolInUse        = errors.New("该模型仍有AI占用，不能删除或修改账号、模型和连接地址")
)

type ModelPoolEntry struct {
	ID             string                   `json:"id"`
	AccountName    string                   `json:"account_name"`
	Profile        agentgateway.SpeechModel `json:"profile"`
	MaxRooms       int                      `json:"max_rooms"`
	Enabled        bool                     `json:"enabled"`
	Revision       uint64                   `json:"revision"`
	HasAPIKey      bool                     `json:"has_api_key"`
	UsedRooms      int                      `json:"used_rooms"`
	AvailableRooms int                      `json:"available_rooms"`
	UpdatedAt      time.Time                `json:"updated_at"`
}
type ModelPoolEntryInput struct {
	ID          string                        `json:"id"`
	AccountName string                        `json:"account_name"`
	Profile     agentgateway.SpeechModelInput `json:"profile"`
	MaxRooms    int                           `json:"max_rooms"`
	Enabled     bool                          `json:"enabled"`
	Revision    uint64                        `json:"revision"`
}
type ModelPoolLease struct {
	LeaseID     string    `json:"lease_id"`
	EntryID     string    `json:"entry_id"`
	AccountName string    `json:"account_name"`
	Model       string    `json:"model"`
	TenantID    int64     `json:"tenant_id"`
	RoomID      int64     `json:"room_id"`
	RuntimeID   int64     `json:"runtime_id"`
	TenantName  string    `json:"tenant_name"`
	RoomName    string    `json:"room_name"`
	AIStatus    string    `json:"ai_status"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

func modelPoolID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "pool-" + hex.EncodeToString(raw[:]), nil
}
func modelPoolFingerprint(key string) string {
	mac := hmac.New(sha256.New, []byte(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY")))
	_, _ = mac.Write([]byte("model-pool-key/v1:" + key))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Store) MigrateModelPool(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS mgmt_model_pool_lock (id TINYINT UNSIGNED PRIMARY KEY, revision BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`INSERT IGNORE INTO mgmt_model_pool_lock(id) VALUES(1)`,
		`CREATE TABLE IF NOT EXISTS mgmt_model_pool_entries (
		 id VARCHAR(64) PRIMARY KEY, account_name VARCHAR(80) NOT NULL, model VARCHAR(160) NOT NULL,
		 profile_json JSON NOT NULL, sealed_key BLOB NOT NULL, key_fingerprint CHAR(64) NOT NULL,
		 max_rooms INT UNSIGNED NOT NULL DEFAULT 0, enabled BOOLEAN NOT NULL DEFAULT FALSE,
		 revision BIGINT UNSIGNED NOT NULL DEFAULT 1, updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
		 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		 UNIQUE KEY uk_model_pool_account_model(account_name,model), UNIQUE KEY uk_model_pool_key_model(key_fingerprint,model)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS mgmt_model_pool_leases (
		 tenant_id BIGINT UNSIGNED NOT NULL, room_id BIGINT UNSIGNED NOT NULL, lease_id VARCHAR(64) NOT NULL,
		 runtime_id BIGINT UNSIGNED NOT NULL, entry_id VARCHAR(64) NOT NULL, account_name VARCHAR(80) NOT NULL,
		 model VARCHAR(160) NOT NULL, profile_json JSON NOT NULL, sealed_key BLOB NOT NULL,
		 status VARCHAR(16) NOT NULL DEFAULT 'active', acquired_at DATETIME(3) NOT NULL,
		 released_at DATETIME(3) NULL, release_reason VARCHAR(80) NOT NULL DEFAULT '',
		 PRIMARY KEY(tenant_id,room_id), UNIQUE KEY uk_model_pool_lease(lease_id),
		 KEY idx_model_pool_capacity(entry_id,status), KEY idx_model_pool_runtime(runtime_id,status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate model pool: %w", err)
		}
	}
	for _, name := range []string{"pool_entry_id", "pool_lease_id", "pool_account_name"} {
		exists, err := s.columnExists(ctx, "live_model_exchanges", name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = s.db.ExecContext(ctx, "ALTER TABLE live_model_exchanges ADD COLUMN "+name+" VARCHAR(80) NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	// Durable assignments survive a Management restart. Only AI terminal states
	// release them; collector status and elapsed wall time are never considered.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockModelPoolTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mgmt_model_pool_leases l LEFT JOIN live_runtime_sessions r ON r.id=l.runtime_id
	 SET l.status='released',l.released_at=UTC_TIMESTAMP(3),l.release_reason='ai_already_stopped'
	 WHERE l.status='active' AND (r.id IS NULL OR r.status NOT IN ('running','paused'))`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.modelPoolReady = true
	return nil
}

func lockModelPoolTx(ctx context.Context, tx *sql.Tx) error {
	var revision uint64
	return tx.QueryRowContext(ctx, `SELECT revision FROM mgmt_model_pool_lock WHERE id=1 FOR UPDATE`).Scan(&revision)
}

// Locking reads see assignments committed while this transaction waited for
// the pool lock, even if earlier business reads established an older snapshot.
func modelPoolUsageTx(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT entry_id FROM mgmt_model_pool_leases WHERE status='active' FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		counts[id]++
	}
	return counts, rows.Err()
}

func validateModelPoolInput(in *ModelPoolEntryInput) error {
	in.AccountName = strings.TrimSpace(in.AccountName)
	if in.AccountName == "" || utf8.RuneCountInString(in.AccountName) > 80 {
		return errors.New("账号名称须为1—80个字，同一账号请沿用同一名称")
	}
	if in.MaxRooms < 0 || in.MaxRooms > 10000 || (in.Enabled && in.MaxRooms == 0) {
		return errors.New("启用时请填写1—10000个直播间名额")
	}
	if in.Profile.TimeoutMS == 0 {
		in.Profile.TimeoutMS = 120000
	}
	if in.Profile.MaxTokens == 0 {
		in.Profile.MaxTokens = 6000
	}
	in.Profile.Model = strings.TrimSpace(in.Profile.Model)
	in.Profile.Name = in.AccountName + " · " + in.Profile.Model
	// Display names can contain long model IDs; connection validation does not
	// depend on that cosmetic name.
	if utf8.RuneCountInString(in.Profile.Name) > 80 {
		in.Profile.Name = string([]rune(in.Profile.Name)[:80])
	}
	in.Profile.Enabled = true
	return agentgateway.ValidateSpeechModel(in.Profile.SpeechModel)
}

func (s *Store) SaveModelPoolEntry(ctx context.Context, input ModelPoolEntryInput, actor int64) (ModelPoolEntry, error) {
	if input.ID == "" {
		var err error
		input.ID, err = modelPoolID()
		if err != nil {
			return ModelPoolEntry{}, err
		}
	}
	if !strings.HasPrefix(input.ID, "pool-") || !agentgateway.ValidSpeechProfileID(input.ID) {
		return ModelPoolEntry{}, errors.New("模型池编号无效")
	}
	input.Profile.ID = input.ID
	if err := validateModelPoolInput(&input); err != nil {
		return ModelPoolEntry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelPoolEntry{}, err
	}
	defer tx.Rollback()
	if err = lockModelPoolTx(ctx, tx); err != nil {
		return ModelPoolEntry{}, err
	}
	var oldProfileRaw []byte
	var sealed []byte
	var oldRevision uint64
	var oldAccount string
	err = tx.QueryRowContext(ctx, `SELECT account_name,profile_json,sealed_key,revision FROM mgmt_model_pool_entries WHERE id=? FOR UPDATE`, input.ID).Scan(&oldAccount, &oldProfileRaw, &sealed, &oldRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ModelPoolEntry{}, err
	}
	if (errors.Is(err, sql.ErrNoRows) && input.Revision != 0) || (err == nil && input.Revision != oldRevision) {
		return ModelPoolEntry{}, ErrModelPoolRevision
	}
	usage, err := modelPoolUsageTx(ctx, tx)
	if err != nil {
		return ModelPoolEntry{}, err
	}
	used := usage[input.ID]
	if used > input.MaxRooms {
		return ModelPoolEntry{}, errors.New("名额不能小于当前仍在占用的直播间数量")
	}
	var old agentgateway.SpeechModel
	if len(oldProfileRaw) > 0 {
		if err = json.Unmarshal(oldProfileRaw, &old); err != nil {
			return ModelPoolEntry{}, err
		}
		if used > 0 && (oldAccount != input.AccountName || old.Model != input.Profile.Model || old.Protocol != input.Profile.Protocol || old.BaseURL != input.Profile.BaseURL) {
			return ModelPoolEntry{}, ErrModelPoolInUse
		}
	}
	key := strings.TrimSpace(input.Profile.NewAPIKey)
	if key == "" {
		if len(sealed) == 0 {
			return ModelPoolEntry{}, errors.New("请填写账号API Key")
		}
		key, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), input.ID, sealed)
		if err != nil {
			return ModelPoolEntry{}, errors.New("账号密钥不可读取，请重新填写")
		}
	}
	if len(key) > 4096 {
		return ModelPoolEntry{}, errors.New("API Key过长")
	}
	sealed, err = agentgateway.SealSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), input.ID, key)
	if err != nil {
		return ModelPoolEntry{}, err
	}
	profile := input.Profile.SpeechModel
	profile.APIKey = ""
	profile.HasAPIKey = true
	raw, err := json.Marshal(profile)
	if err != nil {
		return ModelPoolEntry{}, err
	}
	var duplicate int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_model_pool_entries WHERE id<>? AND model=? AND (account_name=? OR key_fingerprint=?)`, input.ID, profile.Model, input.AccountName, modelPoolFingerprint(key)).Scan(&duplicate); err != nil {
		return ModelPoolEntry{}, err
	}
	if duplicate > 0 {
		return ModelPoolEntry{}, errors.New("这个账号和模型已在池中；同账号多个Key不能增加独立名额，请编辑已有记录")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_model_pool_entries(id,account_name,model,profile_json,sealed_key,key_fingerprint,max_rooms,enabled,revision,updated_by_user_id)
	 VALUES(?,?,?,?,?,?,?,?,1,?) ON DUPLICATE KEY UPDATE account_name=VALUES(account_name),model=VALUES(model),profile_json=VALUES(profile_json),sealed_key=VALUES(sealed_key),key_fingerprint=VALUES(key_fingerprint),max_rooms=VALUES(max_rooms),enabled=VALUES(enabled),revision=revision+1,updated_by_user_id=VALUES(updated_by_user_id),updated_at=UTC_TIMESTAMP(3)`, input.ID, input.AccountName, profile.Model, raw, sealed, modelPoolFingerprint(key), input.MaxRooms, input.Enabled, actor)
	if err != nil {
		return ModelPoolEntry{}, err
	}
	if err = tx.Commit(); err != nil {
		return ModelPoolEntry{}, err
	}
	entries, err := s.ListModelPoolEntries(ctx)
	if err != nil {
		return ModelPoolEntry{}, err
	}
	for _, entry := range entries {
		if entry.ID == input.ID {
			return entry, nil
		}
	}
	return ModelPoolEntry{}, sql.ErrNoRows
}

func (s *Store) ListModelPoolEntries(ctx context.Context) ([]ModelPoolEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.account_name,e.profile_json,e.max_rooms,e.enabled,e.revision,e.updated_at,
	 (SELECT COUNT(*) FROM mgmt_model_pool_leases l WHERE l.entry_id=e.id AND l.status='active') FROM mgmt_model_pool_entries e ORDER BY e.created_at,e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelPoolEntry{}
	for rows.Next() {
		var e ModelPoolEntry
		var raw []byte
		if err = rows.Scan(&e.ID, &e.AccountName, &raw, &e.MaxRooms, &e.Enabled, &e.Revision, &e.UpdatedAt, &e.UsedRooms); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e.Profile); err != nil {
			return nil, err
		}
		e.Profile.APIKey = ""
		e.HasAPIKey = true
		e.AvailableRooms = max(0, e.MaxRooms-e.UsedRooms)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) ListModelPoolLeases(ctx context.Context) ([]ModelPoolLease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.lease_id,l.entry_id,l.account_name,l.model,l.tenant_id,l.room_id,l.runtime_id,
	 COALESCE(t.name,''),COALESCE(c.name,''),COALESCE(r.status,''),l.acquired_at FROM mgmt_model_pool_leases l
	 LEFT JOIN mgmt_tenants t ON t.id=l.tenant_id LEFT JOIN core_rooms c ON c.id=l.room_id AND c.tenant_id=l.tenant_id
	 LEFT JOIN live_runtime_sessions r ON r.id=l.runtime_id WHERE l.status='active' ORDER BY l.acquired_at,l.room_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelPoolLease{}
	for rows.Next() {
		var l ModelPoolLease
		if err = rows.Scan(&l.LeaseID, &l.EntryID, &l.AccountName, &l.Model, &l.TenantID, &l.RoomID, &l.RuntimeID, &l.TenantName, &l.RoomName, &l.AIStatus, &l.AcquiredAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *Store) DeleteModelPoolEntry(ctx context.Context, id string, revision uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockModelPoolTx(ctx, tx); err != nil {
		return err
	}
	usage, err := modelPoolUsageTx(ctx, tx)
	if err != nil {
		return err
	}
	if usage[id] > 0 {
		return ErrModelPoolInUse
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM mgmt_model_pool_entries WHERE id=? AND revision=?`, id, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrModelPoolRevision
	}
	return tx.Commit()
}

func (s *Store) modelPoolCatalogProfile(ctx context.Context, id string) (*agentgateway.SpeechModel, error) {
	var raw, sealed []byte
	if err := s.db.QueryRowContext(ctx, `SELECT profile_json,sealed_key FROM mgmt_model_pool_entries WHERE id=?`, id).Scan(&raw, &sealed); err != nil {
		return nil, err
	}
	var p agentgateway.SpeechModel
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	var err error
	p.APIKey, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), p.ID, sealed)
	return &p, err
}

// All allocations, releases and capacity edits take one small durable lock.
// Multiple Management processes therefore cannot each fill the same last slot.
func (s *Store) acquireModelPoolTx(ctx context.Context, tx *sql.Tx, tenant, room, runtime int64, now time.Time) error {
	if !s.modelPoolReady {
		return nil
	} // Isolated legacy Store fixtures only; production migration is mandatory.
	if err := lockModelPoolTx(ctx, tx); err != nil {
		return err
	}
	var previous int64
	err := tx.QueryRowContext(ctx, `SELECT runtime_id FROM mgmt_model_pool_leases WHERE tenant_id=? AND room_id=? AND status='active' FOR UPDATE`, tenant, room).Scan(&previous)
	if err == nil {
		if previous == runtime {
			return nil
		}
		return errors.New("当前直播间已有模型分配，请先停止原AI")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	usage, err := modelPoolUsageTx(ctx, tx)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,account_name,model,profile_json,sealed_key,max_rooms FROM mgmt_model_pool_entries WHERE enabled=1 AND max_rooms>0 ORDER BY created_at,id FOR UPDATE`)
	if err != nil {
		return err
	}
	var id, account, modelName string
	var raw, sealed []byte
	bestUsed, bestCapacity := 0, 0
	for rows.Next() {
		var candidateID, candidateAccount, candidateModel string
		var candidateRaw, candidateKey []byte
		var capacity int
		if err = rows.Scan(&candidateID, &candidateAccount, &candidateModel, &candidateRaw, &candidateKey, &capacity); err != nil {
			rows.Close()
			return err
		}
		used := usage[candidateID]
		if used >= capacity || (id != "" && used*bestCapacity >= bestUsed*capacity) {
			continue
		}
		id, account, modelName, raw, sealed, bestUsed, bestCapacity = candidateID, candidateAccount, candidateModel, candidateRaw, candidateKey, used, capacity
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if id == "" {
		return ErrModelPoolFull
	}
	if _, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), id, sealed); err != nil {
		return errors.New("模型池账号密钥不可用，请联系管理员重新配置")
	}
	lease, err := modelPoolID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_model_pool_leases(tenant_id,room_id,lease_id,runtime_id,entry_id,account_name,model,profile_json,sealed_key,status,acquired_at)
	 VALUES(?,?,?,?,?,?,?,?,?,'active',?) ON DUPLICATE KEY UPDATE lease_id=VALUES(lease_id),runtime_id=VALUES(runtime_id),entry_id=VALUES(entry_id),account_name=VALUES(account_name),model=VALUES(model),profile_json=VALUES(profile_json),sealed_key=VALUES(sealed_key),status='active',acquired_at=VALUES(acquired_at),released_at=NULL,release_reason=''`, tenant, room, lease, runtime, id, account, modelName, raw, sealed, now)
	return err
}

func (s *Store) releaseModelPoolTx(ctx context.Context, tx *sql.Tx, runtime int64, reason string, now time.Time) error {
	if !s.modelPoolReady {
		return nil
	}
	if err := lockModelPoolTx(ctx, tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE mgmt_model_pool_leases SET status='released',released_at=?,release_reason=? WHERE runtime_id=? AND status='active'`, now, reason, runtime)
	return err
}

func (s *Store) releaseStoppedModelPoolTx(ctx context.Context, tx *sql.Tx, runtime int64) error {
	if !s.modelPoolReady {
		return nil
	}
	var status, reason string
	if err := tx.QueryRowContext(ctx, `SELECT status,stop_reason FROM live_runtime_sessions WHERE id=?`, runtime).Scan(&status, &reason); err != nil {
		return err
	}
	if status == "running" || status == "paused" {
		return nil
	}
	return s.releaseModelPoolTx(ctx, tx, runtime, reason, time.Now().UTC())
}

func (s *Store) roomModelPoolProfile(ctx context.Context) (*agentgateway.SpeechModel, error) {
	scope := agentgateway.ContextRoomModelScope(ctx)
	if !s.modelPoolReady || scope.TenantID <= 0 || scope.RoomID <= 0 {
		return nil, nil
	}
	var raw, sealed []byte
	var runtime int64
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT l.profile_json,l.sealed_key,l.runtime_id,r.status FROM mgmt_model_pool_leases l
	 JOIN live_runtime_sessions r ON r.id=l.runtime_id AND r.tenant_id=l.tenant_id AND r.room_id=l.room_id
	 WHERE l.tenant_id=? AND l.room_id=? AND l.status='active'`, scope.TenantID, scope.RoomID).Scan(&raw, &sealed, &runtime, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if scope.RequireLease {
			return nil, ErrModelPoolLeaseMissing
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if (scope.RuntimeID > 0 && scope.RuntimeID != runtime) || (status != "running" && status != "paused") {
		return nil, ErrModelPoolLeaseMissing
	}
	var p agentgateway.SpeechModel
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.APIKey, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), p.ID, sealed)
	return &p, err
}

// The configured pool controls only room AI work. General administrative
// analysis retains its existing role configuration and cannot consume a slot.
func (s *Store) ResolveRoomModelProfile(ctx context.Context) (*agentgateway.SpeechModel, error) {
	return s.roomModelPoolProfile(ctx)
}
func (s *Store) ModelPoolCatalogProfile(ctx context.Context, id string) (*agentgateway.SpeechModel, error) {
	return s.modelPoolCatalogProfile(ctx, id)
}
