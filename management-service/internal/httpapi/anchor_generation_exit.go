package httpapi

import (
	"context"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"log"
	"strings"
	"time"
)

func producerOwnsRunningSession(current model.LiveRuntimeSession, runtimeID int64, realm string) bool {
	return current.ID == runtimeID && current.BelongsToExecutionRealm(realm) && current.Status == "running" && current.EndedAt == nil
}

func explicitProducerStop(state coreAgentRuntimeState) string {
	switch strings.ToLower(strings.TrimSpace(state.State)) {
	case "stopped", "stopping":
		return strings.ToLower(strings.TrimSpace(state.StopReason))
	default:
		return ""
	}
}

// Normal lifecycle termination is not a voice-generation failure. This path
// first verifies ownership, then preserves Core's authoritative stop reason.
func (s *Server) handleLiveAnchorProducerExit(tenant, room, runtimeID int64, version model.LiveAgentPlanVersion, cause error) {
	cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	current, err := s.store.GetLiveRuntimeByRoom(cleanup, tenant, room)
	if err != nil || !producerOwnsRunningSession(current, runtimeID, s.executionRealm) {
		log.Printf("formal producer exit ignored tenant=%d room=%d session=%d owner changed=%t", tenant, room, runtimeID, err == nil)
		return
	}
	coreState, err := s.getCoreAgentState(cleanup, tenant, room)
	if err != nil {
		// Unknown Core state is handled by the existing owner/reconciliation watch.
		// Never label an unverified lifecycle/transport loss as a TTS failure.
		log.Printf("formal producer exit Core state unconfirmed tenant=%d room=%d session=%d: %v", tenant, room, runtimeID, err)
		return
	}
	reason := explicitProducerStop(coreState)
	if reason == "" {
		// Stop and generation check are atomic in Core. A newer run returns 409;
		// a normal stop racing this request returns its original business reason.
		coreState, err = s.putCoreAgentState(cleanup, tenant, room, map[string]any{
			"command": "stop", "stop_reason": "system_error",
			"expected_boot_id": coreState.BootID, "expected_run_generation": coreState.RunGeneration,
		})
		if err != nil {
			log.Printf("formal producer conditional stop unconfirmed tenant=%d room=%d session=%d: %v", tenant, room, runtimeID, err)
			return
		}
		reason = explicitProducerStop(coreState)
	}
	if reason == "" || reason == "manual_pause" {
		return
	}
	databaseReason := reason
	if reason == "system_error" {
		databaseReason = "formal_mainline_failed"
	}
	stopped, err := s.store.StopLiveRuntimeSessionMeterSystem(cleanup, runtimeID, databaseReason, coreState.WorkingSeconds, time.Now().UTC())
	if err != nil {
		log.Printf("settle formal producer exit tenant=%d room=%d session=%d: %v", tenant, room, runtimeID, err)
		return
	}
	if stopped.StopReason != "formal_mainline_failed" {
		log.Printf("formal producer lifecycle exit tenant=%d room=%d session=%d reason=%s", tenant, room, runtimeID, stopped.StopReason)
		return
	}
	// Only a confirmed failure in this exact session writes failure UI/assets.
	log.Printf("formal mainline tenant=%d room=%d version=%d failed: %v", tenant, room, version.ID, cause)
	if state, e := s.anchorCycles.Get(cleanup, tenant, version.PlanID, room); e == nil && state.Delivery != nil && state.Delivery.RuntimeSessionID == runtimeID {
		state.Status, state.Error = "failed", cause.Error()
		_ = s.anchorCycles.Save(cleanup, state)
	}
	if repo, ok := s.anchorCycles.(anchorcycle.LiveFailureRepository); ok {
		if e := repo.SaveLiveFailure(cleanup, tenant, room, publicLiveFailure(runtimeID, cause)); e != nil {
			log.Printf("save formal failure tenant=%d room=%d session=%d: %v", tenant, room, runtimeID, e)
		}
	}
	if err := s.store.PruneSavedPlanVersionsForRoom(cleanup, tenant, room); err != nil {
		log.Printf("prune saved versions after failed runtime tenant=%d room=%d: %v", tenant, room, err)
	}
}
