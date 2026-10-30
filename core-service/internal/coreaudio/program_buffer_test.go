package coreaudio

import (
	"testing"
	"time"
)

func TestProgramSnapshotReadyMainlineBuffer(t *testing.T) {
	for _, tt := range []struct {
		name       string
		sequential bool
		running    bool
		waiting    bool
		index      int
		position   int
		durations  []int
		want       int64
	}{
		{"original_566_progress", false, true, false, 0, 637030, []int{2414654}, 1777624},
		{"original_566_later_progress", false, true, false, 0, 718330, []int{2414654}, 1696324},
		{"original_near_end", false, true, false, 0, 8600, []int{10000}, 1400},
		{"original_past_end", false, true, false, 0, 11000, []int{10000}, 0},
		{"random_library_is_not_future_queue", false, true, false, 0, 500, []int{1000, 90000}, 500},
		{"random_selected_track_only", false, true, false, 1, 1200, []int{90000, 2000}, 800},
		{"sequential_ready_queue_preserved", true, true, false, 0, 500, []int{1000, 2000}, 2500},
		{"sequential_consumed_tracks_excluded", true, true, false, 1, 500, []int{90000, 2000}, 1500},
		{"original_waiting_not_ready", false, true, true, 0, 0, []int{90000}, 0},
		{"original_stopped", false, false, false, 0, 0, []int{90000}, 0},
		{"original_missing_current_track", false, true, false, -1, 0, []int{90000}, 0},
		{"original_unknown_duration", false, true, false, 0, 0, []int{0}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newTestClient(t, 20)
			program := &programState{RoomID: 91, Sequential: tt.sequential, Running: tt.running, WaitingForContent: tt.waiting, Suspended: true, ResumeOffsetMS: tt.position, TrackIndex: tt.index}
			for _, duration := range tt.durations {
				program.Tracks = append(program.Tracks, programTrack{DurationMS: duration})
			}
			snapshot := client.programSnapshotLocked(program, time.Now())
			if snapshot.BufferedMainlineMS != tt.want {
				t.Fatalf("ready buffer = %d, want %d; snapshot=%+v", snapshot.BufferedMainlineMS, tt.want, snapshot)
			}
		})
	}
}
