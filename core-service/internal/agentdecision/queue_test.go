package agentdecision

import (
	"strings"
	"testing"
	"time"
)

func TestManualPromotesExistingAgentTopicInsteadOfDuplicating(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)

	first := q.Enqueue(1, Candidate{Source: SourceAgent, Question: "什么时候发货"})
	if first.Item == nil {
		t.Fatal("agent candidate missing")
	}
	now = now.Add(time.Second)
	manual := q.Enqueue(1, Candidate{Source: SourceManual, Question: "发什么快递"})
	if !manual.Merged || manual.Item == nil {
		t.Fatalf("manual should merge existing shipping task: %#v", manual)
	}
	if manual.Item.Priority != ManualPriority || !manual.Item.ManualPromoted {
		t.Fatalf("manual should promote merged task: %#v", manual.Item)
	}
	if manual.Item.MergedCount != 2 {
		t.Fatalf("merged_count=%d want 2", manual.Item.MergedCount)
	}
	if got := q.Snapshot(1).Queue; len(got) != 1 {
		t.Fatalf("queue len=%d want 1", len(got))
	}
}

func TestManualBeatsAgentAndNormalAgentsStayFIFO(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:a", Question: "问题A", Priority: 30})
	now = now.Add(time.Second)
	q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:b", Question: "问题B", Priority: 30})
	now = now.Add(time.Second)
	q.Enqueue(1, Candidate{Source: SourceManual, Topic: "Q:c", Question: "人工问题C"})

	items := q.Snapshot(1).Queue
	if len(items) != 3 {
		t.Fatalf("queue=%#v", items)
	}
	if items[0].Topic != "Q:c" || items[1].Topic != "Q:a" || items[2].Topic != "Q:b" {
		t.Fatalf("unexpected order: %#v", items)
	}
}

func TestCooldownSuppressesRepeatedAgentAndManualUnlessForced(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	item := q.Enqueue(1, Candidate{Source: SourceAgent, Question: "哪里发货"}).Item
	if item == nil {
		t.Fatal("missing candidate")
	}
	if _, ok := q.Complete(1, item.ID); !ok {
		t.Fatal("complete failed")
	}
	now = now.Add(10 * time.Second)
	if got := q.Enqueue(1, Candidate{Source: SourceAgent, Question: "发什么快递"}); !got.Suppressed {
		t.Fatalf("agent should be suppressed during cooldown: %#v", got)
	}
	if got := q.Enqueue(1, Candidate{Source: SourceManual, Question: "什么时候发货"}); !got.Suppressed {
		t.Fatalf("manual default should respect cooldown: %#v", got)
	}
	forced := q.Enqueue(1, Candidate{Source: SourceManual, Question: "多久发货", ForceReopen: true})
	if forced.Suppressed || forced.Item == nil || forced.Item.Priority != ManualPriority {
		t.Fatalf("forced manual should reopen: %#v", forced)
	}
}

func TestQuickAnswerBeatsNormalManualAndBypassesCooldown(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	q.Enqueue(1, Candidate{Source: SourceManual, Topic: "Q:normal", Question: "普通回答", ManualAction: "answer"})
	now = now.Add(time.Second)
	quick := q.Enqueue(1, Candidate{Source: SourceManual, Topic: "Q:quick", Question: "马上回答", ManualAction: "quick"})
	if quick.Item == nil || quick.Item.Priority != QuickAnswerPriority || quick.Item.ManualAction != "quick" {
		t.Fatalf("quick answer not marked highest priority: %#v", quick)
	}
	items := q.Snapshot(1).Queue
	if len(items) != 2 || items[0].Topic != "Q:quick" {
		t.Fatalf("quick answer should be first: %#v", items)
	}

	answered := q.Enqueue(2, Candidate{Source: SourceAgent, Question: "哪里发货"}).Item
	if answered == nil {
		t.Fatal("missing agent item")
	}
	if _, ok := q.Complete(2, answered.ID); !ok {
		t.Fatal("complete failed")
	}
	now = now.Add(10 * time.Second)
	forced := q.Enqueue(2, Candidate{Source: SourceManual, Question: "发什么快递", ManualAction: "quick"})
	if forced.Suppressed || forced.Item == nil || forced.Item.ManualAction != "quick" {
		t.Fatalf("quick answer should bypass cooldown: %#v", forced)
	}
}

