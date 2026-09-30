package liveruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type Reconciler struct {
	store *appdb.Store
	core  *coreclient.Client
	audit interface {
		Record(context.Context, model.AdminAuditLog) error
	}
	leader                 interface{ IsLeader() bool }
	interval               time.Duration
	workers                int
	batchSize              int
	legacyLeaseCleanupDone bool
}

type coreRoomState struct {
	CoreBootID                 string     `json:"core_boot_id"`
	TenantID                   int64      `json:"tenant_id"`
	RoomID                     int64      `json:"room_id"`
	Status                     string     `json:"status"`
	UpdatedAt                  time.Time  `json:"updated_at"`
	AgentState                 string     `json:"agent_state"`
	AgentStopReason            string     `json:"agent_stop_reason"`
	AgentMode                  string     `json:"agent_mode"`
	AgentWorkingSeconds        uint64     `json:"agent_working_seconds"`
	AgentLeaseRemainingSeconds uint64     `json:"agent_lease_remaining_seconds"`
	AgentLeaseRenewalDue       bool       `json:"agent_lease_renewal_due"`
	AgentLeaseUntil            *time.Time `json:"agent_lease_until,omitempty"`
	AgentUpdatedAt             time.Time  `json:"agent_updated_at"`
	SessionResumePending       bool       `json:"session_resume_pending"`
}

type stateKey struct {
	TenantID int64
	RoomID   int64
}

type reconcileJob struct {
	Session model.LiveRuntimeSession
	State   coreRoomState
	StateOK bool
}

const (
	liveRuntimeStartGracePeriod = 20 * time.Second
	liveRuntimeReconcileTimeout = 8 * time.Second
	liveRuntimeTenantTimeout    = 6 * time.Second
)

