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
	if err != nil || p.GenerationCount != 2 || len(p.Cards) != 4 {
		t.Fatal(p, err)
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
