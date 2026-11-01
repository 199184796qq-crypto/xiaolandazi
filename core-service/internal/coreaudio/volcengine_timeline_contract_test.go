package coreaudio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"livecompanion/core/internal/audioout"
)

// Explicit offline opt-in: these are the management producer's actual JSON
// tracks derived from the already generated WAV and native word timestamps.
func TestVolcengineLongNativeTimelineContractOffline(t *testing.T) {
	dir := os.Getenv("VOLCENGINE_LONG_OFFLINE_OUTPUT")
	if dir == "" {
		t.Skip("offline management track fixture not requested")
	}
	for _, pct := range []int{90, 110} {
		t.Run(fmt.Sprint(pct), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("core-track-%d.json", pct)))
			if err != nil {
				t.Fatal(err)
			}
			var track audioout.ProgramTrack
			if err = json.Unmarshal(raw, &track); err != nil {
				t.Fatal(err)
			}
			timeline, err := normalizeProgramTimeline(track.Timeline, track.DurationMS)
			if err != nil {
				t.Fatal(err)
			}
			points, err := normalizeProgramSafePoints(track.SafePoints, timeline, track.DurationMS)
			if err != nil {
				t.Fatal(err)
			}
			if len(points) < 5 || len(timeline) < 20 {
				t.Fatal("real native boundaries lost")
			}
			var quickOnly int
			for _, segment := range timeline {
				if segment.PauseCut && !segment.SafeCut {
					quickOnly++
				}
			}
			if quickOnly == 0 {
				t.Fatal("quick punctuation cuts lost")
			}
			t.Logf("tempo=%d cues=%d semantic_points=%d quick_pause_only=%d duration_ms=%d", pct, len(timeline), len(points), quickOnly, track.DurationMS)
		})
	}
}