type coreAgentRuntimeState struct {
	BootID                string     `json:"boot_id"`
	State                 string     `json:"state"`
	StopReason            string     `json:"stop_reason"`
	WorkingSeconds        uint64     `json:"working_seconds"`
	LeaseRemainingSeconds uint64     `json:"lease_remaining_seconds"`
	LeaseUntil            *time.Time `json:"lease_until,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func NewReconciler(
	store *appdb.Store,
	core *coreclient.Client,
	workers,
	batchSize int,
	leaders ...interface{ IsLeader() bool },
) *Reconciler {
	if workers <= 0 {
		workers = 16
	}
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 500
	}
	result := &Reconciler{
		store:     store,
		core:      core,
		interval:  10 * time.Second,
		workers:   workers,
		batchSize: batchSize,
	}
	if len(leaders) > 0 {
		result.leader = leaders[0]
	}
	return result
}

func (r *Reconciler) SetAuditRecorder(recorder interface {
	Record(context.Context, model.AdminAuditLog) error
}) {
	if r == nil {
		return
	}
	r.audit = recorder
}

func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.runCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runCycle(ctx)
		}
	}
}

func (r *Reconciler) runCycle(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, liveRuntimeReconcileTimeout)
	defer cancel()
	started := time.Now()
	r.reconcile(ctx)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		log.Printf("live runtime reconcile cycle timeout duration=%s", time.Since(started).Round(time.Millisecond))
	}
}

func (r *Reconciler) reconcile(ctx context.Context) {
	if r.leader != nil && !r.leader.IsLeader() {
		return
	}
	if !r.legacyLeaseCleanupDone {
		count, err := r.store.CancelAllLegacyLiveQuotaLeases(ctx, time.Now().UTC())
		if err != nil {
			log.Printf("billing manager legacy lease cleanup: %v", err)
			return
		}
		r.legacyLeaseCleanupDone = true
		if count > 0 {
			log.Printf("billing manager released legacy lease registrations sessions=%d", count)
		}
	}
	sessions, err := r.store.ListLiveRuntimeReconcileSessions(ctx)
	if err != nil {
		log.Printf("live runtime list sessions: %v", err)
		return
	}
	if len(sessions) == 0 {
		return
	}

	byTenant := make(map[int64][]reconcileJob)
	for start := 0; start < len(sessions); start += r.batchSize {
		end := start + r.batchSize
		if end > len(sessions) {
			end = len(sessions)
		}
		batch := sessions[start:end]
		states, stateErr := r.roomStates(ctx, batch)
		if stateErr != nil {
			log.Printf("live runtime batch room guard failed sessions=%d: %v", len(batch), stateErr)
			for _, session := range batch {
				byTenant[session.TenantID] = append(byTenant[session.TenantID], reconcileJob{Session: session, StateOK: false})
			}
			continue
		}
		for _, session := range batch {
			state, ok := states[stateKey{TenantID: session.TenantID, RoomID: session.RoomID}]
			byTenant[session.TenantID] = append(byTenant[session.TenantID], reconcileJob{Session: session, State: state, StateOK: ok})
		}
	}

	jobs := make(chan []reconcileJob, r.workers*2)
	var wg sync.WaitGroup
	for i := 0; i < r.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tenantJobs := range jobs {
				tenantCtx, cancel := context.WithTimeout(ctx, liveRuntimeTenantTimeout)
				r.reconcileTenant(tenantCtx, tenantJobs)
				if errors.Is(tenantCtx.Err(), context.DeadlineExceeded) && len(tenantJobs) > 0 {
					log.Printf("live runtime reconcile tenant timeout tenant=%d rooms=%d", tenantJobs[0].Session.TenantID, len(tenantJobs))
				}
				cancel()
			}
		}()
	}
	for _, tenantJobs := range byTenant {
		select {
		case jobs <- tenantJobs:
		case <-ctx.Done():
			close(jobs)
			waitReconcileWorkers(ctx, &wg)
			return
		}
	}
	close(jobs)
	if !waitReconcileWorkers(ctx, &wg) {
		log.Printf("live runtime reconcile workers exceeded cycle deadline; continuing on next scheduler tick")
	}
}

func waitReconcileWorkers(ctx context.Context, wg *sync.WaitGroup) bool {
	if wg == nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// reconcileTenant is the account-level billing manager. The durable
// live_runtime_sessions rows are the registration table: running means the
// room is registered for metering, paused/stopped means it is not. Core only
// reports cumulative working_seconds; it never decides account quota itself.
func (r *Reconciler) reconcileTenant(ctx context.Context, jobs []reconcileJob) {
	if len(jobs) == 0 {
		return
	}
	now := time.Now().UTC()
	tenantID := jobs[0].Session.TenantID

	remaining, err := r.store.GetQuotaRemainingSeconds(ctx, tenantID)
	if err != nil {
		log.Printf("billing manager quota read tenant=%d: %v", tenantID, err)
		return
	}
	if remaining == 0 {
		r.stopTenantForQuota(ctx, tenantID, jobs, now)
		return
	}

	quotaExhausted := false
	for i := range jobs {
		if ctx.Err() != nil {
			return
		}
		job := jobs[i]
		session := job.Session

		if !job.StateOK {
			// A transient Core read failure is not a billing or stop decision. Only
			// clean a durable registration after confirming the room is truly gone.
			if now.Sub(session.StartedAt) < liveRuntimeStartGracePeriod {
				continue
			}
			exists, existsErr := r.coreRoomExists(ctx, session.TenantID, session.RoomID)
			if existsErr != nil {
				log.Printf("billing manager confirm room tenant=%d room=%d session=%d: %v", session.TenantID, session.RoomID, session.ID, existsErr)
				continue
			}
			if exists {
				continue
			}
			if _, err := r.store.AbortLiveRuntimeSession(ctx, session.ID, "room_missing", now); err != nil {
				log.Printf("billing manager cleanup missing room session=%d: %v", session.ID, err)
			} else {
				r.recordSystemAudit(ctx, session, "room.runtime.cleanup_missing", "room_missing", "", 0)
			}
			continue
		}

		if isRecoverableCoreRestartSession(session) {
			fresh, freshErr := r.getCoreAgentRuntime(ctx, tenantID, session.RoomID)
			if freshErr != nil {
				log.Printf("billing manager recover confirm tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, freshErr)
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(fresh.State), "working") {
				// A genuine Core restart remains stopped. Keep this historical row
				// stopped; a later explicit Start creates a fresh paid session.
				continue
			}
			recovered, recoverErr := r.store.RecoverLiveRuntimeSessionAfterCoreRestart(ctx, session.ID, now)
			if recoverErr != nil {
				if !errors.Is(recoverErr, appdb.ErrLiveRuntimeNotRecoverable) {
					log.Printf("billing manager recover runtime tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, recoverErr)
				}
				continue
			}
			log.Printf("billing manager recovered false core restart tenant=%d room=%d session=%d boot=%s working=%d", tenantID, session.RoomID, session.ID, fresh.BootID, fresh.WorkingSeconds)
			r.recordSystemAudit(ctx, recovered, "agent.runtime.recovered", "false_core_restart", fresh.BootID, fresh.WorkingSeconds)
			session = recovered
			job.Session = recovered
			job.State.CoreBootID = fresh.BootID
			job.State.AgentState = fresh.State
			job.State.AgentStopReason = fresh.StopReason
			job.State.AgentWorkingSeconds = fresh.WorkingSeconds
			job.State.AgentUpdatedAt = fresh.UpdatedAt
		}

		if session.Status == "paused" {
			// Paused == unregistered from active consumption. Resume changes the
			// durable row back to running and it automatically re-enters this loop.
			continue
		}
		if session.Status != "running" {
			continue
		}

		roomLive := strings.EqualFold(strings.TrimSpace(job.State.Status), "live")
		if !roomLive {
			stopped, stopErr := r.stopCoreAgentRuntime(ctx, tenantID, session.RoomID, "live_finished")
			if stopErr != nil {
				log.Printf("billing manager stop offline room tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, stopErr)
				continue
			}
			finalSeconds := job.State.AgentWorkingSeconds
			if stopped.WorkingSeconds > finalSeconds {
				finalSeconds = stopped.WorkingSeconds
			}
			if _, err := r.store.StopLiveRuntimeSessionMeterSystem(ctx, session.ID, "room_offline", finalSeconds, now); err != nil {
				log.Printf("billing manager settle offline room tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, err)
				continue
			}
			r.recordSystemAudit(ctx, session, "agent.runtime.auto_stop", "room_offline", stopped.BootID, finalSeconds)
			continue
		}

		if job.State.SessionResumePending {
			// The live-session identity is unresolved; do not invent paid usage
			// until the operator chooses merge/fresh semantics.
			continue
		}

		if job.State.AgentState != "working" {
			fresh, freshErr := r.getCoreAgentRuntime(ctx, tenantID, session.RoomID)
			if freshErr != nil {
				log.Printf("billing manager confirm agent tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, freshErr)
				continue
			}
			if fresh.State == "working" {
				job.State.CoreBootID = fresh.BootID
				job.State.AgentState = fresh.State
				job.State.AgentStopReason = fresh.StopReason
				job.State.AgentWorkingSeconds = fresh.WorkingSeconds
			} else if fresh.State == "starting" {
				continue
			} else {
				reason, authoritative := authoritativeCoreStopReason(fresh)
				if !authoritative {
					continue
				}
				if isCoreRestartReason(reason) {
					confirmed, confirmErr := r.confirmCoreRestartStop(ctx, tenantID, session.RoomID)
					if confirmErr != nil {
						log.Printf("billing manager confirm core restart tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, confirmErr)
						continue
					}
					confirmedState := strings.ToLower(strings.TrimSpace(confirmed.State))
					if confirmedState == "working" {
						job.State.CoreBootID = confirmed.BootID
						job.State.AgentState = confirmed.State
						job.State.AgentStopReason = confirmed.StopReason
						job.State.AgentWorkingSeconds = confirmed.WorkingSeconds
						job.State.AgentUpdatedAt = confirmed.UpdatedAt
						fresh = confirmed
						log.Printf("billing manager ignored transient core restart tenant=%d room=%d session=%d boot=%s working=%d", tenantID, session.RoomID, session.ID, confirmed.BootID, confirmed.WorkingSeconds)
					} else if confirmedState == "starting" {
						continue
					} else {
						fresh = confirmed
						reason, authoritative = authoritativeCoreStopReason(confirmed)
						if !authoritative || !isCoreRestartReason(reason) {
							continue
						}
					}
				}
				if !strings.EqualFold(strings.TrimSpace(job.State.AgentState), "working") {
					if _, err := r.store.StopLiveRuntimeSessionMeterSystem(ctx, session.ID, reason, fresh.WorkingSeconds, now); err != nil {
						log.Printf("billing manager finalize stopped agent tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, err)
						continue
					}
					r.recordSystemAudit(ctx, session, "agent.runtime.auto_stop", reason, fresh.BootID, fresh.WorkingSeconds)
					continue
				}
			}
		}

		metered, meterErr := r.store.ReconcileLiveRuntimeMeter(
			ctx,
			session.ID,
			true,
			job.State.AgentWorkingSeconds,
			now,
		)
		if meterErr != nil {
			log.Printf("billing manager meter tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, meterErr)
			continue
		}
		if metered.TotalBilledSeconds != session.TotalBilledSeconds {
			log.Printf(
				"billing manager usage tenant=%d room=%d session=%d working=%d billed=%d delta=%d",
				tenantID,
				session.RoomID,
				session.ID,
				job.State.AgentWorkingSeconds,
				metered.TotalBilledSeconds,
				metered.TotalBilledSeconds-session.TotalBilledSeconds,
			)
		}
		if metered.Status == "stopped" && strings.EqualFold(metered.StopReason, "quota_exhausted") {
			quotaExhausted = true
		}
	}

	if !quotaExhausted {
		remaining, err = r.store.GetQuotaRemainingSeconds(ctx, tenantID)
		if err != nil {
			log.Printf("billing manager quota recheck tenant=%d: %v", tenantID, err)
			return
		}
		quotaExhausted = remaining == 0
	}
	if quotaExhausted {
		r.stopTenantForQuota(ctx, tenantID, jobs, now)
	}
}

func authoritativeCoreStopReason(state coreAgentRuntimeState) (string, bool) {
	status := strings.ToLower(strings.TrimSpace(state.State))
	if status != "stopped" && status != "stopping" {
		return "", false
	}
	reason := strings.ToLower(strings.TrimSpace(state.StopReason))
	if reason == "" {
		return "", false
	}
	return reason, true
}

func isCoreRestartReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "core_restart", "core_runtime_reset":
		return true
	default:
		return false
	}
}

func isRecoverableCoreRestartSession(session model.LiveRuntimeSession) bool {
	return strings.EqualFold(strings.TrimSpace(session.Status), "stopped") && isCoreRestartReason(session.StopReason)
}

func (r *Reconciler) confirmCoreRestartStop(
	ctx context.Context,
	tenantID, roomID int64,
) (coreAgentRuntimeState, error) {
	const confirmDelay = 500 * time.Millisecond
	timer := time.NewTimer(confirmDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return coreAgentRuntimeState{}, ctx.Err()
	case <-timer.C:
	}
	return r.getCoreAgentRuntime(ctx, tenantID, roomID)
}

func (r *Reconciler) stopTenantForQuota(
	ctx context.Context,
	tenantID int64,
	jobs []reconcileJob,
	now time.Time,
) {
	log.Printf("billing manager quota exhausted tenant=%d active_rooms=%d; stopping registered agents", tenantID, len(jobs))
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		session := job.Session
		if session.Status != "running" {
			continue
		}

		stopped, err := r.stopCoreAgentRuntime(ctx, tenantID, session.RoomID, "quota_exhausted")
		if err != nil {
			// Keep the durable registration open so the next billing cycle retries
			// the explicit stop instead of pretending the room has stopped.
			log.Printf("billing manager quota stop tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, err)
			continue
		}
		finalSeconds := job.State.AgentWorkingSeconds
		if stopped.WorkingSeconds > finalSeconds {
			finalSeconds = stopped.WorkingSeconds
		}
		closed, err := r.store.StopLiveRuntimeSessionMeterSystem(
			ctx,
			session.ID,
			"quota_exhausted",
			finalSeconds,
			now,
		)
		if err != nil {
			log.Printf("billing manager quota settle tenant=%d room=%d session=%d: %v", tenantID, session.RoomID, session.ID, err)
			continue
		}
		log.Printf(
			"billing manager unregistered tenant=%d room=%d session=%d reason=quota_exhausted working=%d billed=%d",
			tenantID,
			session.RoomID,
			session.ID,
			finalSeconds,
			closed.TotalBilledSeconds,
		)
		r.recordSystemAudit(ctx, session, "agent.runtime.auto_stop", "quota_exhausted", stopped.BootID, finalSeconds)
	}
}

func (r *Reconciler) recordSystemAudit(
	ctx context.Context,
	session model.LiveRuntimeSession,
	action string,
	reason string,
	coreBootID string,
	workingSeconds uint64,
) {
	if r == nil || r.audit == nil {
		return
	}
	detail, _ := json.Marshal(map[string]any{
		"working_seconds": workingSeconds,
	})
	beforeState := `{"agent_state":"working"}`
	afterState := `{"agent_state":"stopped"}`
	if action == "room.runtime.cleanup_missing" {
		beforeState = `{"runtime_session":"open"}`
		afterState = `{"runtime_session":"closed","room_exists":false}`
	}
	roomName := ""
	if action != "room.runtime.cleanup_missing" {
		roomName, _ = r.coreRoomName(ctx, session.TenantID, session.RoomID)
	}
	entry := model.AdminAuditLog{
		ActorUserID:      0,
		ActorUsername:    "system",
		ActorRole:        "system",
		ActorType:        "system",
		Source:           "core_reconciler",
		Action:           action,
		TargetTenantID:   session.TenantID,
		TargetRoomID:     session.RoomID,
		ObjectType:       "room",
		ObjectID:         strconv.FormatInt(session.RoomID, 10),
		ObjectName:       roomName,
		Reason:           reason,
		BeforeState:      beforeState,
		AfterState:       afterState,
		RuntimeSessionID: session.ID,
		CoreBootID:       coreBootID,
		DetailJSON:       string(detail),
		Result:           "success",
	}
	if err := r.audit.Record(ctx, entry); err != nil {
		log.Printf("record system audit action=%s tenant=%d room=%d: %v", action, session.TenantID, session.RoomID, err)
	}
}

func (r *Reconciler) roomStates(
	ctx context.Context,
	sessions []model.LiveRuntimeSession,
) (map[stateKey]coreRoomState, error) {
	items := make([]map[string]int64, 0, len(sessions))
	seen := make(map[stateKey]struct{}, len(sessions))
	for _, session := range sessions {
		key := stateKey{TenantID: session.TenantID, RoomID: session.RoomID}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, map[string]int64{
			"tenant_id": session.TenantID,
			"room_id":   session.RoomID,
		})
	}

	resp, err := r.core.DoAny(
		ctx,
		http.MethodPost,
		"/internal/v1/rooms/runtime-states",
		nil,
		map[string]any{"items": items},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("core batch room status %d", resp.StatusCode)
	}
	var body struct {
		Items []coreRoomState `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	states := make(map[stateKey]coreRoomState, len(body.Items))
	for _, state := range body.Items {
		states[stateKey{TenantID: state.TenantID, RoomID: state.RoomID}] = state
	}
	return states, nil
}

