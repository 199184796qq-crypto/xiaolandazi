package interactionruntime

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func durationWelcome(t *testing.T, density string) (*Engine, Scope, string, time.Time) {
	t.Helper()
	e, s, now := newWelcomeEngine(), scope(29), base.Add(time.Minute)
	add(e, s, "arrival", "new-viewer", "member", "", now)
	jobs := e.Jobs(now, 1)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	w := jobs[0]
	body := strings.Repeat("字", 140)
	raw, _ := json.Marshal(map[string]any{"protocol": Protocol, "body": body, "task": w.ID, "expires_at": w.Expires})
	if !e.Accept(w, Content{Body: body, Intent: Intent{Relevant: true}, Knowledge: raw}, nil, now) {
		t.Fatal("accept")
	}
	e.rooms[s.Room].Density = density
	chosen := selectTask(t, e, s, now)
	if chosen.Estimate != 32200*time.Millisecond {
		t.Fatal(chosen.Estimate)
	}
	if _, ok := e.ReadyWelcome(s.Room, chosen.Work.ID, now); !ok {
		t.Fatal("ready")
	}
	return e, s, chosen.Work.ID, now
}

func TestSingle32200msBorrowsEachMinuteBandAndCompletesAfterDeadline(t *testing.T) {
	for _, density := range []string{"cold", "normal", "fast"} {
		t.Run(density, func(t *testing.T) {
			e, s, id, now := durationWelcome(t, density)
			if reason, _, retry := e.PreviewPreparationRetry(s, id, 32200, now); reason != "" || retry != 0 {
				t.Fatal(reason, retry)
			}
			if !e.ReserveAudio(s.Room, id, 32200, now) {
				t.Fatal("oversize single rejected")
			}
			e.Receipts(s.Room, id, "PLAYING", now, 0)
			e.Receipts(s.Room, id, "PROGRESS", now.Add(40*time.Second), 10000)
			// TTS/transport stalled: wall time is 80s, actual PCM is only 32.2s.
			e.Receipts(s.Room, id, "COMPLETED", now.Add(80*time.Second), 32200)
			r := e.rooms[s.Room]
			if r.Tasks[id].State != "completed" || len(r.Played) != 1 || r.Played[0].Seconds != 32.2 {
				t.Fatal(r.Tasks[id], r.Played)
			}
			if r.Welcome.Pending {
				t.Fatal("actually played welcome quota not spent")
			}
			e.Receipts(s.Room, id, "COMPLETED", now.Add(81*time.Second), 32200)
			if len(r.Played) != 1 {
				t.Fatal("duplicate PCM was charged")
			}
			if density != "cold" {
				if durationBudgetAllowed(r, 3000, now.Add(80*time.Second)) {
					t.Fatal("following speech escaped actual debit")
				}
				wait := durationBudgetRetryMS(r, 32200, now.Add(80*time.Second))
				if wait != 60000 {
					t.Fatal("second oversize must not borrow against occupied quota", wait)
				}
			}
		})
	}
}

func TestSingleLongerThanFortySecondsAndPartialFailureUseActualPCM(t *testing.T) {
	e, s, id, now := durationWelcome(t, "normal")
	if !e.ReserveAudio(s.Room, id, 90000, now) {
		t.Fatal("old 40s single cap remains")
	}
	e.Receipts(s.Room, id, "PLAYING", now, 0)
	e.Receipts(s.Room, id, "PROGRESS", now.Add(20*time.Second), 8000)
	e.Receipts(s.Room, id, "FAILED", now.Add(25*time.Second), 9200)
	r := e.rooms[s.Room]
	if len(r.Played) != 1 || r.Played[0].Seconds != 9.2 {
		t.Fatal("failure billed planned/wall duration", r.Played)
	}
}

func TestDurationBudgetComputesEarliestRecoveryAndGuardsBoundary(t *testing.T) {
	e, s, id, now := durationWelcome(t, "normal")
	r := e.rooms[s.Room]
	r.Played = []sample{{At: now.Add(-50 * time.Second), Seconds: 8}}
	reason, remaining, retry := e.PreviewPreparationRetry(s, id, 32200, now)
	if reason != "audio_duration_budget" || remaining != 22000 || retry != 10000 {
		t.Fatal(reason, remaining, retry)
	}
	if durationBudgetAllowed(r, 32200, now.Add(9999*time.Millisecond)) || !durationBudgetAllowed(r, 32200, now.Add(10*time.Second)) {
		t.Fatal("recovery boundary")
	}
	if reason, _, _ := e.PreviewPreparationRetry(s, id, 32200, now.Add(30*time.Second)); reason != "original_content_expired" {
		t.Fatal("renewed original validity", reason)
	}
}

func TestCommonSpeechEstimatorRatesUnicodeAndNoThirtySecondClamp(t *testing.T) {
	for _, tc := range []struct {
		text string
		rate float64
		want int
	}{{strings.Repeat("字", 140), 1, 32200}, {strings.Repeat("字", 140), 1.4, 23000}, {strings.Repeat("字", 140), .5, 64400}, {"🙂你好", 1, 3000}, {strings.Repeat("字", 200), 1, 46000}, {strings.Repeat("字", 140), math.NaN(), 32200}} {
		if got := EstimateDurationMS(tc.text, tc.rate); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}

func TestExpiredUnplayedWelcomeKeepsOpportunityForFreshPeople(t *testing.T) {
	e, s, id, now := durationWelcome(t, "normal")
	e.Lost(s.Room, id, now.Add(30*time.Second))
	e.Jobs(now.Add(31*time.Second), 1)
	if !e.rooms[s.Room].Welcome.Pending || !e.rooms[s.Room].Welcome.LastStart.IsZero() {
		t.Fatal("unplayed welcome spent")
	}
	add(e, s, "fresh-entry", "fresh-viewer", "member", "", now.Add(40*time.Second))
	jobs := e.Jobs(now.Add(40*time.Second), 1)
	if len(jobs) != 1 || len(jobs[0].People) != 1 || jobs[0].People[0].UID != "fresh-viewer" {
		t.Fatal("old names revived or fresh welcome missing", jobs)
	}
}
