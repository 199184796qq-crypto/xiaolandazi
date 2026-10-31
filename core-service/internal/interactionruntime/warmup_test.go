package interactionruntime

import (
	"testing"
	"time"
)

func TestWarmupUses180SecondsFromRunAndEveryActualEnd(t *testing.T) {
	for _, kind := range []string{"welcome", "semantic", "like", "follow", "order", "engagement", "manual", "guide", "time_report"} {
		t.Run(kind, func(t *testing.T) {
			e := New()
			s := scope(29)
			e.SetPolicy(s, Policy{Active: true, SessionStartedAt: base}, base)
			e.Jobs(base.Add(179*time.Second), 1)
			if len(e.Snapshot(29)) != 0 {
				t.Fatal("warmup before 180 seconds")
			}
			e.Receipts(29, kind, "PLAYING", base.Add(179*time.Second))
			e.Jobs(base.Add(600*time.Second), 1)
			if len(e.Snapshot(29)) != 0 {
				t.Fatal("warmup while actual audio still playing")
			}
			e.Receipts(29, kind, "COMPLETED", base.Add(610*time.Second))
			e.Jobs(base.Add(789*time.Second), 1)
			if len(e.Snapshot(29)) != 0 {
				t.Fatal("warmup 179 seconds after actual end")
			}
			e.Jobs(base.Add(790*time.Second), 1)
			jobs := e.Jobs(base.Add(791*time.Second), 1)
			if len(jobs) != 1 || jobs[0].Kind != "guide" {
				t.Fatal("no guide after 180 seconds", jobs)
			}
		})
	}
}

func TestWarmupLatePreparedResultCannotSurviveNewInteraction(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true}, base)
	e.Jobs(base.Add(180*time.Second), 1)
	jobs := e.Jobs(base.Add(181*time.Second), 1)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	e.Receipts(29, "manual", "PLAYING", base.Add(182*time.Second))
	if e.Accept(jobs[0], Content{Body: "刚生成的暖场。", Intent: Intent{Relevant: true}}, nil, base.Add(183*time.Second)) {
		t.Fatal("stale prepared warmup admitted")
	}
	e.Receipts(29, "manual", "COMPLETED", base.Add(190*time.Second))
	if e.Select(s, base.Add(191*time.Second)) != nil {
		t.Fatal("warmup after manual speech")
	}
	e.Jobs(base.Add(369*time.Second), 1)
	for _, v := range e.Snapshot(29) {
		if v.Work.Kind == "guide" && !terminal(v.State) {
			t.Fatal(v)
		}
	}
}

func TestWarmupReservationRechecksBusyQueueAndDoesNotResetOnUnplayedFailure(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true}, base)
	e.Receipts(29, "never-played", "FAILED", base.Add(179*time.Second))
	e.Jobs(base.Add(180*time.Second), 1)
	jobs := e.Jobs(base.Add(181*time.Second), 1)
	if len(jobs) != 1 {
		t.Fatal("unplayed failure postponed warmup", jobs)
	}
	if !e.Accept(jobs[0], Content{Body: "自然暖场。", Intent: Intent{Relevant: true}}, nil, base.Add(181*time.Second)) {
		t.Fatal("accept")
	}
	task := selectTask(t, e, s, base.Add(182*time.Second))
	if !e.ReserveAudio(29, task.Work.ID, 3000, base.Add(183*time.Second)) {
		t.Fatal("initial reservation")
	}
	e.SetPolicy(s, Policy{Active: true, Busy: true}, base.Add(184*time.Second))
	if e.ReserveAudio(29, task.Work.ID, 3000, base.Add(184*time.Second)) {
		t.Fatal("ready WAV bypassed new interaction")
	}
}

func TestCompletedWithoutPlayingReconcilesOnceAndLateDuplicatesDoNotExtendCooldown(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "event", "u", "chat", "问题", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true})
	task := selectTask(t, e, s, base.Add(time.Second))
	if !e.ReserveAudio(29, task.Work.ID, 3000, base.Add(2*time.Second)) {
		t.Fatal("reserve")
	}
	e.Receipts(29, task.Work.ID, "COMPLETED", base.Add(5*time.Second))
	if got := e.Snapshot(29)[0]; got.State != "completed" {
		t.Fatal(got)
	}
	last := e.rooms[29].LastInteraction
	e.Receipts(29, task.Work.ID, "COMPLETED", base.Add(100*time.Second))
	e.Receipts(29, task.Work.ID, "PLAYING", base.Add(101*time.Second))
	if !e.rooms[29].LastInteraction.Equal(last) || len(e.rooms[29].Played) != 1 {
		t.Fatal("duplicate receipt consumed cooldown/budget twice")
	}
}

func TestWarmupDoesNotInheritPreviousRunPlayback(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true}, base)
	e.Receipts(29, "old-manual", "PLAYING", base.Add(time.Second))
	s.Epoch = "new-run"
	at := base.Add(10 * time.Minute)
	e.SetPolicy(s, Policy{Active: true, SessionStartedAt: at}, at)
	e.Jobs(at.Add(180*time.Second), 1)
	if len(e.Jobs(at.Add(181*time.Second), 1)) != 1 {
		t.Fatal("prior run blocked new scene")
	}
}

func TestLostManualPlaybackEventuallyReleasesWarmupAfterGap(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true}, base)
	e.Receipts(29, "manual", "PLAYING", base.Add(time.Second))
	e.ReconcilePlayback(s, map[string]bool{"manual": true}, base.Add(10*time.Second))
	if e.rooms[29].Playback["manual"].Ended.IsZero() == false {
		t.Fatal("live manual item prematurely ended")
	}
	e.ReconcilePlayback(s, map[string]bool{}, base.Add(11*time.Second))
	e.Jobs(base.Add(190*time.Second), 1)
	if len(e.Snapshot(29)) != 0 {
		t.Fatal("cooldown after missing terminal too short")
	}
	e.Jobs(base.Add(191*time.Second), 1)
	if len(e.Jobs(base.Add(192*time.Second), 1)) != 1 {
		t.Fatal("lost manual item blocked warmup forever")
	}
}
