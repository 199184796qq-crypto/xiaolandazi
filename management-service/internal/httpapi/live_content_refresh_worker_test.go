package httpapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// These fakes drive the actual two-phase runner, not a duplicate state machine.
// Embedded interfaces make any accidentally introduced unmocked side effect fail.
type refreshRunnerStore struct {
	contentRefreshStore
	validateErrors []error
	validations    int
	versions       map[int64]model.LiveAgentPlanVersion
	versionReads   []int64
	commits        []model.CreateLiveAgentPlanVersionInput
	completed      []int64
	deferred       []time.Duration
	cancelled      []string
	events         *[]string
}

func (s *refreshRunnerStore) ValidateLiveContentRefreshJob(_ context.Context, _ model.LiveContentRefreshJob, _ time.Time) error {
	i := s.validations
	s.validations++
	*s.events = append(*s.events, "validate")
	if i < len(s.validateErrors) {
		return s.validateErrors[i]
	}
	return nil
}

func (s *refreshRunnerStore) GetLiveAgentPlanVersion(_ context.Context, _, _ int64, id int64) (model.LiveAgentPlanVersion, error) {
	s.versionReads = append(s.versionReads, id)
	v, ok := s.versions[id]
	if !ok {
		return v, fmt.Errorf("unexpected version read: %d", id)
	}
	return v, nil
}

func (s *refreshRunnerStore) CommitLiveContentRefresh(_ context.Context, _ model.LiveContentRefreshJob, input model.CreateLiveAgentPlanVersionInput, _ time.Time) (model.LiveAgentPlanVersion, error) {
	s.commits = append(s.commits, input)
	*s.events = append(*s.events, "commit")
	return s.versions[22], nil
}

func (s *refreshRunnerStore) CompleteLiveContentRefreshJob(_ context.Context, job model.LiveContentRefreshJob, _ time.Time) error {
	s.completed = append(s.completed, job.ID)
	*s.events = append(*s.events, "complete")
	return nil
}

func (s *refreshRunnerStore) DeferLiveContentRefreshJob(_ context.Context, _ model.LiveContentRefreshJob, _ time.Time, delay time.Duration) error {
	s.deferred = append(s.deferred, delay)
	*s.events = append(*s.events, "defer")
	return nil
}

func (s *refreshRunnerStore) CancelLiveContentRefreshJob(_ context.Context, _ model.LiveContentRefreshJob, reason string, _ time.Time) error {
	s.cancelled = append(s.cancelled, reason)
	*s.events = append(*s.events, "cancel")
	return nil
}

type refreshRunnerCore struct {
	contentRefreshCore
	snapshots     []coreclient.ProgramRefreshSnapshot
	reads         int
	refreshes     []coreclient.ProgramRefreshInput
	refreshErrors []error
	refreshResult coreclient.ProgramRefreshSnapshot
	renewals      []refreshRunnerRenewal
	renewError    error
	events        *[]string
}

type refreshRunnerRenewal struct {
	JobID, ProgramID string
	ValidUntil       time.Time
}

func (c *refreshRunnerCore) RenewRoomProgramRefresh(_ context.Context, _, _ int64, jobID, programID string, validUntil time.Time) (coreclient.ProgramRefreshSnapshot, error) {
	c.renewals = append(c.renewals, refreshRunnerRenewal{JobID: jobID, ProgramID: programID, ValidUntil: validUntil})
	*c.events = append(*c.events, "renew_pending")
	return c.refreshResult, c.renewError
}

func (c *refreshRunnerCore) GetRoomProgramSnapshot(_ context.Context, _, _ int64) (coreclient.ProgramRefreshSnapshot, error) {
	i := c.reads
	c.reads++
	*c.events = append(*c.events, "snapshot")
	if i >= len(c.snapshots) {
		i = len(c.snapshots) - 1
	}
	return c.snapshots[i], nil
}

func (c *refreshRunnerCore) RefreshRoomProgram(_ context.Context, _, _ int64, input coreclient.ProgramRefreshInput) (coreclient.ProgramRefreshSnapshot, error) {
	i := len(c.refreshes)
	c.refreshes = append(c.refreshes, input)
	*c.events = append(*c.events, "refresh")
	if i < len(c.refreshErrors) && c.refreshErrors[i] != nil {
		return coreclient.ProgramRefreshSnapshot{}, c.refreshErrors[i]
	}
	return c.refreshResult, nil
}

type refreshRunnerFixture struct {
	runner                      *contentRefreshRunner
	store                       *refreshRunnerStore
	core                        *refreshRunnerCore
	job                         model.LiveContentRefreshJob
	now                         time.Time
	events                      []string
	generationCalls, trackCalls int
	trackErrors                 []error
	onGenerate                  func()
	onTracks                    func()
}

