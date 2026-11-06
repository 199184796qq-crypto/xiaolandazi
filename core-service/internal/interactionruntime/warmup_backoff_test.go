package interactionruntime

import (
	"errors"
	"testing"
	"time"
)

func TestWarmupFailureBackoffDoesNotInventPlaybackOrBlockQuestions(t *testing.T) {
	for _, failure := range []string{"model", "lost", "cancelled", "empty"} {
		t.Run(failure, func(t *testing.T) {
			e := New()
			s := scope(29)
			e.SetPolicy(s, Policy{Active: true, SessionStartedAt: base}, base)
			e.Jobs(base.Add(180*time.Second), 1)
			jobs := e.Jobs(base.Add(181*time.Second), 1)
			if len(jobs) != 1 {
				t.Fatal(jobs)
			}
			at := base.Add(182 * time.Second)
			if failure == "model" {
				e.Accept(jobs[0], Content{}, errors.New("fixture failed"), at)
			} else if failure == "empty" {
				e.Accept(jobs[0], Content{}, nil, at)
			} else {
				e.Accept(jobs[0], Content{Body: "继续看看商品。", Intent: Intent{Relevant: true}}, nil, at)
				task := selectTask(t, e, s, at)
				if failure == "lost" {
					e.Lost(29, task.Work.ID, at)
				} else {
					e.SetPolicy(s, Policy{Active: true, Busy: true}, at)
					e.SetPolicy(s, Policy{Active: true}, at)
				}
			}
			e.Jobs(at, 1)
			questionObserved := false
			for i := 1; i < 30; i++ {
				if i == 1 {
					add(e, s, "real", "viewer", "chat", "哪里发货", at.Add(time.Second))
				}
				for _, w := range e.Jobs(at.Add(time.Duration(i)*time.Second), 1) {
					if w.Kind == "guide" {
						t.Fatal("warmup re-bought inside failure backoff", i)
					}
					if w.Kind == "semantic" {
						questionObserved = true
					}
				}
			}
			if !e.rooms[29].LastInteraction.IsZero() || len(e.rooms[29].Played) != 0 {
				t.Fatal("failed warmup counted as actual playback")
			}
			if !questionObserved {
				t.Fatal("guide backoff blocked real question")
			}
		})
	}
}

func TestWarmupPreflightRechecksRealOtherActivity(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true}, base)
	e.Jobs(base.Add(180*time.Second), 1)
	work := e.Jobs(base.Add(181*time.Second), 1)[0]
	e.Accept(work, Content{Body: "继续看看商品。", Intent: Intent{Relevant: true}}, nil, base.Add(182*time.Second))
	task := selectTask(t, e, s, base.Add(182*time.Second))
	// The next eligibility check must reject a selected guide after a new event,
	// even before the periodic tick has removed its queue entry.
	add(e, s, "new", "viewer", "chat", "哪里发货", base.Add(183*time.Second))
	if reason, _, _ := e.PreviewPreparationRetry(s, task.Work.ID, 3000, base.Add(183*time.Second)); reason != "warmup_activity_changed" {
		t.Fatal("stale warmup could buy TTS", reason)
	}
}

func TestWarmupFailureBackoffExpiresWithoutInventingAnInteractionEnd(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true, SessionStartedAt: base}, base)
	e.Jobs(base.Add(180*time.Second), 1)
	work := e.Jobs(base.Add(181*time.Second), 1)[0]
	at := base.Add(182 * time.Second)
	e.Accept(work, Content{}, errors.New("fixture failed"), at)
	for i := 0; i < 30; i++ {
		if jobs := e.Jobs(at.Add(time.Duration(i)*time.Second), 1); len(jobs) != 0 {
			t.Fatal("retry before backoff expires", i, jobs)
		}
	}
	e.Jobs(at.Add(30*time.Second), 1)
	if jobs := e.Jobs(at.Add(31*time.Second), 1); len(jobs) != 1 || jobs[0].Kind != "guide" {
		t.Fatal("failure backoff froze warmups forever", jobs)
	}
	if !e.rooms[29].LastInteraction.IsZero() || len(e.rooms[29].Played) != 0 {
		t.Fatal("retry recorded fake playback")
	}
}
