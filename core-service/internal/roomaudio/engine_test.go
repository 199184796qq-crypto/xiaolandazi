package roomaudio

import (
	"testing"
	"time"
)

func TestCompositeClockOnlyAdvancesForActiveSource(t *testing.T) {
	engine := New()
	if _, err := engine.StartMainline(7, "S001"); err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, PCMBytesPerFrame)
	first, err := engine.PublishPCM(7, SourceMainline, pcm, "S001")
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || first.PTSMS != 0 {
		t.Fatalf("first frame=%#v", first)
	}
	if _, err := engine.PublishPCM(7, SourceInterrupt, pcm, ""); err == nil {
		t.Fatal("inactive interrupt source must not reach composite output")
	}
	second, err := engine.PublishPCM(7, SourceMainline, pcm, "S001")
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 || second.PTSMS != FrameDurationMS {
		t.Fatalf("second frame=%#v", second)
	}
}

func TestInterruptAndResumeStateMachine(t *testing.T) {
	engine := New()
	if _, err := engine.StartMainline(9, "S010"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareInterrupt(9); err != nil {
		t.Fatal(err)
	}
	armed, err := engine.ArmInterrupt(9, "S011", "S012")
	if err != nil {
		t.Fatal(err)
	}
	if armed.Phase != PhaseArmed || armed.ActiveSource != SourceMainline || armed.PlannedCutSegmentID != "S011" {
		t.Fatalf("armed=%#v", armed)
	}
	if _, err := engine.StartInterrupt(9); err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, PCMBytesPerFrame)
	if _, err := engine.PublishPCM(9, SourceMainline, pcm, "S011"); err == nil {
		t.Fatal("mainline must not emit while interrupt is foreground")
	}
	if _, err := engine.PublishPCM(9, SourceInterrupt, pcm, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareResume(9, "S012"); err != nil {
		t.Fatal(err)
	}
	resume, err := engine.StartResume(9, "")
	if err != nil {
		t.Fatal(err)
	}
	if resume.Phase != PhaseResume || resume.ActiveSource != SourceMainline || resume.CurrentSegmentID != "S012" {
		t.Fatalf("resume=%#v", resume)
	}
	mainline, err := engine.CompleteResume(9)
	if err != nil {
		t.Fatal(err)
	}
	if mainline.Phase != PhaseMainline || mainline.PlannedCutSegmentID != "" || mainline.ResumeSegmentID != "" {
		t.Fatalf("mainline=%#v", mainline)
	}
}

func TestPauseFreezesCompositeClock(t *testing.T) {
	engine := New()
	if _, err := engine.StartMainline(11, "S001"); err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, PCMBytesPerFrame)
	if _, err := engine.PublishPCM(11, SourceMainline, pcm, "S001"); err != nil {
		t.Fatal(err)
	}
	paused, err := engine.Pause(11)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Phase != PhasePaused || paused.OutputPTSMS != FrameDurationMS {
		t.Fatalf("paused=%#v", paused)
	}
	if _, err := engine.PublishPCM(11, SourceMainline, pcm, "S001"); err == nil {
		t.Fatal("paused engine must reject output frames")
	}
	if _, err := engine.Resume(11); err != nil {
		t.Fatal(err)
	}
	frame, err := engine.PublishPCM(11, SourceMainline, pcm, "S001")
	if err != nil {
		t.Fatal(err)
	}
	if frame.PTSMS != FrameDurationMS {
		t.Fatalf("clock moved while paused: %#v", frame)
	}
}

func TestSubscribeReceivesOnlyCompositeFrames(t *testing.T) {
	engine := New()
	if _, err := engine.StartMainline(13, "S001"); err != nil {
		t.Fatal(err)
	}
	frames, cancel, err := engine.Subscribe(13)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	pcm := make([]byte, PCMBytesPerFrame)
	if _, err := engine.PublishPCM(13, SourceMainline, pcm, "S001"); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-frames:
		if frame.Source != SourceMainline || frame.Sequence != 1 {
			t.Fatalf("frame=%#v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive composite frame")
	}
}
