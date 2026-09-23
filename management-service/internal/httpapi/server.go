package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	auditlog "livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/mailer"
	"livecompanion/management/internal/model"
)

type Server struct {
	store        *appdb.Store
	auth         *auth.Resolver
	core         *coreclient.Client
	audit        *auditlog.Store
	mailer       *mailer.Client
	env          string
	avatarDir    string
	publicWebURL string
}

func New(
	store *appdb.Store,
	authResolver *auth.Resolver,
	core *coreclient.Client,
	auditStore *auditlog.Store,
	mailerClient *mailer.Client,
	env string,
	avatarDir string,
	publicWebURL string,
) *Server {
	return &Server{
		store:        store,
		auth:         authResolver,
		core:         core,
		audit:        auditStore,
		mailer:       mailerClient,
		env:          env,
		avatarDir:    avatarDir,
		publicWebURL: strings.TrimRight(publicWebURL, "/"),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)

	mux.HandleFunc("GET /api/v1/auth/captcha", s.authCaptcha)
	mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/v1/auth/register", s.authRegister)
	mux.HandleFunc("GET /api/v1/auth/invite/{code}", s.authInvitePreview)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	mux.HandleFunc("POST /api/v1/auth/change-password", s.authChangePassword)
	mux.HandleFunc("GET /api/v1/auth/sessions", s.authSessions)
	mux.HandleFunc("POST /api/v1/auth/sessions/logout-others", s.authLogoutOtherSessions)

	mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/v1/invitations/dashboard", s.invitationDashboard)
	mux.HandleFunc("PATCH /api/v1/invitations/mine", s.updateOwnInviteCodeStatus)
	mux.HandleFunc("GET /api/v1/resources", s.currentResourceDashboard)

	mux.HandleFunc("GET /api/v1/staff/dashboard", s.staffDashboard)
	mux.HandleFunc("GET /api/v1/staff/finance", s.staffFinanceOverview)
	mux.HandleFunc("GET /api/v1/staff/finance/customers/{tenantID}", s.staffFinanceCustomerDashboard)
	mux.HandleFunc("POST /api/v1/staff/finance/recharge", s.staffFinanceCreateRecharge)
	mux.HandleFunc("POST /api/v1/staff/finance/refund", s.staffFinanceCreateRefund)
	mux.HandleFunc("POST /api/v1/staff/finance/reward", s.staffFinanceCreateReward)
	mux.HandleFunc("POST /api/v1/staff/finance/approvals/{taskID}/approve", s.staffFinanceApproveTask)
	mux.HandleFunc("POST /api/v1/staff/finance/approvals/{taskID}/reject", s.staffFinanceRejectTask)
	mux.HandleFunc("POST /api/v1/staff/groups", s.staffCreateGroup)
	mux.HandleFunc("PATCH /api/v1/staff/groups/{groupID}", s.staffUpdateGroup)
	mux.HandleFunc("POST /api/v1/staff/roles", s.staffCreateRole)
	mux.HandleFunc("PUT /api/v1/staff/roles/{roleID}", s.staffUpdateRole)
	mux.HandleFunc("POST /api/v1/staff/employees", s.staffCreateEmployee)
	mux.HandleFunc("POST /api/v1/staff/employees/{employeeID}/disable", s.staffDisableEmployee)
	mux.HandleFunc("PUT /api/v1/staff/employees/{employeeID}/roles", s.staffReplaceEmployeeRoles)
	mux.HandleFunc("PATCH /api/v1/staff/approval-policies/{policyID}", s.staffUpdateApprovalPolicy)

	mux.HandleFunc("GET /api/v1/account", s.accountDashboard)
	mux.HandleFunc("PATCH /api/v1/account/profile", s.accountUpdateProfile)
	mux.HandleFunc("POST /api/v1/account/avatar", s.accountUploadAvatar)
	mux.HandleFunc("GET /api/v1/account/avatar/{file}", s.accountAvatarFile)
	mux.HandleFunc("GET /api/v1/finance/dashboard", s.financeDashboard)
	mux.HandleFunc("POST /api/v1/finance/recharge-request", s.financeCreateRechargeRequest)

	mux.HandleFunc("GET /api/v1/admin/customers", s.adminListCustomers)
	mux.HandleFunc("PUT /api/v1/admin/customers/{tenantID}/sales-assignment", s.adminAssignCustomerSales)
	mux.HandleFunc("GET /api/v1/admin/customers/{tenantID}/resources", s.adminCustomerResourceDashboard)
	mux.HandleFunc("POST /api/v1/admin/customers/{tenantID}/resources/adjust", s.adminAdjustCustomerResource)
	mux.HandleFunc("POST /api/v1/admin/customers/{userID}/reset-password", s.adminResetCustomerPassword)
	mux.HandleFunc("GET /api/v1/admin/agents", s.adminListAgents)
	mux.HandleFunc("GET /api/v1/admin/agents/{organizationID}/exit-check", s.adminAgentExitCheck)
	mux.HandleFunc("GET /api/v1/admin/agents/{organizationID}/exit-history", s.adminAgentExitHistory)
	mux.HandleFunc("POST /api/v1/admin/agents/{organizationID}/finalize-exit", s.adminFinalizeAgentExit)
	mux.HandleFunc("POST /api/v1/admin/agents", s.adminCreateAgent)
	mux.HandleFunc("GET /api/v1/admin/agent-levels", s.adminAgentLevels)
	mux.HandleFunc("POST /api/v1/admin/agent-levels", s.adminCreateAgentLevel)
	mux.HandleFunc("PUT /api/v1/admin/agent-levels/{levelID}", s.adminUpdateAgentLevel)
	mux.HandleFunc("POST /api/v1/admin/agents/{organizationID}/level-assignments", s.adminAssignAgentLevel)
	mux.HandleFunc("GET /api/v1/admin/agent-contracts", s.adminAgentContracts)
	mux.HandleFunc("POST /api/v1/admin/agent-contracts", s.adminCreateAgentContract)
	mux.HandleFunc("PUT /api/v1/admin/agent-contracts/{contractID}", s.adminUpdateAgentContract)
	mux.HandleFunc("POST /api/v1/admin/agent-contracts/{contractID}/transition", s.adminTransitionAgentContract)
	mux.HandleFunc("POST /api/v1/admin/agent-contracts/{contractID}/attachments", s.adminUploadAgentContractAttachments)
	mux.HandleFunc("DELETE /api/v1/admin/agent-contracts/{contractID}/attachments/{attachmentID}", s.adminDeleteAgentContractAttachment)
	mux.HandleFunc("GET /api/v1/admin/agent-contract-files/{file}", s.adminAgentContractFile)
	mux.HandleFunc("GET /api/v1/admin/sales", s.adminListSalesStaff)
	mux.HandleFunc("GET /api/v1/admin/sales/performance", s.adminSalesPerformance)
	mux.HandleFunc("POST /api/v1/admin/sales", s.adminCreateSalesStaff)
	mux.HandleFunc("GET /api/v1/admin/agents/{orgID}/resources", s.adminAgentResourceDashboard)
	mux.HandleFunc("POST /api/v1/admin/agents/{orgID}/resources/adjust", s.adminAdjustAgentResource)
	mux.HandleFunc("GET /api/v1/admin/audit-logs", s.adminListAuditLogs)
	mux.HandleFunc("PATCH /api/v1/admin/invitations/{codeID}", s.adminUpdateInviteCodeStatus)

	mux.HandleFunc("GET /api/v1/agent/customers", s.agentListCustomers)
	mux.HandleFunc("POST /api/v1/agent/customers", s.agentCreateCustomer)
	mux.HandleFunc("GET /api/v1/sales/customers", s.salesListCustomers)
	mux.HandleFunc("GET /api/v1/agent/customers/{tenantID}/resources", s.agentCustomerResourceDashboard)
	mux.HandleFunc("POST /api/v1/agent/customers/{tenantID}/resources/allocate", s.agentAllocateCustomerResource)

	mux.HandleFunc("GET /api/v1/commercial/memberships", s.commercialListMemberships)
	mux.HandleFunc("POST /api/v1/commercial/memberships", s.commercialCreateMembership)
	mux.HandleFunc("PUT /api/v1/commercial/memberships/{planID}/draft", s.commercialSaveMembershipDraft)
	mux.HandleFunc("POST /api/v1/commercial/memberships/{planID}/publish", s.commercialPublishMembership)
	mux.HandleFunc("GET /api/v1/commercial/time-cards", s.commercialListTimeCards)
	mux.HandleFunc("POST /api/v1/commercial/time-cards", s.commercialCreateTimeCard)
	mux.HandleFunc("PUT /api/v1/commercial/time-cards/{productID}/draft", s.commercialSaveTimeCardDraft)
	mux.HandleFunc("POST /api/v1/commercial/time-cards/{productID}/publish", s.commercialPublishTimeCard)
	mux.HandleFunc("GET /api/v1/commercial/device-products", s.commercialListDeviceProducts)
	mux.HandleFunc("POST /api/v1/commercial/device-products", s.commercialCreateDeviceProduct)
	mux.HandleFunc("PUT /api/v1/commercial/device-products/{productID}/draft", s.commercialSaveDeviceProductDraft)
	mux.HandleFunc("POST /api/v1/commercial/device-products/{productID}/publish", s.commercialPublishDeviceProduct)
	mux.HandleFunc("GET /api/v1/commercial/incentives", s.commercialListIncentivePrograms)
	mux.HandleFunc("POST /api/v1/commercial/incentives", s.commercialCreateIncentiveProgram)
	mux.HandleFunc("PUT /api/v1/commercial/incentives/{programID}/draft", s.commercialSaveIncentiveDraft)
	mux.HandleFunc("POST /api/v1/commercial/incentives/{programID}/publish", s.commercialPublishIncentive)
	mux.HandleFunc("GET /api/v1/finance/settlements", s.financeSettlementDashboard)
	mux.HandleFunc("POST /api/v1/finance/settlements/batches", s.financeCreateSettlementBatch)
	mux.HandleFunc("POST /api/v1/finance/settlements/batches/{batchID}/approve", s.financeApproveSettlementBatch)
	mux.HandleFunc("POST /api/v1/finance/settlements/batches/{batchID}/reject", s.financeRejectSettlementBatch)
	mux.HandleFunc("POST /api/v1/finance/settlements/batches/{batchID}/pay", s.financePaySettlementBatch)
	mux.HandleFunc("GET /api/v1/finance/operating", s.financeOperatingOverview)
	mux.HandleFunc("POST /api/v1/finance/token-purchases", s.financeCreateTokenPurchase)
	mux.HandleFunc("GET /api/v1/shop/time-cards", s.customerShopTimeCards)
	mux.HandleFunc("GET /api/v1/shop/devices", s.customerShopDevices)
	mux.HandleFunc("GET /api/v1/shop/orders", s.customerShopListOrders)
	mux.HandleFunc("POST /api/v1/shop/orders", s.customerShopCreateOrder)
	mux.HandleFunc("GET /api/v1/shop/orders/{orderID}", s.customerShopGetOrder)
	mux.HandleFunc("POST /api/v1/shop/orders/{orderID}/sandbox-pay", s.customerShopSandboxPayOrder)
	mux.HandleFunc("POST /api/v1/shop/orders/{orderID}/sandbox-refund", s.customerShopSandboxRefundOrder)
	mux.HandleFunc("POST /api/v1/shop/orders/{orderID}/cancel", s.customerShopCancelOrder)
	mux.HandleFunc("GET /api/v1/after-sales/requests", s.afterSalesListRequests)
	mux.HandleFunc("POST /api/v1/after-sales/requests", s.afterSalesCreateRequest)
	mux.HandleFunc("GET /api/v1/after-sales/requests/{rmaID}/events", s.afterSalesRequestEvents)
	mux.HandleFunc("GET /api/v1/inventory/warehouses", s.inventoryListWarehouses)
	mux.HandleFunc("GET /api/v1/inventory/device-products", s.inventoryListDeviceProducts)
	mux.HandleFunc("GET /api/v1/inventory/agents", s.inventoryListAgents)
	mux.HandleFunc("GET /api/v1/inventory/devices", s.inventoryListDevices)
	mux.HandleFunc("POST /api/v1/inventory/devices", s.inventoryCreateDevice)
	mux.HandleFunc("POST /api/v1/inventory/inbounds", s.inventoryBatchInbound)
	mux.HandleFunc("POST /api/v1/inventory/devices/{deviceID}/transition", s.inventoryTransitionDevice)
	mux.HandleFunc("POST /api/v1/inventory/devices/{deviceID}/scrap-dispose", s.inventoryDisposeScrapDevice)
	mux.HandleFunc("GET /api/v1/inventory/devices/{deviceID}/ledger", s.inventoryDeviceLedger)
	mux.HandleFunc("GET /api/v1/inventory/ledger", s.inventoryAllLedger)
	mux.HandleFunc("GET /api/v1/inventory/summary", s.inventorySummary)
	mux.HandleFunc("GET /api/v1/inventory/documents", s.inventoryDocuments)
	mux.HandleFunc("GET /api/v1/inventory/rmas", s.inventoryListRMAs)
	mux.HandleFunc("POST /api/v1/inventory/rmas", s.inventoryCreateRMA)
	mux.HandleFunc("POST /api/v1/inventory/rmas/{rmaID}/accept", s.inventoryAcceptRMA)
	mux.HandleFunc("POST /api/v1/inventory/rmas/{rmaID}/start-repair", s.inventoryStartRMARepair)
	mux.HandleFunc("GET /api/v1/inventory/rmas/{rmaID}/events", s.inventoryRMAEvents)
	mux.HandleFunc("GET /api/v1/inventory/rmas/{rmaID}/costs", s.inventoryListRMACosts)
	mux.HandleFunc("POST /api/v1/inventory/rmas/{rmaID}/costs", s.inventoryCreateRMACost)
	mux.HandleFunc("POST /api/v1/inventory/rmas/{rmaID}/complete", s.inventoryCompleteRMA)
	mux.HandleFunc("GET /api/v1/logistics/shipments", s.logisticsListShipments)
	mux.HandleFunc("POST /api/v1/logistics/shipments", s.logisticsCreateShipment)
	mux.HandleFunc("POST /api/v1/logistics/shipments/{shipmentID}/status", s.logisticsUpdateShipmentStatus)
	mux.HandleFunc("GET /api/v1/admin/features/{featureKey}", s.adminListFeatureRecords)
	mux.HandleFunc("POST /api/v1/admin/features/{featureKey}", s.adminCreateFeatureRecord)
	mux.HandleFunc("PUT /api/v1/admin/features/{featureKey}/{recordID}", s.adminUpdateFeatureRecord)
	mux.HandleFunc("DELETE /api/v1/admin/features/{featureKey}/{recordID}", s.adminDeleteFeatureRecord)

	mux.HandleFunc("GET /api/v1/rooms", s.listRooms)
	mux.HandleFunc("POST /api/v1/rooms", s.createRoom)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}", s.getRoom)
	mux.HandleFunc("DELETE /api/v1/rooms/{roomID}", s.deleteRoom)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/events", s.listEvents)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/stream", s.streamEvents)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/preview", s.previewRoom)
	mux.HandleFunc("GET /api/v1/rooms/{roomID}/live/{file}", s.liveMedia)

	return requestLogger(s.adminAuditMiddleware(mux))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "management-service",
		"status":  "ok",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	var tenants []model.Tenant
	var err error

	var staffAccess *model.StaffAccessContext

	if actor.IsInternalStaff() {
		access, accessErr := s.store.GetStaffAccess(r.Context(), actor.UserID)
		if accessErr == nil {
			staffAccess = &access
		}
	}

	if actor.IsPlatformAdmin() {
		tenants, err = s.store.ListTenants(r.Context())
	} else if actor.TenantID != nil {
		var tenant model.Tenant
		tenant, err = s.store.GetTenant(r.Context(), *actor.TenantID)
		if err == nil {
			tenants = []model.Tenant{tenant}
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端信息失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"actor":        actor,
		"staff_access": staffAccess,
		"tenants":      tenants,
		"environment":  s.env,
	})
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	query, ok := s.roomScopeQuery(w, r, actor)
	if !ok {
		return
	}

	resp, err := s.core.Do(r.Context(), http.MethodGet, "/internal/v1/rooms", query, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端账号可创建直播间")
		return
	}

	var input model.CreateRoomRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	input.ExternalRoomID = strings.TrimSpace(input.ExternalRoomID)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Name = strings.TrimSpace(input.Name)
	input.CollectorMode = strings.TrimSpace(input.CollectorMode)

	if input.Platform == "" {
		input.Platform = "douyin"
	}

	if input.Platform == "douyin" {
		externalRoomID, sourceURL, err := normalizeDouyinRoomInput(input.ExternalRoomID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		input.ExternalRoomID = externalRoomID
		if input.SourceURL == "" {
			input.SourceURL = sourceURL
		}
	}

	if input.ExternalRoomID == "" {
		writeError(w, http.StatusBadRequest, "请输入直播间房间号")
		return
	}

	if input.CollectorMode == "" {
		input.CollectorMode = "auto"
	}

	if actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "当前账号没有终端范围")
		return
	}
	input.TenantID = *actor.TenantID

	roomLimit, err := s.store.GetOrganizationResourceBalance(
		r.Context(),
		input.TenantID,
		"room_slots",
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间额度失败")
		return
	}
	roomCount, err := s.store.GetTenantRoomCount(r.Context(), input.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前直播间数量失败")
		return
	}
	if roomCount >= roomLimit {
		writeError(
			w,
			http.StatusConflict,
			"直播间额度不足，请联系平台或所属代理增加直播间额度",
		)
		return
	}

	resp, err := s.core.Do(r.Context(), http.MethodPost, "/internal/v1/rooms", nil, input)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	query := scopeForActor(actor)
	resp, err := s.core.Do(
		r.Context(),
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) deleteRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端账号可删除直播间")
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	resp, err := s.core.Do(
		r.Context(),
		http.MethodDelete,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		scopeForActor(actor),
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	query := scopeForActor(actor)
	if limit := strings.TrimSpace(r.URL.Query().Get("limit")); limit != "" {
		query.Set("limit", limit)
	}

	resp, err := s.core.Do(
		r.Context(),
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/events", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) liveMedia(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	fileName := strings.TrimSpace(r.PathValue("file"))
	if fileName == "" {
		writeError(w, http.StatusBadRequest, "媒体文件不能为空")
		return
	}

	resp, err := s.core.Do(
		r.Context(),
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/live/%s", roomID, fileName),
		scopeForActor(actor),
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "直播画面暂不可用")
		return
	}

	if strings.HasSuffix(fileName, ".m3u8") {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=2")
	}
	s.copyCoreResponse(w, resp)
}
func (s *Server) previewRoom(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	resp, err := s.core.Do(
		r.Context(),
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/preview", roomID),
		scopeForActor(actor),
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}

	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	s.copyCoreResponse(w, resp)
}
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	resp, err := s.core.Stream(
		r.Context(),
		fmt.Sprintf("/internal/v1/rooms/%d/stream", roomID),
		scopeForActor(actor),
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务实时流暂不可用")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.copyCoreResponse(w, resp)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务不支持实时流")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	buffer := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			return
		}
	}
}