func newRefreshRunnerFixture() *refreshRunnerFixture {
	f := &refreshRunnerFixture{now: time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)}
	f.job = model.LiveContentRefreshJob{ID: 80, TenantID: 7, RoomID: 11, PlanID: 3, BaseVersionID: 21,
		RuntimeSessionID: 4, ExecutionRealm: "prod", PublishAfter: f.now.Add(10 * time.Minute), Policy: model.DefaultLiveContentPolicy()}
	base := model.LiveAgentPlanVersion{ID: 21, TenantID: 7, RoomID: 11, PlanID: 3, VersionNo: 5}
	ready := model.LiveAgentPlanVersion{ID: 22, TenantID: 7, RoomID: 11, PlanID: 3, VersionNo: 6,
		GenerationContext: map[string]any{"auto_refresh_core": contentRefreshBaseline{ProgramID: "program-4", Generation: 8}}}
	f.store = &refreshRunnerStore{versions: map[int64]model.LiveAgentPlanVersion{21: base, 22: ready}, events: &f.events}
	snapshot := coreclient.ProgramRefreshSnapshot{ProgramID: "program-4", RoomID: 11, VersionID: 21, VersionNo: 5, Generation: 8, Running: true}
	f.core = &refreshRunnerCore{snapshots: []coreclient.ProgramRefreshSnapshot{snapshot}, events: &f.events,
		refreshResult: coreclient.ProgramRefreshSnapshot{ProgramID: "program-4", VersionID: 21, Generation: 8, Running: true, PendingJobID: refreshJobKey(f.job)}}
	f.runner = &contentRefreshRunner{store: f.store, core: f.core, now: func() time.Time { return f.now }}
	f.runner.generate = func(_ context.Context, job model.LiveContentRefreshJob, base model.LiveAgentPlanVersion, snapshot coreclient.ProgramRefreshSnapshot) (model.CreateLiveAgentPlanVersionInput, error) {
		f.generationCalls++
		f.events = append(f.events, "generate")
		if f.onGenerate != nil {
			f.onGenerate()
		}
		if base.ID != job.BaseVersionID || snapshot.VersionID != base.ID {
			return model.CreateLiveAgentPlanVersionInput{}, errors.New("wrong generation baseline")
		}
		return model.CreateLiveAgentPlanVersionInput{TenantID: job.TenantID, RoomID: job.RoomID, GenerationContext: map[string]any{"auto_refresh_core": contentRefreshBaseline{ProgramID: snapshot.ProgramID, Generation: snapshot.Generation}}}, nil
	}
	f.runner.tracks = func(_ context.Context, version model.LiveAgentPlanVersion) ([]coreclient.ProgramRefreshTrack, error) {
		index := f.trackCalls
		f.trackCalls++
		f.events = append(f.events, "sign_tracks")
		if f.onTracks != nil {
			f.onTracks()
		}
		if index < len(f.trackErrors) && f.trackErrors[index] != nil {
			return nil, f.trackErrors[index]
		}
		if version.ID != 22 {
			return nil, errors.New("wrong prepared version")
		}
		return []coreclient.ProgramRefreshTrack{{ID: "mainline-1", AudioURL: fmt.Sprintf("https://assets.test/prepared.wav?signature=%d", f.trackCalls), Text: "已归档的新主线", DurationMS: 60000}}, nil
	}
	return f
}

func (f *refreshRunnerFixture) ready() {
	f.job.ResultVersionID = 22
	f.now = f.job.PublishAfter
}

func TestContentRefreshRunnerPrepareCommitsWithoutApplying(t *testing.T) {
	f := newRefreshRunnerFixture()
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if f.generationCalls != 1 || len(f.store.commits) != 1 || f.trackCalls != 0 || len(f.core.refreshes) != 0 || len(f.store.completed) != 0 {
		t.Fatalf("prepare must only create durable ready result: %+v", f.events)
	}
	want := []string{"validate", "snapshot", "generate", "validate", "snapshot", "commit"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("phases=%v want=%v", f.events, want)
	}
}

func TestContentRefreshRunnerReadyWaitsForTTLWithoutResigning(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.job.ResultVersionID = 22
	f.now = f.job.PublishAfter.Add(-time.Nanosecond)
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if len(f.store.deferred) != 1 || f.generationCalls != 0 || f.trackCalls != 0 || len(f.core.refreshes) != 0 || len(f.store.completed) != 0 {
		t.Fatalf("ready result applied before TTL: %v", f.events)
	}
}

