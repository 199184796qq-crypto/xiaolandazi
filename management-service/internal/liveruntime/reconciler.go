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
	leader    interface{ IsLeader() bool }
	interval  time.Duration
	workers   int
	batchSize int
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
	sessions, err := r.store.ListRunningLiveRuntimeSessions(ctx)
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

func (r *Reconciler) reconcileTenant(ctx context.Context, jobs []reconcileJob) {
	if len(jobs) == 0 {
		return
	}
	now := time.Now().UTC()
	tenantID := jobs[0].Session.TenantID
	requests := make([]appdb.LiveQuotaLeaseRequest, 0, len(jobs))
	requestJobs := make(map[int64]reconcileJob)

	for _, job := range jobs {
		session := job.Session
		if !job.StateOK {
			// Missing Core proof never becomes billable wall-clock time. Confirm a
			// genuinely deleted room before cleaning the durable zombie session;
			// transient batch/read failures must not mutate business state.
			if now.Sub(session.StartedAt) < liveRuntimeStartGracePeriod {
				continue
			}
			exists, existsErr := r.coreRoomExists(ctx, session.TenantID, session.RoomID)
			if existsErr != nil {
				log.Printf("confirm missing core room tenant=%d room=%d session=%d: %v", session.TenantID, session.RoomID, session.ID, existsErr)
				continue
			}
			if exists {
				log.Printf("live runtime core state missing tenant=%d room=%d", session.TenantID, session.RoomID)
				continue
			}
			if _, err := r.store.AbortLiveRuntimeSession(ctx, session.ID, "room_missing", now); err != nil {
				log.Printf("cleanup missing-room runtime session=%d tenant=%d room=%d: %v", session.ID, session.TenantID, session.RoomID, err)
			} else {
				log.Printf("cleaned missing-room runtime session=%d tenant=%d room=%d", session.ID, session.TenantID, session.RoomID)
				r.recordSystemAudit(ctx, session, "room.runtime.cleanup_missing", "room_missing", "", 0)
			}
			continue
		}
		roomLive := job.State.Status == "live"
		if roomLive && job.State.SessionResumePending {
			_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, true, false, now)
			continue
		}
		if !roomLive && job.State.AgentState == "working" {
			// The collector/session_end path inside Core owns live-finished stops.
			// Do not race it from Management and do not renew this room.
			continue
		}
		if session.Status == "paused" {
			if job.State.AgentState == "stopped" {
				_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, true, true, now)
			}
			continue
		}
		if session.Status != "running" {
			continue
		}

		if job.State.AgentState != "working" {
			// StartLiveRuntimeSession is committed before policy snapshot + quota lease
			// + Core activation finish. A batch snapshot taken in that small window is
			// stale by the time this worker runs. Never kill a brand-new session from
			// that stale observation.
			if now.Sub(session.StartedAt) < liveRuntimeStartGracePeriod {
				continue
			}

			// Batch state can also become stale while tenant jobs wait in the worker
			// queue. Confirm the single-room paid runtime immediately before treating
			// Core as stopped. If confirmation fails, fail closed on billing but keep
			// the durable session for the next reconcile tick.
			fresh, freshErr := r.getCoreAgentRuntime(ctx, session.TenantID, session.RoomID)
			if freshErr != nil {
				log.Printf("confirm stopped core agent tenant=%d room=%d session=%d: %v", session.TenantID, session.RoomID, session.ID, freshErr)
				continue
			}
			if fresh.State == "working" {
				job.State.CoreBootID = fresh.BootID
				job.State.AgentState = fresh.State
				job.State.AgentStopReason = fresh.StopReason
				job.State.AgentWorkingSeconds = fresh.WorkingSeconds
				job.State.AgentLeaseRemainingSeconds = fresh.LeaseRemainingSeconds
				job.State.AgentLeaseUntil = fresh.LeaseUntil
			} else if fresh.State == "starting" {
				// Starting is Core-owned and may still legitimately become working.
				// Leave the durable session untouched until the next observation.
				continue
			} else {
				// Core has already made the stop decision. Management only settles the
				// durable quota record and closes the matching session. A stopping
				// state is also recoverable here so a Management crash cannot strand a
				// room forever between stopping and stopped.
				reason, authoritative := authoritativeCoreStopReason(fresh)
				if !authoritative {
					// An empty stop reason is not authoritative proof that Core
					// intentionally stopped paid work. Treat it as a transient/default
					// snapshot and retry on the next reconciliation cycle instead of
					// inventing core_restart and killing a still-running session.
					log.Printf(
						"ignore unproven stopped core agent tenant=%d room=%d session=%d boot=%s state=%q working_seconds=%d lease_remaining=%d",
						session.TenantID,
						session.RoomID,
						session.ID,
						fresh.BootID,
						fresh.State,
						fresh.WorkingSeconds,
						fresh.LeaseRemainingSeconds,
					)
					continue
				}
				normalCompletion := reason == "quota_exhausted" || reason == "live_finished" || reason == "manual"
				_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, fresh.BootID, fresh.WorkingSeconds, normalCompletion, normalCompletion, now)
				if _, err := r.store.AbortLiveRuntimeSession(ctx, session.ID, reason, now); err != nil {
					log.Printf("abort stopped paid runtime session=%d tenant=%d room=%d: %v", session.ID, session.TenantID, session.RoomID, err)
				} else {
					r.recordSystemAudit(ctx, session, "agent.runtime.auto_stop", reason, fresh.BootID, fresh.WorkingSeconds)
				}
				continue
			}
		}

		if _, err := r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, false, true, now); err != nil {
			log.Printf("live lease reconcile session=%d: %v", session.ID, err)
			continue
		}
		if !job.State.AgentLeaseRenewalDue {
			continue
		}
		runway, err := r.store.LiveQuotaLeaseRunway(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds)
		if err != nil {
			log.Printf("live lease runway session=%d: %v", session.ID, err)
			continue
		}
		request := appdb.LiveQuotaLeaseRequest{
			RoomID:                  session.RoomID,
			RuntimeSessionID:        session.ID,
			CoreBootID:              job.State.CoreBootID,
			CoreWorkingStartSeconds: job.State.AgentWorkingSeconds + runway,
			RequestedSeconds:        appdb.LiveQuotaLeaseSeconds,
		}
		requests = append(requests, request)
		requestJobs[session.ID] = job
	}

	if len(requests) == 0 {
		return
	}
	grants, err := r.store.AllocateLiveQuotaLeases(ctx, tenantID, requests, now)
	if err != nil {
		log.Printf("tenant live quota schedule tenant=%d: %v", tenantID, err)
		return
	}
	granted := make(map[int64]uint64, len(grants))
	for _, grant := range grants {
		granted[grant.RuntimeSessionID] += grant.AllocatedSeconds
		job, ok := requestJobs[grant.RuntimeSessionID]
		if !ok {
			continue
		}
		if err := r.grantCoreAgentLease(ctx, tenantID, grant.RoomID, grant.AllocatedSeconds); err != nil {
			log.Printf("grant core lease tenant=%d room=%d session=%d: %v", tenantID, grant.RoomID, grant.RuntimeSessionID, err)
			_, _ = r.store.ReconcileLiveQuotaLeases(ctx, tenantID, grant.RuntimeSessionID, job.State.CoreBootID, job.State.AgentWorkingSeconds, true, false, now)
			// Keep the durable session open. Core remains authoritative and will
			// either receive a later lease retry or stop itself when its current
			// lease expires. Closing the session here can create a second session
			// while the old Core runtime is still working.
		} else {
			log.Printf(
				"live lease renewed tenant=%d room=%d session=%d seconds=%d previous_remaining=%d",
				tenantID,
				grant.RoomID,
				grant.RuntimeSessionID,
				grant.AllocatedSeconds,
				job.State.AgentLeaseRemainingSeconds,
			)
		}
	}
	for _, request := range requests {
		if granted[request.RuntimeSessionID] > 0 {
			continue
		}
		// The scheduler only reports that no further lease can be granted. Core
		// owns the actual stop at lease expiry; the next state observation will
		// settle and close the durable session with quota_exhausted.
		log.Printf("live quota exhausted; waiting for Core lease expiry tenant=%d room=%d session=%d", tenantID, request.RoomID, request.RuntimeSessionID)
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

func (r *Reconciler) grantCoreAgentLease(
	ctx context.Context,
	tenantID, roomID int64,
	leaseSeconds uint64,
) error {
	if leaseSeconds == 0 {
		return nil
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := r.core.DoRoom(
		ctx, tenantID, roomID, http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query, map[string]any{"lease_seconds": leaseSeconds},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core agent lease status %d", resp.StatusCode)
	}
	return nil
}
