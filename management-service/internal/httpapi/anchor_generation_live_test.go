package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"testing"
	"time"
)

type deliveryCoreFixture struct {
	snapshot      coreclient.ProgramRefreshSnapshot
	sends, renews int
	persisted     *bool
	failAck       bool
}

func (f *deliveryCoreFixture) GetRoomProgramSnapshot(context.Context, int64, int64) (coreclient.ProgramRefreshSnapshot, error) {
	return f.snapshot, nil
}
func (f *deliveryCoreFixture) RefreshRoomProgram(_ context.Context, _, _ int64, input coreclient.ProgramRefreshInput) (coreclient.ProgramRefreshSnapshot, error) {
	if f.persisted != nil && !*f.persisted {
		return f.snapshot, errors.New("intent was not persisted")
	}
	f.sends++
	if input.NewVersionID != f.snapshot.VersionID || input.NewVersionNo != f.snapshot.VersionNo {
		return f.snapshot, errors.New("scheme version restarted")
	}
	f.snapshot.Generation = input.Generation
	f.snapshot.LastAppliedJobID = input.JobID
	if f.failAck {
		return coreclient.ProgramRefreshSnapshot{}, errors.New("lost acknowledgement")
	}
	return f.snapshot, nil
}
func (f *deliveryCoreFixture) RenewRoomProgramRefresh(context.Context, int64, int64, string, string, time.Time) (coreclient.ProgramRefreshSnapshot, error) {
	f.renews++
	return f.snapshot, nil
}

func deliveryFixture(t *testing.T) (*anchorcycle.State, *deliveryCoreFixture, liveAnchorDeliveryRunner) {
	t.Helper()
	state := cycleTestState(t)
	state.Surface = "formal_live"
	state.TenantID = 1
	state.PlanID = 2
	state.RoomID = 3
	state.Delivery = &anchorcycle.MainlineDelivery{VersionID: 7, ProgramID: "program"}
	core := &deliveryCoreFixture{snapshot: coreclient.ProgramRefreshSnapshot{Running: true, Sequential: true, ProgramID: "program", VersionID: 7, VersionNo: 4}}
	runner := liveAnchorDeliveryRunner{core: core, save: func(context.Context, *anchorcycle.State) error { return nil }, owner: func(context.Context) (bool, error) { return false, nil }, now: time.Now, wait: func(ctx context.Context) error { return ctx.Err() }}
	return &state, core, runner
}

func TestLiveHandoffLostAcknowledgementReconcilesWithoutResend(t *testing.T) {
	state, core, runner := deliveryFixture(t)
	persisted := false
	core.persisted = &persisted
	core.failAck = true
	runner.save = func(_ context.Context, s *anchorcycle.State) error {
		if s.Delivery.PendingJobID != "" {
			persisted = true
		}
		return nil
	}
	tracks := []coreclient.ProgramRefreshTrack{{ID: "one", DurationMS: 1000}, {ID: "two", DurationMS: 2000}}
	if err := runner.send(context.Background(), state, tracks); err != nil {
		t.Fatal(err)
	}
	if core.sends != 1 || state.Delivery.Generation != 1 || state.Delivery.PendingJobID != "" || len(state.Delivery.DeliveredCandidates) != 2 {
		t.Fatal(core, state.Delivery)
	}
}
func TestLiveHandoffPersistenceFailureNeverMutatesCore(t *testing.T) {
	state, core, runner := deliveryFixture(t)
	runner.save = func(context.Context, *anchorcycle.State) error { return errors.New("redis unavailable") }
	if err := runner.send(context.Background(), state, []coreclient.ProgramRefreshTrack{{ID: "one"}}); err == nil || core.sends != 0 {
		t.Fatal(err, core.sends)
	}
}
func TestLiveHandoffStopAndChangedProgramCannotBeOverwritten(t *testing.T) {
	for _, changed := range []bool{false, true} {
		state, core, runner := deliveryFixture(t)
		if changed {
			core.snapshot.ProgramID = "new-room-program"
		} else {
			runner.owner = func(context.Context) (bool, error) { return false, anchorcycle.ErrStale }
		}
		if err := runner.send(context.Background(), state, []coreclient.ProgramRefreshTrack{{ID: "one"}}); !errors.Is(err, anchorcycle.ErrStale) || core.sends != 0 {
			t.Fatal(err, core.sends)
		}
	}
}
func TestLiveHandoffPauseDoesNotPublishAndCancellationEndsWait(t *testing.T) {
	state, core, runner := deliveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	runner.owner = func(context.Context) (bool, error) { return true, nil }
	runner.wait = func(context.Context) error { cancel(); return ctx.Err() }
	if err := runner.send(ctx, state, []coreclient.ProgramRefreshTrack{{ID: "one"}}); !errors.Is(err, context.Canceled) || core.sends != 0 {
		t.Fatal(err, core.sends)
	}
}
func TestLiveHandoffRejectsCustomerPreview(t *testing.T) {
	state, core, runner := deliveryFixture(t)
	state.Surface = "generation_preview"
	if err := runner.send(context.Background(), state, []coreclient.ProgramRefreshTrack{{ID: "one"}}); err == nil || core.sends != 0 {
		t.Fatal(err)
	}
}
func TestLiveBufferUsesRemainingActualAudioNotTextCount(t *testing.T) {
	snapshot := coreclient.ProgramRefreshSnapshot{Running: true, TrackIndex: 1, CurrentMS: 1500}
	if got := bufferedMainlineMS(snapshot, []int{4000, 5000, 6000}); got != 9500 {
		t.Fatal(got)
	}
	snapshot.WaitingForContent = true
	if got := bufferedMainlineMS(snapshot, []int{4000, 5000, 6000}); got != 0 {
		t.Fatal(got)
	}
}