func TestContentRefreshRunnerReadyRetryResignsButNeverRegenerates(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	transient := errors.New("Core connection lost")
	f.core.refreshErrors = []error{transient, nil}
	if err := f.runner.process(context.Background(), f.job); !errors.Is(err, transient) {
		t.Fatalf("first attempt=%v", err)
	}
	f.now = f.now.Add(time.Minute)
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if f.generationCalls != 0 || len(f.store.commits) != 0 || f.trackCalls != 2 || len(f.core.refreshes) != 2 {
		t.Fatalf("ready retry must reuse archived paid result: %v", f.events)
	}
	first, second := f.core.refreshes[0], f.core.refreshes[1]
	if first.Tracks[0].AudioURL == second.Tracks[0].AudioURL {
		t.Fatal("ready retry reused stale URL rather than signing again")
	}
	if second.JobID != refreshJobKey(f.job) || second.ExpectedProgramID != "program-4" || second.ExpectedVersionID != 21 || second.ExpectedGeneration != 8 || second.Generation != 9 || second.NewVersionID != 22 {
		t.Fatalf("refresh lost compare-and-swap baseline: %+v", second)
	}
	if !second.ValidUntil.Equal(f.now.Add(30*time.Second)) || !first.ValidUntil.Before(second.ValidUntil) {
		t.Fatalf("pending application must receive a bounded fresh authorization deadline: first=%s second=%s", first.ValidUntil, second.ValidUntil)
	}
	if len(f.store.completed) != 0 || len(f.store.deferred) != 1 {
		t.Fatal("queued Core result prematurely completed")
	}
}

func TestContentRefreshRunnerSigningFailureRetriesOnlyArchivedAssetSigning(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	signingErr := errors.New("asset signing temporarily unavailable")
	f.trackErrors = []error{signingErr, nil}
	if err := f.runner.process(context.Background(), f.job); !errors.Is(err, signingErr) {
		t.Fatalf("signing failure=%v", err)
	}
	if len(f.core.refreshes) != 0 || f.generationCalls != 0 || len(f.store.commits) != 0 {
		t.Fatal("failed signing generated/committed/applied content")
	}
	f.now = f.now.Add(time.Minute)
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if f.trackCalls != 2 || len(f.core.refreshes) != 1 || f.generationCalls != 0 || len(f.store.commits) != 0 {
		t.Fatalf("signing retry paid for another generation: %v", f.events)
	}
}

func TestContentRefreshRunnerDoesNotOverwriteManualSwitchStopOrRevocation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*refreshRunnerFixture)
		wantErr error
	}{
		{"stopped before generation", func(f *refreshRunnerFixture) { f.core.snapshots[0].Running = false }, db.ErrLiveContentRefreshObsolete},
		{"manual switch before generation", func(f *refreshRunnerFixture) { f.core.snapshots[0].VersionID = 99 }, db.ErrLiveContentRefreshObsolete},
		{"revoked before generation", func(f *refreshRunnerFixture) { f.store.validateErrors = []error{db.ErrLiveContentRefreshObsolete} }, db.ErrLiveContentRefreshObsolete},
		{"stopped before apply", func(f *refreshRunnerFixture) { f.ready(); f.core.snapshots[0].Running = false }, db.ErrLiveContentRefreshObsolete},
		{"manual switch before apply", func(f *refreshRunnerFixture) { f.ready(); f.core.snapshots[0].VersionID = 99 }, db.ErrLiveContentRefreshObsolete},
		{"restarted program before apply", func(f *refreshRunnerFixture) { f.ready(); f.core.snapshots[0].ProgramID = "new-program" }, db.ErrLiveContentRefreshObsolete},
		{"newer generation before apply", func(f *refreshRunnerFixture) { f.ready(); f.core.snapshots[0].Generation++ }, db.ErrLiveContentRefreshObsolete},
		{"another pending job", func(f *refreshRunnerFixture) { f.ready(); f.core.snapshots[0].PendingJobID = "manual-job" }, db.ErrLiveContentRefreshObsolete},
		{"revoked while signing", func(f *refreshRunnerFixture) {
			f.ready()
			f.store.validateErrors = []error{nil, db.ErrLiveContentRefreshObsolete}
		}, db.ErrLiveContentRefreshObsolete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRefreshRunnerFixture()
			tt.setup(f)
			err := f.runner.process(context.Background(), f.job)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err=%v want=%v", err, tt.wantErr)
			}
			if f.generationCalls != 0 || len(f.store.commits) != 0 || len(f.core.refreshes) != 0 || len(f.store.completed) != 0 {
				t.Fatalf("obsolete job modified program: %v", f.events)
			}
		})
	}
}

