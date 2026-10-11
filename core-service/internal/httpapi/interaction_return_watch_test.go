package httpapi

import (
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
)

func startReturnWatchTest(t *testing.T) (*Server, string, chan time.Time, chan time.Time, <-chan struct{}) {
	t.Helper()
	s := New(nil, nil, nil, nil, nil, "development", "test-secret")
	item := s.agentDecisions.Enqueue(44, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "Q:first", Question: "第一条", Priority: 40}).Item
	if item == nil {
		t.Fatal("missing first decision")
	}
	claimed, ok := s.agentDecisions.ClaimNext(44)
	if !ok || claimed.ID != item.ID {
		t.Fatal("claim first decision")
	}
	if _, err := s.roomAudio.StartMainline(44, "mainline-before"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.roomAudio.StartInterrupt(44); err != nil {
		t.Fatal(err)
	}
	if _, err := s.speechRuntime.Update(44, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusReturning, SpeechTaskID: "physical-reply", DecisionID: item.ID, ReplyText: "回答", DurationMS: 1000}); err != nil {
		t.Fatal(err)
	}
	notice, poll := make(chan time.Time), make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.awaitInteractionMainlineResume(44, "physical-reply", item.ID, notice, poll)
	}()
	t.Cleanup(func() {
		close(poll)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("return watcher leaked")
		}
	})
	return s, item.ID, notice, poll, done
}

func feedReturnWatch(t *testing.T, channel chan time.Time, done <-chan struct{}) {
	t.Helper()
	select {
	case channel <- time.Now():
	case <-done:
		t.Fatal("watcher stopped before physical return")
	case <-time.After(time.Second):
		t.Fatal("watcher did not consume signal")
	}
}

func finishReturnWatch(t *testing.T, poll chan time.Time, done <-chan struct{}) {
	t.Helper()
	select {
	case poll <- time.Now():
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not observe terminal state")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("return watcher remained stuck")
	}
}

func emitMainlineAfterReturn(t *testing.T, s *Server) {
	t.Helper()
	if _, err := s.roomAudio.StartMainline(44, "mainline-after"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.roomAudio.PublishPCMAt(44, roomaudio.SourceMainline, make([]byte, roomaudio.PCMBytesPerFrame), "mainline-after", 20); err != nil {
		t.Fatal(err)
	}
}

func TestInteractionLatePhysicalReturnSurvivesWarningAndUnblocksNextDecision(t *testing.T) {
	s, first, notice, poll, done := startReturnWatchTest(t)
	next := s.agentDecisions.Enqueue(44, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "Q:next", Question: "下一条", Priority: 30}).Item
	if next == nil {
		t.Fatal("missing next decision")
	}
	// Replay the exact former terminal branch: the 30-second signal arrives
	// while PCM still belongs to the interrupt. The same watcher must survive.
	feedReturnWatch(t, notice, done)
	feedReturnWatch(t, poll, done)
	speech, _ := s.speechRuntime.Snapshot(44)
	if !speechSnapshotBusy(speech) || speech.Interrupt.Status != speechruntime.StatusReturning {
		t.Fatal("overlapped old audio", speech)
	}
	if _, claimed := s.agentDecisions.ClaimNext(44); claimed {
		t.Fatal("claimed next while old audio still owns room")
	}
	// Merely starting the mainline without output is not an actual return.
	if _, err := s.roomAudio.StartMainline(44, "mainline-after"); err != nil {
		t.Fatal(err)
	}
	feedReturnWatch(t, poll, done)
	speech, _ = s.speechRuntime.Snapshot(44)
	if !speechSnapshotBusy(speech) {
		t.Fatal("return counted before mainline PCM")
	}
	if _, err := s.roomAudio.PublishPCMAt(44, roomaudio.SourceMainline, make([]byte, roomaudio.PCMBytesPerFrame), "mainline-after", 20); err != nil {
		t.Fatal(err)
	}
	finishReturnWatch(t, poll, done)
	speech, _ = s.speechRuntime.Snapshot(44)
	if speechSnapshotBusy(speech) || speech.Interrupt.Status != speechruntime.StatusCompleted {
		t.Fatal("returning ownership not cleared", speech)
	}
	if _, exists := s.agentDecisions.Get(44, first); exists {
		t.Fatal("completed decision stayed claimed")
	}
	claimed, ok := s.agentDecisions.ClaimNext(44)
	if !ok || claimed.ID != next.ID {
		t.Fatal("following decision blocked", claimed, ok)
	}
}

func TestInteractionReturnWatcherExitsWhenStoppedOrReplaced(t *testing.T) {
	for _, action := range []string{"stop", "replace"} {
		t.Run(action, func(t *testing.T) {
			s, _, notice, poll, done := startReturnWatchTest(t)
			feedReturnWatch(t, notice, done)
			if action == "stop" {
				s.speechRuntime.Reset(44)
			} else {
				_, err := s.speechRuntime.Update(44, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusPlaying, SpeechTaskID: "new-reply", DecisionID: "new-decision", ReplyText: "新任务"})
				if err != nil {
					t.Fatal(err)
				}
			}
			emitMainlineAfterReturn(t, s)
			finishReturnWatch(t, poll, done)
			speech, _ := s.speechRuntime.Snapshot(44)
			if action == "stop" && speech.Interrupt.Status != speechruntime.StatusIdle {
				t.Fatal("old callback resurrected stopped room", speech)
			}
			if action == "replace" && (speech.Interrupt.SpeechTaskID != "new-reply" || speech.Interrupt.Status != speechruntime.StatusPlaying) {
				t.Fatal("old callback overwrote replacement", speech)
			}
		})
	}
}

func TestLatePhysicalReturnDoesNotRewriteCancelledDecision(t *testing.T) {
	s, first, notice, poll, done := startReturnWatchTest(t)
	feedReturnWatch(t, notice, done)
	if _, removed := s.agentDecisions.Remove(44, first); !removed {
		t.Fatal("decision not removed")
	}
	emitMainlineAfterReturn(t, s)
	finishReturnWatch(t, poll, done)
	speech, _ := s.speechRuntime.Snapshot(44)
	if speechSnapshotBusy(speech) || speech.Interrupt.Status != speechruntime.StatusCompleted {
		t.Fatal("physical return left room occupied", speech)
	}
	queue := s.agentDecisions.Snapshot(44)
	if queue.Completion.Completed != 0 || queue.Completion.Cancelled != 1 || len(queue.TerminalHistory) != 1 || queue.TerminalHistory[0].Status != "CANCELLED" {
		t.Fatal("terminal accounting rewritten", queue.Completion, queue.TerminalHistory)
	}
}