func TestLiveAnchorBatchTargetStartsWithOneTrackThenBuffersTwo(t *testing.T) {
	state := &anchorcycle.State{Delivery: &anchorcycle.MainlineDelivery{}}
	if got := liveAnchorBatchTarget(state); got != 1 {
		t.Fatalf("cold-start target = %d, want 1", got)
	}
	state.Delivery.ProgramID = "program"
	if got := liveAnchorBatchTarget(state); got != 2 {
		t.Fatalf("running target = %d, want 2", got)
	}
	if got := liveAnchorBatchTarget(nil); got != 2 {
		t.Fatalf("invalid-state fallback target = %d, want 2", got)
	}
}

func TestFormalJobsRespectParentStopAndPreviewJobsSurviveBrowserDisconnect(t *testing.T) {
	server := &Server{}
	parent, cancel := context.WithCancel(context.Background())
	formal, done := server.cycleJobContext(parent, "formal", 1, 3, "run", false)
	defer done()
	preview, donePreview := server.cycleJobContext(parent, "preview", 1, 0, "test", true)
	defer donePreview()
	cancel()
	if formal.Err() == nil || preview.Err() != nil {
		t.Fatal("lifecycle cancellation leaked between surfaces")
	}
}
func TestTestHTTPAccessCannotOperateFormalProducer(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/workbench", nil)
	state := cycleTestState(t)
	state.Surface = "formal_live"
	if (&Server{}).requireCycleRunAccess(recorder, req, model.Actor{}, &state) || recorder.Code != 403 {
		t.Fatal("formal run exposed to test APIs")
	}
}

func TestRealtimeMetadataVersionRequiresServerSnapshotButNoPreviewAudio(t *testing.T) {
	input := model.CreateLiveAgentPlanVersionInput{RoomID: 3, DurationMinutes: 90, RoundMinutes: 7, VoiceIdentity: model.LiveAgentVoiceIdentity{Name: "saved", Source: "official", Provider: "qwen", Model: "qwen-tts", VoiceID: "selected", Rate: 1}, GenerationContext: map[string]any{"content_mode": model.LiveContentAIDynamic}}
	if validateLiveAgentPlanVersionInput(&input) == nil {
		t.Fatal("empty dynamic version bypassed initialization")
	}
	input.GenerationContext["anchor_generation"] = liveAnchorScheme{Version: 1, StyleID: 1, ScorecardID: 2, WheelID: 3}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		t.Fatal(err)
	}
	input.GenerationContext["content_mode"] = model.LiveContentAIPregenerated
	if validateLiveAgentPlanVersionInput(&input) == nil {
		t.Fatal("static mode accepted zero formal voices")
	}
}
