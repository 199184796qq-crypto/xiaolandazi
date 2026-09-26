package roomintel

import (
	"testing"
	"time"
)

func TestHotRoomPrefersAggregateQuestions(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	for i := 0; i < 50; i++ {
		p.Add(Event{Type: EventChat, UserID: "u", Topic: "PRICE", Content: "多少钱", Question: true, OccurredAt: now.Add(-10 * time.Second)})
	}
	for i := 0; i < 80; i++ {
		p.Add(Event{Type: EventEnter, OccurredAt: now.Add(-10 * time.Second)})
	}
	p.Add(Event{Type: EventEnter, Online: 100, OccurredAt: now})
	s := p.Snapshot(now, 6)
	if s.Heat != HeatOverheated {
		t.Fatalf("heat=%s", s.Heat)
	}
	if !s.PreferAggregateQNA || s.PreferOneToOneQNA {
		t.Fatalf("snapshot=%#v", s)
	}
	if len(s.TopTopics) == 0 || s.TopTopics[0].Topic != "FAMILY:价格费用" {
		t.Fatalf("topics=%#v", s.TopTopics)
	}
}

func TestColdRoomPrefersOneToOne(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	p.Add(Event{Type: EventEnter, Online: 20, OccurredAt: now})
	s := p.Snapshot(now, 6)
	if s.Heat != HeatCold || !s.PreferOneToOneQNA {
		t.Fatalf("snapshot=%#v", s)
	}
}

func TestSessionCountersDoNotShrinkWhenRollingWindowExpires(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	old := now.Add(-20 * time.Minute)
	p.Add(Event{Type: EventEnter, OccurredAt: old})
	p.Add(Event{Type: EventChat, OccurredAt: old})
	p.Add(Event{Type: EventLike, Count: 12, OccurredAt: old})
	p.Add(Event{Type: EventFollow, OccurredAt: old})
	p.Add(Event{Type: EventGift, OccurredAt: old})

	s := p.Snapshot(now, 6)
	if s.SessionEntries != 1 || s.SessionChats != 1 || s.SessionLikes != 12 || s.SessionFollows != 1 || s.SessionGifts != 1 {
		t.Fatalf("session counters shrank with rolling window: %#v", s)
	}
	if s.Entries30s != 0 || s.Chats30s != 0 || s.Likes30s != 0 || s.Follows30s != 0 {
		t.Fatalf("rolling counters should still expire independently: %#v", s)
	}
}

func TestQuestionBucketsPersistForWholeSession(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	p.Add(Event{Type: EventChat, UserID: "u1", Content: "在哪个地方", Question: true, OccurredAt: now.Add(-12 * time.Minute)})
	p.Add(Event{Type: EventChat, UserID: "u2", Content: "地址在哪里", Question: true, OccurredAt: now.Add(-11 * time.Minute)})
	p.Add(Event{Type: EventChat, UserID: "u3", Content: "多少钱", Question: true, OccurredAt: now.Add(-7 * time.Minute)})
	p.Add(Event{Type: EventChat, UserID: "u4", Content: "价格多少", Question: true, OccurredAt: now.Add(-6 * time.Minute)})
	p.Add(Event{Type: EventChat, UserID: "u5", Content: "配料怎么样", Question: true, OccurredAt: now.Add(-time.Minute)})

	s := p.Snapshot(now, 6)
	if len(s.TopTopics) != 3 {
		t.Fatalf("session question buckets not preserved: %#v", s.TopTopics)
	}
	seen := map[string]TopicBucket{}
	for _, bucket := range s.TopTopics {
		seen[bucket.Topic] = bucket
	}
	if seen["Q:地点"].Count != 2 || seen["FAMILY:价格费用"].Count != 2 {
		t.Fatalf("unexpected dynamic session buckets: %#v", s.TopTopics)
	}
	if seen["Q:地点"].SampleQuestions[0] != "在哪个地方" {
		t.Fatalf("bucket label must keep first real question: %#v", seen["Q:地点"])
	}
	if seen["Q:配料怎么样"].Count != 1 || seen["Q:配料怎么样"].SampleQuestions[0] != "配料怎么样" {
		t.Fatalf("first real question should be visible immediately: %#v", s.TopTopics)
	}
	if s.QuestionCount30s != 0 {
		t.Fatalf("rolling question pressure should still expire independently: %#v", s)
	}
}

