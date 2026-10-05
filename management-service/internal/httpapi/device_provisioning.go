package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type deviceClaimWindow struct {
	count int
	until time.Time
}
type deviceClaimLimiter struct {
	mu      sync.Mutex
	windows map[int64]deviceClaimWindow
	global  deviceClaimWindow
}

func (l *deviceClaimLimiter) allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.windows == nil {
		l.windows = make(map[int64]deviceClaimWindow)
	}
	for id, w := range l.windows {
		if now.After(w.until) {
			delete(l.windows, id)
		}
	}
	if now.After(l.global.until) {
		l.global = deviceClaimWindow{until: now.Add(time.Minute)}
	}
	w := l.windows[userID]
	if w.until.IsZero() {
		w.until = now.Add(time.Minute)
	}
	if w.count >= 5 || l.global.count >= 100 {
		return false
	}
	w.count++
	l.global.count++
	l.windows[userID] = w
	return true
}

func (s *Server) SetXiaozhiInternalToken(token string) {
	s.xiaozhiInternalToken = strings.TrimSpace(token)
}
func (s *Server) registerDeviceProvisioningRoutes(mux *http.ServeMux) {
	s.registerDeviceBusinessRoutes(mux)
	mux.HandleFunc("POST /internal/v1/xiaozhi/provision", s.hardwareProvision)
	mux.HandleFunc("POST /internal/v1/xiaozhi/heartbeat", s.hardwareHeartbeat)
	mux.HandleFunc("GET /api/v1/live/devices/default-name", s.liveDeviceDefaultName)
	mux.HandleFunc("POST /api/v1/live/devices/claim", s.liveClaimDevice)
	mux.HandleFunc("PATCH /api/v1/live/devices/{deviceID}", s.liveRenameDevice)
	mux.HandleFunc("DELETE /api/v1/live/devices/{deviceID}/bind", s.liveUnbindDevice)
	mux.HandleFunc("PUT /api/v1/inventory/devices/{deviceID}/hardware", s.inventoryConfigureHardware)
	mux.HandleFunc("POST /api/v1/inventory/devices/{deviceID}/release-ownership", s.inventoryReleaseOwnership)
}
func (s *Server) authorizeHardware(w http.ResponseWriter, r *http.Request) bool {
	if s.xiaozhiInternalToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Xiaozhi-Internal-Token")), []byte(s.xiaozhiInternalToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "未授权的设备网关")
		return false
	}
	return true
}
func (s *Server) hardwareProvision(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	var input struct {
		HardwareMAC string `json:"hardware_mac"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if _, err := model.NormalizeHardwareMAC(input.HardwareMAC); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	out, err := s.store.ProvisionDevice(r.Context(), input.HardwareMAC, s.xiaozhiInternalToken)
	if err != nil {
		writeError(w, 500, "读取设备登记状态失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}
func (s *Server) hardwareHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHardware(w, r) {
		return
	}
	var input struct {
		HardwareMAC string `json:"hardware_mac"`
		RoomID      int64  `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	item, err := s.store.HardwareHeartbeat(r.Context(), input.HardwareMAC, input.RoomID)
	if err != nil {
		s.deviceOperationError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) deviceCustomer(w http.ResponseWriter, r *http.Request) (model.Actor, int64, bool) {
	a, ok := s.resolveActor(w, r)
	if !ok {
		return a, 0, false
	}
	if a.Role != "customer" || a.TenantID == nil || *a.TenantID <= 0 {
		writeError(w, 403, "请使用客户账号添加设备")
		return a, 0, false
	}
	return a, *a.TenantID, true
}
func (s *Server) liveDeviceDefaultName(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	name, err := s.store.NextDeviceName(r.Context(), tenant)
	if err != nil {
		writeError(w, 500, "读取默认设备名称失败")
		return
	}
	writeJSON(w, 200, map[string]string{"device_name": name})
}
func (s *Server) liveClaimDevice(w http.ResponseWriter, r *http.Request) {
	a, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	if s.xiaozhiInternalToken == "" {
		writeError(w, 503, "设备绑定服务暂未启用")
		return
	}
	if !s.deviceClaims.allow(a.UserID) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "尝试次数过多，请稍后再试")
		return
	}
	var input model.ClaimDeviceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	item, err := s.store.ClaimDevice(r.Context(), tenant, a.UserID, input, s.xiaozhiInternalToken)
	if err != nil {
		s.deviceOperationError(w, err)
		return
	}
	writeJSON(w, 201, item)
}
func (s *Server) liveRenameDevice(w http.ResponseWriter, r *http.Request) {
	a, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	var input struct {
		DeviceName string `json:"device_name"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	item, err := s.store.RenameDevice(r.Context(), tenant, a.UserID, id, input.DeviceName)
	if err != nil {
		s.deviceOperationError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) liveUnbindDevice(w http.ResponseWriter, r *http.Request) {
	a, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	item, err := s.store.UnbindLiveDevice(r.Context(), tenant, a.UserID, id)
	if err != nil {
		s.deviceOperationError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) inventoryConfigureHardware(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	var input struct {
		HardwareMAC  string `json:"hardware_mac"`
		ClaimEnabled bool   `json:"claim_enabled"`
		Reason       string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(input.Reason) == "" || utf8.RuneCountInString(input.Reason) > 255 {
		writeError(w, 400, "请填写登记或激活原因（最多 255 字）")
		return
	}
	if err := s.store.ConfigureDeviceHardware(r.Context(), a.UserID, id, input.HardwareMAC, input.ClaimEnabled, input.Reason); err != nil {
		s.deviceOperationError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) inventoryReleaseOwnership(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.requireStaffPermission(w, r, "inventory.after_sales.manage")
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > 255 {
		writeError(w, 400, "请填写售后释放原因（最多 255 字）")
		return
	}
	if err := s.store.ReleaseDeviceOwnership(r.Context(), a.UserID, id, reason); err != nil {
		s.deviceOperationError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deviceOperationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, 404, "设备不存在或不属于当前客户")
	case errors.Is(err, appdb.ErrLiveBindingBusy):
		writeError(w, 409, "设备或直播间正在工作，请先停止直播任务")
	case errors.Is(err, appdb.ErrDeviceClaimCode), errors.Is(err, appdb.ErrDeviceClaimUnavailable):
		writeError(w, 400, err.Error())
	case errors.Is(err, appdb.ErrDeviceNameUsed), isDuplicateDBError(err):
		writeError(w, 409, "设备名称、SN 或 MAC 已使用")
	default:
		writeError(w, 400, "设备操作失败，请检查输入或联系售后")
	}
}
