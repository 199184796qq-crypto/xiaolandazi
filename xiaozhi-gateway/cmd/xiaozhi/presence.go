package main

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const deviceProbeInterval = 5 * time.Second
const deviceResponseTimeout = 20 * time.Second

type devicePresence struct {
	mu        sync.Mutex
	lastSeen  time.Time
	nonce     string
	sequence  uint64
	confirmed chan struct{}
}

func newDevicePresence(now time.Time) *devicePresence {
	return &devicePresence{lastSeen: now, confirmed: make(chan struct{}, 1)}
}
func (p *devicePresence) challenge(session string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sequence++
	p.nonce = session + "-" + strconv.FormatUint(p.sequence, 10)
	return p.nonce
}
func (p *devicePresence) pong(nonce string, now time.Time) bool {
	p.mu.Lock()
	if nonce == "" || nonce != p.nonce {
		p.mu.Unlock()
		return false
	}
	p.nonce = ""
	p.lastSeen = now
	p.mu.Unlock()
	select {
	case p.confirmed <- struct{}{}:
	default:
	}
	return true
}
func (p *devicePresence) seen() time.Time { p.mu.Lock(); defer p.mu.Unlock(); return p.lastSeen }

func (g *gateway) reportConnection(ctx context.Context, mac, connectionID string, p provisioning, phase string, seen time.Time) (string, bool, error) {
	var out struct {
		DisplayStatus string `json:"display_status"`
		Applied       bool   `json:"applied"`
		StopPending   bool   `json:"stop_pending"`
	}
	err := g.managementRequest(ctx, "/internal/v1/xiaozhi/connection", map[string]any{
		"hardware_mac": mac, "connection_id": connectionID, "tenant_id": p.TenantID, "device_id": p.DeviceID, "phase": phase, "observed_at": seen.UTC(),
	}, &out)
	return out.DisplayStatus, out.Applied && !out.StopPending, err
}

func (c *lockedConn) ping(nonce string) error {
	// WriteControl is safe alongside the serialized Opus writer and cannot
	// wait behind an arbitrarily long audio write while the box is offline.
	return c.conn.WriteControl(websocket.PingMessage, []byte(nonce), time.Now().Add(2*time.Second))
}
