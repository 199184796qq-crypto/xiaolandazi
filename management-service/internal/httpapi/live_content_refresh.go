package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const contentRefreshLease = 2 * time.Minute

type contentRefreshStore interface {
	ValidateLiveContentRefreshJob(context.Context, model.LiveContentRefreshJob, time.Time) error
	RenewLiveContentRefreshLease(context.Context, model.LiveContentRefreshJob, time.Time, time.Duration) error
	GetLiveAgentPlanVersion(context.Context, int64, int64, int64) (model.LiveAgentPlanVersion, error)
	CommitLiveContentRefresh(context.Context, model.LiveContentRefreshJob, model.CreateLiveAgentPlanVersionInput, time.Time) (model.LiveAgentPlanVersion, error)
	CompleteLiveContentRefreshJob(context.Context, model.LiveContentRefreshJob, time.Time) error
	DeferLiveContentRefreshJob(context.Context, model.LiveContentRefreshJob, time.Time, time.Duration) error
	CancelLiveContentRefreshJob(context.Context, model.LiveContentRefreshJob, string, time.Time) error
	FailLiveContentRefreshJob(context.Context, model.LiveContentRefreshJob, string, time.Time) error
}

type contentRefreshCore interface {
	GetRoomProgramSnapshot(context.Context, int64, int64) (coreclient.ProgramRefreshSnapshot, error)
	RefreshRoomProgram(context.Context, int64, int64, coreclient.ProgramRefreshInput) (coreclient.ProgramRefreshSnapshot, error)
	RenewRoomProgramRefresh(context.Context, int64, int64, string, string, time.Time) (coreclient.ProgramRefreshSnapshot, error)
}

type contentRefreshRunner struct {
	store    contentRefreshStore
	core     contentRefreshCore
	generate func(context.Context, model.LiveContentRefreshJob, model.LiveAgentPlanVersion, coreclient.ProgramRefreshSnapshot) (model.CreateLiveAgentPlanVersionInput, error)
	tracks   func(context.Context, model.LiveAgentPlanVersion) ([]coreclient.ProgramRefreshTrack, error)
	now      func() time.Time
}

type contentRefreshBaseline struct {
	ProgramID  string `json:"program_id"`
	Generation uint64 `json:"generation"`
}

func refreshBaseline(data map[string]any) (contentRefreshBaseline, error) {
	var result contentRefreshBaseline
	raw, err := json.Marshal(data["auto_refresh_core"])
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	if err != nil || result.ProgramID == "" {
		return result, errors.New("missing Core refresh baseline")
	}
	return result, nil
}

func refreshJobKey(job model.LiveContentRefreshJob) string {
	return fmt.Sprintf("content-refresh-%d", job.ID)
}

