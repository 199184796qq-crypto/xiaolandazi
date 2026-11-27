package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// The device gateway is a loopback service. Wi-Fi credentials are passed to
// the active connection only and are excluded from persistence and responses.
const terminalWifiGatewayURL = "http://127.0.0.1:8083/internal/v1/terminal-wifi"

type terminalWifiInput struct {
	Action       string `json:"action"`
	RequestID    string `json:"request_id,omitempty"`
	Code         string `json:"code,omitempty"`
	SSID         string `json:"ssid,omitempty"`
	OriginalSSID string `json:"original_ssid,omitempty"`
	Password     string `json:"password"`
}

func (s *Server) liveDeviceWifi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	device, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	var input terminalWifiInput
	if err := maintenanceRead(r, &input, 4096); err != nil || (input.Action != "challenge" && input.Action != "list" && input.Action != "save") {
		writeError(w, http.StatusBadRequest, "Wi-Fi 配置参数无效")
		return
	}
	if _, err := s.store.GetLiveDevice(r.Context(), tenant, device); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "当前账号下没有这台设备")
		} else {
			writeError(w, http.StatusServiceUnavailable, "读取设备状态失败，请稍后重试")
		}
		return
	}
	if s.xiaozhiInternalToken == "" {
		writeError(w, http.StatusServiceUnavailable, "Wi-Fi 配置暂不可用")
		return
	}
	payload, err := json.Marshal(struct {
		terminalWifiInput
		TenantID int64 `json:"tenant_id"`
		DeviceID int64 `json:"device_id"`
		UserID   int64 `json:"user_id"`
	}{terminalWifiInput: input, TenantID: tenant, DeviceID: device, UserID: actor.UserID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "Wi-Fi 配置参数无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, terminalWifiGatewayURL, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Wi-Fi 配置暂不可用")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xiaozhi-Internal-Token", s.xiaozhiInternalToken)
	// Keep the token and submitted password on loopback even if the service
	// returns an unexpected redirect.
	client := http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "连接盒子超时，请核对设备后重试")
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest {
		writeError(w, http.StatusBadRequest, "请核对 8 位配网码、Wi-Fi 名称和密码格式")
		return
	}
	var out struct {
		Status    string `json:"status"`
		RequestID string `json:"request_id,omitempty"`
		Networks  []struct {
			SSID    string `json:"ssid"`
			Pending bool   `json:"pending,omitempty"`
		} `json:"networks"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&out) != nil {
		writeError(w, http.StatusServiceUnavailable, "Wi-Fi 配置暂不可用")
		return
	}
	switch out.Status {
	case "ready":
		if input.Action == "challenge" && len(out.RequestID) == 22 {
			writeJSON(w, http.StatusOK, map[string]string{"status": out.Status, "request_id": out.RequestID})
			return
		}
	case "listed":
		if input.Action == "list" && out.Networks != nil && len(out.Networks) <= 11 {
			writeJSON(w, http.StatusOK, map[string]any{"status": out.Status, "networks": out.Networks})
			return
		}
	case "saved":
		if input.Action == "save" {
			writeJSON(w, http.StatusOK, map[string]string{"status": out.Status})
			return
		}
	case "wrong_code":
		writeError(w, http.StatusUnprocessableEntity, "配网码有误，请核对盒子屏幕")
		return
	case "expired":
		writeError(w, http.StatusConflict, "配网码已过期，请重新获取")
		return
	case "busy":
		writeError(w, http.StatusConflict, "盒子正在等待配网确认，请稍后再试")
		return
	case "storage_error":
		writeError(w, http.StatusConflict, "盒子保存 Wi-Fi 失败，请重试")
		return
	}
	writeError(w, http.StatusConflict, "盒子暂未确认操作。请确认设备在线、空闲并已支持网页配网")
}
