package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestInteractionHistoryActionsMySQL(t *testing.T) {
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
	add := func(tenant, room, plan int64, status, question string) int64 {
		t.Helper()
		o := model.InteractionLearningObservation{TenantID: tenant, RoomID: room, ActorUserID: 1, Question: question, SourceType: "initial_simulation", SourceRef: "fixture:" + question, Review: model.InteractionLearningReview{PlanID: plan, Kind: "semantic", Meaning: question, Context: "口味咨询", Reason: "确认口味", SuggestedRule: "麻辣和五香可选"}}
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
	closed := add(14, 8, 4, "closed", "辣不辣哦")
	adopted := add(14, 8, 4, "adopted", "已经纠正的口味")
	result, err := s.db.ExecContext(ctx, `INSERT INTO agent_memory_items(tenant_id,room_id,memory_type,memory_key,target,created_by_user_id) VALUES(14,8,'semantic','plan:4:flavour','口味选择',1)`)
	if err != nil {
		t.Fatal(err)
	}
	memoryID, _ := result.LastInsertId()
	result, err = s.db.ExecContext(ctx, `INSERT INTO agent_memory_versions(memory_item_id,version_no,status,content_text,structured_json,source_session_id,source_result_id,created_by_user_id) VALUES(?,1,'active','保留已采用口味规则','{"review_plan_id":4}',?,1,1)`, memoryID, adopted)
	if err != nil {
		t.Fatal(err)
	}
	versionID, _ := result.LastInsertId()
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_memory_items SET current_version_id=? WHERE id=?`, versionID, memoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_learning_sessions SET adopted_memory_item_id=? WHERE id=?`, memoryID, adopted); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][3]int64{{15, 8, 4}, {14, 9, 4}, {14, 8, 5}} {
		if err := s.DeleteInteractionHistory(ctx, scope[0], scope[1], scope[2], closed); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("unscoped deletion", scope, err)
		}
		if _, err := s.CreateInteractionHistoryCorrection(ctx, 1, scope[0], scope[1], scope[2], closed); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("unscoped correction", scope, err)
		}
	}
	var wg sync.WaitGroup
	ids := make(chan int64, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			draft, err := s.CreateInteractionHistoryCorrection(ctx, 1, 14, 8, 4, adopted)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- draft.ID
		}()
	}
	wg.Wait()
	close(ids)
	var draftID int64
	for id := range ids {
		if draftID != 0 && draftID != id {
			t.Fatal("retry duplicated draft", draftID, id)
		}
		draftID = id
	}
	if draftID == 0 || draftID == adopted {
		t.Fatal("adopted history was reopened", draftID)
	}
	draft, err := s.GetAgentLearningSession(ctx, 14, 8, draftID)
	if err != nil || draft.Review.HistoryOriginID != adopted || draft.SourceRef != fmt.Sprintf("agent_memory:%d", memoryID) || draft.Question != "已经纠正的口味" || draft.Status != "editing" {
		t.Fatal("lost correction context", draft, err)
	}
	if _, err := s.GetLatestAgentLearningResult(ctx, draftID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old result copied into new correction", err)
	}
	if _, err := s.AppendAgentLearningTurn(ctx, draft, "现在可以选不辣", model.AgentLearningResult{MemoryType: "semantic", MemoryKey: "plan:4:flavour", Target: "口味选择", ResultText: "新纠正预览", Structured: map[string]any{"review_plan_id": int64(4)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteInteractionHistory(ctx, 14, 8, 4, adopted); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAgentLearningTurn(ctx, draft, "删除后不再写入", model.AgentLearningResult{}); err == nil {
		t.Fatal("late correction wrote after history deletion")
	}
	if _, err := s.AdoptAgentLearningSession(ctx, 14, 8, draftID, 1); err == nil {
		t.Fatal("hidden draft could be adopted")
	}
	if _, err := s.CreateInteractionHistoryCorrection(ctx, 1, 14, 8, 4, adopted); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted history could be corrected", err)
	}
	if err := s.DeleteInteractionHistory(ctx, 14, 8, 4, adopted); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("duplicate deletion succeeded", err)
	}
	memory, err := s.GetAgentMemoryItem(ctx, 14, 8, memoryID)
	if err != nil || memory.CurrentVersion == nil || memory.CurrentVersion.ID != versionID || memory.CurrentVersion.ContentText != "保留已采用口味规则" {
		t.Fatal("adopted asset changed", memory, err)
	}
	items, pending, total, err := s.ListInteractionLearningHistory(ctx, 14, 8, 4, "all", "", "", 30, 0)
	if err != nil || pending != 0 || total != 1 || len(items) != 1 || items[0].ID != closed {
		t.Fatal("deleted history or draft still visible", items, pending, total, err)
	}
	continued, err := s.CreateInteractionHistoryCorrection(ctx, 1, 14, 8, 4, closed)
	if err != nil || continued.Status != "editing" || continued.ID == closed {
		t.Fatal(continued, err)
	}
	for _, status := range []string{"closed", "adopted", "editing"} {
		id := add(14, 8, 4, status, "删除状态"+status)
		if err := s.DeleteInteractionHistory(ctx, 14, 8, 4, id); err != nil {
			t.Fatal(status, err)
		}
	}
	visible, pending, err := s.ListInteractionLearningReviews(ctx, 14, 8, 4)
	if err != nil || pending != 1 || len(visible) != 1 || visible[0].ID != continued.ID {
		t.Fatal("hidden pending records affected badge", visible, pending, err)
	}
}