func (s *Server) setDevActor(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var input struct {
		Role     string `json:"role"`
		TenantID int64  `json:"tenant_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	if err := s.auth.SetDevelopmentActor(
		r.Context(),
		w,
		strings.TrimSpace(input.Role),
		input.TenantID,
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createDevEvent(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}

	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}

	var input map[string]any
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))

	resp, err := s.core.Do(
		r.Context(),
		http.MethodPost,
		fmt.Sprintf("/internal/v1/dev/rooms/%d/events", roomID),
		query,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) tenantForRoom(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
	roomID int64,
) (int64, bool) {
	if !actor.IsPlatformAdmin() {
		if actor.TenantID == nil {
			writeError(w, http.StatusForbidden, "当前账号没有终端范围")
			return 0, false
		}
		return *actor.TenantID, true
	}

	resp, err := s.core.Do(
		r.Context(),
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		nil,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "核心服务暂不可用")
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		copyResponse(w, resp)
		return 0, false
	}

	var room struct {
		TenantID int64 `json:"tenant_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&room); err != nil || room.TenantID <= 0 {
		writeError(w, http.StatusBadGateway, "核心服务返回了无效房间数据")
		return 0, false
	}
	return room.TenantID, true
}

func (s *Server) roomScopeQuery(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
) (url.Values, bool) {
	hasSystemScope := actor.IsPlatformAdmin()
	if !hasSystemScope && actor.IsInternalStaff() {
		access, err := s.store.GetStaffAccess(r.Context(), actor.UserID)
		if err == nil && staffHasPermission(access, "system.architecture.view") {
			hasSystemScope = true
		}
	}

	if !hasSystemScope {
		return scopeForActor(actor), true
	}

	query := url.Values{}
	rawTenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if rawTenantID == "" {
		return query, true
	}

	tenantID, err := strconv.ParseInt(rawTenantID, 10, 64)
	if err != nil || tenantID <= 0 {
		writeError(w, http.StatusBadRequest, "无效的终端 ID")
		return nil, false
	}
	if _, err := s.store.GetTenant(r.Context(), tenantID); err != nil {
		writeError(w, http.StatusBadRequest, "终端不存在")
		return nil, false
	}

	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	return query, true
}
func scopeForActor(actor model.Actor) url.Values {
	query := url.Values{}
	if !actor.IsPlatformAdmin() && actor.TenantID != nil {
		query.Set("tenant_id", strconv.FormatInt(*actor.TenantID, 10))
	}
	return query
}

