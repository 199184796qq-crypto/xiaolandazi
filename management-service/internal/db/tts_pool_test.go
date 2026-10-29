package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/ttsscheduler"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTTSPoolAccountValidation(t *testing.T) {
	base := ttsscheduler.AccountInput{ID: ttsscheduler.CurrentAccountID, Name: "原账号", AccountUID: "10001", Enabled: true, RPM: 180, RPS: 3, Revision: 1}
	for _, tc := range []struct {
		name   string
		change func(*ttsscheduler.AccountInput)
	}{
		{"rpm181", func(a *ttsscheduler.AccountInput) { a.RPM = 181 }},
		{"rps4", func(a *ttsscheduler.AccountInput) { a.RPS = 4 }},
		{"negative", func(a *ttsscheduler.AccountInput) { a.RPS = -1 }},
		{"uid", func(a *ttsscheduler.AccountInput) { a.AccountUID = "" }},
		{"missing revision", func(a *ttsscheduler.AccountInput) { a.Revision = 0 }},
		{"environment secret", func(a *ttsscheduler.AccountInput) { a.NewAPIKey = "must-not-replace-env" }},
		{"new without key", func(a *ttsscheduler.AccountInput) { a.ID = ""; a.Revision = 0 }},
		{"missing voice", func(a *ttsscheduler.AccountInput) { a.ID = "second" }},
		{"bad mapping", func(a *ttsscheduler.AccountInput) { a.Voices = map[string]string{"not-model-bound": "voice"} }},
		{"duplicate model", func(a *ttsscheduler.AccountInput) {
			a.Models = []string{"qwen-audio-3.1-tts-flash", " qwen-audio-3.1-tts-flash "}
		}},
		{"bad model", func(a *ttsscheduler.AccountInput) { a.Models = []string{"wrong/model"} }},
		{"empty model", func(a *ttsscheduler.AccountInput) { a.Models = []string{" "} }},
		{"too many models", func(a *ttsscheduler.AccountInput) {
			for i := 0; i < 21; i++ {
				a.Models = append(a.Models, fmt.Sprintf("qwen-%d", i))
			}
		}},
		{"unselected mapping", func(a *ttsscheduler.AccountInput) {
			a.Models = []string{"cosyvoice-v3.5-flash"}
			a.Voices = map[string]string{"qwen-audio-3.1-tts-flash/voice-a": "voice-b"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			tc.change(&a)
			if err := validateTTSAccount(&a); err == nil {
				t.Fatal("unsafe account accepted")
			}
		})
	}
	if err := validateTTSAccount(&base); err != nil {
		t.Fatal(err)
	}
}

