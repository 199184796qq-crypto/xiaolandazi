package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/speechruntime"
)

type watchdogRecoveryClient struct {
	fakeAudioTaskClient
	currentTask string
	running     bool
	recovered   []string
	recoverErr  error
}

func (f *watchdogRecoveryClient) ProgramSnapshot(_ context.Context, room int64) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{RoomID: room, Running: f.running, Task: &audioout.SpeechTask{ID: f.currentTask}}, nil
}

func (f *watchdogRecoveryClient) RecoverProgramInteraction(_ context.Context, _ int64, task string) (audioout.RoomProgramSnapshot, error) {
	f.recovered = append(f.recovered, task)
	return audioout.RoomProgramSnapshot{}, f.recoverErr
}

func TestMissingInteractionTerminalRecoversMainlineWithoutCompletingFailedAnswer(t *testing.T) {
	server := New(nil, nil, nil, nil, nil, "development", "token")
	client := &watchdogRecoveryClient{currentTask: "answer", running: true}
	server.SetAudioClient(client, "")
	started := time.Now().Add(-10 * time.Second)
	_, err := server.speechRuntime.Update(29, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusPlaying, SpeechTaskID: "answer", DecisionID: "decision", StartedAt: &started, DurationMS: 1000})
	if err != nil {
		t.Fatal(err)
	}
	server.expireRoomAudioInteraction(29, audioout.SpeechTask{ID: "answer", DurationMS: 1000}, "decision", started, true)
	if len(client.recovered) != 1 || client.recovered[0] != "answer" || client.completedTaskID != "" {
		t.Fatal("watchdog did not take failure recovery path", client)
	}
	snapshot, _ := server.speechRuntime.Snapshot(29)
	if snapshot.Interrupt.Status != speechruntime.StatusFailed {
		t.Fatal("invented completed playback", snapshot)
	}
	server.expireRoomAudioInteraction(29, audioout.SpeechTask{ID: "answer", DurationMS: 1000}, "decision", started, true)
	if len(client.recovered) != 1 {
		t.Fatal("watchdog completed twice")
	}
}

func TestInteractionWatchdogCannotResumeNewOrStoppedTask(t *testing.T) {
	for _, test := range []struct {
		name, current string
		running       bool
	}{
		{"replacement", "new-answer", true}, {"stopped", "answer", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := New(nil, nil, nil, nil, nil, "development", "token")
			client := &watchdogRecoveryClient{currentTask: test.current, running: test.running}
			server.SetAudioClient(client, "")
			_, _ = server.speechRuntime.Update(29, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusFailed, SpeechTaskID: "answer", DecisionID: "decision"})
			server.recoverExpiredRoomAudioInteraction(29, "answer", "decision", 0)
			if len(client.recovered) != 0 {
				t.Fatal("watchdog revived wrong lifecycle")
			}
		})
	}
}

func TestInteractionRecoveryRetryLosesPermissionAfterRuntimeReplacement(t *testing.T) {
	server := New(nil, nil, nil, nil, nil, "development", "token")
	client := &watchdogRecoveryClient{currentTask: "answer", running: true, recoverErr: errors.New("temporary publish error")}
	server.SetAudioClient(client, "")
	_, _ = server.speechRuntime.Update(29, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusFailed, SpeechTaskID: "answer", DecisionID: "decision"})
	server.recoverExpiredRoomAudioInteraction(29, "answer", "decision", 0)
	_, _ = server.speechRuntime.Update(29, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusPaused, SpeechTaskID: "new", DecisionID: "new-decision"})
	// Invoke the same callback directly; the scheduled callback takes this guard too.
	server.recoverExpiredRoomAudioInteraction(29, "answer", "decision", 1)
	if len(client.recovered) != 1 {
		t.Fatal("old recovery crossed task replacement")
	}
}