func normalizeDouyinRoomInput(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}

	const baseURL = "https://live.douyin.com/"

	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		roomID := strings.Trim(strings.TrimSpace(value), "/")
		if roomID == "" {
			return "", "", fmt.Errorf("请输入直播间链接或房间号")
		}
		return roomID, baseURL + roomID + "?from=web_code_link", nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", "", fmt.Errorf("直播间链接格式不正确")
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "live.douyin.com" {
		return "", "", fmt.Errorf("当前仅支持 live.douyin.com 直播链接或直播间房间号")
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return "", "", fmt.Errorf("直播间链接里没有房间号")
	}

	roomID := strings.Split(path, "/")[0]
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return "", "", fmt.Errorf("直播间链接里没有房间号")
	}

	query := parsed.Query()
	if query.Get("from") == "" {
		query.Set("from", "web_code_link")
	}
	parsed.RawQuery = query.Encode()

	return roomID, parsed.String(), nil
}
func (s *Server) resolveActor(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, err := s.auth.Resolve(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) copyCoreResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	copyResponse(w, resp)
}

func copyResponse(w http.ResponseWriter, resp *http.Response) {
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(r.PathValue("roomID"), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "无效的直播间 ID")
		return 0, false
	}
	return value, true
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		fmt.Printf("[management] %s %s %s\n", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
