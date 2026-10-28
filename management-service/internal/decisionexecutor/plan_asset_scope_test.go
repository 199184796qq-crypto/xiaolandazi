package decisionexecutor

import (
	"context"
	"reflect"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/readingrules"
)

type planRulesFixture struct {
	*fakeStore
	planReads, roomReads int
}

func (s *planRulesFixture) GetPlanReadingRules(context.Context, int64, int64) (model.RoomReadingRules, error) {
	s.planReads++
	return model.RoomReadingRules{RulesText: "旧文 => jiu4 wen2"}, nil
}
func (s *planRulesFixture) GetRoomReadingRules(context.Context, int64, int64) (model.RoomReadingRules, error) {
	s.roomReads++
	return model.RoomReadingRules{RulesText: "错误房间覆盖 => wrong"}, nil
}
func TestReadingRulesUsePlanSnapshotAcrossRoomsAndVersionChanges(t *testing.T) {
	store := &planRulesFixture{fakeStore: readyVoiceStore()}
	store.publishedVersion.GenerationContext = map[string]any{"reading_rules": "绸都 => chou2 du1"}
	w := New(store, nil, nil, nil)
	ctx := context.Background()
	for _, room := range []int64{11, 29} {
		got := w.readingRulesFor(ctx, model.LiveRuntimeSession{TenantID: 7, RoomID: room})
		if !reflect.DeepEqual(got, readingrules.Parse("绸都 => chou2 du1")) {
			t.Fatal(room, got)
		}
	}
	if store.roomReads != 0 || store.planReads != 0 {
		t.Fatal("snapshot read a mutable room/plan override")
	}
	store.publishedVersion.ID++
	store.publishedVersion.GenerationContext["reading_rules"] = ""
	if got := w.readingRulesFor(ctx, model.LiveRuntimeSession{TenantID: 7, RoomID: 29}); len(got) != 0 {
		t.Fatal("new empty rules retained old version rules", got)
	}
}
