package agentwork

import (
	"errors"
	"testing"
)

func TestConditionalStopCannotTouchNewRunOrDifferentBoot(t *testing.T) {
	r := New()
	old, err := r.StartAgent(27, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.StopAgent(27, StopReasonManual)
	next, err := r.StartAgent(27, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.StopAgentIfCurrent(27, StopReasonSystemError, old.BootID, old.RunGeneration); !errors.Is(err, ErrRuntimeReplaced) {
		t.Fatal("old failure stopped next run", err)
	}
	if _, err = r.StopAgentIfCurrent(27, StopReasonSystemError, "previous-boot", next.RunGeneration); !errors.Is(err, ErrRuntimeReplaced) {
		t.Fatal("wrong boot accepted", err)
	}
	if r.Get(27).State != StateWorking {
		t.Fatal("new run affected")
	}
}

func TestConditionalLateFailurePreservesNormalStopReason(t *testing.T) {
	for _, reason := range []StopReason{StopReasonLiveFinished, StopReasonManual, StopReasonManualPause, StopReasonDeviceOffline, StopReasonQuotaExhausted} {
		t.Run(string(reason), func(t *testing.T) {
			r := New()
			current, _ := r.StartAgent(27, 0)
			_, _ = r.StopAgent(27, reason)
			late, err := r.StopAgentIfCurrent(27, StopReasonSystemError, current.BootID, current.RunGeneration)
			if err != nil || late.StopReason != reason {
				t.Fatal("normal reason replaced", late, err)
			}
		})
	}
}

func TestConditionalStopOfMatchingRunAndNoStartingRace(t *testing.T) {
	r := New()
	current, _ := r.StartAgent(27, 0)
	stopped, err := r.StopAgentIfCurrent(27, StopReasonSystemError, current.BootID, current.RunGeneration)
	if err != nil || stopped.State != StateStopped || stopped.StopReason != StopReasonSystemError {
		t.Fatal(stopped, err)
	}
	_, _ = r.SetWithReason(27, StateStarting, "", 0)
	if _, err = r.StopAgentIfCurrent(27, StopReasonSystemError, current.BootID, current.RunGeneration); !errors.Is(err, ErrRuntimeReplaced) {
		t.Fatal("old stop during new start accepted", err)
	}
}
