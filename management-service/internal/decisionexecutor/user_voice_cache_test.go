package decisionexecutor

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"testing"
	"time"
)

type tenantFAQPolicyStore struct {
	*faqTestStore
	enabled map[int64]bool
	fail    bool
	hours   int
}

func (s *tenantFAQPolicyStore) GenerationCacheReuseEnabledForTenant(_ context.Context, tenant int64, key string) (bool, error) {
	if s.fail {
		return false, errors.New("policy unavailable")
	}
	if key != model.InteractionGenerationCacheReuseSetting {
		return false, errors.New("wrong policy")
	}
	return s.enabled[tenant], nil
}
func (s *tenantFAQPolicyStore) AnchorVoiceCacheRetentionHoursForTenant(context.Context, int64) (int, error) {
	return s.hours, nil
}

func TestUserVoiceCacheInteractionScopesAndLifetime(t *testing.T) {
	w, base, _, _, tts, now := newFAQTestWorker()
	store := &tenantFAQPolicyStore{faqTestStore: base, enabled: map[int64]bool{14: true, 15: false}, hours: 1}
	w.store = store
	ctx := context.Background()
	if !w.interactionCacheReuseEnabled(ctx, 14) || w.interactionCacheReuseEnabled(ctx, 15) {
		t.Fatal("interaction ignored tenant scope")
	}
	key := "fixture-user-14"
	expiry := now.Add(12 * time.Hour)
	request := ttsgateway.SynthesizeRequest{Text: "已确认的回答。", VoiceID: "voice-1"}
	w.faqCache.buckets[key] = []faqAnswerVariant{{Text: request.Text, ExpiresAt: expiry}}
	item := &decisionItem{faqCacheTicket: &faqCacheTicket{TenantID: 14, Key: key, Text: request.Text, ExpiresAt: expiry}}
	if _, err := w.synthesizeFAQAudio(ctx, item, request); err != nil {
		t.Fatal(err)
	}
	if _, err := w.synthesizeFAQAudio(ctx, item, request); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 1 {
		t.Fatal("enabled user failed to reuse existing audio")
	}
	until := w.faqCache.buckets[key][0].AudioUntil
	if until.After(now.Add(time.Hour)) {
		t.Fatal("audio exceeded user's retention", until)
	}
	store.enabled[14] = false
	if _, err := w.synthesizeFAQAudio(ctx, item, request); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 2 || len(w.faqCache.buckets[key]) != 1 {
		t.Fatal("switch off did not bypass cache or removed history")
	}
	store.enabled[14] = true
	store.fail = true
	if _, err := w.synthesizeFAQAudio(ctx, item, request); err != nil {
		t.Fatal("failed policy stopped fresh synthesis", err)
	}
	if tts.calls != 3 {
		t.Fatal("failed policy reused audio")
	}
}
