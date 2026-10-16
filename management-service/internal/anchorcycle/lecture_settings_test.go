package anchorcycle

import (
	"livecompanion/management/internal/lecturetask"
	"testing"
)

func TestExplicitLectureCadenceRespectsActualCyclesAndPlayback(t *testing.T) {
	if !lectureEnabled {
		t.Skip("主线讲解任务已暂缓（lectureEnabled=false）")
	}
	s := lectureFixture(t)
	s.Lectures.Library.Policy = lecturetask.Policy{Enabled: true, Frequency: "normal", MinCycles: 2, MaxCycles: 2, MinIntervalSeconds: 600}
	s.CycleNumber = 3
	s.Phase = "opening"
	s.Lectures.CompletedCycles = 1
	s.Lectures.PlayedMS = 610000
	if s.BeginLecture() {
		t.Fatal("generated rounds counted instead of completed playback")
	}
	s.Lectures.CompletedCycles = 2
	s.Lectures.PlayedMS = 599999
	if s.BeginLecture() {
		t.Fatal("minimum playback interval ignored")
	}
	s.Lectures.PlayedMS = 610000
	if !s.BeginLecture() || s.Lectures.NextCycles != 2 {
		t.Fatal("explicit settings not used")
	}
	if s.Lectures.Uses[1].Count != 0 {
		t.Fatal("scheduling counted as playback")
	}
	for _, bad := range []lecturetask.Policy{
		{Enabled: true, Frequency: "normal", MinCycles: 4, MaxCycles: 2},
		{Enabled: true, Frequency: "normal", MinCycles: 2, MaxCycles: 2, MinIntervalSeconds: 10},
	} {
		if bad.Validate() == nil {
			t.Fatal("invalid config accepted", bad)
		}
	}
	old := lecturetask.DefaultPolicy()
	lo, hi, ms := old.Cadence(1)
	if lo != 4 || hi != 6 || ms != 600000 {
		t.Fatal("legacy defaults changed")
	}
}
