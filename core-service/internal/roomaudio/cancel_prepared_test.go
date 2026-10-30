package roomaudio

import "testing"

func TestCancelledUnplayedCutPreservesPCMClockAndMainline(t *testing.T) {
	e := New()
	e.StartMainline(29, "sentence-a")
	first, err := e.PublishPCM(29, SourceMainline, make([]byte, PCMBytesPerFrame), "sentence-a")
	if err != nil {
		t.Fatal(err)
	}
	e.PrepareInterrupt(29)
	e.ArmInterrupt(29, "sentence-b", "sentence-c")
	if e.CancelPreparedInterrupt(29, "old-cut") {
		t.Fatal("old cancellation reset a newer cut")
	}
	if !e.CancelPreparedInterrupt(29, "sentence-b") {
		t.Fatal("did not release pending cut")
	}
	snapshot := e.Snapshot(29)
	if snapshot.Phase != PhaseMainline || snapshot.PlannedCutSegmentID != "" || snapshot.ActiveSource != SourceMainline {
		t.Fatal(snapshot)
	}
	next, err := e.PublishPCM(29, SourceMainline, make([]byte, PCMBytesPerFrame), "sentence-a")
	if err != nil || next.Sequence != first.Sequence+1 || next.PTSMS != first.PTSMS+FrameDurationMS {
		t.Fatal(next, err)
	}
	e.PrepareInterrupt(29)
	e.ArmInterrupt(29, "sentence-d", "sentence-e")
	e.StartInterrupt(29)
	if e.CancelPreparedInterrupt(29, "sentence-d") {
		t.Fatal("cancelled a playing answer")
	}
}
