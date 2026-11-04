package interactionruntime

import (
	"testing"
	"time"
)

func TestNoPCMOrExhaustedAudioBudgetDefersModelAndKeepsRawInput(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "q", "viewer", "chat", "哪里发货", base)
	e.SetPreparationBlocked(s, "no_pcm_cut", base)
	if b := e.Batches(base.Add(5*time.Second), 4); len(b) != 0 {
		t.Fatal("model scheduled without acoustic window", b)
	}
	if len(e.rooms[29].History["viewer"]) != 1 {
		t.Fatal("raw input lost")
	}
	e.SetPreparationBlocked(s, "", base.Add(5*time.Second))
	r := e.rooms[29]
	r.Played = []sample{{At: base.Add(time.Second), Seconds: 40}}
	if b := e.Batches(base.Add(8*time.Second), 4); len(b) != 0 {
		t.Fatal("model scheduled at full rolling duration")
	}
	// Old PCM has only a 19s overlap at t=42, enough to prepare fresh work
	// while the original event is still within its unchanged 60s deadline.
	if b := e.Batches(base.Add(42*time.Second), 4); len(b) != 1 {
		t.Fatal("preparation did not resume", b)
	}
}
func TestPreparationPreviewDoesNotConsumeOrRefreshTask(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "q", "viewer", "chat", "发货", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	before := e.Snapshot(29)[0]
	for i := 0; i < 20; i++ {
		reason, _ := e.PreviewPreparation(s, v.Work.ID, 16000, base.Add(2*time.Second))
		if reason != "" {
			t.Fatal(reason)
		}
	}
	after := e.Snapshot(29)[0]
	if !before.Work.Expires.Equal(after.Work.Expires) || before.Estimate != after.Estimate || len(e.rooms[29].Played) != 0 {
		t.Fatal("preview mutated quota/lifetime")
	}
}
