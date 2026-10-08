package anchorcycle

import (
	"context"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
	"time"
)

func TestStartupMilestonesNeverClaimCompletedPlayback(t *testing.T) {
	last := 0
	for _, stage := range []string{"reading_plan", "connecting_service", "preparing_content", "checking_content", "generating_voice", "voice_ready", "sending_audio", "waiting_playback"} {
		p := StartupPercent(stage)
		if p <= last || p >= 100 {
			t.Fatal(stage, p)
		}
		last = p
	}
	if StartupPercent("internal_secret_name") != 0 {
		t.Fatal("unknown stage accepted")
	}
}
func TestRedisLiveStartupIntegration(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("explicit isolated Redis verification required")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	ctx := context.Background()
	store := NewRedis(client, "test-live-startup")
	defer store.EndRoom(ctx, 990181, 990182)
	put := func(stage string) {
		t.Helper()
		err := store.SaveLiveStartup(ctx, 990181, 990182, LiveStartup{SessionID: 990183, Stage: stage, Percent: StartupPercent(stage), UpdatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	put("generating_voice")
	put("preparing_content")
	p, err := store.GetLiveStartup(ctx, 990181, 990182, 990183)
	if err != nil || p == nil || p.Stage != "generating_voice" {
		t.Fatal("regressed milestone", p, err)
	}
	for _, scope := range [][3]int64{{990191, 990182, 990183}, {990181, 990192, 990183}, {990181, 990182, 990193}} {
		p, err = store.GetLiveStartup(ctx, scope[0], scope[1], scope[2])
		if err != nil || p != nil {
			t.Fatal("scope leak", p, err)
		}
	}
	states, err := store.RoomStates(ctx, 990181, 990182)
	if err != nil || len(states) != 0 {
		t.Fatal("startup became restorable state", states, err)
	}
	if ttl := client.TTL(ctx, store.liveStartupKey(990181, 990182, 990183)).Val(); ttl <= 0 || ttl > TTL {
		t.Fatal(ttl)
	}
	if p, err = NewRedis(client, "test-live-startup").GetLiveStartup(ctx, 990181, 990182, 990183); err != nil || p != nil {
		t.Fatal("restart restored status", p, err)
	}
	if err = store.EndRoom(ctx, 990181, 990182); err != nil {
		t.Fatal(err)
	}
	if p, err = store.GetLiveStartup(ctx, 990181, 990182, 990183); err != nil || p != nil {
		t.Fatal("cleanup retained status", p, err)
	}
}