func (r *Reconciler) getCoreAgentRuntime(
	ctx context.Context,
	tenantID, roomID int64,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := r.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query,
		nil,
	)
	if err != nil {
		return coreAgentRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreAgentRuntimeState{}, fmt.Errorf("core agent runtime status %d", resp.StatusCode)
	}
	var state coreAgentRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return coreAgentRuntimeState{}, err
	}
	return state, nil
}

func (r *Reconciler) coreRoomExists(
	ctx context.Context,
	tenantID, roomID int64,
) (bool, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := r.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		query,
		nil,
	)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	default:
		return false, fmt.Errorf("core room status %d", resp.StatusCode)
	}
}

func (r *Reconciler) coreRoomName(
	ctx context.Context,
	tenantID, roomID int64,
) (string, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := r.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		query,
		nil,
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("core room status %d", resp.StatusCode)
	}
	var room struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&room); err != nil {
		return "", err
	}
	return room.Name, nil
}

func (r *Reconciler) stopCoreAgentRuntime(
	ctx context.Context,
	tenantID, roomID int64,
	reason string,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := r.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query,
		map[string]any{
			"command":     "stop",
			"stop_reason": strings.TrimSpace(reason),
		},
	)
	if err != nil {
		return coreAgentRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreAgentRuntimeState{}, fmt.Errorf("core agent stop status %d", resp.StatusCode)
	}
	var state coreAgentRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return coreAgentRuntimeState{}, err
	}
	return state, nil
}