func TestTTSPoolMySQLModelConfigurationAndRouting(t *testing.T) {
	s, second := ttsPoolFixture(t)
	ctx := t.Context()
	const qwen = "qwen-audio-3.1-tts-flash"
	const cosy = "cosyvoice-v3.5-flash"
	current := ttsscheduler.AccountInput{ID: ttsscheduler.CurrentAccountID, Name: "限定Cosy", AccountUID: "10001", Enabled: true, RPM: 180, RPS: 3, Revision: 2, Models: []string{cosy}}
	if err := s.SaveTTSAccount(ctx, current, 1); err != nil {
		t.Fatal(err)
	}
	ttsEnqueue(t, s, "use-backup", "voice-a")
	claim, err := s.ClaimTTS(ctx, "use-backup")
	if err != nil || !claim.Granted || claim.Credential.AccountID != second || claim.Credential.VoiceID != "voice-b" {
		t.Fatal("model restriction was ignored", err)
	}
	// No compatible voice exists in the selected backup account. The current
	// account must not serve the excluded model, even though it has spare quota.
	ttsEnqueue(t, s, "unsupported-voice", "unmapped")
	claim, err = s.ClaimTTS(ctx, "unsupported-voice")
	if err != nil || claim.Granted {
		t.Fatal("excluded model escaped through current account", err)
	}
	current.Revision++
	current.Models = nil // A stale UI without the new field must preserve it.
	if err = s.SaveTTSAccount(ctx, current, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.MigrateTTSPool(ctx, "current-fixture-key"); err != nil {
		t.Fatal(err)
	}
	snap, err := s.TTSPoolSnapshot(ctx)
	if err != nil || len(snap.Accounts[0].Models) != 1 || snap.Accounts[0].Models[0] != cosy || snap.Accounts[1].Models[0] != qwen {
		t.Fatal("saved model or legacy mapping lost", err)
	}
	if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "cosy", Model: cosy, Provider: "cosyvoice", VoiceID: "cosy-voice", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimTTS(ctx, "cosy")
	if err != nil || !claim.Granted || claim.Credential.AccountID != current.ID {
		t.Fatal("incompatible earlier request blocked selected model", err)
	}
	// Explicitly return to the old follow-plan policy. Quota history survives.
	current.Revision++
	current.Models = []string{}
	if err = s.SaveTTSAccount(ctx, current, 1); err != nil {
		t.Fatal(err)
	}
	snap, err = s.TTSPoolSnapshot(ctx)
	if err != nil || len(snap.Accounts[0].Models) != 0 || snap.Accounts[0].InFlight != 1 {
		t.Fatal("clearing selection changed quota history", err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET admitted_at=UTC_TIMESTAMP(6)-INTERVAL 2 SECOND WHERE id='cosy'`); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimTTS(ctx, "unsupported-voice")
	if err != nil || !claim.Granted || claim.Credential.AccountID != current.ID {
		t.Fatal("explicit follow-plan selection did not apply", err)
	}
	// A newly configured account persists the selected model and encryption;
	// an older editor cannot add a voice for a different model through omission.
	input := ttsscheduler.AccountInput{Name: "新模型账号", AccountUID: "10003", NewAPIKey: "model-config-fixture-key", Enabled: true, RPM: 180, RPS: 3, Models: []string{cosy}, Voices: map[string]string{cosy + "/original": "target"}}
	if err = s.SaveTTSAccount(ctx, input, 1); err != nil {
		t.Fatal("new model account", err)
	}
	snap, err = s.TTSPoolSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range snap.Accounts {
		if a.AccountUID != "10003" {
			continue
		}
		if len(a.Models) != 1 || a.Models[0] != cosy {
			t.Fatal("new account did not retain model")
		}
		input = ttsscheduler.AccountInput{ID: a.ID, Name: a.Name, AccountUID: a.AccountUID, Enabled: true, RPM: 180, RPS: 3, Revision: a.Revision, Voices: map[string]string{qwen + "/original": "wrong"}}
		if err = s.SaveTTSAccount(ctx, input, 1); !errors.Is(err, ttsscheduler.ErrConfig) {
			t.Fatal("older editor bypassed selected models", err)
		}
	}
}

func TestTTSPoolMySQLModelUsageAndSharedQuota(t *testing.T) {
	s, second := ttsPoolFixture(t)
	ctx := t.Context()
	// Mixed-model requests share the same account quota, including failed ones.
	for _, row := range []struct {
		id, account, model, status string
		age                        int
	}{
		{"u1", ttsscheduler.CurrentAccountID, "qwen-audio-3.1-tts-flash", "running", 5},
		{"u2", ttsscheduler.CurrentAccountID, "cosyvoice-v3.5-flash", "failed", 10},
		{"u3", second, "qwen-audio-3.1-tts-flash", "completed", 90},
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO mgmt_tts_pool_jobs(id,account_id,model,provider,voice_id,status,queued_at,admitted_at,expires_at) VALUES(?,?,?,'qwen_audio','voice-a',?,TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)),TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)),UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE)`, row.id, row.account, row.model, row.status, -row.age, -row.age); err != nil {
			t.Fatal(err)
		}
	}
	ttsEnqueue(t, s, "not-admitted", "voice-a")
	snap, err := s.TTSPoolSnapshot(ctx)
	if err != nil || snap.Queued != 1 {
		t.Fatal("snapshot", err)
	}
	a, b := snap.Accounts[0], snap.Accounts[1]
	if a.UsedMinute != 2 || a.InFlight != 1 || len(a.ModelUsage) != 2 || a.ModelUsage[0].Model != "qwen-audio-3.1-tts-flash" || a.ModelUsage[0].InFlight != 1 || a.ModelUsage[1].UsedMinute != 1 {
		t.Fatal("account/model counters conflated")
	}
	if b.UsedMinute != 0 || len(b.ModelUsage) != 1 || b.ModelUsage[0].InFlight != 0 || b.ModelUsage[0].LastRequestedAt.IsZero() {
		t.Fatal("recent completed usage missing or assigned to wrong account")
	}
	if a.ModelUsage[0].LastRequestedAt.After(snap.ObservedAt) || a.ModelUsage[0].LastRequestedAt.Before(snap.ObservedAt.Add(-time.Minute)) {
		t.Fatal("invalid actual request timestamp")
	}
	// Reading usage is observational: it cannot settle running work or grant a
	// queued request, and it never serializes the account key or sealed bytes.
	var status string
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM mgmt_tts_pool_jobs WHERE id='u1'`).Scan(&status); err != nil || status != "running" {
		t.Fatal("snapshot mutated running request", err)
	}
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "fixture-key") || strings.Contains(string(raw), "sealed_key") {
		t.Fatal("snapshot exposed credentials")
	}
	// Prove selecting two models cannot create a second quota bucket.
	if err = s.FinishTTS(ctx, "not-admitted", "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_accounts SET models_json=JSON_ARRAY('qwen-audio-3.1-tts-flash','cosyvoice-v3.5-flash'),rpm=2 WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "shared-limit", Model: "cosyvoice-v3.5-flash", Provider: "cosyvoice", VoiceID: "voice-a", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimTTS(ctx, "shared-limit")
	if err != nil || claim.Granted {
		t.Fatal("different model bypassed account-wide RPM", err)
	}
}

func TestTTSPoolMySQLUpgradeLegacySchema(t *testing.T) {
	t.Setenv("MODEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("fixture-key-", 4))
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE TABLE mgmt_tts_pool_accounts(id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,name VARCHAR(80) NOT NULL,provider VARCHAR(32) NOT NULL,credential_source VARCHAR(32) NOT NULL,configured BOOLEAN NOT NULL,enabled BOOLEAN NOT NULL,rpm INT NOT NULL,rps INT NOT NULL,revision BIGINT UNSIGNED NOT NULL,updated_by_user_id BIGINT NOT NULL,updated_at DATETIME(6) NOT NULL)`,
		`CREATE TABLE mgmt_tts_pool_jobs(seq BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,account_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',model VARCHAR(160) NOT NULL,tenant_id BIGINT NOT NULL DEFAULT 0,room_id BIGINT NOT NULL DEFAULT 0,session_id BIGINT NOT NULL DEFAULT 0,status VARCHAR(16) NOT NULL,queued_at DATETIME(6) NOT NULL,expires_at DATETIME(6) NOT NULL,admitted_at DATETIME(6) NULL,finished_at DATETIME(6) NULL,UNIQUE KEY uk_tts_pool_job(id),KEY idx_tts_pool_fifo(status,seq),KEY idx_tts_pool_window(account_id,admitted_at),KEY idx_tts_pool_expiry(expires_at))`,
		`INSERT INTO mgmt_tts_pool_accounts VALUES('current-aliyun-tts','旧名称','aliyun','current',TRUE,FALSE,120,2,7,9,'2026-10-29 12:00:00.123456')`,
		`INSERT INTO mgmt_tts_pool_jobs(id,account_id,model,status,queued_at,expires_at,admitted_at,finished_at) VALUES('legacy-receipt','current-aliyun-tts','qwen-audio-3.1-tts-flash','completed','2026-10-29 12:00:00.123456','2026-10-29 12:02:00.123456','2026-10-29 12:00:01.123456','2026-10-29 12:00:02.123456')`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for pass := 0; pass < 2; pass++ {
		if err := s.MigrateTTSPool(ctx, "current-fixture-key"); err != nil {
			t.Fatal(err)
		}
		var name, uid, fingerprint, status, model, provider, voice string
		var enabled bool
		var rpm, rps, revision, actor, accounts, jobs int
		if err := s.db.QueryRowContext(ctx, `SELECT name,account_uid,key_fingerprint,enabled,rpm,rps,revision,updated_by_user_id FROM mgmt_tts_pool_accounts WHERE id='current-aliyun-tts'`).Scan(&name, &uid, &fingerprint, &enabled, &rpm, &rps, &revision, &actor); err != nil {
			t.Fatal(err)
		}
		if name != "旧名称" || uid != "current" || fingerprint != modelPoolFingerprint("current-fixture-key") || enabled || rpm != 120 || rps != 2 || revision != 7 || actor != 9 {
			t.Fatal("migration overwrote existing account state")
		}
		if err := s.db.QueryRowContext(ctx, `SELECT status,model,provider,voice_id FROM mgmt_tts_pool_jobs WHERE id='legacy-receipt' AND queued_at='2026-10-29 12:00:00.123456' AND admitted_at='2026-10-29 12:00:01.123456' AND finished_at='2026-10-29 12:00:02.123456'`).Scan(&status, &model, &provider, &voice); err != nil {
			t.Fatal("legacy receipt changed", err)
		}
		if status != "completed" || model != "qwen-audio-3.1-tts-flash" || provider != "aliyun" || voice != "" {
			t.Fatal("legacy quota history was changed")
		}
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_accounts`).Scan(&accounts); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_jobs`).Scan(&jobs); err != nil {
			t.Fatal(err)
		}
		if accounts != 1 || jobs != 1 {
			t.Fatal("migration duplicated account or deleted history")
		}
		if _, err := s.TTSPoolSnapshot(ctx); err != nil {
			t.Fatal("upgraded schema cannot serve account pool", err)
		}
	}
}
func ttsPoolFixture(t *testing.T) (*Store, string) {
	t.Helper()
	t.Setenv("MODEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("fixture-key-", 4))
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_runtime_sessions(id BIGINT PRIMARY KEY,tenant_id BIGINT,room_id BIGINT,status VARCHAR(32))`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO live_runtime_sessions(id,tenant_id,room_id,status) VALUES(550,14,29,'running')`); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateTTSPool(ctx, "current-fixture-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTTSAccount(ctx, ttsscheduler.AccountInput{ID: ttsscheduler.CurrentAccountID, Name: "当前", AccountUID: "10001", Enabled: true, RPM: 180, RPS: 3, Revision: 1}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTTSAccount(ctx, ttsscheduler.AccountInput{Name: "备用", AccountUID: "10002", NewAPIKey: "second-fixture-key", Enabled: true, RPM: 180, RPS: 3, Voices: map[string]string{"qwen-audio-3.1-tts-flash/voice-a": "voice-b"}}, 1); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.TTSPoolSnapshot(ctx)
	if err != nil || len(snapshot.Accounts) != 2 {
		t.Fatal("snapshot", err)
	}
	for _, a := range snapshot.Accounts {
		if a.ID != ttsscheduler.CurrentAccountID {
			return s, a.ID
		}
	}
	t.Fatal("second account missing")
	return nil, ""
}
func ttsEnqueue(t *testing.T, s *Store, id, voice string) {
	t.Helper()
	if err := s.EnqueueTTS(t.Context(), ttsscheduler.Work{ID: id, Model: "qwen-audio-3.1-tts-flash", Provider: "qwen_audio", VoiceID: voice, Scope: ttsscheduler.Scope{TenantID: 14, RoomID: 29, SessionID: 550}, TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
}
func ttsFillMinute(t *testing.T, s *Store, account string) {
	t.Helper()
	for i := 0; i < 180; i++ {
		_, err := s.db.ExecContext(t.Context(), `INSERT INTO mgmt_tts_pool_jobs(id,account_id,model,provider,voice_id,status,queued_at,expires_at,admitted_at,finished_at) VALUES(?,?,'other-model','qwen_audio','voice-a','completed',UTC_TIMESTAMP(6)-INTERVAL 30 SECOND,UTC_TIMESTAMP(6)+INTERVAL 30 SECOND,UTC_TIMESTAMP(6)-INTERVAL 30 SECOND,UTC_TIMESTAMP(6))`, fmt.Sprintf("fill-%s-%d", account, i), account)
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestTTSPoolMySQLNextAccountQueueAndRestart(t *testing.T) {
	s, second := ttsPoolFixture(t)
	ctx := t.Context()
	ttsFillMinute(t, s, ttsscheduler.CurrentAccountID)
	ttsEnqueue(t, s, "first", "voice-a")
	permit, err := s.ClaimTTS(ctx, "first")
	if err != nil || !permit.Granted || permit.Credential.AccountID != second || permit.Credential.VoiceID != "voice-b" || permit.Credential.APIKey != "second-fixture-key" {
		t.Fatalf("did not switch full account: granted=%t error=%v", permit.Granted, err)
	}
	if err = s.FinishTTS(ctx, "first", "completed"); err != nil {
		t.Fatal(err)
	}
	ttsEnqueue(t, s, "second", "voice-a")
	claim, err := s.ClaimTTS(ctx, "second")
	if err != nil || claim.Granted {
		t.Fatal("completion incorrectly released second-window quota", err)
	}
	ttsFillMinute(t, s, second)
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET admitted_at=UTC_TIMESTAMP(6)-INTERVAL 2 SECOND WHERE id='first'`); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimTTS(ctx, "second")
	if err != nil || claim.Granted {
		t.Fatal("all accounts full must queue", err)
	}
	if err = s.MigrateTTSPool(ctx, "current-fixture-key"); err != nil {
		t.Fatal(err)
	}
	claim, err = (&Store{db: s.db}).ClaimTTS(ctx, "second")
	if err != nil || claim.Granted {
		t.Fatal("restart/process escaped account quota", err)
	}
	snap, err := s.TTSPoolSnapshot(ctx)
	if err != nil || snap.Queued != 1 || snap.Accounts[0].UsedMinute != 180 {
		t.Fatal("incorrect queue statistics", err)
	}
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "fixture-key") || strings.Contains(string(raw), "sealed_key") {
		t.Fatal("credential returned by admin API")
	}
	var sealed []byte
	if err = s.db.QueryRowContext(ctx, `SELECT sealed_key FROM mgmt_tts_pool_accounts WHERE id=?`, second).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "second-fixture-key") {
		t.Fatal("plaintext credential stored")
	}
	if err = s.FinishTTS(ctx, "second", "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTTS(ctx, "second"); !errors.Is(err, ttsscheduler.ErrExpired) {
		t.Fatal("cancelled request reactivated", err)
	}
}
func TestTTSPoolMySQLCASDuplicateAndVoiceIsolation(t *testing.T) {
	s, second := ttsPoolFixture(t)
	ctx := t.Context()
	snap, _ := s.TTSPoolSnapshot(ctx)
	current := snap.Accounts[0]
	a := ttsscheduler.AccountInput{ID: current.ID, Name: current.Name, AccountUID: current.AccountUID, Enabled: false, RPM: 180, RPS: 3, Revision: current.Revision}
	if err := s.SaveTTSAccount(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTTSAccount(ctx, a, 1); !errors.Is(err, ttsscheduler.ErrRevision) {
		t.Fatal("stale admin saved", err)
	}
	a.Revision++
	a.AccountUID = "changed-identity"
	if err := s.SaveTTSAccount(ctx, a, 1); !errors.Is(err, ttsscheduler.ErrConfig) {
		t.Fatal("account identity changed", err)
	}
	if err := s.MigrateTTSPool(ctx, "current-fixture-key"); err != nil {
		t.Fatal(err)
	}
	ttsEnqueue(t, s, "incompatible", "unmapped-voice")
	claim, err := s.ClaimTTS(ctx, "incompatible")
	if err != nil || claim.Granted {
		t.Fatal("unmapped voice switched account", err)
	}
	ttsEnqueue(t, s, "compatible", "voice-a")
	claim, err = s.ClaimTTS(ctx, "compatible")
	if err != nil || !claim.Granted || claim.Credential.AccountID != second {
		t.Fatal("incompatible head blocked unrelated available account", err)
	}
	for _, entry := range []ttsscheduler.AccountInput{
		{Name: "同主账号", AccountUID: "10002", NewAPIKey: "different-key", RPM: 180, RPS: 3},
		{Name: "同Key", AccountUID: "10003", NewAPIKey: "second-fixture-key", RPM: 180, RPS: 3},
		{Name: "重复当前Key", AccountUID: "10004", NewAPIKey: "current-fixture-key", RPM: 180, RPS: 3},
	} {
		if err := s.SaveTTSAccount(ctx, entry, 1); err == nil {
			t.Fatal("duplicate account increased allowance")
		}
	}
}
func TestTTSPoolMySQLConcurrentWorkers(t *testing.T) {
	s, _ := ttsPoolFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 1600*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	granted := 0
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service := ttsscheduler.New(t.Context(), &Store{db: s.db})
			permit, err := service.Acquire(ctx, ttsscheduler.Work{Model: "qwen-audio-3.1-tts-flash", Provider: "qwen_audio", VoiceID: "voice-a"})
			if err == nil {
				permit.Finish("completed")
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted < 2 {
		t.Fatal("available accounts not used")
	}
	rows, err := s.db.QueryContext(t.Context(), `SELECT account_id,admitted_at FROM mgmt_tts_pool_jobs WHERE admitted_at IS NOT NULL ORDER BY admitted_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	times := map[string][]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err = rows.Scan(&id, &at); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, prior := range times[id] {
			if prior.After(at.Add(-time.Second)) {
				n++
			}
		}
		if n >= 3 {
			t.Fatal("concurrent workers exceeded account RPS")
		}
		times[id] = append(times[id], at)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	snap, err := s.TTSPoolSnapshot(t.Context())
	if err != nil || snap.Queued != 0 {
		t.Fatal("cancelled waiters retained", err)
	}
}

func TestTTSPoolMySQLStoppedAndPausedSession(t *testing.T) {
	s, _ := ttsPoolFixture(t)
	ctx := t.Context()
	ttsEnqueue(t, s, "paused", "voice-a")
	if _, err := s.db.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='paused' WHERE id=550`); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimTTS(ctx, "paused")
	if err != nil || claim.Granted {
		t.Fatal("paused room bought speech", err)
	}
	if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "other-room", Model: "qwen-audio-3.1-tts-flash", Provider: "qwen_audio", VoiceID: "voice-a", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimTTS(ctx, "other-room")
	if err != nil || !claim.Granted {
		t.Fatal("paused FIFO head blocked other room", err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='stopped' WHERE id=550`); err != nil {
		t.Fatal(err)
	}
	claim, err = s.ClaimTTS(ctx, "paused")
	if !errors.Is(err, ttsscheduler.ErrExpired) || claim.Granted {
		t.Fatal("stopped room bought queued speech", err)
	}
	if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "wrong-scope", Model: "qwen-audio-3.1-tts-flash", VoiceID: "voice-a", Scope: ttsscheduler.Scope{TenantID: 15, RoomID: 29, SessionID: 550}, TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTTS(ctx, "wrong-scope"); !errors.Is(err, ttsscheduler.ErrExpired) {
		t.Fatal("another tenant used old session", err)
	}
	if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "orphan", Model: "qwen-audio-3.1-tts-flash", VoiceID: "voice-a", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET heartbeat_at=UTC_TIMESTAMP(6)-INTERVAL 15 SECOND WHERE id='orphan'`); err != nil {
		t.Fatal(err)
	}
	if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "after-orphan", Model: "qwen-audio-3.1-tts-flash", VoiceID: "voice-a", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM mgmt_tts_pool_jobs WHERE id='orphan'`).Scan(&status); err != nil || status != "cancelled" {
		t.Fatal("abandoned process kept FIFO blocked", err)
	}
}
