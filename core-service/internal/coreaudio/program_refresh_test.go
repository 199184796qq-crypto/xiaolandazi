package coreaudio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
)

func startRefreshFixture(t *testing.T) (*Client, *programState, audioout.RoomProgramSnapshot, *httptest.Server) {
	t.Helper()
	client, _ := newTestClient(t, 60000)
	server := newWAVServer(t, client.testPath)
	start, err := client.startPreparedProgram(21, "existing-billed-session", "existing program", 10, 1, []programTrack{
		{ID: "A", AudioURL: server.URL + "/old-a", DurationMS: 60000},
		{ID: "B", AudioURL: server.URL + "/old-b", DurationMS: 60000},
		{ID: "C", AudioURL: server.URL + "/old-c", DurationMS: 60000},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = client.StopProgram(context.Background(), 21); server.Close() })
	return client, client.programs[21], start, server
}

func refreshFixtureInput(start audioout.RoomProgramSnapshot, url string) audioout.RefreshProgramInput {
	return audioout.RefreshProgramInput{
		RoomID: 21, JobID: "refresh-job-1", ExpectedProgramID: start.ProgramID,
		ExpectedVersionID: 10, ExpectedGeneration: 0, Generation: 1, NewVersionID: 11, NewVersionNo: 2,
		ValidUntil: time.Now().Add(30 * time.Second),
		Tracks: []audioout.ProgramTrack{
			{ID: "A", AudioURL: url + "/new-a", DurationMS: 60000},
			{ID: "B", AudioURL: url + "/new-b", DurationMS: 60000},
			{ID: "C", AudioURL: url + "/new-c", DurationMS: 60000},
		},
	}
}

func TestProgramRefreshPreservesCurrentAndQueuedTrackThenAppliesIdempotently(t *testing.T) {
	client, program, start, server := startRefreshFixture(t)
	client.mu.Lock()
	program.PlannedForTaskID, program.PlannedNextTrack = start.Task.ID, 1
	client.mu.Unlock()
	input := refreshFixtureInput(start, server.URL)
	staged, err := client.RefreshProgram(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Generation != 0 || staged.PendingGeneration != 1 || staged.VersionID != 10 || staged.Task.ID != start.Task.ID || staged.Task.AudioURL != start.Task.AudioURL {
		t.Fatalf("staging altered current playback: %+v", staged)
	}
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatalf("pending retry: %v", err)
	}
	conflicting := input
	conflicting.NewVersionID = 12
	if _, err := client.RefreshProgram(context.Background(), conflicting); !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("different payload accepted: %v", err)
	}
	conflicting = input
	conflicting.JobID = "concurrent-job"
	if _, err := client.RefreshProgram(context.Background(), conflicting); !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("second pending job accepted: %v", err)
	}
	client.advanceMainline(program, start.Task.ID)
	queued, _ := client.ProgramSnapshot(context.Background(), 21)
	if queued.TrackID != "B" || queued.Generation != 0 || !strings.HasSuffix(queued.Task.AudioURL, "/old-b") {
		t.Fatalf("queued old audio was replaced: %+v", queued)
	}
	client.planNextMainline(program, queued.Task.ID)
	client.advanceMainline(program, queued.Task.ID)
	applied, _ := client.ProgramSnapshot(context.Background(), 21)
	if applied.Generation != 1 || applied.VersionID != 11 || applied.VersionNo != 2 || applied.PendingJobID != "" || applied.LastAppliedJobID != input.JobID || !strings.Contains(applied.Task.AudioURL, "/new-") {
		t.Fatalf("not applied at subsequent boundary: %+v", applied)
	}
	if applied.ProgramID != start.ProgramID || applied.Task.SessionID != start.Task.SessionID || !applied.StartedAt.Equal(start.StartedAt) || applied.Sequence != start.Sequence+2 {
		t.Fatal("refresh restarted session/program/billing timeline")
	}
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatalf("applied retry: %v", err)
	}
}

func TestProgramRefreshWaitsForPausedMainlineAndRejectsStoppedOrNewProgram(t *testing.T) {
	client, program, start, server := startRefreshFixture(t)
	if _, err := client.PauseProgram(context.Background(), 21); err != nil {
		t.Fatal(err)
	}
	input := refreshFixtureInput(start, server.URL)
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	client.advanceMainline(program, start.Task.ID)
	paused, _ := client.ProgramSnapshot(context.Background(), 21)
	if !paused.Suspended || paused.Generation != 0 {
		t.Fatal("refresh bypassed pause")
	}
	resumed, err := client.ResumeProgram(context.Background(), 21)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Generation != 0 || resumed.TrackID != "A" {
		t.Fatal("refresh interrupted resumed old track")
	}
	client.advanceMainline(program, resumed.Task.ID)
	applied, _ := client.ProgramSnapshot(context.Background(), 21)
	if applied.Generation != 1 {
		t.Fatal("did not apply after resumed track ended")
	}
	_, _ = client.StopProgram(context.Background(), 21)
	if _, err := client.RefreshProgram(context.Background(), input); !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("stopped program accepted: %v", err)
	}
	_, err = client.startPreparedProgram(21, "manual-new-session", "manual", 20, 3, []programTrack{{ID: "manual", AudioURL: server.URL, DurationMS: 60000}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RefreshProgram(context.Background(), input); !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("manual switch overwritten: %v", err)
	}
}