func TestRealtimeQuestionBucketsDropDormantContextAfterTenHours(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	p := NewPool()
	p.Add(Event{EventID: 1, Type: EventChat, UserID: "old-price", Content: "多少钱", Question: true, OccurredAt: now.Add(-11 * time.Hour)})
	p.Add(Event{EventID: 2, Type: EventChat, UserID: "old-only", Content: "这个盒子是什么颜色", Question: true, OccurredAt: now.Add(-11 * time.Hour)})
	p.Add(Event{EventID: 3, Type: EventChat, UserID: "new-price", Content: "价格多少", Question: true, OccurredAt: now.Add(-time.Minute)})

	s := p.Snapshot(now, 6)
	if len(s.TopTopics) != 1 {
		t.Fatalf("dormant realtime bucket should be removed after 10h: %#v", s.TopTopics)
	}
	bucket := s.TopTopics[0]
	if bucket.Topic != "FAMILY:价格费用" || bucket.Count != 2 || bucket.ArchivedQuestionCount != 1 {
		t.Fatalf("active bucket should retain only compact historical count: %#v", bucket)
	}
	if len(bucket.Questions) != 1 || bucket.Questions[0].EventID != 3 {
		t.Fatalf("old concrete question leaked into realtime detail: %#v", bucket.Questions)
	}
	if len(bucket.SampleQuestions) != 1 || bucket.SampleQuestions[0] != "价格多少" {
		t.Fatalf("old sample text leaked into realtime context: %#v", bucket.SampleQuestions)
	}
	if len(bucket.TTSQuestions) != 1 || bucket.TTSQuestions[0].EventID != 3 || bucket.TTSEligibleCount != 1 {
		t.Fatalf("TTS window contains stale question: %#v", bucket)
	}
	if len(p.buckets) != 1 {
		t.Fatalf("stale bucket still occupies realtime memory: %d", len(p.buckets))
	}
}

func TestRetainedQuestionRemainsTTSEligiblePastThirtyMinutes(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	p := NewPool()
	p.Add(Event{EventID: 21, Type: EventChat, UserID: "u1", Content: "这个多少钱", Question: true, OccurredAt: now.Add(-2 * time.Hour)})

	s := p.Snapshot(now, 6)
	if len(s.TopTopics) != 1 {
		t.Fatalf("expected retained question bucket: %#v", s.TopTopics)
	}
	bucket := s.TopTopics[0]
	if len(bucket.Questions) != 1 || len(bucket.TTSQuestions) != 1 || bucket.TTSEligibleCount != 1 {
		t.Fatalf("question retained in realtime pool must stay answerable without a 30-minute cutoff: %#v", bucket)
	}
}

func TestShippingQuestionAppearsImmediatelyAndLaterVariantsMerge(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	p.Add(Event{EventID: 101, Type: EventChat, UserID: "u1", Nickname: "用户甲", Content: "哪里发货", Question: true, OccurredAt: now})

	first := p.Snapshot(now, 6)
	if len(first.TopTopics) != 1 || first.TopTopics[0].Count != 1 || first.TopTopics[0].SampleQuestions[0] != "哪里发货" {
		t.Fatalf("first shipping question should form a visible bucket: %#v", first.TopTopics)
	}
	if len(first.TopTopics[0].Questions) != 1 || first.TopTopics[0].Questions[0].EventID != 101 || first.TopTopics[0].Questions[0].Nickname != "用户甲" {
		t.Fatalf("bucket must expose the concrete question detail: %#v", first.TopTopics[0].Questions)
	}

	p.Add(Event{Type: EventChat, UserID: "u2", Content: "什么时候发货", Question: true, OccurredAt: now.Add(time.Second)})
	p.Add(Event{Type: EventChat, UserID: "u3", Content: "多久发货", Question: true, OccurredAt: now.Add(2 * time.Second)})
	merged := p.Snapshot(now.Add(2*time.Second), 6)
	if len(merged.TopTopics) != 1 || merged.TopTopics[0].Count != 3 {
		t.Fatalf("shipping variants should merge into the first real bucket: %#v", merged.TopTopics)
	}
	if merged.TopTopics[0].SampleQuestions[0] != "哪里发货" {
		t.Fatalf("bucket label must keep the first real question: %#v", merged.TopTopics[0])
	}

	p.Add(Event{Type: EventChat, UserID: "u4", Content: "在哪里买", Question: true, OccurredAt: now.Add(3 * time.Second)})
	separate := p.Snapshot(now.Add(3*time.Second), 6)
	if len(separate.TopTopics) != 2 {
		t.Fatalf("different purchase intent must stay separate: %#v", separate.TopTopics)
	}
}

