package roomaudio

import "testing"

func TestCurrentInteractionTextRollsOnlyDuringAudioAndKeepsObserver(t *testing.T) {
	e := New()
	var spoken []SpokenSegment
	e.SetSpokenObserver(func(s SpokenSegment) { spoken = append(spoken, s) })
	_, _ = e.StartMainline(29, "main")
	pcm := make([]byte, PCMBytesPerFrame)
	_, _ = e.PublishPCMAt(29, SourceMainline, pcm, "", 0)
	text := "欢迎新来的朋友。想了解的可以提问。"
	e.SetInterruptCurrentText(29, []SpeechSegment{{"part1", 120, 160, "欢迎新来的朋友。"}, {"part2", 160, 220, "想了解的可以提问。"}}, text, 100)
	_, _ = e.PrepareInterrupt(29)
	_, _ = e.ArmInterrupt(29, "main", "main")
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil || f.Next != nil {
		t.Fatal("prepared text shown before audio", f)
	}
	_, _ = e.StartInterrupt(29)
	for _, tc := range []struct {
		at                      int
		previous, current, next string
	}{{0, "", "", ""}, {100, "", "", ""},
		{120, "", "欢迎新来的朋友。", "想了解的可以提问。"},
		{160, "欢迎新来的朋友。", "想了解的可以提问。", ""},
		{200, "欢迎新来的朋友。", "想了解的可以提问。", ""},
		{220, "", "", ""}, {240, "", "", ""}} {
		if _, err := e.PublishPCMAt(29, SourceInterrupt, pcm, "", tc.at); err != nil {
			t.Fatal(err)
		}
		f := e.Snapshot(29).SpeechFeed
		for _, row := range []struct {
			item *SpeechFeedItem
			want string
		}{{f.Previous, tc.previous}, {f.Current, tc.current}, {f.Next, tc.next}} {
			if (row.item != nil) != (row.want != "") || (row.item != nil && (row.item.Text != row.want || row.item.Tone != SpeechToneInterrupt)) {
				t.Fatalf("cursor=%d want=%q feed=%+v", tc.at, row.want, f)
			}
		}
	}
	if len(spoken) != 2 || spoken[0].Text != "欢迎新来的朋友。" || spoken[1].Text != "想了解的可以提问。" {
		t.Fatal("internal observer timing/text changed", spoken)
	}
	for _, cue := range e.CompositeCues(29) {
		if cue.Source == SourceInterrupt {
			t.Fatal("transcript exported as precise SRT", cue)
		}
	}
	_, _ = e.PrepareResume(29, "main")
	_, _ = e.StartResume(29, "main")
	_, _ = e.PublishPCMAt(29, SourceMainline, pcm, "", 20)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil || f.Next != nil || f.Previous != nil {
		t.Fatal("text leaked into untimed mainline", f)
	}
}

func TestCurrentInteractionTextCannotLeakAcrossRepliesOrOverrideRealTimeline(t *testing.T) {
	e := New()
	_, _ = e.StartMainline(29, "")
	_, _ = e.StartInterrupt(29)
	pcm := make([]byte, PCMBytesPerFrame)
	e.SetInterruptCurrentText(29, nil, "第一条正文", 100)
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 120)
	if f := e.Snapshot(29).SpeechFeed; f.Current == nil || f.Current.Text != "第一条正文" {
		t.Fatal(f)
	}
	e.SetInterruptCurrentText(29, nil, "下一条正文", 100)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil {
		t.Fatal("next reply leaked before PCM", f)
	}
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 120)
	if f := e.Snapshot(29).SpeechFeed; f.Current == nil || f.Current.Text != "下一条正文" {
		t.Fatal(f)
	}
	e.SetInterruptTimeline(29, []SpeechSegment{{"real", 120, 160, "真实字幕"}})
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 120)
	if f := e.Snapshot(29).SpeechFeed; f.Current == nil || f.Current.Text != "真实字幕" {
		t.Fatal("known text replaced real timeline", f)
	}
	e.SetInterruptCurrentText(29, nil, "   ", 100)
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 120)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil {
		t.Fatal("empty reply reused old text", f)
	}
	e.Reset(29)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil {
		t.Fatal("stopped reply retained text", f)
	}
}

func TestRollingInteractionDoesNotFillGapsOrPreviewUntimedBridge(t *testing.T) {
	e := New()
	_, _ = e.StartMainline(29, "")
	e.SetInterruptCurrentText(29, []SpeechSegment{{"one", 120, 160, "第一句。"}, {"two", 180, 220, "第二句。"}}, "第一句。第二句。", 100)
	_, _ = e.StartInterrupt(29)
	e.AppendInterruptTranscriptSegment(29, SpeechSegment{"bridge", 220, 260, "没有时间轴的旧桥接原文"})
	pcm := make([]byte, PCMBytesPerFrame)
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 160)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil || f.Previous != nil || f.Next != nil {
		t.Fatal("gap filled with full body", f)
	}
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 180)
	if f := e.Snapshot(29).SpeechFeed; f.Current == nil || f.Current.Text != "第二句。" || f.Next != nil {
		t.Fatal("untimed bridge preview leaked beyond TTS window", f)
	}
	_, _ = e.Pause(29)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil || f.Previous != nil || f.Next != nil {
		t.Fatal("paused reply retained rolling rows", f)
	}
	_, _ = e.Resume(29)
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 200)
	if f := e.Snapshot(29).SpeechFeed; f.Current == nil || f.Current.Text != "第二句。" {
		t.Fatal("resume lost caption cursor", f)
	}
	_, _ = e.PublishPCMAt(29, SourceInterrupt, pcm, "", 220)
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil || f.Previous != nil || f.Next != nil {
		t.Fatal("untimed bridge displayed after original audio", f)
	}
	_, _ = e.PrepareResume(29, "")
	if f := e.Snapshot(29).SpeechFeed; f.Current != nil || f.Previous != nil || f.Next != nil {
		t.Fatal("preparing resume retained reply", f)
	}
}
