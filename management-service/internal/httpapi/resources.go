package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	appdb "livecompanion/management/internal/db"
)

type adjustResourceRequest struct {
	ResourceType string `json:"resource_type"`
	Delta        int64  `json:"delta"`
	Reason       string `json:"reason"`
}

type allocateResourceRequest struct {
	ResourceType string `json:"resource_type"`
	Quantity     int64  `json:"quantity"`
	Reason       string `json:"reason"`
}

func (s *Server) currentResourceDashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.TenantID == nil ||
		(actor.Role != "agent_admin" && actor.Role != "customer") {
		writeError(w, http.StatusForbidden, "当前账号没有组织资源账户")
		return
	}

	item, err := s.store.GetResourceDashboard(r.Context(), *actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资源账户失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminAgentResourceDashboard(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.requireAnyStaffPermission(
		w,
		r,
		"finance.resource.view",
		"finance.resource.adjust",
		"commercial.ai_time.view",
	)
	if !ok {
		return
	}

	orgID, ok := parsePositivePathID(w, r.PathValue("orgID"))
	if !ok {
		return
	}

	item, err := s.store.GetResourceDashboard(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "代理组织不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取代理资源账户失败")
		return
	}
	if item.OrgType != "agent" {
		writeError(w, http.StatusBadRequest, "目标组织不是代理")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminAdjustAgentResource(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.resource.adjust")
	if !ok {
		return
	}

	orgID, ok := parsePositivePathID(w, r.PathValue("orgID"))
	if !ok {
		return
	}

	var input adjustResourceRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ResourceType == "" || input.Delta == 0 {
		writeError(w, http.StatusBadRequest, "资源类型和调整数量不能为空")
		return
	}
	if input.ResourceType != "ai_seconds" {
		writeError(w, http.StatusBadRequest, "财务仅允许调整 AI 时长")
		return
	}
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "资源调整原因不能为空")
		return
	}

	err := s.store.AdjustOrganizationResource(
		r.Context(),
		orgID,
		input.ResourceType,
		input.Delta,
		actor.UserID,
		input.Reason,
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrInsufficientResource):
			writeError(w, http.StatusConflict, "扣减后资源余额不能小于 0")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "代理组织不存在")
		default:
			writeError(w, http.StatusBadRequest, "调整资源失败："+err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentCustomerResourceDashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgent(w, r)
	if !ok {
		return
	}

	customerOrgID, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}

	belongs, err := s.store.IsDirectCustomerOfAgent(
		r.Context(),
		customerOrgID,
		*actor.TenantID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验终端归属失败")
		return
	}
	if !belongs {
		writeError(w, http.StatusForbidden, "只能查看当前代理名下终端资源")
		return
	}

	item, err := s.store.GetResourceDashboard(r.Context(), customerOrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端资源账户失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) agentAllocateCustomerResource(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgent(w, r)
	if !ok {
		return
	}

	customerOrgID, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}

	var input allocateResourceRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ResourceType == "" || input.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "资源类型和分配数量无效")
		return
	}
	if input.ResourceType != "ai_seconds" {
		writeError(w, http.StatusBadRequest, "代理仅允许向终端分配 AI 时长")
		return
	}
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "资源分配原因不能为空")
		return
	}

	err := s.store.TransferOrganizationResource(
		r.Context(),
		*actor.TenantID,
		customerOrgID,
		input.ResourceType,
		input.Quantity,
		actor.UserID,
		input.Reason,
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrInsufficientResource):
			writeError(w, http.StatusConflict, "代理资源余额不足")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusForbidden, "只能给当前代理名下终端分配资源")
		default:
			writeError(w, http.StatusBadRequest, "分配资源失败："+err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parsePositivePathID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, false
	}
	return id, true
}
func (s *Server) adminCustomerResourceDashboard(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.requireAnyStaffPermission(
		w,
		r,
		"finance.resource.view",
		"finance.resource.adjust",
		"commercial.ai_time.view",
	)
	if !ok {
		return
	}

	orgID, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}

	item, err := s.store.GetResourceDashboard(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "终端组织不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取终端资源账户失败")
		return
	}
	if item.OrgType != "customer" {
		writeError(w, http.StatusBadRequest, "目标组织不是终端")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminAdjustCustomerResource(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.resource.adjust")
	if !ok {
		return
	}

	orgID, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}

	var input adjustResourceRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ResourceType == "" || input.Delta == 0 {
		writeError(w, http.StatusBadRequest, "资源类型和调整数量不能为空")
		return
	}
	if input.ResourceType != "ai_seconds" {
		writeError(w, http.StatusBadRequest, "财务仅允许调整 AI 时长")
		return
	}
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "资源调整原因不能为空")
		return
	}

	item, err := s.store.GetResourceDashboard(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "终端组织不存在")
		return
	}
	if item.OrgType != "customer" {
		writeError(w, http.StatusBadRequest, "目标组织不是终端")
		return
	}

	err = s.store.AdjustOrganizationResource(
		r.Context(),
		orgID,
		input.ResourceType,
		input.Delta,
		actor.UserID,
		input.Reason,
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrInsufficientResource):
			writeError(w, http.StatusConflict, "扣减后资源余额不能小于 0")
		default:
			writeError(w, http.StatusBadRequest, "调整终端资源失败："+err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
