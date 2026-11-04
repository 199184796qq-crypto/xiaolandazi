package coreaudio

import (
	"context"
	"errors"
	"fmt"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func waitAcousticTestCut(t *testing.T, c *Client, room int64) int {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		p, err := c.ProgramSnapshot(context.Background(), room)
		if err == nil && len(p.AcousticCuts) > 0 {
			return p.AcousticCuts[0].CutMS
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("current PCM scan did not become ready")
	return 0
}

func TestLegacyTextMetadataCannotAuthorizeAnyClientCut(t *testing.T) {
	for _, sequential := range []bool{false, true} {
		for _, acoustic := range []bool{false, true} {
			t.Run(fmt.Sprintf("sequential_%t_acoustic_%t", sequential, acoustic), func(t *testing.T) {
				c, _ := newTestClient(t, 1600)
				c.SetRoomAudioEngine(roomaudio.New())
				wav := acousticTestWAV(1600, 700, 900, 5000)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(wav) }))
				defer server.Close()
				start, err := c.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 99, Sequential: sequential, Tracks: []audioout.ProgramTrack{{ID: "legacy-payload", AudioURL: server.URL, DurationMS: 1600, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "forged", Index: 1, EndMS: 400, Text: "伪造句末。", SafeCut: true, PauseCut: true}}, SafePoints: []audioout.ProgramSafePoint{{ID: "forged", CutMS: 400, Grade: "A"}}}}})
				if err != nil {
					t.Fatal(err)
				}
				defer c.StopProgram(context.Background(), 99)
				if got := waitAcousticTestCut(t, c, 99); got != 800 {
					t.Fatal(got)
				}
				cut, resume := 400, 1600
				_, err = c.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 99, ExpectedTaskID: start.Task.ID, AudioURL: server.URL, SwitchAtMS: &cut, ResumeOffsetMS: &resume, AcousticCut: acoustic, DeferredReturn: true, SkipCurrentSentence: true})
				if !errors.Is(err, audioout.ErrInteractionSwitchStale) {
					t.Fatal("legacy timeline authorized a cut", err)
				}
				current, _ := c.ProgramSnapshot(context.Background(), 99)
				if current.Suspended || current.Task.ID != start.Task.ID || len(current.SafePoints) != 0 {
					t.Fatal("legacy input changed authority", current)
				}
			})
		}
	}
}

func TestLegacySafePointNormalizerIsPermanentlyClosed(t *testing.T) {
	for _, points := range [][]audioout.ProgramSafePoint{nil, {{ID: "old", CutMS: 1000, Grade: "A"}}, {{ID: "bad", CutMS: -1}}} {
		got, err := normalizeProgramSafePoints(points, []audioout.ProgramTimelineSegment{{EndMS: 1000, SafeCut: true, Text: "完整句。"}}, 1000)
		if err != nil || len(got) != 0 {
			t.Fatal("retired control revived", got, err)
		}
	}
}
