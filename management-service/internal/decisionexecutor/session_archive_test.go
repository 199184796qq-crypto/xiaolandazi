package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
	"testing"
	"time"
)

type sessionArchiveFixture struct {
	*fakeStore
	assets []model.SessionSpeechAsset
	marks  map[string]time.Time
}

func (s *sessionArchiveFixture) SaveSessionSpeechAsset(ctx context.Context, a model.SessionSpeechAsset) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.assets = append(s.assets, a)
	return nil
}
func (s *sessionArchiveFixture) MarkSessionSpeechPlayed(ctx context.Context, tenant, room, session int64, ref string, at time.Time) error {
	if tenant == 1 && room == 2 && session == 3 {
		s.marks[ref] = at
	}
	return nil
}
func TestSessionArchiveWaitsForPlaybackAndUsedBridge(t *testing.T) {
	s := &sessionArchiveFixture{fakeStore: &fakeStore{}, marks: map[string]time.Time{}}
	w := &Worker{store: s}
	session := model.LiveRuntimeSession{ID: 3, TenantID: 1, RoomID: 2}
	v := coreSpeechRuntimeSnapshot{}
	at := time.Now().UTC()
	bridgeAt := at.Add(10 * time.Second)
	v.Interrupt.DecisionID = "decision"
	v.Interrupt.StartedAt = &at
	v.Interrupt.Status = "ready"
	w.archiveSessionPlayback(context.Background(), session, v)
	if len(s.marks) != 0 {
		t.Fatal("queued answer exported")
	}
	v.Interrupt.Status = "playing"
	w.archiveSessionPlayback(context.Background(), session, v)
	if len(s.marks) != 1 || !s.marks["reply:decision"].Equal(at) {
		t.Fatal("actual reply not captured", s.marks)
	}
	v.Interrupt.Status = "returning"
	v.Interrupt.UpdatedAt = &bridgeAt
	w.archiveSessionPlayback(context.Background(), session, v)
	if len(s.marks) != 1 {
		t.Fatal("unused bridge exported")
	}
	v.Interrupt.BridgeUsed = true
	w.archiveSessionPlayback(context.Background(), session, v)
	if !s.marks["bridge:decision"].Equal(bridgeAt) {
		t.Fatal("bridge playback time incorrect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.archiveSessionAudio(ctx, session, "reply:decision", "interaction", "真实口播", "https://bucket.aliyuncs.com/a.wav", 1000)
	if len(s.assets) != 1 || s.assets[0].PlayedAt != nil || s.assets[0].SessionID != 3 {
		t.Fatal("staging lost or prematurely marked played")
	}
}
