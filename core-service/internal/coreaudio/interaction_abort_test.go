package coreaudio

import (
	"context"
	"errors"
	"testing"

	"livecompanion/core/internal/roomaudio"
)

func TestAbortProgramInteractionReturnsToMainline(t *testing.T) {
	client, program, start, _ := sequentialFixture(t)
	client.hub.Expire(start.Task.ID)
	client.mu.Lock()
	program.Suspended = true
	program.CurrentTaskID = "interaction-1"
	program.ResumeOffsetMS = 2000
	client.mu.Unlock()

	snapshot, err := client.AbortProgramInteraction(context.Background(), program.RoomID, "interaction-1")
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Running || snapshot.Suspended || snapshot.Task == nil || snapshot.Task.ID == "interaction-1" {
		t.Fatalf("interaction not returned to mainline: %+v", snapshot)
	}
	if _, err := client.AbortProgramInteraction(context.Background(), program.RoomID, "interaction-1"); !errors.Is(err, ErrInteractionNotActive) {
		t.Fatalf("second abort should be inert, got %v", err)
	}
}

func TestAbortProgramInteractionStopsAtIdleWhenProgramStopped(t *testing.T) {
	client, program, _, _ := sequentialFixture(t)
	client.mu.Lock()
	program.Running = false
	program.Suspended = true
	program.CurrentTaskID = "interaction-2"
	client.mu.Unlock()

	snapshot, err := client.AbortProgramInteraction(context.Background(), program.RoomID, "interaction-2")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Running {
		t.Fatalf("stopped program revived: %+v", snapshot)
	}
	if phase := client.roomAudio.Snapshot(program.RoomID).Phase; phase != roomaudio.PhaseIdle {
		t.Fatalf("engine stayed in %s, want idle", phase)
	}
	client.mu.RLock()
	stale := program.CurrentTaskID
	client.mu.RUnlock()
	if stale != "" {
		t.Fatalf("dead interaction task retained: %q", stale)
	}
}

func TestAbortProgramInteractionRejectsStaleTask(t *testing.T) {
	client, program, _, _ := sequentialFixture(t)
	client.mu.Lock()
	program.Suspended = true
	program.CurrentTaskID = "new-interaction"
	client.mu.Unlock()

	if _, err := client.AbortProgramInteraction(context.Background(), program.RoomID, "old-interaction"); !errors.Is(err, ErrInteractionNotActive) {
		t.Fatalf("stale abort accepted: %v", err)
	}
	client.mu.RLock()
	current := program.CurrentTaskID
	client.mu.RUnlock()
	if current != "new-interaction" {
		t.Fatalf("stale abort mutated current task: %q", current)
	}
}
