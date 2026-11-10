package db

import (
	"context"
	"errors"
	"livecompanion/management/internal/contextword"
	"strings"
	"sync"
	"testing"
)

func TestContextWordsMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string) {
		t.Helper()
		if _, e := s.db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_context_words (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15),(6,14)`)
	out, e := s.PlanContextWords(ctx, 14, 4)
	if e != nil || out.Revision != 0 || len(out.Items) != 0 {
		t.Fatal(out, e)
	}
	input := contextword.Catalog{Items: []contextword.Entry{{Triggers: []string{"切"}, Meaning: "切块", When: "商品处理", Enabled: true}}}
	saved, e := s.SavePlanContextWords(ctx, 14, 4, 8, input)
	if e != nil || saved.Revision != 1 || saved.Items[0].ID == "" {
		t.Fatal(saved, e)
	}
	if _, e = s.SavePlanContextWords(ctx, 15, 4, 8, input); e == nil {
		t.Fatal("foreign plan saved")
	}
	if _, e = s.SavePlanContextWords(ctx, 14, 4, 8, input); !errors.Is(e, ErrContextWordsConflict) {
		t.Fatal("stale save", e)
	}
	if other, e := s.PlanContextWords(ctx, 15, 4); e != nil || len(other.Items) != 0 {
		t.Fatal("cross tenant leak")
	}
	if other, e := s.PlanContextWords(ctx, 14, 6); e != nil || len(other.Items) != 0 {
		t.Fatal("cross plan leak")
	}
	forged := saved
	forged.Items = append([]contextword.Entry(nil), saved.Items...)
	forged.Items[0].ID = "foreign"
	if _, e = s.SavePlanContextWords(ctx, 14, 4, 8, forged); !errors.Is(e, ErrContextWordsConflict) {
		t.Fatal("forged ID", e)
	}
	exec(`CREATE TRIGGER reject_context_update BEFORE UPDATE ON live_agent_plan_context_words FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture rollback'`)
	if _, e = s.SavePlanContextWords(ctx, 14, 4, 8, saved); e == nil {
		t.Fatal("forced failure ignored")
	}
	got, e := s.PlanContextWords(ctx, 14, 4)
	if e != nil || got.Revision != 1 || got.Items[0].Meaning != "切块" {
		t.Fatal("rollback", got, e)
	}
	exec(`DROP TRIGGER reject_context_update`)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.SavePlanContextWords(ctx, 14, 4, 8, saved); results <- err }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrContextWordsConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	got, e = s.PlanContextWords(ctx, 14, 4)
	if e != nil {
		t.Fatal(e)
	}
	got.Items = nil
	got, e = s.SavePlanContextWords(ctx, 14, 4, 8, got)
	if e != nil || got.Revision != 3 || len(got.Items) != 0 {
		t.Fatal("explicit clear", got, e)
	}
}
