package liveruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type Reconciler struct {
	store     *appdb.Store
	core      *coreclient.Client
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
	AgentMode                  string     `json:"agent_mode"`
	AgentWorkingSeconds        uint64     `json:"agent_working_seconds"`
	AgentLeaseRemainingSeconds uint64     `json:"agent_lease_remaining_seconds"`
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

const liveRuntimeStartGracePeriod = 20 * time.Second

type coreAgentRuntimeState struct {
	BootID                string     `json:"boot_id"`
	State                 string     `json:"state"`
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

func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
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
				r.reconcileTenant(ctx, tenantJobs)
			}
		}()
	}
	for _, tenantJobs := range byTenant {
		jobs <- tenantJobs
	}
	close(jobs)
	wg.Wait()
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
			// Missing Core proof never becomes billable wall-clock time.
			log.Printf("live runtime core state missing tenant=%d room=%d", session.TenantID, session.RoomID)
			continue
		}
		roomLive := job.State.Status == "live"
		if roomLive && job.State.SessionResumePending {
			_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, true, false, now)
			if err := r.setCoreAgentState(ctx, session.TenantID, session.RoomID, "stopped", job.State.AgentWorkingSeconds); err != nil {
				log.Printf("hold core agent for session decision tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
			}
			continue
		}
		if !roomLive {
			_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, true, false, now)
			if _, err := r.store.AbortLiveRuntimeSession(ctx, session.ID, "room_offline", now); err != nil {
				log.Printf("abort offline paid runtime session=%d: %v", session.ID, err)
			}
			if err := r.setCoreAgentState(ctx, session.TenantID, session.RoomID, "stopped", job.State.AgentWorkingSeconds); err != nil {
				log.Printf("sync core offline stop tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
			}
			continue
		}
		if session.Status == "paused" {
			_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, true, false, now)
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
				job.State.AgentWorkingSeconds = fresh.WorkingSeconds
				job.State.AgentLeaseRemainingSeconds = fresh.LeaseRemainingSeconds
				job.State.AgentLeaseUntil = fresh.LeaseUntil
			} else {
				// A lease may have ended cleanly between ticks. Settle any fully confirmed
				// completed lease first, then cancel the unfinished tail with zero charge.
				_, _ = r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, fresh.BootID, fresh.WorkingSeconds, false, true, now)
				if _, err := r.store.AbortLiveRuntimeSession(ctx, session.ID, "core_runtime_reset", now); err != nil {
					log.Printf("abort stopped paid runtime session=%d tenant=%d room=%d: %v", session.ID, session.TenantID, session.RoomID, err)
				}
				continue
			}
		}

		if _, err := r.store.ReconcileLiveQuotaLeases(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds, false, true, now); err != nil {
			log.Printf("live lease reconcile session=%d: %v", session.ID, err)
			continue
		}
		runway, err := r.store.LiveQuotaLeaseRunway(ctx, session.TenantID, session.ID, job.State.CoreBootID, job.State.AgentWorkingSeconds)
		if err != nil {
			log.Printf("live lease runway session=%d: %v", session.ID, err)
			continue
		}
		if runway <= appdb.LiveQuotaLeaseRenewThresholdSeconds {
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
			_, _ = r.store.AbortLiveRuntimeSession(ctx, grant.RuntimeSessionID, "core_lease_failed", now)
		}
	}
	for _, request := range requests {
		if granted[request.RuntimeSessionID] > 0 {
			continue
		}
		job := requestJobs[request.RuntimeSessionID]
		runway, _ := r.store.LiveQuotaLeaseRunway(ctx, tenantID, request.RuntimeSessionID, request.CoreBootID, job.State.AgentWorkingSeconds)
		if runway == 0 {
			if err := r.setCoreAgentState(ctx, tenantID, request.RoomID, "stopped", job.State.AgentWorkingSeconds); err != nil {
				log.Printf("stop core on quota exhaustion tenant=%d room=%d: %v", tenantID, request.RoomID, err)
			}
			_, _ = r.store.AbortLiveRuntimeSession(ctx, request.RuntimeSessionID, "quota_exhausted", now)
		}
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

func (r *Reconciler) setCoreAgentState(
	ctx context.Context,
	tenantID, roomID int64,
	state string,
	baseWorkingSeconds uint64,
) error {
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
			"state":                state,
			"base_working_seconds": baseWorkingSeconds,
		},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core agent runtime status %d", resp.StatusCode)
	}
	return nil
}