// One durable job has two phases: prepare all assets, then apply at a Core
// track boundary after the TTL. Neither phase starts/restarts a live session.
func (w *contentRefreshRunner) process(ctx context.Context, job model.LiveContentRefreshJob) error {
	if err := w.store.ValidateLiveContentRefreshJob(ctx, job, w.now()); err != nil {
		return err
	}
	snapshot, err := w.core.GetRoomProgramSnapshot(ctx, job.TenantID, job.RoomID)
	if err != nil {
		return err
	}
	if !snapshot.Running {
		return w.cancel(ctx, job, "Core program stopped")
	}
	if snapshot.Suspended && job.ResultVersionID == 0 {
		return w.store.DeferLiveContentRefreshJob(ctx, job, w.now(), 15*time.Second)
	}
	if job.ResultVersionID == 0 {
		if snapshot.VersionID != job.BaseVersionID || snapshot.PendingJobID != "" {
			return w.cancel(ctx, job, "Core baseline changed")
		}
		base, err := w.store.GetLiveAgentPlanVersion(ctx, job.TenantID, job.PlanID, job.BaseVersionID)
		if err != nil {
			return err
		}
		input, err := w.generate(ctx, job, base, snapshot)
		if err != nil {
			return err
		}
		if err := w.store.ValidateLiveContentRefreshJob(ctx, job, w.now()); err != nil {
			return err
		}
		latest, err := w.core.GetRoomProgramSnapshot(ctx, job.TenantID, job.RoomID)
		if err != nil {
			return err
		}
		if !latest.Running || latest.Suspended || latest.PendingJobID != "" || latest.ProgramID != snapshot.ProgramID || latest.VersionID != snapshot.VersionID || latest.Generation != snapshot.Generation {
			return w.cancel(ctx, job, "Core changed while preparing")
		}
		_, err = w.store.CommitLiveContentRefresh(ctx, job, input, w.now())
		return err
	}
	if w.now().Before(job.PublishAfter) {
		return w.store.DeferLiveContentRefreshJob(ctx, job, w.now(), 5*time.Second)
	}
	if snapshot.VersionID == job.ResultVersionID {
		// Lost acknowledgement: the Core boundary application already succeeded.
		return w.store.CompleteLiveContentRefreshJob(ctx, job, w.now())
	}
	if snapshot.PendingJobID == refreshJobKey(job) {
		if err := w.store.ValidateLiveContentRefreshJob(ctx, job, w.now()); err != nil {
			return err
		}
		_, err := w.core.RenewRoomProgramRefresh(ctx, job.TenantID, job.RoomID, refreshJobKey(job), snapshot.ProgramID, w.now().Add(30*time.Second))
		if errors.Is(err, coreclient.ErrProgramRefreshConflict) {
			return w.cancel(ctx, job, "Core pending lease expired or changed")
		}
		if err != nil {
			return err
		}
		return w.store.DeferLiveContentRefreshJob(ctx, job, w.now(), 5*time.Second)
	}
	if snapshot.Suspended {
		return w.store.DeferLiveContentRefreshJob(ctx, job, w.now(), 15*time.Second)
	}
	version, err := w.store.GetLiveAgentPlanVersion(ctx, job.TenantID, job.PlanID, job.ResultVersionID)
	if err != nil {
		return err
	}
	baseline, err := refreshBaseline(version.GenerationContext)
	if err != nil {
		return err
	}
	if snapshot.ProgramID != baseline.ProgramID || snapshot.VersionID != job.BaseVersionID || snapshot.Generation != baseline.Generation || snapshot.PendingJobID != "" {
		return w.cancel(ctx, job, "Core baseline changed before application")
	}
	tracks, err := w.tracks(ctx, version)
	if err != nil {
		return err
	}
	if err := w.store.ValidateLiveContentRefreshJob(ctx, job, w.now()); err != nil {
		return err
	}
	result, err := w.core.RefreshRoomProgram(ctx, job.TenantID, job.RoomID, coreclient.ProgramRefreshInput{
		JobID: refreshJobKey(job), ExpectedProgramID: baseline.ProgramID, ExpectedVersionID: job.BaseVersionID,
		ValidUntil:         w.now().Add(30 * time.Second),
		ExpectedGeneration: baseline.Generation, Generation: baseline.Generation + 1,
		NewVersionID: version.ID, NewVersionNo: version.VersionNo, Tracks: tracks,
	})
	if errors.Is(err, coreclient.ErrProgramRefreshConflict) {
		return w.cancel(ctx, job, "Core rejected stale refresh")
	}
	if err != nil {
		return err
	}
	if result.VersionID == job.ResultVersionID {
		return w.store.CompleteLiveContentRefreshJob(ctx, job, w.now())
	}
	return w.store.DeferLiveContentRefreshJob(ctx, job, w.now(), 5*time.Second)
}

func (w *contentRefreshRunner) cancel(ctx context.Context, job model.LiveContentRefreshJob, reason string) error {
	if err := w.store.CancelLiveContentRefreshJob(ctx, job, reason, w.now()); err != nil {
		return err
	}
	return db.ErrLiveContentRefreshObsolete
}

