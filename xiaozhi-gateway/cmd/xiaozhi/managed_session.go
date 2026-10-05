package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// An unclaimed device keeps a limited connection for displaying its code. Only
// the management service's current room binding can start an audio stream.
func (g *gateway) managedDeviceSession(ctx context.Context, writer *lockedConn, mac, clientID, sessionID string, p provisioning, sampleRate int, commandsEnabled, cameraEnabled, feedbackEnabled bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reads := readMessages(ctx, writer.conn)
	control := newTerminalControlSession(ctx, g, writer, mac, clientID, sampleRate, commandsEnabled, cameraEnabled, feedbackEnabled)
	defer control.close()
	controlTicker := time.NewTicker(100 * time.Millisecond)
	defer controlTicker.Stop()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var audioCancel context.CancelFunc
	var audioDone chan error
	var activeRoom int64
	var lastStatus string
	stopAudio := func() {
		if audioCancel != nil {
			audioCancel()
			audioCancel = nil
		}
		activeRoom = 0
	}
	defer stopAudio()
	syncState := func(p provisioning) bool {
		if control.state != "" && (p.TenantID != control.lease.TenantID || p.DeviceID != control.lease.DeviceID || (p.State != "claimed" && p.State != "bound")) {
			control.finish("rejected", "设备归属已改变，本次指令已取消", nil)
		}
		status := p.Message
		if p.State == "claimed" || p.State == "bound" {
			heartbeatStatus, err := g.reportHeartbeat(ctx, mac, p.RoomID)
			if err != nil {
				stopAudio()
				return false
			}
			if p.RoomID > 0 {
				status = heartbeatStatus
			}
		}
		if activeRoom != p.RoomID || p.State != "bound" {
			stopAudio()
		}
		if p.State == "bound" && p.RoomID > 0 && audioCancel == nil && audioDone == nil {
			audioCtx, c := context.WithCancel(ctx)
			audioCancel = c
			activeRoom = p.RoomID
			done := make(chan error, 1)
			audioDone = done
			go func(room int64) { done <- g.streamRoomAudio(audioCtx, writer, room, sessionID) }(p.RoomID)
		}
		payload := map[string]any{"type": "device_status", "state": p.State, "message": strings.ReplaceAll(status, "小蓝直播", "小蓝搭子"), "binding_code": p.Code, "expires_at": p.ExpiresAt, "device_name": p.DeviceName, "room_id": p.RoomID, "binding_role": p.BindingRole, "room_name": p.RoomName, "room_number": p.RoomNumber}
		raw, _ := json.Marshal(payload)
		if string(raw) != lastStatus {
			if writer.json(payload) != nil {
				return false
			}
			lastStatus = string(raw)
		}
		return true
	}
	if !syncState(p) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := g.provision(ctx, mac)
			if err != nil {
				stopAudio()
				return
			}
			p = next
			if !syncState(p) {
				return
			}
		case <-audioDone:
			stopAudio()
			audioDone = nil
		case now := <-controlTicker.C:
			control.tick(now)
		case result := <-control.results:
			if control.state == "processing" && result.requestID == control.lease.RequestID {
				next, err := g.provision(ctx, mac)
				if err != nil || next.TenantID != control.lease.TenantID || next.DeviceID != control.lease.DeviceID || (next.State != "claimed" && next.State != "bound") {
					control.finish("rejected", "设备归属已改变，本次指令已取消", nil)
					continue
				}
			}
			control.recognised(result)
		case result := <-control.understandings:
			if control.state == "understanding" && result.requestID == control.lease.RequestID {
				next, err := g.provision(ctx, mac)
				if err != nil || next.TenantID != control.lease.TenantID || next.DeviceID != control.lease.DeviceID || (next.State != "claimed" && next.State != "bound") {
					control.finish("rejected", "设备归属已改变，本次指令已取消", nil)
					continue
				}
			}
			control.understood(result)
		case result := <-control.dispatches:
			control.dispatched(result)
		case result := <-control.terminals:
			control.committed(result)
		case read, open := <-reads:
			if !open || read.err != nil {
				return
			}
			if read.messageType == websocket.BinaryMessage {
				control.appendPacket(read.payload)
				continue
			}
			if read.messageType == websocket.TextMessage {
				var message controlMessage
				if json.Unmarshal(read.payload, &message) != nil {
					continue
				}
				switch message.Type {
				case "goodbye":
					return
				case "abort":
					if message.RequestID == "" || message.RequestID == control.lease.RequestID {
						control.abort()
					}
				case "listen":
					switch message.State {
					case "start":
						control.start(p, message.RequestID, time.Now())
					case "detect":
						control.wake(message.RequestID, message.Text)
					case "stop":
						if message.RequestID == "" || message.RequestID == control.lease.RequestID {
							control.process()
						}
						// The explicit start owns the command ID and must precede both
						// the authenticated detect marker and microphone audio frames.
					}
				case "device_control_ack":
					control.acknowledge(message)
				case "control_feedback_playback":
					control.feedbackPlayback(message, time.Now())
				}
			}
		}
	}
}
