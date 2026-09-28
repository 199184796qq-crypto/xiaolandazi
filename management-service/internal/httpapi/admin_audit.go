package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

type auditStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditStatusWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func (s *Server) adminAuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldAuditRequest(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		actor, err := s.auth.Resolve(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		entry := model.AdminAuditLog{
			ActorUserID:   actor.UserID,
			ActorUsername: actor.Username,
			ActorRole:     actor.Role,
			ActorType:     "user",
			Source:        auditSourceForActor(actor),
			Action:        auditAction(r.Method, r.URL.Path),
			HTTPMethod:    r.Method,
			Path:          r.URL.Path,
			ClientIP:      requestClientIP(r),
			RequestID:     strings.TrimSpace(r.Header.Get("X-Request-ID")),
			Result:        "started",
		}
		enrichAuditFromPayload(readAuditJSONPayload(r), &entry)
		s.fillAuditTarget(r, &entry)

		pendingID, err := s.audit.Begin(r.Context(), entry)
		if err != nil {
			writeError(
				w,
				http.StatusServiceUnavailable,
				"审计日志服务暂不可用，管理操作已阻止",
			)
			return
		}

		recorder := &auditStatusWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}
		next.ServeHTTP(recorder, r)

		entry.Result = "http_" + strconv.Itoa(recorder.status)
		if recorder.status >= 200 && recorder.status < 300 {
			applyAuditSuccessState(&entry)
		}
		if err := s.audit.Complete(
			r.Context(),
			pendingID,
			entry,
		); err != nil {
			log.Printf(
				"admin audit finalize failed action=%s actor=%s error=%v",
				entry.Action,
				entry.ActorUsername,
				err,
			)
		}
	})
}

func auditSourceForActor(actor model.Actor) string {
	switch actor.Role {
	case "customer":
		return "customer_web"
	case "agent_admin":
		return "agent_web"
	case "platform_admin", "staff", "sales_staff":
		return "internal_web"
	default:
		return "web"
	}
}

func readAuditJSONPayload(r *http.Request) map[string]any {
	if r == nil || r.Body == nil || !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return nil
	}
	const maxAuditBody = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAuditBody+1))
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if len(raw) == 0 || len(raw) > maxAuditBody {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return payload
}

func enrichAuditFromPayload(payload map[string]any, entry *model.AdminAuditLog) {
	if entry == nil || payload == nil {
		return
	}
	if reason, ok := payload["reason"].(string); ok {
		entry.Reason = strings.TrimSpace(reason)
	}
	if entry.Action == "room.monitor.update" {
		if enabled, ok := payload["enabled"].(bool); ok {
			if enabled {
				entry.Action = "room.monitor.connect"
				entry.AfterState = `{"monitor_enabled":true}`
			} else {
				entry.Action = "room.monitor.disconnect"
				entry.AfterState = `{"monitor_enabled":false}`
			}
		}
	}
	if entry.Action == "room.create" {
		if name, ok := payload["name"].(string); ok {
			entry.ObjectType = "room"
			entry.ObjectName = strings.TrimSpace(name)
		}
		if tenant, ok := payload["tenant_id"].(float64); ok && tenant > 0 {
			entry.TargetTenantID = int64(tenant)
		}
	}
	if entry.Action == "device.bind_room" {
		if roomID, ok := payload["room_id"].(float64); ok && roomID > 0 {
			entry.TargetRoomID = int64(roomID)
		}
		if role, ok := payload["binding_role"].(string); ok {
			entry.DetailJSON = mustAuditJSON(map[string]any{"binding_role": strings.TrimSpace(role)})
		}
	}
	if entry.Action == "agent.plan.bind_room" {
		if roomID, ok := payload["room_id"].(float64); ok && roomID > 0 {
			entry.TargetRoomID = int64(roomID)
		}
		if tenantID, ok := payload["tenant_id"].(float64); ok && tenantID > 0 {
			entry.TargetTenantID = int64(tenantID)
		}
	}
	if entry.Action == "device.control" {
		if roomID, ok := payload["room_id"].(float64); ok && roomID > 0 {
			entry.TargetRoomID = int64(roomID)
		}
		if action, ok := payload["action"].(string); ok {
			entry.Reason = strings.TrimSpace(action)
		}
	}
}

func mustAuditJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func applyAuditSuccessState(entry *model.AdminAuditLog) {
	if entry == nil || entry.AfterState != "" {
		return
	}
	switch entry.Action {
	case "room.delete", "room.delete_non_cooperating":
		entry.AfterState = `{"exists":false}`
	case "agent.runtime.start", "agent.runtime.resume":
		entry.AfterState = `{"agent_state":"working"}`
	case "agent.runtime.pause", "agent.runtime.stop":
		entry.AfterState = `{"agent_state":"stopped"}`
	}
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func shouldAuditRequest(method string, path string) bool {
	if !isMutationMethod(method) {
		return false
	}
	switch {
	case strings.HasSuffix(path, "/heartbeat"):
		return false
	case strings.HasSuffix(path, "/runtime/events"):
		return false
	case strings.HasSuffix(path, "/agent-learning/intent"):
		return false
	case strings.HasSuffix(path, "/agent-learning/chat"):
		return false
	case strings.HasSuffix(path, "/agent/chat"):
		return false
	case strings.Contains(path, "/agent-learning/sessions/") && strings.HasSuffix(path, "/turns"):
		return false
	case strings.Contains(path, "/agent-learning/sessions/") && strings.HasSuffix(path, "/test"):
		return false
	case strings.Contains(path, "/agent-decisions/"):
		return false
	case strings.HasSuffix(path, "/policy-agent/chat"):
		return false
	case strings.HasSuffix(path, "/policies/admin/agent/chat"):
		return false
	default:
		return true
	}
}

func auditAction(method string, path string) string {
	if isMutationMethod(method) {
		switch {
		case strings.HasPrefix(path, "/api/v1/customer-business/receipts"):
			return "finance.customer_receipt.mutate"
		case strings.HasPrefix(path, "/api/v1/service/tickets"):
			return "service.ticket.mutate"
		case path == "/api/v1/sales/leads":
			return "sales.lead.create"
		case strings.HasPrefix(path, "/api/v1/sales/leads/"):
			if strings.HasSuffix(path, "/convert") {
				return "sales.lead.convert"
			}
			if strings.HasSuffix(path, "/lost") {
				return "sales.lead.lost"
			}
			if strings.HasSuffix(path, "/activities") {
				return "sales.lead.activity"
			}
			return "sales.lead.update"
		case strings.HasPrefix(path, "/api/v1/admin/sales/") && strings.HasSuffix(path, "/handover"):
			return "sales.portfolio.handover"
		case strings.HasPrefix(path, "/api/v1/liveops/customer-handoffs/"):
			return "liveops.handoff.update"
		}
	}
	switch {
	case method == http.MethodPost &&
		path == "/api/v1/auth/change-password":
		return "admin.change_password"
	case method == http.MethodPost &&
		path == "/api/v1/auth/logout":
		return "admin.logout"
	case method == http.MethodPut &&
		path == "/api/v1/system/agent-routing/draft":
		return "system.agent_routing.draft_save"
	case method == http.MethodPost &&
		path == "/api/v1/system/agent-routing/publish":
		return "system.agent_routing.publish"
	case method == http.MethodPost &&
		path == "/api/v1/system/agent-routing/rollback":
		return "system.agent_routing.rollback"
	case method == http.MethodPost &&
		path == "/api/v1/system/agent-routing/assist":
		return "system.agent_routing.assist"
	case method == http.MethodPut &&
		path == "/api/v1/system/agent-understanding/policy":
		return "system.agent_understanding.policy_save"
	case method == http.MethodDelete &&
		strings.HasPrefix(path, "/api/v1/system/agent-understanding/policy/"):
		return "system.agent_understanding.policy_delete"
	case method == http.MethodPost &&
		path == "/api/v1/rooms":
		return "room.create"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/monitor"):
		return "room.monitor.update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/runtime/start"):
		return "agent.runtime.start"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/runtime/pause"):
		return "agent.runtime.pause"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/runtime/resume"):
		return "agent.runtime.resume"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/runtime/stop"):
		return "agent.runtime.stop"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/runtime/mode"):
		return "agent.runtime.mode_update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/runtime/plan"):
		return "agent.runtime.plan_update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/rooms/") &&
		strings.Contains(path, "/policy/versions/") &&
		strings.HasSuffix(path, "/publish"):
		return "strategy.publish"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/rooms/") &&
		strings.Contains(path, "/policy/versions/") &&
		strings.HasSuffix(path, "/rollback"):
		return "strategy.rollback"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/rooms/") &&
		strings.HasSuffix(path, "/agent-learning/sessions"):
		return "agent.learning.session.create"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/rooms/") &&
		strings.Contains(path, "/agent-learning/sessions/") &&
		strings.HasSuffix(path, "/adopt"):
		return "agent.learning.adopt"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/rooms/") &&
		strings.Contains(path, "/agent-memories/") &&
		strings.HasSuffix(path, "/deactivate"):
		return "agent.memory.deactivate"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/rooms/") &&
		strings.Contains(path, "/agent-memories/") &&
		strings.Contains(path, "/versions/") &&
		strings.HasSuffix(path, "/rollback"):
		return "agent.memory.rollback"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live-agent-plans/") &&
		strings.Contains(path, "/scripts/") &&
		strings.HasSuffix(path, "/analyze"):
		return "agent.plan.script.analyze"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/live-agent-plans/") &&
		!strings.Contains(path, "/scripts/"):
		return "agent.plan.update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live-agent-plans/") &&
		strings.HasSuffix(path, "/scripts"):
		return "agent.plan.script.create"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/live-agent-plans/") &&
		strings.Contains(path, "/scripts/"):
		return "agent.plan.script.update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live-agent-plans/") &&
		strings.HasSuffix(path, "/room-bindings"):
		return "agent.plan.bind_room"
	case method == http.MethodDelete &&
		strings.HasPrefix(path, "/api/v1/live-agent-plans/") &&
		strings.Contains(path, "/room-bindings/"):
		return "agent.plan.unbind_room"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/devices/") &&
		strings.HasSuffix(path, "/bind"):
		return "device.bind_room"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/live/devices/") &&
		strings.HasSuffix(path, "/control"):
		return "device.control"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/capture/audio/start"):
		return "room.recording.start"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/rooms/") &&
		strings.HasSuffix(path, "/capture/audio/stop"):
		return "room.recording.stop"
	case method == http.MethodDelete &&
		isDirectRoomPath(path):
		return "room.delete"
	case method == http.MethodPost &&
		path == "/api/v1/commercial/memberships":
		return "commercial.membership.create"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/commercial/memberships/") &&
		strings.HasSuffix(path, "/draft"):
		return "commercial.membership.draft_save"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/commercial/memberships/") &&
		strings.HasSuffix(path, "/publish"):
		return "commercial.membership.publish"
	case method == http.MethodPost &&
		path == "/api/v1/admin/agents":
		return "agent.create"
	case method == http.MethodPost &&
		path == "/api/v1/agent/customers":
		return "agent.customer.create"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/admin/invitations/"):
		return "invitation.status_update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/agents/") &&
		strings.HasSuffix(path, "/resources/adjust"):
		return "agent.resource_adjust"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/customers/") &&
		strings.HasSuffix(path, "/resources/adjust"):
		return "customer.resource_adjust"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/agent/customers/") &&
		strings.HasSuffix(path, "/resources/allocate"):
		return "agent.customer.resource_allocate"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/admin/customers/") &&
		strings.HasSuffix(path, "/cooperation"):
		return "customer.cooperation_update"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/admin/customers/") &&
		strings.HasSuffix(path, "/reset-password"):
		return "customer.password_reset"
	case method == http.MethodDelete &&
		strings.HasPrefix(path, "/api/v1/admin/customers/"):
		return "customer.delete"
	case method == http.MethodPost &&
		path == "/api/v1/staff/groups":
		return "staff.group.create"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/staff/groups/"):
		return "staff.group.update"
	case method == http.MethodPost &&
		path == "/api/v1/staff/roles":
		return "staff.role.create"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/staff/roles/"):
		return "staff.role.update"
	case method == http.MethodPost &&
		path == "/api/v1/staff/employees":
		return "staff.employee.create"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/employees/") &&
		strings.HasSuffix(path, "/disable"):
		return "staff.employee.disable"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/employees/") &&
		strings.HasSuffix(path, "/reset-password"):
		return "staff.employee.password_reset"
	case method == http.MethodPut &&
		strings.HasPrefix(path, "/api/v1/staff/employees/") &&
		strings.HasSuffix(path, "/roles"):
		return "staff.employee.role_update"
	case method == http.MethodPatch &&
		strings.HasPrefix(path, "/api/v1/staff/approval-policies/"):
		return "staff.approval_policy.update"
	case method == http.MethodPost &&
		path == "/api/v1/commercial/ai-time/requests":
		return "commercial.ai_time.request"
	case method == http.MethodPost &&
		path == "/api/v1/staff/finance/recharge":
		return "finance.recharge.create"
	case method == http.MethodPost &&
		path == "/api/v1/staff/finance/refund":
		return "finance.refund.create"
	case method == http.MethodPost &&
		path == "/api/v1/staff/finance/reward":
		return "finance.reward.grant"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/finance/approvals/") &&
		strings.HasSuffix(path, "/approve"):
		return "finance.approval.approve"
	case method == http.MethodPost &&
		strings.HasPrefix(path, "/api/v1/staff/finance/approvals/") &&
		strings.HasSuffix(path, "/reject"):
		return "finance.approval.reject"
	default:
		return method + " " + path
	}
}

