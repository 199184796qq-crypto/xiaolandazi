package coreaudio

import (
	"context"
	"livecompanion/core/internal/audioout"
	"testing"
	"time"
)

func appendFixtureInput(start audioout.RoomProgramSnapshot, url string) audioout.AppendProgramInput {
	return audioout.AppendProgramInput{RoomID: start.RoomID, ExpectedProgramID: start.ProgramID, ExpectedVersionID: start.VersionID, Tracks: []audioout.ProgramTrack{{ID: "ready-unit-c", Label: "next", Text: "下一段完整内容。", AudioURL: url, DurationMS: 1000}}}
}
func TestAppendReadyUnitPreservesCursorAndIsIdempotent(t *testing.T) {
	client, program, start, url := sequentialFixture(t)
	client.mu.Lock()
	program.Tracks[1].Text = "回归后继续讲下一项规格。"
	client.mu.Unlock()
	input := appendFixtureInput(start, url)
	result, err := client.AppendProgram(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Task.ID != start.Task.ID || result.TrackID != start.TrackID || result.TrackCount != 3 {
		t.Fatal("append altered active playback", result)
	}
	if result.BufferedMainlineMS <= 0 || result.NextTrackText == "" {
		t.Fatal("missing queue inventory/context", result)
	}
	repeated, err := client.AppendProgram(context.Background(), input)
	if err != nil || repeated.TrackCount != 3 {
		t.Fatal("append repeated an immutable unit", repeated, err)
	}
	input.Tracks[0].Text = "different content"
	if _, err = client.AppendProgram(context.Background(), input); err == nil {
		t.Fatal("ID collision overwrote content")
	}
	input.ExpectedProgramID = "other-run"
	if _, err = client.AppendProgram(context.Background(), input); err == nil {
		t.Fatal("append entered a different run")
	}
}
func TestAppendDoesNotReplayCompletedTrackAfterPause(t *testing.T) {
	client, program, start, url := sequentialFixture(t)
	client.advanceMainline(program, start.Task.ID)
	second, _ := client.ProgramSnapshot(context.Background(), start.RoomID)
	client.advanceMainline(program, second.Task.ID)
	exhausted, _ := client.ProgramSnapshot(context.Background(), start.RoomID)
	if !exhausted.WaitingForContent {
		t.Fatal("fixture has not reached EOF")
	}
	if _, err := client.PauseProgram(context.Background(), start.RoomID); err != nil {
		t.Fatal(err)
	}
	input := appendFixtureInput(start, url)
	paused, err := client.AppendProgram(context.Background(), input)
	if err != nil || !paused.Suspended {
		t.Fatal("append resumed a paused broadcast", paused, err)
	}
	if _, err = client.ResumeProgram(context.Background(), start.RoomID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := client.ProgramSnapshot(context.Background(), start.RoomID)
		if current.TrackID == "ready-unit-c" && !current.WaitingForContent {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("resume failed to select the fresh appended unit")
}
