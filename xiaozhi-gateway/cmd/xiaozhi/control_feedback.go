package main

import (
	"context"
	"errors"
	"log"
	"time"
)

type feedbackPermit struct {
	requestID   string
	phase       string
	listenAfter bool
	expiresAt   time.Time
}

type terminalResult struct {
	requestID string
	status    string
	message   string
	err       error
}

func validAddressing(value string) string {
	switch value {
	case "female", "male", "child":
		return value
	default:
		return "neutral"
	}
}

func greetingMessage(addressing string) string {
	switch validAddressing(addressing) {
	case "female":
		return "靓女，有什么吩咐？"
	case "male":
		return "帅哥，有什么吩咐？"
	case "child":
		return "小伙伴，有什么吩咐？"
	default:
		return "我在，有什么吩咐？"
	}
}

func acceptedMessage(addressing string) string {
	switch validAddressing(addressing) {
	case "female":
		return "靓女稍等，马上就干。"
	case "male":
		return "帅哥稍等，马上就干。"
	case "child":
		return "小伙伴稍等，马上就干。"
	default:
		return "好的，稍等，马上就干。"
	}
}

func feedbackKey(requestID, phase string) string { return requestID + "\n" + phase }

func (control *terminalControlSession) feedback(phase, message string, listenAfter bool) {
	if !control.feedbackEnabled || control.suppressFeedback {
		return
	}
	now := time.Now()
	for key, permit := range control.feedbackPermits {
		if !now.Before(permit.expiresAt) {
			delete(control.feedbackPermits, key)
		}
	}
	key := feedbackKey(control.lease.RequestID, phase)
	if _, sent := control.feedbackPermits[key]; sent {
		return
	}
	if len(control.feedbackPermits) >= 32 {
		var oldestKey string
		var oldest time.Time
		for key, permit := range control.feedbackPermits {
			if oldestKey == "" || permit.expiresAt.Before(oldest) {
				oldestKey, oldest = key, permit.expiresAt
			}
		}
		delete(control.feedbackPermits, oldestKey)
	}
	control.feedbackPermits[key] = feedbackPermit{requestID: control.lease.RequestID, phase: phase, listenAfter: listenAfter, expiresAt: now.Add(90 * time.Second)}
	payload := map[string]any{"type": "control_feedback", "request_id": control.lease.RequestID, "phase": phase, "addressing": validAddressing(control.addressing), "message": message, "listen_after": listenAfter}
	if phase == "completed" {
		if control.command.Action == "capture" {
			payload["prompt"] = "capture_completed"
		} else if control.command.Operation == "query" {
			payload["prompt"] = "info"
		}
	}
	_ = control.writer.json(payload)
}

func (control *terminalControlSession) feedbackPlayback(message controlMessage, now time.Time) {
	if !control.feedbackEnabled {
		return
	}
	key := feedbackKey(message.RequestID, message.Phase)
	permit, exists := control.feedbackPermits[key]
	if !exists || !now.Before(permit.expiresAt) {
		return
	}
	switch message.State {
	case "start":
		if control.feedbackActive != "" {
			return
		}
		control.feedbackActive = key
		control.feedbackDeadline = now.Add(15 * time.Second)
		control.writer.pauseFeedbackAudio(true)
	case "stop":
		if control.feedbackActive != key {
			return
		}
		if permit.listenAfter {
			// Cover the short greeting->new listen transition. A missing start
			// cannot leave the terminal permanently muted.
			control.feedbackActive = "await-listen"
			control.feedbackDeadline = now.Add(2 * time.Second)
		} else {
			control.feedbackActive = ""
			control.writer.pauseFeedbackAudio(false)
		}
		delete(control.feedbackPermits, key)
	}
}

func (control *terminalControlSession) clearFeedbackPlayback() {
	control.feedbackActive = ""
	control.feedbackDeadline = time.Time{}
	control.writer.pauseFeedbackAudio(false)
}

func (control *terminalControlSession) greet() {
	// The dedicated wake_greeting ASR result is already a persisted succeeded
	// no-operation event. It must never pass through command-dispatch.
	control.g.controls.release(control.lease)
	control.writer.pauseRoomAudio(false)
	message := greetingMessage(control.addressing)
	// Queue the local prompt before done removes the firmware's processing gate.
	control.feedback("greeting", message, true)
	control.status("done", message)
	control.state = ""
}

func (control *terminalControlSession) persistTerminal(status, message string, value *int) {
	control.state = "finishing"
	control.deadline = time.Now().Add(12 * time.Second)
	lease, command := control.lease, control.command
	ctx, cancel := context.WithTimeout(control.ctx, 10*time.Second)
	control.cancelASR = cancel
	go func() {
		err := control.g.persistCommandEvent(ctx, lease, command, status, value, message)
		select {
		case control.terminals <- terminalResult{requestID: lease.RequestID, status: status, message: message, err: err}:
		case <-ctx.Done():
		}
	}()
}

func (control *terminalControlSession) committed(result terminalResult) {
	if control.state != "finishing" || result.requestID != control.lease.RequestID {
		return
	}
	if control.cancelASR != nil {
		control.cancelASR()
		control.cancelASR = nil
	}
	if result.err != nil {
		log.Printf("device command receipt unavailable request_id=%s", control.lease.RequestID)
		control.finalTerminal("failed", "操作结果未确认，请查看屏幕或稍后重试")
		return
	}
	control.finalTerminal(result.status, result.message)
}

func (control *terminalControlSession) finalTerminal(status, message string) {
	control.g.controls.release(control.lease)
	control.writer.pauseRoomAudio(false)
	// Enhanced firmware keeps busy through ACK -> persisted receipt. Its prompt
	// queue must take ownership before terminal status clears that processing gate.
	if status == "succeeded" {
		spoken := "已经调整好了。"
		if control.command.Action == "capture" {
			spoken = "已经拍好并上传了。"
		} else if control.command.Operation == "query" {
			spoken = "结果已经显示在屏幕上了。"
		}
		control.feedback("completed", spoken, false)
		control.status("done", message)
	} else {
		control.feedback("failed", "这次没执行成功，请再试一次。", false)
		control.status("error", message)
	}
	control.state, control.packets = "", nil
}

func (control *terminalControlSession) feedbackTick(now time.Time) {
	if control.feedbackActive != "" && !now.Before(control.feedbackDeadline) {
		delete(control.feedbackPermits, control.feedbackActive)
		control.clearFeedbackPlayback()
	}
	if control.state == "finishing" && !now.Before(control.deadline) {
		control.committed(terminalResult{requestID: control.lease.RequestID, err: errors.New("command receipt timeout")})
	}
}
