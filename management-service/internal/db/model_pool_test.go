package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func TestModelPoolInputAndCredentialMasking(t *testing.T) {
	id, err := modelPoolID()
	if err != nil {
		t.Fatal(err)
	}
	base := ModelPoolEntryInput{ID: id, AccountName: "账号A", Profile: agentgateway.SpeechModelInput{SpeechModel: agentgateway.SpeechModel{ID: id, Protocol: "openai_chat", BaseURL: "https://example.test/v1", Model: "qwen3.7-flash"}}, MaxRooms: 2, Enabled: true}
	if err = validateModelPoolInput(&base); err != nil {
		t.Fatal(err)
	}
	for _, capacity := range []int{-1, 0, 10001} {
		next := base
		next.MaxRooms = capacity
		if validateModelPoolInput(&next) == nil {
			t.Fatalf("invalid enabled capacity %d accepted", capacity)
		}
	}
	base.Profile.APIKey = "fixture-secret-must-not-leak"
	raw, err := json.Marshal(ModelPoolEntry{ID: id, Profile: base.Profile.SpeechModel})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), base.Profile.APIKey) {
		t.Fatal("credential appeared in public entry")
	}
}

// The optional integration environment always creates a random schema through
// the repository's existing fixture. It never inserts synthetic AI into the
// configured business database and does not contact a model provider.
func TestModelPoolMySQLLifecycleAndConcurrentCapacity(t *testing.T) {
	s := salesIsolatedMySQL(t)
	t.Setenv("MODEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("fixture-encryption-key", 3))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_runtime_sessions(id BIGINT PRIMARY KEY,tenant_id BIGINT,room_id BIGINT,status VARCHAR(16),stop_reason VARCHAR(80) NOT NULL DEFAULT '')`)
	exec(`CREATE TABLE core_rooms(id BIGINT PRIMARY KEY,tenant_id BIGINT,name VARCHAR(80),status VARCHAR(16))`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(q), "CREATE TABLE IF NOT EXISTS live_model_exchanges (") {
			exec(q)
		}
	}
	if err := s.MigrateModelPool(ctx); err != nil {
		t.Fatal(err)
	}
	input := func(name, key string, capacity int) ModelPoolEntryInput {
		return ModelPoolEntryInput{AccountName: name, Profile: agentgateway.SpeechModelInput{SpeechModel: agentgateway.SpeechModel{Protocol: "openai_chat", BaseURL: "https://example.test/v1", Model: "qwen3.7-flash"}, NewAPIKey: key}, MaxRooms: capacity, Enabled: true}
	}
	a, err := s.SaveModelPoolEntry(ctx, input("账号A", "fixture-key-A", 2), 27)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveModelPoolEntry(ctx, input("账号B", "fixture-key-B", 1), 27)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveModelPoolEntry(ctx, input("账号A", "another-key-from-same-account", 2), 27); err == nil {
		t.Fatal("same account/model was split into independent capacities")
	}
	if _, err = s.SaveModelPoolEntry(ctx, input("账号A别名", "fixture-key-A", 2), 27); err == nil {
		t.Fatal("same key/model was registered twice")
	}
	for i := int64(1); i <= 4; i++ {
		exec(`INSERT INTO live_runtime_sessions(id,tenant_id,room_id,status) VALUES(?,14,?,'running')`, i, 100+i)
		exec(`INSERT INTO core_rooms(id,tenant_id,name,status) VALUES(?,14,'fixture','live')`, 100+i)
	}
	exec(`INSERT INTO mgmt_tenants(id,name) VALUES(14,'fixture')`)
	var wg sync.WaitGroup
	var snapshots sync.WaitGroup
	snapshots.Add(4)
	start := make(chan struct{})
	results := make(chan error, 4)
	for i := int64(1); i <= 4; i++ {
		wg.Add(1)
		go func(runtime int64) {
			defer wg.Done()
			tx, e := s.db.BeginTx(ctx, nil)
			if e != nil {
				snapshots.Done()
				results <- e
				return
			}
			defer tx.Rollback()
			// Establish a repeatable-read snapshot before allocation waits on
			// another transaction. Stale business snapshots must not overfill X.
			var before int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_model_pool_leases`).Scan(&before); e != nil {
				snapshots.Done()
				results <- e
				return
			}
			snapshots.Done()
			<-start
			e = s.acquireModelPoolTx(ctx, tx, 14, 100+runtime, runtime, time.Now().UTC())
			if e == nil {
				e = tx.Commit()
			}
			results <- e
		}(i)
	}
	snapshots.Wait()
	close(start)
	wg.Wait()
	close(results)
	succeeded, full := 0, 0
	for e := range results {
		if e == nil {
			succeeded++
		} else if errors.Is(e, ErrModelPoolFull) {
			full++
		} else {
			t.Fatal(e)
		}
	}
	if succeeded != 3 || full != 1 {
		t.Fatalf("capacity exceeded or underfilled: success=%d full=%d", succeeded, full)
	}
	leases, err := s.ListModelPoolLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 3 {
		t.Fatal("expected exactly three durable assignments")
	}
	counts := map[string]int{}
	for _, l := range leases {
		counts[l.EntryID]++
	}
	if counts[a.ID] != 2 || counts[b.ID] != 1 {
		t.Fatalf("per-account capacity wrong: %+v", counts)
	}
	chosen := leases[0]
	repeat, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.acquireModelPoolTx(ctx, repeat, 14, chosen.RoomID, chosen.RuntimeID, time.Now().UTC()); err != nil {
		repeat.Rollback()
		t.Fatal(err)
	}
	if err = repeat.Commit(); err != nil {
		t.Fatal(err)
	}
	again, err := s.ListModelPoolLeases(ctx)
	if err != nil || len(again) != 3 || again[0].LeaseID != chosen.LeaseID {
		t.Fatal("repeated start consumed a second slot")
	}
	scope := agentgateway.RoomModelScope{TenantID: 14, RoomID: chosen.RoomID, RuntimeID: chosen.RuntimeID, RequireLease: true}
	scoped := agentgateway.WithRoomModelScope(ctx, scope)
	old, err := s.ResolveSpeechModel(scoped, "unrelated-old-review-profile")
	if err != nil {
		t.Fatal(err)
	}
	if old.ID != chosen.EntryID || old.Model != "qwen3.7-flash" {
		t.Fatal("room did not override old role profile")
	}
	roles, err := s.SpeechModels(scoped)
	if err != nil {
		t.Fatal(err)
	}
	if roles.DefaultID != old.ID || roles.ReviewDefaultID != old.ID || roles.SystemDefaultID != old.ID {
		t.Fatal("speech, review, planning split across credentials")
	}
	id, err := s.BeginModelExchange(scoped, ModelExchangeScope{TenantID: 14, RoomID: chosen.RoomID}, "mainline_final_content_review", "old-review", agentgateway.Request{Stage: "mainline_final_content_review"})
	if err != nil {
		t.Fatal(err)
	}
	var traceEntry, traceLease string
	if err = s.db.QueryRowContext(ctx, `SELECT pool_entry_id,pool_lease_id FROM live_model_exchanges WHERE id=?`, id).Scan(&traceEntry, &traceLease); err != nil || traceEntry != chosen.EntryID || traceLease != chosen.LeaseID {
		t.Fatal("request not attributed to actual allocation")
	}
	entries, err := s.ListModelPoolEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var editable ModelPoolEntry
	for _, e := range entries {
		if e.ID == chosen.EntryID {
			editable = e
		}
	}
	changed := ModelPoolEntryInput{ID: editable.ID, AccountName: editable.AccountName, Profile: agentgateway.SpeechModelInput{SpeechModel: editable.Profile, NewAPIKey: "fixture-rotated-key"}, MaxRooms: editable.MaxRooms, Enabled: false, Revision: editable.Revision}
	stale := changed
	stale.Revision++
	if _, err = s.SaveModelPoolEntry(ctx, stale, 27); !errors.Is(err, ErrModelPoolRevision) {
		t.Fatal("stale editor changed pool capacity")
	}
	tooSmall := changed
	tooSmall.MaxRooms = 0
	if _, err = s.SaveModelPoolEntry(ctx, tooSmall, 27); err == nil {
		t.Fatal("capacity reduced below active allocations")
	}
	if _, err = s.SaveModelPoolEntry(ctx, changed, 27); err != nil {
		t.Fatal(err)
	}
	still, err := s.ResolveSpeechModel(scoped, "")
	if err != nil || still.APIKey != old.APIKey {
		t.Fatal("credential rotation or disabling changed an active snapshot")
	}
	if err = s.DeleteModelPoolEntry(ctx, editable.ID, editable.Revision+1); !errors.Is(err, ErrModelPoolInUse) {
		t.Fatal("active account was removable")
	}
	exec(`UPDATE core_rooms SET status='stopped' WHERE id=?`, chosen.RoomID)
	exec(`UPDATE live_runtime_sessions SET status='paused' WHERE id=?`, chosen.RuntimeID)
	restarted := &Store{db: s.db}
	if err = restarted.MigrateModelPool(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.ListModelPoolLeases(ctx)
	if err != nil || len(restored) != 3 {
		t.Fatal("room offline, pause or Management restart released a running AI allocation")
	}
	if _, err = restarted.ResolveSpeechModel(scoped, ""); err != nil {
		t.Fatal("paused AI lost its model")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='stopped',stop_reason='manual_stop' WHERE id=?`, chosen.RuntimeID); err != nil {
		t.Fatal(err)
	}
	if err = s.releaseStoppedModelPoolTx(ctx, tx, chosen.RuntimeID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSpeechModel(scoped, ""); !errors.Is(err, ErrModelPoolLeaseMissing) {
		t.Fatal("late stopped-session work could still call the provider")
	}
	// Reenable the entry and reuse the freed slot. A late stop for the old
	// runtime must not release the replacement room's new allocation.
	entries, err = s.ListModelPoolEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == editable.ID {
			changed.Revision = e.Revision
			changed.Enabled = true
			changed.Profile.NewAPIKey = ""
		}
	}
	if _, err = s.SaveModelPoolEntry(ctx, changed, 27); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO live_runtime_sessions(id,tenant_id,room_id,status) VALUES(10,14,?,'running')`, chosen.RoomID)
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.acquireModelPoolTx(ctx, tx, 14, chosen.RoomID, 10, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.releaseModelPoolTx(ctx, tx, chosen.RuntimeID, "late_old_stop", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	newScope := agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: 14, RoomID: chosen.RoomID, RuntimeID: 10, RequireLease: true})
	newProfile, err := s.ResolveSpeechModel(newScope, "")
	if err != nil || newProfile.APIKey != "fixture-rotated-key" {
		t.Fatal("replacement lost its slot or did not use new credential")
	}
	if _, err = s.ResolveSpeechModel(scoped, ""); !errors.Is(err, ErrModelPoolLeaseMissing) {
		t.Fatal("old runtime crossed into the replacement lease")
	}
	public, _ := json.Marshal(entries)
	if strings.Contains(string(public), "fixture-key") || strings.Contains(string(public), "fixture-rotated-key") {
		t.Fatal("credential leaked from pool listing")
	}
}