func TestCooldownStateExpiresEvenAfterAccumulatingSimilarQuestions(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	item := q.Enqueue(1, Candidate{Source: SourceAgent, Question: "哪里发货"}).Item
	if item == nil {
		t.Fatal("missing candidate")
	}
	if _, ok := q.Complete(1, item.ID); !ok {
		t.Fatal("complete failed")
	}
	now = now.Add(10 * time.Second)
	q.Enqueue(1, Candidate{Source: SourceAgent, Question: "发什么快递"})
	if got := q.Snapshot(1).RecentlyAnswered; len(got) != 1 || got[0].Accumulated != 1 {
		t.Fatalf("cooldown accumulation missing: %#v", got)
	}
	now = now.Add(81 * time.Second)
	if got := q.Snapshot(1).RecentlyAnswered; len(got) != 0 {
		t.Fatalf("expired cooldown should disappear: %#v", got)
	}
}

func TestReleaseReturnsClaimedItemToPendingQueue(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	created := q.Enqueue(1, Candidate{Source: SourceManual, Question: "什么时候发货", ManualAction: "answer"}).Item
	if created == nil {
		t.Fatal("missing decision")
	}
	claimed, ok := q.ClaimNext(1)
	if !ok || claimed == nil || claimed.Status != StatusClaimed {
		t.Fatalf("claim failed: %#v", claimed)
	}
	originalExpiresAt := claimed.ExpiresAt
	now = now.Add(time.Second)
	released, ok := q.Release(1, claimed.ID)
	if !ok || released == nil || released.Status != StatusPending || released.ClaimedAt != nil {
		t.Fatalf("release failed: %#v", released)
	}
	if !released.ExpiresAt.Equal(originalExpiresAt) {
		t.Fatalf("release must not extend task lifetime: got=%s want=%s", released.ExpiresAt, originalExpiresAt)
	}
	claimedAgain, ok := q.ClaimNext(1)
	if !ok || claimedAgain == nil || claimedAgain.ID != created.ID {
		t.Fatalf("released item should be claimable again: %#v", claimedAgain)
	}
}

func TestClaimTimeoutReturnsTaskToPendingWithoutExtendingLifetime(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 5*time.Minute, 90*time.Second, 8)
	created := q.Enqueue(15, Candidate{Source: SourceAgent, Topic: "Q:timeout", Question: "超时以后还执行吗", Priority: 40}).Item
	if created == nil {
		t.Fatal("missing decision")
	}
	claimed, ok := q.ClaimNext(15)
	if !ok || claimed == nil {
		t.Fatal("claim failed")
	}
	originalExpiresAt := claimed.ExpiresAt

	now = now.Add(ClaimExecutionTimeout + time.Second)
	snapshot := q.Snapshot(15)
	if len(snapshot.Queue) != 1 {
		t.Fatalf("expected task to remain queued: %#v", snapshot.Queue)
	}
	item := snapshot.Queue[0]
	if item.Status != StatusPending || item.ClaimedAt != nil {
		t.Fatalf("timed out claim must return to pending: %#v", item)
	}
	if !item.ExpiresAt.Equal(originalExpiresAt) {
		t.Fatalf("claim timeout must not extend lifetime: got=%s want=%s", item.ExpiresAt, originalExpiresAt)
	}
}

func TestClaimTimeoutDropsTaskWhenOriginalLifetimeAlreadyExpired(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 30*time.Second, 90*time.Second, 8)
	created := q.Enqueue(15, Candidate{Source: SourceAgent, Topic: "Q:expired-timeout", Question: "老问题", Priority: 40}).Item
	if created == nil {
		t.Fatal("missing decision")
	}
	if _, ok := q.ClaimNext(15); !ok {
		t.Fatal("claim failed")
	}

	now = now.Add(ClaimExecutionTimeout + time.Second)
	if got := q.Snapshot(15).Queue; len(got) != 0 {
		t.Fatalf("expired claimed task must be dropped after execution timeout: %#v", got)
	}
}

