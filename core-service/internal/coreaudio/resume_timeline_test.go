package coreaudio

import "testing"

func TestPrepareProgramResumeLockedAdvancesAtTrackEnd(t *testing.T) {
	program := &programState{
		TrackIndex: 0,
		Tracks: []programTrack{
			{ID: "A", DurationMS: 10000},
			{ID: "B", DurationMS: 12000},
		},
	}
	resume := prepareProgramResumeLocked(program, 10000)
	if resume != 0 || program.TrackIndex != 1 {
		t.Fatalf("resume=%d track=%d, want resume=0 track=1", resume, program.TrackIndex)
	}
}

func TestPrepareProgramResumeLockedWrapsToFirstTrack(t *testing.T) {
	program := &programState{
		TrackIndex: 1,
		Tracks: []programTrack{
			{ID: "A", DurationMS: 10000},
			{ID: "B", DurationMS: 12000},
		},
	}
	resume := prepareProgramResumeLocked(program, 12000)
	if resume != 0 || program.TrackIndex != 0 {
		t.Fatalf("resume=%d track=%d, want resume=0 track=0", resume, program.TrackIndex)
	}
}