func TestContentRefreshRunnerRechecksAfterPaidGeneration(t *testing.T) {
	for _, mutation := range []string{"stop", "manual switch", "program restart", "new generation", "revocation", "suspended", "another pending job"} {
		t.Run(mutation, func(t *testing.T) {
			f := newRefreshRunnerFixture()
			latest := f.core.snapshots[0]
			switch mutation {
			case "stop":
				latest.Running = false
			case "manual switch":
				latest.VersionID = 99
			case "program restart":
				latest.ProgramID = "new-program"
			case "new generation":
				latest.Generation++
			case "revocation":
				f.store.validateErrors = []error{nil, db.ErrLiveContentRefreshObsolete}
			case "suspended":
				latest.Suspended = true
			case "another pending job":
				latest.PendingJobID = "manual-job"
			}
			f.core.snapshots = append(f.core.snapshots, latest)
			err := f.runner.process(context.Background(), f.job)
			if !errors.Is(err, db.ErrLiveContentRefreshObsolete) {
				t.Fatalf("stale generated result accepted: %v", err)
			}
			if f.generationCalls != 1 || len(f.store.commits) != 0 || len(f.core.refreshes) != 0 {
				t.Fatalf("stale result escaped recheck: %v", f.events)
			}
		})
	}
}

func TestContentRefreshRunnerAlreadyAppliedCompletesWithoutPaidWork(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	f.core.snapshots[0].VersionID = 22
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if len(f.store.completed) != 1 || f.generationCalls != 0 || f.trackCalls != 0 || len(f.core.refreshes) != 0 || len(f.store.versionReads) != 0 {
		t.Fatalf("already applied result was regenerated or resubmitted: %v", f.events)
	}
}

func TestContentRefreshRunnerImmediateCoreAcknowledgementCompletes(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	f.core.refreshResult.VersionID = 22
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if len(f.store.completed) != 1 || len(f.core.refreshes) != 1 || f.generationCalls != 0 {
		t.Fatalf("applied result not completed: %v", f.events)
	}
}

func TestContentRefreshRunnerCoreCASConflictCancelsWithoutRegeneration(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	f.core.refreshErrors = []error{coreclient.ErrProgramRefreshConflict}
	err := f.runner.process(context.Background(), f.job)
	if !errors.Is(err, db.ErrLiveContentRefreshObsolete) || len(f.store.cancelled) != 1 || len(f.store.completed) != 0 || f.generationCalls != 0 {
		t.Fatalf("CAS rejection was not terminal: err=%v events=%v", err, f.events)
	}
}

func TestContentRefreshRunnerSuspendedReadyJobDefersWithoutWork(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	f.core.snapshots[0].Suspended = true
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if len(f.store.deferred) != 1 || f.generationCalls != 0 || f.trackCalls != 0 || len(f.core.refreshes) != 0 {
		t.Fatalf("suspended room received work: %v", f.events)
	}
}

func TestContentRefreshRunnerPendingRenewsOnlyShortAuthorizationLease(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	f.core.snapshots[0].PendingJobID = refreshJobKey(f.job)
	if err := f.runner.process(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
	if len(f.core.renewals) != 1 || len(f.store.deferred) != 1 || f.store.deferred[0] != 5*time.Second || f.generationCalls != 0 || f.trackCalls != 0 || len(f.core.refreshes) != 0 {
		t.Fatalf("pending work was regenerated/requeued: %v", f.events)
	}
	renew := f.core.renewals[0]
	if renew.JobID != refreshJobKey(f.job) || renew.ProgramID != "program-4" || !renew.ValidUntil.Equal(f.now.Add(30*time.Second)) {
		t.Fatalf("invalid pending lease: %+v", renew)
	}
}

func TestContentRefreshRunnerRevokedGrantNeverRenewsPendingApplication(t *testing.T) {
	for _, revokeAt := range []string{"before poll", "after snapshot"} {
		t.Run(revokeAt, func(t *testing.T) {
			f := newRefreshRunnerFixture()
			f.ready()
			f.core.snapshots[0].PendingJobID = refreshJobKey(f.job)
			if revokeAt == "before poll" {
				f.store.validateErrors = []error{db.ErrLiveContentRefreshObsolete}
			} else {
				f.store.validateErrors = []error{nil, db.ErrLiveContentRefreshObsolete}
			}
			err := f.runner.process(context.Background(), f.job)
			if !errors.Is(err, db.ErrLiveContentRefreshObsolete) || len(f.core.renewals) != 0 || len(f.core.refreshes) != 0 || f.generationCalls != 0 {
				t.Fatalf("revoked job renewed its application window: err=%v events=%v", err, f.events)
			}
		})
	}
}

func TestContentRefreshRunnerPendingLeaseConflictCancels(t *testing.T) {
	f := newRefreshRunnerFixture()
	f.ready()
	f.core.snapshots[0].PendingJobID = refreshJobKey(f.job)
	f.core.renewError = coreclient.ErrProgramRefreshConflict
	err := f.runner.process(context.Background(), f.job)
	if !errors.Is(err, db.ErrLiveContentRefreshObsolete) || len(f.store.cancelled) != 1 || len(f.core.renewals) != 1 || len(f.core.refreshes) != 0 || f.generationCalls != 0 {
		t.Fatalf("expired pending lease not cancelled: err=%v events=%v", err, f.events)
	}
}
