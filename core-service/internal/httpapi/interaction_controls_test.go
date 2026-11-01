package httpapi

import (
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/strategycenter"
	"testing"
	"time"
)

func TestControlsPrunePendingAndKeepMatchingActualPCM(t *testing.T) {
	q := agentdecision.New()
	p := strategycenter.New()
	s := &Server{agentDecisions: q, strategyPolicies: p}
	pending := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "new", MissionKind: "welcome"}).Item
	playing := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "playing", MissionKind: "welcome", Priority: 120, ForceReopen: true}).Item
	q.ClaimNext(29)
	manual := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceManual, Topic: "manual", MissionKind: "welcome"}).Item
	q.BindPlayback(29, playing.ID, "pcm-task")
	audioDevStates.Store(s, &audioDevState{events: map[string]audioout.PlaybackEvent{"pcm-task": {RoomID: 29, SpeechTaskID: "pcm-task", ReceiverID: "core-pcm-output", Status: "PLAYING", OccurredAt: time.Now()}}, interactions: map[string]*audioInteractionMeta{"pcm-task": {RoomID: 29, DecisionID: playing.ID}}})
	defer audioDevStates.Delete(s)
	prefs := strategycenter.DefaultRoomInteractionPreferences(29)
	prefs.Switches = map[string]bool{"welcome": false}
	p.PutRoomInteractionPreferences(29, prefs)
	s.pruneDisabledInteractions(29)
	if _, ok := q.Get(29, pending.ID); ok {
		t.Fatal("unplayed welcome survived")
	}
	for _, id := range []string{playing.ID, manual.ID} {
		if _, ok := q.Get(29, id); !ok {
			t.Fatal("audible or manual item removed", id)
		}
	}
}
