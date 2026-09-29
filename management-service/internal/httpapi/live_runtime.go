package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type coreRoomRuntimeState struct {
	ID           int64  `json:"id"`
	TenantID     int64  `json:"tenant_id"`
	Status       string `json:"status"`
	DeviceOnline bool   `json:"device_online"`
}

type coreAgentRuntimeState struct {
	BootID                string     `json:"boot_id"`
	RoomID                int64      `json:"room_id"`
	State                 string     `json:"state"`
	StopReason            string     `json:"stop_reason"`
	Mode                  string     `json:"mode"`
	PlanID                int64      `json:"plan_id"`
	PlanName              string     `json:"plan_name"`
	HotRevision           uint64     `json:"hot_revision"`
	HotModules            []string   `json:"hot_modules,omitempty"`
	HotUpdatedAt          *time.Time `json:"hot_updated_at,omitempty"`
	WorkingSeconds        uint64     `json:"working_seconds"`
	LeaseRemainingSeconds uint64     `json:"lease_remaining_seconds"`
	LeaseRenewalDue       bool       `json:"lease_renewal_due"`
	LeaseUntil            *time.Time `json:"lease_until,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type coreSessionRuntimeState struct {
	ResumePending bool `json:"resume_pending"`
}

func (s *Server) liveAgentSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	item, err := s.store.GetLiveAgentSettings(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取场控Agent基础设置失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveUpdateAgentSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	var input model.LiveAgentSettingsInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "基础设置格式错误")
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.RoleName = strings.TrimSpace(input.RoleName)
	input.SelfIntroduction = strings.TrimSpace(input.SelfIntroduction)
	input.Mission = strings.TrimSpace(input.Mission)
	input.Greeting = strings.TrimSpace(input.Greeting)
	if utf8.RuneCountInString(input.DisplayName) > 128 ||
		utf8.RuneCountInString(input.RoleName) > 160 ||
		utf8.RuneCountInString(input.SelfIntroduction) > 600 ||
		utf8.RuneCountInString(input.Mission) > 1200 ||
		utf8.RuneCountInString(input.Greeting) > 600 {
		writeError(w, http.StatusBadRequest, "基础设置内容过长")
		return
	}
	item, err := s.store.UpsertLiveAgentSettings(
		r.Context(), tenantID, actor.UserID, input,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存场控Agent基础设置失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveListDevices(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID := int64(0)
	if actor.TenantID != nil {
		tenantID = *actor.TenantID
	} else if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "当前账号没有终端客户范围")
		return
	}
	items, err := s.store.ListLiveDevices(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备绑定状态失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) liveBindDevice(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	deviceID, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}

	var input model.BindLiveDeviceInput
	if err := readJSON(w, r, &input); err != nil || input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择要绑定的直播间")
		return
	}

	tenantID := int64(0)
	if actor.TenantID != nil {
		tenantID = *actor.TenantID
	} else {
		var tenantOK bool
		tenantID, tenantOK = s.tenantForRoom(w, r, actor, input.RoomID)
		if !tenantOK {
			return
		}
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, input.RoomID); err != nil {
		writeError(w, http.StatusBadRequest, "直播间不存在或不属于当前客户")
		return
	}

	item, err := s.store.BindLiveDevice(
		r.Context(),
		tenantID,
		actor.UserID,
		deviceID,
		input.RoomID,
		input.BindingRole,
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrLiveBindingBusy):
			writeError(w, http.StatusConflict, "设备或直播间正在工作，请先停止AI直播任务再更换绑定")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备不存在或不属于当前客户")
		default:
			writeError(w, http.StatusInternalServerError, "绑定设备失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveDeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	deviceID, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}

	var input model.DeviceHeartbeatInput
	if r.ContentLength > 0 {
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "设备心跳格式错误")
			return
		}
	}

	item, err := s.store.HeartbeatLiveDevice(
		r.Context(),
		tenantID,
		deviceID,
		input.RoomID,
		input.Metadata,
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrLiveDeviceNotBound):
			writeError(w, http.StatusConflict, "设备没有绑定到该直播间")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备不存在或不属于当前客户")
		default:
			writeError(w, http.StatusInternalServerError, "设备心跳写入失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveDeviceControl(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	deviceID, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	var input model.DeviceControlInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "设备控制参数格式错误")
		return
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	if input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "缺少直播间")
		return
	}
	switch input.Action {
	case "connect", "pause", "resume", "disconnect":
	default:
		writeError(w, http.StatusBadRequest, "不支持的设备控制动作")
		return
	}
	item, err := s.store.ControlLiveDevice(
		r.Context(), tenantID, actor.UserID, deviceID, input.RoomID, input.Action,
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrLiveDeviceNotBound):
			writeError(w, http.StatusConflict, "设备没有绑定到该直播间")
		case errors.Is(err, appdb.ErrLiveDeviceOffline):
			writeError(w, http.StatusConflict, "设备当前未连接，请先连接")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备不存在或不属于当前客户")
		default:
			writeError(w, http.StatusInternalServerError, "设备控制失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveQuotaSummary(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端用户可查看终端 AI 剩余时长")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	quotaSummary, err := s.store.GetLiveQuotaSummary(r.Context(), tenantID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取AI时长失败")
		return
	}
	billingRooms, err := s.store.ListTenantLiveBillingRooms(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正在扣费的直播间失败")
		return
	}
	quotaSummary.ActiveBillingRooms = make([]model.LiveBillingRoomSummary, 0, len(billingRooms))
	for _, room := range billingRooms {
		agentRuntime, runtimeErr := s.getCoreAgentState(r.Context(), tenantID, room.RoomID)
		if runtimeErr != nil {
			continue
		}
		if agentRuntime.State != "working" {
			continue
		}
		quotaSummary.ActiveBillingRooms = append(quotaSummary.ActiveBillingRooms, room)
	}
	writeJSON(w, http.StatusOK, quotaSummary)
}

func (s *Server) liveTimeCardAssets(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端用户可查看时长卡包")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	page := 1
	pageSize := 5
	if value := strings.TrimSpace(r.URL.Query().Get("page")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("page_size")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	items, err := s.store.ListLiveTimeCardAssets(r.Context(), tenantID, page, pageSize, time.Now().UTC())
	if err != nil {
		log.Printf("live time card pack list failed tenant=%d page=%d page_size=%d: %v", tenantID, page, pageSize, err)
		writeError(w, http.StatusInternalServerError, "读取时长卡包失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) liveTimeCardActivate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端用户可启用时长卡")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	assetID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("assetID")), 10, 64)
	if err != nil || assetID <= 0 {
		writeError(w, http.StatusBadRequest, "时长卡编号无效")
		return
	}
	summary, err := s.store.ActivateLiveTimeCardAsset(r.Context(), tenantID, assetID, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrTimeCardNotActivatable):
			writeError(w, http.StatusConflict, "这张时长卡当前不能启用，可能已启用、已过期或已失效")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "时长卡不存在")
		default:
			writeError(w, http.StatusInternalServerError, "启用时长卡失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset_id": assetID,
		"quota":    summary,
	})
}

func (s *Server) liveQuotaActivateCards(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端用户可启用时长卡")
		return
	}
	writeError(w, http.StatusConflict, "批量自动启用已停用，请到“AI 时长 → 时长卡包”选择具体卡片使用")
}

func (s *Server) liveRuntimeStatus(w http.ResponseWriter, r *http.Request) {
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

	room, err := s.getCoreRoomState(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法读取直播间状态")
		return
	}
	agentRuntime, err := s.getCoreAgentState(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法读取智能体真实运行状态")
		return
	}
	// Core is the authority for paid runtime state. A Core restart intentionally
	// resets the paid Agent/TTS stack to stopped; reading this status endpoint
	// must never restart paid work from a stale database session.
	var persistedSession *model.LiveRuntimeSession
	if session, sessionErr := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID); sessionErr == nil {
		persistedSession = &session
	} else if !errors.Is(sessionErr, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取AI运行状态失败")
		return
	}
	if persistedPlan, planErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID); planErr == nil {
		if agentRuntime.PlanID != persistedPlan.ID || strings.TrimSpace(agentRuntime.PlanName) != strings.TrimSpace(persistedPlan.Name) {
			synced, syncErr := s.setCoreAgentPlan(r.Context(), tenantID, roomID, persistedPlan.ID, persistedPlan.Name)
			if syncErr != nil {
				writeError(w, http.StatusBadGateway, "无法同步智能体直播方案到核心运行态")
				return
			}
			agentRuntime = synced
		}
	} else if errors.Is(planErr, appdb.ErrLiveAgentPlanNotFound) {
		if agentRuntime.PlanID != 0 {
			synced, syncErr := s.setCoreAgentPlan(r.Context(), tenantID, roomID, 0, "")
			if syncErr != nil {
				writeError(w, http.StatusBadGateway, "无法清理核心运行态中的智能体直播方案")
				return
			}
			agentRuntime = synced
		}
	} else {
		writeError(w, http.StatusInternalServerError, "读取智能体直播方案失败")
		return
	}
	quotaSummary, err := s.store.GetLiveQuotaSummary(r.Context(), tenantID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取AI时长失败")
		return
	}

	snapshot := model.LiveRuntimeSnapshot{
		AgentState:             agentRuntime.State,
		AgentMode:              agentRuntime.Mode,
		AgentPlanID:            agentRuntime.PlanID,
		AgentPlanName:          agentRuntime.PlanName,
		AgentWorkingSeconds:    agentRuntime.WorkingSeconds,
		QuotaRemainingSeconds:  quotaSummary.ActiveSeconds,
		ReserveTimeCardSeconds: quotaSummary.ReserveTimeCardSeconds,
		ReserveTimeCardCount:   quotaSummary.ReserveTimeCardCount,
		CurrentQuota:           quotaSummary.Current,
		TimeCards:              quotaSummary.TimeCards,
		RoomLive:               room.Status == "live",
	}
	if persistedSession != nil {
		snapshot.Session = persistedSession
		if persistedSession.Status == "paused" {
			snapshot.AgentState = "paused"
		}
		if (persistedSession.Status == "running" || persistedSession.Status == "paused") && persistedSession.DeviceID != nil {
			if device, deviceErr := s.store.GetLiveDevice(r.Context(), tenantID, *persistedSession.DeviceID); deviceErr == nil {
				snapshot.Device = &device
			}
		}
	}
	if snapshot.Device == nil {
		if device, deviceErr := s.store.GetBoundLiveDeviceByRoom(r.Context(), tenantID, roomID); deviceErr == nil {
			snapshot.Device = &device
		} else if !errors.Is(deviceErr, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "读取直播间绑定设备失败")
			return
		}
	}

	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) liveRuntimeMode(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		Mode string `json:"mode"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "模式参数格式错误")
		return
	}
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode != "control" && input.Mode != "anchor" {
		writeError(w, http.StatusBadRequest, "mode 只允许 control 或 anchor")
		return
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前客户")
		return
	}
	if input.Mode == "anchor" {
		if _, planErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID); errors.Is(planErr, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusConflict, "主播模式需要先选择智能体直播方案")
			return
		} else if planErr != nil {
			writeError(w, http.StatusInternalServerError, "读取智能体直播方案失败")
			return
		}
	}
	state, err := s.setCoreAgentMode(r.Context(), tenantID, roomID, input.Mode)
	if err != nil {
		writeError(w, http.StatusBadGateway, "切换直播搭子模式失败")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) liveRuntimePlan(w http.ResponseWriter, r *http.Request) {
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
	var input struct {
		PlanID int64 `json:"plan_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "方案参数格式错误")
		return
	}
	if input.PlanID <= 0 {
		writeError(w, http.StatusBadRequest, "plan_id 必须大于 0")
		return
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前客户")
		return
	}
	plan, err := s.store.SelectLiveAgentPlanForRoom(r.Context(), tenantID, input.PlanID, roomID, actor.UserID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusConflict, "这个方案还没有绑定到当前直播间，请先绑定后再切换使用")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "切换智能体直播方案失败")
		return
	}
	state, err := s.setCoreAgentPlan(r.Context(), tenantID, roomID, plan.ID, plan.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "同步智能体直播方案到核心运行态失败")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) liveRuntimeStart(w http.ResponseWriter, r *http.Request) {
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

	var input model.StartLiveRuntimeInput
	if r.ContentLength > 0 {
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "启动参数格式错误")
			return
		}
	}

	room, err := s.getCoreRoomState(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法读取直播间状态")
		return
	}
	if room.Status != "live" {
		writeError(w, http.StatusConflict, "直播间当前未开播，AI不会开始计时")
		return
	}
	if sessionState, sessionErr := s.getCoreSessionState(r.Context(), tenantID, roomID); sessionErr != nil {
		writeError(w, http.StatusBadGateway, "无法读取直播续接状态")
		return
	} else if sessionState.ResumePending {
		writeError(w, http.StatusConflict, "请先选择续接上一场或作为新直播，再启动AI")
		return
	}
	agentRuntime, agentErr := s.getCoreAgentState(r.Context(), tenantID, roomID)
	if agentErr != nil {
		writeError(w, http.StatusBadGateway, "无法读取直播搭子模式")
		return
	}
	var publishedPlanVersion *model.LiveAgentPlanVersion
	if strings.EqualFold(strings.TrimSpace(agentRuntime.Mode), "anchor") {
		selectedPlan, planErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID)
		if errors.Is(planErr, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusConflict, "主播模式需要先选择智能体直播方案")
			return
		} else if planErr != nil {
			writeError(w, http.StatusInternalServerError, "读取智能体直播方案失败")
			return
		}
		published, versionErr := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenantID, roomID)
		if errors.Is(versionErr, appdb.ErrLiveAgentPlanVersionNotFound) {
			writeError(w, http.StatusConflict, "当前直播间还没有已发布的智能体版本，请先在直播策略中保存版本并发布")
			return
		}
		if versionErr != nil {
			writeError(w, http.StatusInternalServerError, "读取已发布直播智能体版本失败")
			return
		}
		if published.PlanID != selectedPlan.ID {
			writeError(w, http.StatusConflict, "当前选择的直播方案与已发布版本不一致，请重新发布当前方案")
			return
		}
		if len(published.Variants) == 0 {
			writeError(w, http.StatusConflict, "已发布直播智能体版本没有可播放的正式声音")
			return
		}
		publishedPlanVersion = &published
	}

	if existing, existingErr := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID); existingErr == nil {
		switch existing.Status {
		case "running":
			if agentRuntime.State == "working" && agentRuntime.BootID != "" && agentRuntime.LeaseRemainingSeconds > 0 {
				writeJSON(w, http.StatusOK, existing)
				return
			}
			// Core restart/lease expiry leaves a stale durable session. An explicit
			// Start click closes it without charging the unfinished lease, then starts fresh.
			if _, abortErr := s.store.AbortLiveRuntimeSession(r.Context(), existing.ID, "core_runtime_reset", time.Now().UTC()); abortErr != nil {
				writeError(w, http.StatusInternalServerError, "清理旧AI运行状态失败")
				return
			}
		case "paused":
			// "开始" 与 "继续" 是两套语义：明确点“开始”时丢弃暂停游标，
			// 重新创建运行会话并从当前发布版本的第一稿开头播放。
			if strings.EqualFold(strings.TrimSpace(agentRuntime.Mode), "anchor") {
				_ = s.requestCorePublishedPlanProgramControl(r.Context(), tenantID, roomID, "stop")
			}
			if _, abortErr := s.store.AbortLiveRuntimeSession(r.Context(), existing.ID, "manual_restart", time.Now().UTC()); abortErr != nil {
				writeError(w, http.StatusInternalServerError, "清理暂停中的旧AI运行状态失败")
				return
			}
		}
	} else if !errors.Is(existingErr, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取AI运行状态失败")
		return
	}

	session, err := s.store.StartLiveRuntimeSession(
		r.Context(),
		tenantID,
		roomID,
		actor.UserID,
		input.DeviceID,
		time.Now().UTC(),
	)
	if err != nil {
		switch {
		case errors.Is(err, appdb.ErrLiveRuntimeAlreadyRunning):
			writeError(w, http.StatusConflict, "当前直播间AI已经在工作")
		case errors.Is(err, appdb.ErrLiveQuotaExhausted):
			writeError(w, http.StatusPaymentRequired, "AI时长已用完，请先充值时长")
		case errors.Is(err, appdb.ErrLiveDeviceNotBound):
			writeError(w, http.StatusConflict, "请先给直播间绑定工作设备")
		default:
			writeError(w, http.StatusInternalServerError, "启动AI直播伴播失败")
		}
		return
	}

	if industryCode, l1, l2, l3, policyErr := s.store.LoadLivePolicyLayers(
		r.Context(), tenantID, roomID,
	); policyErr != nil {
		log.Printf(
			"live runtime policy load failed tenant=%d room=%d session=%d: %v",
			tenantID, roomID, session.ID, policyErr,
		)
	} else {
		effectivePolicy := policy.BuildEffective(industryCode, l1, l2, l3)
		if snapshotErr := s.store.SaveLiveRuntimePolicySnapshot(
			r.Context(),
			model.LiveRuntimePolicySnapshot{
				SessionID:    session.ID,
				TenantID:     tenantID,
				RoomID:       roomID,
				IndustryCode: industryCode,
				L1VersionID:  policyVersionID(l1),
				L2VersionID:  policyVersionID(l2),
				L3VersionID:  policyVersionID(l3),
				Effective:    effectivePolicy,
			},
		); snapshotErr != nil {
			log.Printf(
				"live runtime policy snapshot failed tenant=%d room=%d session=%d: %v",
				tenantID, roomID, session.ID, snapshotErr,
			)
		}
	}

	leaseSeconds, leaseErr := s.allocateInitialLiveQuotaLease(
		r.Context(), tenantID, roomID, session.ID, agentRuntime.BootID, session.TotalBilledSeconds,
	)
	if leaseErr != nil {
		_, _ = s.store.AbortLiveRuntimeSession(r.Context(), session.ID, "quota_unavailable", time.Now().UTC())
		if errors.Is(leaseErr, appdb.ErrLiveQuotaExhausted) {
			writeError(w, http.StatusPaymentRequired, "AI时长已用完，请先充值时长")
		} else {
			writeError(w, http.StatusInternalServerError, "申请AI运行额度失败")
		}
		return
	}
	if err := s.requestCoreAgentStart(r.Context(), tenantID, roomID, session.TotalBilledSeconds, leaseSeconds); err != nil {
		_, _ = s.store.ReconcileLiveQuotaLeases(r.Context(), tenantID, session.ID, agentRuntime.BootID, session.TotalBilledSeconds, true, false, time.Now().UTC())
		_, _ = s.store.AbortLiveRuntimeSession(r.Context(), session.ID, "core_start_failed", time.Now().UTC())
		writeError(w, http.StatusBadGateway, "智能体启动失败，请稍后重试")
		return
	}
	if publishedPlanVersion != nil {
		if audioErr := s.requestCorePublishedPlanProgramStart(
			r.Context(), tenantID, roomID, session.ID, *publishedPlanVersion,
		); audioErr != nil {
			stopped, _ := s.requestCoreAgentStop(r.Context(), tenantID, roomID, session.TotalBilledSeconds, "system_error")
			_, _ = s.store.ReconcileLiveQuotaLeases(r.Context(), tenantID, session.ID, stopped.BootID, stopped.WorkingSeconds, true, true, time.Now().UTC())
			_, _ = s.store.AbortLiveRuntimeSession(r.Context(), session.ID, "core_audio_start_failed", time.Now().UTC())
			writeError(w, http.StatusBadGateway, "正式声音主线加载失败，请检查已发布声音资源")
			return
		}
	}

	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) liveRuntimePause(w http.ResponseWriter, r *http.Request) {
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

	agentRuntime, agentErr := s.getCoreAgentState(r.Context(), tenantID, roomID)
	if agentErr != nil {
		writeError(w, http.StatusBadGateway, "无法读取智能体真实运行状态")
		return
	}
	existing, err := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusConflict, "当前直播间AI没有在工作")
		return
	}
	if strings.EqualFold(strings.TrimSpace(agentRuntime.Mode), "anchor") {
		if err := s.requestCorePublishedPlanProgramControl(r.Context(), tenantID, roomID, "pause"); err != nil {
			writeError(w, http.StatusBadGateway, "主线声音暂停失败，请稍后重试")
			return
		}
	}
	// Freeze the audio cursor first, then stop paid work and settle the durable
	// session. Billing therefore stops at pause while the mainline millisecond
	// cursor remains available for Continue.
	// Core runtime authoritative and prevents paid output from continuing while
	// Management is writing pause/quota state.
	stoppedRuntime, err := s.requestCoreAgentStop(r.Context(), tenantID, roomID, agentRuntime.WorkingSeconds, "manual_pause")
	if err != nil {
		if strings.EqualFold(strings.TrimSpace(agentRuntime.Mode), "anchor") {
			_ = s.requestCorePublishedPlanProgramControl(r.Context(), tenantID, roomID, "resume")
		}
		writeError(w, http.StatusBadGateway, "智能体暂停准备失败，请稍后重试")
		return
	}
	if _, err := s.store.ReconcileLiveQuotaLeases(
		r.Context(), tenantID, existing.ID, stoppedRuntime.BootID,
		stoppedRuntime.WorkingSeconds, true, true, time.Now().UTC(),
	); err != nil {
		writeError(w, http.StatusInternalServerError, "结算当前AI运行额度失败")
		return
	}
	session, err := s.store.PauseLiveRuntimeSession(
		r.Context(), tenantID, roomID, actor.UserID, time.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, appdb.ErrLiveRuntimeNotRunning) {
			writeError(w, http.StatusConflict, "当前直播间AI没有在工作")
			return
		}
		writeError(w, http.StatusInternalServerError, "暂停AI直播伴播失败")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) liveRuntimeResume(w http.ResponseWriter, r *http.Request) {
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

	room, err := s.getCoreRoomState(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法读取直播间状态")
		return
	}
	if room.Status != "live" {
		writeError(w, http.StatusConflict, "直播间当前未开播，AI不会继续计时")
		return
	}
	if sessionState, sessionErr := s.getCoreSessionState(r.Context(), tenantID, roomID); sessionErr != nil {
		writeError(w, http.StatusBadGateway, "无法读取直播续接状态")
		return
	} else if sessionState.ResumePending {
		writeError(w, http.StatusConflict, "请先选择直播续接方式，再继续AI")
		return
	}

	agentRuntime, agentErr := s.getCoreAgentState(r.Context(), tenantID, roomID)
	if agentErr != nil {
		writeError(w, http.StatusBadGateway, "无法读取智能体真实运行状态")
		return
	}
	existing, existingErr := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID)
	if existingErr != nil || existing.Status != "paused" {
		writeError(w, http.StatusConflict, "当前直播间AI没有处于暂停状态")
		return
	}
	leaseSeconds, leaseErr := s.allocateInitialLiveQuotaLease(
		r.Context(), tenantID, roomID, existing.ID, agentRuntime.BootID, agentRuntime.WorkingSeconds,
	)
	if leaseErr != nil {
		if errors.Is(leaseErr, appdb.ErrLiveQuotaExhausted) {
			writeError(w, http.StatusPaymentRequired, "AI时长已用完，请先充值时长")
		} else {
			writeError(w, http.StatusInternalServerError, "申请AI运行额度失败")
		}
		return
	}
	session, err := s.store.ResumeLiveRuntimeSession(
		r.Context(), tenantID, roomID, actor.UserID, time.Now().UTC(),
	)
	if err != nil {
		_, _ = s.store.ReconcileLiveQuotaLeases(r.Context(), tenantID, existing.ID, agentRuntime.BootID, agentRuntime.WorkingSeconds, true, false, time.Now().UTC())
		switch {
		case errors.Is(err, appdb.ErrLiveRuntimeNotRunning):
			writeError(w, http.StatusConflict, "当前直播间AI没有处于暂停状态")
		case errors.Is(err, appdb.ErrLiveQuotaExhausted):
			writeError(w, http.StatusPaymentRequired, "AI时长已用完，请先充值时长")
		default:
			writeError(w, http.StatusInternalServerError, "继续AI直播伴播失败")
		}
		return
	}
	if err := s.requestCoreAgentStart(r.Context(), tenantID, roomID, session.TotalBilledSeconds, leaseSeconds); err != nil {
		_, _ = s.store.ReconcileLiveQuotaLeases(r.Context(), tenantID, session.ID, agentRuntime.BootID, agentRuntime.WorkingSeconds, true, false, time.Now().UTC())
		_, _ = s.store.PauseLiveRuntimeSession(r.Context(), tenantID, roomID, actor.UserID, time.Now().UTC())
		writeError(w, http.StatusBadGateway, "智能体恢复失败，请稍后重试")
		return
	}
	if strings.EqualFold(strings.TrimSpace(agentRuntime.Mode), "anchor") {
		if audioErr := s.requestCorePublishedPlanProgramControl(r.Context(), tenantID, roomID, "resume"); audioErr != nil {
			stopped, _ := s.requestCoreAgentStop(r.Context(), tenantID, roomID, session.TotalBilledSeconds, "system_error")
			_, _ = s.store.ReconcileLiveQuotaLeases(r.Context(), tenantID, session.ID, stopped.BootID, stopped.WorkingSeconds, true, true, time.Now().UTC())
			_, _ = s.store.PauseLiveRuntimeSession(r.Context(), tenantID, roomID, actor.UserID, time.Now().UTC())
			writeError(w, http.StatusBadGateway, "主线声音恢复失败，请稍后重试")
			return
		}
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) liveRuntimeStop(w http.ResponseWriter, r *http.Request) {
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

	var input model.StopLiveRuntimeInput
	if r.ContentLength > 0 {
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "停止参数格式错误")
			return
		}
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "manual_stop"
	}

	agentRuntime, agentErr := s.getCoreAgentState(r.Context(), tenantID, roomID)
	if agentErr != nil {
		writeError(w, http.StatusBadGateway, "无法读取智能体真实运行状态")
		return
	}
	existing, err := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusConflict, "当前直播间AI没有在工作")
		return
	}
	if strings.EqualFold(strings.TrimSpace(agentRuntime.Mode), "anchor") {
		if audioErr := s.requestCorePublishedPlanProgramControl(r.Context(), tenantID, roomID, "stop"); audioErr != nil {
			log.Printf("live runtime stop mainline room=%d failed: %v", roomID, audioErr)
		}
	}
	stoppedRuntime, err := s.requestCoreAgentStop(r.Context(), tenantID, roomID, agentRuntime.WorkingSeconds, reason)
	if err != nil {
		writeError(w, http.StatusBadGateway, "智能体停止准备失败，请稍后重试")
		return
	}
	if _, err := s.store.ReconcileLiveQuotaLeases(
		r.Context(), tenantID, existing.ID, stoppedRuntime.BootID,
		stoppedRuntime.WorkingSeconds, true, true, time.Now().UTC(),
	); err != nil {
		writeError(w, http.StatusInternalServerError, "结算当前AI运行额度失败")
		return
	}
	session, err := s.store.StopLiveRuntimeSessionWithoutMeter(
		r.Context(), tenantID, roomID, actor.UserID, reason, time.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, appdb.ErrLiveRuntimeNotRunning) {
			writeError(w, http.StatusConflict, "当前直播间AI没有在工作")
			return
		}
		writeError(w, http.StatusInternalServerError, "停止AI直播伴播失败")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (s *Server) liveRuntimeRecordEvent(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前客户")
		return
	}
	var input model.RecordLiveRuntimeEventInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "直播操作记录格式错误")
		return
	}
	item, err := s.store.RecordLiveRuntimeEvent(
		r.Context(), tenantID, roomID, actor.UserID, input,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "不支持的直播操作记录")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveRuntimeEvents(w http.ResponseWriter, r *http.Request) {
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
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	items, err := s.store.ListLiveRuntimeEvents(r.Context(), tenantID, roomID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播操作记录失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func actorTenantID(w http.ResponseWriter, actor model.Actor) (int64, bool) {
	if actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "当前账号没有终端客户范围")
		return 0, false
	}
	return *actor.TenantID, true
}

func namedPathID(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	label string,
) (int64, bool) {
	value, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "无效的"+label+" ID")
		return 0, false
	}
	return value, true
}

func (s *Server) getCoreRoomState(
	ctx context.Context,
	tenantID, roomID int64,
) (coreRoomRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))

	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d", roomID),
		query,
		nil,
	)
	if err != nil {
		return coreRoomRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreRoomRuntimeState{}, fmt.Errorf("core room status %d", resp.StatusCode)
	}

	var room coreRoomRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&room); err != nil {
		return coreRoomRuntimeState{}, err
	}
	if room.TenantID != tenantID {
		return coreRoomRuntimeState{}, sql.ErrNoRows
	}
	return room, nil
}

func (s *Server) getCoreAgentState(
	ctx context.Context,
	tenantID, roomID int64,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
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

func (s *Server) getCoreSessionState(
	ctx context.Context,
	tenantID, roomID int64,
) (coreSessionRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/session-stats", roomID),
		query,
		nil,
	)
	if err != nil {
		return coreSessionRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreSessionRuntimeState{}, fmt.Errorf("core session status %d", resp.StatusCode)
	}
	var state coreSessionRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return coreSessionRuntimeState{}, err
	}
	return state, nil
}

func (s *Server) setCoreAgentMode(
	ctx context.Context,
	tenantID, roomID int64,
	mode string,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query,
		map[string]any{"mode": mode},
	)
	if err != nil {
		return coreAgentRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreAgentRuntimeState{}, fmt.Errorf("core agent mode status %d", resp.StatusCode)
	}
	var state coreAgentRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return coreAgentRuntimeState{}, err
	}
	return state, nil
}

func (s *Server) setCoreAgentPlan(
	ctx context.Context,
	tenantID, roomID, planID int64,
	planName string,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query,
		map[string]any{"plan_id": planID, "plan_name": strings.TrimSpace(planName)},
	)
	if err != nil {
		return coreAgentRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreAgentRuntimeState{}, fmt.Errorf("core agent plan status %d", resp.StatusCode)
	}
	var state coreAgentRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return coreAgentRuntimeState{}, err
	}
	return state, nil
}

func (s *Server) hotReloadCoreAgentRoom(
	ctx context.Context,
	tenantID, roomID int64,
	modules ...string,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query,
		map[string]any{"hot_reload": modules},
	)
	if err != nil {
		return coreAgentRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreAgentRuntimeState{}, fmt.Errorf("core hot reload status %d", resp.StatusCode)
	}
	var state coreAgentRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return coreAgentRuntimeState{}, err
	}
	return state, nil
}

func (s *Server) hotReloadLiveAgentPlanRooms(
	ctx context.Context,
	tenantID, planID int64,
	modules ...string,
) error {
	if s.core == nil || planID <= 0 || len(modules) == 0 {
		return nil
	}
	roomIDs, err := s.store.ListSelectedLiveAgentPlanRoomIDs(ctx, tenantID, planID)
	if err != nil {
		return err
	}
	for _, roomID := range roomIDs {
		state, reloadErr := s.hotReloadCoreAgentRoom(ctx, tenantID, roomID, modules...)
		if reloadErr != nil {
			return fmt.Errorf("room %d hot reload: %w", roomID, reloadErr)
		}
		log.Printf("live plan hot applied tenant=%d plan=%d room=%d revision=%d modules=%v", tenantID, planID, roomID, state.HotRevision, state.HotModules)
	}
	return nil
}

func (s *Server) allocateInitialLiveQuotaLease(
	ctx context.Context,
	tenantID, roomID, sessionID int64,
	coreBootID string,
	coreWorkingStart uint64,
) (uint64, error) {
	if strings.TrimSpace(coreBootID) == "" {
		return 0, errors.New("core boot id is missing")
	}
	grants, err := s.store.AllocateLiveQuotaLeases(ctx, tenantID, []appdb.LiveQuotaLeaseRequest{{
		RoomID:                  roomID,
		RuntimeSessionID:        sessionID,
		CoreBootID:              coreBootID,
		CoreWorkingStartSeconds: coreWorkingStart,
		RequestedSeconds:        appdb.LiveQuotaLeaseSeconds,
	}}, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	for _, grant := range grants {
		if grant.RuntimeSessionID == sessionID && grant.AllocatedSeconds > 0 {
			return grant.AllocatedSeconds, nil
		}
	}
	return 0, appdb.ErrLiveQuotaExhausted
}

func (s *Server) requestCoreAgentStart(
	ctx context.Context,
	tenantID, roomID int64,
	baseWorkingSeconds uint64,
	leaseSeconds uint64,
) error {
	_, err := s.putCoreAgentState(ctx, tenantID, roomID, map[string]any{
		"command":              "start",
		"base_working_seconds": baseWorkingSeconds,
		"lease_seconds":        leaseSeconds,
	})
	return err
}

func (s *Server) requestCorePublishedPlanProgramStart(
	ctx context.Context,
	tenantID, roomID, runtimeSessionID int64,
	version model.LiveAgentPlanVersion,
) error {
	if version.ID <= 0 || version.VersionNo <= 0 || version.RoomID != roomID {
		return errors.New("published live agent plan version is invalid")
	}
	// "开始" must always create a fresh playback cursor. Clear any stale
	// paused/running program before loading the current published version.
	if err := s.requestCorePublishedPlanProgramControl(ctx, tenantID, roomID, "stop"); err != nil {
		return fmt.Errorf("reset previous audio program: %w", err)
	}
	tracks := make([]map[string]any, 0, len(version.Variants))
	for _, variant := range version.Variants {
		if !variant.IsFormal {
			continue
		}
		audioURL := strings.TrimSpace(variant.AudioURL)
		if variant.AudioAssetID > 0 && s.assetStorage != nil {
			asset, assetErr := s.store.GetMediaAsset(ctx, tenantID, variant.AudioAssetID)
			if assetErr != nil {
				return fmt.Errorf("%s稿声音资产不存在: %w", strings.TrimSpace(variant.VariantKey), assetErr)
			}
			assetStore, storeErr := s.assetStorage.For(asset.StorageDriver)
			if storeErr != nil {
				return fmt.Errorf("%s稿声音存储不可用: %w", strings.TrimSpace(variant.VariantKey), storeErr)
			}
			expiry := 12 * time.Hour
			if s.assetURLTTL > expiry {
				expiry = s.assetURLTTL
			}
			if signedURL, signErr := assetStore.SignedURL(ctx, asset.ObjectKey, expiry); signErr != nil {
				return fmt.Errorf("%s稿声音地址签发失败: %w", strings.TrimSpace(variant.VariantKey), signErr)
			} else if strings.TrimSpace(signedURL) != "" {
				audioURL = strings.TrimSpace(signedURL)
			}
		}
		if audioURL == "" {
			return fmt.Errorf("%s稿缺少正式声音", strings.TrimSpace(variant.VariantKey))
		}
		label := strings.TrimSpace(variant.Title)
		if label == "" {
			label = strings.TrimSpace(variant.VariantKey) + "稿"
		}
		tracks = append(tracks, map[string]any{
			"id":          strings.TrimSpace(variant.VariantKey),
			"label":       label,
			"text":        strings.TrimSpace(variant.Text),
			"audio_url":   audioURL,
			"duration_ms": variant.AudioDurationMS,
			"timeline":    variant.Timeline,
			"safe_points": variant.SafePoints,
		})
	}
	if len(tracks) == 0 {
		return errors.New("published live agent plan has no audio tracks")
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/audio/program/start", roomID),
		query,
		map[string]any{
			"session_id": fmt.Sprintf("live-runtime-%d", runtimeSessionID),
			"label":      fmt.Sprintf("直播智能体 V%d 主线", version.VersionNo),
			"version_id": version.ID,
			"version_no": version.VersionNo,
			"tracks":     tracks,
		},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core audio program start status %d", resp.StatusCode)
	}
	return nil
}

func (s *Server) requestCorePublishedPlanProgramControl(
	ctx context.Context,
	tenantID, roomID int64,
	action string,
) error {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "pause" && action != "resume" && action != "stop" {
		return errors.New("invalid audio program action")
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/audio/program/%s", roomID, action),
		query,
		map[string]any{},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core audio program %s status %d", action, resp.StatusCode)
	}
	return nil
}

func (s *Server) grantCoreAgentLease(
	ctx context.Context,
	tenantID, roomID int64,
	leaseSeconds uint64,
) error {
	if leaseSeconds == 0 {
		return nil
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx, tenantID, roomID, http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query,
		map[string]any{"lease_seconds": leaseSeconds},
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

func (s *Server) requestCoreAgentStop(
	ctx context.Context,
	tenantID, roomID int64,
	baseWorkingSeconds uint64,
	stopReason string,
) (coreAgentRuntimeState, error) {
	body := map[string]any{
		"command":              "stop",
		"base_working_seconds": baseWorkingSeconds,
	}
	if strings.TrimSpace(stopReason) != "" {
		body["stop_reason"] = strings.TrimSpace(stopReason)
	}
	return s.putCoreAgentState(ctx, tenantID, roomID, body)
}

func (s *Server) putCoreAgentState(
	ctx context.Context,
	tenantID, roomID int64,
	body map[string]any,
) (coreAgentRuntimeState, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		ctx, tenantID, roomID, http.MethodPut,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", roomID),
		query, body,
	)
	if err != nil {
		return coreAgentRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return coreAgentRuntimeState{}, fmt.Errorf("core agent runtime status %d", resp.StatusCode)
	}
	var runtime coreAgentRuntimeState
	if err := json.NewDecoder(resp.Body).Decode(&runtime); err != nil {
		return coreAgentRuntimeState{}, err
	}
	return runtime, nil
}

func (s *Server) setCoreDeviceOnline(
	ctx context.Context,
	tenantID, roomID int64,
	online bool,
) error {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))

	resp, err := s.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPatch,
		fmt.Sprintf("/internal/v1/rooms/%d/runtime", roomID),
		query,
		map[string]any{"device_online": online},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core runtime status %d", resp.StatusCode)
	}
	return nil
}
