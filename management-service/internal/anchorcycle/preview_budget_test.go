package anchorcycle

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/model"
	"os"
	"testing"
	"time"
)

func TestRedisStreamControlledGenerationHasNoPlaybackDeadline(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" { t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR") }
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000)); defer client.Close()
	repo := NewRedis(client, "test-stream-owned-budget")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second); defer cancel()
	state := fixture(t, 3); state.Config.TTS = &VoiceConfig{Rate:1, BufferSeconds:120}
	state.StreamControlled, state.PreviewLimitMS = true, 120000
	state.TenantID, state.PlanID = 900001, 900077
	for _, text := range []string{"完整第一稿。", "完整第二稿。", "完整第三稿。"} { add(t, &state, text, 90) }
	token := ID(); if err := repo.Acquire(ctx,state.TenantID,state.PlanID,0,token); err != nil { t.Fatal(err) }
	defer repo.Release(ctx,state.TenantID,state.PlanID,0,token); defer repo.EndRoom(context.Background(),state.TenantID,0)
	if err := repo.Create(ctx,&state,token); err != nil { t.Fatal(err) }
	for i, segment := range state.Accepted {
		job := ID(); voice, claimed, err := repo.ClaimVoice(ctx,state.TenantID,state.PlanID,0,state.RunID,segment.Selected.ID,job,false)
		if err != nil || !claimed || voice.PreviewReservedMS != 0 { t.Fatal("playback budget restricted complete generation",i,voice,err) }
		duration := int64(40000); if i == 0 { duration = 111367 }
		if err := repo.CompleteVoice(ctx,state.TenantID,state.PlanID,0,state.RunID,segment.Selected.ID,job,VoiceAsset{Status:"ready",Bundle:model.LiveAgentPlanVersionVariant{Text:segment.Selected.Text,AudioDurationMS:duration}}); err != nil { t.Fatal(err) }
	}
	actual, err := repo.Get(ctx,state.TenantID,state.PlanID,0)
	if err != nil || actual.Accepted[1].Voice.Bundle.AudioDurationMS != 40000 { t.Fatal("final complete audio was clipped",err) }
}

func TestRedisPreviewBudgetAtomicClaimsAndDurationChecks(t *testing.T) {
	addr := os.Getenv("ANCHOR_CYCLE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ANCHOR_CYCLE_TEST_REDIS_ADDR")
	}
	client := redis.NewClient(RedisOptions(addr, os.Getenv("REDIS_PASSWORD"), 0, 5000))
	defer client.Close()
	repo := NewRedis(client, "test-preview-budget")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state := fixture(t, 3)
	state.Config.TTS = &VoiceConfig{Rate: 1, BufferSeconds: 120}
	state.PreviewLimitMS = 120000
	state.TenantID = 900001
	state.PlanID = 900004
	state.RoomID = 0
	add(t, &state, "第一句。", 90)
	add(t, &state, "第二句。", 90)
	add(t, &state, "第三句。", 90)
	token := ID()
	if err := repo.Acquire(ctx, state.TenantID, state.PlanID, 0, token); err != nil {
		t.Fatal(err)
	}
	defer repo.Release(ctx, state.TenantID, state.PlanID, 0, token)
	defer repo.EndRoom(context.Background(), state.TenantID, 0)
	if err := repo.Create(ctx, &state, token); err != nil {
		t.Fatal(err)
	}
	first, second, third := state.Accepted[0].Selected.ID, state.Accepted[1].Selected.ID, state.Accepted[2].Selected.ID
	job := ID()
	voice, owned, err := repo.ClaimVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, first, job, false)
	if err != nil || !owned || voice.PreviewReservedMS != 120000 {
		t.Fatal(voice, owned, err)
	}
	if _, _, err = repo.ClaimVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, second, ID(), false); !errors.Is(err, ErrBusy) {
		t.Fatal("parallel budget reservation allowed", err)
	}
	if err = repo.CompleteVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, first, job, VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 70000}}); err != nil {
		t.Fatal(err)
	}
	job = ID()
	voice, owned, err = repo.ClaimVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, second, job, false)
	if err != nil || !owned || voice.PreviewReservedMS != 50000 {
		t.Fatal(voice, owned, err)
	}
	if err = repo.CompleteVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, second, job, VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 60000}}); err == nil {
		t.Fatal("over-limit full asset accepted")
	}
	safeTimeline := []model.LiveAgentPlanTimelineSegment{{StartMS: 0, EndMS: 60000, SafeCut: true}}
	if err = repo.CompleteVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, second, job, VoiceAsset{Status: "ready", PreviewTruncated: true, Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 60000, Timeline: safeTimeline}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repo.ClaimVoice(ctx, state.TenantID, state.PlanID, 0, state.RunID, third, ID(), false); !errors.Is(err, ErrPreviewBudgetExhausted) {
		t.Fatal("budget completion was not typed", err)
	}
}
