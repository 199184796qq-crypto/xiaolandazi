package agentwork

import (
	"errors"
	"testing"
	"time"
)

func TestConditionalStartCannotReviveLaterControlIntent(t *testing.T) {
	for _, change := range []string{"stop", "pause", "conditional-stop", "mode", "plan", "new-run", "boot"} {
		t.Run(change, func(t *testing.T) {
			r := New()
			_, _ = r.StartAgent(29, 0)
			old, _ := r.StopAgent(29, StopReasonManualPause)
			switch change {
			case "stop":
				_, _ = r.StopAgent(29, StopReasonManual)
			case "pause":
				_, _ = r.StopAgent(29, StopReasonManualPause)
			case "conditional-stop":
				_, _ = r.StopAgentIfCurrent(29, StopReasonManual, old.BootID, old.RunGeneration)
			case "mode":
				_, _ = r.SetMode(29, ModeAnchor)
			case "plan":
				_, _ = r.SetPlan(29, 7, "新方案")
			case "new-run":
				_, _ = r.StartAgent(29, old.WorkingSeconds)
			case "boot":
				old.BootID = "previous-boot"
			}
			before := r.Get(29)
			if _, err := r.StartAgentIfCurrent(29, old.WorkingSeconds, old.BootID, old.RunGeneration, old.ControlRevision); !errors.Is(err, ErrStartBaselineConflict) {
				t.Fatal("old resume was not rejected", err)
			}
			after := r.Get(29)
			if after.State != before.State || after.RunGeneration != before.RunGeneration || after.ControlRevision != before.ControlRevision || after.StopReason != before.StopReason {
				t.Fatal("rejected request changed the current lifecycle", before, after)
			}
		})
	}
}

func TestConditionalStartAdvancesOnceAndWorkingStartPreservesMeter(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	r := newRegistry(func() time.Time { return now })
	_, _ = r.StartAgent(29, 100)
	now = now.Add(2 * time.Second)
	paused, _ := r.StopAgent(29, StopReasonManualPause)
	resumed, err := r.StartAgentIfCurrent(29, paused.WorkingSeconds, paused.BootID, paused.RunGeneration, paused.ControlRevision)
	if err != nil || resumed.RunGeneration != paused.RunGeneration+1 || resumed.ControlRevision != paused.ControlRevision+1 || resumed.WorkingSeconds != 102 {
		t.Fatal("conditional start did not commit one transition", resumed, err)
	}
	if _, err = r.StartAgentIfCurrent(29, 0, paused.BootID, paused.RunGeneration, paused.ControlRevision); !errors.Is(err, ErrStartBaselineConflict) {
		t.Fatal("consumed paused baseline was accepted again", err)
	}
	now = now.Add(3 * time.Second)
	repeated, err := r.StartAgentIfCurrent(29, 0, resumed.BootID, resumed.RunGeneration, resumed.ControlRevision)
	if err != nil || repeated.RunGeneration != resumed.RunGeneration || repeated.ControlRevision != resumed.ControlRevision || repeated.WorkingSeconds != 105 {
		t.Fatal("working start reset billing or created another run", repeated, err)
	}
	if repeated, err = r.StartAgent(29, 0); err != nil || repeated.RunGeneration != resumed.RunGeneration || repeated.ControlRevision != resumed.ControlRevision || repeated.WorkingSeconds != 105 {
		t.Fatal("legacy working start reset billing", repeated, err)
	}
}

func TestConditionalStartAndStopHaveOneAtomicOrder(t *testing.T) {
	for i := 0; i < 100; i++ {
		r := New()
		_, _ = r.StartAgent(29, 0)
		paused, _ := r.StopAgent(29, StopReasonManualPause)
		gate, done := make(chan struct{}), make(chan struct{}, 2)
		go func() {
			<-gate
			_, _ = r.StartAgentIfCurrent(29, 0, paused.BootID, paused.RunGeneration, paused.ControlRevision)
			done <- struct{}{}
		}()
		go func() { <-gate; _, _ = r.StopAgent(29, StopReasonManual); done <- struct{}{} }()
		close(gate)
		<-done
		<-done
		if current := r.Get(29); current.State != StateStopped || current.ControlRevision <= paused.ControlRevision {
			t.Fatal("late start resurrected a manual stop", current)
		}
	}
}
