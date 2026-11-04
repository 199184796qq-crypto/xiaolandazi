package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
	"net/http"
	"testing"
)

type cleanupPreparedStore struct {
	*preparedTestStore
	revision string
}

func (s *cleanupPreparedStore) VoiceCacheRoomRevision(context.Context, int64, int64) (string, error) {
	return s.revision, nil
}
func TestVoiceCleanupPreparedAudioCannotResumeAfterClear(t *testing.T) {
	base := &preparedTestStore{fakeStore: readyVoiceStore()}
	store := &cleanupPreparedStore{preparedTestStore: base}
	core := &fakeCore{dispatchStatus: http.StatusConflict, dispatchRaw: `{"error":"主线已换段，请重新选择切点"}`}
	agent, tts := &fakeAgent{}, &fakeTTS{}
	worker := New(store, core, agent, tts)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	if e := worker.processRoom(context.Background(), session); e != nil {
		t.Fatal(e)
	}
	if len(worker.prepared) != 1 || tts.calls != 1 {
		t.Fatal("fixture did not prepare audio")
	}
	store.revision = "new-cleared-epoch"
	core.dispatchStatus = 0
	core.claimRaw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","title":"发货物流","sample_questions":["什么时候发货"],"manual_action":"answer"}}`
	if e := worker.processRoom(context.Background(), session); e != nil {
		t.Fatal(e)
	}
	if tts.calls != 2 || agent.calls != 2 {
		t.Fatal("cleared prepared content was dispatched", tts.calls, agent.calls)
	}
}
