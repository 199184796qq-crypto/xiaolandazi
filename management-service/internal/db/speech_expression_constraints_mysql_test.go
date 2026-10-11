package db

import (
	"context"
	"livecompanion/management/internal/contentpools"
	"sync"
	"testing"
)

func TestSpeechExpressionConstraintMySQL(t *testing.T) {
	s := lectureTestStore(t)
	ctx := context.Background()
	exec := func(q string) {
		t.Helper()
		if _, e := s.db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,status VARCHAR(20))`)
	exec(`CREATE TABLE live_agent_plan_facts(id BIGINT PRIMARY KEY,tenant_id BIGINT,plan_id BIGINT,version_no BIGINT,status VARCHAR(20),fact_value TEXT,source_type VARCHAR(30))`)
	exec(`CREATE TABLE live_plan_content_revisions(tenant_id BIGINT,plan_id BIGINT,revision BIGINT,PRIMARY KEY(tenant_id,plan_id))`)
	exec(`INSERT INTO live_agent_plans VALUES(4,14,'active')`)
	exec(`INSERT INTO live_agent_plan_facts VALUES(36,14,4,2,'active','开袋即食','story_subject')`)
	exec(`INSERT INTO live_plan_content_revisions VALUES(14,4,7)`)
	if e := s.MigrateContentCorrections(ctx); e != nil {
		t.Fatal(e)
	}
	c := contentpools.Correction{ID: "program-rule", Kind: contentpools.ExpressionConstraintKind, Products: []int64{1}, Sources: []string{"knowledge:36"}, Original: "早餐开袋吃", Feedback: "不要拿早餐举例", Content: contentpools.CorrectionContent{Summary: "避免早餐场景", Forbidden: "不用早餐或早晨赶时间举例"}}
	// Wrong tenant/version and a clarification never commit a rule.
	if e := s.AdoptSpeechExpressionConstraint(ctx, 15, 4, 8, 36, 2, 7, c); e == nil {
		t.Fatal("cross tenant accepted")
	}
	if e := s.AdoptSpeechExpressionConstraint(ctx, 14, 4, 8, 36, 1, 7, c); e == nil {
		t.Fatal("stale fact accepted")
	}
	question := c
	question.Content.Question = "尚未确认"
	if e := s.AdoptSpeechExpressionConstraint(ctx, 14, 4, 8, 36, 2, 7, question); e == nil {
		t.Fatal("clarification adopted")
	}
	if e := s.AdoptSpeechExpressionConstraint(ctx, 14, 4, 8, 36, 2, 7, c); e != nil {
		t.Fatal(e)
	}
	if e := s.AdoptSpeechExpressionConstraint(ctx, 14, 4, 8, 36, 2, 7, c); e != nil {
		t.Fatal("retry should be idempotent", e)
	}
	items, e := s.ListContentCorrections(ctx, 14, 4)
	if e != nil || len(items) != 1 || items[0].Feedback != c.Feedback || items[0].Content.Forbidden != c.Content.Forbidden {
		t.Fatal(items, e)
	}
	var version, actor int64
	var value, source string
	if e = s.db.QueryRowContext(ctx, `SELECT version_no,fact_value,source_type FROM live_agent_plan_facts WHERE id=36`).Scan(&version, &value, &source); e != nil || version != 2 || value != "开袋即食" || source != "story_subject" {
		t.Fatal("fact/root binding altered", version, value, source, e)
	}
	if e = s.db.QueryRowContext(ctx, `SELECT created_by FROM live_plan_content_corrections WHERE id='program-rule'`).Scan(&actor); e != nil || actor != 8 {
		t.Fatal("actor provenance lost", actor, e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			next := c
			next.ID = id
			if e := s.AdoptSpeechExpressionConstraint(ctx, 14, 4, 8, 36, 2, 8, next); e == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatal("lost update protection", wins)
	}
	t.Log("PASS isolated rule adoption: durable original and constraint, tenant/version isolation, unchanged secondary root, actor, idempotency, concurrent CAS")
}
