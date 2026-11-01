package roomaudio

import "testing"

func TestTimedCaptionsClearInGapsAndOnUntimedTrack(t *testing.T) {
	e := New()
	e.SetMainlineTimeline(29, []SpeechSegment{{"real", 40, 80, "真正字幕"}})
	_, _ = e.StartMainline(29, "real")
	pcm := make([]byte, PCMBytesPerFrame)
	for _, tc := range []struct {
		cursor int
		want   bool
	}{{0, false}, {40, true}, {80, false}, {100, false}} {
		if _, err := e.PublishPCMAt(29, SourceMainline, pcm, "real", tc.cursor); err != nil {
			t.Fatal(err)
		}
		feed := e.Snapshot(29).SpeechFeed
		if (feed.Current != nil) != tc.want {
			t.Fatalf("cursor=%d feed=%+v", tc.cursor, feed)
		}
	}
	e.SetMainlineTimeline(29, nil)
	_, _ = e.StartMainline(29, "real") // Old/recycled IDs cannot restore captions.
	_, _ = e.PublishPCMAt(29, SourceMainline, pcm, "real", 0)
	feed := e.Snapshot(29).SpeechFeed
	if feed.Current != nil || feed.Previous != nil || feed.Next != nil {
		t.Fatal(feed)
	}
}

func TestEstimatedInteractionTranscriptIsInternalOnly(t *testing.T) {
	e := New()
	var spoken []SpokenSegment
	e.SetSpokenObserver(func(s SpokenSegment) { spoken = append(spoken, s) })
	e.SetMainlineTimeline(29, []SpeechSegment{{"main", 0, 100, "主线字幕"}})
	_, _ = e.StartMainline(29, "main")
	_, _ = e.PublishPCMAt(29, SourceMainline, make([]byte, PCMBytesPerFrame), "main", 0)
	e.SetInterruptTranscriptTimeline(29, []SpeechSegment{{"estimate", 0, 100, "想要的扣1。"}})
	_, _ = e.PrepareInterrupt(29)
	_, _ = e.ArmInterrupt(29, "main", "main")
	if e.Snapshot(29).SpeechFeed.Next != nil {
		t.Fatal("estimated transcript leaked into upcoming captions")
	}
	_, _ = e.StartInterrupt(29)
	_, _ = e.PublishPCMAt(29, SourceInterrupt, make([]byte, PCMBytesPerFrame), "", 0)
	feed := e.Snapshot(29).SpeechFeed
	if feed.Current != nil || feed.Previous != nil || feed.Next != nil {
		t.Fatal("untimed interruption retained mainline subtitles", feed)
	}
	if len(spoken) != 2 || spoken[1].Text != "想要的扣1。" {
		t.Fatal("internal observer changed", spoken)
	}
	for _, cue := range e.CompositeCues(29) {
		if cue.Source == SourceInterrupt {
			t.Fatal("estimated transcript exported as SRT", cue)
		}
	}
	e.AppendInterruptTranscriptSegment(29, SpeechSegment{"bridge", 20, 40, "仅有桥接原文"})
	_, _ = e.PublishPCMAt(29, SourceInterrupt, make([]byte, PCMBytesPerFrame), "", 20)
	if e.Snapshot(29).SpeechFeed.Current != nil {
		t.Fatal("untimed bridge leaked")
	}
	e.SetInterruptTimeline(29, []SpeechSegment{{"estimate", 0, 100, "真实互动字幕"}})
	_, _ = e.PublishPCMAt(29, SourceInterrupt, make([]byte, PCMBytesPerFrame), "", 0)
	if feed := e.Snapshot(29).SpeechFeed; feed.Current == nil || feed.Current.Text != "真实互动字幕" {
		t.Fatal(feed)
	}
	e.Reset(29)
	for _, cue := range e.CompositeCues(29) {
		if cue.Text == "想要的扣1。" || cue.Text == "仅有桥接原文" {
			t.Fatal("stopped-session export leaked transcript", cue)
		}
	}
}
