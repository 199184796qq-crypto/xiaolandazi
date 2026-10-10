package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"testing"
)

func TestMainlineExactDuplicateCleanupKeepsRawAndBindsNewVoiceText(t *testing.T) {
	for _, c := range []struct {
		previous, next, want string
		allow                bool
		count                int
	}{
		{"先看规格。半只装刚好。", "半只装刚好。再看包装。", "再看包装。", false, 1},
		{"半只装刚好。再看包装。", "半只装刚好。再看包装。", "", false, 1},
		{"半只装刚好。再看包装。", "半只装刚好。", "半只装刚好。", false, 0},
		{"半只装刚好。", "半只装刚好！再看包装。", "半只装刚好！再看包装。", false, 0},
		{"半只装刚好。", "半只装刚好。再看包装。", "半只装刚好。再看包装。", true, 0},
	} {
		state := &anchorcycle.State{Config: anchorcycle.Config{AllowConsecutiveRepeat: c.allow}, Accepted: []anchorcycle.Segment{{Selected: anchorcycle.Candidate{Text: c.previous, OutputText: c.previous}}}}
		candidate := anchorcycle.Candidate{Text: c.next, RawText: c.next, OutputText: c.next, CacheID: "old-voice"}
		cleanCycleConsecutiveSpeech(state, &candidate)
		if candidate.OutputText != c.want || candidate.RawText != c.next || candidate.ExactDuplicateRemovals != c.count {
			t.Fatalf("%+v got=%+v", c, candidate)
		}
		if c.count > 0 && candidate.CacheID != "" {
			t.Fatal("old cached audio survived a text change")
		}
		if c.count == 0 && candidate.CacheID != "old-voice" {
			t.Fatal("unmodified cached audio invalidated")
		}
	}
}