func (s *Server) RunLiveContentRefresh(ctx context.Context) {
	if s == nil || s.store == nil || s.core == nil {
		return
	}
	owner := "management-content-refresh"
	if s.leader != nil {
		owner = s.leader.Owner()
	}
	runner := &contentRefreshRunner{store: s.store, core: s.core, generate: s.generateContentRefresh, tracks: s.contentRefreshProgramTracks, now: time.Now}
	// Applying/renewing and cancellation have their own bounded lanes: a long
	// Qwen/TTS generation must never starve a pending Core authorization lease.
	for n := 0; n < 4; n++ {
		go s.runReadyContentRefresh(ctx, runner, fmt.Sprintf("%s-apply-%d", owner, n))
	}
	go s.runCancelledContentRefresh(ctx, owner+"-cancel")
	// Two bounded workers, shared durable leases, no goroutine per room or
	// per tick. A slow voice job must not block discovery/cancellation.
	for n := 0; n < 2; n++ {
		go func(worker int) {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				if s.leader != nil && !s.leader.IsLeader() {
					continue
				}
				claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				job, found, err := s.store.ClaimLiveContentRefreshJob(claimCtx, fmt.Sprintf("%s-%d", owner, worker), s.executionRealm, time.Now(), contentRefreshLease)
				cancel()
				if err != nil {
					log.Printf("content refresh claim: %v", err)
					continue
				}
				if !found {
					continue
				}
				jobCtx, stopJob := context.WithTimeout(ctx, 20*time.Minute)
				heartbeatDone := make(chan struct{})
				go func() {
					defer close(heartbeatDone)
					tick := time.NewTicker(20 * time.Second)
					defer tick.Stop()
					for {
						select {
						case <-jobCtx.Done():
							return
						case <-tick.C:
						}
						if s.leader != nil && !s.leader.IsLeader() {
							stopJob()
							return
						}
						renewCtx, cancel := context.WithTimeout(jobCtx, 8*time.Second)
						err := s.store.RenewLiveContentRefreshLease(renewCtx, job, time.Now(), contentRefreshLease)
						cancel()
						if err != nil {
							stopJob()
							return
						}
					}
				}()
				err = runner.process(jobCtx, job)
				stopJob()
				<-heartbeatDone
				if err != nil && !errors.Is(err, db.ErrLiveContentRefreshObsolete) && !errors.Is(err, db.ErrLiveContentRefreshLeaseLost) {
					// Keep old audio playing. Record a bounded retry, never fall
					// back to whole-track regeneration or a different voice.
					failureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_ = s.store.FailLiveContentRefreshJob(failureCtx, job, err.Error(), time.Now())
					cancel()
					log.Printf("content refresh failed job=%d room=%d: %v", job.ID, job.RoomID, err)
				}
			}
		}(n)
	}
	var candidateCursor int64
	run := func() {
		if s.leader != nil && !s.leader.IsLeader() {
			return
		}
		queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := s.store.CancelInactiveLiveContentRefreshJobs(queryCtx, s.executionRealm, time.Now()); err != nil {
			log.Printf("content refresh cancellation: %v", err)
			return
		}
		candidates, err := s.store.ListLiveContentRefreshCandidatesAfter(queryCtx, s.executionRealm, candidateCursor, 200)
		if err != nil {
			log.Printf("content refresh candidates: %v", err)
			return
		}
		if len(candidates) == 0 {
			candidateCursor = 0
		}
		for _, candidate := range candidates {
			candidateCursor = candidate.RuntimeSessionID
			if _, _, err := s.store.EnsureLiveContentRefreshJob(queryCtx, candidate, time.Now()); err != nil && !errors.Is(err, db.ErrLiveContentRefreshObsolete) {
				log.Printf("content refresh schedule room=%d: %v", candidate.RoomID, err)
			}
		}
	}
	log.Printf("content refresh scheduler started realm=%s", s.executionRealm)
	run()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (s *Server) runReadyContentRefresh(ctx context.Context, runner *contentRefreshRunner, owner string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.leader != nil && !s.leader.IsLeader() {
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		job, found, err := s.store.ClaimReadyLiveContentRefreshJob(jobCtx, owner, s.executionRealm, time.Now(), contentRefreshLease)
		if err == nil && found {
			err = runner.process(jobCtx, job)
		}
		cancel()
		if err != nil && !errors.Is(err, db.ErrLiveContentRefreshObsolete) && !errors.Is(err, db.ErrLiveContentRefreshLeaseLost) {
			failureCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			if found {
				_ = s.store.FailLiveContentRefreshJob(failureCtx, job, err.Error(), time.Now())
			}
			stop()
			log.Printf("content refresh apply job=%d: %v", job.ID, err)
		}
	}
}

func (s *Server) runCancelledContentRefresh(ctx context.Context, owner string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.leader != nil && !s.leader.IsLeader() {
			continue
		}
		for i := 0; i < 20 && ctx.Err() == nil; i++ {
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			job, found, err := s.store.ClaimCancelledLiveContentRefreshJob(jobCtx, owner, s.executionRealm, time.Now(), contentRefreshLease)
			if err == nil && found {
				err = s.cancelCoreContentRefresh(jobCtx, job)
				if err == nil {
					err = s.store.AcknowledgeLiveContentRefreshCancellation(jobCtx, job, time.Now())
				}
			}
			cancel()
			if err != nil {
				failureCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				if found {
					_ = s.store.DeferCancelledLiveContentRefreshJob(failureCtx, job, time.Now(), 15*time.Second)
				}
				stop()
				log.Printf("content refresh cancel job=%d: %v", job.ID, err)
			}
			if !found {
				break
			}
		}
	}
}

