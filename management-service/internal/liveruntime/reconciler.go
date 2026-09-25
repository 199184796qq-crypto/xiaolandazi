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
	TenantID             int64     `json:"tenant_id"`
	RoomID               int64     `json:"room_id"`
	Status               string    `json:"status"`
	UpdatedAt            time.Time `json:"updated_at"`
	AgentState           string    `json:"agent_state"`
	AgentMode            string    `json:"agent_mode"`
	AgentWorkingSeconds  uint64    `json:"agent_working_seconds"`
	AgentUpdatedAt       time.Time `json:"agent_updated_at"`
	SessionResumePending bool      `json:"session_resume_pending"`
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

	jobs := make(chan reconcileJob, r.workers*2)
	var wg sync.WaitGroup
	for i := 0; i < r.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				r.reconcileOne(ctx, job)
			}
		}()
	}

	for start := 0; start < len(sessions); start += r.batchSize {
		end := start + r.batchSize
		if end > len(sessions) {
			end = len(sessions)
		}
		batch := sessions[start:end]
		states, err := r.roomStates(ctx, batch)
		if err != nil {
			log.Printf("live runtime batch room guard failed sessions=%d: %v", len(batch), err)
			for _, session := range batch {
				jobs <- reconcileJob{Session: session, StateOK: false}
			}
			continue
		}
		for _, session := range batch {
			state, ok := states[stateKey{TenantID: session.TenantID, RoomID: session.RoomID}]
			jobs <- reconcileJob{Session: session, State: state, StateOK: ok}
		}
	}

	close(jobs)
	wg.Wait()
}

func (r *Reconciler) reconcileOne(ctx context.Context, job reconcileJob) {
	session := job.Session
	if !job.StateOK {
		// Never charge wall-clock time when Core cannot prove that the paid
		// intelligent-agent layer was actually working.
		log.Printf(
			"live runtime core state missing tenant=%d room=%d",
			session.TenantID,
			session.RoomID,
		)
		return
	}

	roomLive := job.State.Status == "live"
	if roomLive && job.State.SessionResumePending {
		// 重新开播后必须等用户明确选择“续接上一场/新开一场”。这一段时间
		// 只保留免费采集，绝不能让后台对账器把付费 Agent 自动拉起来。
		if err := r.setCoreAgentState(ctx, session.TenantID, session.RoomID, "stopped", job.State.AgentWorkingSeconds); err != nil {
			log.Printf("hold core agent for session decision tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
		}
		return
	}
	if !roomLive {
		updated, err := r.store.ReconcileLiveRuntimeMeter(
			ctx,
			session.ID,
			false,
			job.State.AgentWorkingSeconds,
			time.Now().UTC(),
		)
		if err != nil {
			log.Printf("live runtime offline reconcile session=%d: %v", session.ID, err)
			return
		}
		if err := r.setCoreAgentState(ctx, updated.TenantID, updated.RoomID, "stopped", updated.TotalBilledSeconds); err != nil {
			log.Printf("sync core agent stop tenant=%d room=%d: %v", updated.TenantID, updated.RoomID, err)
		}
		return
	}

	if session.Status == "paused" {
		if err := r.setCoreAgentState(ctx, session.TenantID, session.RoomID, "paused", session.TotalBilledSeconds); err != nil {
			log.Printf("sync core agent pause tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
		}
		return
	}

	if session.Status != "running" {
		return
	}

	// Core is the work-time meter. If Core restarted or its gate drifted to
	// stopped, restore it from the durable billed total first and start counting
	// again from that baseline. Do not charge the outage gap.
	if job.State.AgentState != "working" || job.State.AgentWorkingSeconds < session.TotalBilledSeconds {
		if err := r.setCoreAgentState(ctx, session.TenantID, session.RoomID, "working", session.TotalBilledSeconds); err != nil {
			log.Printf("restore core agent meter tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
		}
		return
	}

	updated, err := r.store.ReconcileLiveRuntimeMeter(
		ctx,
		session.ID,
		true,
		job.State.AgentWorkingSeconds,
		time.Now().UTC(),
	)
	if err != nil {
		log.Printf("live runtime meter reconcile session=%d: %v", session.ID, err)
		return
	}

	agentState := "stopped"
	if updated.Status == "running" {
		agentState = "working"
	} else if updated.Status == "paused" {
		agentState = "paused"
	}
	if err := r.setCoreAgentState(ctx, updated.TenantID, updated.RoomID, agentState, updated.TotalBilledSeconds); err != nil {
		log.Printf("sync core agent runtime tenant=%d room=%d state=%s: %v", updated.TenantID, updated.RoomID, agentState, err)
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
