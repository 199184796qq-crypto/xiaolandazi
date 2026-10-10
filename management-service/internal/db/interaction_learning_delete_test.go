package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

func TestSimulationReviewDeleteMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	wanted := map[string]bool{"agent_learning_sessions": true, "agent_learning_evidence": true, "agent_learning_results": true, "agent_memory_items": true, "agent_memory_versions": true, "agent_memory_evidence_stats": true}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(raw)
		if len(fields) > 5 && wanted[fields[5]] {
			if _, err := s.db.ExecContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
			delete(wanted, fields[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatal("missing tables", wanted)
	}
	add := func(tenant, room, plan int64, source, status, question string) int64 {
		t.Helper()
		o := model.InteractionLearningObservation{TenantID: tenant, RoomID: room, ActorUserID: 1, Question: question, SourceType: source, SourceRef: "fixture:" + question, Review: model.InteractionLearningReview{PlanID: plan, Kind: "semantic", Meaning: question, Context: "用户咨询", Reason: "模拟建议", NeededFacts: []string{}}}
		if err := s.RecordInteractionLearningReview(ctx, o); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := s.db.QueryRowContext(ctx, `SELECT id FROM agent_learning_sessions WHERE tenant_id=? AND room_id=? AND question=?`, tenant, room, question).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if status != "editing" {
			if _, err := s.db.ExecContext(ctx, `UPDATE agent_learning_sessions SET status=?,pending_review_key=NULL WHERE id=?`, status, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	target := add(14, 8, 4, "initial_simulation", "editing", "删除当前模拟建议")
	item, err := s.GetAgentLearningSession(ctx, 14, 8, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendAgentLearningTurn(ctx, item, "尚未采纳的纠正", model.AgentLearningResult{MemoryType: model.AgentMemoryTypeSemantic, MemoryKey: "fixture", ResultText: "尚未采纳", Structured: map[string]any{"review_plan_id": int64(4)}}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][3]int64{{15, 8, 4}, {14, 9, 4}, {14, 8, 5}} {
		if _, err := s.DeleteSimulationReviews(ctx, scope[0], scope[1], scope[2], target); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("scope deletion allowed", scope, err)
		}
	}
	preserved := []int64{
		add(14, 8, 4, "live_uncertain", "editing", "保留直播记录"), add(14, 8, 4, "initial_simulation", "closed", "保留已处理历史"), add(14, 8, 4, "initial_simulation", "adopted", "保留已采纳历史"),
		add(15, 8, 4, "initial_simulation", "editing", "保留其他租户"), add(14, 9, 4, "initial_simulation", "editing", "保留其他直播间"), add(14, 8, 5, "initial_simulation", "editing", "保留其他方案"),
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO agent_memory_items(tenant_id,room_id,memory_type,memory_key,target,created_by_user_id) VALUES(14,8,'semantic','saved-rule','已采用回答策略',1)`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.DeleteSimulationReviews(ctx, 14, 8, 4, target); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	for _, table := range []string{"agent_learning_evidence", "agent_learning_results"} {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE session_id=?`, target).Scan(&n); err != nil || n != 0 {
			t.Fatal("orphan preview", table, n, err)
		}
	}
	if _, err := s.AppendAgentLearningTurn(ctx, item, "删除后不能追加", model.AgentLearningResult{MemoryType: model.AgentMemoryTypeSemantic, MemoryKey: "fixture", ResultText: "过期写入"}); err == nil {
		t.Fatal("deleted session accepted stale correction")
	}
	for i := 0; i < 32; i++ {
		add(14, 8, 4, "initial_simulation", "editing", fmt.Sprintf("跨页模拟建议%d", i))
	}
	_, pending, total, err := s.ListInteractionLearningHistory(ctx, 14, 8, 4, "pending", "initial_simulation", "不存在的检索词", 30, 0)
	if err != nil || pending != 32 || total != 0 {
		t.Fatal("filtered search lost source-wide count", pending, total, err)
	}
	if n, err := s.DeleteSimulationReviews(ctx, 14, 8, 4, 0); err != nil || n != 32 {
		t.Fatal("bulk omitted pages", n, err)
	}
	for _, id := range preserved {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_learning_sessions WHERE id=?`, id).Scan(&n); err != nil || n != 1 {
			t.Fatal("deleted unrelated record", id, n, err)
		}
	}
	var memories int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_memory_items`).Scan(&memories); err != nil || memories != 1 {
		t.Fatal("adopted strategy deleted", memories, err)
	}
	if n, err := s.DeleteSimulationReviews(ctx, 14, 8, 4, 0); err != nil || n != 0 {
		t.Fatal("bulk not idempotent", n, err)
	}
	if id := add(14, 8, 4, "initial_simulation", "editing", "删除当前模拟建议"); id == target {
		t.Fatal("deleted key was not released")
	}
}

func TestSimulationScratchSQLGuard(t *testing.T) {
	for _, table := range []string{"agent_learning_sessions", "agent_learning_evidence", "agent_learning_results", "agent_memory_items", "agent_memory_versions", "agent_memory_evidence_stats"} {
		for _, query := range []string{"SELECT id FROM " + table + " WHERE id=? FOR UPDATE", "DELETE FROM " + table + " WHERE id IN (?,?)"} {
			out, err := rewriteSalesScratchSQL(query, "sbt_simulation_guard_")
			if err != nil || !strings.Contains(out, "sbt_simulation_guard_"+table) {
				t.Fatal("unisolated simulation fixture", query, out, err)
			}
		}
	}
}
