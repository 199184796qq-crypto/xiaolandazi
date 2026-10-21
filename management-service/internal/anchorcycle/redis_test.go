package anchorcycle

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Opt-in real Redis checks use a fresh random boot namespace, never existing
// broadcast keys. The implementation itself has no database or disk fallback.
func TestRedisCycleIntegration(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR for real Redis verification")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewRedis(client, "test-cycle")
	state := fixture(t, 2)
	state.TenantID = 900001
	state.PlanID = 900002
	state.RoomID = 900003
	// Candidate/score traces already exceed 150KB in real workbench sessions.
	// Exercise the same order of payload through the actual forwarded endpoint.
	state.StyleMD = strings.Repeat("大循环状态传输测试。", 7000)
	defer store.EndRoom(context.Background(), state.TenantID, state.RoomID)
	token := ID()
	if err := store.Acquire(ctx, state.TenantID, state.PlanID, state.RoomID, token); err != nil {
		t.Fatal(err)
	}
	if err := store.Acquire(ctx, state.TenantID, state.PlanID, state.RoomID, ID()); !errors.Is(err, ErrBusy) {
		t.Fatalf("duplicate request accepted: %v", err)
	}
	if err := store.Create(ctx, &state, token); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(ctx, state.TenantID, state.PlanID, state.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StyleMD != state.StyleMD {
		t.Fatal("large state payload truncated")
	}
	for probe := 0; probe < 5; probe++ {
		started := time.Now()
		if _, err = store.Get(ctx, state.TenantID, state.PlanID, state.RoomID); err != nil {
			t.Fatal(err)
		}
		t.Logf("large state read %d: %s", probe+1, time.Since(started))
	}
	if err = store.Save(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	if err = store.Save(ctx, &state); !errors.Is(err, ErrStale) {
		t.Fatal("stale revision allowed")
	}
	listed, listErr := store.RoomStates(ctx, state.TenantID, state.RoomID)
	if listErr != nil || len(listed) != 1 || listed[0].RunID != state.RunID {
		t.Fatalf("room states included leases or missed state: %v %v", listed, listErr)
	}
	if expiry := client.TTL(ctx, store.key(state.TenantID, state.PlanID, state.RoomID)).Val(); expiry <= 0 || expiry > TTL {
		t.Fatal(expiry)
	}
	// Restart means a new namespace, not restoration of the retained Redis key.
	restarted := NewRedis(client, "test-cycle")
	if _, err = restarted.Get(ctx, state.TenantID, state.PlanID, state.RoomID); !errors.Is(err, ErrMissing) {
		t.Fatal("restart resumed old run")
	}
	// Releasing a lease with an unrelated token must not unlock the actual call.
	store.Release(ctx, state.TenantID, state.PlanID, state.RoomID, "wrong-token")
	if err = store.Acquire(ctx, state.TenantID, state.PlanID, state.RoomID, ID()); !errors.Is(err, ErrBusy) {
		t.Fatal("wrong token unlocked request")
	}
	if err = store.Delete(ctx, state.TenantID, state.PlanID, state.RoomID, "other-run"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, state.TenantID, state.PlanID, state.RoomID); err != nil {
		t.Fatal("ending another run deleted this one")
	}
	if err = store.Delete(ctx, state.TenantID, state.PlanID, state.RoomID, state.RunID); err != nil {
		t.Fatal(err)
	}
	if err = store.Save(ctx, loaded); !errors.Is(err, ErrStale) {
		t.Fatal("late completion recreated ended run")
	}
	// An end during session creation revokes the lease, so late Create is refused.
	late := fixture(t, 1)
	late.TenantID = state.TenantID
	late.RoomID = state.RoomID
	late.PlanID = state.PlanID
	if err = store.Acquire(ctx, late.TenantID, late.PlanID, late.RoomID, token); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, late.TenantID, late.PlanID, late.RoomID, state.RunID); err != nil {
		t.Fatal(err)
	}
	if err = store.Acquire(ctx, late.TenantID, late.PlanID, late.RoomID, ID()); !errors.Is(err, ErrBusy) {
		t.Fatal("duplicate end for old run revoked a new start")
	}
	if err = store.EndRoom(ctx, late.TenantID, late.RoomID); err != nil {
		t.Fatal(err)
	}
	if err = store.Create(ctx, &late, token); !errors.Is(err, ErrStale) {
		t.Fatal("late start recreated ended room")
	}
}

func TestRedisRollingReentryStateSurvivesSameProcessReload(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR for real Redis verification")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewRedis(client, "test-opening")
	s := rollingFixture(t, 2)
	s.TenantID = 900001
	s.PlanID = 900002
	s.RoomID = 900005
	defer store.EndRoom(context.Background(), s.TenantID, s.RoomID)
	token := ID()
	if err := store.Acquire(ctx, s.TenantID, s.PlanID, s.RoomID, token); err != nil {
		t.Fatal(err)
	}
	defer store.Release(context.Background(), s.TenantID, s.PlanID, s.RoomID, token)
	if err := store.Create(ctx, &s, token); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		add(t, &s, "真实Redis保存的采纳稿", 70)
		if err := store.Save(ctx, &s); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := store.Get(ctx, s.TenantID, s.PlanID, s.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != "opening" || loaded.CycleNumber != 2 || loaded.OpeningCount != 1 || loaded.OpeningVersion() != "接新版" || loaded.Task() != s.Task() || loaded.SessionID != s.SessionID || len(loaded.Used) != 3 {
		t.Fatal("Redis reload lost cross-cycle state")
	}
}

func TestRedisRecoversUnknownGeneration(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("real Redis opt-in")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewRedis(client, "test-cycle")
	s := fixture(t, 1)
	s.TenantID = 900001
	s.RoomID = 900004
	s.PlanID = 900002
	defer store.EndRoom(context.Background(), s.TenantID, s.RoomID)
	token := ID()
	if err := store.Acquire(ctx, s.TenantID, s.PlanID, s.RoomID, token); err != nil {
		t.Fatal(err)
	}
	// 宏观回灌已下线：Busy 期间没有「可选的后置调用」可以重放，租约丢失
	// 一律视为上一轮结果未确认，不能当作已完成继续。
	s.Busy = true
	if err := store.Create(ctx, &s, token); err != nil {
		t.Fatal(err)
	}
	store.Release(ctx, s.TenantID, s.PlanID, s.RoomID, token)
	recovered, err := store.Get(ctx, s.TenantID, s.PlanID, s.RoomID)
	if err != nil || recovered.Busy || !recovered.Uncertain {
		t.Fatalf("unknown generation could repeat: %v %v", recovered, err)
	}
}
