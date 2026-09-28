package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"livecompanion/management/internal/model"
)

type coreRuntimeStatus struct {
	Available        bool      `json:"available"`
	Status           string    `json:"status"`
	CoreBootID       string    `json:"core_boot_id,omitempty"`
	ActiveRooms      int       `json:"active_rooms"`
	CheckedAt        time.Time `json:"checked_at"`
	LastTransitionAt time.Time `json:"last_transition_at,omitempty"`
	initialized      bool
}

type coreHealthPayload struct {
	Status      string `json:"status"`
	BootID      string `json:"boot_id"`
	ActiveRooms int    `json:"active_rooms"`
}

func (s *Server) getCoreRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	s.coreStatusMu.RLock()
	status := s.coreStatus
	s.coreStatusMu.RUnlock()
	if !status.initialized {
		status = s.probeCoreStatus(r.Context())
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) RunCoreStatusWatch(ctx context.Context) {
	if s == nil || s.core == nil {
		return
	}
	check := func() {
		probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		next := s.probeCoreStatus(probeCtx)
		s.applyCoreStatusTransition(ctx, next)
	}
	check()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func (s *Server) probeCoreStatus(ctx context.Context) coreRuntimeStatus {
	now := time.Now().UTC()
	status := coreRuntimeStatus{
		Available:   false,
		Status:      "unavailable",
		CheckedAt:   now,
		initialized: true,
	}
	resp, err := s.core.DoAny(ctx, http.MethodGet, "/healthz", nil, nil)
	if err != nil {
		return status
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
		return status
	}
	var payload coreHealthPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return status
	}
	status.Available = true
	status.Status = "ok"
	status.CoreBootID = payload.BootID
	status.ActiveRooms = payload.ActiveRooms
	return status
}

func (s *Server) applyCoreStatusTransition(ctx context.Context, next coreRuntimeStatus) {
	s.coreStatusMu.Lock()
	previous := s.coreStatus
	if !previous.initialized {
		next.LastTransitionAt = next.CheckedAt
		s.coreStatus = next
		s.coreStatusMu.Unlock()
		return
	}
	transition := ""
	reason := ""
	if previous.Available && !next.Available {
		transition = "system.core.offline"
		reason = "health_check_failed"
		next.LastTransitionAt = next.CheckedAt
	} else if !previous.Available && next.Available {
		transition = "system.core.recovered"
		reason = "health_check_recovered"
		next.LastTransitionAt = next.CheckedAt
	} else if previous.Available && next.Available &&
		previous.CoreBootID != "" && next.CoreBootID != "" &&
		previous.CoreBootID != next.CoreBootID {
		transition = "system.core.recovered"
		reason = "boot_id_changed"
		next.LastTransitionAt = next.CheckedAt
	} else {
		next.LastTransitionAt = previous.LastTransitionAt
	}
	s.coreStatus = next
	s.coreStatusMu.Unlock()

	if transition == "" || !s.shouldRecordCoreSystemAudit() {
		return
	}
	s.recordCoreSystemAudit(ctx, transition, reason, previous, next)
	if transition == "system.core.recovered" {
		s.recordRecoveredCollectors(ctx, next.CoreBootID)
	}
}

func (s *Server) shouldRecordCoreSystemAudit() bool {
	return s.leader == nil || s.leader.IsLeader()
}

func (s *Server) recordCoreSystemAudit(
	ctx context.Context,
	action string,
	reason string,
	previous coreRuntimeStatus,
	next coreRuntimeStatus,
) {
	if s.audit == nil {
		return
	}
	detail, _ := json.Marshal(map[string]any{
		"previous_boot_id": previous.CoreBootID,
		"core_boot_id":     next.CoreBootID,
		"active_rooms":     next.ActiveRooms,
	})
	entry := model.AdminAuditLog{
		ActorUsername: "system",
		ActorRole:     "system",
		ActorType:     "system",
		Source:        "management_core_watch",
		Action:        action,
		ObjectType:    "system",
		ObjectID:      "core-service",
		ObjectName:    "Core 服务",
		Reason:        reason,
		CoreBootID:    next.CoreBootID,
		DetailJSON:    string(detail),
		Result:        "success",
	}
	if action == "system.core.offline" {
		entry.BeforeState = `{"core_available":true}`
		entry.AfterState = `{"core_available":false}`
	} else if reason == "boot_id_changed" {
		entry.BeforeState = fmt.Sprintf(`{"core_available":true,"boot_id":%q}`, previous.CoreBootID)
		entry.AfterState = fmt.Sprintf(`{"core_available":true,"boot_id":%q}`, next.CoreBootID)
	} else {
		entry.BeforeState = `{"core_available":false}`
		entry.AfterState = `{"core_available":true}`
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		log.Printf("record core status audit action=%s: %v", action, err)
	}
}

func (s *Server) recordRecoveredCollectors(ctx context.Context, bootID string) {
	if s.audit == nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := s.core.DoAny(probeCtx, http.MethodGet, "/internal/v1/rooms", nil, nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return
	}
	var payload struct {
		Items []struct {
			ID             int64  `json:"id"`
			TenantID       int64  `json:"tenant_id"`
			Name           string `json:"name"`
			Status         string `json:"status"`
			MonitorEnabled bool   `json:"monitor_enabled"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return
	}
	for _, room := range payload.Items {
		if !room.MonitorEnabled {
			continue
		}
		after, _ := json.Marshal(map[string]any{
			"monitor_enabled": true,
			"status":          room.Status,
		})
		if err := s.audit.Record(ctx, model.AdminAuditLog{
			ActorUsername:  "system",
			ActorRole:      "system",
			ActorType:      "system",
			Source:         "management_core_watch",
			Action:         "room.monitor.auto_recovered",
			TargetTenantID: room.TenantID,
			TargetRoomID:   room.ID,
			ObjectType:     "room",
			ObjectID:       fmt.Sprintf("%d", room.ID),
			ObjectName:     room.Name,
			Reason:         "core_recovered",
			BeforeState:    `{"core_available":false}`,
			AfterState:     string(after),
			CoreBootID:     bootID,
			Result:         "success",
		}); err != nil {
			log.Printf("record collector recovery audit room=%d: %v", room.ID, err)
		}
	}
}
