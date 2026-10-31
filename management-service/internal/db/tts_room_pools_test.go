package db

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/ttsscheduler"
)

func TestTTSPoolMySQLFireTenConcurrentAcrossRooms(t *testing.T) {
	s, _ := ttsPoolFixture(t)
	ctx := t.Context()
	a := ttsscheduler.AccountInput{Name: "火山十并发", Provider: "volcengine", AccountUID: "fire-10", NewAPIKey: "fire-ten-local", Enabled: true, RPM: 1, RPS: 1, Models: []string{"seed-icl-2.0"}, Voices: map[string]string{"seed-icl-2.0/S_source": "S_target"}}
	if err := s.SaveTTSAccount(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.TTSPoolSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var fire string
	for _, account := range snapshot.Accounts {
		if account.Provider == "volcengine" {
			if account.RPM != 0 || account.RPS != 0 || account.MaxConcurrent != 10 {
				t.Fatal("fire inherited rate gates", account)
			}
			fire = account.ID
		}
	}
	if fire == "" {
		t.Fatal("fire account missing")
	}
	ttsFillMinute(t, s, fire) // 180 recent starts must not block this concurrency pool.
	for i := 0; i < 11; i++ {
		id := fmt.Sprintf("fire-room-%d", i)
		if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: id, Provider: "volcengine", Model: "seed-icl-2.0", VoiceID: "S_source", Scope: ttsscheduler.Scope{TenantID: 14, RoomID: int64(100 + i)}, TTL: time.Minute}); err != nil {
			t.Fatal(err)
		}
		claim, e := (&Store{db: s.db}).ClaimTTS(ctx, id)
		if e != nil || claim.Granted != (i < 10) {
			t.Fatal("ten concurrent across rooms", i, claim.Granted, e)
		}
		if claim.Granted && (claim.Credential.AccountID != fire || claim.Credential.VoiceID != "S_target") {
			t.Fatal("cross-provider route")
		}
	}
	if err = s.FinishTTS(ctx, "fire-room-0", "failed"); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimTTS(ctx, "fire-room-10")
	if err != nil || !claim.Granted {
		t.Fatal("failure did not release concurrency immediately", err)
	}
	// A second independent fire account provides another ten slots.
	a.AccountUID = "fire-second"
	a.NewAPIKey = "fire-second-local"
	if err = s.SaveTTSAccount(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	for i := 11; i < 22; i++ {
		id := fmt.Sprintf("fire-room-%d", i)
		if err = s.EnqueueTTS(ctx, ttsscheduler.Work{ID: id, Provider: "volcengine", Model: "seed-icl-2.0", VoiceID: "S_source", Scope: ttsscheduler.Scope{TenantID: 14, RoomID: int64(100 + i)}, TTL: time.Minute}); err != nil {
			t.Fatal(err)
		}
		claim, e := s.ClaimTTS(ctx, id)
		if e != nil || claim.Granted != (i < 21) {
			t.Fatal("next fire account or full queue", i, claim.Granted, e)
		}
	}
	ttsEnqueue(t, s, "ali-unblocked", "voice-a")
	claim, err = s.ClaimTTS(ctx, "ali-unblocked")
	if err != nil || !claim.Granted || claim.Credential.AccountID != ttsscheduler.CurrentAccountID {
		t.Fatal("fire backlog blocked standard pool", err)
	}
	snapshot, err = s.TTSPoolSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var running int
	for _, account := range snapshot.Accounts {
		if account.Provider == "volcengine" {
			if account.InFlight != 10 {
				t.Fatal("account cap not shared", account.InFlight)
			}
			running += account.InFlight
		}
	}
	if running != 20 || snapshot.Queued != 1 {
		t.Fatal("bad occupancy or waiting count", running, snapshot.Queued)
	}
}

