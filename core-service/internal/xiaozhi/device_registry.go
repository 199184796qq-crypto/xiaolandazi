package xiaozhi

import (
	"sync"
	"time"
)

// DeviceState describes an ESP32-S3 Xiaozhi terminal connected to the gateway.
// It is intentionally independent from Room so device lifecycle can be managed
// before a device is bound to a live room.
type DeviceState struct {
	DeviceID      string    `json:"device_id"`
	SN            string    `json:"sn"`
	BoundRoomID   int64     `json:"bound_room_id,omitempty"`
	Online        bool      `json:"online"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Registry keeps the runtime state of connected Xiaozhi terminals.
// Persistence and websocket gateway integration are layered above this.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]DeviceState
}

func NewRegistry() *Registry {
	return &Registry{devices: make(map[string]DeviceState)}
}

func (r *Registry) Heartbeat(device DeviceState) DeviceState {
	now := time.Now()
	device.Online = true
	device.LastHeartbeat = now
	device.UpdatedAt = now

	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[device.DeviceID] = device
	return device
}

func (r *Registry) Offline(deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, ok := r.devices[deviceID]
	if !ok {
		return
	}
	device.Online = false
	device.UpdatedAt = time.Now()
	r.devices[deviceID] = device
}

func (r *Registry) Get(deviceID string) (DeviceState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	device, ok := r.devices[deviceID]
	return device, ok
}
