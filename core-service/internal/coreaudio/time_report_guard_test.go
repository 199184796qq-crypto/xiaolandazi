package coreaudio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
)

func TestReportIsRevalidatedAfterActualPCMCutWait(t *testing.T) {
	c, _ := newTestClient(t, 2000)
	engine := roomaudio.New()
	c.SetRoomAudioEngine(engine)
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(testWAV(2000)) }))
	defer audio.Close()
	captain := messagecaptain.New()
	c.SetMessageCaptain(captain)
	calls := 0
	var first time.Time
	remove := captain.Register("time-report-validity", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind != "interaction.v2.reserve-audio" {
			return nil
		}
		calls++
		var req struct {
			Decision string    `json:"decision_id"`
			Duration int       `json:"duration_ms"`
			Start    time.Time `json:"start_at"`
		}
		if err := json.Unmarshal(m.Payload, &req); err != nil {
			t.Fatal(err)
		}
		if req.Decision != "report" || req.Duration != 2000 {
			t.Fatal("actual duration/decision lost", req)
		}
		if calls == 1 {
			first = req.Start
			return nil
		}
		if req.Start.Before(first.Add(-80 * time.Millisecond)) {
			t.Fatal("actual start not refreshed after wait", req.Start, first)
		}
		return errors.New("original report time expired while waiting")
	})
	defer remove()
	start, err := c.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 96, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "main", AudioURL: audio.URL, DurationMS: 2000, Text: "第一句。下一句。", Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "first", Index: 1, EndMS: 800, Text: "第一句。", SafeCut: true}, {SegmentID: "second", Index: 2, StartMS: 800, EndMS: 2000, Text: "下一句。", SafeCut: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopProgram(context.Background(), 96)
	cut, resume := 800, 800
	_, err = c.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{RoomID: 96, ExpectedTaskID: start.Task.ID, DecisionID: "report", AudioURL: audio.URL, Text: "下午了。", SwitchAtMS: &cut, ResumeOffsetMS: &resume})
	if !errors.Is(err, errInteractionV2Wait) || calls != 2 {
		t.Fatal("cut wait bypassed final validity guard", err, calls)
	}
	snapshot, _ := c.ProgramSnapshot(context.Background(), 96)
	if snapshot.Suspended || engine.Snapshot(96).ActiveSource == roomaudio.SourceInterrupt {
		t.Fatal("expired report suspended or replaced mainline", snapshot)
	}
}
