package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type faqTestStore struct {
	*fakeStore
	policy    model.LiveContentPolicyRecord
	policyErr error
	layer     *model.LivePolicyVersion
}

func (s *faqTestStore) GetLiveContentPolicy(context.Context, int64, int64) (model.LiveContentPolicyRecord, error) {
	return s.policy, s.policyErr
}

func (s *faqTestStore) LoadLivePolicyLayers(context.Context, int64, int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error) {
	return "general", s.layer, nil, nil, nil
}

func newFAQTestWorker() (*Worker, *faqTestStore, *fakeCore, *fakeAgent, *fakeTTS, *time.Time) {
	store := &faqTestStore{fakeStore: readyVoiceStore(), policy: model.LiveContentPolicyRecord{Policy: model.DefaultLiveContentPolicy()}}
	store.policy.Policy.FAQVariantCount = 1
	store.policy.Policy.MinRepeatSeconds = 0
	core, agent, tts := &fakeCore{}, &fakeAgent{}, &fakeTTS{}
	worker := New(store, core, agent, tts)
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }
	return worker, store, core, agent, tts, &now
}

func executeFAQ(t *testing.T, worker *Worker, core *fakeCore, roomID int64, question string) {
	t.Helper()
	claim := claimResponse{Claimed: true, Item: &decisionItem{
		ID: fmt.Sprintf("faq-%d", core.dispatches+1), Topic: "FAMILY:价格费用", Title: "商品咨询", MissionKind: "reply_chat",
		SampleQuestions: []string{question},
	}}
	data, err := json.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	core.claimRaw = string(data)
	if err := worker.processRoom(context.Background(), model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: roomID, Status: "running"}); err != nil {
		t.Fatal(err)
	}
}

func TestFAQProcessRoomCachesSafeParaphrasesWithoutSlidingExpiry(t *testing.T) {
	worker, store, core, agent, tts, now := newFAQTestWorker()
	if store.policy.Policy.FAQTTLSeconds != 7200 {
		t.Fatalf("default FAQ TTL=%d", store.policy.Policy.FAQTTLSeconds)
	}
	executeFAQ(t, worker, core, 11, "多少钱？")
	*now = now.Add(119 * time.Minute)
	executeFAQ(t, worker, core, 11, "价格是多少")
	if agent.calls != 1 || tts.calls != 1 || core.dispatches != 2 {
		t.Fatalf("safe FAQ should reuse text+audio: llm=%d tts=%d dispatch=%d", agent.calls, tts.calls, core.dispatches)
	}
	*now = now.Add(time.Minute)
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 2 || tts.calls != 2 {
		t.Fatalf("2h expiry must regenerate despite recent hit: llm=%d tts=%d", agent.calls, tts.calls)
	}
}

func TestFAQProcessRoomInvalidatesDependenciesAndIsolatesRooms(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*faqTestStore)
	}{
		{"fact", func(s *faqTestStore) {
			s.facts = []model.LiveAgentPlanFact{{Status: "active", Key: "物流", Value: "按页面说明发货", VersionNo: 2}}
		}},
		{"temporary host state", func(s *faqTestStore) { s.humanProfile.StateText = "今天声音轻一点" }},
		{"rules", func(s *faqTestStore) {
			s.layer = &model.LivePolicyVersion{ID: 8, VersionNo: 2, SourceText: "新规则"}
		}},
		{"policy revision", func(s *faqTestStore) { s.policy.SystemRevision++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker, store, core, agent, _, _ := newFAQTestWorker()
			executeFAQ(t, worker, core, 11, "多少钱")
			tt.mutate(store)
			executeFAQ(t, worker, core, 11, "多少钱")
			if agent.calls != 2 {
				t.Fatalf("changed %s reused cached answer: %d", tt.name, agent.calls)
			}
		})
	}
	worker, _, core, agent, _, _ := newFAQTestWorker()
	executeFAQ(t, worker, core, 11, "多少钱")
	executeFAQ(t, worker, core, 12, "多少钱")
	if agent.calls != 2 {
		t.Fatal("another room reused answer")
	}
}

func TestFAQProcessRoomVoiceChangeResynthesizesAndPolicyFailureNeverReuses(t *testing.T) {
	worker, store, core, agent, tts, _ := newFAQTestWorker()
	executeFAQ(t, worker, core, 11, "多少钱")
	store.profiles[0].VoiceID = "changed-voice"
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 1 || tts.calls != 2 || tts.last.VoiceID != "changed-voice" {
		t.Fatalf("voice change must regenerate audio: llm=%d tts=%d voice=%s", agent.calls, tts.calls, tts.last.VoiceID)
	}
	store.policyErr = errors.New("policy read failed")
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 2 || tts.calls != 3 {
		t.Fatal("policy failure reused stale cache")
	}
}

