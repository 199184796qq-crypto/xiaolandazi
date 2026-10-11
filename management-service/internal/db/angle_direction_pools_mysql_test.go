package db

import (
	"context"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestAngleDirectionPoolMySQLRetainsGenerationsAndScopes(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := s.MigrateAngleDirectionPools(ctx); err != nil {
			t.Fatal(err)
		}
	}
	key := strings.Repeat("a", 64)
	for _, id := range []string{"generation-a", "generation-b"} {
		p := anchorcycle.ParseDirectionContent("1. 使用安排\n1. 包装选择", id, []anchorcycle.HookFact{{ID: "knowledge:16"}}, []string{"2"}, "保存")
		p.SourceSHA = key
		if err := s.AppendAngleDirections(ctx, 14, 4, key, map[string]any{"scope": "2"}, p); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.LoadAngleDirections(ctx, 14, 4, key)
	if err != nil || p.GenerationCount != 2 || len(p.Cards) != 2 {
		t.Fatal(p, err)
	}
	var retained int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_angle_direction_generations WHERE tenant_id=? AND plan_id=? AND pool_key=?`, 14, 4, key).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("deduplication deleted raw generation history", retained, err)
	}
	use := anchorcycle.AnglePlayedUse{AngleTrackBinding: anchorcycle.AngleTrackBinding{PoolKey: key, HookID: p.Cards[0].ID}, TrackID: "track", StartedAt: "now"}
	for i := 0; i < 2; i++ {
		if err := s.RecordAngleDirectionPlay(ctx, 14, 4, "program", use); err != nil {
			t.Fatal(err)
		}
	}
	p, err = s.LoadAngleDirections(ctx, 14, 4, key)
	if err != nil || p.LifetimePlays[use.HookID] != 1 {
		t.Fatal(p, err)
	}
	for _, scope := range [][2]int64{{15, 4}, {14, 5}} {
		if got, err := s.LoadAngleDirections(ctx, scope[0], scope[1], key); err != nil || got != nil {
			t.Fatal("cross scope", got, err)
		}
	}
	if got, err := s.LoadAngleDirections(ctx, 14, 4, strings.Repeat("b", 64)); err != nil || got != nil {
		t.Fatal("obsolete source", got, err)
	}
}

func TestAngleDirectionReviewMySQLCacheScopeAndHistory(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if err := s.MigrateAngleDirectionPools(ctx); err != nil {
		t.Fatal(err)
	}
	key, contextSHA := strings.Repeat("a", 64), strings.Repeat("c", 64)
	approved := anchorcycle.AngleHook{ID: "old-id", Action: "按已确认功能说明用途"}
	rejected := anchorcycle.AngleHook{ID: "rejected-id", Action: "增加未确认功能"}
	r := anchorcycle.DirectionReviewReceipt{HookID: approved.ID, ContentSHA: anchorcycle.DirectionContentSHA(approved), ContextSHA: contextSHA, Decision: "通过", Raw: "通过", ExchangeID: 123}
	if err := s.SaveAngleDirectionReview(ctx, 14, 4, key, r); err != nil {
		t.Fatal(err)
	}
	r.Decision, r.ExchangeID = "有问题", 456
	if err := s.SaveAngleDirectionReview(ctx, 14, 4, key, r); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.LoadAngleDirectionReviews(ctx, 14, 4, key, contextSHA)
	if err != nil || len(loaded) != 1 || loaded[0].Decision != "通过" || loaded[0].ExchangeID != 123 {
		t.Fatal("terminal receipt overwritten or not reusable", loaded, err)
	}
	r.Error = "timeout"
	if err := s.SaveAngleDirectionReview(ctx, 14, 4, key, r); err == nil {
		t.Fatal("incomplete judgment persisted as reusable")
	}
	for _, c := range []struct {
		tenant, plan int64
		key, context string
	}{{15, 4, key, contextSHA}, {14, 5, key, contextSHA}, {14, 4, strings.Repeat("b", 64), contextSHA}, {14, 4, key, strings.Repeat("d", 64)}} {
		if got, e := s.LoadAngleDirectionReviews(ctx, c.tenant, c.plan, c.key, c.context); e != nil || len(got) != 0 {
			t.Fatal("review crossed tenant, plan, pool or current rule boundary", got, e)
		}
	}
	for i, card := range []anchorcycle.AngleHook{approved, rejected} {
		p := &anchorcycle.AnglePlan{GenerationID: []string{"g1", "g2"}[i], SourceSHA: key, ReviewContextSHA: contextSHA, GeneratedCards: []anchorcycle.AngleHook{card}, Raw: card.Action}
		if i == 0 {
			p.Cards = []anchorcycle.AngleHook{card}
		}
		if e := s.AppendAngleDirections(ctx, 14, 4, key, map[string]string{"task": "用途"}, p); e != nil {
			t.Fatal(e)
		}
	}
	p, err := s.LoadAngleDirections(ctx, 14, 4, key)
	if err != nil || len(p.Cards) != 1 || len(p.ReviewCandidates) != 2 || p.GenerationCount != 2 || p.ReviewContextSHA != "" {
		t.Fatal("new run lost accumulated candidates or bypassed current receipt reconciliation", p, err)
	}
	approved.ID = "new-generation-id"
	if got := anchorcycle.ReviewedDirectionCards([]anchorcycle.AngleHook{approved}, loaded, contextSHA); len(got) != 1 || got[0].ID != approved.ID {
		t.Fatal("same content could not reuse receipt after a program-owned identity change", got)
	}
}
