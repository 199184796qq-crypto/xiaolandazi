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

func TestSpeechFeedFollowsCompositeSourceTimeline(t *testing.T) {
	engine := New()
	engine.SetMainlineTimeline(21, []SpeechSegment{
		{SegmentID: "S001", StartMS: 0, EndMS: 1000, Text: "第一段主线"},
		{SegmentID: "S002", StartMS: 1000, EndMS: 2000, Text: "第二段主线"},
		{SegmentID: "S003", StartMS: 2000, EndMS: 3000, Text: "第三段主线"},
	})
	if _, err := engine.StartMainline(21, "S001"); err != nil {
		t.Fatal(err)
	}
	engine.SetInterruptTimeline(21, []SpeechSegment{
		{SegmentID: "interrupt-001", StartMS: 0, EndMS: 900, Text: "第一段插入"},
		{SegmentID: "interrupt-002", StartMS: 900, EndMS: 1800, Text: "第二段插入"},
	})
	pcm := make([]byte, PCMBytesPerFrame)
	if _, err := engine.PublishPCMAt(21, SourceMainline, pcm, "S002", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareInterrupt(21); err != nil {
		t.Fatal(err)
	}
	armed, err := engine.ArmInterrupt(21, "S002", "S003")
	if err != nil {
		t.Fatal(err)
	}
	if armed.SpeechFeed.Current == nil || armed.SpeechFeed.Current.Text != "第二段主线" || armed.SpeechFeed.Current.Tone != SpeechToneCut {
		t.Fatalf("armed feed=%#v", armed.SpeechFeed)
	}
	if armed.SpeechFeed.Next == nil || armed.SpeechFeed.Next.Text != "第一段插入" || armed.SpeechFeed.Next.Tone != SpeechToneInterrupt {
		t.Fatalf("armed next=%#v", armed.SpeechFeed.Next)
	}
	if _, err := engine.StartInterrupt(21); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PublishPCMAt(21, SourceInterrupt, pcm, "", 0); err != nil {
		t.Fatal(err)
	}
	firstInterrupt := engine.Snapshot(21)
	if firstInterrupt.SpeechFeed.Current == nil || firstInterrupt.SpeechFeed.Current.Text != "第一段插入" || firstInterrupt.SpeechFeed.Current.Tone != SpeechToneInterrupt {
		t.Fatalf("first interrupt feed=%#v", firstInterrupt.SpeechFeed)
	}
	if firstInterrupt.SpeechFeed.Previous == nil || firstInterrupt.SpeechFeed.Previous.Tone != SpeechToneCut {
		t.Fatalf("interrupt previous=%#v", firstInterrupt.SpeechFeed.Previous)
	}
	if _, err := engine.PublishPCMAt(21, SourceInterrupt, pcm, "", 1000); err != nil {
		t.Fatal(err)
	}
	secondInterrupt := engine.Snapshot(21)
	if secondInterrupt.SpeechFeed.Current == nil || secondInterrupt.SpeechFeed.Current.Text != "第二段插入" {
		t.Fatalf("second interrupt feed=%#v", secondInterrupt.SpeechFeed)
	}
	if secondInterrupt.SpeechFeed.Next == nil || secondInterrupt.SpeechFeed.Next.Text != "第三段主线" || secondInterrupt.SpeechFeed.Next.Tone != SpeechToneResume {
		t.Fatalf("interrupt next=%#v", secondInterrupt.SpeechFeed.Next)
	}
	if _, err := engine.PrepareResume(21, "S003"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.StartResume(21, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.CompleteResume(21); err != nil {
		t.Fatal(err)
	}
	resumed := engine.Snapshot(21)
	if resumed.SpeechFeed.Current == nil || resumed.SpeechFeed.Current.Text != "第三段主线" || resumed.SpeechFeed.Current.Tone != SpeechToneResume {
		t.Fatalf("resume feed=%#v", resumed.SpeechFeed)
	}
	if resumed.SpeechFeed.Previous == nil || resumed.SpeechFeed.Previous.Text != "第二段插入" || resumed.SpeechFeed.Previous.Tone != SpeechToneInterrupt {
		t.Fatalf("resume previous=%#v", resumed.SpeechFeed.Previous)
	}
}
