package anchorcycle

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisLiveFailureIntegration(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR for isolated Redis verification")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	ctx := context.Background()
	store := NewRedis(client, "test-live-failure")
	defer store.EndRoom(ctx, 990081, 990082)
	failure := LiveFailure{SessionID: 990083, Code: "plan_sources_changed", Message: "save and publish"}
	if err := store.SaveLiveFailure(ctx, 990081, 990082, failure); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetLiveFailure(ctx, 990081, 990082, 990083)
	if err != nil || got == nil || got.Message != failure.Message {
		t.Fatal(got, err)
	}
	for _, scope := range [][3]int64{{990091, 990082, 990083}, {990081, 990092, 990083}, {990081, 990082, 990093}} {
		got, err = store.GetLiveFailure(ctx, scope[0], scope[1], scope[2])
		if err != nil || got != nil {
			t.Fatal("failure leaked into another scope", got, err)
		}
	}
	states, err := store.RoomStates(ctx, 990081, 990082)
	if err != nil || len(states) != 0 {
		t.Fatal("failure interpreted as restorable cycle", states, err)
	}
	if ttl := client.TTL(ctx, store.liveFailureKey(990081, 990082, 990083)).Val(); ttl <= 0 || ttl > TTL {
		t.Fatal(ttl)
	}
	restarted := NewRedis(client, "test-live-failure")
	if got, err = restarted.GetLiveFailure(ctx, 990081, 990082, 990083); err != nil || got != nil {
		t.Fatal("new process restored failure", got, err)
	}
	if err = store.EndRoom(ctx, 990081, 990082); err != nil {
		t.Fatal(err)
	}
	if got, err = store.GetLiveFailure(ctx, 990081, 990082, 990083); err != nil || got != nil {
		t.Fatal("room cleanup retained failure", got, err)
	}
}
