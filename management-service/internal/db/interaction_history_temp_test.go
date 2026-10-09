package db

import (
	"context"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/model"
	"os"
	"testing"
	"time"
)

// Explicit opt-in. A connection-private temporary table shadows the business
// table for this one pool only; disconnecting drops every test record. No global
// grant, business rows, migrations or shared table writes are involved.
func TestInteractionHistoryConnectionPrivateTemporaryTable(t *testing.T) {
	if os.Getenv("TEXT_HISTORY_TEMP_TEST") != "1" {
		t.Skip("explicit temporary-table integration test required")
	}
	s, err := Open(config.Load())
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer s.Close()
	s.db.SetMaxOpenConns(1)
	s.db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = s.db.ExecContext(ctx, `CREATE TEMPORARY TABLE tmp_text_history_fixture LIKE agent_learning_sessions`); err != nil {
		t.Fatal("temporary fixture unavailable", err)
	}
	if _, err = s.db.ExecContext(ctx, `ALTER TABLE tmp_text_history_fixture RENAME TO agent_learning_sessions`); err != nil {
		t.Fatal("temporary shadow fixture unavailable", err)
	}
	var n int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_learning_sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatal("fixture must start empty", err)
	}
	o := model.InteractionLearningObservation{TenantID: 14, RoomID: 26, ActorUserID: 1, SourceType: "live_uncertain", SourceRef: "isolated-temp-price", Question: "这个价格多少钱", Reply: "84.5元，不到90元", Review: model.InteractionLearningReview{PlanID: 4, Kind: "semantic", Meaning: "问价格", Reason: "给用户的建议", Context: "本场价格问题"}}
	if err = s.RecordInteractionLearningReview(ctx, o); err != nil {
		t.Fatal(err)
	}
	items, pending, total, err := s.ListInteractionLearningHistory(ctx, 14, 26, 4, "pending", "live_uncertain", "价格", 30, 0)
	if err != nil || pending != 1 || total != 1 || len(items) != 1 {
		t.Fatal(items, pending, total, err)
	}
	id := items[0].ID
	if err = s.CloseInteractionLearningReview(ctx, 14, 26, id); err != nil {
		t.Fatal(err)
	}
	items, pending, total, err = s.ListInteractionLearningHistory(ctx, 14, 26, 4, "closed", "", "不到90", 30, 0)
	if err != nil || pending != 0 || total != 1 || len(items) != 1 || items[0].ID != id || items[0].Status != "closed" {
		t.Fatal("dismissed evidence disappeared", items, pending, total, err)
	}
	for _, scope := range [][3]int64{{15, 26, 4}, {14, 27, 4}, {14, 26, 5}} {
		items, pending, total, err = s.ListInteractionLearningHistory(ctx, scope[0], scope[1], scope[2], "all", "", "", 30, 0)
		if err != nil || len(items) != 0 || pending != 0 || total != 0 {
			t.Fatal("scope leak", err)
		}
	}
	items, _, total, err = s.ListInteractionLearningHistory(ctx, 14, 26, 4, "all", "", "", 30, 30)
	if err != nil || total != 1 || len(items) != 0 {
		t.Fatal("pagination", err)
	}
	t.Log("PASS: temporary fixture only; closed history retained, pending count, text search, tenant/room/plan isolation and pagination")
}
