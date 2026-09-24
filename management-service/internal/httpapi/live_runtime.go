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
	writeJSON(w, http.StatusOK, quotaSummary)
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
	quotaSummary, err := s.store.GetLiveQuotaSummary(r.Context(), tenantID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取AI时长失败")
		return
	}

	snapshot := model.LiveRuntimeSnapshot{
		QuotaRemainingSeconds:  quotaSummary.ActiveSeconds,
		ReserveTimeCardSeconds: quotaSummary.ReserveTimeCardSeconds,
		ReserveTimeCardCount:   quotaSummary.ReserveTimeCardCount,
		CurrentQuota:           quotaSummary.Current,
		TimeCards:              quotaSummary.TimeCards,
		RoomLive:               room.Status == "live",
	}
	session, err := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID)
	if err == nil {
		snapshot.Session = &session
		if session.Status == "running" && session.DeviceID != nil {
			if device, deviceErr := s.store.GetLiveDevice(r.Context(), tenantID, *session.DeviceID); deviceErr == nil {
				snapshot.Device = &device
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取AI运行状态失败")
		return
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
		case errors.Is(err, appdb.ErrLiveDeviceOffline):
			writeError(w, http.StatusConflict, "绑定设备当前不在线")
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

	_ = s.setCoreDeviceOnline(r.Context(), tenantID, roomID, true)
	writeJSON(w, http.StatusCreated, session)
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

	session, err := s.store.StopLiveRuntimeSession(
		r.Context(),
		tenantID,
		roomID,
		actor.UserID,
		reason,
		time.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, appdb.ErrLiveRuntimeNotRunning) {
			writeError(w, http.StatusConflict, "当前直播间AI没有在工作")
			return
		}
		writeError(w, http.StatusInternalServerError, "停止AI直播伴播失败")
		return
	}

	_ = s.setCoreDeviceOnline(r.Context(), tenantID, roomID, false)
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