func TestProgramRefreshRechecksBaselineAfterAssetPreparation(t *testing.T) {
	client, _, start, server := startRefreshFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	blocker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(testWAV(1000))
	}))
	defer blocker.Close()
	input := refreshFixtureInput(start, blocker.URL)
	done := make(chan error, 1)
	go func() { _, err := client.RefreshProgram(context.Background(), input); done <- err }()
	<-entered
	_, _ = client.StopProgram(context.Background(), 21)
	manual, err := client.startPreparedProgram(21, "manual-session", "manual", 20, 2, []programTrack{{ID: "manual", AudioURL: server.URL, DurationMS: 60000}})
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("stale prepared job accepted: %v", err)
	}
	current, _ := client.ProgramSnapshot(context.Background(), 21)
	if current.ProgramID != manual.ProgramID || current.PendingJobID != "" || current.VersionID != 20 {
		t.Fatalf("manual session corrupted: %+v", current)
	}
}

func TestProgramRefreshFailedAssetKeepsCurrentProgram(t *testing.T) {
	client, _, start, _ := startRefreshFixture(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer bad.Close()
	if _, err := client.RefreshProgram(context.Background(), refreshFixtureInput(start, bad.URL)); err == nil {
		t.Fatal("missing audio accepted")
	}
	current, _ := client.ProgramSnapshot(context.Background(), 21)
	if current.Generation != 0 || current.PendingJobID != "" || current.Task.ID != start.Task.ID {
		t.Fatal("failed prepare mutated live program")
	}
}

func TestProgramRefreshCancelKeepsAudioAndTombstonesReplay(t *testing.T) {
	client, program, start, server := startRefreshFixture(t)
	input := refreshFixtureInput(start, server.URL)
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	cancel := audioout.CancelProgramRefreshInput{RoomID: 21, JobID: input.JobID, ExpectedProgramID: start.ProgramID}
	for i := 0; i < 2; i++ {
		result, err := client.CancelProgramRefresh(context.Background(), cancel)
		if err != nil || result.PendingJobID != "" || result.Task.ID != start.Task.ID || result.Generation != 0 {
			t.Fatalf("cancel changed playback: %+v %v", result, err)
		}
	}
	if _, err := client.RefreshProgram(context.Background(), input); !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("cancelled job replayed: %v", err)
	}
	client.advanceMainline(program, start.Task.ID)
	result, _ := client.ProgramSnapshot(context.Background(), 21)
	if result.Generation != 0 || !strings.Contains(result.Task.AudioURL, "/old-") {
		t.Fatal("cancelled content entered playlist")
	}
}

func TestProgramRefreshCancellationWinsInFlightProbe(t *testing.T) {
	client, _, start, _ := startRefreshFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	blocker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		_, _ = w.Write(testWAV(1000))
	}))
	defer blocker.Close()
	input := refreshFixtureInput(start, blocker.URL)
	done := make(chan error, 1)
	go func() { _, err := client.RefreshProgram(context.Background(), input); done <- err }()
	<-entered
	_, err := client.CancelProgramRefresh(context.Background(), audioout.CancelProgramRefreshInput{RoomID: 21, JobID: input.JobID, ExpectedProgramID: start.ProgramID})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("probe reinstalled cancelled job: %v", err)
	}
}

func TestProgramRefreshLeaseExpiryKeepsOldPlaylistAndCannotRenewExpiredJob(t *testing.T) {
	client, program, start, server := startRefreshFixture(t)
	input := refreshFixtureInput(start, server.URL)
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	program.PendingRefresh.ValidUntil = time.Now().Add(-time.Second)
	client.mu.Unlock()
	client.advanceMainline(program, start.Task.ID)
	current, _ := client.ProgramSnapshot(context.Background(), 21)
	if current.Generation != 0 || current.PendingJobID != "" || !strings.Contains(current.Task.AudioURL, "/old-") {
		t.Fatalf("expired content applied: %+v", current)
	}
	_, err := client.RenewProgramRefresh(context.Background(), audioout.RenewProgramRefreshInput{RoomID: 21, JobID: input.JobID, ExpectedProgramID: start.ProgramID, ValidUntil: time.Now().Add(30 * time.Second)})
	if !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("expired tombstone renewed: %v", err)
	}
}

func TestProgramRefreshLeaseRenewalDoesNotReprepareContent(t *testing.T) {
	client, program, start, server := startRefreshFixture(t)
	input := refreshFixtureInput(start, server.URL)
	if _, err := client.RefreshProgram(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	program.PendingRefresh.ValidUntil = time.Now().Add(-time.Second)
	client.mu.Unlock()
	server.Close() // Renewal must not download/sign/generate the prepared assets.
	lease := time.Now().Add(30 * time.Second)
	_, err := client.RenewProgramRefresh(context.Background(), audioout.RenewProgramRefreshInput{RoomID: 21, JobID: input.JobID, ExpectedProgramID: start.ProgramID, ValidUntil: lease})
	if err != nil {
		t.Fatal(err)
	}
	client.mu.RLock()
	got := program.PendingRefresh.ValidUntil
	client.mu.RUnlock()
	if !got.Equal(lease) {
		t.Fatal("lease not renewed")
	}
	client.advanceMainline(program, start.Task.ID)
	current, _ := client.ProgramSnapshot(context.Background(), 21)
	if current.Generation != 1 || current.LastAppliedJobID != input.JobID {
		t.Fatal("renewed job not applied")
	}
}