func TestFAQProcessRoomFillsVariantsAndHonorsRepeatCooldown(t *testing.T) {
	worker, store, core, agent, tts, now := newFAQTestWorker()
	store.policy.Policy.FAQVariantCount = 2
	store.policy.Policy.MinRepeatSeconds = 1200
	agent.responses = []string{"这个问题我们给大家讲清楚，请以当前页面的说明为准。", "大家关心的这个问题，看当前页面的说明就可以了。", "这位朋友，当前页面已经写明了，请按页面说明来。"}
	executeFAQ(t, worker, core, 11, "多少钱")
	*now = now.Add(time.Minute)
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 2 {
		t.Fatalf("variant pool was not filled: %d", agent.calls)
	}
	*now = now.Add(19 * time.Minute)
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 2 || tts.calls != 2 {
		t.Fatal("cooled variant was not reused")
	}
	*now = now.Add(time.Second)
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 3 {
		t.Fatal("all variants cooling down should generate a fresh answer")
	}
	if !strings.Contains(agent.requests[2].Messages[1].Content, "禁止逐字重复") {
		t.Fatal("generation lacks previous-variant constraint")
	}
	for _, variants := range worker.faqCache.buckets {
		if len(variants) > 2 {
			t.Fatal("variant pool is unbounded")
		}
	}
}

func TestFAQProcessRoomNeverDispatchesDuplicateDuringCooldown(t *testing.T) {
	worker, store, core, agent, tts, _ := newFAQTestWorker()
	store.policy.Policy.MinRepeatSeconds = 1200
	executeFAQ(t, worker, core, 11, "多少钱")
	err := worker.processRoom(context.Background(), model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"})
	if err == nil || !strings.Contains(err.Error(), "冷却期") {
		t.Fatalf("identical fresh model output must be rejected during cooldown: %v", err)
	}
	if agent.calls != 2 || tts.calls != 1 || core.dispatches != 1 || core.releases < 1 {
		t.Fatalf("duplicate must not reach TTS/dispatch and must release claim: llm=%d tts=%d dispatch=%d releases=%d", agent.calls, tts.calls, core.dispatches, core.releases)
	}
}

func TestFAQDoesNotReusePersonalizedOrDifferentQualifiedQuestions(t *testing.T) {
	worker, _, core, agent, _, _ := newFAQTestWorker()
	executeFAQ(t, worker, core, 11, "1号链接多少钱")
	executeFAQ(t, worker, core, 11, "2号链接多少钱")
	if agent.calls != 2 {
		t.Fatal("different products collapsed into same FAQ")
	}
	for _, item := range []decisionItem{
		{MissionKind: "welcome_named", SampleQuestions: []string{"多少钱"}},
		{ManualOrigin: "agent_input", SampleQuestions: []string{"多少钱"}},
		{ManualOrigin: "test_simulation", SampleQuestions: []string{"多少钱"}},
		{Nicknames: []string{"小陈"}, SampleQuestions: []string{"多少钱"}},
		{ExecutionMode: "verbatim", SampleQuestions: []string{"多少钱"}},
	} {
		if faqCacheEligible(item) {
			t.Fatalf("personalized/manual item cacheable: %+v", item)
		}
	}
}

func TestFAQAudioExpiryNeverOutlivesSignedURL(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	content := now.Add(2 * time.Hour)
	tests := []struct {
		name, raw string
		want      time.Time
	}{
		{"unsigned", "https://audio.local/reply.wav", content},
		{"oss", fmt.Sprintf("https://audio.local/reply.wav?Expires=%d&Signature=x", now.Add(5*time.Minute).Unix()), now.Add(4 * time.Minute)},
		{"s3", "https://audio.local/reply.wav?X-Amz-Date=20261004T000000Z&X-Amz-Expires=300&X-Amz-Signature=x", now.Add(4 * time.Minute)},
		{"ossv4", "https://audio.local/reply.wav?x-oss-date=20261004T000000Z&x-oss-expires=300", now.Add(4 * time.Minute)},
		{"unknown", "https://audio.local/reply.wav?token=x", time.Time{}},
		{"invalid expiry", "https://audio.local/reply.wav?Expires=invalid", time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := faqAudioExpiry(tt.raw, content); !got.Equal(tt.want) {
				t.Fatalf("expiry=%s want %s", got, tt.want)
			}
		})
	}
}

