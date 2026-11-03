package voicebatch

import (
	"livecompanion/management/internal/model"
	"testing"
	"time"
)

func TestBatchNeedsFullCoverageOrTwoHours(t *testing.T) {
	s := State{Completed: map[string]bool{"piece1:a": true}}
	if s.Due() {
		t.Fatal("one cached original is not a full pool")
	}
	s.FullTargets = []string{"piece1:a", "piece2:b"}
	if s.Due() {
		t.Fatal("one piece cannot finish the whole host pool")
	}
	s.Completed["piece2:b"] = true
	if !s.Due() {
		t.Fatal("all frozen pool versions played, next batch should be allowed")
	}
	s.FullTargets = nil
	s.ElapsedMS = int64((2*time.Hour - time.Millisecond) / time.Millisecond)
	if s.Due() {
		t.Fatal("early time switch")
	}
	s.ElapsedMS++
	if !s.Due() {
		t.Fatal("two-hour alternative did not release")
	}
}

func TestPendingHostNeedsExactAudibleReceiptAndKeepsNewerCommand(t *testing.T) {
	a := model.LiveAgentVoiceIdentity{Model: "m", VoiceID: "A"}
	b := a
	b.VoiceID = "B"
	c := a
	c.VoiceID = "C"
	s := State{Active: a, Pending: &b, Requested: &c, PendingInstance: "s1-track2", Batch: 8, ElapsedMS: 1000, Completed: map[string]bool{"old": true}, FullTargets: []string{"old"}}
	if s.Started("old-track", time.Now()) || s.Active.VoiceID != "A" {
		t.Fatal("old receipt switched host")
	}
	if !s.Started("s1-track2", time.Now()) {
		t.Fatal("exact receipt did not commit")
	}
	if s.Active.VoiceID != "B" || s.Batch != 9 || s.ElapsedMS != 0 || len(s.Completed) != 0 || len(s.FullTargets) != 0 || s.Requested.VoiceID != "C" {
		t.Fatal(s)
	}
	if s.Started("s1-track2", time.Now()) {
		t.Fatal("duplicate receipt counted another batch")
	}
}

func TestObservedTimeExcludesPauseRestartAndSwitchWait(t *testing.T) {
	now := time.Now()
	s := State{}
	s.Observe(now)
	s.Observe(now.Add(2 * time.Second))
	if s.ElapsedMS != 2000 {
		t.Fatal(s)
	}
	s.Observe(now.Add(time.Hour))
	if s.ElapsedMS != 2000 {
		t.Fatal("pause/server gap counted")
	}
	p := model.LiveAgentVoiceIdentity{}
	s.Pending = &p
	s.Observe(now.Add(time.Hour + time.Second))
	if s.ElapsedMS != 2000 {
		t.Fatal("pending switch counted")
	}
}