func isDirectRoomPath(path string) bool {
	if !strings.HasPrefix(path, "/api/v1/rooms/") {
		return false
	}
	rest := strings.Trim(strings.TrimPrefix(path, "/api/v1/rooms/"), "/")
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	_, err := strconv.ParseInt(rest, 10, 64)
	return err == nil
}

func (s *Server) fillAuditTarget(
	r *http.Request,
	entry *model.AdminAuditLog,
) {
	roomID := positivePathValue(r, "roomID")
	if roomID <= 0 {
		roomID = entry.TargetRoomID
	}
	if roomID > 0 {
		entry.TargetRoomID = roomID
		if entry.ObjectType == "" {
			entry.ObjectType = "room"
			entry.ObjectID = strconv.FormatInt(roomID, 10)
		}
		if actor, resolveErr := s.auth.Resolve(r); resolveErr == nil && actor.TenantID != nil {
			entry.TargetTenantID = *actor.TenantID
		}
		resp, coreErr := s.core.Do(
			r.Context(),
			http.MethodGet,
			"/internal/v1/rooms/"+strconv.FormatInt(roomID, 10),
			nil,
			nil,
		)
		if coreErr == nil && resp != nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var room struct {
					TenantID       int64  `json:"tenant_id"`
					Name           string `json:"name"`
					Status         string `json:"status"`
					MonitorEnabled bool   `json:"monitor_enabled"`
				}
				if json.NewDecoder(resp.Body).Decode(&room) == nil {
					entry.TargetTenantID = room.TenantID
					entry.ObjectName = room.Name
					state, _ := json.Marshal(map[string]any{
						"status":          room.Status,
						"monitor_enabled": room.MonitorEnabled,
					})
					entry.BeforeState = string(state)
				}
			}
		}
		if strings.HasPrefix(entry.Action, "agent.runtime.") && entry.TargetTenantID > 0 {
			query := url.Values{}
			query.Set("tenant_id", strconv.FormatInt(entry.TargetTenantID, 10))
			runtimeResp, runtimeErr := s.core.DoRoom(
				r.Context(),
				entry.TargetTenantID,
				roomID,
				http.MethodGet,
				"/internal/v1/rooms/"+strconv.FormatInt(roomID, 10)+"/agent-runtime",
				query,
				nil,
			)
			if runtimeErr == nil && runtimeResp != nil {
				defer runtimeResp.Body.Close()
				if runtimeResp.StatusCode == http.StatusOK {
					var runtime struct {
						BootID                string `json:"boot_id"`
						State                 string `json:"state"`
						StopReason            string `json:"stop_reason"`
						WorkingSeconds        uint64 `json:"working_seconds"`
						LeaseRemainingSeconds uint64 `json:"lease_remaining_seconds"`
					}
					if json.NewDecoder(runtimeResp.Body).Decode(&runtime) == nil {
						entry.CoreBootID = runtime.BootID
						state, _ := json.Marshal(map[string]any{
							"agent_state":             runtime.State,
							"stop_reason":             runtime.StopReason,
							"working_seconds":         runtime.WorkingSeconds,
							"lease_remaining_seconds": runtime.LeaseRemainingSeconds,
						})
						entry.BeforeState = string(state)
					}
				}
			}
			if session, sessionErr := s.store.GetLiveRuntimeByRoom(r.Context(), entry.TargetTenantID, roomID); sessionErr == nil {
				entry.RuntimeSessionID = session.ID
			}
		}
		if entry.Action == "room.delete" {
			if actor, resolveErr := s.auth.Resolve(r); resolveErr == nil && actor.Role != "customer" {
				entry.Reason = "customer_non_cooperating"
			}
		}
	}
	if versionID := positivePathValue(r, "versionID"); versionID > 0 {
		entry.ObjectType = "strategy_version"
		entry.ObjectID = strconv.FormatInt(versionID, 10)
	}
	if sessionID := positivePathValue(r, "sessionID"); sessionID > 0 && strings.HasPrefix(entry.Action, "agent.learning.") {
		entry.ObjectType = "agent_learning_session"
		entry.ObjectID = strconv.FormatInt(sessionID, 10)
	}
	if memoryID := positivePathValue(r, "memoryID"); memoryID > 0 {
		entry.ObjectType = "agent_memory"
		entry.ObjectID = strconv.FormatInt(memoryID, 10)
	}
	if planID := positivePathValue(r, "planID"); planID > 0 {
		entry.ObjectType = "agent_plan"
		entry.ObjectID = strconv.FormatInt(planID, 10)
	}
	if deviceID := positivePathValue(r, "deviceID"); deviceID > 0 {
		entry.ObjectType = "device"
		entry.ObjectID = strconv.FormatInt(deviceID, 10)
	}
	switch entry.Action {
	case "strategy.publish":
		entry.AfterState = mustAuditJSON(map[string]any{"strategy_status": "published", "version_id": positivePathValue(r, "versionID")})
	case "strategy.rollback":
		entry.AfterState = mustAuditJSON(map[string]any{"strategy_status": "rolled_back", "version_id": positivePathValue(r, "versionID")})
	case "agent.memory.deactivate":
		entry.AfterState = `{"memory_active":false}`
	case "agent.memory.rollback":
		entry.AfterState = mustAuditJSON(map[string]any{"memory_version_id": positivePathValue(r, "versionID")})
	case "agent.plan.bind_room":
		entry.AfterState = mustAuditJSON(map[string]any{"bound": true, "room_id": entry.TargetRoomID})
	case "agent.plan.unbind_room":
		entry.AfterState = mustAuditJSON(map[string]any{"bound": false, "room_id": entry.TargetRoomID})
	case "device.bind_room":
		entry.AfterState = mustAuditJSON(map[string]any{"bound_room_id": entry.TargetRoomID})
	}
	if !strings.HasPrefix(
		r.URL.Path,
		"/api/v1/admin/customers/",
	) {
		return
	}

	userID := positivePathValue(r, "userID")
	var err error
	if userID <= 0 {
		tenantID := positivePathValue(r, "tenantID")
		if tenantID <= 0 {
			return
		}
		userID, err = s.store.GetCustomerUserIDByTenantID(r.Context(), tenantID)
		if err != nil {
			return
		}
	}

	entry.TargetUserID = userID
	customer, err := s.store.GetAdminCustomer(
		r.Context(),
		userID,
	)
	if err != nil {
		return
	}
	entry.TargetUsername = customer.Username
	entry.TargetTenantID = customer.TenantID
}

