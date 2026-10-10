package coreaudio

import "testing"

func TestCompletelySkippedTrackHasZeroPlayedTerminalReceipt(t *testing.T) {
	p := &programState{Tracks: []programTrack{{ID: "story", DurationMS: 40000}}}
	finishPlaybackReceipt(p, "story")
	finishPlaybackReceipt(p, "story")
	if len(p.PlaybackReceipts) != 1 || !p.PlaybackReceipts[0].Terminal || p.PlaybackReceipts[0].Complete || p.PlaybackReceipts[0].PlayedMS != 0 {
		t.Fatal(p.PlaybackReceipts)
	}
}

func TestPlaybackCoverageRejectsSkippedAndDuplicateRanges(t *testing.T) {
	ranges, delta := mergePlaybackRanges(nil, 0, 12000)
	if delta != 12000 {
		t.Fatal(delta)
	}
	ranges, delta = mergePlaybackRanges(ranges, 0, 12000)
	if delta != 0 {
		t.Fatal("duplicate counted", delta)
	}
	ranges, delta = mergePlaybackRanges(ranges, 20000, 30000)
	if delta != 10000 || len(ranges) != 2 {
		t.Fatal(ranges, delta)
	}
	p := &programState{PlaybackCoverage: map[string]playbackCoverage{"L": {Ranges: ranges, Duration: 30000}}}
	finishPlaybackReceipt(p, "L")
	r := p.PlaybackReceipts[0]
	if r.Complete || !r.Terminal || r.PlayedMS != 22000 {
		t.Fatal("skipped material counted as completed", r)
	}
	finishPlaybackReceipt(p, "L")
	if p.PlaybackSequence != 1 {
		t.Fatal("duplicate terminal receipt")
	}
}
