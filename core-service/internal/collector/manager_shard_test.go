package collector

import (
	"testing"

	"livecompanion/core/internal/model"
)

func TestExactlyOneShardOwnsRoom(t *testing.T) {
	room := model.Room{ID: 991, TenantID: 27}
	owners := 0
	for shard := 0; shard < 4; shard++ {
		manager := &Manager{shardIndex: shard, shardCount: 4}
		if manager.OwnsRoom(room) {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("expected exactly one owner, got %d", owners)
	}
}

func TestSingleShardOwnsEveryRoom(t *testing.T) {
	manager := &Manager{shardIndex: 0, shardCount: 1}
	if !manager.OwnsRoom(model.Room{ID: 1, TenantID: 1}) {
		t.Fatal("single shard should own every room")
	}
}
func TestShardRoutingGoldenFixture(t *testing.T) {
	room := model.Room{ID: 991, TenantID: 27}
	for shard := 0; shard < 4; shard++ {
		manager := &Manager{shardIndex: shard, shardCount: 4}
		want := shard == 1
		if got := manager.OwnsRoom(room); got != want {
			t.Fatalf("shard %d ownership=%v want=%v", shard, got, want)
		}
	}
}
