package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/errordetail"
	"livecompanion/management/internal/model"
)

type liveResumeProgram struct {
	ProgramID       string  `json:"program_id"`
	VersionID       int64   `json:"version_id"`
	SessionID       string  `json:"session_id"`
	Running         bool    `json:"running"`
	ManualPaused    *bool   `json:"manual_paused"`
	ControlRevision *uint64 `json:"control_revision"`
}

type liveResumeFence struct {
	ProgramID       string `json:"expected_program_id"`
	VersionID       int64  `json:"expected_version_id"`
	ControlRevision uint64 `json:"expected_control_revision"`
	BootID          string `json:"expected_boot_id"`
	RunGeneration   uint64 `json:"expected_run_generation"`
}

func (p liveResumeProgram) validSession(session int64) bool {
	return p.ProgramID != "" && p.VersionID > 0 && p.Running && p.ManualPaused != nil && p.ControlRevision != nil &&
		(p.SessionID == fmt.Sprintf("live-runtime-%d", session) || p.SessionID == fmt.Sprintf("recording-stream-%d", session))
}

func (p liveResumeProgram) matches(f liveResumeFence, session int64) bool {
	return p.validSession(session) && p.ProgramID == f.ProgramID && p.VersionID == f.VersionID && *p.ControlRevision == f.ControlRevision
}

// This operation deliberately has no StartProgram or generation callback.
// The fixed program and Core's guarded resume own the existing cursor.
type liveResumeRecovery struct {
	Session            model.LiveRuntimeSession
	Realm              string
	Fence              liveResumeFence
	HasProgram         bool
	PreviousGeneration uint64
	StartUnconfirmed   bool
	ReadSession        func(context.Context) (model.LiveRuntimeSession, error)
	ReadAgent          func(context.Context) (coreAgentRuntimeState, error)
	ReadProgram        func(context.Context) (liveResumeProgram, error)
	Resume             func(context.Context, liveResumeFence) (liveResumeProgram, error)
	Wait               func(context.Context, time.Duration) error
}

func (r *liveResumeRecovery) attempt(ctx context.Context) error {
	current, err := r.ReadSession(ctx)
	if err != nil {
		return err
	}
	if !producerOwnsRunningSession(current, r.Session.ID, r.Realm) || current.TenantID != r.Session.TenantID || current.RoomID != r.Session.RoomID {
		return anchorcycle.ErrStale
	}
	agent, err := r.ReadAgent(ctx)
	if err != nil {
		return err
	}
	if agent.BootID != r.Fence.BootID || agent.RoomID != r.Session.RoomID {
		return anchorcycle.ErrStale
	}
	if r.StartUnconfirmed && agent.RunGeneration == r.PreviousGeneration && agent.State != "working" {
		// A lost start response does not prove that start was accepted. Never
		// blindly send another unguarded start, especially after a manual stop.
		return errors.New("智能体恢复尚未确认，正在核对；如持续未恢复，请重新人工操作")
	}
	if agent.RunGeneration != r.Fence.RunGeneration || agent.State != "working" {
		return anchorcycle.ErrStale
	}
	if !r.HasProgram {
		return nil
	}
	program, err := r.ReadProgram(ctx)
	if err != nil {
		return err
	}
	if !program.matches(r.Fence, r.Session.ID) {
		return anchorcycle.ErrStale
	}
	if !*program.ManualPaused {
		return nil
	} // Includes an interaction now owning the audio lane.
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := r.Resume(ctx, r.Fence)
	if err != nil {
		return err
	}
	if !result.matches(r.Fence, r.Session.ID) {
		return anchorcycle.ErrStale
	}
	if *result.ManualPaused {
		return errors.New("声音恢复尚未确认，正在核对原播放位置")
	}
	return nil
}

func (r *liveResumeRecovery) run(ctx context.Context) error {
	return retryLiveOperation(ctx, "播放恢复核对", 0, r.Wait, func() error {
		limited, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return r.attempt(limited)
	})
}