func TestTTSPoolMySQLRoomPolicyAdmissionAndCAS(t *testing.T) {
	s, _ := ttsPoolFixture(t)
	ctx := t.Context()
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE core_rooms(id BIGINT PRIMARY KEY,tenant_id BIGINT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO core_rooms VALUES(29,14),(30,14),(31,15)`); err != nil {
		t.Fatal(err)
	}
	policy, err := s.GetTTSRoomPoolPolicy(ctx, 14, 29)
	if err != nil || policy.Revision != 0 || len(policy.AllowedPools) != 2 {
		t.Fatal("default compatibility policy", err, policy)
	}
	work := func(id, provider, model, voice string, room int64) ttsscheduler.Work {
		return ttsscheduler.Work{ID: id, Provider: provider, Model: model, VoiceID: voice, Scope: ttsscheduler.Scope{TenantID: 14, RoomID: room}, TTL: time.Minute}
	}
	if err = s.EnqueueTTS(ctx, work("running-ali", "qwen_audio", "qwen-audio-3.1-tts-flash", "voice-a", 29)); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimTTS(ctx, "running-ali")
	if err != nil || !claim.Granted {
		t.Fatal("initial admission", err)
	}
	if err = s.EnqueueTTS(ctx, work("queued-ali", "aliyun_qwen_clone", "qwen-audio-3.1-tts-flash", "voice-a", 29)); err != nil {
		t.Fatal(err)
	}
	policy.AllowedPools = []string{ttsscheduler.PoolHighFidelity}
	saved, err := s.SaveTTSRoomPoolPolicy(ctx, policy, 1)
	if err != nil || saved.Revision != 1 {
		t.Fatal("policy save", err, saved)
	}
	if _, err = s.SaveTTSRoomPoolPolicy(ctx, policy, 2); !errors.Is(err, ttsscheduler.ErrRevision) {
		t.Fatal("stale save accepted", err)
	}
	var running, queued string
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM mgmt_tts_pool_jobs WHERE id='running-ali'`).Scan(&running); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM mgmt_tts_pool_jobs WHERE id='queued-ali'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if running != "running" || queued != "cancelled" {
		t.Fatal("policy must cancel pending only", running, queued)
	}
	if err = s.EnqueueTTS(ctx, work("denied", "aliyun_qwen", "qwen-audio-3.1-tts-flash", "voice-a", 29)); !errors.Is(err, ttsscheduler.ErrPoolDenied) {
		t.Fatal("new disallowed request inserted", err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_tts_pool_jobs WHERE id='denied'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("denial changed queue", err)
	}
	if err = s.EnqueueTTS(ctx, work("other-room", "qwen_audio", "qwen-audio-3.1-tts-flash", "voice-a", 30)); err != nil {
		t.Fatal("another room inherited policy", err)
	}
	if err = s.EnqueueTTS(ctx, work("allowed-fire", "volcengine", "seed-icl-2.0", "S_existing", 29)); err != nil {
		t.Fatal("allowed pool rejected", err)
	}
	// A direct writer or old process cannot bypass the second check at claim.
	if _, err = s.db.ExecContext(ctx, `INSERT INTO mgmt_tts_pool_jobs(id,model,provider,voice_id,tenant_id,room_id,status,queued_at,expires_at,heartbeat_at) VALUES('old-writer','qwen-audio-3.1-tts-flash','qwen_audio','voice-a',14,29,'queued',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE,UTC_TIMESTAMP(6))`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTTS(ctx, "old-writer"); !errors.Is(err, ttsscheduler.ErrPoolDenied) {
		t.Fatal("claim bypassed policy", err)
	}
	for _, bad := range []TTSRoomPoolPolicy{{TenantID: 14, RoomID: 31, AllowedPools: []string{"standard"}}, {TenantID: 14, RoomID: 29, AllowedPools: nil}, {TenantID: 14, RoomID: 29, AllowedPools: []string{"wrong"}}, {TenantID: 14, RoomID: 29, AllowedPools: []string{"standard", "standard"}}} {
		if _, err = s.SaveTTSRoomPoolPolicy(ctx, bad, 1); !errors.Is(err, ttsscheduler.ErrConfig) {
			t.Fatal("invalid scope or list accepted", err)
		}
	}
	// Two administrators reading the same revision cannot overwrite each other.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.SaveTTSRoomPoolPolicy(ctx, saved, 1); errs <- e }()
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, ttsscheduler.ErrRevision) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("lost concurrent update", success, conflict)
	}
	if err = s.MigrateTTSPool(ctx, "current-fixture-key"); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetTTSRoomPoolPolicy(ctx, 14, 29)
	if err != nil || after.Revision != 2 || len(after.AllowedPools) != 1 || after.AllowedPools[0] != "high_fidelity" {
		t.Fatal("migration overwrote policy", after, err)
	}
}
