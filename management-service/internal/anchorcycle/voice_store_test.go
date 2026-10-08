package anchorcycle

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/model"
)

func TestRedisAsyncVoiceDoesNotBlockOrOverwriteGeneration(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	store := NewRedis(client, "test-async-voice")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := rollingFixture(t, 2)
	s.Config.TTS = &VoiceConfig{Rate: 1, BufferSeconds: 120}
	s.TenantID = 900001
	s.PlanID = 900002
	s.RoomID = 900003
	add(t, &s, "已采纳第一稿", 70)
	defer store.EndRoom(context.Background(), s.TenantID, s.RoomID)
	token := ID()
	if err := store.Acquire(ctx, s.TenantID, s.PlanID, s.RoomID, token); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, &s, token); err != nil {
		t.Fatal(err)
	}
	// Main generation lease remains held, yet the TTS worker can claim its own
	// adopted clip without changing the generation revision or model task.
	id, job := s.Accepted[0].Selected.ID, ID()
	v, claimed, err := store.ClaimVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, job, false)
	if err != nil || !claimed || v.Status != "generating" {
		t.Fatal(v, claimed, err)
	}
	_, claimed, err = store.ClaimVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, ID(), false)
	if err != nil || claimed {
		t.Fatal("duplicate synthesis claimed", err)
	}
	if _, _, err = store.ClaimVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, "unadopted", ID(), false); !errors.Is(err, ErrStale) {
		t.Fatal("unadopted clip claimed", err)
	}
	old, err := store.Get(ctx, s.TenantID, s.PlanID, s.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Revision != s.Revision || old.Accepted[0].Voice.Status != "generating" {
		t.Fatal("voice changed generation revision")
	}
	ready := VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioURL: "https://example.invalid/clip.wav", AudioDurationMS: 12000}}
	if err := store.CompleteVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, job, ready); err != nil {
		t.Fatal(err)
	}
	// Saving a model snapshot taken BEFORE voice completion must retain the new
	// completed asset; no stale entire-state reply may reset it to generating.
	add(t, old, "TTS期间已经生成的下一稿", 70)
	if err := store.Save(ctx, old); err != nil {
		t.Fatal(err)
	}
	actual, err := store.Get(ctx, s.TenantID, s.PlanID, s.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.Accepted) != 2 || actual.Accepted[0].Voice.Status != "ready" || actual.Accepted[0].Voice.Bundle.AudioDurationMS != 12000 || actual.Cursor != 2 {
		t.Fatal("concurrent state overwritten", actual)
	}
	store.Release(ctx, s.TenantID, s.PlanID, s.RoomID, token)
	if err := store.Delete(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID); err != nil {
		t.Fatal(err)
	}
	if client.Exists(ctx, store.voiceKey(s.TenantID, s.PlanID, s.RoomID, s.RunID)).Val() != 0 {
		t.Fatal("voice cache not cleared on end")
	}
	if err := store.CompleteVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, job, ready); !errors.Is(err, ErrStale) {
		t.Fatal("late voice resurrected ended run", err)
	}
}

func TestRedisVoiceFailureRequiresExplicitRetryAndNonce(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	store := NewRedis(client, "test-voice-retry")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := fixture(t, 2)
	s.Config.TTS = &VoiceConfig{}
	s.TenantID = 900001
	s.PlanID = 900002
	s.RoomID = 900003
	add(t, &s, "采纳", 90)
	defer store.EndRoom(context.Background(), s.TenantID, s.RoomID)
	token := ID()
	if err := store.Acquire(ctx, s.TenantID, s.PlanID, s.RoomID, token); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, &s, token); err != nil {
		t.Fatal(err)
	}
	id, first, second := s.Accepted[0].Selected.ID, ID(), ID()
	if _, _, err := store.ClaimVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, first, false); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, first, VoiceAsset{Status: "failed", Error: "provider failed"}); err != nil {
		t.Fatal(err)
	}
	v, claimed, err := store.ClaimVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, second, false)
	if err != nil || claimed || v.Status != "failed" {
		t.Fatal("failure automatically retried")
	}
	if _, claimed, err := store.ClaimVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, second, true); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.CompleteVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, first, VoiceAsset{Status: "ready"}); !errors.Is(err, ErrStale) {
		t.Fatal("old worker overwrote retry", err)
	}
	if err := store.CompleteVoice(ctx, s.TenantID, s.PlanID, s.RoomID, s.RunID, id, second, VoiceAsset{Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.RoomStates(ctx, s.TenantID, s.RoomID)
	if err != nil || len(listed) != 1 {
		t.Fatal("room scan must exclude the voice hash", err)
	}
	// Unscoped-to-run cleanup still resolves the exact stored run, rather than
	// leaving that run's voice hash behind under an empty suffix.
	if err := store.Delete(ctx, s.TenantID, s.PlanID, s.RoomID, ""); err != nil {
		t.Fatal(err)
	}
	if client.Exists(ctx, store.voiceKey(s.TenantID, s.PlanID, s.RoomID, s.RunID)).Val() != 0 {
		t.Fatal("voice cache not cleared when deleting the current run")
	}
}
