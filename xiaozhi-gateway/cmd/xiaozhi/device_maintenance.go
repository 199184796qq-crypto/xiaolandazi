package main

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Optional, versioned capabilities. An old or malformed profile never changes
// the existing audio/control connection behavior.
type deviceMaintenanceProfile struct {
	Board           string `json:"board"`
	Chip            string `json:"chip"`
	FlashBytes      int64  `json:"flash_bytes"`
	PartitionLayout string `json:"partition_layout"`
	FirmwareVersion string `json:"firmware_version"`
	FirmwareSHA256  string `json:"firmware_sha256"`
	BootID          string `json:"boot_id"`
	Storage         string `json:"storage"`
}

// Record only bounded categories and the protocol close code. Error text may
// contain arbitrary remote data and must not reach device diagnostic logs.
func deviceReadEnd(err error) (string, int) {
	var closed *websocket.CloseError
	if errors.As(err, &closed) {
		return "peer_close", closed.Code
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "read_timeout", 0
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "transport_eof", 0
	}
	return "transport_read_error", 0
}

func validMaintenanceProfile(p *deviceMaintenanceProfile) bool {
	if p == nil || p.Board != "bread-compact-wifi-s3cam" || p.Chip != "esp32s3" || p.FlashBytes != 16<<20 || p.PartitionLayout != "v2-16m" {
		return false
	}
	if p.FirmwareVersion == "" || len(p.FirmwareVersion) > 64 || strings.ContainsAny(p.FirmwareVersion, "\r\n\x00") {
		return false
	}
	if len(p.FirmwareSHA256) != 64 || len(p.BootID) < 16 || len(p.BootID) > 64 {
		return false
	}
	if _, err := hex.DecodeString(p.FirmwareSHA256); err != nil {
		return false
	}
	if _, err := hex.DecodeString(p.BootID); err != nil {
		return false
	}
	return p.Storage == "partition" || p.Storage == "rtc" || p.Storage == "ram"
}

type deviceFirmwareOffer struct {
	Version         string `json:"version"`
	URL             string `json:"url"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	Board           string `json:"board"`
	Chip            string `json:"chip"`
	FlashBytes      int64  `json:"flash_bytes"`
	PartitionLayout string `json:"partition_layout"`
}

type deviceDiagnosticsGrant struct {
	UploadURL      string `json:"upload_url"`
	Token          string `json:"token"`
	OwnerEpoch     string `json:"owner_epoch"`
	ExpiresAt      int64  `json:"expires_at"`
	MaxBatchBytes  int    `json:"max_batch_bytes"`
	MaxBatchEvents int    `json:"max_batch_events"`
}

type deviceMaintenanceResponse struct {
	Diagnostics *deviceDiagnosticsGrant `json:"diagnostics,omitempty"`
	Firmware    *deviceFirmwareOffer    `json:"firmware,omitempty"`
}

func maintenanceRequest(mac, connectionID string, p *deviceMaintenanceProfile) map[string]any {
	return map[string]any{"hardware_mac": mac, "connection_id": connectionID, "device_profile": p}
}

func validHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

func validFirmwareOffer(f *deviceFirmwareOffer, p *deviceMaintenanceProfile) bool {
	if f == nil || !validMaintenanceProfile(p) || f.Version == "" || f.Version == p.FirmwareVersion || len(f.Version) > 64 || !validHTTPS(f.URL) || len(f.SHA256) != 64 || f.Size <= 0 || f.Size > 0x3f0000 {
		return false
	}
	if _, err := hex.DecodeString(f.SHA256); err != nil {
		return false
	}
	return f.Board == p.Board && f.Chip == p.Chip && f.FlashBytes == p.FlashBytes && f.PartitionLayout == p.PartitionLayout
}

func validDiagnosticsGrant(d *deviceDiagnosticsGrant) bool {
	if d == nil || !validHTTPS(d.UploadURL) || d.Token == "" || len(d.Token) > 4096 || len(d.OwnerEpoch) != 64 || d.MaxBatchBytes <= 0 || d.MaxBatchBytes > 32768 || d.MaxBatchEvents <= 0 || d.MaxBatchEvents > 128 {
		return false
	}
	if _, err := hex.DecodeString(d.OwnerEpoch); err != nil {
		return false
	}
	return d.ExpiresAt > time.Now().Unix() && d.ExpiresAt < time.Now().Add(15*time.Minute).Unix()
}

func (g *gateway) startupFirmwareOffer(ctx context.Context, mac string, p *deviceMaintenanceProfile) *deviceFirmwareOffer {
	if !validMaintenanceProfile(p) || g.cfg.managementBaseURL == "" {
		return nil
	}
	var result deviceMaintenanceResponse
	if g.managementRequest(ctx, "/internal/v1/xiaozhi/firmware-check", maintenanceRequest(mac, "", p), &result) != nil || !validFirmwareOffer(result.Firmware, p) {
		return nil
	}
	return result.Firmware
}

// Optional HTTP and refresh run outside the presence/control/audio loop. A
// missing endpoint, bad catalog or slow maintenance service cannot close it.
func (g *gateway) runDeviceMaintenance(ctx context.Context, writer *lockedConn, mac, connectionID string, p *deviceMaintenanceProfile) {
	if !validMaintenanceProfile(p) {
		return
	}
	refresh := time.NewTicker(90 * time.Second)
	defer refresh.Stop()
	for {
		var result deviceMaintenanceResponse
		err := g.managementRequest(ctx, "/internal/v1/xiaozhi/maintenance", maintenanceRequest(mac, connectionID, p), &result)
		if err == nil && ctx.Err() == nil {
			if !validDiagnosticsGrant(result.Diagnostics) {
				result.Diagnostics = nil
			}
			if !validFirmwareOffer(result.Firmware, p) {
				result.Firmware = nil
			}
			if result.Diagnostics != nil || result.Firmware != nil {
				// Maintenance is best effort. Never wait on the audio writer lock.
				writer.tryMaintenance(ctx, result)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
		}
	}
}

func (c *lockedConn) tryMaintenance(ctx context.Context, payload deviceMaintenanceResponse) {
	if ctx.Err() != nil || !c.mu.TryLock() {
		return
	}
	defer c.mu.Unlock()
	if ctx.Err() != nil || (!c.lastAudioWrite.IsZero() && time.Since(c.lastAudioWrite) < 30*time.Second) {
		return
	}
	raw := struct {
		Type string `json:"type"`
		deviceMaintenanceResponse
	}{"device_maintenance", payload}
	// Send only while idle and use the normal transport deadline. A shorter
	// optional write could poison Gorilla's write state before normal audio
	// would time out; resetting a deadline cannot recover that state.
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = c.conn.WriteJSON(raw)
}
