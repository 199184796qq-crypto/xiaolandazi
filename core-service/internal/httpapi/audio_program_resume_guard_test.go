package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
)

type conditionalResumeClient struct {
	legacyCalls, conditionalCalls int
	guard                         audioout.ProgramResumeGuard
	beforeCommit                  func()
}

func (f *conditionalResumeClient) StartProgram(context.Context, audioout.StartProgramInput) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{}, nil
}
func (f *conditionalResumeClient) PauseProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{}, nil
}
func (f *conditionalResumeClient) StopProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{}, nil
}
func (f *conditionalResumeClient) ResumeProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error) {
	f.legacyCalls++
	return audioout.RoomProgramSnapshot{Running: true}, nil
}
func (f *conditionalResumeClient) ResumeProgramIfCurrent(_ context.Context, _ int64, guard audioout.ProgramResumeGuard, allowed func() bool) (audioout.RoomProgramSnapshot, error) {
	f.conditionalCalls++
	f.guard = guard
	if f.beforeCommit != nil {
		f.beforeCommit()
	}
	if !allowed() {
		return audioout.RoomProgramSnapshot{}, coreaudio.ErrProgramResumeBaselineConflict
	}
	return audioout.RoomProgramSnapshot{ProgramID: guard.ExpectedProgramID, VersionID: guard.ExpectedVersionID, ControlRevision: guard.ExpectedControlRevision, Running: true}, nil
}

func TestConditionalResumeHTTPContractAndLegacyCompatibility(t *testing.T) {
	server := &Server{agentWork: agentwork.New()}
	agent, _ := server.agentWork.StartAgent(29, 0)
	guard := audioout.ProgramResumeGuard{ExpectedProgramID: "fixed-program", ExpectedVersionID: 208, ExpectedControlRevision: 0, ExpectedBootID: agent.BootID, ExpectedRunGeneration: agent.RunGeneration}
	raw, _ := json.Marshal(guard)
	client := &conditionalResumeClient{}
	for _, body := range []string{"", "{}", string(raw)} {
		request := httptest.NewRequest(http.MethodPost, "/internal/v1/rooms/29/audio/program/resume", strings.NewReader(body))
		if _, err := server.resumeRoomAudioProgramChecked(httptest.NewRecorder(), request, 29, client); err != nil {
			t.Fatal(body, err)
		}
	}
	if client.legacyCalls != 2 || client.conditionalCalls != 1 || client.guard != guard {
		t.Fatal("conditional request silently fell back or changed its zero revision", client)
	}
}

func TestConditionalResumeHTTPRejectsIncompleteAndReplacedRuntime(t *testing.T) {
	for _, test := range []string{"incomplete", "bad-boot", "bad-generation", "stopped", "stop-before-commit"} {
		t.Run(test, func(t *testing.T) {
			server := &Server{agentWork: agentwork.New()}
			agent, _ := server.agentWork.StartAgent(29, 0)
			guard := audioout.ProgramResumeGuard{ExpectedProgramID: "program", ExpectedVersionID: 208, ExpectedControlRevision: 3, ExpectedBootID: agent.BootID, ExpectedRunGeneration: agent.RunGeneration}
			client := &conditionalResumeClient{}
			switch test {
			case "bad-boot":
				guard.ExpectedBootID = "previous-boot"
			case "bad-generation":
				guard.ExpectedRunGeneration++
			case "stopped":
				_, _ = server.agentWork.StopAgent(29, agentwork.StopReasonManual)
			case "stop-before-commit":
				client.beforeCommit = func() { _, _ = server.agentWork.StopAgent(29, agentwork.StopReasonManual) }
			}
			raw, _ := json.Marshal(guard)
			if test == "incomplete" {
				raw = []byte(`{"expected_program_id":"program"}`)
			}
			request := httptest.NewRequest(http.MethodPost, "/resume", strings.NewReader(string(raw)))
			_, err := server.resumeRoomAudioProgramChecked(httptest.NewRecorder(), request, 29, client)
			if !errors.Is(err, coreaudio.ErrProgramResumeBaselineConflict) || client.legacyCalls != 0 {
				t.Fatal("stale conditional resume reached unconditional control", err, client)
			}
		})
	}
}
