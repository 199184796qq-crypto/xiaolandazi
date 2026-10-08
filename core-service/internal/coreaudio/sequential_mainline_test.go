package coreaudio

import (
	"context"
	"livecompanion/core/internal/audioout"
	"testing"
	"time"
)

func sequentialFixture(t *testing.T) (*Client, *programState, audioout.RoomProgramSnapshot, string) {
	client, program, start, server := startRefreshFixture(t)
	client.mu.Lock()
	program.Sequential = true
	program.Tracks = program.Tracks[:2]
	client.mu.Unlock()
	return client, program, start, server.URL
}

func TestSequentialMainlineWaitsAtExhaustionAndResumesFreshBatchWithoutSessionRestart(t *testing.T) {
	client, program, start, url := sequentialFixture(t)
	next, _, _ := client.selectNextMainlineTrack(program, 0)
	if next != 1 {
		t.Fatal("sequential queue reordered", next)
	}
	client.advanceMainline(program, start.Task.ID)
	second, _ := client.ProgramSnapshot(context.Background(), 21)
	if second.TrackID != "B" || second.WaitingForContent {
		t.Fatal(second)
	}
	client.advanceMainline(program, second.Task.ID)
	exhausted, _ := client.ProgramSnapshot(context.Background(), 21)
	if !exhausted.Running || !exhausted.WaitingForContent || exhausted.Sequence != second.Sequence {
		t.Fatal("replayed after exhaustion", exhausted)
	}
	input := refreshFixtureInput(start, url)
	input.Tracks = []audioout.ProgramTrack{{ID: "fresh-C", AudioURL: url + "/new-c", DurationMS: 60000}, {ID: "fresh-D", AudioURL: url + "/new-d", DurationMS: 60000}}
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := client.ProgramSnapshot(context.Background(), 21)
		if snapshot.Generation == 1 {
			if snapshot.WaitingForContent || snapshot.TrackID != "fresh-C" || snapshot.ProgramID != start.ProgramID || snapshot.Task.SessionID != start.Task.SessionID {
				t.Fatal(snapshot)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fresh batch did not resume at waiting boundary")
}

func TestSequentialExhaustionPauseDoesNotReplayAndStagedBatchWaitsForResume(t *testing.T) {
	client, program, start, url := sequentialFixture(t)
	client.advanceMainline(program, start.Task.ID)
	second, _ := client.ProgramSnapshot(context.Background(), 21)
	client.advanceMainline(program, second.Task.ID)
	if _, err := client.PauseProgram(context.Background(), 21); err != nil {
		t.Fatal(err)
	}
	noContent, err := client.ResumeProgram(context.Background(), 21)
	if err != nil || !noContent.WaitingForContent || noContent.Sequence != second.Sequence {
		t.Fatal("resuming empty queue replayed", noContent, err)
	}
	_, _ = client.PauseProgram(context.Background(), 21)
	input := refreshFixtureInput(start, url)
	input.Tracks = []audioout.ProgramTrack{{ID: "fresh", AudioURL: url, DurationMS: 60000}}
	staged, err := client.RefreshProgram(context.Background(), input)
	if err != nil || staged.Generation != 0 || !staged.Suspended {
		t.Fatal(staged, err)
	}
	resumed, err := client.ResumeProgram(context.Background(), 21)
	if err != nil || resumed.Generation != 1 || resumed.TrackID != "fresh" || resumed.WaitingForContent {
		t.Fatal("new batch not resumed correctly", resumed, err)
	}
}

func TestSequentialEmptyQueueNeverSelectsPreviousTrack(t *testing.T) {
	client, program, _, _ := sequentialFixture(t)
	if index, _, _ := client.selectNextMainlineTrack(program, 1); index != -1 {
		t.Fatal("last track repeats", index)
	}
}
