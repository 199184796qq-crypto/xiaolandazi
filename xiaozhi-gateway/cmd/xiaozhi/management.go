package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type provisioning struct {
	DeviceID    int64      `json:"device_id"`
	DeviceName  string     `json:"device_name"`
	TenantID    int64      `json:"tenant_id"`
	RoomID      int64      `json:"room_id"`
	BindingRole string     `json:"binding_role"`
	RoomName    string     `json:"room_name"`
	RoomNumber  string     `json:"room_number"`
	State       string     `json:"state"`
	Message     string     `json:"message"`
	Code        string     `json:"binding_code"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func (g *gateway) managementRequest(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.managementBaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xiaozhi-Internal-Token", g.cfg.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("management http=%d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
}
func (g *gateway) provision(ctx context.Context, mac string) (provisioning, error) {
	if g.cfg.managementBaseURL == "" {
		room, bound := g.bindings.room(mac)
		p := provisioning{RoomID: room, State: "claimed", Message: "请绑定直播间"}
		if bound {
			p.State = "bound"
			p.Message = "连续待机"
		}
		return p, nil
	}
	var p provisioning
	err := g.managementRequest(ctx, "/internal/v1/xiaozhi/provision", map[string]string{"hardware_mac": mac}, &p)
	return p, err
}
func (g *gateway) reportHeartbeat(ctx context.Context, mac string, roomID int64) (string, error) {
	if g.cfg.managementBaseURL == "" {
		return "连续待机", nil
	}
	var out struct {
		DisplayStatus string `json:"display_status"`
	}
	err := g.managementRequest(ctx, "/internal/v1/xiaozhi/heartbeat", map[string]any{"hardware_mac": mac, "room_id": roomID}, &out)
	return out.DisplayStatus, err
}