func TestCapacityNeverDropsClaimedTask(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 5*time.Minute, 90*time.Second, 2)
	low := q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:low-active", Question: "低优先级执行中", Priority: 10}).Item
	if low == nil {
		t.Fatal("missing low-priority task")
	}
	claimed, ok := q.ClaimNext(1)
	if !ok || claimed == nil || claimed.ID != low.ID {
		t.Fatalf("claim failed: %#v", claimed)
	}
	q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:high-1", Question: "高优先级1", Priority: 80})
	result := q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:high-2", Question: "高优先级2", Priority: 70})
	if result.Dropped == nil || result.Dropped.Topic != "Q:high-2" {
		t.Fatalf("expected lowest pending task to be dropped, got %#v", result.Dropped)
	}
	snapshot := q.Snapshot(1)
	foundClaimed := false
	for _, item := range snapshot.Queue {
		if item.ID == claimed.ID && item.Status == StatusClaimed {
			foundClaimed = true
		}
	}
	if !foundClaimed {
		t.Fatalf("claimed task must survive capacity pressure: %#v", snapshot.Queue)
	}
}

func TestSweepExpiresPendingWithoutRoomAccess(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 5*time.Second, 90*time.Second, 8)
	if q.Enqueue(3, Candidate{Source: SourceAgent, Topic: "Q:sweep", Question: "过期任务"}).Item == nil {
		t.Fatal("missing task")
	}
	now = now.Add(6 * time.Second)
	if removed := q.Sweep(); removed != 1 {
		t.Fatalf("removed=%d want 1", removed)
	}
	if got := q.Snapshot(3).Queue; len(got) != 0 {
		t.Fatalf("sweep must remove expired task: %#v", got)
	}
}

func TestClaimedDecisionSurvivesQueueTTL(t *testing.T) {
	now := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 5*time.Second, 90*time.Second, 8)
	created := q.Enqueue(15, Candidate{Source: SourceAgent, Topic: "Q:lifecycle", Question: "互动生命周期", Priority: 40}).Item
	if created == nil {
		t.Fatal("missing decision")
	}
	claimed, ok := q.ClaimNext(15)
	if !ok || claimed == nil || claimed.Status != StatusClaimed {
		t.Fatalf("claim failed: %#v", claimed)
	}

	now = now.Add(30 * time.Second)
	snapshot := q.Snapshot(15)
	if len(snapshot.Queue) != 1 || snapshot.Queue[0].ID != claimed.ID || snapshot.Queue[0].Status != StatusClaimed {
		t.Fatalf("claimed decision must survive waiting TTL: %#v", snapshot.Queue)
	}
}

func TestRemoveCanRemovePendingOrClaimedDecision(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	pending := q.Enqueue(1, Candidate{Source: SourceManual, Question: "待移除", ManualAction: "answer"}).Item
	claimed := q.Enqueue(1, Candidate{Source: SourceManual, Question: "执行中", ManualAction: "quick"}).Item
	if pending == nil || claimed == nil {
		t.Fatal("missing decisions")
	}
	claimedNow, ok := q.ClaimNext(1)
	if !ok || claimedNow == nil || claimedNow.ID != claimed.ID {
		t.Fatalf("expected quick decision to be claimed: %#v", claimedNow)
	}
	removedClaimed, ok := q.Remove(1, claimed.ID)
	if !ok || removedClaimed == nil || removedClaimed.ID != claimed.ID || removedClaimed.Status != StatusClaimed {
		t.Fatalf("claimed decision remove failed: %#v", removedClaimed)
	}
	removedPending, ok := q.Remove(1, pending.ID)
	if !ok || removedPending == nil || removedPending.ID != pending.ID {
		t.Fatalf("pending decision remove failed: %#v", removedPending)
	}
	if got := q.Snapshot(1).Queue; len(got) != 0 {
		t.Fatalf("queue should be empty after removals: %#v", got)
	}
	if _, ok := q.Get(1, claimed.ID); ok {
		t.Fatal("removed claimed decision must not be retrievable")
	}
}