func TestCoarseCommerceFamiliesGroupBroadIntentWithoutOverMerging(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	shipping := []string{"哪里发货", "发什么快递", "什么时候发货", "多久到", "送货上楼吗"}
	for index, text := range shipping {
		p.Add(Event{Type: EventChat, UserID: "ship", Content: text, Question: true, OccurredAt: now.Add(time.Duration(index) * time.Second)})
	}
	prices := []string{"5斤多少钱", "价格多少", "有没有优惠", "运费多少钱"}
	for index, text := range prices {
		p.Add(Event{Type: EventChat, UserID: "price", Content: text, Question: true, OccurredAt: now.Add(time.Duration(index+10) * time.Second)})
	}
	p.Add(Event{Type: EventChat, UserID: "buy", Content: "在哪里买", Question: true, OccurredAt: now.Add(20 * time.Second)})
	p.Add(Event{Type: EventChat, UserID: "after", Content: "收到破损怎么退款", Question: true, OccurredAt: now.Add(21 * time.Second)})

	s := p.Snapshot(now.Add(30*time.Second), 6)
	seen := map[string]TopicBucket{}
	for _, bucket := range s.TopTopics {
		seen[bucket.Topic] = bucket
	}
	if seen["FAMILY:发货物流"].Count != len(shipping) {
		t.Fatalf("shipping questions should share one coarse family: %#v", s.TopTopics)
	}
	if seen["FAMILY:价格费用"].Count != len(prices) {
		t.Fatalf("money-related questions should share one coarse family: %#v", s.TopTopics)
	}
	if seen["FAMILY:购买下单"].Count != 1 {
		t.Fatalf("purchase intent should stay separate from shipping: %#v", s.TopTopics)
	}
	if seen["FAMILY:售后退款"].Count != 1 {
		t.Fatalf("after-sale intent should stay separate: %#v", s.TopTopics)
	}
}

func TestCoarseFamilyPriorityForShippingFeeAndPurchaseLocation(t *testing.T) {
	if got := QuestionTopic("运费多少钱"); got != "FAMILY:价格费用" {
		t.Fatalf("shipping fee should prefer price family, got %q", got)
	}
	if got := QuestionTopic("在哪里买"); got != "FAMILY:购买下单" {
		t.Fatalf("purchase location should prefer purchase family, got %q", got)
	}
	if got := QuestionTopic("哪里发货"); got != "FAMILY:发货物流" {
		t.Fatalf("shipping origin should use shipping family, got %q", got)
	}
}

func TestRemoveUserDropsEventsAndSemanticBucketContribution(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	p.Add(Event{Type: EventChat, UserID: "blocked", Content: "多少钱", Question: true, OccurredAt: now})
	p.Add(Event{Type: EventChat, UserID: "blocked", Content: "价格多少", Question: true, OccurredAt: now})
	p.Add(Event{Type: EventChat, UserID: "allowed", Content: "多久发货", Question: true, OccurredAt: now})
	p.Add(Event{Type: EventChat, UserID: "allowed-2", Content: "多久发货呢", Question: true, OccurredAt: now})

	p.RemoveUser("blocked")
	s := p.Snapshot(now, 6)
	if s.Chats30s != 2 || s.QuestionCount30s != 2 {
		t.Fatalf("blocked user still contributes to intelligence: %#v", s)
	}
	if len(s.TopTopics) != 1 || s.TopTopics[0].SampleQuestions[0] != "多久发货" {
		t.Fatalf("blocked user still contributes to semantic buckets: %#v", s.TopTopics)
	}
}

func TestMergeTopicsPreservesQuestionsAndFutureAlias(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	p.Add(Event{EventID: 1, Type: EventChat, UserID: "u1", Content: "这个能不能放冰箱", Question: true, OccurredAt: now})
	p.Add(Event{EventID: 2, Type: EventChat, UserID: "u2", Content: "需要冷藏吗", Question: true, OccurredAt: now.Add(time.Second)})
	before := p.Snapshot(now.Add(time.Second), 6)
	if len(before.TopTopics) != 2 {
		t.Fatalf("expected two rule buckets before semantic merge: %#v", before.TopTopics)
	}

	target := before.TopTopics[0].Topic
	source := before.TopTopics[1].Topic
	if got := p.MergeTopics(target, []string{source}); got != 1 {
		t.Fatalf("merged=%d", got)
	}
	merged := p.Snapshot(now.Add(2*time.Second), 6)
	if len(merged.TopTopics) != 1 || merged.TopTopics[0].Count != 2 || len(merged.TopTopics[0].Questions) != 2 {
		t.Fatalf("semantic merge lost concrete questions: %#v", merged.TopTopics)
	}

	p.Add(Event{EventID: 3, Type: EventChat, UserID: "u3", Content: "需要冷藏吗", Question: true, OccurredAt: now.Add(3 * time.Second)})
	after := p.Snapshot(now.Add(3*time.Second), 6)
	if len(after.TopTopics) != 1 || after.TopTopics[0].Count != 3 {
		t.Fatalf("merged source should remain aliased for the session: %#v", after.TopTopics)
	}
}
