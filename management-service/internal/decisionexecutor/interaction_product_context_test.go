package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

type productContextTestStore struct {
	*fakeStore
	selected model.LiveAgentPlanVersion
	set      contentpools.Set
	reads    int
}

func (s *productContextTestStore) GetPublishedLiveAgentPlanVersionForRoom(context.Context, int64, int64) (model.LiveAgentPlanVersion, error) {
	return s.selected, nil
}
func (s *productContextTestStore) GetContentPoolSet(context.Context, int64, int64, int64) (contentpools.Set, error) {
	s.reads++
	return s.set, nil
}

func TestInteractionProductContextUsesBoundSnapshotAndKeepsServiceOut(t *testing.T) {
	s := &productContextTestStore{fakeStore: readyVoiceStore(), selected: model.LiveAgentPlanVersion{PlanID: 4, GenerationContext: map[string]any{"content_pools": map[string]any{"set_id": int64(9), "source_sha": "saved"}}}}
	s.set = contentpools.Set{ID: 9, SourceSHA: "saved", Sources: contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "3", Name: "家电"}}, Rows: []contentpools.SourceRow{{ID: "function", Area: "knowledge", Fields: map[string]string{"分类": "function"}}, {ID: "shipping", Area: "knowledge", Fields: map[string]string{"分类": "fulfillment"}}, {ID: "lecture", Area: "lecture"}}}, Pools: []contentpools.Pool{{Facts: []contentpools.Fact{{ID: "function", Text: "内胆不能放明火上", Sources: []string{"function"}, Products: []int64{1}}, {ID: "shipping", Text: "物流哨兵", Sources: []string{"shipping"}, Products: []int64{1}}, {ID: "lecture", Text: "二级哨兵", Sources: []string{"lecture"}, Products: []int64{1}}}}}}
	w := New(s, nil, nil, nil)
	text, err := w.interactionProductContext(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, 4, false)
	if err != nil || !strings.Contains(text, "3号链接：家电") || !strings.Contains(text, "内胆不能放明火上") || strings.Contains(text, "哨兵") {
		t.Fatal(text, err)
	}
	if _, err = w.interactionProductContext(context.Background(), model.LiveRuntimeSession{}, 5, false); err == nil {
		t.Fatal("another plan accepted")
	}
	s.set.SourceSHA = "changed"
	if _, err = w.interactionProductContext(context.Background(), model.LiveRuntimeSession{}, 4, false); err == nil {
		t.Fatal("different source accepted")
	}
}
