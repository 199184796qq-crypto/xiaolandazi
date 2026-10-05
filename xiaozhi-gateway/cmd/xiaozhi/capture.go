package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image/jpeg"
	"io"
	"net/http"
	"time"
)

const maxCaptureBytes = 2 << 20

func (g *gateway) capture(w http.ResponseWriter, r *http.Request) {
	if g.cfg.managementBaseURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "设备拍照服务未启用"})
		return
	}
	grant, err := g.parseCaptureGrant(r.URL.Query().Get("grant"), time.Now())
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "拍照授权无效或已过期"})
		return
	}
	// Ownership may have been released/reclaimed since the command was issued.
	p, err := g.provision(r.Context(), grant.MAC)
	if err != nil || p.TenantID != grant.TenantID || p.DeviceID != grant.DeviceID || (p.State != "claimed" && p.State != "bound") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "设备归属已改变，请重新发起拍照"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCaptureBytes+(64<<10))
	if err := r.ParseMultipartForm(maxCaptureBytes + (64 << 10)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "照片过大或上传格式无效"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有收到照片"})
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxCaptureBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "照片不得超过2MB"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCaptureBytes+1))
	if err != nil || len(data) > maxCaptureBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "照片不得超过2MB"})
		return
	}
	configuration, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > 2048 || configuration.Height > 2048 {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "只接受设备拍摄的有效JPEG照片"})
		return
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "JPEG照片内容不完整"})
		return
	}
	digest := sha256.Sum256(data)
	cached, err := g.controls.beginCapture(grant, digest)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if cached != nil {
		writeJSON(w, http.StatusOK, cached)
		return
	}
	lease := controlLease{MAC: grant.MAC, ClientID: grant.ClientID, RequestID: grant.RequestID, DeviceID: grant.DeviceID, TenantID: grant.TenantID}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stopCancel := context.AfterFunc(g.controls.captureContext(grant), cancel)
	defer stopCancel()
	var response captureResponse
	err = g.deviceUpload(ctx, "/internal/v1/xiaozhi/capture", "environment.jpg", "image/jpeg", data, lease, &response)
	success := err == nil && response.Accepted && response.RequestID == grant.RequestID && response.BillingMode == "disabled" && response.ChargedBeans == 0
	g.controls.finishCapture(grant, response, success)
	if !success {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "照片接收失败，请稍后重新拍照"})
		return
	}
	writeJSON(w, http.StatusOK, response)
}
