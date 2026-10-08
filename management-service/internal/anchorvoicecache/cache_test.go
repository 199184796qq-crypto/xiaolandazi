package anchorvoicecache

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestCacheDefaultAndCleanupScope(t *testing.T) {
	if DefaultTTL != 5*time.Hour {
		t.Fatal(DefaultTTL)
	}
	s := New(nil, "test-realm")
	e := s.NewEntry(900001, Digest("angle"), anchorcycle.ID(), "fixture-bucket", DefaultTTL)
	if err := s.validate(e); err != nil {
		t.Fatal(err)
	}
	if len(ObjectKeys(e)) != 5 {
		t.Fatal(ObjectKeys(e))
	}
	e.ObjectPrefix = "tenants/900001/formal/"
	if s.validate(e) == nil {
		t.Fatal("broad deletion accepted")
	}
}

func TestRedisVoiceCacheSurvivesRestartExpiresWithoutRenewal(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR")
	}
	client := redis.NewClient(anchorcycle.RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	realm := "test-cache-" + anchorcycle.ID()
	s := New(client, realm)
	e := s.NewEntry(900001, Digest("test-angle"), anchorcycle.ID(), "fixture-bucket", DefaultTTL)
	e.Text = "原始话术"
	e.OutputText = "原始话术。"
	e.Bundle = model.LiveAgentPlanVersionVariant{AudioDurationMS: 1000}
	defer s.Forget(context.Background(), e)
	if err := s.Reserve(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	before := client.PTTL(ctx, s.prefix+"entry:"+e.ID).Val()
	if before <= 0 || before > DefaultTTL {
		t.Fatal(before)
	}
	restarted := New(client, realm)
	got, err := restarted.Lookup(ctx, e.Group)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if after := client.PTTL(ctx, s.prefix+"entry:"+e.ID).Val(); after > before {
		t.Fatal("hit extended TTL", after, before)
	}
	if got[0].ExpiresAt != e.ExpiresAt {
		t.Fatal("expiry changed")
	}
	// Expired metadata is refused even if a Redis item lingered. Cleanup records
	// survive cache TTL and process restart until the OSS objects are deleted.
	e.ExpiresAt = time.Now().Add(-time.Second)
	if err := s.Publish(ctx, e); err == nil {
		t.Fatal("expired entry published")
	}
	raw, _ := json.Marshal(e)
	if err := client.Set(ctx, s.prefix+"entry:"+e.ID, raw, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err = restarted.Lookup(ctx, e.Group)
	if err != nil || len(got) != 0 {
		t.Fatal("expired entry reused", got, err)
	}
	due, err := restarted.Due(ctx, 10)
	if err != nil || len(due) != 1 || due[0].ID != e.ID {
		t.Fatal(due, err)
	}
	if err := restarted.Forget(ctx, due[0]); err != nil {
		t.Fatal(err)
	}
	if client.HExists(ctx, s.prefix+"cleanup", e.ID).Val() {
		t.Fatal("cleanup reference retained")
	}
	bad := e
	bad.ID = strings.Repeat("x", 32)
	if err := s.Reserve(ctx, bad); err == nil {
		t.Fatal("invalid id accepted")
	}
}
