package interactionruntime

import (
	"fmt"
	"testing"
	"time"
)

// Deterministic playback, body and synthesis fixtures. This exercises the real
// engine under five load profiles, not a separate scheduling implementation.
func TestWelcomeThirtyMinuteMixedRoomsReplay(t *testing.T) {
	for _, scenario := range []struct {
		name                      string
		entries, semantic, period int
		slow                      bool
	}{
		{"low entry low semantic", 2, 1, 0, false}, {"high entry low semantic", 80, 3, 30, false},
		{"high entry medium semantic", 80, 8, 180, false}, {"high entry high semantic", 80, 25, 300, false},
		{"provider slow then recovers", 80, 8, 180, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			e := New()
			s := scope(29)
			e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"guide": false}}, base)
			type pending struct {
				w   Work
				due time.Time
			}
			models := map[string]pending{}
			var chosen *Task
			var synthAt, completeAt time.Time
			var starts []time.Time
			attempts, bodyCalls := 0, 0
			for second := 0; second < 1800; second++ {
				now := base.Add(time.Duration(second) * time.Second)
				previous := second * scenario.entries / 60
				current := (second + 1) * scenario.entries / 60
				for i := previous; i < current; i++ {
					add(e, s, fmt.Sprintf("entry%d", i), fmt.Sprintf("new%d", i), "member", "", now)
				}
				r := e.room(s, now)
				if r.SemanticUsers == nil {
					r.SemanticUsers = map[string]time.Time{}
				}
				for i := 0; i < scenario.semantic; i++ {
					r.SemanticUsers[fmt.Sprintf("q%d", i)] = now
				}
				if second%45 == 0 {
					add(e, s, fmt.Sprintf("q-event%d", second), "q0", "chat", "咨询当前方案", now)
				}
				if !completeAt.IsZero() && !now.Before(completeAt) {
					e.Receipts(29, chosen.Work.ID, "COMPLETED", now)
					chosen = nil
					completeAt = time.Time{}
				}
				for id, p := range models {
					if !now.Before(p.due) {
						welcomeBody(t, e, p.w, now)
						delete(models, id)
					}
				}
				if chosen != nil && completeAt.IsZero() && !now.Before(synthAt) {
					ok := true
					if chosen.Work.WelcomeQuota {
						_, ok = e.ReadyWelcome(29, chosen.Work.ID, now)
					}
					if ok && e.ReserveAudio(29, chosen.Work.ID, 6000, now) {
						e.Receipts(29, chosen.Work.ID, "PLAYING", now)
						completeAt = now.Add(6 * time.Second)
						if chosen.Work.WelcomeQuota {
							starts = append(starts, now)
						}
					} else if !now.Before(chosen.Work.Expires) {
						chosen = nil
					}
				}
				if chosen == nil {
					if task := e.Select(s, now); task != nil {
						e.Bind(task.Work, task.Work.ID, now)
						chosen = task
						synthAt = now.Add(2 * time.Second)
						if task.Work.WelcomeQuota {
							attempts++
							if scenario.slow && attempts <= 3 {
								synthAt = now.Add(45 * time.Second)
							}
						}
					}
				}
				for _, batch := range e.Batches(now, 4) {
					for _, w := range batch.Works {
						models[w.ID] = pending{w, now.Add(time.Second)}
						if w.WelcomeQuota {
							bodyCalls++
						}
					}
				}
			}
			if len(starts) == 0 {
				t.Fatal("no welcome completed", attempts)
			}
			for i := 1; i < len(starts); i++ {
				if starts[i-1].Sub(base) > 300*time.Second && starts[i].Sub(starts[i-1]) < time.Duration(scenario.period)*time.Second {
					t.Fatal("quota overlapped", starts[i-1], starts[i])
				}
			}
			if scenario.slow && (attempts < 4 || !starts[0].After(base.Add(90*time.Second))) {
				t.Fatal("did not recover after expired renditions", attempts, starts)
			}
			t.Logf("virtual_seconds=1800 entrants_per_min=%d semantic_people_per_180s=%d welcome_started=%d welcome_body_calls=%d synthesis_attempts=%d", scenario.entries, scenario.semantic, len(starts), bodyCalls, attempts)
		})
	}
}