func TestSimulationCanCompleteWithoutEnteringCooldown(t *testing.T) {
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	created := q.Enqueue(15, Candidate{
		Source:       SourceManual,
		Question:     "哪年的菜籽",
		ManualOrigin: "test_simulation",
		ManualAction: "answer",
	})
	if created.Item == nil {
		t.Fatalf("simulation should enqueue even when semantic topic is unknown: %#v", created)
	}
	claimed, ok := q.ClaimNext(15)
	if !ok || claimed == nil || claimed.ManualOrigin != "test_simulation" {
		t.Fatalf("simulation claim failed: %#v", claimed)
	}
	result, ok := q.CompleteSimulation(15, claimed.ID, "咱家用的是今年新采购的菜籽。", "intent", "智能体+菜籽油+方案1", 11)
	if !ok {
		t.Fatal("simulation completion failed")
	}
	if result.Question != "哪年的菜籽" || result.UserLayerVersion != 11 {
		t.Fatalf("unexpected simulation result: %#v", result)
	}
	snapshot := q.Snapshot(15)
	if len(snapshot.Queue) != 0 || len(snapshot.RecentlyAnswered) != 0 {
		t.Fatalf("simulation must not pollute real queue/cooldown: %#v", snapshot)
	}
	if len(snapshot.SimulationResults) != 1 || snapshot.SimulationResults[0].Reply == "" {
		t.Fatalf("simulation result missing: %#v", snapshot.SimulationResults)
	}
}

func TestAgentInputPreviewIsIsolatedAndCompletesWithoutCooldown(t *testing.T) {
	now := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	normal := q.Enqueue(15, Candidate{
		Source:   SourceAgent,
		Question: "欢迎新来的朋友",
		Priority: 20,
	})
	if normal.Item == nil {
		t.Fatal("normal decision missing")
	}
	preview := q.Enqueue(15, Candidate{
		Source:       SourceManual,
		Question:     "欢迎新来的朋友",
		ManualOrigin: "agent_input_preview",
		ManualAction: "answer",
	})
	if preview.Item == nil {
		t.Fatal("preview decision missing")
	}
	if preview.Merged {
		t.Fatal("agent input preview must not merge into the real decision queue item")
	}
	if !strings.HasPrefix(preview.Item.Topic, "PREVIEW:") {
		t.Fatalf("preview topic must be isolated, got %q", preview.Item.Topic)
	}

	claimed, ok := q.ClaimNext(15)
	if !ok || claimed == nil || claimed.ID != preview.Item.ID {
		t.Fatalf("preview should be claimable first: %#v", claimed)
	}
	result, ok := q.CompleteSimulation(15, claimed.ID, "欢迎新来的朋友们，进来的都先看看咱们今天的直播内容。", "intent", "方案", 3)
	if !ok || result.Reply == "" {
		t.Fatalf("preview completion failed: %#v", result)
	}
	snapshot := q.Snapshot(15)
	if len(snapshot.RecentlyAnswered) != 0 {
		t.Fatalf("preview must not enter cooldown: %#v", snapshot.RecentlyAnswered)
	}
	if len(snapshot.Queue) != 1 || snapshot.Queue[0].ID != normal.Item.ID {
		t.Fatalf("real queue item should remain untouched: %#v", snapshot.Queue)
	}
}

func TestCapacityDropsLowestPriorityAndTTLExpires(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 30*time.Second, 90*time.Second, 2)
	q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:low", Question: "低", Priority: 10})
	now = now.Add(time.Second)
	q.Enqueue(1, Candidate{Source: SourceAgent, Topic: "Q:mid", Question: "中", Priority: 30})
	now = now.Add(time.Second)
	result := q.Enqueue(1, Candidate{Source: SourceManual, Topic: "Q:manual", Question: "人工"})
	if result.Dropped == nil || result.Dropped.Topic != "Q:low" {
		t.Fatalf("expected low priority drop: %#v", result)
	}
	now = now.Add(31 * time.Second)
	if got := q.Snapshot(1).Queue; len(got) != 0 {
		t.Fatalf("expired queue should be empty: %#v", got)
	}
}

func TestRepeatedAgentTopicRaisesPriority(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 90*time.Second, 8)
	q.Enqueue(1, Candidate{Source: SourceAgent, Question: "5斤多少钱", Priority: 30})
	now = now.Add(time.Second)
	merged := q.Enqueue(1, Candidate{Source: SourceAgent, Question: "价格多少", Priority: 30})
	if merged.Item == nil || merged.Item.MergedCount != 2 || merged.Item.Priority <= 30 {
		t.Fatalf("repeated topic should gain priority: %#v", merged)
	}
}