func (s *Server) cancelCoreContentRefresh(ctx context.Context, job model.LiveContentRefreshJob) error {
	// Before a prepared result exists there cannot be a Core queue request.
	if job.ResultVersionID == 0 {
		return nil
	}
	version, err := s.store.GetLiveAgentPlanVersion(ctx, job.TenantID, job.PlanID, job.ResultVersionID)
	if err != nil {
		return err
	}
	baseline, err := refreshBaseline(version.GenerationContext)
	if err != nil {
		return err
	}
	_, err = s.core.CancelRoomProgramRefresh(ctx, job.TenantID, job.RoomID, refreshJobKey(job), baseline.ProgramID)
	// A stopped/deleted/replaced program no longer owns this pending update.
	// Keep the generated assets/history; never delete a sound that a Core
	// boundary could already have accepted immediately before revocation.
	if errors.Is(err, coreclient.ErrProgramRefreshNotFound) {
		return nil
	}
	if errors.Is(err, coreclient.ErrProgramRefreshConflict) {
		snapshot, readErr := s.core.GetRoomProgramSnapshot(ctx, job.TenantID, job.RoomID)
		if errors.Is(readErr, coreclient.ErrProgramRefreshNotFound) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if snapshot.PendingJobID != refreshJobKey(job) {
			return nil
		}
	}
	return err
}

func (s *Server) generateContentRefresh(ctx context.Context, job model.LiveContentRefreshJob, base model.LiveAgentPlanVersion, snapshot coreclient.ProgramRefreshSnapshot) (model.CreateLiveAgentPlanVersionInput, error) {
	input := model.CreateLiveAgentPlanVersionInput{
		TenantID: job.TenantID, RoomID: job.RoomID, DurationMinutes: base.DurationMinutes, RoundMinutes: base.RoundMinutes,
		VoiceIdentity: base.VoiceIdentity, Variants: append([]model.LiveAgentPlanVersionVariant(nil), base.Variants...),
	}
	generation, policyPrompt, err := s.contentRefreshGenerationContext(ctx, base)
	if err != nil {
		return input, err
	}
	if job.Draft != nil {
		input = *job.Draft
		baseline, err := refreshBaseline(input.GenerationContext)
		if err != nil {
			return input, err
		}
		if baseline.ProgramID != snapshot.ProgramID || baseline.Generation != snapshot.Generation {
			if err := s.store.CancelLiveContentRefreshJob(ctx, job, "saved Core baseline changed", time.Now()); err != nil {
				return input, err
			}
			return input, db.ErrLiveContentRefreshObsolete
		}
	} else {
		raw, err := json.Marshal(generation)
		if err != nil {
			return input, err
		}
		if err := json.Unmarshal(raw, &input.GenerationContext); err != nil {
			return input, err
		}
		input.GenerationContext["auto_refresh_core"] = contentRefreshBaseline{ProgramID: snapshot.ProgramID, Generation: snapshot.Generation}
		if err := s.store.SaveLiveContentRefreshDraft(ctx, job, input, time.Now()); err != nil {
			return input, err
		}
	}
	actorID := int64(0) // System-generated assets, not a fabricated customer action.
	stillAllowed := func() error { return s.store.ValidateLiveContentRefreshJob(ctx, job, time.Now()) }
	for index, original := range base.Variants {
		if !original.IsFormal {
			continue
		}
		if index >= len(input.Variants) || input.Variants[index].VariantKey != original.VariantKey {
			return input, errors.New("invalid saved refresh draft")
		}
		if input.Variants[index].AudioAssetID != original.AudioAssetID {
			continue
		} // durable completed checkpoint
		variant, err := s.refreshContentVariant(ctx, job.TenantID, actorID, job.PlanID, job.RoomID, generation, policyPrompt, base.VoiceIdentity, original, job.Policy.ReplacementPercent, stillAllowed)
		if err != nil {
			return input, err
		}
		if err := s.store.TrackLiveContentRefreshAsset(ctx, job, variant.AudioAssetID, time.Now()); err != nil {
			return input, err
		}
		input.Variants[index] = variant
		if err := s.store.SaveLiveContentRefreshDraft(ctx, job, input, time.Now()); err != nil {
			return input, err
		}
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		return input, err
	}
	if err := s.validateLiveAgentVersionAssets(ctx, job.TenantID, input.Variants); err != nil {
		return input, err
	}
	return input, nil
}
