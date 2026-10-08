package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"time"

	appdb "livecompanion/management/internal/db"
)

func (s *Server) hardwareConnection(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	var input appdb.HardwareConnectionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "设备连接事件格式错误")
		return
	}
	if err := appdb.ValidateHardwareConnection(input, time.Now().UTC()); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// Finishing the stop must not depend on a box retaining power or the
	// gateway keeping its HTTP request open until the acknowledgement.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 12*time.Second)
	defer cancel()
	result, err := s.store.ObserveHardwareConnection(ctx, input)
	if err != nil {
		s.deviceOperationError(w, err)
		return
	}
	stopped := false
	if result.Applied && input.Phase == "shutdown" && result.BindingRole == "primary" && result.RoomID > 0 {
		stopped, err = s.stopForHardwareShutdown(ctx, result)
		if err != nil {
			log.Printf("hardware shutdown stop deferred tenant=%d room=%d device=%d: %v", result.TenantID, result.RoomID, result.DeviceID, err)
		}
	}
	display := "离线"
	if input.Phase == "connect" || input.Phase == "heartbeat" {
		if device, getErr := s.store.GetLiveDevice(ctx, result.TenantID, result.DeviceID); getErr == nil {
			display = device.DisplayStatus
		}
	}
	writeJSON(w, 200, map[string]any{"applied": result.Applied, "display_status": display, "agent_stopped": stopped, "stop_pending": err != nil})
}

func (s *Server) stopForHardwareShutdown(ctx context.Context, target appdb.HardwareConnectionResult) (bool, error) {
	session, err := s.store.GetLiveRuntimeByRoom(ctx, target.TenantID, target.RoomID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if session.Status != "running" && session.Status != "paused" {
		return true, nil
	}
	if !session.BelongsToExecutionRealm(s.executionRealm) || session.DeviceID == nil || *session.DeviceID != target.DeviceID {
		return false, errors.New("关机设备与当前运行场次不一致")
	}
	// Do not revive or charge a stopped registration while a Core stop is
	// retried. The device_shutdown marker also forces the periodic guard.
	stopped, stopErr := s.requestCoreAgentStop(ctx, target.TenantID, target.RoomID, session.TotalBilledSeconds, "device_shutdown")
	seconds := session.TotalBilledSeconds
	if stopErr == nil && stopped.WorkingSeconds > seconds {
		seconds = stopped.WorkingSeconds
	}
	_, settleErr := s.store.StopLiveRuntimeSessionMeterSystem(ctx, session.ID, "device_shutdown", seconds, time.Now().UTC())
	if settleErr != nil {
		return false, settleErr
	}
	cleanupErr := s.StopHardwareRoomAudio(ctx, target.TenantID, target.RoomID)
	err = errors.Join(stopErr, cleanupErr)
	return err == nil, err
}

func (s *Server) StopHardwareRoomAudio(ctx context.Context, tenantID, roomID int64) error {
	programErr := s.requestCorePublishedPlanProgramControl(ctx, tenantID, roomID, "stop")
	cycleErr := s.endAnchorCyclesForRoom(ctx, tenantID, roomID)
	return errors.Join(programErr, cycleErr)
}
