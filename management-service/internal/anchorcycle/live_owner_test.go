package anchorcycle

import (
	"context"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
	"time"
)

func TestRedisLiveOwnerSharedLivenessDoesNotRecoverRunState(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	realm := "test-live-owner-" + ID()
	first, second := NewRedis(client, realm), NewRedis(client, realm)
	const tenant, room, runtime int64 = 900001, 900003, 900010
	token, other := ID(), ID()
	defer first.ReleaseLiveOwner(context.Background(), tenant, room, runtime, token)
	if claimed, err := first.ClaimLiveOwner(ctx, tenant, room, runtime, token); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if claimed, err := second.ClaimLiveOwner(ctx, tenant, room, runtime, other); err != nil || claimed {
		t.Fatal("duplicate producer allowed", claimed, err)
	}
	if alive, err := second.LiveOwnerActive(ctx, tenant, room, runtime); err != nil || !alive {
		t.Fatal("cross-node owner invisible", alive, err)
	}
	if renewed, err := second.RenewLiveOwner(ctx, tenant, room, runtime, other); err != nil || renewed {
		t.Fatal("foreign lease renewed", renewed, err)
	}
	if _, err := second.Get(ctx, tenant, 900002, room); err != ErrMissing {
		t.Fatal("shared liveness restored progress", err)
	}
	if err := second.ReleaseLiveOwner(ctx, tenant, room, runtime, other); err != nil {
		t.Fatal(err)
	}
	if alive, _ := first.LiveOwnerActive(ctx, tenant, room, runtime); !alive {
		t.Fatal("wrong token removed owner")
	}
	if renewed, err := first.RenewLiveOwner(ctx, tenant, room, runtime, token); err != nil || !renewed {
		t.Fatal(renewed, err)
	}
	if err := first.ReleaseLiveOwner(ctx, tenant, room, runtime, token); err != nil {
		t.Fatal(err)
	}
	if alive, _ := second.LiveOwnerActive(ctx, tenant, room, runtime); alive {
		t.Fatal("released owner survived")
	}
}