func (s *Server) resumeCoreRequest(ctx context.Context, tenant, room int64, method, suffix string, body any) (liveResumeProgram, error) {
	var result liveResumeProgram
	resp, err := s.core.DoRoomOwner(ctx, tenant, room, method, fmt.Sprintf("/internal/v1/rooms/%d/audio/program%s", room, suffix), url.Values{"tenant_id": {strconv.FormatInt(tenant, 10)}}, body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return result, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var rejection struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &rejection)
		if resp.StatusCode == http.StatusConflict && (rejection.Code == "audio_resume_baseline_conflict" || rejection.Code == "audio_start_cancelled") {
			return result, anchorcycle.ErrStale
		}
		return result, fmt.Errorf("播放恢复服务响应%d：%s", resp.StatusCode, errordetail.Safe(rejection.Error))
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("播放恢复回执尚未确认：%w", err)
	}
	if result.ManualPaused == nil || result.ControlRevision == nil {
		return result, errors.New("播放恢复回执缺少状态信息，正在继续核对")
	}
	return result, nil
}

func (s *Server) resumeAgentRequest(ctx context.Context, tenant, room int64, method string, body any) (coreAgentRuntimeState, error) {
	var result coreAgentRuntimeState
	resp, err := s.core.DoRoomOwner(ctx, tenant, room, method, fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", room), url.Values{"tenant_id": {strconv.FormatInt(tenant, 10)}}, body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("智能体恢复服务响应%d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (s *Server) cancelLiveResumeRecovery(tenant, room int64) {
	s.cycleCancels.Lock()
	defer s.cycleCancels.Unlock()
	for key, entry := range s.cycleCancels.entries {
		if entry.tenant == tenant && entry.room == room && strings.HasPrefix(key, "resume-") {
			entry.cancel()
		}
	}
}

func (s *Server) liveResumeRecoveryContext(tenant, room int64) (context.Context, func()) {
	parent := s.anchorProducerContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	key := "resume-" + anchorcycle.ID()
	s.cycleCancels.Lock()
	if s.cycleCancels.entries == nil {
		s.cycleCancels.entries = map[string]cycleCancel{}
	}
	for other, entry := range s.cycleCancels.entries {
		if entry.tenant == tenant && entry.room == room && strings.HasPrefix(other, "resume-") {
			entry.cancel()
		}
	}
	s.cycleCancels.entries[key] = cycleCancel{tenant: tenant, room: room, cancel: cancel}
	s.cycleCancels.Unlock()
	return ctx, func() { cancel(); s.cycleCancels.Lock(); delete(s.cycleCancels.entries, key); s.cycleCancels.Unlock() }
}

func (s *Server) reportLiveResumeRecovery(ctx context.Context, session model.LiveRuntimeSession, message string) {
	log.Printf("live resume tenant=%d room=%d session=%d status=%s", session.TenantID, session.RoomID, session.ID, message)
	if s.audit == nil {
		return
	}
	raw, _ := json.Marshal(map[string]any{"场次": session.ID, "状态": message})
	limited, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := s.audit.Record(limited, model.AdminAuditLog{ActorType: "system", Source: "直播恢复", Action: "直播恢复核对", TargetTenantID: session.TenantID, TargetRoomID: session.RoomID, RuntimeSessionID: session.ID, Reason: message, DetailJSON: string(raw), Result: "已记录"})
	if err != nil {
		log.Printf("live resume diagnostic save failed: %s", errordetail.Safe(err.Error()))
	}
}

func (s *Server) continueLiveResumeRecovery(ctx context.Context, done func(), recovery *liveResumeRecovery) {
	defer done()
	err := recovery.run(ctx)
	if err == nil {
		s.reportLiveResumeRecovery(ctx, recovery.Session, "已确认继续原场次和原播放位置")
		return
	}
	if liveContinuityTerminal(ctx, err) {
		log.Printf("live resume ended tenant=%d room=%d session=%d ownership or manual intent changed", recovery.Session.TenantID, recovery.Session.RoomID, recovery.Session.ID)
		return
	}
	log.Printf("live resume unconfirmed session=%d: %s", recovery.Session.ID, errordetail.Safe(err.Error()))
}

func writeLiveResumePending(w http.ResponseWriter, session model.LiveRuntimeSession) {
	// Preserve the existing session response shape and add an honest message;
	// accepting the command is never presented as having heard external audio.
	writeJSON(w, http.StatusAccepted, struct {
		model.LiveRuntimeSession
		RecoveryPending bool   `json:"recovery_pending"`
		Message         string `json:"message"`
	}{session, true, "恢复尚未确认，正在核对原场次和播放位置；如持续未恢复，请重新人工操作"})
}
