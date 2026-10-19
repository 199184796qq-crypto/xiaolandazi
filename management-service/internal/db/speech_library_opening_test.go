package db

import (
	"testing"

	lib "livecompanion/management/internal/speechlibrary"
)

func TestFirstOpeningMySQLHistoryIsolationAndIdempotence(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	if e := s.RememberFirstOpening(ctx, sc, "本场已审核通过的新开场。"); e != nil {
		t.Fatal(e)
	}
	if e := s.RememberFirstOpening(ctx, sc, "同场重试不能覆盖原开场。"); e != nil {
		t.Fatal(e)
	}
	next := sc
	next.Runtime = 2
	history, e := s.RecentFirstOpenings(ctx, next, 5)
	if e != nil || len(history) != 1 || history[0] != "本场已审核通过的新开场。" {
		t.Fatal(history, e)
	}
	for _, change := range []func(*lib.Scope){func(v *lib.Scope) { v.Tenant++ }, func(v *lib.Scope) { v.Room++ }, func(v *lib.Scope) { v.Plan++ }, func(v *lib.Scope) { v.Realm = "other" }, func(v *lib.Scope) { v.RoomContext = lib.Digest("new facts") }} {
		copy := next
		change(&copy)
		out, e := s.RecentFirstOpenings(ctx, copy, 5)
		if e != nil || len(out) != 0 {
			t.Fatal("another plan/room/tenant/realm/source leaked", out, e)
		}
	}
	if _, e := s.db.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='stopped',ended_at=NOW() WHERE id=?`, next.Runtime); e != nil {
		t.Fatal(e)
	}
	if e := s.RememberFirstOpening(ctx, next, "已结束场次不得写入。"); e != nil {
		t.Fatal(e)
	}
	sc.Runtime = 3
	out, e := s.RecentFirstOpenings(ctx, sc, 5)
	if e != nil || len(out) != 1 {
		t.Fatal("ended runtime persisted opening", out, e)
	}
}