type faqSignedTTS struct {
	fakeTTS
	now *time.Time
}

func (s *faqSignedTTS) SynthesizeURL(ctx context.Context, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	response, err := s.fakeTTS.SynthesizeURL(ctx, request)
	response.AudioURL = fmt.Sprintf("https://audio.local/faq.wav?Expires=%d&Signature=test", s.now.Add(5*time.Minute).Unix())
	return response, err
}

func TestFAQProcessRoomRefreshesExpiringSignedAudioWithoutExtendingText(t *testing.T) {
	worker, _, core, agent, _, now := newFAQTestWorker()
	tts := &faqSignedTTS{now: now}
	worker.tts = tts
	executeFAQ(t, worker, core, 11, "多少钱")
	var originalExpiry time.Time
	for _, variants := range worker.faqCache.buckets {
		originalExpiry = variants[0].ExpiresAt
	}
	*now = now.Add(3 * time.Minute)
	executeFAQ(t, worker, core, 11, "多少钱")
	if tts.calls != 1 {
		t.Fatal("valid signed audio should be reused")
	}
	*now = now.Add(time.Minute)
	executeFAQ(t, worker, core, 11, "多少钱")
	if agent.calls != 1 || tts.calls != 2 {
		t.Fatalf("expired signed URL must refresh only audio: llm=%d tts=%d", agent.calls, tts.calls)
	}
	for _, variants := range worker.faqCache.buckets {
		if !variants[0].ExpiresAt.Equal(originalExpiry) {
			t.Fatal("audio renewal extended text lifetime")
		}
	}
}

func TestFAQCacheBoundedAndEvictedVariantsKeepRepeatCooldown(t *testing.T) {
	worker, store, _, _, _, now := newFAQTestWorker()
	store.policy.Policy.MinRepeatSeconds = 1200
	item := decisionItem{SampleQuestions: []string{"多少钱"}}
	session := model.LiveRuntimeSession{TenantID: 7, RoomID: 11, ID: 9}
	first := worker.prepareFAQCache(context.Background(), session, item, "system", "prompt")
	if err := worker.rememberFAQText(first, "第一种说法"); err != nil {
		t.Fatal(err)
	}
	second := worker.prepareFAQCache(context.Background(), session, item, "system", "prompt")
	if second.Hit {
		t.Fatal("recent answer reused")
	}
	if err := worker.rememberFAQText(second, "第二种说法"); err != nil {
		t.Fatal(err)
	}
	third := worker.prepareFAQCache(context.Background(), session, item, "system", "prompt")
	if err := worker.rememberFAQText(third, "第一种说法"); err == nil {
		t.Fatal("evicted variant lost cooldown")
	}
	for i := 0; i < maxFAQCacheBuckets+20; i++ {
		ticket := &faqCacheTicket{Key: fmt.Sprint(i), Policy: store.policy.Policy, GeneratedAt: *now, ExpiresAt: now.Add(2 * time.Hour)}
		if err := worker.rememberFAQText(ticket, "回答"); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Second)
	}
	if len(worker.faqCache.buckets) != maxFAQCacheBuckets || len(worker.faqCache.recent) > maxFAQCacheBuckets {
		t.Fatal("FAQ cache grew beyond its bound")
	}
}

func TestFAQNicknameWithoutSelectedAddressingCanReuseButNamedReplyCannot(t *testing.T) {
	worker, _, _, _, _, _ := newFAQTestWorker()
	session := model.LiveRuntimeSession{TenantID: 7, RoomID: 11}
	seenNamed, seenGeneric := false, false
	for i := 0; i < 100; i++ {
		item := decisionItem{ID: fmt.Sprint(i), MissionKind: "reply_chat", SampleQuestions: []string{"多少钱"}, Nicknames: []string{"小陈"}}
		worker.applyAddressingStrategyStage(context.Background(), session, item.ID, &item)
		if item.faqPersonalized {
			seenNamed = true
			if faqCacheEligible(item) {
				t.Fatal("named answer became reusable")
			}
		} else {
			seenGeneric = true
			if !faqCacheEligible(item) {
				t.Fatal("nonpersonalized FAQ rejected solely because source event carried a nickname")
			}
		}
	}
	if !seenNamed || !seenGeneric {
		t.Fatal("test did not exercise both addressing choices")
	}
}
