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

func TestAppendExactDuplicateOmittedAndExplicitRepeatRetained(t *testing.T) {
	client, program, start, url := sequentialFixture(t)
	client.mu.Lock()
	program.Tracks[1].Text = "怕买整只吃不完浪费，这个半只装刚好。"
	client.mu.Unlock()
	input := appendFixtureInput(start, url)
	input.Tracks[0].Text = program.Tracks[1].Text
	for attempt := 0; attempt < 2; attempt++ {
		current, err := client.AppendProgram(context.Background(), input)
		if err != nil || current.TrackCount != 2 || current.Task.ID != start.Task.ID {
			t.Fatal("exact duplicate queued, retry changed playback, or stream stopped", current, err)
		}
	}
	input.Tracks[0].ID = "explicit-repeat"
	input.Tracks[0].AllowConsecutiveRepeat = true
	current, err := client.AppendProgram(context.Background(), input)
	if err != nil || current.TrackCount != 3 {
		t.Fatal("explicit repeat omitted", current, err)
	}
	input.Tracks[0].ID = "changed-wording"
	input.Tracks[0].AllowConsecutiveRepeat = false
	input.Tracks[0].Text = "那怕买整只吃不完浪费，这个半只装刚好。"
	current, err = client.AppendProgram(context.Background(), input)
	if err != nil || current.TrackCount != 4 {
		t.Fatal("near-match omitted", current, err)
	}
	input.Tracks[0].ID = "nonconsecutive"
	input.Tracks[0].Text = program.Tracks[1].Text
	current, err = client.AppendProgram(context.Background(), input)
	if err != nil || current.TrackCount != 5 {
		t.Fatal("nonconsecutive repeat omitted", current, err)
	}
}

func TestSequentialStartKeepsOneIdenticalClipAndExplicitRepeat(t *testing.T) {
	client, _, _, url := sequentialFixture(t)
	result, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 93, Sequential: true, Tracks: []audioout.ProgramTrack{
		{ID: "one", Text: "欢迎老乡回来。", AudioURL: url, DurationMS: 1000},
		{ID: "two", Text: "欢迎老乡回来。", AudioURL: url, DurationMS: 1000},
		{ID: "three", Text: "欢迎老乡回来。", AudioURL: url, DurationMS: 1000, AllowConsecutiveRepeat: true},
	}})
	if err != nil || result.TrackCount != 2 || !result.Running {
		t.Fatal("incorrect initial queue", result, err)
	}
	defer client.StopProgram(context.Background(), 93)
}
