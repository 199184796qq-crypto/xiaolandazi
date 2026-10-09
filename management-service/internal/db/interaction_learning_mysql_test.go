package db

import (
	"context"
	"livecompanion/management/internal/model"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInteractionLearningMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	wanted := map[string]bool{"agent_learning_sessions": true, "agent_learning_evidence": true, "agent_learning_results": true, "agent_memory_items": true, "agent_memory_versions": true, "agent_memory_evidence_stats": true}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(raw)
		if len(fields) > 5 && wanted[fields[5]] {
			if fields[5] == "agent_learning_sessions" {
				for _, line := range []string{"    review_metadata_json JSON NULL,\n", "    pending_review_key CHAR(64) NULL,\n", "    UNIQUE KEY uk_agent_learning_pending_review (pending_review_key),\n"} {
					raw = strings.ReplaceAll(raw, line, "")
				}
			}
			if _, err := s.db.ExecContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
			delete(wanted, fields[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatal("missing fixtures", wanted)
	}
	for i := 0; i < 2; i++ {
		if err := s.MigrateInteractionLearning(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.db.ExecContext(ctx, `CREATE TABLE semantic_documents(tenant_id BIGINT,room_id BIGINT,content_type VARCHAR(32),source_id VARCHAR(128))`)
	if err != nil {
		t.Fatal(err)
	}
	o := model.InteractionLearningObservation{TenantID: 14, RoomID: 8, ActorUserID: 1, Question: "不抽可以吗", Reply: "目前不能确认", SourceType: "live_uncertain", SourceRef: "runtime:s1:q1", Review: model.InteractionLearningReview{PlanID: 4, Kind: "missing_fact", Meaning: "非真空选择", Context: "包装", Reason: "未提供选择规则", NeededFacts: []string{"是否支持非真空"}}}
	for i := 0; i < 2; i++ {
		if err := s.RecordInteractionLearningReview(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	items, n, err := s.ListInteractionLearningReviews(ctx, 14, 8, 4)
	if err != nil || n != 1 || len(items) != 1 || items[0].Review.Occurrences != 1 {
		t.Fatal(items, n, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			next := o
			next.SourceRef = "q" + string(rune('a'+i))
			if err := s.RecordInteractionLearningReview(ctx, next); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	items, n, err = s.ListInteractionLearningReviews(ctx, 14, 8, 4)
	if err != nil || n != 1 || items[0].Review.Occurrences != 6 {
		t.Fatal(items, n, err)
	}
	for _, scope := range [][3]int64{{15, 8, 4}, {14, 9, 4}, {14, 8, 5}} {
		a, n, err := s.ListInteractionLearningReviews(ctx, scope[0], scope[1], scope[2])
		if err != nil || n != 0 || len(a) != 0 {
			t.Fatal("scope leak", a, n, err)
		}
	}
	id := items[0].ID
	if err := s.CloseInteractionLearningReview(ctx, 15, 8, id); err == nil {
		t.Fatal("other tenant closed review")
	}
	if err := s.CloseInteractionLearningReview(ctx, 14, 8, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordInteractionLearningReview(ctx, o); err != nil {
		t.Fatal(err)
	}
	items, n, err = s.ListInteractionLearningReviews(ctx, 14, 8, 4)
	if err != nil || n != 1 || items[0].ID == id {
		t.Fatal("resolved key blocked new review", items, n, err)
	}
	current := items[0]
	_, err = s.AppendAgentLearningTurn(ctx, current, "我们确实是默认抽真空", model.AgentLearningResult{MemoryType: model.AgentMemoryTypeFact, MemoryKey: "plan:4:pack", ResultText: "默认抽真空", Structured: map[string]any{"review_plan_id": int64(4), "source": "human_confirmed"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.AdoptAgentLearningSession(ctx, 14, 8, current.ID, 1)
	if err != nil || out.Session.Status != "adopted" || out.Memory.MemoryType != model.AgentMemoryTypeFact {
		t.Fatal(out, err)
	}
	_, n, err = s.ListInteractionLearningReviews(ctx, 14, 8, 4)
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err := s.RecordInteractionLearningReview(ctx, o); err != nil {
		t.Fatal("adopted key prevented new evidence", err)
	}
}
