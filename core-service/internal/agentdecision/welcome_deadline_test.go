package agentdecision

import (
	"testing"
	"time"
)

func TestWelcomeInsufficientPreparationIsAutomaticFailureAndReleasesLane(t *testing.T) {
	for _, reason := range []string{"welcome_tts_time_insufficient", "welcome_names_deadline_missing"} {
		t.Run(reason, func(t *testing.T) {
			now := time.Now()
			q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 20)
			item := q.Enqueue(29, Candidate{Topic: "welcome", MissionKind: "welcome_batch", Question: "欢迎", TTLSeconds: 30}).Item
			q.ClaimExpected(29, item.ID)
			got, ok := q.YieldPreparation(29, item.ID, reason)
			if !ok || got.Attempts != 0 || !got.ExpiresAt.Equal(item.ExpiresAt) {
				t.Fatal("yield changed lease", got)
			}
			snap := q.Snapshot(29)
			if len(snap.Queue) != 0 || len(snap.TerminalHistory) != 1 || snap.TerminalHistory[0].Status != "FAILED" || snap.TerminalHistory[0].Reason != reason {
				t.Fatal("not an automatic terminal", snap)
			}
			if _, ok := q.YieldPreparation(29, item.ID, reason); ok {
				t.Fatal("duplicate receipt")
			}
			next := q.Enqueue(29, Candidate{Topic: "question", Question: "发货", TTLSeconds: 30}).Item
			if head := q.ClaimableHead(29); head == nil || head.ID != next.ID {
				t.Fatal("welcome blocks next task")
			}
		})
	}
}

func TestAutomaticAdmissionFailureCannotRemoveClaimedOrBoundPlayback(t *testing.T) {
	for _, state := range []string{"pending", "claimed", "bound"} {
		t.Run(state, func(t *testing.T) {
			q := New()
			item := q.Enqueue(29, Candidate{Topic: "welcome", Question: "欢迎", TTLSeconds: 30}).Item
			if state != "pending" {
				q.ClaimExpected(29, item.ID)
			}
			if state == "bound" {
				q.BindPlayback(29, item.ID, "pcm")
			}
			if removed := q.FailUnstarted(29, item.ID, "interaction_v2_expired"); removed != (state == "pending") {
				t.Fatal("ownership lost", state, removed)
			}
			if state == "bound" {
				if _, ok := q.YieldPreparation(29, item.ID, "welcome_tts_time_insufficient"); ok {
					t.Fatal("released playing welcome")
				}
				if _, ok := q.Complete(29, item.ID); !ok {
					t.Fatal("normal return cannot complete")
				}
				terminal := q.Snapshot(29).TerminalHistory
				if len(terminal) != 1 || terminal[0].Status != "COMPLETED" {
					t.Fatal(terminal)
				}
			}
		})
	}
}