func positivePathValue(r *http.Request, key string) int64 {
	if r == nil {
		return 0
	}
	raw := strings.TrimSpace(r.PathValue(key))
	if raw == "" {
		raw = auditPathValue(r.URL.Path, key)
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func auditPathValue(path string, key string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	after := func(label string) string {
		for index := 0; index+1 < len(parts); index++ {
			if parts[index] == label {
				return parts[index+1]
			}
		}
		return ""
	}
	switch key {
	case "roomID":
		for index := 0; index+1 < len(parts); index++ {
			if parts[index] == "rooms" {
				return parts[index+1]
			}
			if parts[index] == "room-bindings" {
				return parts[index+1]
			}
		}
	case "versionID":
		return after("versions")
	case "sessionID":
		return after("sessions")
	case "memoryID":
		return after("agent-memories")
	case "planID":
		return after("live-agent-plans")
	case "deviceID":
		return after("devices")
	case "userID":
		for index := 0; index+1 < len(parts); index++ {
			if parts[index] == "customers" && parts[index+1] != "by-tenant" {
				return parts[index+1]
			}
		}
	case "tenantID":
		for index := 0; index+2 < len(parts); index++ {
			if parts[index] == "customers" && parts[index+1] == "by-tenant" {
				return parts[index+2]
			}
		}
	}
	return ""
}

func requestClientIP(r *http.Request) string {
	host := r.RemoteAddr
	if parsedHost, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = parsedHost
	}
	return host
}
