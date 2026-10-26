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
	pending   []deviceProbe
	sequence  uint64
	confirmed chan struct{}
}

type deviceProbe struct {
	nonce string
	sent  time.Time
}

func newDevicePresence(now time.Time) *devicePresence {
	return &devicePresence{lastSeen: now, confirmed: make(chan struct{}, 1)}
}
func (p *devicePresence) challenge(session string) string {
	return p.challengeAt(session, time.Now())
}

func (p *devicePresence) challengeAt(session string, now time.Time) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(now)
	p.sequence++
	nonce := session + "-" + strconv.FormatUint(p.sequence, 10)
	p.pending = append(p.pending, deviceProbe{nonce: nonce, sent: now})
	// A delayed response can still prove liveness after a newer probe has been
	// sent. Keep only the response window, never an unbounded/replayable nonce set.
	const limit = int(deviceResponseTimeout/deviceProbeInterval) + 1
	if len(p.pending) > limit {
		p.pending = p.pending[len(p.pending)-limit:]
	}
	return nonce
}

func (p *devicePresence) prune(now time.Time) {
	kept := p.pending[:0]
	for _, probe := range p.pending {
		if age := now.Sub(probe.sent); age >= 0 && age < deviceResponseTimeout {
			kept = append(kept, probe)
		}
	}
	p.pending = kept
}
func (p *devicePresence) pong(nonce string, now time.Time) bool {
	p.mu.Lock()
	p.prune(now)
	index := -1
	for i, probe := range p.pending {
		if nonce != "" && nonce == probe.nonce {
			index = i
			break
		}
	}
	if index < 0 || now.Before(p.lastSeen) {
		p.mu.Unlock()
		return false
	}
	p.pending = append(p.pending[:index], p.pending[index+1:]...)
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
