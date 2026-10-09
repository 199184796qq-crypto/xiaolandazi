package roomaudio

import (
	"strings"
	"testing"
)

func TestCompositeSRTUsesActualPCMAndIncludesIndependentBridge(t *testing.T) {
	e := New()
	room := int64(81)
	pcm := make([]byte, PCMBytesPerFrame)
	e.SetMainlineTimeline(room, []SpeechSegment{{"main1", 0, 100, "切点前主线"}, {"main2", 100, 200, "回归主线"}})
	if _, err := e.StartMainline(room, "main1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PublishPCMAt(room, SourceMainline, pcm, "main1", 0); err != nil {
		t.Fatal(err)
	}
	e.SetInterruptTimeline(room, []SpeechSegment{{"answer", 0, 100, "核心回答"}})
	if _, err := e.PrepareInterrupt(room); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ArmInterrupt(room, "main1", "main2"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartInterrupt(room); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PublishPCMAt(room, SourceInterrupt, pcm, "", 0); err != nil {
		t.Fatal(err)
	}
	e.AppendInterruptSpeechSegment(room, SpeechSegment{"bridge", 20, 40, "刚才说到吃法，咱们接着讲。"})
	if strings.Contains(e.CompositeSRT(room), "刚才") {
		t.Fatal("staged bridge was exported as played")
	}
	if _, err := e.PublishPCMAt(room, SourceInterrupt, pcm, "", 20); err != nil {
		t.Fatal(err)
	}
	if feed := e.Snapshot(room).SpeechFeed; feed.Current == nil || feed.Current.SegmentID != "bridge" || feed.Next == nil || feed.Next.SegmentID != "main2" {
		t.Fatal("bridge caption missing", feed)
	}
	if _, err := e.PrepareResume(room, "main2"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartResume(room, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CompleteResume(room); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PublishPCMAt(room, SourceMainline, pcm, "main2", 100); err != nil {
		t.Fatal(err)
	}
	cues := e.CompositeCues(room)
	if len(cues) != 4 {
		t.Fatal("wrong actual-output cues", cues)
	}
	for i, c := range cues {
		if c.StartMS != int64(i*20) || c.EndMS != int64((i+1)*20) {
			t.Fatal("SRT used source cursor instead of composite PTS", c)
		}
	}
	srt := e.CompositeSRT(room)
	if !strings.Contains(srt, "00:00:00,040 --> 00:00:00,060\n刚才说到吃法") {
		t.Fatal(srt)
	}
	e.Reset(room)
	_, cancel, err := e.Subscribe(room)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if e.CompositeSRT(room) != srt {
		t.Fatal("idle receiver erased ended-session captions")
	}
	e.ClearRoom(room)
	if e.CompositeSRT(room) != "" {
		t.Fatal("deleted room retained captions")
	}
}
